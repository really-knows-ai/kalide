// Package template defines the compiled-in slide and section templates eypres
// renders, together with the schema engine that validates deck content against
// them.
//
// This file holds the template DATA MODEL only: the shape of a template
// definition (its name, usage, field/section/body schema, html/template layout
// and validating example slide) and the supporting field, section, body and
// example types. Nothing here checks values, formats numbers or dates,
// composes or nests sections, registers templates or defines the built-ins;
// those are separate units in this package:
//
//   - CheckValues — the schema value-checking engine;
//   - Format      — number/date display formats;
//   - Section     — composition, variants and controlled effects;
//   - Registry    — registration and build-time checks;
//   - Builtins    — the compiled-in template definitions.
//
// They all consume the types declared here.
//
// Dependency direction: built-in templates load their layouts and example
// Markdown from internal/assets, so internal/template imports internal/assets
// and never internal/slide. The slide parser resolves template names through
// the read-only slide.Catalogue contract, which *template.Registry satisfies
// structurally; the compile-time conformance assertion lives in
// internal/validate (phase 5) so this package carries no dependency on
// internal/slide.
//
// Every name that an author will write and that can be invalid is modelled as
// a defined string type (Usage, FieldType, BodyMode) so an invalid value is
// representable now and reported by the build-time checks later.
package template

// Usage names how a template may be used: as a whole slide or as a section
// inside one (template-definition requires exactly one usage per template).
//
// Usage is a defined string so an invalid value is representable; the registry
// build-time checks and the slide parser reject anything that is not
// UsageSlide or UsageSection.
type Usage string

const (
	// UsageSlide marks a template that describes a whole slide: a slide
	// file's frontmatter `template:` must name one of these.
	UsageSlide Usage = "slide"

	// UsageSection marks a template that describes a section inside a slide
	// or inside another section template. Section declarations accept only
	// these.
	UsageSection Usage = "section"
)

// FieldType names the value type of a template field (field-types).
//
// FieldType is a defined string so an invalid value is representable; the
// schema checker reports it. A section template used as a field's type is
// FieldSectionTemplate together with Field.SectionTemplate naming the
// template, and the same form is used for a list's single item type.
type FieldType string

const (
	// FieldText is inline Markdown following the body inline rules. Its
	// `plain` flag is Field.Plain.
	FieldText FieldType = "text"

	// FieldNumber is a plain YAML number; a quoted/string value is a type
	// error. Its inclusive bounds are Field.Min and Field.Max.
	FieldNumber FieldType = "number"

	// FieldDate is a YYYY-MM-DD date; any other form is an error.
	FieldDate FieldType = "date"

	// FieldBoolean is a YAML boolean.
	FieldBoolean FieldType = "boolean"

	// FieldEnum is one of Field.Variants.
	FieldEnum FieldType = "enum"

	// FieldImage is a path under the deck's assets/ directory; existence is
	// checked by the schema engine.
	FieldImage FieldType = "image"

	// FieldLink is a `#label` anchor or an http(s) URL; the same rules as a
	// body link.
	FieldLink FieldType = "link"

	// FieldList is a YAML sequence whose every element has the single item
	// type Field.Item (field-types: list of exactly one item type).
	FieldList FieldType = "list"

	// FieldSectionTemplate is structured YAML data shaped by the section
	// template named Field.SectionTemplate (no separate record types).
	FieldSectionTemplate FieldType = "section-template"
)

// Field is one declared template field: the key Name an author writes, its
// value Type, and the rules the schema checker applies to it.
//
// Which of the rule fields below are meaningful depends on Type; the schema
// checker ignores rules that do not apply to a field's type.
type Field struct {
	// Name is the YAML key an author writes for this field. Reserved names
	// (`body`, the `_format` suffix, `notes`) are rejected at build time,
	// not here.
	Name string

	// Type selects the value type and which rules below apply.
	Type FieldType

	// Plain, for a text field, strips inline Markdown styles silently
	// instead of interpreting them (field-types `plain` flag).
	Plain bool

	// Variants, for an enum field, is the closed set of accepted values.
	// Exactly one of them may be the Default, and an enum field's value is
	// how a layout variant is selected (template-variants).
	Variants []string

	// Formats, for a number or date field, is the closed set of named display
	// formats the author may select with this field's reserved
	// `<field>_format` sibling key. For example a number field may declare
	// compact, exact and percent. An empty Formats means the field takes no
	// format selection. The names and their functions are defined by Format.
	Formats []string

	// DefaultFormat, for a number or date field, is the format used when the
	// author writes no `<field>_format` key. It must be one of Formats (or a
	// built-in name when Formats is empty); an empty DefaultFormat means an
	// omitted selection leaves the value unformatted.
	DefaultFormat string

	// SectionTemplate, for a FieldSectionTemplate field, names the
	// section-usage template whose fields describe this field's structured
	// YAML value.
	SectionTemplate string

	// Item, for a list field, is the single item type every element must
	// have (field-types: list of exactly one item type). Its Name is
	// irrelevant; its Type (and, for a section-template item, its
	// SectionTemplate) describes one element. It is nil for non-list fields.
	Item *Field

	// Required says the field must be present in the frontmatter.
	Required bool

	// Default is the value used when the field is omitted: a Go value of the
	// field's type (a string, a number, a bool, or an enum variant). A nil
	// Default means there is no default.
	Default any

	// MaxLength, for a text field, is the maximum number of characters after
	// Markdown is stripped. 0 means no limit.
	MaxLength int

	// MinItems and MaxItems, for a list field, bound the number of items.
	// MinItems 0 means no minimum; MaxItems <= 0 means no maximum (matching
	// the slide parser's unbounded-repeat convention).
	MinItems int
	MaxItems int

	// Min and Max, for a number field, are inclusive NUMBER bounds. A nil
	// bound is absent, so a legitimate 0 is distinct from "unset". They do
	// not apply to a date field; use MinDate and MaxDate for that.
	Min *float64
	Max *float64

	// MinDate and MaxDate, for a date field, are inclusive bounds written as
	// YYYY-MM-DD. An empty string is absent, mirroring Formats and
	// DefaultFormat.
	MinDate string
	MaxDate string

	// Description is the one-line help shown by `eypres templates` and the
	// gallery.
	Description string
}

// SectionDecl is one named section a template declares: the section-template
// names it accepts and how many times it may appear (template-composition).
//
// It is the per-section schema entry the composition unit (Section) and the
// registry consume. A section instance's `template:` is required iff
// len(Accepted) > 1; with exactly one accepted template the instance resolves
// to it automatically.
type SectionDecl struct {
	// Name is the section name an author writes as `# name`.
	Name string

	// Accepted lists the section-usage template names this section may
	// resolve to. It never contains a slide-usage template.
	Accepted []string

	// Min is the fewest instances required. 0 means no minimum.
	Min int

	// Max is the most instances allowed. <= 0 means unbounded, matching the
	// slide parser's repeat-limit convention.
	Max int
}

// BodyMode says whether a template's implied `body` field is required,
// optional or disallowed (body-rules).
//
// BodyMode is a defined string so an invalid value is representable; the
// build-time checks reject anything that is not one of the constants below.
type BodyMode string

const (
	// BodyRequired requires a non-empty body.
	BodyRequired BodyMode = "required"

	// BodyOptional allows a body or none.
	BodyOptional BodyMode = "optional"

	// BodyDisallowed rejects any body; the template takes data only.
	BodyDisallowed BodyMode = "disallowed"
)

// BodyRule is the rule for a template's implied `body` field (body-rules).
//
// Body content comes only from the Markdown after the frontmatter; a `body:`
// key in YAML is an author error with guidance, not a way to set it. A section
// template used as a field type takes YAML data only, so its body is
// BodyDisallowed.
type BodyRule struct {
	// Mode is required, optional or disallowed.
	Mode BodyMode

	// MaxWords, MaxParagraphs and MaxListItems bound the body's size as
	// measured by the Markdown validator. 0 means no limit.
	MaxWords      int
	MaxParagraphs int
	MaxListItems  int

	// Subheadings says whether `##`/`###` subheadings are allowed in the
	// body.
	Subheadings bool
}

// Layout is a template's Go html/template layout: its name (used in error
// messages and template lookup) and its source text. Loading and parsing the
// layout are not done here.
type Layout struct {
	// Name identifies the layout, e.g. "title" or "title.html.tmpl". It is
	// the name the layout is parsed and executed under.
	Name string

	// Text is the layout's html/template source, exactly as loaded from the
	// embedded assets.
	Text string
}

// Example carries a template's example slide both ways:
//
//   - Markdown is the slide's source, validated end-to-end through the full
//     parser pipeline (slide parser + Markdown validator + schema engine) in
//     phase 5; and
//   - Frontmatter and Sections are the same slide as structured data
//     (frontmatter/field values plus declared section instances with their
//     YAML data), which the phase-4 build-time checks validate without
//     parsing any Markdown.
//
// The two representations describe the same slide; phase 5 is what proves it
// end to end.
type Example struct {
	// Markdown is the example slide's full source.
	Markdown string

	// Frontmatter holds the slide's field values, keyed by field name.
	Frontmatter map[string]any

	// Sections holds the slide's declared section instances in source order,
	// excluding the reserved notes section.
	Sections []ExampleSection
}

// ExampleSection is one declared section instance of an Example: the section
// name, the section template it resolves to, and the YAML data written in its
// frontmatter fence.
type ExampleSection struct {
	// Name is the declared section name (`# name`).
	Name string

	// Template is the section-usage template the instance resolves to.
	Template string

	// Frontmatter holds the section's YAML frontmatter values, keyed by the
	// section template's field names. It is nil when the section has no
	// frontmatter.
	Frontmatter map[string]any

	// Body is the section's Markdown body. Phase-4 checks treat it as the
	// structured example's body text; Markdown parsing happens in phase 5.
	Body string
}

// Template is the compiled-in definition of one slide or section template
// (template-definition): its identity and usage, the field/section/body schema
// an author's content is checked against, its html/template layout, and a
// validating example slide.
//
// Templates are compiled into the binary and are never editable by deck
// authors.
type Template struct {
	// Name is the unique template name an author writes in a slide's
	// `template:` or a section's `template:` key.
	Name string

	// Description is a one-line summary shown by `eypres templates` and the
	// gallery.
	Description string

	// Usage says whether this template describes a whole slide or a section
	// inside one. Exactly one usage applies.
	Usage Usage

	// Fields is the template's declared field schema, in declaration order.
	Fields []Field

	// Sections is the named sections the template declares, in declaration
	// order. A section template may declare sections of its own
	// (composition), nested with no depth limit.
	Sections []SectionDecl

	// Body is the rule for the implied body field.
	Body BodyRule

	// Layout is the template's Go html/template layout.
	Layout Layout

	// Example is the template's example slide, carried as both source
	// Markdown and structured data.
	Example Example
}
