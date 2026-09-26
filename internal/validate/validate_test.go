package validate

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/really-knows-ai/kalide/internal/template"
)

// TestValidate covers the one author-facing error shape (ValidationError /
// Format) and the fail-fast, deterministic whole-deck walk in its fixed order:
// config -> filenames -> slides -> links. Every deck is an in-memory
// fstest.MapFS tree, so the suite touches neither the disk nor the network.
func TestValidate(t *testing.T) {
	t.Run("Format", testFormat)
	t.Run("ordering", testOrdering)
	t.Run("determinism", testDeterminism)
	t.Run("nesting depth", testNestingDepth)
	t.Run("links", testLinks)
	t.Run("valid deck", testValidDeck)
	t.Run("body rules", testBodyRules)
}

// testFormat pins the exact rendered text of ValidationError through Format,
// including the specification's worked example, and the Error/New helpers.
func testFormat(t *testing.T) {
	checkFormat(t, "spec example with line, nested path and fix",
		ValidationError{File: "slides/3-team.md", Line: 12, Path: []string{"column[1]", "people[0]", "name"}, What: "required", Fix: "add a name: value"},
		"slides/3-team.md:12 › column[1] › people[0] › name: required — add a name: value")

	checkFormat(t, "line omitted when unknown",
		ValidationError{File: "slides/3-team.md", Path: []string{"name"}, What: "required", Fix: "add a name: value"},
		"slides/3-team.md › name: required — add a name: value")

	checkFormat(t, "path omitted when empty",
		ValidationError{File: "kalide.yaml", Line: 4, What: `missing required key "title"`},
		`kalide.yaml:4: missing required key "title"`)

	checkFormat(t, "fix clause omitted when empty",
		ValidationError{File: "slides/1-a.md", Line: 3, Path: []string{"body"}, What: "disallowed: the body is not allowed"},
		"slides/1-a.md:3 › body: disallowed: the body is not allowed")

	checkFormat(t, "no line and no path",
		ValidationError{File: "kalide.yaml", What: "boom"},
		"kalide.yaml: boom")

	t.Run("New copies the path", func(t *testing.T) {
		segments := []string{"one", "two"}
		e := New("f.md", 3, segments, "what", "")
		segments[0] = "mutated"
		if got := Format(e); got != "f.md:3 › one › two: what" {
			t.Fatalf("Format(New(...)) = %q, want %q", got, "f.md:3 › one › two: what")
		}
	})
}

// checkFormat asserts Format and Error render exactly want.
func checkFormat(t *testing.T, name string, e ValidationError, want string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if got := Format(e); got != want {
			t.Fatalf("Format() =\n  %q\nwant\n  %q", got, want)
		}
		if got := e.Error(); got != want {
			t.Fatalf("Error() = %q, want Format() = %q", got, want)
		}
	})
}

// testOrdering proves the validator reports only the earliest fault in the fixed
// order config -> filenames -> slides -> links, and the earliest slide in
// number/letter order.
func testOrdering(t *testing.T) {
	reg := mustBuiltins(t)

	t.Run("config beats filenames and slides", func(t *testing.T) {
		wantError(t, reg, deckFiles("kalide.yaml", "navigation: diagonal\n", "slides/notaslide.md", "not a slide\n", "slides/1-a.md", "---\ntemplate: content\n---\n"),
			`kalide.yaml:1: key "navigation": unknown navigation mode "diagonal" (valid values: default, linear, grid)`)
	})

	t.Run("filenames beat slide contents", func(t *testing.T) {
		wantError(t, reg, deckFiles("kalide.yaml", "title: T\n", "slides/notaslide.md", "not a slide\n", "slides/1-a.md", "---\ntemplate: content\n---\n"),
			"slides/notaslide.md: filename must be <number>[letter]-<label>.md")
	})

	// A content slide with a missing required heading and an unknown body link:
	// the step-3 field error must win over the step-4 link error.
	t.Run("slides beat links", func(t *testing.T) {
		wantError(t, reg, deckFiles("kalide.yaml", "title: T\n", "slides/1-a.md", "---\ntemplate: content\n---\n\nSee [x](#nope).\n\n# columns\n\nA\n\n# columns\n\nB\n"),
			`slides/1-a.md › heading: required: field "heading" is required but missing — add a heading: value`)
	})

	t.Run("horizontal slide beats its vertical slide", func(t *testing.T) {
		preg := mustRegistry(t, plainTemplate())
		wantError(t, preg, deckFiles("kalide.yaml", "title: T\n", "slides/1-first.md", "---\ntemplate: plain\n---\n\nSee [x](#zzz).\n", "slides/1a-second.md", "---\ntemplate: plain\n---\n\nSee [y](#yyy).\n"),
			`slides/1-first.md:5: unknown link label "zzz" — use the label of one of the deck's slides`)
	})
}

// testDeterminism asserts repeated runs over the same invalid deck yield the
// same single error.
func testDeterminism(t *testing.T) {
	reg := mustBuiltins(t)
	files := deckFiles("kalide.yaml", "title: T\n", "slides/1-a.md", "---\ntemplate: content\n---\n\n# columns\n\nA\n\n# columns\n\nB\n")
	want := `slides/1-a.md › heading: required: field "heading" is required but missing — add a heading: value`

	first := ""
	for i := 0; i < 5; i++ {
		verr, invalid := Validate(mapDeck(files), reg, exampleThemeRegistry())
		if !invalid {
			t.Fatalf("run %d: invalid = false, want true", i)
		}
		got := Format(verr)
		if got != want {
			t.Fatalf("run %d: error = %q, want %q", i, got, want)
		}
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatalf("run %d: error = %q, previous = %q", i, got, first)
		}
	}
}

// testNestingDepth proves the field path is tracked uniformly through a
// section-template-as-type value and a list at depth: a missing required field
// reports its full path.
func testNestingDepth(t *testing.T) {
	person := &template.Template{Name: "person", Usage: template.UsageSection, Fields: []template.Field{{Name: "name", Type: template.FieldText, Required: true}}}
	mid := &template.Template{Name: "mid", Usage: template.UsageSection, Fields: []template.Field{{Name: "people", Type: template.FieldList, Item: &template.Field{Type: template.FieldSectionTemplate, SectionTemplate: "person"}}}}
	deep := &template.Template{Name: "deep", Usage: template.UsageSlide, Fields: []template.Field{{Name: "block", Type: template.FieldSectionTemplate, SectionTemplate: "mid"}}}

	wantError(t, mustRegistry(t, deep, mid, person), deckFiles("kalide.yaml", "title: T\n", "slides/1-deep.md", "---\ntemplate: deep\nblock:\n  people:\n    - {}\n---\n"),
		`slides/1-deep.md › block › people › [0] › name: required: field "name" is required but missing — add a name: value`)
}

// testLinks covers the step-4 link pass: unknown #label with a closest-match
// suggestion, a bad scheme, and links in both `link` fields and inline `text`
// fields.
func testLinks(t *testing.T) {
	t.Run("unknown label with suggestion in a body", func(t *testing.T) {
		wantError(t, mustRegistry(t, plainTemplate()), deckFiles("kalide.yaml", "title: T\n", "slides/1-overview.md", "---\ntemplate: plain\n---\n\nSee [the plan](#overveiw).\n"),
			`slides/1-overview.md:5: unknown link label "overveiw" — did you mean "overview"?`)
	})

	t.Run("bad scheme in a body", func(t *testing.T) {
		wantError(t, mustRegistry(t, plainTemplate()), deckFiles("kalide.yaml", "title: T\n", "slides/1-bad.md", "---\ntemplate: plain\n---\n\nSee [ftp](ftp://example.com).\n"),
			`slides/1-bad.md:5: link destination "ftp://example.com" uses the unsupported "ftp" scheme — use a #label link or an http(s) URL, for example [text](#label) or [text](https://example.com)`)
	})

	t.Run("first link in a body is reported by line", func(t *testing.T) {
		wantError(t, mustRegistry(t, plainTemplate()), deckFiles("kalide.yaml", "title: T\n", "slides/1-a.md", "---\ntemplate: plain\n---\n\nSee [x](#zzz).\n\nAnd [y](#yyy).\n"),
			`slides/1-a.md:5: unknown link label "zzz" — use the label of one of the deck's slides`)
	})

	t.Run("link field and inline text field", func(t *testing.T) {
		refs := &template.Template{Name: "refs", Usage: template.UsageSlide, Fields: []template.Field{{Name: "target", Type: template.FieldLink}, {Name: "note", Type: template.FieldText}}}

		// CheckValues collects links in field declaration order, so the link
		// field is reported before the inline link in the text field.
		wantError(t, mustRegistry(t, refs), deckFiles("kalide.yaml", "title: T\n", "slides/1-refs.md", "---\ntemplate: refs\ntarget: \"#nope\"\nnote: \"see [x](#nope2)\"\n---\n"),
			`slides/1-refs.md › target: unknown link label "nope" — use the label of one of the deck's slides`)

		// With the link field valid, the inline text link is the first offender.
		wantError(t, mustRegistry(t, refs), deckFiles("kalide.yaml", "title: T\n", "slides/1-refs.md", "---\ntemplate: refs\ntarget: \"https://example.com\"\nnote: \"see [x](#nope2)\"\n---\n"),
			`slides/1-refs.md › note: unknown link label "nope2" — use the label of one of the deck's slides`)
	})
}

// testValidDeck asserts a sound deck returns the zero ValidationError and false,
// including a known inter-slide #label link.
func testValidDeck(t *testing.T) {
	reg := mustRegistry(t, plainTemplate())
	verr, invalid := Validate(mapDeck(deckFiles("kalide.yaml", "title: T\n", "slides/1-overview.md", "---\ntemplate: plain\n---\n", "slides/2-detail.md", "---\ntemplate: plain\n---\n\nBack to [overview](#overview).\n")), reg, exampleThemeRegistry())
	if invalid {
		t.Fatalf("Validate() invalid = true with %q, want a valid deck", Format(verr))
	}
	if !reflect.DeepEqual(verr, ValidationError{}) {
		t.Fatalf("Validate() error = %#v, want the zero ValidationError", verr)
	}
}

// testBodyRules covers a required body missing and each size rule, asserting the
// body violation is the first (and only) error and carries its body line.
func testBodyRules(t *testing.T) {
	t.Run("required body missing", func(t *testing.T) {
		needsBody := &template.Template{Name: "needsbody", Usage: template.UsageSlide, Body: template.BodyRule{Mode: template.BodyRequired}}
		wantError(t, mustRegistry(t, needsBody), deckFiles("kalide.yaml", "title: T\n", "slides/1-empty.md", "---\ntemplate: needsbody\n---\n"),
			"slides/1-empty.md:4 › body: required: the body is required but missing — add body text after the frontmatter")
	})

	t.Run("max_words exceeded", func(t *testing.T) {
		short := &template.Template{Name: "short", Usage: template.UsageSlide, Body: template.BodyRule{Mode: template.BodyOptional, MaxWords: 3}}
		wantError(t, mustRegistry(t, short), deckFiles("kalide.yaml", "title: T\n", "slides/1-short.md", "---\ntemplate: short\n---\n\none two three four\n"),
			"slides/1-short.md:4 › body: max_words: the body is 4 words, maximum is 3 — shorten the body to at most 3 words")
	})

	t.Run("max_paragraphs exceeded", func(t *testing.T) {
		onepara := &template.Template{Name: "onepara", Usage: template.UsageSlide, Body: template.BodyRule{Mode: template.BodyOptional, MaxParagraphs: 1}}
		wantError(t, mustRegistry(t, onepara), deckFiles("kalide.yaml", "title: T\n", "slides/1-para.md", "---\ntemplate: onepara\n---\n\nfirst paragraph.\n\nsecond paragraph.\n"),
			"slides/1-para.md:4 › body: max_paragraphs: the body has 2 paragraphs, maximum is 1 — shorten the body to at most 1 paragraphs")
	})
}

// plainTemplate is a slide template with no fields, no sections and an optional
// body: a deck of plain slides passes steps 1-3 and exercises the link pass
// alone.
func plainTemplate() *template.Template {
	return &template.Template{Name: "plain", Usage: template.UsageSlide, Body: template.BodyRule{Mode: template.BodyOptional}}
}

// deckFiles builds an ordered name/source pairing for one in-memory deck. Pairs
// are (path, source) repeated; an odd trailing argument is ignored.
func deckFiles(pairs ...string) map[string]string {
	files := make(map[string]string, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		files[pairs[i]] = pairs[i+1]
	}
	return files
}

// mapDeck builds an in-memory fstest.MapFS deck from path -> source.
func mapDeck(files map[string]string) fstest.MapFS {
	mfs := make(fstest.MapFS, len(files))
	for name, data := range files {
		mfs[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return mfs
}

// mustBuiltins returns a fixture template registry standing in for the
// deleted Go-authored builtins: it loads a small in-memory templates/
// library (title, content, column) through template.LoadLibrary +
// template.NewRegistryFromLibrary, mirroring the shapes
// internal/validate/integration_test.go's builtinTitleExample and
// builtinContentExample exercise (title/subtitle/date; heading/layout/
// metric/show_metric/as_of composing "columns" of "column"; a 2-paragraph
// body limit on content), so existing test expectations (heading required,
// max_paragraphs 2) still hold.
func mustBuiltins(t *testing.T) *template.Registry {
	t.Helper()
	fsys := builtinsFixtureFS()
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	return reg
}

// builtinsFixtureFS is the in-memory templates/ library mustBuiltins loads:
// title, content (composing "columns" of "column") and column, with example
// content matching the shapes internal/validate/integration_test.go's
// builtinTitleExample/builtinContentExample constants exercise end to end.
func builtinsFixtureFS() fstest.MapFS {
	const titleManifest = `description: Title slide
fields:
  - name: title
    type: text
    required: true
  - name: subtitle
    type: text
  - name: date
    type: date
    formats: [long, short]
    default_format: long
body:
  mode: disallowed
`
	const contentManifest = `description: Content slide with composed columns
fields:
  - name: heading
    type: text
    required: true
  - name: layout
    type: text
  - name: metric
    type: number
    formats: [compact]
    default_format: compact
  - name: show_metric
    type: boolean
  - name: as_of
    type: date
    formats: [long]
    default_format: long
sections:
  - name: columns
    accepted: [column]
body:
  mode: optional
  max_paragraphs: 2
`
	const columnManifest = `description: A single column section
fields:
  - name: title
    type: text
body:
  mode: optional
`
	const titleExample = `---
template: title
title: Quarterly Business Review
subtitle: Performance, outlook and priorities
date: 2026-09-25
date_format: long
---
# notes
Greet the audience, then hand over to the presenters.
`
	const contentExample = `---
template: content
heading: Where the growth is coming from
layout: columns
metric: 1250000
metric_format: compact
show_metric: true
as_of: 2026-09-25
as_of_format: long
---

Revenue is up across every region, led by services.

# columns
` + "```" + `
template: column
title: Revenue
` + "```" + `

Recurring revenue grew 18% year over year.

# columns
` + "```" + `
template: column
title: New customers
` + "```" + `

We added 1,250 new logos in the quarter.

# notes
Pause on the metric so the number lands.
`
	const columnExample = "```\ntitle: Sample\n```\n"

	return fstest.MapFS{
		"templates/library.yaml":                     {Data: []byte("name: builtins-fixture\nformat: 1\n")},
		"templates/slides/title/template.yaml":       {Data: []byte(titleManifest)},
		"templates/slides/title/layout.html.tmpl":    {Data: []byte("<section>{{.title}}</section>")},
		"templates/slides/title/example.md":          {Data: []byte(titleExample)},
		"templates/slides/content/template.yaml":     {Data: []byte(contentManifest)},
		"templates/slides/content/layout.html.tmpl":  {Data: []byte("<section>{{.heading}}</section>")},
		"templates/slides/content/example.md":        {Data: []byte(contentExample)},
		"templates/sections/column/template.yaml":    {Data: []byte(columnManifest)},
		"templates/sections/column/layout.html.tmpl": {Data: []byte("<div>{{.title}}</div>")},
		"templates/sections/column/example.md":       {Data: []byte(columnExample)},
	}
}

// mustRegistry registers a purpose-built registry for tests that need templates
// beyond the built-ins.
func mustRegistry(t *testing.T, templates ...*template.Template) *template.Registry {
	t.Helper()
	reg := template.NewRegistry(nil)
	for _, tmpl := range templates {
		if err := reg.Register(tmpl); err != nil {
			t.Fatalf("register %q: %v", tmpl.Name, err)
		}
	}
	return reg
}

// wantError asserts the deck is invalid and Format renders exactly want.
func wantError(t *testing.T, reg *template.Registry, files map[string]string, want string) {
	t.Helper()
	verr, invalid := Validate(mapDeck(files), reg, exampleThemeRegistry())
	if !invalid {
		t.Fatalf("Validate() invalid = false, want error %q", want)
	}
	if got := Format(verr); got != want {
		t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
	}
}
