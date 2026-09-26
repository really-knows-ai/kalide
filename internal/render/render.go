// Package render turns a parsed, validated slide into the reveal.js markup the
// deck page embeds.
//
// A slide is rendered by executing its template's Go html/template layout with
// the author's validated field and section data, and then:
//
//   - number and date fields are formatted through internal/template's format
//     catalogue (the field's `<field>_format` sibling, or its default_format);
//   - text fields are rendered as inline Markdown HTML (or passed as plain,
//     html/template-escaped text when the field is `plain`);
//   - the slide body and every section body are rendered as the accepted
//     Markdown subset to block HTML;
//   - the slide's anchor id (its deck label) is emitted on the layout's root
//     <section>;
//   - a `# notes` section is emitted as a reveal.js
//     `<aside class="notes">…</aside>` inside the slide, where the embedded
//     notes plugin reads it.
//
// Controlled effects — fragments, transitions and backgrounds — are never
// invented here: they live in the compiled-in template layouts, exactly as the
// author-facing schema forbids an author from setting them.
//
// RenderSlide consumes the PARSED, VALIDATED model (internal/slide.Slide plus
// the template registry that produced it); it never re-parses Markdown or
// re-checks a value. The deck page itself (the .reveal > .slides document) is
// RenderDeck's job, in its own unit.
//
// html/template auto-escaping stays on: the only values inserted without
// re-escaping are the ones goldmark produced from Markdown (which escapes its
// text and, by the markdown subset, can contain no raw HTML) and the trusted
// label, attribute-escaped explicitly.
package render

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	htmltmpl "html/template"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
)

// RenderError reports one slide that could not be rendered: its file identity
// (when known) and the underlying cause. It is a programming/definition failure
// rather than a deck-author error, because RenderSlide is only ever handed
// content that has already passed whole-deck validation.
type RenderError struct {
	// File is the slide's identity, as carried by slide.Slide.File. It is
	// empty for a failure that has no slide (for example a nil registry).
	File string

	// Err is the underlying cause.
	Err error
}

// Error renders the positioned failure.
func (e *RenderError) Error() string {
	if e.File == "" {
		return "render: " + e.Err.Error()
	}
	return e.File + ": " + e.Err.Error()
}

// Unwrap returns the underlying cause.
func (e *RenderError) Unwrap() error { return e.Err }

// RenderSlide renders one parsed, validated slide as a single reveal.js
// <section> fragment, ready to be placed directly inside the deck page's
// `.reveal > .slides` container.
//
// parsed is the slide's parsed structure (internal/slide.Parse); its Template
// must name a slide-usage template. label is the slide's deck label (the text
// after the dash in its filename, deck.Slide.Label), which becomes the anchor id
// on the root <section>; unique-slide-labels guarantees it is unique across the
// deck. reg is the populated template registry the slide was validated against
// — typically built from the project's templates/ library
// (template.NewRegistryFromLibrary). funcMap is the library's layout func map
// (template.LayoutFuncMap, `media` bound to the served templates/media URL base,
// plus the number/date format functions); a nil funcMap falls back to
// template.LayoutFuncMap(nil, ""), under which `media` always fails as if no
// templates/media directory existed.
//
// The layout is executed with a map[string]any keyed by field name, as the
// template layouts document: text fields carry rendered inline HTML (plain
// fields carry escaped text), number and date fields carry their formatted
// display string, `body` carries the rendered Markdown body HTML, and each
// declared section name carries a slice of instance maps in source order. The
// format functions and `media` are exposed to the layout through funcMap, so a
// library layout may format a value or resolve a media URL itself
// (template-media, template-language).
//
// When parsed has a `# notes` section, RenderSlide appends an
// `<aside class="notes">` inside the slide with its rendered body; the embedded
// reveal.js notes plugin reads it. When it has none, no aside is emitted.
//
// A definition failure (unknown or non-slide template, missing layout, an
// unresolvable format) is returned as a *RenderError. RenderSlide is
// deterministic: the same slide always renders to the same fragment.
func RenderSlide(parsed *slide.Slide, label string, reg *template.Registry, funcMap htmltmpl.FuncMap) (htmltmpl.HTML, error) {
	if parsed == nil {
		return "", &RenderError{Err: errors.New("nil slide")}
	}
	if reg == nil {
		return "", &RenderError{File: parsed.File, Err: errors.New("nil template registry")}
	}

	slideTmpl, ok := reg.Lookup(parsed.Template)
	if !ok || slideTmpl == nil {
		return "", &RenderError{File: parsed.File, Err: fmt.Errorf("unknown slide template %q", parsed.Template)}
	}
	if slideTmpl.Usage != template.UsageSlide {
		return "", &RenderError{File: parsed.File, Err: fmt.Errorf("template %q is a %s template, not a slide template", parsed.Template, slideTmpl.Usage)}
	}

	r := &renderer{reg: reg, formats: template.BuiltinFormats}

	data, err := r.slideData(parsed, slideTmpl)
	if err != nil {
		return "", &RenderError{File: parsed.File, Err: err}
	}

	if funcMap == nil {
		funcMap = template.LayoutFuncMap(nil, "")
	}
	root, err := parseLayouts(slideTmpl, reg, funcMap)
	if err != nil {
		return "", &RenderError{File: parsed.File, Err: err}
	}

	var buf bytes.Buffer
	if err := root.ExecuteTemplate(&buf, layoutName(slideTmpl), data); err != nil {
		return "", &RenderError{File: parsed.File, Err: fmt.Errorf("execute layout: %w", err)}
	}

	out := addAnchor(buf.String(), label)
	if parsed.Notes != nil {
		notes, err := renderBody(parsed.Notes.Body)
		if err != nil {
			return "", &RenderError{File: parsed.File, Err: err}
		}
		out = addNotes(out, `<aside class="notes">`+string(notes)+`</aside>`)
	}
	return htmltmpl.HTML(out), nil
}

// renderer carries the two collaborators every field conversion needs: the
// template registry (to resolve a section-template field's nested schema) and
// the number/date format catalogue.
type renderer struct {
	reg     *template.Registry
	formats *template.Format
}

// slideData builds the layout execution context for one slide: its converted
// field values, its rendered body, and its sections grouped by declared name.
func (r *renderer) slideData(s *slide.Slide, tmpl *template.Template) (map[string]any, error) {
	data, err := r.values(s.Frontmatter, tmpl)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.Body) != "" {
		body, err := renderBody(s.Body)
		if err != nil {
			return nil, err
		}
		data["body"] = body
	}

	// Section instances are grouped by their declared name, preserving source
	// order, so a layout ranges over them in the order the author wrote them.
	groups := make(map[string][]map[string]any)
	for i := range s.Sections {
		sec := &s.Sections[i]
		if sec.Template == "" {
			continue
		}
		secTmpl, ok := r.reg.Lookup(sec.Template)
		if !ok || secTmpl == nil {
			continue
		}
		inst, err := r.values(sec.Frontmatter, secTmpl)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(sec.Body) != "" {
			body, err := renderBody(sec.Body)
			if err != nil {
				return nil, err
			}
			inst["body"] = body
		}
		groups[sec.Name] = append(groups[sec.Name], inst)
	}
	for name, instances := range groups {
		data[name] = instances
	}
	return data, nil
}

// values converts one map of validated field data against t's field schema,
// returning the layout-ready map. It is used for a slide's frontmatter, a
// section instance's frontmatter, and a nested section-template-as-type value,
// so the same rules apply at every depth. The reserved `template:` selector and
// any unknown key are simply absent from the field schema and are ignored here;
// validation already rejected an unknown key.
func (r *renderer) values(data map[string]any, t *template.Template) (map[string]any, error) {
	out := make(map[string]any, len(t.Fields))
	for i := range t.Fields {
		f := &t.Fields[i]
		raw, present := data[f.Name]
		if !present {
			if f.Default == nil {
				continue
			}
			raw = f.Default
		}
		v, err := r.fieldValue(f, raw, data)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		out[f.Name] = v
	}
	return out, nil
}

// fieldValue converts one validated field value to its layout representation.
// data is the mapping the value came from, so a number or date field can find
// its `<field>_format` sibling; it may be nil for a list element, which carries
// no selector of its own.
func (r *renderer) fieldValue(f *template.Field, raw any, data map[string]any) (any, error) {
	switch f.Type {
	case template.FieldText:
		s, ok := raw.(string)
		if !ok {
			// A non-string value (already rejected by validation) is left as
			// text for html/template to escape.
			return raw, nil
		}
		if f.Plain {
			// A plain field strips Markdown styles silently and is rendered as
			// escaped text, so "**bold**" reads "bold" rather than showing its
			// markers.
			return plainText(s), nil
		}
		return renderInline(s), nil

	case template.FieldNumber, template.FieldDate:
		return r.formatted(f, raw, data)

	case template.FieldList:
		items, ok := asSlice(raw)
		if !ok || f.Item == nil {
			return raw, nil
		}
		out := make([]any, len(items))
		for i, elem := range items {
			v, err := r.fieldValue(f.Item, elem, nil)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil

	case template.FieldSectionTemplate:
		nested, ok := r.reg.Lookup(f.SectionTemplate)
		if !ok || nested == nil {
			return raw, nil
		}
		m, ok := asMap(raw)
		if !ok {
			return raw, nil
		}
		return r.values(m, nested)

	default:
		// boolean, enum, link and image values render as written.
		return raw, nil
	}
}

// formatted renders a number or date field through the format selected by its
// `<field>_format` sibling, or the field's default_format, using the same
// catalogue the schema checker validated against. With no effective format a
// number is rendered without a trailing ".0" and a date is normalised to
// YYYY-MM-DD, so a layout never prints a raw time.Time.
func (r *renderer) formatted(f *template.Field, raw any, data map[string]any) (any, error) {
	chosen := ""
	if data != nil {
		chosen, _ = data[f.Name+"_format"].(string)
	}
	fn, err := r.formats.Resolve(f, chosen)
	if err != nil {
		return nil, err
	}
	if fn == nil {
		switch f.Type {
		case template.FieldDate:
			return dateString(raw), nil
		case template.FieldNumber:
			return numberString(raw), nil
		}
		return raw, nil
	}
	return fn(raw)
}

// numberString renders a number value without a trailing ".0" for whole
// values; a non-numeric value (already rejected by validation) is passed
// through.
func numberString(raw any) any {
	switch v := raw.(type) {
	case float64:
		return formatNumber(v)
	case float32:
		return formatNumber(float64(v))
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return raw
	}
}

// formatNumber renders a whole float without a decimal point.
func formatNumber(n float64) string {
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// dateString normalises a date value to YYYY-MM-DD when no named format
// applies. Validation accepts a time.Time or a YYYY-MM-DD string.
func dateString(raw any) string {
	switch v := raw.(type) {
	case time.Time:
		return v.Format("2006-01-02")
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// parseLayouts parses the slide template and every layout reachable from it —
// declared section templates and section-template-as-type fields, at every
// depth — into one html/template namespace keyed by layout name, so a composed
// layout can invoke another with `{{ template "<name>" . }}`. funcMap is the
// library's layout func map (template.LayoutFuncMap): `media` plus the
// number/date format functions, installed on the namespace root so every
// library layout using `media` or a format function parses and executes
// (template-media, template-language).
func parseLayouts(slideTmpl *template.Template, reg *template.Registry, funcMap htmltmpl.FuncMap) (*htmltmpl.Template, error) {
	// The namespace root carries its own name only so html/template has a
	// handle; it deliberately differs from every layout name. Naming the root
	// after the slide's own layout would make root.New(layoutName(slideTmpl))
	// shadow that layout with an empty associated template, so executing the
	// slide layout would fail with "is an incomplete template".
	root := htmltmpl.New("layouts").Funcs(funcMap)
	for _, t := range reachableTemplates(slideTmpl, reg) {
		if t.Layout.Text == "" {
			return nil, fmt.Errorf("template %q has no layout text", t.Name)
		}
		if _, err := root.New(layoutName(t)).Parse(t.Layout.Text); err != nil {
			return nil, fmt.Errorf("template %q: parse layout: %w", t.Name, err)
		}
	}
	return root, nil
}

// reachableTemplates returns tmpl followed by every template reachable from it
// through declared sections and section-template-as-type fields, each once, in
// declaration order. Missing names are skipped: validation has already rejected
// an undefined name, and a layout simply cannot invoke a template that does not
// exist.
func reachableTemplates(root *template.Template, reg *template.Registry) []*template.Template {
	seen := make(map[string]bool)
	var out []*template.Template

	var add func(t *template.Template)
	add = func(t *template.Template) {
		if t == nil || seen[t.Name] {
			return
		}
		seen[t.Name] = true
		out = append(out, t)
		for i := range t.Sections {
			for _, name := range t.Sections[i].Accepted {
				if st, ok := reg.Lookup(name); ok {
					add(st)
				}
			}
		}
		for i := range t.Fields {
			addFieldTemplate(&t.Fields[i], reg, add)
		}
	}
	add(root)
	return out
}

// addFieldTemplate follows a field's section-template reference, recursing
// through a list's single item type.
func addFieldTemplate(f *template.Field, reg *template.Registry, add func(*template.Template)) {
	switch f.Type {
	case template.FieldSectionTemplate:
		if st, ok := reg.Lookup(f.SectionTemplate); ok {
			add(st)
		}
	case template.FieldList:
		if f.Item != nil {
			addFieldTemplate(f.Item, reg, add)
		}
	}
}

// layoutName is the name a template's layout is parsed and executed under,
// falling back to the template name when the content loader did not set one.
func layoutName(t *template.Template) string {
	if t.Layout.Name != "" {
		return t.Layout.Name
	}
	return t.Name
}

// inlineParagraphTags are the paragraph open/close tags goldmark emits around
// an inline-mode text field. mdcheck validates a text field as exactly one
// paragraph, so removing that wrapper — and only that wrapper — yields the
// field's inline HTML.
const (
	inlineOpenTag  = "<p>"
	inlineCloseTag = "</p>\n"
)

// renderInline renders an inline-Markdown text field to HTML without the
// paragraph wrapper goldmark adds around a block. Text fields are validated in
// mdcheck's inline mode, so the single paragraph is the parser's wrapper, not
// author content. goldmark escapes text and code spans, and the accepted subset
// admits no raw HTML, so the result is safe to inject unescaped.
func renderInline(src string) htmltmpl.HTML {
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := goldmark.New().Convert([]byte(src), &buf); err != nil {
		// Convert only fails when the destination writer fails; a bytes.Buffer
		// never does, so this is unreachable and the raw source is a safe
		// fallback for html/template to escape.
		return htmltmpl.HTML(src)
	}
	out := buf.String()
	if strings.HasPrefix(out, inlineOpenTag) && strings.HasSuffix(out, inlineCloseTag) {
		out = out[len(inlineOpenTag) : len(out)-len(inlineCloseTag)]
	}
	return htmltmpl.HTML(out)
}

// plainText returns an inline-Markdown field's visible text with all styles
// removed, for a `plain` field: "**bold**" becomes "bold". goldmark's Text and
// String nodes carry the visible text while every style wrapper (emphasis,
// code span, link) contributes only its markers, which are omitted here.
// html/template escapes the result on output.
func plainText(src string) string {
	if src == "" {
		return ""
	}
	source := []byte(src)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	var b strings.Builder
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		writeNodeText(&b, n, source)
	}
	return b.String()
}

// writeNodeText appends the visible text of n and its descendants. goldmark's
// Text nodes carry source segments, String and CodeSpan nodes carry literal
// text. Every other node is a style wrapper whose markers are intentionally
// omitted.
func writeNodeText(b *strings.Builder, n ast.Node, src []byte) {
	switch t := n.(type) {
	case *ast.Text:
		b.Write(t.Segment.Value(src))
		if t.SoftLineBreak() || t.HardLineBreak() {
			b.WriteByte(' ')
		}
	case *ast.String:
		b.Write(t.Value)
	case *ast.CodeSpan:
		// An inline code span stores its text directly and is treated as a
		// leaf, so its text is not written twice.
		b.Write(t.Text(src))
		return
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		writeNodeText(b, c, src)
	}
}

// renderBody renders a body — a slide body, a section body or the notes body —
// as the accepted Markdown subset to block HTML, using goldmark's CommonMark
// core with no extensions, exactly as internal/mdcheck validated it.
func renderBody(src string) (htmltmpl.HTML, error) {
	if strings.TrimSpace(src) == "" {
		return "", nil
	}
	var buf bytes.Buffer
	if err := goldmark.New().Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return htmltmpl.HTML(buf.String()), nil
}

// addAnchor emits the slide's anchor id on the layout's root <section>, which
// is the reveal.js slide element. The label is attribute-escaped even though
// the deck filename grammar only admits [A-Za-z0-9_-]. When a layout emits no
// <section> at all, the fragment is wrapped in one, so the slide still has a
// stable anchor.
func addAnchor(fragment, label string) string {
	if label == "" {
		return fragment
	}
	attr := ` id="` + html.EscapeString(label) + `"`
	i := strings.Index(fragment, "<section")
	if i < 0 || !tagBoundary(fragment, i+len("<section")) {
		return "<section" + attr + ">" + fragment + "</section>"
	}
	j := i + len("<section")
	return fragment[:j] + attr + fragment[j:]
}

// addNotes inserts the speaker-notes aside inside the slide element, before its
// closing </section>, so the reveal.js notes plugin finds it as a child of the
// current slide. With no closing </section> the aside is appended.
func addNotes(fragment, notes string) string {
	if notes == "" {
		return fragment
	}
	i := strings.LastIndex(fragment, "</section>")
	if i < 0 {
		return fragment + notes
	}
	return fragment[:i] + notes + fragment[i:]
}

// tagBoundary reports whether a "<section" occurrence is a real tag opening,
// rather than the prefix of some other name such as "<sectional>".
func tagBoundary(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	switch s[i] {
	case ' ', '\t', '\n', '\r', '>', '/':
		return true
	}
	return false
}

// asSlice returns v as a []any for any slice or array kind, so YAML-decoded
// sequences render whatever their element type.
func asSlice(v any) ([]any, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// asMap returns v as a map[string]any, accepting the string-keyed and
// interface-keyed maps a YAML decoder may produce.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	}
	return nil, false
}
