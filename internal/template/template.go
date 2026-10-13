// Package template defines the compiled-in slide and section templates kalide
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
	// template named Field.SectionTemplate.
	FieldSectionTemplate FieldType = "section-template"

	// FieldRecord is an inline record: a list's `item:` descriptor declaring
	// `type: record` together with its own `fields:` (record-item-type). A
	// record is never a field's `type:` — it appears only as a list's item
	// descriptor — and its fields use exactly the same descriptors as a
	// template's top-level fields, validated recursively. They live in
	// Field.Fields.
	FieldRecord FieldType = "record"
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

	// Nullable accepts an explicit YAML `null` for this field — or, on a
	// record item descriptor, a null list entry — whatever its Type
	// (nullable-fields). A null value runs no bound, format or membership
	// check. Null is distinct from absent: a `required` nullable field's key
	// must be present (it may be null), and an explicit null never triggers
	// Default.
	Nullable bool

	// Plain, for a text field, strips inline Markdown styles silently
	// instead of interpreting them (field-types `plain` flag).
	Plain bool

	// Coerce, for a text field, accepts a YAML number or boolean in place of
	// a string and converts it to its string form (text-coercion). A value
	// that is neither a string nor, under Coerce, a number or boolean stays
	// a type error.
	Coerce bool

	// Variants, for an enum field, is the closed set of accepted values.
	// Exactly one of them may be the Default, and an enum field's value is
	// how a layout variant is selected (template-variants).
	Variants []string

	// VariantsFrom, for an enum field, is instead of an inline Variants set
	// a glob relative to the root of the library layer that owns the
	// template, whose matching files lie under that layer's media/ tree
	// (e.g. `media/bgs/*.jpg`). The glob is expanded when the layer loads;
	// the field's valid values are the matching files, each written
	// relative to that layer's media/ tree (e.g. `bgs/right-8.jpg`), in
	// sorted order. Declaring Variants and VariantsFrom together on one
	// field, or a VariantsFrom glob matching no file, is a templates error
	// at load (enum-variants-from). An empty VariantsFrom means no
	// expansion.
	VariantsFrom string

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

	// Roots, for an image field, names the root set the field's path points
	// at, in written order: "assets", the deck's assets/ tree, or "media",
	// the media/ tree of the library layer that owns the field's template
	// (image-field-roots). A path is resolved against the roots in their
	// written order and must name an existing file in a declared root; with
	// no Roots it resolves under the deck's assets/.
	Roots []string

	// Dir, for an image field, optionally restricts the accepted path to a
	// subdirectory within a root. An empty Dir means no subdirectory
	// restriction (image-field-roots).
	Dir string

	// Extensions, for an image field, optionally restricts the accepted
	// path to a set of allowed file-name extensions. An empty Extensions
	// means no extension restriction (image-field-roots).
	Extensions []string

	// Item, for a list field, is the single item type every element must
	// have (field-types: list of exactly one item type). Its Name is
	// irrelevant; its Type (and, for a section-template item, its
	// SectionTemplate) describes one element. It is nil for non-list fields.
	Item *Field

	// Fields, for a list item descriptor whose Type is FieldRecord, is the
	// inline record's field schema: the same descriptors as a template's
	// top-level fields — name, type, rules and description — validated
	// recursively, so a record's field may itself be a list whose item is a
	// record (record-item-type). Each may be Nullable. It is nil for every
	// other field.
	Fields []Field

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

	// MaxItemsFrom and MinItemsFrom, for a list field, name another list
	// field whose item count bounds this list: MaxItemsFrom bounds this list
	// to at most the named list's count, MinItemsFrom to at least it
	// (cross-field-list-constraints). A leading `/name` names a top-level
	// field of the template; a bare name names a sibling field within the
	// same inline record (record-item-type). An empty string means no
	// cross-field bound.
	MaxItemsFrom string
	MinItemsFrom string

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

	// Description is the one-line help shown by `kalide templates` and the
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

	// MaxDepth, for a section declaration that accepts its own template
	// (directly or through a cycle), is the positive-integer bound an
	// author writes as `max_depth: N` (bounded-self-nesting). It bounds how
	// many times this declaration's section may recur in one containment
	// chain: an instance it produces has at most N enclosing instances that
	// resolve to the same template it does, so its `.item.depth` is at most
	// N. It belongs to the declaration that carries it; a declaration
	// without it is unbounded — 0 means no bound. A value that is not a
	// positive integer is a templates error naming the offending
	// `template.yaml`.
	MaxDepth int
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

	// Tables is the template's optional GFM-table rule, declared as the
	// `body.tables` mapping in its `template.yaml` (table-opt-in). A non-nil
	// Tables opts the template in: every body it owns may contain GFM tables,
	// bounded by the rule's limits and cell attributes. A nil Tables means the
	// template has not opted in, so a body's table is refused.
	Tables *TableRule
}

// TableRule is a template's optional GFM-table rule, declared as the
// `body.tables` mapping in its `template.yaml` (table-opt-in). A template that
// declares it accepts GFM tables in every body it owns; a template without it
// has no TableRule and refuses a body's table. Its limits bound each table in
// every body the template owns, independently (table-limits).
//
// The zero value of a `min_*` field is no minimum. The zero value of a `max_*`
// field is unbounded, except MaxTables: its declared default is 1, which the
// manifest parser writes when `max_tables` is absent, while an explicit
// `max_tables: 0` means unbounded.
type TableRule struct {
	// MinTables and MaxTables count the tables in one body. MinTables 0 is
	// no minimum; MaxTables 0 is unbounded and defaults to 1 when
	// `max_tables` is absent.
	MinTables int
	MaxTables int

	// MinColumns and MaxColumns bound one table's column count. MinColumns
	// 0 is no minimum; MaxColumns 0 is unbounded.
	MinColumns int
	MaxColumns int

	// MinRows and MaxRows bound one table's body rows, the header
	// excluded. MinRows 0 is no minimum; MaxRows 0 is unbounded.
	MinRows int
	MaxRows int

	// CellAttributes are the attributes a cell may set in this template's
	// tables, declared by the `cell_attributes` list of `body.tables`
	// (table-cell-attributes). Each is decoded and type-checked at load; an
	// ambiguous declaration set is refused at load by
	// checkCellAttributeAmbiguity. Empty when the template declares none.
	CellAttributes []CellAttribute
}

// CellAttribute is one cell attribute a template declares for its GFM tables,
// an entry of the `body.tables.cell_attributes` list (table-cell-attributes).
// Its Name is what an author writes in a cell's `{...}` attribute group or as
// a bare-name short-hand; its Kind is the attribute's value type, one of the
// two field types FieldBoolean and FieldEnum, with Variants the closed set of
// an enum's accepted values (domain.value.cell-attribute). A declared
// attribute is type-checked at load like a field of its Kind and appears,
// typed by this declaration, in a cell's `attrs` map (table-layout-context).
//
// The name `br` is reserved and is never a declared cell attribute. A declared
// name that is both a boolean attribute and an enum variant, or a variant of
// more than one declared enum, is refused at template load by
// checkCellAttributeAmbiguity.
type CellAttribute struct {
	// Name is the attribute name an author writes, e.g. `bold` or
	// `highlight`. The reserved name `br` is never a declared attribute.
	Name string

	// Kind is the attribute's value type: FieldBoolean or FieldEnum. Any
	// other field type is a templates error at load.
	Kind FieldType

	// Variants, for an enum attribute, is the closed set of accepted
	// values. A bare name that is a variant of exactly one declared enum
	// attribute sets that attribute by mutex (table-cell-attributes). It is
	// empty for a boolean attribute.
	Variants []string
}

// Layout is a template's Go html/template layout: its name (used in error
// messages and template lookup), its optional on-disk display path and its
// source text. Loading and parsing the layout are not done here.
type Layout struct {
	// Name identifies the layout. It is the name the layout is parsed and
	// executed under: for an embedded template a name such as
	// "title.html.tmpl", and for a library layout its layer-relative
	// forward-slash path within its layer, e.g.
	// "slides/hello/layout.html.tmpl" — the name an author writes in a
	// `{{ template "<name>" . }}` action.
	Name string

	// Path is the layout's on-disk path, e.g.
	// "templates/slides/hello/layout.html.tmpl". It is display-only: it
	// carries the position an error or other human-readable output reports.
	// It is never a namespace key and never used for template lookup — Name
	// is. It is empty for a generated layout with no on-disk file (for
	// example the gallery wrapper), in which case an error position falls
	// back to Name.
	Path string

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

	// Deferred marks a template whose example is validated end to end
	// elsewhere (checkLibraryExamples, against example.md through the full
	// render context) rather than through Registry.Validate's schema-only
	// structured-data check. A library-loaded template's manifest never
	// carries hand-authored Frontmatter/Sections, so Registry.checkExample
	// has nothing to check and skips it when Deferred is set.
	Deferred bool
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

	// Description is a one-line summary shown by `kalide templates` and the
	// gallery.
	Description string

	// Usage says whether this template describes a whole slide or a section
	// inside one. Exactly one usage applies.
	Usage Usage

	// Internal marks a template hidden from the default listing: a template
	// whose manifest declares `internal: true` is omitted from
	// `kalide templates` unless --all is passed (internal-templates). An
	// internal template stays resolvable as a `section`-helper target and as
	// a `section_template` field type, but is refused as a Markdown
	// `template:` target.
	Internal bool

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
