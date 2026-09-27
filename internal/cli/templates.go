package cli

// This file implements `kalide templates` (phase-7 task 7):
// requirements.requirement.cli-templates-list and
// requirements.requirement.cli-templates-show.
//
//   - `kalide templates` lists every template in the project's resolved
//     template library (the configured `templates:` path, or the local
//     templates/ directory when the key is absent): its name, its usage
//     (slide|section) and its one-line description.
//   - `kalide templates <name>` documents one template: its fields (type,
//     required, default, limits, formats, description), its sections (accepted
//     templates and min/max repeats), its implied body rules and its example
//     slide, exactly as loaded from the resolved library.
//
// The catalogue is the project's resolved template library
// (template.LoadDeckLibrary + template.NewRegistryFromLibrary), the same
// loaded registry the validator and renderer use, so the documentation cannot
// drift from the behaviour. An unresolvable library is reported the same way
// `kalide start` reports it, before any listing is attempted
// (templates-dir-required, no-built-in-fallback). An unknown name is an author
// error: it is reported with the closest-match "did you mean …?" suggestion
// from internal/suggest, matching the parser's and theme registry's error
// style.
//
// runTemplates is called from Run with the arguments after the `templates`
// command word (Run rejects more than one name before dispatching here). All
// output goes to the supplied writers, so the command is testable without a
// real terminal.

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/suggest"
	"github.com/really-knows-ai/kalide/internal/template"
)

// runTemplates implements `kalide templates [name]`.
//
// With no arguments it lists every template in the project's resolved
// template library. With exactly one argument it shows that template's full
// documentation. It returns the process status code: 0 on success; 1 when
// the project's template library cannot be resolved, when the name is
// unknown (the error names the available templates and suggests the
// closest), or when the loaded library cannot be built into a registry,
// which is a programming error rather than an author error.
func runTemplates(args []string, stdout, stderr io.Writer) int {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "kalide templates: %v\n", err)
		return 1
	}

	// Resolve the deck's template library through the loader's single
	// root-resolution point (external-template-library): a configured
	// `templates:` path wins and is authoritative, else the local templates/
	// directory is used. The resolved root may sit outside the deck, so the
	// library is loaded from the operating-system path, not through the deck
	// fs.FS. A missing or unresolvable library is a templates error naming the
	// path, with no fallback (templates-dir-required).
	//
	// The raw `templates:` value is read directly here because deck.LoadConfig
	// needs a theme registry, which comes from the not-yet-resolved library;
	// this mirrors the reload pipeline. An unreadable config yields "" and the
	// loader then looks for the local templates/ directory.
	deckFS := os.DirFS(root)
	configured := ""
	if data, readErr := fs.ReadFile(deckFS, deck.ConfigFile); readErr == nil {
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) == nil {
			configured, _ = raw["templates"].(string)
		}
	}

	library, err := template.LoadDeckLibrary(root, configured)
	if err != nil {
		fmt.Fprintf(stderr, "kalide templates: %v\n", err)
		return 1
	}

	registry, err := template.NewRegistryFromLibrary(library)
	if err != nil {
		fmt.Fprintf(stderr, "kalide templates: %v\n", err)
		return 1
	}

	if len(args) == 0 {
		printTemplateList(stdout, registry)
		return 0
	}

	name := args[0]
	t, ok := registry.Lookup(name)
	if !ok {
		fmt.Fprintln(stderr, templateUnknownMessage(name, registry.TemplateNames()))
		return 1
	}
	printTemplateDetails(stdout, t)
	return 0
}

// templateUnknownMessage builds the author-facing message for an unknown
// template name: the offending name, the closest-match "did you mean …?"
// suggestion when internal/suggest finds one, and the sorted list of available
// templates. It is a plain string so the caller chooses the writer.
func templateUnknownMessage(name string, names []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "kalide templates: unknown template %q", name)
	if s := suggest.Closest(name, names); s != "" {
		fmt.Fprintf(&b, ": did you mean %q?", s)
	}
	if len(names) > 0 {
		fmt.Fprintf(&b, " (available templates: %s)", strings.Join(names, ", "))
	}
	return b.String()
}

// printTemplateList writes the one-line-per-template list: name, usage and
// description, in the registry's sorted name order. The name and usage columns
// are padded so the descriptions line up.
func printTemplateList(w io.Writer, registry *template.Registry) {
	templates := registry.Templates()

	nameWidth := 0
	for _, t := range templates {
		if t != nil && len(t.Name) > nameWidth {
			nameWidth = len(t.Name)
		}
	}

	fmt.Fprintln(w, "Templates:")
	fmt.Fprintln(w)
	for _, t := range templates {
		if t == nil {
			continue
		}
		fmt.Fprintf(w, "  %-*s  %-7s  %s\n", nameWidth, t.Name, string(t.Usage), t.Description)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `kalide templates <name>` for a template's fields, sections and example.")
}

// printTemplateDetails writes one template's full documentation: its identity,
// its field schema, its declared sections, its implied body rule and its
// example slide. Every section is printed even when empty, so the shape of the
// output is stable.
func printTemplateDetails(w io.Writer, t *template.Template) {
	fmt.Fprintf(w, "%s (%s)\n", t.Name, string(t.Usage))
	if t.Description != "" {
		fmt.Fprintln(w, t.Description)
	}
	fmt.Fprintln(w)
	printTemplateFields(w, t)
	printTemplateSections(w, t)
	printTemplateBody(w, t)
	printTemplateExample(w, t)
}

// printTemplateFields writes the field schema, one field per line, in
// declaration order.
func printTemplateFields(w io.Writer, t *template.Template) {
	fmt.Fprintln(w, "Fields:")
	if len(t.Fields) == 0 {
		fmt.Fprintln(w, "  none")
		fmt.Fprintln(w)
		return
	}
	for i := range t.Fields {
		f := &t.Fields[i]
		line := "  " + f.Name + " (" + templateFieldAttributes(f) + ")"
		if f.Description != "" {
			line += " — " + f.Description
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
}

// templateFieldAttributes renders a field's documentation attributes: its type,
// whether it is required or optional, its default when it has one, the limits
// that apply to its type and its named formats. They are joined with "; ".
func templateFieldAttributes(f *template.Field) string {
	parts := []string{"type: " + string(f.Type)}
	if f.Required {
		parts = append(parts, "required")
	} else {
		parts = append(parts, "optional")
	}
	if f.Default != nil {
		parts = append(parts, "default: "+fmt.Sprint(f.Default))
	}
	if limits := templateFieldLimits(f); limits != "" {
		parts = append(parts, limits)
	}
	if formats := templateFieldFormats(f); formats != "" {
		parts = append(parts, "formats: "+formats)
	}
	return strings.Join(parts, "; ")
}

// templateFieldLimits renders the limits that apply to a field's type: text
// length and plainness, number bounds, date bounds, list item bounds, or the
// closed set of enum variants. It returns "" when no limit applies.
func templateFieldLimits(f *template.Field) string {
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
		parts = append(parts, templateNumberBounds(f.Min, f.Max)...)
	case template.FieldDate:
		parts = append(parts, templateDateBounds(f.MinDate, f.MaxDate)...)
	case template.FieldList:
		parts = append(parts, templateListBounds(f.MinItems, f.MaxItems)...)
	case template.FieldEnum:
		if len(f.Variants) > 0 {
			parts = append(parts, "one of "+strings.Join(f.Variants, ", "))
		}
	}
	return strings.Join(parts, ", ")
}

// templateFieldFormats renders a field's named formats and its default format:
// for example "compact, exact, percent (default compact)". It returns "" when
// the field takes no format.
func templateFieldFormats(f *template.Field) string {
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

// templateNumberBounds renders inclusive number bounds: "≥ n", "≤ n" or "n–m".
func templateNumberBounds(min, max *float64) []string {
	switch {
	case min != nil && max != nil:
		return []string{templateBoundText(*min) + "–" + templateBoundText(*max)}
	case min != nil:
		return []string{"≥ " + templateBoundText(*min)}
	case max != nil:
		return []string{"≤ " + templateBoundText(*max)}
	default:
		return nil
	}
}

// templateDateBounds renders inclusive date bounds: "from YYYY-MM-DD",
// "to YYYY-MM-DD" or "YYYY-MM-DD–YYYY-MM-DD".
func templateDateBounds(min, max string) []string {
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

// templateListBounds renders list item bounds: "n–m items", "≥ n items" or
// "≤ m items". A zero minimum and a non-positive maximum mean unbounded.
func templateListBounds(min, max int) []string {
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

// templateBoundText renders a numeric bound without a trailing ".0": 0 becomes
// "0".
func templateBoundText(n float64) string {
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// printTemplateSections writes the declared sections, one per line, in
// declaration order: the section name, the section-usage templates it accepts
// and its repeat bounds.
func printTemplateSections(w io.Writer, t *template.Template) {
	fmt.Fprintln(w, "Sections:")
	if len(t.Sections) == 0 {
		fmt.Fprintln(w, "  none")
		fmt.Fprintln(w)
		return
	}
	for i := range t.Sections {
		d := &t.Sections[i]
		accepted := "any"
		if len(d.Accepted) > 0 {
			accepted = strings.Join(d.Accepted, ", ")
		}
		fmt.Fprintf(w, "  %s (accepts %s; repeats %s)\n", d.Name, accepted, templateSectionRepeats(d))
	}
	fmt.Fprintln(w)
}

// templateSectionRepeats renders a section's repeat bounds: "2–4", "≥ 2",
// "≤ 4" or "any" when the section is unrestricted.
func templateSectionRepeats(d *template.SectionDecl) string {
	switch {
	case d.Min > 0 && d.Max > 0:
		return strconv.Itoa(d.Min) + "–" + strconv.Itoa(d.Max)
	case d.Min > 0:
		return "≥ " + strconv.Itoa(d.Min)
	case d.Max > 0:
		return "≤ " + strconv.Itoa(d.Max)
	default:
		return "any"
	}
}

// printTemplateBody writes the implied body rule: its mode and, when any apply,
// its limits.
func printTemplateBody(w io.Writer, t *template.Template) {
	fmt.Fprintln(w, "Body:")
	fmt.Fprintf(w, "  mode: %s\n", string(t.Body.Mode))
	if limits := templateBodyLimits(t.Body); limits != "" {
		fmt.Fprintf(w, "  limits: %s\n", limits)
	}
	fmt.Fprintln(w)
}

// templateBodyLimits renders the bounds applied to a body: "max 2 paragraphs",
// "max 120 words", "max 4 list items" and, when the rule allows a body but no
// subheadings, "no subheadings". It returns "" when there are none.
func templateBodyLimits(b template.BodyRule) string {
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

// printTemplateExample writes the template's example slide verbatim, so an
// author can copy it. A section-usage template's example is a section fragment,
// not a whole slide, which is how it is shown.
func printTemplateExample(w io.Writer, t *template.Template) {
	fmt.Fprintln(w, "Example:")
	fmt.Fprintln(w)
	md := t.Example.Markdown
	if strings.TrimSpace(md) == "" {
		fmt.Fprintln(w, "  (no example)")
		return
	}
	fmt.Fprint(w, md)
	if !strings.HasSuffix(md, "\n") {
		fmt.Fprintln(w)
	}
}
