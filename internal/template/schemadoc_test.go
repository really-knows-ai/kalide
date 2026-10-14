package template

import "testing"

// This file is the unit-test deliverable for plan.phase-05.task-7: the shared
// template schema-documentation vocabulary in schemadoc.go.
//
// It pins the exact rendered strings both surfaces (`kalide templates <name>`
// as indented text and the /templates gallery as HTML tables) build from, so a
// drift in the shared vocabulary cannot silently desynchronise the two:
//
//   - FieldLimits renders a field's type limits (text length/plainness,
//     inclusive number/date/list bounds) with numeric bounds never carrying a
//     trailing ".0", and returns "" for an enum field (each surface prints the
//     variants in its own form) and for every limit-free type.
//   - FieldFormats renders a field's named formats with its default.
//   - SectionRepeats renders a section's repeat bounds and "" when the section
//     is unrestricted (Min<=0 && Max<=0); each surface renders that "" in its
//     own form ("any").
//   - BodyLimits renders a body rule's limits in word/paragraph/list-item order
//     followed by "no subheadings" when a body is allowed but subheadings are
//     not.
//
// Everything is in-process over zero-valued structs: no library, filesystem or
// watcher is touched, so the tests run under -short.

// TestFieldLimits pins FieldLimits' exact output per field type. Numeric bounds
// are checked to render without a trailing ".0" for integral values, and an
// enum field (or any other limit-free type) is checked to return "".
func TestFieldLimits(t *testing.T) {
	tests := []struct {
		name  string
		field *Field
		want  string
	}{
		{
			name:  "text with no rules has no limits",
			field: &Field{Type: FieldText},
			want:  "",
		},
		{
			name:  "text max length only",
			field: &Field{Type: FieldText, MaxLength: 40},
			want:  "max 40 chars",
		},
		{
			name:  "text plain only",
			field: &Field{Type: FieldText, Plain: true},
			want:  "plain",
		},
		{
			name:  "text max length and plain join in order",
			field: &Field{Type: FieldText, MaxLength: 40, Plain: true},
			want:  "max 40 chars, plain",
		},
		{
			name:  "number with no bounds has no limits",
			field: &Field{Type: FieldNumber},
			want:  "",
		},
		{
			name:  "number min only",
			field: &Field{Type: FieldNumber, Min: float64Ptr(1)},
			want:  "≥ 1",
		},
		{
			name:  "number max only",
			field: &Field{Type: FieldNumber, Max: float64Ptr(10)},
			want:  "≤ 10",
		},
		{
			name:  "number both bounds omit the trailing .0",
			field: &Field{Type: FieldNumber, Min: float64Ptr(1), Max: float64Ptr(10)},
			want:  "1–10",
		},
		{
			name:  "number integral bound rendered from a float omits .0",
			field: &Field{Type: FieldNumber, Min: float64Ptr(3.0)},
			want:  "≥ 3",
		},
		{
			name:  "number zero bound is a legitimate bound",
			field: &Field{Type: FieldNumber, Min: float64Ptr(0)},
			want:  "≥ 0",
		},
		{
			name:  "number fractional bounds keep their fraction",
			field: &Field{Type: FieldNumber, Min: float64Ptr(0.5), Max: float64Ptr(2.5)},
			want:  "0.5–2.5",
		},
		{
			name:  "date with no bounds has no limits",
			field: &Field{Type: FieldDate},
			want:  "",
		},
		{
			name:  "date min only",
			field: &Field{Type: FieldDate, MinDate: "2024-01-01"},
			want:  "from 2024-01-01",
		},
		{
			name:  "date max only",
			field: &Field{Type: FieldDate, MaxDate: "2024-12-31"},
			want:  "to 2024-12-31",
		},
		{
			name:  "date both bounds",
			field: &Field{Type: FieldDate, MinDate: "2024-01-01", MaxDate: "2024-12-31"},
			want:  "2024-01-01–2024-12-31",
		},
		{
			name:  "list with no bounds has no limits",
			field: &Field{Type: FieldList},
			want:  "",
		},
		{
			name:  "list min only",
			field: &Field{Type: FieldList, MinItems: 2},
			want:  "≥ 2 items",
		},
		{
			name:  "list max only",
			field: &Field{Type: FieldList, MaxItems: 5},
			want:  "≤ 5 items",
		},
		{
			name:  "list both bounds",
			field: &Field{Type: FieldList, MinItems: 2, MaxItems: 5},
			want:  "2–5 items",
		},
		{
			name:  "list non-positive max means unbounded",
			field: &Field{Type: FieldList, MinItems: 0, MaxItems: -1},
			want:  "",
		},
		{
			name:  "enum returns empty even with variants",
			field: &Field{Type: FieldEnum, Variants: []string{"a", "b"}},
			want:  "",
		},
		{
			name:  "enum returns empty ignoring unrelated limit fields",
			field: &Field{Type: FieldEnum, MaxLength: 40, Plain: true},
			want:  "",
		},
		{
			name:  "boolean has no limits",
			field: &Field{Type: FieldBoolean},
			want:  "",
		},
		{
			name:  "section-template has no limits",
			field: &Field{Type: FieldSectionTemplate, SectionTemplate: "card"},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FieldLimits(tt.field); got != tt.want {
				t.Errorf("FieldLimits(%+v) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

// TestFieldFormats pins FieldFormats' exact output for named formats, a
// default-only field and a field that takes no format.
func TestFieldFormats(t *testing.T) {
	tests := []struct {
		name  string
		field *Field
		want  string
	}{
		{
			name:  "no names and no default",
			field: &Field{Type: FieldNumber},
			want:  "",
		},
		{
			name:  "names only",
			field: &Field{Type: FieldNumber, Formats: []string{"compact", "exact", "percent"}},
			want:  "compact, exact, percent",
		},
		{
			name:  "default only without names",
			field: &Field{Type: FieldNumber, DefaultFormat: "compact"},
			want:  "default compact",
		},
		{
			name:  "names with a default",
			field: &Field{Type: FieldNumber, Formats: []string{"compact", "exact", "percent"}, DefaultFormat: "compact"},
			want:  "compact, exact, percent (default compact)",
		},
		{
			name:  "default among a single name",
			field: &Field{Type: FieldDate, Formats: []string{"long"}, DefaultFormat: "long"},
			want:  "long (default long)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FieldFormats(tt.field); got != tt.want {
				t.Errorf("FieldFormats(%+v) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

// TestSectionRepeats pins SectionRepeats' bounded forms and its "" result for
// an unrestricted section (Min<=0 && Max<=0), which each surface renders as
// "any".
func TestSectionRepeats(t *testing.T) {
	tests := []struct {
		name string
		decl *SectionDecl
		want string
	}{
		{
			name: "both bounds",
			decl: &SectionDecl{Min: 2, Max: 4},
			want: "2–4",
		},
		{
			name: "min only",
			decl: &SectionDecl{Min: 2},
			want: "≥ 2",
		},
		{
			name: "max only",
			decl: &SectionDecl{Max: 4},
			want: "≤ 4",
		},
		{
			name: "unrestricted is empty",
			decl: &SectionDecl{},
			want: "",
		},
		{
			name: "non-positive max is unrestricted",
			decl: &SectionDecl{Min: 0, Max: -1},
			want: "",
		},
		{
			name: "negative min and max are unrestricted",
			decl: &SectionDecl{Min: -1, Max: -1},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SectionRepeats(tt.decl); got != tt.want {
				t.Errorf("SectionRepeats(%+v) = %q, want %q", tt.decl, got, tt.want)
			}
		})
	}
}

// TestBodyLimits pins BodyLimits' exact output: the word/paragraph/list-item
// limits in that order, followed by "no subheadings" only when a body is
// allowed (not BodyDisallowed) and subheadings are not.
func TestBodyLimits(t *testing.T) {
	tests := []struct {
		name string
		body BodyRule
		want string
	}{
		{
			name: "all size limits in word, paragraph, list-item order",
			body: BodyRule{Mode: BodyDisallowed, MaxWords: 120, MaxParagraphs: 2, MaxListItems: 4, Subheadings: true},
			want: "max 120 words, max 2 paragraphs, max 4 list items",
		},
		{
			name: "no subheadings when a body is optional and subheadings are off",
			body: BodyRule{Mode: BodyOptional, Subheadings: false},
			want: "no subheadings",
		},
		{
			name: "no subheadings when a body is required and subheadings are off",
			body: BodyRule{Mode: BodyRequired, Subheadings: false},
			want: "no subheadings",
		},
		{
			name: "size limits precede no subheadings",
			body: BodyRule{Mode: BodyRequired, MaxWords: 120, Subheadings: false},
			want: "max 120 words, no subheadings",
		},
		{
			name: "subheadings allowed adds nothing",
			body: BodyRule{Mode: BodyOptional, Subheadings: true},
			want: "",
		},
		{
			name: "disallowed body adds no subheadings clause",
			body: BodyRule{Mode: BodyDisallowed, MaxWords: 5},
			want: "max 5 words",
		},
		{
			name: "disallowed and empty renders nothing",
			body: BodyRule{Mode: BodyDisallowed},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BodyLimits(tt.body); got != tt.want {
				t.Errorf("BodyLimits(%+v) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}
