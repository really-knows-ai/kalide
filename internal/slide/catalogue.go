// Package slide parses slide files and owns the template-catalogue contract the
// parser resolves names against.
//
// Catalogue is a narrow, read-only view of the compiled-in template registry.
// The registry lives in internal/template and does not import this package: it
// satisfies Catalogue structurally (directly, or through an adapter), so this
// package depends on the contract alone and never on the registry.
package slide

// Catalogue is the read-only view of the slide/section template catalogue that
// slide parsing consults.
type Catalogue interface {
	// LookupSlideTemplate reports how the named template may be used, whether
	// it is internal, and whether it exists at all. usage is "slide" when the
	// template describes a whole slide and "section" when it describes a
	// section inside one. internal reports the named template's Internal flag
	// (the manifest's `internal: true`) and is false when no template has that
	// name. ok is false when no template has that name.
	//
	// An internal template is refused as a Markdown `template:` target — a
	// slide frontmatter `template:` or a section instance's `template:` — while
	// its name stays in TemplateNames so a closest-match suggestion can still
	// offer it. This surface does not affect the `section`-helper or
	// `section_template` resolutions: those consult the registry's Lookup
	// directly, not LookupSlideTemplate, and so still resolve an internal
	// template.
	LookupSlideTemplate(name string) (usage string, internal bool, ok bool)

	// TemplateNames returns every template name, sorted, so callers can pass
	// them to a closest-match "did you mean …?" helper.
	TemplateNames() []string

	// SectionNames returns the section names declared by the slide template
	// tmpl. It never includes the reserved name "notes": notes are reserved for
	// the parser's speaker-notes section and are not declarable as a section
	// template.
	SectionNames(tmpl string) []string

	// SectionDecl returns the declaration for section section of slide template
	// tmpl: the section templates it accepts and the min and max number of
	// times the section may repeat. ok is false when the slide template does
	// not declare that section.
	//
	// A template: key is required on a section instance iff len(accepted) > 1:
	// with exactly one accepted template the instance resolves to it, and with
	// several the author must choose one.
	SectionDecl(tmpl, section string) (accepted []string, min, max int, ok bool)
}
