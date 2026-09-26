package template

import (
	"reflect"
	"testing"
)

// This file is the unit-test deliverable for phase-05.task-11: it exercises
// CheckBody, the implied-body rule checker, through the real API and asserts
// against the real ValueError shape and the real CommonMark-core measurement
// (goldmark, the same parser internal/mdcheck uses).
//
// The `body:` YAML-key rejection is deliberately not tested here: it is
// reported by CheckValues' unknown-key branch (it is the layer that sees YAML
// keys) and is already covered in checkvalues_test.go; CheckBody owns the two
// guidance strings only.
//
// The test stays in package template because internal/validate imports
// internal/template: a test that imported validate here would be an import
// cycle. The optional validate-level adaptation case belongs to the
// internal/validate tests (phase-05.task-4).

// weBody is a wantErr constructor for a body violation: CheckBody always
// carries the implied `body` path, so only the rule and message vary.
func weBody(rule string, value any, what, fix string) *wantErr {
	w := we(rule, "body", value, what, fix)
	return &w
}

// TestCheckBody is the table-driven suite for template.CheckBody. Each case
// asserts the first violation's rule, path, actual value, author-facing
// what/fix text and reported line, or that a valid body yields the zero
// ValueError and false.
func TestCheckBody(t *testing.T) {
	// The guidance strings shared by several cases, read straight from the
	// implementation's contract so a wording drift is caught.
	const (
		requiredWhat = "required: the body is required but missing"
		requiredFix  = "add body text after the frontmatter"

		disallowedFix = "remove the body text; this template takes data only"
		subheadFix    = "remove the subheading, or express it as a field or a new slide"
	)

	tests := []struct {
		name     string
		rule     BodyRule
		body     string
		opts     []BodyOption
		want     *wantErr
		wantLine int
	}{
		// --- mode required ------------------------------------------------
		{
			name: "required: empty body is missing",
			rule: BodyRule{Mode: BodyRequired},
			body: "",
			want: weBody("required", nil, requiredWhat, requiredFix),
		},
		{
			name: "required: whitespace-only body is missing",
			rule: BodyRule{Mode: BodyRequired},
			body: "   \n\t\n  ",
			want: weBody("required", nil, requiredWhat, requiredFix),
		},
		{
			name: "required: non-empty body passes",
			rule: BodyRule{Mode: BodyRequired},
			body: "Hello world",
		},

		// --- mode optional ------------------------------------------------
		{
			name: "optional: absent body passes",
			rule: BodyRule{Mode: BodyOptional},
			body: "",
		},
		{
			name: "optional: present body passes",
			rule: BodyRule{Mode: BodyOptional},
			body: "Some words here",
		},

		// --- mode disallowed ----------------------------------------------
		{
			name: "disallowed: any body errors with its word count",
			rule: BodyRule{Mode: BodyDisallowed},
			body: "hello world",
			want: weBody("disallowed", "hello world",
				"disallowed: the body is not allowed, but the body has 2 word(s)",
				disallowedFix),
		},
		{
			// The count is of Markdown-stripped text ("bold and code" = 3),
			// not the raw source.
			name: "disallowed: counts stripped words",
			rule: BodyRule{Mode: BodyDisallowed},
			body: "**bold** and `code`",
			want: weBody("disallowed", "**bold** and `code`",
				"disallowed: the body is not allowed, but the body has 3 word(s)",
				disallowedFix),
		},
		{
			name: "disallowed: empty body passes",
			rule: BodyRule{Mode: BodyDisallowed},
			body: "",
		},
		{
			name: "disallowed: whitespace-only body passes",
			rule: BodyRule{Mode: BodyDisallowed},
			body: " \n\t ",
		},

		// --- max_words ----------------------------------------------------
		{
			name: "max_words: under the limit passes",
			rule: BodyRule{Mode: BodyOptional, MaxWords: 5},
			body: "one two three",
		},
		{
			name: "max_words: exactly at the limit passes",
			rule: BodyRule{Mode: BodyOptional, MaxWords: 3},
			body: "one two three",
		},
		{
			name: "max_words: over the limit errors",
			rule: BodyRule{Mode: BodyOptional, MaxWords: 2},
			body: "one two three",
			want: weBody("max_words", "one two three",
				"max_words: the body is 3 words, maximum is 2",
				"shorten the body to at most 2 words"),
		},
		{
			// Emphasis and inline-code markup must not be counted: the
			// stripped text is "bold and code" = 3 words, not the raw source.
			name: "max_words: counts Markdown-stripped text",
			rule: BodyRule{Mode: BodyOptional, MaxWords: 2},
			body: "**bold** and `code`",
			want: weBody("max_words", "**bold** and `code`",
				"max_words: the body is 3 words, maximum is 2",
				"shorten the body to at most 2 words"),
		},
		{
			// A link's visible text counts; its destination does not. The
			// raw body is far longer than the stripped "see the docs here".
			name: "max_words: link text counts, destination does not",
			rule: BodyRule{Mode: BodyOptional, MaxWords: 3},
			body: "see [the docs](https://example.com/really/long/path) here",
			want: weBody("max_words", "see [the docs](https://example.com/really/long/path) here",
				"max_words: the body is 4 words, maximum is 3",
				"shorten the body to at most 3 words"),
		},

		// --- max_paragraphs -----------------------------------------------
		{
			name: "max_paragraphs: under the limit passes",
			rule: BodyRule{Mode: BodyOptional, MaxParagraphs: 3},
			body: "one\n\ntwo",
		},
		{
			name: "max_paragraphs: exactly at the limit passes",
			rule: BodyRule{Mode: BodyOptional, MaxParagraphs: 2},
			body: "one\n\ntwo",
		},
		{
			name: "max_paragraphs: over the limit errors",
			rule: BodyRule{Mode: BodyOptional, MaxParagraphs: 2},
			body: "one\n\ntwo\n\nthree",
			want: weBody("max_paragraphs", "one\n\ntwo\n\nthree",
				"max_paragraphs: the body has 3 paragraphs, maximum is 2",
				"shorten the body to at most 2 paragraphs"),
		},
		{
			// A loose list item's content is wrapped in a Paragraph whose
			// parent is the ListItem, not the document, so list looseness
			// must not change the paragraph count.
			name: "max_paragraphs: loose list items are not paragraphs",
			rule: BodyRule{Mode: BodyOptional, MaxParagraphs: 1},
			body: "- alpha\n\n- bravo\n\n- charlie\n",
		},

		// --- max_list_items -----------------------------------------------
		{
			name: "max_list_items: under the limit passes",
			rule: BodyRule{Mode: BodyOptional, MaxListItems: 3},
			body: "- one\n- two",
		},
		{
			name: "max_list_items: exactly at the limit passes",
			rule: BodyRule{Mode: BodyOptional, MaxListItems: 2},
			body: "- one\n- two",
		},
		{
			name: "max_list_items: over the limit errors",
			rule: BodyRule{Mode: BodyOptional, MaxListItems: 2},
			body: "- one\n- two\n- three",
			want: weBody("max_list_items", "- one\n- two\n- three",
				"max_list_items: the body has 3 list items, maximum is 2",
				"shorten the body to at most 2 list items"),
		},
		{
			// Items are counted across every list, not just the first.
			name: "max_list_items: counts items across separate lists",
			rule: BodyRule{Mode: BodyOptional, MaxListItems: 2},
			body: "- a\n- b\n\n1. c",
			want: weBody("max_list_items", "- a\n- b\n\n1. c",
				"max_list_items: the body has 3 list items, maximum is 2",
				"shorten the body to at most 2 list items"),
		},

		// --- subheadings --------------------------------------------------
		{
			name: "subheadings false: ## is rejected",
			rule: BodyRule{Mode: BodyOptional},
			body: "## Details",
			want: weBody("subheadings", "## Details",
				`subheadings: "## Details" is a subheading, but subheadings are not allowed`,
				subheadFix),
		},
		{
			name: "subheadings false: ### is rejected",
			rule: BodyRule{Mode: BodyOptional},
			body: "### Smaller",
			want: weBody("subheadings", "### Smaller",
				`subheadings: "### Smaller" is a subheading, but subheadings are not allowed`,
				subheadFix),
		},
		{
			name: "subheadings true: ## and ### are allowed",
			rule: BodyRule{Mode: BodyOptional, Subheadings: true},
			body: "## Details\n\n### Even smaller",
		},
		{
			// Only ## and ### are subheadings; # is reserved and rejected
			// elsewhere (mdcheck/slide parser), so CheckBody does not flag it.
			name: "subheadings false: # is not a subheading",
			rule: BodyRule{Mode: BodyOptional},
			body: "# Reserved top-level",
		},

		// --- WithBodyLine -------------------------------------------------
		{
			name:     "line: missing required body carries the supplied line",
			rule:     BodyRule{Mode: BodyRequired},
			body:     "",
			opts:     []BodyOption{WithBodyLine(7)},
			want:     weBody("required", nil, requiredWhat, requiredFix),
			wantLine: 7,
		},
		{
			name: "line: a size violation carries the body start line",
			rule: BodyRule{Mode: BodyOptional, MaxWords: 1},
			body: "one two",
			opts: []BodyOption{WithBodyLine(4)},
			want: weBody("max_words", "one two",
				"max_words: the body is 2 words, maximum is 1",
				"shorten the body to at most 1 words"),
			wantLine: 4,
		},
		{
			// A forbidden subheading is positioned on its own line, not the
			// body's start line: body line 3 with the body starting at file
			// line 5 is file line 7.
			name: "line: forbidden subheading placed on its own line",
			rule: BodyRule{Mode: BodyOptional},
			body: "intro\n\n## Details",
			opts: []BodyOption{WithBodyLine(5)},
			want: weBody("subheadings", "## Details",
				`subheadings: "## Details" is a subheading, but subheadings are not allowed`,
				subheadFix),
			wantLine: 7,
		},
		{
			name: "line: subheading on the first body line",
			rule: BodyRule{Mode: BodyOptional},
			body: "## Details",
			opts: []BodyOption{WithBodyLine(1)},
			want: weBody("subheadings", "## Details",
				`subheadings: "## Details" is a subheading, but subheadings are not allowed`,
				subheadFix),
			wantLine: 1,
		},

		// --- zero/negative limits and unknown modes -----------------------
		{
			// A zero (unset) bound means no limit: a body with plenty of
			// words, paragraphs and list items still passes.
			name: "zero limits mean no limit",
			rule: BodyRule{Mode: BodyOptional},
			body: "one two three four five six seven eight nine ten\n\neleven twelve\n\n- a\n- b\n- c\n- d",
		},
		{
			name: "negative limits mean no limit",
			rule: BodyRule{Mode: BodyOptional, MaxWords: -1, MaxParagraphs: -1, MaxListItems: -1},
			body: "one two three four five six seven eight nine ten\n\neleven twelve\n\n- a\n- b\n- c\n- d",
		},
		{
			// Build-time checks own mode validity, so an unrecognised mode
			// is treated as optional by CheckBody.
			name: "unrecognised mode: absent body is optional",
			rule: BodyRule{Mode: BodyMode("bogus")},
			body: "",
		},
		{
			name: "unrecognised mode: present body passes",
			rule: BodyRule{Mode: BodyMode("bogus")},
			body: "anything goes",
		},

		// --- first-violation order ----------------------------------------
		{
			name: "order: required beats the size limits",
			rule: BodyRule{Mode: BodyRequired, MaxWords: 1},
			body: "",
			want: weBody("required", nil, requiredWhat, requiredFix),
		},
		{
			name: "order: disallowed beats the size limits",
			rule: BodyRule{Mode: BodyDisallowed, MaxWords: 1},
			body: "one two",
			want: weBody("disallowed", "one two",
				"disallowed: the body is not allowed, but the body has 2 word(s)",
				disallowedFix),
		},
		{
			name: "order: max_words beats subheadings",
			rule: BodyRule{Mode: BodyOptional, MaxWords: 1},
			body: "## Details here",
			want: weBody("max_words", "## Details here",
				"max_words: the body is 2 words, maximum is 1",
				"shorten the body to at most 1 words"),
		},
		{
			name: "order: max_paragraphs beats max_list_items",
			rule: BodyRule{Mode: BodyOptional, MaxParagraphs: 1, MaxListItems: 2},
			body: "one\n\ntwo\n\n- a\n- b\n- c",
			want: weBody("max_paragraphs", "one\n\ntwo\n\n- a\n- b\n- c",
				"max_paragraphs: the body has 2 paragraphs, maximum is 1",
				"shorten the body to at most 1 paragraphs"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, invalid := CheckBody(tt.rule, tt.body, tt.opts...)

			if tt.want == nil {
				if invalid {
					t.Fatalf("CheckBody() = (%s, true), want a valid body",
						renderErrors([]ValueError{got}))
				}
				if !reflect.DeepEqual(got, ValueError{}) {
					t.Fatalf("CheckBody() = %+v, false, want the zero ValueError", got)
				}
				return
			}

			if !invalid {
				t.Fatalf("CheckBody() invalid = false, want rule %q", tt.want.Rule)
			}
			if got.Rule != tt.want.Rule {
				t.Errorf("Rule = %q, want %q", got.Rule, tt.want.Rule)
			}
			if p := got.PathString(); p != tt.want.Path {
				t.Errorf("Path = %q, want %q", p, tt.want.Path)
			}
			if !reflect.DeepEqual(got.Value, tt.want.Value) {
				t.Errorf("Value = %#v, want %#v", got.Value, tt.want.Value)
			}
			if got.What != tt.want.What {
				t.Errorf("What = %q, want %q", got.What, tt.want.What)
			}
			if got.Fix != tt.want.Fix {
				t.Errorf("Fix = %q, want %q", got.Fix, tt.want.Fix)
			}
			if got.Line != tt.wantLine {
				t.Errorf("Line = %d, want %d", got.Line, tt.wantLine)
			}
		})
	}
}
