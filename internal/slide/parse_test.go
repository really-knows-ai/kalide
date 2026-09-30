package slide

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

// fakeTemplate is one fake catalogue entry: a template's usage and, for
// slide templates, its declared sections with their accepted section templates
// and repeat bounds.
type fakeTemplate struct {
	usage    string
	sections []string
	accepted map[string][]string
	min      map[string]int
	max      map[string]int
}

// fakeCatalogue is a hand-written Catalogue: the parser only ever consumes the
// interface, so tests can declare exactly the names a case needs without
// dragging in internal/template. max <= 0 means unbounded, matching how the
// parser reads SectionDecl (a positive max is a hard limit).
type fakeCatalogue map[string]fakeTemplate

func (c fakeCatalogue) LookupSlideTemplate(name string) (string, bool) {
	t, ok := c[name]
	if !ok {
		return "", false
	}
	return t.usage, true
}

func (c fakeCatalogue) TemplateNames() []string {
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c fakeCatalogue) SectionNames(tmpl string) []string {
	names := append([]string(nil), c[tmpl].sections...)
	sort.Strings(names)
	return names
}

func (c fakeCatalogue) SectionDecl(tmpl, section string) ([]string, int, int, bool) {
	t, ok := c[tmpl]
	if !ok || !slices.Contains(t.sections, section) {
		return nil, 0, 0, false
	}
	return t.accepted[section], t.min[section], t.max[section], true
}

// testCatalogue returns the catalogue most cases share. It exercises every
// shape Parse branches on:
//
//   - content: two sections, "columns" accepting exactly one template and
//     "gallery" accepting two (>1 => a template: is required);
//   - limited / needs / once: "columns" with a hard max, a min, and a max of 1;
//   - badnotes: a slide template that (illegally) declares the reserved
//     "notes" section name.
func testCatalogue() fakeCatalogue {
	return fakeCatalogue{
		"title": {usage: "slide"},
		"content": {usage: "slide", sections: []string{"columns", "gallery"},
			accepted: map[string][]string{
				"columns": {"column"},
				"gallery": {"fig", "card"},
			}},
		"limited": {usage: "slide", sections: []string{"columns"},
			accepted: map[string][]string{"columns": {"column"}},
			max:      map[string]int{"columns": 2}},
		"needs": {usage: "slide", sections: []string{"columns"},
			accepted: map[string][]string{"columns": {"column"}},
			min:      map[string]int{"columns": 2}},
		"once": {usage: "slide", sections: []string{"columns"},
			accepted: map[string][]string{"columns": {"column"}},
			max:      map[string]int{"columns": 1}},
		"badnotes": {usage: "slide", sections: []string{NotesSection}},
		"columns":  {usage: "section"},
		"column":   {usage: "section"},
		"fig":      {usage: "section"},
		"card":     {usage: "section"},

		// Nested composition: a slide template whose "columns" section accepts
		// the container section template "group", which itself declares child
		// sections ("blocks") accepting the leaf section template "block"
		// (template-composition, slide-sections). "limitedgroup"/"needsgroup"
		// are the same shape with a per-parent max/min on "blocks", so a
		// parent's repeat bounds can be exercised over its own children alone.
		"grouped": {usage: "slide", sections: []string{"columns"},
			accepted: map[string][]string{"columns": {"group", "limitedgroup", "needsgroup"}}},
		"group": {usage: "section", sections: []string{"blocks"},
			accepted: map[string][]string{"blocks": {"block"}}},
		"limitedgroup": {usage: "section", sections: []string{"blocks"},
			accepted: map[string][]string{"blocks": {"block"}},
			max:      map[string]int{"blocks": 2}},
		"needsgroup": {usage: "section", sections: []string{"blocks"},
			accepted: map[string][]string{"blocks": {"block"}},
			min:      map[string]int{"blocks": 2}},
		"block": {usage: "section"},
	}
}

// src joins lines into a source file with a trailing newline, so every test
// case can be written one element per line and counted unambiguously.
func src(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

// parseOK parses src and fails the test on any error.
func parseOK(t *testing.T, file string, src []byte, cat Catalogue) *Slide {
	t.Helper()
	s, err := Parse(file, src, cat)
	if err != nil {
		t.Fatalf("Parse(%s): unexpected error: %v", file, err)
	}
	return s
}

// wantParseError fails unless err is a *ParseError at exactly file:line whose
// message contains every want substring. It also checks the rendered position
// via ParseError.Error, including the section instance's indexed containment
// path when one is present: a top-level section `# columns` is `columns[0]`, so
// the canonical form is `file:line › columns[0]: message`
// (nested-section-validation).
func wantParseError(t *testing.T, err error, file string, line int, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Parse: got nil error, want error at %s:%d", file, line)
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("Parse error type = %T (%v), want *ParseError", err, err)
	}
	if pe.File != file || pe.Line != line {
		t.Fatalf("Parse error = %s:%d: %s; want %s:%d", pe.File, pe.Line, pe.Msg, file, line)
	}
	if line > 0 {
		want := fmt.Sprintf("%s:%d", file, line)
		if pe.Path != "" {
			want += " › " + pe.Path
		}
		want += ": "
		if !strings.HasPrefix(pe.Error(), want) {
			t.Fatalf("ParseError.Error() = %q, want prefix %q", pe.Error(), want)
		}
	}
	for _, w := range wants {
		if !strings.Contains(pe.Msg, w) {
			t.Fatalf("Parse error message = %q, want it to contain %q", pe.Msg, w)
		}
	}
}

// wantParseErrorPath is wantParseError plus an exact containment-path check:
// path is the rendered ` › `-joined chain (name[index] segments), or "" at the
// slide's top level. It pins the canonical path form the source change
// introduces (nested-section-validation).
func wantParseErrorPath(t *testing.T, err error, file string, line int, path string, wants ...string) {
	t.Helper()
	wantParseError(t, err, file, line, wants...)
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("Parse error type = %T (%v), want *ParseError", err, err)
	}
	if pe.Path != path {
		t.Fatalf("ParseError.Path = %q, want %q (Error() = %q)", pe.Path, path, pe.Error())
	}
}

// TestParse covers internal/slide.Parse (deck-structure): frontmatter,
// template resolution through the Catalogue contract, body/section structure,
// section repeats, section frontmatter fences and reserved `# notes`.
// Everything runs in-process against a fake Catalogue, so it holds under
// -short.
func TestParse(t *testing.T) {
	t.Run("constants", testParseConstants)
	t.Run("frontmatter", testParseFrontmatter)
	t.Run("template", testParseTemplate)
	t.Run("structure", testParseStructure)
	t.Run("nested sections", testParseNestedSections)
	t.Run("sections", testParseSections)
	t.Run("repeats", testParseRepeats)
	t.Run("section fence", testParseSectionFence)
	t.Run("notes", testParseNotes)
}

// testParseConstants pins the reserved names and fences the parser enforces.
func testParseConstants(t *testing.T) {
	if NotesSection != "notes" {
		t.Errorf("NotesSection = %q, want %q", NotesSection, "notes")
	}
	if SlideDelimiter != "---" {
		t.Errorf("SlideDelimiter = %q, want %q", SlideDelimiter, "---")
	}
	if SectionFence != "```" {
		t.Errorf("SectionFence = %q, want %q", SectionFence, "```")
	}
}

// testParseFrontmatter covers the --- … --- frontmatter rules.
func testParseFrontmatter(t *testing.T) {
	cat := testCatalogue()
	const file = "slides/1-intro.md"

	t.Run("missing entirely", func(t *testing.T) {
		_, err := Parse(file, []byte(""), cat)
		wantParseError(t, err, file, 1, "missing slide frontmatter")
	})

	t.Run("not on line 1 errors at the offending line", func(t *testing.T) {
		_, err := Parse(file, src("Title", "---", "template: title", "---"), cat)
		wantParseError(t, err, file, 2, "slide frontmatter must start on line 1")
	})

	t.Run("unterminated errors at the opening line", func(t *testing.T) {
		_, err := Parse(file, src("---", "template: title", "Body"), cat)
		wantParseError(t, err, file, 1, "unterminated slide frontmatter", "---")
	})

	t.Run("missing template key", func(t *testing.T) {
		_, err := Parse(file, src("---", "title: Hello", "---"), cat)
		wantParseError(t, err, file, 1, `missing required key "template"`)
	})

	t.Run("empty template value", func(t *testing.T) {
		_, err := Parse(file, src("---", `template: ""`, "---"), cat)
		wantParseError(t, err, file, 1, `missing required key "template"`)
	})
}

// testParseTemplate covers slide-template lookup and usage through Catalogue.
func testParseTemplate(t *testing.T) {
	cat := testCatalogue()
	const file = "slides/1-intro.md"

	t.Run("unknown template suggests the closest name", func(t *testing.T) {
		_, err := Parse(file, src("---", "template: titel", "---"), cat)
		wantParseError(t, err, file, 2, `unknown template "titel"`, `did you mean "title"?`)
	})

	t.Run("unknown template with nothing close has no suggestion", func(t *testing.T) {
		_, err := Parse(file, src("---", "template: zzzzzzzz", "---"), cat)
		wantParseError(t, err, file, 2, `unknown template "zzzzzzzz"`)
		if strings.Contains(err.Error(), "did you mean") {
			t.Fatalf("error unexpectedly suggested a template: %v", err)
		}
	})

	t.Run("section template used as a slide", func(t *testing.T) {
		_, err := Parse(file, src("---", "template: columns", "---"), cat)
		wantParseError(t, err, file, 2,
			`"columns" is a section template, not a slide template`)
	})

	t.Run("template line points at the key", func(t *testing.T) {
		_, err := Parse(file, src("---", "title: X", "template: titel", "---"), cat)
		wantParseError(t, err, file, 3, `did you mean "title"?`)
	})
}

// testParseStructure covers the body-before-first-section boundary and the
// parsed Slide/Section/Notes shape.
func testParseStructure(t *testing.T) {
	cat := testCatalogue()
	const file = "slides/1-intro.md"

	slide := parseOK(t, file, src(
		"---",
		"template: content",
		"title: Hello",
		"---",
		"",
		"Intro line one",
		"Intro line two",
		"",
		"# columns",
		"body one",
		"",
		"# gallery",
		"```",
		"template: fig",
		"```",
		"gallery body",
		"# notes",
		"notes here",
	), cat)

	if slide.File != file {
		t.Errorf("File = %q, want %q", slide.File, file)
	}
	if slide.Template != "content" || slide.TemplateLine != 2 {
		t.Errorf("Template = %q line %d, want %q line 2", slide.Template, slide.TemplateLine, "content")
	}
	if slide.Frontmatter["title"] != "Hello" {
		t.Errorf("Frontmatter[title] = %v, want %q", slide.Frontmatter["title"], "Hello")
	}
	if wantBody := "\nIntro line one\nIntro line two\n"; slide.Body != wantBody || slide.BodyLine != 5 {
		t.Errorf("Body = %q line %d, want %q line 5", slide.Body, slide.BodyLine, wantBody)
	}

	if len(slide.Sections) != 2 {
		t.Fatalf("len(Sections) = %d, want 2", len(slide.Sections))
	}
	columns, gallery := slide.Sections[0], slide.Sections[1]
	if columns.Name != "columns" || columns.HeadingLine != 9 {
		t.Errorf("Section[0] = %q line %d, want %q line 9", columns.Name, columns.HeadingLine, "columns")
	}
	if columns.Template != "column" {
		t.Errorf("columns.Template = %q, want %q (resolved from the single accepted template)", columns.Template, "column")
	}
	if columns.Body != "body one\n" || columns.BodyLine != 10 {
		t.Errorf("columns.Body = %q line %d, want %q line 10", columns.Body, columns.BodyLine, "body one\n")
	}
	if gallery.Name != "gallery" || gallery.HeadingLine != 12 {
		t.Errorf("Section[1] = %q line %d, want %q line 12", gallery.Name, gallery.HeadingLine, "gallery")
	}
	if gallery.FenceLine != 13 || gallery.TemplateLine != 14 || gallery.Template != "fig" {
		t.Errorf("gallery template = %q (fence %d, key %d), want %q (fence 13, key 14)",
			gallery.Template, gallery.FenceLine, gallery.TemplateLine, "fig")
	}
	if gallery.Body != "gallery body" || gallery.BodyLine != 16 {
		t.Errorf("gallery.Body = %q line %d, want %q line 16", gallery.Body, gallery.BodyLine, "gallery body")
	}

	if slide.Notes == nil {
		t.Fatal("Notes = nil, want the # notes section")
	}
	if slide.Notes.HeadingLine != 17 || slide.Notes.Body != "notes here" || slide.Notes.BodyLine != 18 {
		t.Errorf("Notes = line %d body %q line %d, want line 17 body %q line 18",
			slide.Notes.HeadingLine, slide.Notes.Body, slide.Notes.BodyLine, "notes here")
	}
}

// testParseNestedSections covers the heading-depth nesting contract of
// internal/slide.Parse (slide-sections, nested-section-validation): a heading
// one level deeper than the most recent shallower heading opens a child of
// that section; a child may carry its own plain-fence frontmatter; a child
// heading is scoped to the *resolved* template of its enclosing instance, so
// an undeclared child name is rejected with a closest-match suggestion; a
// parent's min/max is counted over its own child instances and the error
// carries the aggregate parent path (`columns[2] › blocks`); a depth-(n+1)
// heading inside a template that declares NO child sections stays ordinary
// Markdown in the enclosing body; and a depth-skipping sequence (`#` then
// `###`) is still a child of the shallower heading.
func testParseNestedSections(t *testing.T) {
	cat := testCatalogue()
	const file = "slides/1-group.md"

	t.Run("heading-depth nesting", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: group",
			"```",
			"column body",
			"",
			"## blocks",
			"```",
			"template: block",
			"```",
			"block body",
		), cat)

		if len(slide.Sections) != 1 {
			t.Fatalf("len(Sections) = %d, want 1", len(slide.Sections))
		}
		columns := slide.Sections[0]
		if columns.Name != "columns" || columns.Level != 1 || columns.Index != 0 {
			t.Errorf("columns = %q level %d index %d, want %q level 1 index 0", columns.Name, columns.Level, columns.Index, "columns")
		}
		if columns.Body != "column body\n" || columns.BodyLine != 9 {
			t.Errorf("columns.Body = %q line %d, want %q line 9", columns.Body, columns.BodyLine, "column body\n")
		}
		if len(columns.Children) != 1 {
			t.Fatalf("len(columns.Children) = %d, want 1", len(columns.Children))
		}
		blocks := columns.Children[0]
		if blocks.Name != "blocks" || blocks.Level != 2 || blocks.Index != 0 {
			t.Errorf("blocks = %q level %d index %d, want %q level 2 index 0", blocks.Name, blocks.Level, blocks.Index, "blocks")
		}
		if blocks.Template != "block" {
			t.Errorf("blocks.Template = %q, want %q (resolved from the single accepted template)", blocks.Template, "block")
		}
		if blocks.Body != "block body" || blocks.BodyLine != 15 {
			t.Errorf("blocks.Body = %q line %d, want %q line 15", blocks.Body, blocks.BodyLine, "block body")
		}
	})

	t.Run("per-child frontmatter", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: group",
			"```",
			"",
			"## blocks",
			"```",
			"template: block",
			"weight: 3",
			"```",
			"block body",
		), cat)

		blocks := slide.Sections[0].Children[0]
		if blocks.FenceLine != 11 || blocks.TemplateLine != 12 {
			t.Errorf("blocks fence = %d template key = %d, want fence 11 template key 12", blocks.FenceLine, blocks.TemplateLine)
		}
		if blocks.Frontmatter["weight"] != 3 {
			t.Errorf("blocks.Frontmatter[weight] = %v (%T), want 3", blocks.Frontmatter["weight"], blocks.Frontmatter["weight"])
		}
	})

	t.Run("depth-skipping heading is a child of the shallower heading", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: group",
			"```",
			"",
			"### blocks",
			"```",
			"template: block",
			"```",
		), cat)

		columns := slide.Sections[0]
		if len(columns.Children) != 1 {
			t.Fatalf("len(columns.Children) = %d, want 1 (the ### heading nests under #)", len(columns.Children))
		}
		if got := columns.Children[0]; got.Name != "blocks" || got.Level != 3 || got.Index != 0 {
			t.Errorf("child = %q level %d index %d, want %q level 3 index 0", got.Name, got.Level, got.Index, "blocks")
		}
	})

	t.Run("unknown child name suggests the closest declared name", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: group",
			"```",
			"",
			"## blocsk",
		), cat)
		wantParseErrorPath(t, err, file, 10, "columns[0]",
			`unknown section "blocsk"`, `did you mean "blocks"?`)
	})

	t.Run("child name scoped to the resolved template, not the declaring slide", func(t *testing.T) {
		// "blocks" is declared by the group section template, never by the
		// grouped slide template; scoping the child to the slide's own
		// declarations would wrongly reject it.
		slide := parseOK(t, file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: group",
			"```",
			"",
			"## blocks",
		), cat)
		if got := slide.Sections[0].Children; len(got) != 1 || got[0].Name != "blocks" {
			t.Fatalf("columns.Children = %+v, want the single %q child", got, "blocks")
		}
	})

	t.Run("per-parent max carries the aggregate parent path", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: limitedgroup",
			"```",
			"",
			"## blocks",
			"",
			"## blocks",
			"",
			"## blocks",
		), cat)
		wantParseErrorPath(t, err, file, 14, "columns[0] › blocks",
			`section "blocks": at most 2 allowed, found 3`)
	})

	t.Run("per-parent max counts only one parent's children", func(t *testing.T) {
		// Three columns instances each hold two blocks; the max is 2 per
		// parent, so no instance exceeds it even though the slide has six
		// blocks children in total.
		slide := parseOK(t, file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: limitedgroup",
			"```",
			"",
			"## blocks",
			"",
			"## blocks",
			"",
			"# columns",
			"```",
			"template: limitedgroup",
			"```",
			"",
			"## blocks",
			"",
			"## blocks",
			"",
			"# columns",
			"```",
			"template: limitedgroup",
			"```",
			"",
			"## blocks",
			"",
			"## blocks",
		), cat)
		if len(slide.Sections) != 3 {
			t.Fatalf("len(Sections) = %d, want 3", len(slide.Sections))
		}
		for i := range slide.Sections {
			if got := len(slide.Sections[i].Children); got != 2 {
				t.Errorf("Sections[%d].Children = %d, want 2", i, got)
			}
		}
	})

	t.Run("per-parent min carries the aggregate parent path", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: needsgroup",
			"```",
			"",
			"## blocks",
		), cat)
		wantParseErrorPath(t, err, file, 5, "columns[0] › blocks",
			`section "blocks": requires at least 2, found 1`)
	})

	t.Run("deeper heading in a template with no child sections stays in the body", func(t *testing.T) {
		// columns resolves to "column", which declares no child sections, so
		// the ## heading is ordinary Markdown in the column's body.
		slide := parseOK(t, file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"column body",
			"",
			"## subheading",
			"",
			"sub text",
		), cat)

		if len(slide.Sections) != 1 {
			t.Fatalf("len(Sections) = %d, want 1", len(slide.Sections))
		}
		col := slide.Sections[0]
		if len(col.Children) != 0 {
			t.Fatalf("columns.Children = %+v, want none (column declares no child sections)", col.Children)
		}
		const wantBody = "column body\n\n## subheading\n\nsub text"
		if col.Body != wantBody {
			t.Errorf("columns.Body = %q, want %q", col.Body, wantBody)
		}
	})

	t.Run("notes is top-level only", func(t *testing.T) {
		// `# notes` after a nested child is a top-level reserved section, and
		// a deeper heading never starts notes.
		slide := parseOK(t, file, src(
			"---",
			"template: grouped",
			"---",
			"",
			"# columns",
			"```",
			"template: group",
			"```",
			"",
			"## blocks",
			"",
			"# notes",
			"note body",
		), cat)
		if slide.Notes == nil || slide.Notes.HeadingLine != 12 || slide.Notes.Body != "note body" {
			t.Fatalf("Notes = %+v, want the top-level section at line 12 body %q", slide.Notes, "note body")
		}
	})
}

// testParseSections covers section-name declaration and section-template
// resolution errors.
func testParseSections(t *testing.T) {
	cat := testCatalogue()
	const file = "slides/1-intro.md"

	t.Run("undeclared section suggests the closest name", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# colums",
		), cat)
		wantParseError(t, err, file, 5, `unknown section "colums"`, `did you mean "columns"?`)
	})

	t.Run("explicit section template resolves", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"```",
			"template: column",
			"```",
		), cat)
		got := slide.Sections[0]
		if got.Template != "column" || got.TemplateLine != 7 {
			t.Errorf("Template = %q line %d, want %q line 7", got.Template, got.TemplateLine, "column")
		}
	})

	t.Run("section template not accepted by the section", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"```",
			"template: card",
			"```",
		), cat)
		wantParseErrorPath(t, err, file, 7, "columns[0]", `unknown template for section columns "card"`)
	})
}

// testParseRepeats covers min/max repeat limits, including that max <= 0 is
// unbounded.
func testParseRepeats(t *testing.T) {
	cat := testCatalogue()
	const file = "slides/1-intro.md"

	t.Run("max exceeded errors at the offending heading", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: limited",
			"---",
			"",
			"# columns",
			"a",
			"# columns",
			"b",
			"# columns",
			"c",
		), cat)
		wantParseError(t, err, file, 9, `section "columns": at most 2 allowed, found 3`)
	})

	t.Run("min unmet errors at the template line", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: needs",
			"---",
			"",
			"# columns",
		), cat)
		wantParseError(t, err, file, 2, `section "columns": requires at least 2, found 1`)
	})

	t.Run("within min and max parses", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: needs",
			"---",
			"",
			"# columns",
			"a",
			"# columns",
			"b",
		), cat)
		if len(slide.Sections) != 2 {
			t.Fatalf("len(Sections) = %d, want 2", len(slide.Sections))
		}
	})

	t.Run("max <= 0 is unbounded", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"a",
			"# columns",
			"b",
			"# columns",
			"c",
		), cat)
		if len(slide.Sections) != 3 {
			t.Fatalf("len(Sections) = %d, want 3 (unbounded)", len(slide.Sections))
		}
	})
}

// testParseSectionFence covers the ``` section-frontmatter fence: where it may
// appear, its language-tag rule, and when template: is required.
func testParseSectionFence(t *testing.T) {
	cat := testCatalogue()
	const file = "slides/1-intro.md"

	t.Run("template required at the fence when >1 accepted", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# gallery",
			"```",
			"x: 1",
			"```",
		), cat)
		wantParseErrorPath(t, err, file, 6, "gallery[0]",
			`section "gallery": a template: is required when the section accepts more than one template`)
	})

	t.Run("template required at the heading when >1 accepted and no fence", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# gallery",
		), cat)
		wantParseErrorPath(t, err, file, 5, "gallery[0]",
			`section "gallery": a template: is required when the section accepts more than one template`)
	})

	t.Run("template optional with exactly one accepted (fence present)", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"```",
			"width: 2",
			"```",
		), cat)
		got := slide.Sections[0]
		if got.Template != "column" {
			t.Errorf("Template = %q, want %q (the single accepted template)", got.Template, "column")
		}
		if got.FenceLine != 6 {
			t.Errorf("FenceLine = %d, want 6", got.FenceLine)
		}
		if got.Frontmatter["width"] != 2 {
			t.Errorf("Frontmatter[width] = %v (%T), want 2", got.Frontmatter["width"], got.Frontmatter["width"])
		}
	})

	t.Run("template optional with exactly one accepted (no fence)", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"body",
		), cat)
		if got := slide.Sections[0].Template; got != "column" {
			t.Errorf("Template = %q, want %q (the single accepted template)", got, "column")
		}
	})

	t.Run("language tag on the fence errors at the fence line", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"```yaml",
			"width: 2",
			"```",
		), cat)
		wantParseErrorPath(t, err, file, 6, "columns[0]", `section frontmatter fence must not have a language tag ("yaml")`)
	})

	t.Run("fence not directly after the heading errors at the fence line", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"",
			"```",
			"width: 2",
			"```",
		), cat)
		wantParseErrorPath(t, err, file, 7, "columns[0]", "frontmatter fence is only allowed immediately after a section heading")
	})
}

// testParseNotes covers the reserved `# notes` section: position, uniqueness,
// reserved-name rejection and exclusion from repeat limits.
func testParseNotes(t *testing.T) {
	cat := testCatalogue()
	const file = "slides/1-intro.md"

	t.Run("notes followed by another section errors at the later heading", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# notes",
			"note body",
			"# columns",
			"more",
		), cat)
		wantParseError(t, err, file, 7, `"notes" section must be the last section on the slide`)
	})

	t.Run("duplicate notes errors at the second heading", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# notes",
			"first",
			"# notes",
			"second",
		), cat)
		wantParseError(t, err, file, 7, `duplicate "notes" section`)
	})

	t.Run("notes as a declared section name is reserved", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: badnotes",
			"---",
			"",
			"# notes",
			"body",
		), cat)
		wantParseError(t, err, file, 5, `"notes" is reserved and cannot be declared as a section name`)
	})

	t.Run("notes as a section template is reserved", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: content",
			"---",
			"",
			"# columns",
			"```",
			"template: notes",
			"```",
		), cat)
		wantParseErrorPath(t, err, file, 7, "columns[0]", `"notes" is reserved and cannot be used as a section template`)
	})

	t.Run("notes do not count toward a max", func(t *testing.T) {
		slide := parseOK(t, file, src(
			"---",
			"template: once",
			"---",
			"",
			"# columns",
			"a",
			"# notes",
			"b",
		), cat)
		if len(slide.Sections) != 1 {
			t.Fatalf("len(Sections) = %d, want 1 (notes excluded)", len(slide.Sections))
		}
		if slide.Notes == nil || slide.Notes.HeadingLine != 7 {
			t.Fatalf("Notes = %+v, want the section at line 7", slide.Notes)
		}
	})

	t.Run("notes do not satisfy a min", func(t *testing.T) {
		_, err := Parse(file, src(
			"---",
			"template: needs",
			"---",
			"",
			"# columns",
			"a",
			"# notes",
			"b",
		), cat)
		wantParseError(t, err, file, 2, `section "columns": requires at least 2, found 1`)
	})
}
