package server

// This file implements the /templates gallery (phase-7 task 5):
// requirements.requirement.templates-gallery. It serves the embedded
// gallery page (assets.Pages(), "gallery.html.tmpl"; phase-7 task 3) populated
// from the project's template registry, built from its loaded templates/
// library.
//
// For every registered template it shows the template's name, usage
// (slide|section), one-line description, the example slide RENDERED through
// internal/render, and the field/section/body documentation the page's context
// documents: each field's type, required flag, default, applied limits and
// formats; each declared section's accepted templates and repeat bounds; and the
// implied body rule's mode and limits.
//
// A slide-usage template's example is a complete slide source, so it is parsed
// with internal/slide.Parse and rendered with render.RenderSlide directly. A
// section-usage template's example is a section FRAGMENT, not a whole slide: it
// is wrapped in a synthetic single-section slide template (the same pattern
// internal/validate.ValidateBuiltinExamples uses to exercise a section example),
// registered into a copy of the registry so the real registry is never mutated,
// and rendered with render.RenderSlide. That reuses the one renderer, so the
// gallery preview shows exactly what a deck slide would.
//
// The gallery is static: it is built once from the embedded page and the
// registry, and it neither reads the deck nor touches the network. The page's
// "live-reload" block stays empty.
//
// internal/template imports internal/assets, never the reverse; this file is
// the only server code that reaches into internal/template, and it registers
// its route through (Server).Handle, the seam internal/server exposes for extra
// routes.

import (
	"bytes"
	"errors"
	"fmt"
	htmltmpl "html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/really-knows-ai/kalide/internal/assets"
	"github.com/really-knows-ai/kalide/internal/render"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
)

const (
	// galleryPath is the route the gallery is registered under, exactly the
	// path `eypres start`'s terminal output and the e2e test address.
	galleryPath = "/templates"

	// galleryPageName is the gallery page's path within the embedded pages
	// sub-tree (assets.Pages()), matching the accessor's documented layout.
	galleryPageName = "gallery.html.tmpl"

	// galleryTitle and gallerySubtitle are the page heading and the line under
	// it. An empty title would fall back to "Templates" in the page itself.
	galleryTitle    = "Templates"
	gallerySubtitle = "Built-in slide and section templates."

	// gallerySectionName is the single section name the synthetic wrapper
	// slide declares when rendering a section-usage template's example.
	gallerySectionName = "example"

	// gallerySyntheticBase names the synthetic wrapper slide template before
	// it is made unique against the registry.
	gallerySyntheticBase = "__eypres_gallery_example"
)

// galleryHandler returns the HTTP handler serving the /templates gallery for
// reg, the project's template registry (template.NewRegistryFromLibrary over
// the loaded Library). reg must be non-nil: a nil registry is a caller error,
// reported by the handler as a 500 rather than silently substituted. funcMap is
// the library's layout func map (template.LayoutFuncMap: `media` bound to the
// served templates/media URL base, plus the format functions), threaded into
// every example's render so a slide or section preview using `media` renders
// the served URL (templates-gallery, template-media). A nil reg or a failure
// to parse the embedded page is reported as a 500 by the handler, never a
// panic, so a mis-embed is visible rather than fatal.
//
// It is registered on the server's mux by Listen (server.go), so every running
// server serves the gallery as soon as it is listening. Tests may build one
// directly to render a gallery from a custom registry.
//
// The handler is stateless and safe for concurrent requests: the embedded page
// is parsed once here and the per-request documents are derived from the
// registry, whose templates are immutable.
func galleryHandler(reg *template.Registry, funcMap htmltmpl.FuncMap) http.Handler {
	page, pageErr := htmltmpl.New(galleryPageName).ParseFS(assets.Pages(), galleryPageName)
	var regErr error
	if reg == nil {
		regErr = errors.New("nil template registry")
	}
	return &galleryServer{reg: reg, regErr: regErr, page: page, pageErr: pageErr, funcMap: funcMap}
}

// galleryServer is the /templates handler. page and pageErr are the embedded
// page parsed once when the handler is built; pageErr is non-nil only when that
// parse failed, which for the embedded gallery page is a programming error.
// reg is the registry to document (built when the handler was built, so the
// handler holds only immutable values); regErr is non-nil only when a nil
// registry was given. funcMap is the library's layout func map every example
// preview renders with.
type galleryServer struct {
	reg     *template.Registry
	regErr  error
	page    *htmltmpl.Template
	pageErr error
	funcMap htmltmpl.FuncMap
}

// ServeHTTP renders the gallery. It accepts GET and HEAD (as every page does),
// answers 500 when the embedded page could not be parsed or the registry could
// not be built, and otherwise builds the document and writes it uncached.
func (g *galleryServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if g.pageErr != nil {
		http.Error(w, "server: parse gallery page: "+g.pageErr.Error(), http.StatusInternalServerError)
		return
	}
	if g.regErr != nil {
		http.Error(w, "server: gallery registry: "+g.regErr.Error(), http.StatusInternalServerError)
		return
	}

	entries := galleryEntries(g.reg, g.funcMap)

	var buf bytes.Buffer
	data := map[string]any{
		"Title":     galleryTitle,
		"Subtitle":  gallerySubtitle,
		"Templates": entries,
	}
	if err := g.page.ExecuteTemplate(&buf, galleryPageName, data); err != nil {
		http.Error(w, "server: execute gallery page: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page is derived from compiled-in templates, so it is stable, but it
	// is also served by a long-running `eypres start`; no-store keeps a stale
	// page from surviving a rebuild.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// The gallery's execution-context shapes, matching gallery.html.tmpl's
// documented `.Templates` entries. The field names are exactly the page's.

// galleryEntry is one template's card: its identity plus the rendered example
// and the documentation tables.
type galleryEntry struct {
	Name        string
	Usage       string
	Description string
	Example     htmltmpl.HTML
	Fields      []galleryField
	Sections    []gallerySection
	Body        *galleryBody
}

// galleryField is one row of the field documentation table.
type galleryField struct {
	Name        string
	Type        string
	Required    bool
	Default     string
	Limits      string
	Formats     string
	Description string
}

// gallerySection is one row of the section documentation table.
type gallerySection struct {
	Name        string
	Accepted    string
	Repeats     string
	Description string
}

// galleryBody is the implied-body documentation block.
type galleryBody struct {
	Mode    string
	Limits  string
	Summary string
}

// galleryEntries builds one galleryEntry per registered template, in the
// registry's sorted name order (Registry.Templates), rendering each example
// through internal/render. A template whose example cannot be parsed or
// rendered is not fatal: the card is still emitted, with the rendered preview
// omitted and the reason shown as escaped text, so one bad example never hides
// the rest of the gallery. Built-in examples are validated at build time
// (validate.ValidateBuiltinExamples), so such a failure is a programming error.
func galleryEntries(reg *template.Registry, funcMap htmltmpl.FuncMap) []galleryEntry {
	templates := reg.Templates()
	entries := make([]galleryEntry, 0, len(templates))
	for _, t := range templates {
		if t == nil {
			continue
		}
		entry := galleryEntry{
			Name:        t.Name,
			Usage:       string(t.Usage),
			Description: t.Description,
			Fields:      galleryFields(t),
			Sections:    gallerySections(t),
			Body: &galleryBody{
				Mode:    string(t.Body.Mode),
				Limits:  bodyLimits(t.Body),
				Summary: bodySummary(t.Body),
			},
		}
		example, err := galleryExample(t, reg, funcMap)
		if err != nil {
			entry.Example = htmltmpl.HTML(
				`<p class="gallery-page__empty">Example unavailable: ` +
					htmltmpl.HTMLEscapeString(err.Error()) + `</p>`)
		} else {
			entry.Example = example
		}
		entries = append(entries, entry)
	}
	return entries
}

// galleryExample renders one template's example slide through internal/render.
// A slide-usage template's example is a whole slide; a section-usage template's
// is a fragment and is rendered in a synthetic single-section slide context.
func galleryExample(t *template.Template, reg *template.Registry, funcMap htmltmpl.FuncMap) (htmltmpl.HTML, error) {
	if strings.TrimSpace(t.Example.Markdown) == "" {
		return "", nil
	}
	if t.Usage == template.UsageSection {
		return gallerySectionExample(t, reg, funcMap)
	}
	return gallerySlideExample(t, reg, funcMap)
}

// gallerySlideExample parses a slide-usage template's example source and
// renders it. The example's own `template:` names the slide template, so no
// synthetic context is needed. The slide's label is the template name, giving
// the preview a stable anchor id. funcMap is the library's layout func map
// (template.LayoutFuncMap), threaded to RenderSlide so a layout using `media`
// renders the served templates/media URL.
func gallerySlideExample(t *template.Template, reg *template.Registry, funcMap htmltmpl.FuncMap) (htmltmpl.HTML, error) {
	parsed, err := slide.Parse(galleryExampleFile(t), []byte(t.Example.Markdown), reg)
	if err != nil {
		return "", err
	}
	return render.RenderSlide(parsed, t.Name, reg, funcMap)
}

// gallerySectionExample renders a section-usage template's example fragment in
// a synthetic context: it registers a wrapper slide template (declaring one
// section accepting exactly t) into a copy of reg, wraps the fragment as that
// section's source, parses it and renders it through render.RenderSlide.
// funcMap is the library's layout func map, threaded through so a section
// layout using `media` renders the served templates/media URL. The real
// registry is never mutated, mirroring
// internal/validate.ValidateBuiltinExamples' treatment of a section example.
func gallerySectionExample(t *template.Template, reg *template.Registry, funcMap htmltmpl.FuncMap) (htmltmpl.HTML, error) {
	name := galleryFreeName(reg)
	// The section's layout is parsed and executed under its Layout.Name when
	// set (a library-sourced section's Layout.Name is its full manifest path,
	// e.g. "templates/sections/item/layout.html.tmpl"), falling back to its
	// template name otherwise — the same rule internal/render's layoutName
	// applies. The wrapper's synthetic layout must invoke that same handle,
	// not the bare template name, or `{{ template }}` looks up a name that
	// was never registered under.
	sectionLayoutName := t.Name
	if t.Layout.Name != "" {
		sectionLayoutName = t.Layout.Name
	}
	wrapper := &template.Template{
		Name:        name,
		Description: "Synthetic slide wrapping one section template's example.",
		Usage:       template.UsageSlide,
		Sections: []template.SectionDecl{{
			Name:     gallerySectionName,
			Accepted: []string{t.Name},
			Min:      1,
			Max:      1,
		}},
		Body: template.BodyRule{Mode: template.BodyOptional},
		// The wrapper exists only to give the section example a slide to live
		// in; its layout renders the one section instance through the section
		// template's own layout, invoked by its actual registered handle
		// (sectionLayoutName) rather than a hardcoded bare name.
		Layout: template.Layout{
			Name: name,
			Text: `<section class="ey-slide">{{ range .` + gallerySectionName +
				` }}{{ template "` + sectionLayoutName + `" . }}{{ end }}</section>`,
		},
	}

	rebuilt, err := registryWithTemplate(reg, wrapper)
	if err != nil {
		return "", fmt.Errorf("build synthetic slide context: %w", err)
	}

	src := "---\ntemplate: " + name + "\n---\n# " + gallerySectionName + "\n" + t.Example.Markdown
	parsed, err := slide.Parse(galleryExampleFile(t), []byte(src), rebuilt)
	if err != nil {
		return "", err
	}
	return render.RenderSlide(parsed, t.Name, rebuilt, funcMap)
}

// registryWithTemplate returns a copy of reg with extra registered, so the
// synthetic wrapper slide can be resolved without mutating the caller's
// registry. Content loading is skipped (the templates are already loaded);
// registration runs the same local definition check Register always runs.
func registryWithTemplate(reg *template.Registry, extra *template.Template) (*template.Registry, error) {
	rebuilt := template.NewRegistry(nil)
	for _, t := range reg.Templates() {
		if t == nil {
			continue
		}
		if err := rebuilt.Register(t); err != nil {
			return nil, err
		}
	}
	if err := rebuilt.Register(extra); err != nil {
		return nil, err
	}
	return rebuilt, nil
}

// galleryFreeName returns gallerySyntheticBase, or it with underscores appended
// until it names no registered template, so the synthetic wrapper never shadows
// a real template.
func galleryFreeName(reg *template.Registry) string {
	name := gallerySyntheticBase
	for {
		if _, ok := reg.Lookup(name); !ok {
			return name
		}
		name += "_"
	}
}

// galleryExampleFile is the identity used when parsing an example, matching the
// convention internal/validate uses (`<template>/example.md`).
func galleryExampleFile(t *template.Template) string {
	return t.Name + "/example.md"
}

// galleryFields renders a template's field schema, in declaration order.
func galleryFields(t *template.Template) []galleryField {
	out := make([]galleryField, 0, len(t.Fields))
	for i := range t.Fields {
		f := &t.Fields[i]
		out = append(out, galleryField{
			Name:        f.Name,
			Type:        string(f.Type),
			Required:    f.Required,
			Default:     fieldDefault(f),
			Limits:      fieldLimits(f),
			Formats:     fieldFormats(f),
			Description: f.Description,
		})
	}
	return out
}

// gallerySections renders a template's declared sections, in declaration order.
// SectionDecl carries no free-text description, so the description cell is
// left empty rather than invented.
func gallerySections(t *template.Template) []gallerySection {
	out := make([]gallerySection, 0, len(t.Sections))
	for i := range t.Sections {
		d := &t.Sections[i]
		out = append(out, gallerySection{
			Name:        d.Name,
			Accepted:    strings.Join(d.Accepted, ", "),
			Repeats:     sectionRepeats(d),
			Description: "",
		})
	}
	return out
}

// fieldDefault renders a field's default in its display form, or "" when the
// field has no default. A boolean false default is therefore "false", distinct
// from no default.
func fieldDefault(f *template.Field) string {
	if f.Default == nil {
		return ""
	}
	return fmt.Sprint(f.Default)
}

// fieldLimits renders the rules that apply to a field's type as one human
// string: text length and plainness, number bounds, date bounds, list item
// bounds, or the enum variants. It returns "" when no rule applies.
func fieldLimits(f *template.Field) string {
	var parts []string
	switch f.Type {
	case template.FieldText:
		if f.MaxLength > 0 {
			parts = append(parts, fmt.Sprintf("max %d chars", f.MaxLength))
		}
		if f.Plain {
			parts = append(parts, "plain")
		}
	case template.FieldNumber:
		parts = append(parts, numberBounds(f.Min, f.Max)...)
	case template.FieldDate:
		parts = append(parts, dateBounds(f.MinDate, f.MaxDate)...)
	case template.FieldList:
		parts = append(parts, listBounds(f.MinItems, f.MaxItems)...)
	case template.FieldEnum:
		if len(f.Variants) > 0 {
			parts = append(parts, "one of "+strings.Join(f.Variants, ", "))
		}
	}
	return strings.Join(parts, ", ")
}

// numberBounds renders inclusive number bounds: "≥ n", "≤ n" or "n–m".
func numberBounds(min, max *float64) []string {
	switch {
	case min != nil && max != nil:
		return []string{boundText(*min) + "–" + boundText(*max)}
	case min != nil:
		return []string{"≥ " + boundText(*min)}
	case max != nil:
		return []string{"≤ " + boundText(*max)}
	default:
		return nil
	}
}

// dateBounds renders inclusive date bounds: "from YYYY-MM-DD", "to YYYY-MM-DD"
// or "YYYY-MM-DD–YYYY-MM-DD".
func dateBounds(min, max string) []string {
	switch {
	case min != "" && max != "":
		return []string{min + "–" + max}
	case min != "":
		return []string{"from " + min}
	case max != "":
		return []string{"to " + max}
	default:
		return nil
	}
}

// listBounds renders list item bounds: "n–m items", "≥ n items" or
// "≤ m items". A zero minimum and a non-positive maximum mean unbounded.
func listBounds(min, max int) []string {
	switch {
	case min > 0 && max > 0:
		return []string{strconv.Itoa(min) + "–" + strconv.Itoa(max) + " items"}
	case min > 0:
		return []string{"≥ " + strconv.Itoa(min) + " items"}
	case max > 0:
		return []string{"≤ " + strconv.Itoa(max) + " items"}
	default:
		return nil
	}
}

// fieldFormats renders a field's named formats and its default: for example
// "compact, exact, percent (default compact)". It returns "" when the field
// takes no format.
func fieldFormats(f *template.Field) string {
	if len(f.Formats) == 0 && f.DefaultFormat == "" {
		return ""
	}
	out := strings.Join(f.Formats, ", ")
	if f.DefaultFormat != "" {
		if out != "" {
			out += " (default " + f.DefaultFormat + ")"
		} else {
			out = "default " + f.DefaultFormat
		}
	}
	return out
}

// sectionRepeats renders a section's repeat bounds: "2–4", "≥ 1" or "≤ 4". It
// returns "" when the section is unrestricted, which the page renders as "any".
func sectionRepeats(d *template.SectionDecl) string {
	switch {
	case d.Min > 0 && d.Max > 0:
		return strconv.Itoa(d.Min) + "–" + strconv.Itoa(d.Max)
	case d.Min > 0:
		return "≥ " + strconv.Itoa(d.Min)
	case d.Max > 0:
		return "≤ " + strconv.Itoa(d.Max)
	default:
		return ""
	}
}

// bodySummary is the one-line plain-language sentence for an implied body rule.
func bodySummary(b template.BodyRule) string {
	switch b.Mode {
	case template.BodyRequired:
		return "A body is required."
	case template.BodyDisallowed:
		return "This template takes no body."
	default:
		return "A body is optional."
	}
}

// bodyLimits renders the bounds applied to a body: "max 2 paragraphs",
// "max 120 words", "max 4 list items" and, when the rule allows a body but no
// subheadings, "no subheadings". It returns "" when there are none.
func bodyLimits(b template.BodyRule) string {
	var parts []string
	if b.MaxWords > 0 {
		parts = append(parts, fmt.Sprintf("max %d words", b.MaxWords))
	}
	if b.MaxParagraphs > 0 {
		parts = append(parts, fmt.Sprintf("max %d paragraphs", b.MaxParagraphs))
	}
	if b.MaxListItems > 0 {
		parts = append(parts, fmt.Sprintf("max %d list items", b.MaxListItems))
	}
	if b.Mode != template.BodyDisallowed && !b.Subheadings {
		parts = append(parts, "no subheadings")
	}
	return strings.Join(parts, ", ")
}

// boundText renders a numeric bound without a trailing ".0": 0 becomes "0".
func boundText(n float64) string {
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}
