package template

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This file is the unit-test deliverable for phase-04.task-7: it exercises
// CheckValues, the schema value-checking engine, for every field type and rule,
// the text plain flag and Markdown-stripped max_length, the body `body:` key
// rejection, number/date `_format` selection, lists of section-template items,
// enum variants, controlled-effect key rejection, and the unknown-field
// closest-match suggestions — plus the composition repeat limits and nesting
// that phase 4's composition engine shares with the schema engine.
//
// The tests assert against the real API (CheckValues signature, the ValueError
// rule/path/value/what/fix shape, Result.Links) and against the real
// internal/mdcheck and internal/suggest behaviour, so a change in either layer
// that breaks an author-facing message is caught here.

// wantErr is the subset of ValueError a table case asserts. Path is compared
// through Value.PathString, and Value through reflect.DeepEqual so slices and
// maps compare structurally.
type wantErr struct {
	Rule  string
	Path  string
	Value any
	What  string
	Fix   string
}

// we is a constructor so the expected-error tables stay readable.
func we(rule, path string, value any, what, fix string) wantErr {
	return wantErr{Rule: rule, Path: path, Value: value, What: what, Fix: fix}
}

// newSlide builds a slide-usage template. CheckValues inspects only Name,
// Usage, Fields, Sections and Body, so a test template carries no layout or
// example content.
func newSlide(name string, fields ...Field) *Template {
	return &Template{Name: name, Usage: UsageSlide, Fields: fields}
}

// newSection builds a section-usage template.
func newSection(name string, fields ...Field) *Template {
	return &Template{Name: name, Usage: UsageSection, Fields: fields}
}

// resolverFor returns the section-template resolver over the given templates,
// mirroring *Registry.Lookup's signature.
func resolverFor(ts ...*Template) func(string) (*Template, bool) {
	byName := make(map[string]*Template, len(ts))
	for _, t := range ts {
		byName[t.Name] = t
	}
	return func(name string) (*Template, bool) {
		t, ok := byName[name]
		return t, ok
	}
}

// colTemplate is the shared nested section template used across the nesting
// cases: a title (max 10 characters) and a non-negative amount.
func colTemplate() *Template {
	zero := 0.0
	return newSection("col",
		Field{Name: "title", Type: FieldText, MaxLength: 10},
		Field{Name: "amount", Type: FieldNumber, Min: &zero},
	)
}

// TestCheckValues is the main table-driven suite: every field type's accept and
// reject path, the value rules, the body/effect reserved-key rejections, the
// `_format` selection and unknown-field suggestions. Each case asserts rule,
// path, the actual value, and the author-facing what/fix text.
func TestCheckValues(t *testing.T) {
	zero := 0.0
	col := colTemplate()
	colResolve := resolverFor(col)

	textTmpl := newSlide("text",
		Field{Name: "text", Type: FieldText, MaxLength: 10},
		Field{Name: "plain_text", Type: FieldText, Plain: true, MaxLength: 20},
		Field{Name: "short", Type: FieldText, MaxLength: 3},
	)
	linkTextTmpl := newSlide("linktext", Field{Name: "text", Type: FieldText})
	numTmpl := newSlide("num", Field{Name: "num", Type: FieldNumber, Min: &zero, Max: float64Ptr(100)})
	dateTmpl := newSlide("date", Field{Name: "date", Type: FieldDate})
	dateBoundsTmpl := newSlide("datebounds", Field{Name: "date", Type: FieldDate, MinDate: "2020-01-01", MaxDate: "2025-12-31"})
	boolTmpl := newSlide("bool", Field{Name: "flag", Type: FieldBoolean})
	enumTmpl := newSlide("enum", Field{Name: "mode", Type: FieldEnum, Variants: []string{"a", "b"}})
	imageTmpl := newSlide("image", Field{Name: "img", Type: FieldImage})
	linkTmpl := newSlide("link", Field{Name: "url", Type: FieldLink})
	listTmpl := newSlide("list",
		Field{Name: "items", Type: FieldList, Item: &Field{Type: FieldText}, MinItems: 1, MaxItems: 2},
		Field{Name: "nums", Type: FieldList, Item: &Field{Type: FieldNumber}},
	)
	reqTmpl := newSlide("req", Field{Name: "title", Type: FieldText, Required: true})
	sectionTmpl := newSlide("sec", Field{Name: "block", Type: FieldSectionTemplate, SectionTemplate: "col"})
	noSecTmpl := newSlide("nosec", Field{Name: "block", Type: FieldSectionTemplate})
	nilResTmpl := newSlide("nilres", Field{Name: "block", Type: FieldSectionTemplate, SectionTemplate: "col"})
	listSectionTmpl := newSlide("listsec", Field{Name: "colitems", Type: FieldList,
		Item: &Field{Type: FieldSectionTemplate, SectionTemplate: "col"}, MinItems: 1, MaxItems: 2})
	suggestTmpl := newSlide("sug",
		Field{Name: "title", Type: FieldText, MaxLength: 80},
		Field{Name: "metric", Type: FieldNumber, Formats: []string{"compact", "exact"}},
	)
	fmtTmpl := newSlide("fmt",
		Field{Name: "amount", Type: FieldNumber, Formats: []string{"compact", "exact", "percent"}, DefaultFormat: "compact"},
		Field{Name: "when", Type: FieldDate, Formats: []string{"long", "short"}, DefaultFormat: "long"},
		Field{Name: "plain", Type: FieldText},
	)
	bodyTmpl := newSlide("body", Field{Name: "title", Type: FieldText})
	bodyTmpl.Body = BodyRule{Mode: BodyRequired, MaxWords: 10}
	effectTmpl := newSlide("effect", Field{Name: "title", Type: FieldText})

	const boldAndCode = "**bold** and `code`"

	tests := []struct {
		name      string
		tmpl      *Template
		data      map[string]any
		resolve   func(string) (*Template, bool)
		opts      []Option
		want      []wantErr
		wantLinks []LinkRef
		wantLine  int
	}{
		// --- text ---------------------------------------------------------
		{
			name: "text valid",
			tmpl: textTmpl,
			data: map[string]any{"text": "hello"},
		},
		{
			name: "text wrong type",
			tmpl: textTmpl,
			data: map[string]any{"text": 42},
			want: []wantErr{we("type", "text", 42,
				"type: expected a text field, got a number", "quote the value as a string")},
		},
		{
			// mdcheck reports the inline issue at fragment line 1; CheckValues
			// keeps it (see the note on Line in the report) rather than 0.
			name: "text inline Markdown error",
			tmpl: textTmpl,
			data: map[string]any{"text": "~~strike~~"},
			want: []wantErr{we("disallowed-construct", "text", "~~strike~~",
				"strikethrough (~~text~~) is not supported", "remove the ~~ markers or use plain text")},
			wantLine: 1,
		},
		{
			// A link's destination is stripped before counting, so 28 raw
			// characters pass a 10-character limit. The link is still
			// collected for the phase-5 link pass.
			name: "text max_length counts after strip (passes)",
			tmpl: textTmpl,
			data: map[string]any{"text": "[link](https://example.com)"},
			wantLinks: []LinkRef{
				{Path: []PathSegment{{Name: "text"}}, Line: 1, Text: "link", Destination: "https://example.com"},
			},
		},
		{
			// The reported count is the stripped text ("bold and code" = 13),
			// not the raw 19 characters, proving the strip.
			name: "text max_length counts after strip (fails)",
			tmpl: textTmpl,
			data: map[string]any{"text": boldAndCode},
			want: []wantErr{we("max_length", "text", boldAndCode,
				"max_length: text is 13 characters, maximum is 10", "shorten the value")},
		},
		{
			// Rune count, not byte count: five Japanese characters.
			name: "text max_length counts runes",
			tmpl: textTmpl,
			data: map[string]any{"short": "日本語です"},
			want: []wantErr{we("max_length", "short", "日本語です",
				"max_length: text is 5 characters, maximum is 3", "shorten the value")},
		},
		{
			name: "plain text skips Markdown rules",
			tmpl: textTmpl,
			data: map[string]any{"plain_text": "~~strike~~"},
		},
		{
			name: "plain text still enforces max_length",
			tmpl: textTmpl,
			data: map[string]any{"plain_text": "~~strike~~ strike strike"},
			want: []wantErr{we("max_length", "plain_text", "~~strike~~ strike strike",
				"max_length: text is 24 characters, maximum is 20", "shorten the value")},
		},
		{
			name: "text collects inline links",
			tmpl: linkTextTmpl,
			data: map[string]any{"text": "see [here](#intro) and [site](https://example.com)"},
			wantLinks: []LinkRef{
				{Path: []PathSegment{{Name: "text"}}, Line: 1, Text: "here", Destination: "#intro", Label: "intro"},
				{Path: []PathSegment{{Name: "text"}}, Line: 1, Text: "site", Destination: "https://example.com"},
			},
		},
		{
			name: "text line source attaches line",
			tmpl: textTmpl,
			data: map[string]any{"text": 7},
			opts: []Option{WithLineSource(func([]PathSegment) int { return 12 })},
			want: []wantErr{we("type", "text", 7,
				"type: expected a text field, got a number", "quote the value as a string")},
			wantLine: 12,
		},

		// --- number -------------------------------------------------------
		{
			name: "number valid int",
			tmpl: numTmpl,
			data: map[string]any{"num": 42},
		},
		{
			name: "number valid float",
			tmpl: numTmpl,
			data: map[string]any{"num": 3.5},
		},
		{
			name: "number quoted value",
			tmpl: numTmpl,
			data: map[string]any{"num": "42"},
			want: []wantErr{we("type", "num", "42",
				`type: expected a number, got the quoted value "42"`, "write the number without quotes")},
		},
		{
			name: "number wrong type",
			tmpl: numTmpl,
			data: map[string]any{"num": true},
			want: []wantErr{we("type", "num", true,
				"type: expected a number, got a boolean", "write the number without quotes")},
		},
		{
			name: "number below min",
			tmpl: numTmpl,
			data: map[string]any{"num": -1},
			want: []wantErr{we("min", "num", -1,
				"min: -1 is below the minimum 0", "use a value of at least 0")},
		},
		{
			name: "number above max",
			tmpl: numTmpl,
			data: map[string]any{"num": 101},
			want: []wantErr{we("max", "num", 101,
				"max: 101 exceeds the maximum 100", "use a value of at most 100")},
		},

		// --- date ---------------------------------------------------------
		{
			name: "date valid string",
			tmpl: dateTmpl,
			data: map[string]any{"date": "2026-09-25"},
		},
		{
			name: "date valid time.Time",
			tmpl: dateTmpl,
			data: map[string]any{"date": time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)},
		},
		{
			name: "date bad format",
			tmpl: dateTmpl,
			data: map[string]any{"date": "25/09/2026"},
			want: []wantErr{we("date", "date", "25/09/2026",
				`date: "25/09/2026" is not a YYYY-MM-DD date`,
				"write the date as YYYY-MM-DD, for example 2024-01-02")},
		},
		{
			name: "date wrong type",
			tmpl: dateTmpl,
			data: map[string]any{"date": 20260925},
			want: []wantErr{we("type", "date", 20260925,
				"type: expected a YYYY-MM-DD date, got a number", "write the date as YYYY-MM-DD")},
		},
		{
			name: "date within bounds",
			tmpl: dateBoundsTmpl,
			data: map[string]any{"date": "2023-06-15"},
		},
		{
			name: "date time.Time within bounds",
			tmpl: dateBoundsTmpl,
			data: map[string]any{"date": time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)},
		},
		{
			name: "date below min",
			tmpl: dateBoundsTmpl,
			data: map[string]any{"date": "2019-12-31"},
			want: []wantErr{we("min", "date", "2019-12-31",
				"min: 2019-12-31 is below the minimum 2020-01-01", "use a date on or after 2020-01-01")},
		},
		{
			name: "date above max",
			tmpl: dateBoundsTmpl,
			data: map[string]any{"date": "2026-01-01"},
			want: []wantErr{we("max", "date", "2026-01-01",
				"max: 2026-01-01 exceeds the maximum 2025-12-31", "use a date on or before 2025-12-31")},
		},
		{
			name: "date equal to min passes (inclusive)",
			tmpl: dateBoundsTmpl,
			data: map[string]any{"date": "2020-01-01"},
		},
		{
			name: "date equal to max passes (inclusive)",
			tmpl: dateBoundsTmpl,
			data: map[string]any{"date": "2025-12-31"},
		},
		{
			// An unset MinDate/MaxDate leaves the date range unchecked.
			name: "date no bounds",
			tmpl: newSlide("nobounds", Field{Name: "date", Type: FieldDate, MinDate: "", MaxDate: ""}),
			data: map[string]any{"date": "1900-01-01"},
		},
		{
			// A malformed date still yields the `date` rule error, not a
			// range error, even with bounds set.
			name: "date malformed with bounds",
			tmpl: dateBoundsTmpl,
			data: map[string]any{"date": "2023/06/15"},
			want: []wantErr{we("date", "date", "2023/06/15",
				`date: "2023/06/15" is not a YYYY-MM-DD date`,
				"write the date as YYYY-MM-DD, for example 2024-01-02")},
		},
		{
			// A non-string non-date still yields the `type` rule error, not a
			// range error, even with bounds set.
			name: "date wrong type with bounds",
			tmpl: dateBoundsTmpl,
			data: map[string]any{"date": 20230615},
			want: []wantErr{we("type", "date", 20230615,
				"type: expected a YYYY-MM-DD date, got a number", "write the date as YYYY-MM-DD")},
		},

		// --- boolean ------------------------------------------------------
		{
			name: "boolean valid",
			tmpl: boolTmpl,
			data: map[string]any{"flag": true},
		},
		{
			name: "boolean wrong type",
			tmpl: boolTmpl,
			data: map[string]any{"flag": "true"},
			want: []wantErr{we("type", "flag", "true",
				"type: expected a boolean, got a quoted string", "write true or false without quotes")},
		},

		// --- enum ---------------------------------------------------------
		{
			name: "enum valid variant",
			tmpl: enumTmpl,
			data: map[string]any{"mode": "a"},
		},
		{
			name: "enum invalid variant",
			tmpl: enumTmpl,
			data: map[string]any{"mode": "c"},
			want: []wantErr{we("enum", "mode", "c",
				`enum: "c" is not one of the allowed variants (a, b)`, "use one of: a, b")},
		},
		{
			name: "enum wrong type",
			tmpl: enumTmpl,
			data: map[string]any{"mode": 3},
			want: []wantErr{we("type", "mode", 3,
				"type: expected one of the enum variants, got a number", "use one of: a, b")},
		},

		// --- image --------------------------------------------------------
		{
			name: "image valid path",
			tmpl: imageTmpl,
			data: map[string]any{"img": "assets/pic.png"},
		},
		{
			name: "image not under assets",
			tmpl: imageTmpl,
			data: map[string]any{"img": "pic.png"},
			want: []wantErr{we("image", "img", "pic.png",
				`image: "pic.png" is not under assets/`,
				"move the image into the deck's assets/ directory and reference it as assets/…")},
		},
		{
			name: "image exists via callback",
			tmpl: imageTmpl,
			data: map[string]any{"img": "assets/ok.png"},
			opts: []Option{WithImageExists(func(p string) bool { return p == "assets/ok.png" })},
		},
		{
			name: "image missing via callback",
			tmpl: imageTmpl,
			data: map[string]any{"img": "assets/missing.png"},
			opts: []Option{WithImageExists(func(p string) bool { return p == "assets/ok.png" })},
			want: []wantErr{we("image", "img", "assets/missing.png",
				`image: "assets/missing.png" does not exist`, "add the file under assets/ or fix the path")},
		},
		{
			name: "image wrong type",
			tmpl: imageTmpl,
			data: map[string]any{"img": 5},
			want: []wantErr{we("type", "img", 5,
				"type: expected an image path, got a number", "write the path as a string")},
		},

		// --- link ---------------------------------------------------------
		{
			name: "link valid label",
			tmpl: linkTmpl,
			data: map[string]any{"url": "#summary"},
			wantLinks: []LinkRef{
				{Path: []PathSegment{{Name: "url"}}, Destination: "#summary", Label: "summary"},
			},
		},
		{
			name: "link valid http",
			tmpl: linkTmpl,
			data: map[string]any{"url": "https://example.com"},
		},
		{
			name: "link bad scheme",
			tmpl: linkTmpl,
			data: map[string]any{"url": "mailto:x@example.com"},
			want: []wantErr{we("link", "url", "mailto:x@example.com",
				`link: "mailto:x@example.com" is neither a #label anchor nor an http(s) URL`,
				"use a #label anchor or an http(s) URL")},
		},
		{
			name: "link empty label",
			tmpl: linkTmpl,
			data: map[string]any{"url": "#"},
			want: []wantErr{we("link", "url", "#",
				`link: "#" has an empty label`, "use a non-empty #label, for example #summary")},
		},
		{
			name: "link wrong type",
			tmpl: linkTmpl,
			data: map[string]any{"url": 5},
			want: []wantErr{we("type", "url", 5,
				"type: expected a link, got a number", "write the link as a string")},
		},

		// --- list ---------------------------------------------------------
		{
			name: "list valid",
			tmpl: listTmpl,
			data: map[string]any{"items": []any{"a", "b"}},
		},
		{
			name: "list wrong type",
			tmpl: listTmpl,
			data: map[string]any{"items": "a"},
			want: []wantErr{we("type", "items", "a",
				"type: expected a list, got a quoted string", "write the value as a YAML sequence")},
		},
		{
			name: "list below min_items",
			tmpl: listTmpl,
			data: map[string]any{"items": []any{}},
			want: []wantErr{we("min_items", "items", []any{},
				"min_items: 0 item(s) is below the minimum 1", "add at least 1 item(s)")},
		},
		{
			name: "list above max_items",
			tmpl: listTmpl,
			data: map[string]any{"items": []any{"a", "b", "c"}},
			want: []wantErr{we("max_items", "items", []any{"a", "b", "c"},
				"max_items: 3 item(s) exceeds the maximum 2", "remove items until at most 2 remain")},
		},
		{
			name: "list item type mismatch",
			tmpl: listTmpl,
			data: map[string]any{"nums": []any{1, "two"}},
			want: []wantErr{we("type", "nums › [1]", "two",
				`type: expected a number, got the quoted value "two"`, "write the number without quotes")},
		},

		// --- required -----------------------------------------------------
		{
			name: "required present",
			tmpl: reqTmpl,
			data: map[string]any{"title": "x"},
		},
		{
			name: "required missing",
			tmpl: reqTmpl,
			data: map[string]any{},
			want: []wantErr{we("required", "title", nil,
				`required: field "title" is required but missing`, "add a title: value")},
		},

		// --- section-template-as-type -------------------------------------
		{
			name:    "section-template valid",
			tmpl:    sectionTmpl,
			resolve: colResolve,
			data:    map[string]any{"block": map[string]any{"title": "T", "amount": 1}},
		},
		{
			name:    "section-template nested field error",
			tmpl:    sectionTmpl,
			resolve: colResolve,
			data:    map[string]any{"block": map[string]any{"amount": "2"}},
			want: []wantErr{we("type", "block › amount", "2",
				`type: expected a number, got the quoted value "2"`, "write the number without quotes")},
		},
		{
			name:    "section-template nested unknown field suggests nested field",
			tmpl:    sectionTmpl,
			resolve: colResolve,
			data:    map[string]any{"block": map[string]any{"titel": "x"}},
			want: []wantErr{we("unknown-field", "block › titel", "x",
				`unknown field "titel"`, `did you mean "title"?`)},
		},
		{
			name:    "section-template value not a mapping",
			tmpl:    sectionTmpl,
			resolve: colResolve,
			data:    map[string]any{"block": "x"},
			want: []wantErr{we("type", "block", "x",
				`type: expected a mapping for section template "col", got a quoted string`,
				"write the section data as YAML key: value pairs")},
		},
		{
			name:    "section-template unresolvable",
			tmpl:    sectionTmpl,
			resolve: func(string) (*Template, bool) { return nil, false },
			data:    map[string]any{"block": map[string]any{}},
			want: []wantErr{we("section-template", "block", map[string]any{},
				`section-template: unknown section template "col"`,
				"check the template name against the built-in templates")},
		},
		{
			name: "section-template field declares no template",
			tmpl: noSecTmpl,
			data: map[string]any{"block": map[string]any{}},
			want: []wantErr{we("section-template", "block", map[string]any{},
				"section-template: field declares no section template", "fix the template definition")},
		},
		{
			name: "section-template without resolver",
			tmpl: nilResTmpl,
			data: map[string]any{"block": map[string]any{}},
			want: []wantErr{we("section-template", "block", map[string]any{},
				`section-template: cannot resolve "col"`, "pass a section template resolver to CheckValues")},
		},

		// --- list of a section-template item type -------------------------
		{
			name:    "list of section-template valid",
			tmpl:    listSectionTmpl,
			resolve: colResolve,
			data: map[string]any{"colitems": []any{
				map[string]any{"title": "A"},
				map[string]any{"title": "B"},
			}},
		},
		{
			name:    "list of section-template item error",
			tmpl:    listSectionTmpl,
			resolve: colResolve,
			data: map[string]any{"colitems": []any{
				map[string]any{"title": "A"},
				map[string]any{"titel": "B"},
			}},
			want: []wantErr{we("unknown-field", "colitems › [1] › titel", "B",
				`unknown field "titel"`, `did you mean "title"?`)},
		},
		{
			name:    "list of section-template item wrong type",
			tmpl:    listSectionTmpl,
			resolve: colResolve,
			data:    map[string]any{"colitems": []any{"A"}},
			want: []wantErr{we("type", "colitems › [0]", "A",
				`type: expected a mapping for section template "col", got a quoted string`,
				"write the section data as YAML key: value pairs")},
		},

		// --- unknown-field suggestions ------------------------------------
		{
			name: "unknown field suggests title",
			tmpl: suggestTmpl,
			data: map[string]any{"titel": "x"},
			want: []wantErr{we("unknown-field", "titel", "x",
				`unknown field "titel"`, `did you mean "title"?`)},
		},
		{
			name: "misspelled _format sibling suggests _format key",
			tmpl: suggestTmpl,
			data: map[string]any{"metric_fromat": "exact"},
			want: []wantErr{we("unknown-field", "metric_fromat", "exact",
				`unknown field "metric_fromat"`, `did you mean "metric_format"?`)},
		},
		{
			name: "unknown field suggests declared field",
			tmpl: suggestTmpl,
			data: map[string]any{"metrik": 1},
			want: []wantErr{we("unknown-field", "metrik", 1,
				`unknown field "metrik"`, `did you mean "metric"?`)},
		},
		{
			// Nothing is close, so the error carries no "did you mean".
			name: "far-off unknown field has no suggestion",
			tmpl: suggestTmpl,
			data: map[string]any{"qwertyuiop": 1},
			want: []wantErr{we("unknown-field", "qwertyuiop", 1,
				`unknown field "qwertyuiop"`,
				`remove "qwertyuiop" or use one of the template's declared fields`)},
		},

		// --- body key -----------------------------------------------------
		{
			name: "body key rejected with guidance",
			tmpl: bodyTmpl,
			data: map[string]any{"body": "hello"},
			want: []wantErr{we("unknown-field", "body", "hello",
				`unknown field "body"`,
				"body content comes from the Markdown after the frontmatter, not a body: key; body rules are declared in the template")},
		},
		{
			// A body-required template's missing body is not a CheckValues
			// error: `body` is the implied field, enforced by the body rules
			// (phase 5), not a declared frontmatter field.
			name: "missing body is not a CheckValues error",
			tmpl: bodyTmpl,
			data: map[string]any{},
		},

		// --- controlled-effect keys ---------------------------------------
		{
			name: "transition key rejected",
			tmpl: effectTmpl,
			data: map[string]any{"transition": "fade"},
			want: []wantErr{we("unknown-field", "transition", "fade",
				`unknown field "transition"`,
				`fragments, transitions and backgrounds are set in the template layout, not in content; remove "transition"`)},
		},
		{
			name: "background key rejected",
			tmpl: effectTmpl,
			data: map[string]any{"background": "#fff"},
			want: []wantErr{we("unknown-field", "background", "#fff",
				`unknown field "background"`,
				`fragments, transitions and backgrounds are set in the template layout, not in content; remove "background"`)},
		},
		{
			name: "fragment key rejected",
			tmpl: effectTmpl,
			data: map[string]any{"fragment": true},
			want: []wantErr{we("unknown-field", "fragment", true,
				`unknown field "fragment"`,
				`fragments, transitions and backgrounds are set in the template layout, not in content; remove "fragment"`)},
		},

		// --- _format selection --------------------------------------------
		{
			name: "format valid selection",
			tmpl: fmtTmpl,
			data: map[string]any{"amount": 1, "amount_format": "exact"},
		},
		{
			name: "date format valid selection",
			tmpl: fmtTmpl,
			data: map[string]any{"when": "2026-09-25", "when_format": "short"},
		},
		{
			name: "format unknown name lists valid names",
			tmpl: fmtTmpl,
			data: map[string]any{"amount": 1, "amount_format": "bogus"},
			want: []wantErr{we("format", "amount_format", "bogus",
				`unknown format "bogus" for field "amount" (valid: compact, exact, percent)`,
				"use one of: compact, exact, percent")},
		},
		{
			name: "date format unknown name lists valid names",
			tmpl: fmtTmpl,
			data: map[string]any{"when": "2026-09-25", "when_format": "day"},
			want: []wantErr{we("format", "when_format", "day",
				`unknown format "day" for field "when" (valid: long, short)`,
				"use one of: long, short")},
		},
		{
			name: "format wrong value type",
			tmpl: fmtTmpl,
			data: map[string]any{"amount": 1, "amount_format": 5},
			want: []wantErr{we("format", "amount_format", 5,
				"type: expected a format name, got a number",
				"name one of the field's formats with a string")},
		},
		{
			name: "format key on a field with no formats",
			tmpl: fmtTmpl,
			data: map[string]any{"plain": "x", "plain_format": "anything"},
			want: []wantErr{we("format", "plain_format", "anything",
				`field "plain" declares no formats; "anything" is not available`,
				"remove the key or check the template definition")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckValues(tt.data, tt.tmpl, tt.resolve, tt.opts...)

			if len(got.Errors) != len(tt.want) {
				t.Fatalf("CheckValues() errors = %d, want %d\n got: %s",
					len(got.Errors), len(tt.want), renderErrors(got.Errors))
			}
			for i, w := range tt.want {
				e := got.Errors[i]
				if e.Rule != w.Rule {
					t.Errorf("error[%d] Rule = %q, want %q (what: %s)", i, e.Rule, w.Rule, e.What)
				}
				if p := e.PathString(); p != w.Path {
					t.Errorf("error[%d] Path = %q, want %q", i, p, w.Path)
				}
				if !reflect.DeepEqual(e.Value, w.Value) {
					t.Errorf("error[%d] Value = %#v, want %#v", i, e.Value, w.Value)
				}
				if e.What != w.What {
					t.Errorf("error[%d] What = %q, want %q", i, e.What, w.What)
				}
				if e.Fix != w.Fix {
					t.Errorf("error[%d] Fix = %q, want %q", i, e.Fix, w.Fix)
				}
				if e.Line != tt.wantLine {
					t.Errorf("error[%d] Line = %d, want %d", i, e.Line, tt.wantLine)
				}
			}

			assertLinks(t, got.Links, tt.wantLinks)
		})
	}
}

// assertLinks compares the collected LinkRefs field by field so a link's
// position or label regression is caught without depending on slice identity.
func assertLinks(t *testing.T, got, want []LinkRef) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Links = %v, want %v", got, want)
	}
	for i, w := range want {
		g := got[i]
		if p := PathString(g.Path); p != PathString(w.Path) {
			t.Errorf("links[%d].Path = %q, want %q", i, p, PathString(w.Path))
		}
		if g.Line != w.Line {
			t.Errorf("links[%d].Line = %d, want %d", i, g.Line, w.Line)
		}
		if g.Text != w.Text {
			t.Errorf("links[%d].Text = %q, want %q", i, g.Text, w.Text)
		}
		if g.Destination != w.Destination {
			t.Errorf("links[%d].Destination = %q, want %q", i, g.Destination, w.Destination)
		}
		if g.Label != w.Label {
			t.Errorf("links[%d].Label = %q, want %q", i, g.Label, w.Label)
		}
	}
}

// renderErrors is a failure aid: the full positioned errors, one per line.
func renderErrors(errs []ValueError) string {
	if len(errs) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for i, e := range errs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(e.Error())
	}
	return b.String()
}

// TestCheckValuesNesting extends the schema checks to arbitrary composition
// depth: a section-template value may itself carry a section-template value, and
// the checker recurses with the full path while still suggesting the nested
// template's own field names. Section.Walk proves the same composition nests
// without a depth limit and reports an undefined nested template.
func TestCheckValuesNesting(t *testing.T) {
	inner := newSection("inner", Field{Name: "caption", Type: FieldText, MaxLength: 20})
	mid := newSection("mid", Field{Name: "sub", Type: FieldSectionTemplate, SectionTemplate: "inner"})
	outer := newSlide("outer", Field{Name: "block", Type: FieldSectionTemplate, SectionTemplate: "mid"})
	resolve := resolverFor(inner, mid)

	t.Run("two levels valid", func(t *testing.T) {
		got := CheckValues(map[string]any{
			"block": map[string]any{"sub": map[string]any{"caption": "hi"}},
		}, outer, resolve)
		if len(got.Errors) != 0 {
			t.Fatalf("errors = %s, want none", renderErrors(got.Errors))
		}
	})

	t.Run("two levels unknown field suggests nested field", func(t *testing.T) {
		got := CheckValues(map[string]any{
			"block": map[string]any{"sub": map[string]any{"captoin": "hi"}},
		}, outer, resolve)
		if len(got.Errors) != 1 {
			t.Fatalf("errors = %s, want 1", renderErrors(got.Errors))
		}
		e := got.Errors[0]
		if e.Rule != "unknown-field" || e.PathString() != "block › sub › captoin" ||
			e.Fix != `did you mean "caption"?` {
			t.Errorf("error = rule %q path %q fix %q", e.Rule, e.PathString(), e.Fix)
		}
	})

	t.Run("two levels value not a mapping", func(t *testing.T) {
		got := CheckValues(map[string]any{
			"block": map[string]any{"sub": "x"},
		}, outer, resolve)
		if len(got.Errors) != 1 {
			t.Fatalf("errors = %s, want 1", renderErrors(got.Errors))
		}
		e := got.Errors[0]
		if e.Rule != "type" || e.PathString() != "block › sub" {
			t.Errorf("error = rule %q path %q, want type at block › sub", e.Rule, e.PathString())
		}
	})

	t.Run("Section.Walk descends the composition tree", func(t *testing.T) {
		leaf := newSection("leaf")
		midA := newSection("midA")
		midA.Sections = []SectionDecl{{Name: "inner", Accepted: []string{"leaf"}}}
		root := newSlide("root")
		root.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"midA"}}}

		walked, err := NewSection(resolverFor(midA, leaf)).Walk(root)
		if err != nil {
			t.Fatalf("Walk() error = %v", err)
		}
		var names []string
		for _, tmpl := range walked {
			names = append(names, tmpl.Name)
		}
		if !reflect.DeepEqual(names, []string{"midA", "leaf"}) {
			t.Errorf("Walk() = %v, want [midA leaf]", names)
		}
	})

	t.Run("Section.Walk reports an undefined nested template", func(t *testing.T) {
		midGhost := newSection("midGhost")
		midGhost.Sections = []SectionDecl{{Name: "x", Accepted: []string{"ghost"}}}
		root := newSlide("root2")
		root.Sections = []SectionDecl{{Name: "x", Accepted: []string{"midGhost"}}}

		_, err := NewSection(resolverFor(midGhost)).Walk(root)
		if err == nil || err.Error() != `section template "ghost" is not defined` {
			t.Errorf("Walk() error = %v, want undefined ghost", err)
		}
	})
}

// TestCheckValuesCompositionRepeats covers Section.CheckRepeats: the min/max
// repeat limits a slide template declares over its section instances, in section
// declaration order, with one positioned error per excess instance and a single
// one when a minimum is unmet. It also proves the WithLineSource option attaches
// the section instance's line.
func TestCheckValuesCompositionRepeats(t *testing.T) {
	composed := &Template{
		Name:  "composed",
		Usage: UsageSlide,
		Sections: []SectionDecl{
			{Name: "columns", Accepted: []string{"column"}, Min: 2, Max: 4},
			{Name: "footer", Accepted: []string{"column"}, Min: 0, Max: 1},
		},
	}

	tests := []struct {
		name  string
		names []string
		opts  []Option
		want  []wantErr
	}{
		{
			name:  "at the minimum",
			names: []string{"columns", "columns"},
		},
		{
			name:  "below the minimum",
			names: []string{"columns"},
			want: []wantErr{we("min_sections", "columns", 1,
				`min_sections: section "columns" has 1 instance(s), minimum is 2`,
				`add at least 1 "columns" section(s)`)},
		},
		{
			name:  "above the maximum",
			names: []string{"columns", "columns", "columns", "columns", "columns"},
			want: []wantErr{we("max_sections", "columns[4]", 5,
				`max_sections: section "columns" has 5 instance(s), maximum is 4`,
				`use at most 4 "columns" section(s)`)},
		},
		{
			name:  "undeclared sections are ignored",
			names: []string{"mystery", "columns", "columns"},
		},
		{
			name:  "second declared section over its max",
			names: []string{"columns", "columns", "footer", "footer"},
			want: []wantErr{we("max_sections", "footer[1]", 2,
				`max_sections: section "footer" has 2 instance(s), maximum is 1`,
				`use at most 1 "footer" section(s)`)},
		},
		{
			name:  "line source attaches the instance line",
			names: []string{"columns", "columns", "columns", "columns", "columns"},
			opts:  []Option{WithLineSource(func([]PathSegment) int { return 42 })},
			want: []wantErr{we("max_sections", "columns[4]", 5,
				`max_sections: section "columns" has 5 instance(s), maximum is 4`,
				`use at most 4 "columns" section(s)`)},
		},
	}

	sec := NewSection(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sec.CheckRepeats(composed, tt.names, tt.opts...)
			if len(got) != len(tt.want) {
				t.Fatalf("CheckRepeats() = %s, want %d error(s)", renderErrors(got), len(tt.want))
			}
			for i, w := range tt.want {
				e := got[i]
				if e.Rule != w.Rule || e.PathString() != w.Path ||
					!reflect.DeepEqual(e.Value, w.Value) || e.What != w.What || e.Fix != w.Fix {
					t.Errorf("error[%d] = rule %q path %q value %v what %q fix %q, want %+v",
						i, e.Rule, e.PathString(), e.Value, e.What, e.Fix, w)
				}
				if len(tt.opts) > 0 && e.Line != 42 {
					t.Errorf("error[%d] Line = %d, want 42", i, e.Line)
				}
			}
		})
	}

	// This is the unit-test deliverable for plan.phase-02.task-8: parity
	// between a Go-authored *Template (like composed above) and the same
	// definition decoded from a template.yaml manifest by parseManifest —
	// CheckValues and Section.CheckRepeats behave identically either way,
	// because both feed the same *Template shape.
	t.Run("manifest-derived definitions: parity with Go-authored fixtures", func(t *testing.T) {
		t.Run("field rules and number/date formats", func(t *testing.T) {
			manifestTmpl := mustParseManifest(t, "mfield", KindSlide, `fields:
  - name: title
    type: text
    required: true
    max_length: 10
  - name: count
    type: number
    min: 0
    max: 100
    formats: [compact, exact]
    default_format: compact
  - name: when
    type: date
    min_date: "2020-01-01"
    max_date: "2025-12-31"
    formats: [long, short]
    default_format: long
`)
			goTmpl := newSlide("mfield",
				Field{Name: "title", Type: FieldText, Required: true, MaxLength: 10},
				Field{Name: "count", Type: FieldNumber, Min: float64Ptr(0), Max: float64Ptr(100),
					Formats: []string{"compact", "exact"}, DefaultFormat: "compact"},
				Field{Name: "when", Type: FieldDate, MinDate: "2020-01-01", MaxDate: "2025-12-31",
					Formats: []string{"long", "short"}, DefaultFormat: "long"},
			)

			cases := []map[string]any{
				{"title": "hello", "count": 42, "when": "2023-01-01"},
				{"title": "", "count": -1, "when": "2019-01-01"},
				{"count": 42, "when": "2023-01-01"}, // missing required title
			}
			for i, data := range cases {
				gotManifest := CheckValues(data, manifestTmpl, nil)
				gotGo := CheckValues(data, goTmpl, nil)
				if len(gotManifest.Errors) != len(gotGo.Errors) {
					t.Fatalf("case %d: manifest errors = %s, go errors = %s, want the same count",
						i, renderErrors(gotManifest.Errors), renderErrors(gotGo.Errors))
				}
				for j := range gotGo.Errors {
					gm, gg := gotManifest.Errors[j], gotGo.Errors[j]
					if gm.Rule != gg.Rule || gm.PathString() != gg.PathString() || gm.What != gg.What || gm.Fix != gg.Fix {
						t.Errorf("case %d error[%d]: manifest = %+v, go = %+v, want the same", i, j, gm, gg)
					}
				}
			}
		})

		t.Run("section composition min/max repeats", func(t *testing.T) {
			manifestTmpl := mustParseManifest(t, "mcomposed", KindSlide, `sections:
  - name: columns
    accepted: [column]
    min: 2
    max: 4
  - name: footer
    accepted: [column]
    min: 0
    max: 1
`)
			sec := NewSection(nil)
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					got := sec.CheckRepeats(manifestTmpl, tt.names, tt.opts...)
					want := sec.CheckRepeats(composed, tt.names, tt.opts...)
					if len(got) != len(want) {
						t.Fatalf("manifest CheckRepeats() = %s, want the same shape as the Go-authored fixture's %s",
							renderErrors(got), renderErrors(want))
					}
					for i := range want {
						if got[i].Rule != want[i].Rule || got[i].PathString() != want[i].PathString() ||
							got[i].What != want[i].What || got[i].Fix != want[i].Fix {
							t.Errorf("error[%d] = %+v, want %+v", i, got[i], want[i])
						}
					}
				})
			}
		})
	})
}

// TestCheckValuesFormats coordinates with format.go: the checker resolves a
// `_format` selection through Format.Resolve (the one source of truth the
// renderer shares), and the resolution itself applies the field's default_format
// when no selection is written and reports a *FormatError naming the valid
// formats. CheckValues only sees the selection key; default_format and the
// returned function are the Format unit's contract.
func TestCheckValuesFormats(t *testing.T) {
	amount := &Field{Name: "amount", Type: FieldNumber,
		Formats: []string{"compact", "exact", "percent"}, DefaultFormat: "compact"}
	when := &Field{Name: "when", Type: FieldDate,
		Formats: []string{"long", "short"}, DefaultFormat: "long"}
	noFormats := &Field{Name: "plain", Type: FieldText}
	badDefault := &Field{Name: "bad", Type: FieldNumber,
		Formats: []string{"compact"}, DefaultFormat: "nope"}
	noDeclared := &Field{Name: "nd", Type: FieldNumber}

	t.Run("default_format applies when no selection", func(t *testing.T) {
		if fn, err := BuiltinFormats.Resolve(amount, ""); err != nil || fn == nil {
			t.Errorf(`Resolve(amount, "") = (%v, %v), want the compact default`, fn, err)
		}
		if fn, err := BuiltinFormats.Resolve(when, ""); err != nil || fn == nil {
			t.Errorf(`Resolve(when, "") = (%v, %v), want the long default`, fn, err)
		}
	})

	t.Run("declared selection resolves", func(t *testing.T) {
		if fn, err := BuiltinFormats.Resolve(amount, "exact"); err != nil || fn == nil {
			t.Errorf(`Resolve(amount, "exact") = (%v, %v), want a function`, fn, err)
		}
		if fn, err := BuiltinFormats.Resolve(when, "short"); err != nil || fn == nil {
			t.Errorf(`Resolve(when, "short") = (%v, %v), want a function`, fn, err)
		}
	})

	t.Run("no effective format yields nil", func(t *testing.T) {
		if fn, err := BuiltinFormats.Resolve(noFormats, ""); err != nil || fn != nil {
			t.Errorf(`Resolve(plain, "") = (%v, %v), want (nil, nil)`, fn, err)
		}
	})

	t.Run("unknown selection names the valid formats", func(t *testing.T) {
		_, err := BuiltinFormats.Resolve(amount, "bogus")
		var fe *FormatError
		if !errors.As(err, &fe) {
			t.Fatalf("Resolve() error = %v, want *FormatError", err)
		}
		if fe.Field != "amount" || fe.Name != "bogus" {
			t.Errorf("FormatError = field %q name %q, want amount/bogus", fe.Field, fe.Name)
		}
		if !reflect.DeepEqual(fe.Valid, []string{"compact", "exact", "percent"}) {
			t.Errorf("FormatError.Valid = %v, want the field's declared formats", fe.Valid)
		}
		if got, want := fe.Error(), `unknown format "bogus" for field "amount" (valid: compact, exact, percent)`; got != want {
			t.Errorf("FormatError.Error() = %q, want %q", got, want)
		}
	})

	t.Run("format on a field with no formats", func(t *testing.T) {
		_, err := BuiltinFormats.Resolve(noFormats, "x")
		var fe *FormatError
		if !errors.As(err, &fe) {
			t.Fatalf("Resolve() error = %v, want *FormatError", err)
		}
		if len(fe.Valid) != 0 {
			t.Errorf("FormatError.Valid = %v, want empty", fe.Valid)
		}
		if got, want := fe.Error(), `field "plain" declares no formats; "x" is not available`; got != want {
			t.Errorf("FormatError.Error() = %q, want %q", got, want)
		}
	})

	t.Run("unknown default_format is reported", func(t *testing.T) {
		_, err := BuiltinFormats.Resolve(badDefault, "")
		var fe *FormatError
		if !errors.As(err, &fe) {
			t.Fatalf("Resolve() error = %v, want *FormatError", err)
		}
		if fe.Name != "nope" || !reflect.DeepEqual(fe.Valid, []string{"compact"}) {
			t.Errorf("FormatError = name %q valid %v, want nope/[compact]", fe.Name, fe.Valid)
		}
	})

	t.Run("empty Formats uses the catalogue for the kind", func(t *testing.T) {
		if fn, err := BuiltinFormats.Resolve(noDeclared, "compact"); err != nil || fn == nil {
			t.Errorf(`Resolve(nd, "compact") = (%v, %v), want a function`, fn, err)
		}
		_, err := BuiltinFormats.Resolve(noDeclared, "bogus")
		var fe *FormatError
		if !errors.As(err, &fe) {
			t.Fatalf("Resolve() error = %v, want *FormatError", err)
		}
		if !reflect.DeepEqual(fe.Valid, []string{"compact", "exact", "percent"}) {
			t.Errorf("FormatError.Valid = %v, want the sorted catalogue names", fe.Valid)
		}
	})
}

// float64Ptr returns a pointer to v, for the Field.Min/Max bounds.
func float64Ptr(v float64) *float64 { return &v }
