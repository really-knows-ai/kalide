package template

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

// This file is the unit-test deliverable for phase-04.task-8: the Registry
// build-time checks and the read-only slide.Catalogue surface.
//
// It proves each build-time rejection with one failing fixture registry per
// rejection — a reference cycle, a section accepting a slide-usage template, a
// required-body template used as a field type, the reserved names (`body`,
// `notes`, the `_format` suffix), and structured example data that fails the
// schema — each asserting the specific error the registry reports.
//
// It also proves the built-in templates all load and that each one's STRUCTURED
// example data (frontmatter/field values plus declared section instances)
// passes the build-time checks. No Markdown or slide source is parsed here:
// end-to-end example validation through the parser pipeline is phase 5
// (validate.ValidateBuiltinExamples).
//
// Finally it exercises the four Catalogue methods — LookupSlideTemplate,
// TemplateNames, SectionNames and SectionDecl — plus duplicate-name
// registration and Validate's fixed, deterministic check order.

// regSlide returns a slide-usage fixture. Registry validation reads only the
// fields its checks consume, so a fixture carries no layout or example content
// unless a case sets one.
func regSlide(name string, fields ...Field) *Template {
	return &Template{Name: name, Usage: UsageSlide, Fields: fields}
}

// regSection returns a section-usage fixture.
func regSection(name string, fields ...Field) *Template {
	return &Template{Name: name, Usage: UsageSection, Fields: fields}
}

// This block (through demoLibraryFS) is the unit-test deliverable for
// plan.phase-02.task-6 and plan.phase-02.task-7: it proves parseManifest and
// NewRegistryFromLibrary — the library-driven replacement for the Go-authored
// Builtins/regSlide/regSection fixtures above — decode the full schema
// vocabulary (field types, field rules, number/date formats, variants,
// composition/repeats, section frontmatter, body rules, kind-by-directory)
// and reject the same build-time problems the registry always has, this time
// surfaced through a *LibraryError positioned at the offending template.yaml.

// manifestLibraryTemplate builds a minimal *LibraryTemplate carrying just
// enough for parseManifest: a name, a kind (which decides Usage) and the raw
// manifest bytes. It carries no layout/example content — callers that need
// NewRegistryFromLibrary or LoadLibrary build a fuller fixture themselves.
func manifestLibraryTemplate(name string, kind TemplateKind, manifestYAML string) *LibraryTemplate {
	return &LibraryTemplate{
		Name:          name,
		Kind:          kind,
		ManifestPath:  string(kind) + "s/" + name + "/" + ManifestFile,
		ManifestBytes: []byte(manifestYAML),
	}
}

// mustParseManifest parses manifestYAML for a template named name of kind
// kind and fails the test on error. An optional defs supplies the fixture's
// parsed LibraryDefinitions, so the manifest's type:/include:/@group names
// resolve against its owning layer; a caller whose fixture declares no
// definitions omits it (nil).
func mustParseManifest(t *testing.T, name string, kind TemplateKind, manifestYAML string, defs ...*LibraryDefinitions) *Template {
	t.Helper()
	var defsArg *LibraryDefinitions
	if len(defs) > 0 {
		defsArg = defs[0]
	}
	def, err := parseManifest(manifestLibraryTemplate(name, kind, manifestYAML), defsArg)
	if err != nil {
		t.Fatalf("parseManifest(%s) error = %v, want nil", name, err)
	}
	return def
}

// demoLibraryFS returns a full, valid templates/ library (unrooted, so a
// caller wraps it in rootedFS before LoadLibrary) exercising the whole
// manifest vocabulary in one slide template (deck) composing a section
// template (column): every field type, field rules (max_length, min/max,
// min_items/max_items), number and date formats with a default, an enum
// variant with a default, a section-template field type, a declared section
// with min/max repeats (template-composition), and a body rule with
// max_words — plus a section template (column) with its own fields and a
// disallowed body, so a section-template field type never targets a
// required-body template. Its example.md declares two "columns" section
// instances with their own YAML frontmatter (section frontmatter, slide
// sections).
func demoLibraryFS() fstest.MapFS {
	const deckManifest = `description: Full-featured deck slide
fields:
  - name: title
    type: text
    required: true
    max_length: 50
  - name: count
    type: number
    min: 0
    max: 100
    formats: [compact, exact]
    default_format: compact
  - name: when
    type: date
    formats: [long, short]
    default_format: long
    min_date: "2020-01-01"
    max_date: "2030-12-31"
  - name: active
    type: boolean
  - name: mode
    type: enum
    variants: [a, b]
    default: a
  - name: img
    type: image
  - name: link
    type: link
  - name: tags
    type: list
    min_items: 1
    max_items: 3
    item:
      name: item
      type: text
  - name: block
    type: section-template
    section_template: column
sections:
  - name: columns
    accepted: [column]
    min: 1
    max: 3
body:
  mode: optional
  max_words: 50
`
	const columnManifest = `description: A single column section
fields:
  - name: label
    type: text
    max_length: 20
  - name: value
    type: number
body:
  mode: disallowed
`
	const deckExample = "---\n" +
		"title: Hello World\n" +
		"count: 42\n" +
		"count_format: exact\n" +
		"when: 2025-06-15\n" +
		"when_format: short\n" +
		"active: true\n" +
		"mode: a\n" +
		"img: assets/pic.png\n" +
		"link: https://example.com\n" +
		"tags:\n" +
		"  - one\n" +
		"  - two\n" +
		"block:\n" +
		"  label: Nested\n" +
		"  value: 5\n" +
		"---\n" +
		"Some body text here.\n" +
		"\n" +
		"# columns\n" +
		"```\n" +
		"label: Column A\n" +
		"value: 1\n" +
		"```\n" +
		"\n" +
		"# columns\n" +
		"```\n" +
		"label: Column B\n" +
		"value: 2\n" +
		"```\n"
	const columnExample = "```\n" +
		"label: Sample\n" +
		"value: 7\n" +
		"```\n"

	return fstest.MapFS{
		"library.yaml":                     {Data: []byte("name: fulldemo\nformat: 1\n")},
		"slides/deck/template.yaml":        {Data: []byte(deckManifest)},
		"slides/deck/layout.html.tmpl":     {Data: []byte("<section>{{.title}}</section>")},
		"slides/deck/example.md":           {Data: []byte(deckExample)},
		"sections/column/template.yaml":    {Data: []byte(columnManifest)},
		"sections/column/layout.html.tmpl": {Data: []byte("<div>{{.label}}</div>")},
		"sections/column/example.md":       {Data: []byte(columnExample)},
	}
}

// buildAndValidate mirrors the built-ins loader: register every fixture
// template in order, then run the whole-registry checks once. It returns the
// first error either step reports, so a fixture whose definition is rejected at
// registration and one rejected only by Validate are both exercised.
func buildAndValidate(ts ...*Template) error {
	r := NewRegistry(nil)
	for _, t := range ts {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return r.Validate()
}

// chainSections returns a composition chain of section templates named names,
// each declaring one "slot" section that accepts the next name; the last
// accepts nothing. It builds the fixture for the composition-depth bound
// (requirements.constraint.section-depth-limit).
func chainSections(names ...string) []*Template {
	ts := make([]*Template, len(names))
	for i, name := range names {
		ts[i] = regSection(name)
		if i+1 < len(names) {
			ts[i].Sections = []SectionDecl{{Name: "slot", Accepted: []string{names[i+1]}}}
		}
	}
	return ts
}

// assertStringSlice compares a []string against an expected literal, reporting
// both the length and the elements on failure.
func assertStringSlice(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// fixtureLibraryFS returns a small library (title, content, column) that
// stands in for the deleted Go-authored builtins: it exercises the same
// shapes TestRegistry asserted against Builtins() — a plain slide with no
// sections (title), a slide composing a repeated section (content, 2..4
// "columns" of "column"), and the section template itself (column) — so this
// test proves the same registry behaviour now sourced from a loaded library.
func fixtureLibraryFS() fstest.MapFS {
	const contentManifest = `description: Slide with column sections
fields:
  - name: title
    type: text
    required: true
sections:
  - name: columns
    accepted: [column]
    min: 2
    max: 4
`
	const columnManifest = `description: A single column section
fields:
  - name: label
    type: text
`
	const contentExample = "---\n" +
		"template: content\n" +
		"title: Quarterly Results\n" +
		"---\n" +
		"\n" +
		"# columns\n" +
		"```\n" +
		"label: Column A\n" +
		"```\n" +
		"\n" +
		"# columns\n" +
		"```\n" +
		"label: Column B\n" +
		"```\n"

	return fstest.MapFS{
		"library.yaml":                     {Data: []byte("name: fixture\nformat: 1\n")},
		"slides/title/template.yaml":       {Data: []byte("description: Title slide\nfields:\n  - name: title\n    type: text\n    required: true\n")},
		"slides/title/layout.html.tmpl":    {Data: []byte("<section>{{.title}}</section>")},
		"slides/title/example.md":          {Data: []byte("---\ntemplate: title\ntitle: Quarterly Business Review\n---\n")},
		"slides/content/template.yaml":     {Data: []byte(contentManifest)},
		"slides/content/layout.html.tmpl":  {Data: []byte("<section>{{.title}}</section>")},
		"slides/content/example.md":        {Data: []byte(contentExample)},
		"sections/column/template.yaml":    {Data: []byte(columnManifest)},
		"sections/column/layout.html.tmpl": {Data: []byte("<div>{{.label}}</div>")},
		"sections/column/example.md":       {Data: []byte("```\nlabel: Sample\n```\n")},
	}
}

// mustFixtureRegistry loads fixtureLibraryFS through the same LoadLibrary +
// NewRegistryFromLibrary path a project's templates/ takes.
func mustFixtureRegistry(t *testing.T) *Registry {
	t.Helper()
	lib, err := LoadLibrary(rootedFS(fixtureLibraryFS()), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil", err)
	}
	reg, err := NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("NewRegistryFromLibrary() error = %v, want nil", err)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	return reg
}

// TestRegistry proves a library-loaded registry's templates load and
// validate (structured example data is checked separately, end to end,
// through checkLibraryExamples during LoadLibrary — reg.Templates() carries
// only Example.Markdown from a library, not the Frontmatter/Sections a
// Go-authored builtin populated), and the four Catalogue methods behave.
func TestRegistry(t *testing.T) {
	t.Run("fixture library loads, validates and carries content", func(t *testing.T) {
		r := mustFixtureRegistry(t)

		want := []string{"column", "content", "title"}
		assertStringSlice(t, "TemplateNames()", r.TemplateNames(), want)

		for _, tmpl := range r.Templates() {
			if strings.TrimSpace(tmpl.Layout.Name) == "" {
				t.Errorf("template %q: Layout.Name is empty, want the manifest layout path", tmpl.Name)
			}
			if strings.TrimSpace(tmpl.Layout.Text) == "" {
				t.Errorf("template %q: Layout.Text is empty, want the parsed layout", tmpl.Name)
			}
			if strings.TrimSpace(tmpl.Example.Markdown) == "" {
				t.Errorf("template %q: Example.Markdown is empty, want the library's example slide", tmpl.Name)
			}
		}
	})

	t.Run("fixture usages and example shapes", func(t *testing.T) {
		r := mustFixtureRegistry(t)

		title, ok := r.Lookup("title")
		if !ok {
			t.Fatal("Lookup(title) = not found")
		}
		if title.Usage != UsageSlide {
			t.Errorf("title.Usage = %q, want %q", title.Usage, UsageSlide)
		}
		if !strings.Contains(title.Example.Markdown, "Quarterly Business Review") {
			t.Errorf("title example markdown = %q, want it to contain the structured example value", title.Example.Markdown)
		}

		content, ok := r.Lookup("content")
		if !ok {
			t.Fatal("Lookup(content) = not found")
		}
		if content.Usage != UsageSlide {
			t.Errorf("content.Usage = %q, want %q", content.Usage, UsageSlide)
		}
		if got := strings.Count(content.Example.Markdown, "# columns"); got != 2 {
			t.Errorf("content example markdown has %d columns sections, want 2", got)
		}

		column, ok := r.Lookup("column")
		if !ok {
			t.Fatal("Lookup(column) = not found")
		}
		if column.Usage != UsageSection {
			t.Errorf("column.Usage = %q, want %q", column.Usage, UsageSection)
		}
	})

	t.Run("LookupSlideTemplate reports usage and internal", func(t *testing.T) {
		r := mustFixtureRegistry(t)
		tests := []struct {
			name         string
			wantUsage    string
			wantInternal bool
			wantOK       bool
		}{
			{"title", "slide", false, true},
			{"content", "slide", false, true},
			{"column", "section", false, true},
			{"missing", "", false, false},
		}
		for _, tt := range tests {
			usage, internal, ok := r.LookupSlideTemplate(tt.name)
			if usage != tt.wantUsage || internal != tt.wantInternal || ok != tt.wantOK {
				t.Errorf("LookupSlideTemplate(%q) = (%q, %v, %v), want (%q, %v, %v)",
					tt.name, usage, internal, ok, tt.wantUsage, tt.wantInternal, tt.wantOK)
			}
		}
	})

	// internal-templates: an internal template (a manifest's `internal: true`)
	// stays resolvable through the registry's Lookup and through the two
	// resolution paths that do not consult the Catalogue surface — a
	// `section`-helper call and a `section_template` field type — while
	// LookupSlideTemplate reports the Internal flag beside usage and ok, so a
	// caller can distinguish an internal template from an unknown name (which
	// reports ("", false, false)). Refusing an internal template as a Markdown
	// `template:` target is the slide parser's rule (internal/slide), not the
	// registry's, so it is exercised there.
	t.Run("internal templates stay resolvable but report internal", func(t *testing.T) {
		hidden := mustParseManifest(t, "hidden", KindSection, "internal: true\n")
		if !hidden.Internal {
			t.Fatalf("hidden.Internal = false, want true from the `internal: true` manifest")
		}

		// A `section`-helper call to the internal template resolves at
		// Validate, and a section_template field naming it resolves through
		// checkFieldTypes: neither consults LookupSlideTemplate, so an internal
		// template stays a valid section-helper target and field type.
		helperHost := regLayout("helperhost", `{{ section "hidden" }}`)
		fieldHost := regSlide("fieldhost", Field{Name: "block", Type: FieldSectionTemplate, SectionTemplate: "hidden"})

		r := NewRegistry(nil)
		for _, tmpl := range []*Template{hidden, helperHost, fieldHost} {
			if err := r.Register(tmpl); err != nil {
				t.Fatalf("Register(%q) error = %v, want nil", tmpl.Name, err)
			}
		}
		if err := r.Validate(); err != nil {
			t.Fatalf("Validate() error = %v, want nil (an internal template stays resolvable)", err)
		}

		// Lookup still resolves the internal template.
		if got, ok := r.Lookup("hidden"); !ok || got != hidden {
			t.Errorf("Lookup(hidden) = (%v, %v), want the internal template and true", got, ok)
		}

		// The Catalogue surface carries the Internal flag beside usage and ok.
		usage, internal, ok := r.LookupSlideTemplate("hidden")
		if usage != string(UsageSection) || !internal || !ok {
			t.Errorf("LookupSlideTemplate(hidden) = (%q, %v, %v), want (%q, true, true)", usage, internal, ok, UsageSection)
		}

		// An unknown name reports no usage, not internal, and not found.
		usage, internal, ok = r.LookupSlideTemplate("missing")
		if usage != "" || internal || ok {
			t.Errorf(`LookupSlideTemplate(missing) = (%q, %v, %v), want ("", false, false)`, usage, internal, ok)
		}
	})

	t.Run("TemplateNames is sorted and a fresh allocation", func(t *testing.T) {
		r := NewRegistry(nil)
		for _, name := range []string{"zeta", "alpha", "mid"} {
			if err := r.Register(regSlide(name)); err != nil {
				t.Fatalf("Register(%q) error = %v", name, err)
			}
		}
		assertStringSlice(t, "TemplateNames()", r.TemplateNames(), []string{"alpha", "mid", "zeta"})

		names := r.TemplateNames()
		names[0] = "mutated"
		if got := r.TemplateNames(); got[0] != "alpha" {
			t.Errorf("TemplateNames() after mutating a prior result = %v, want the registry unchanged", got)
		}
	})

	t.Run("SectionNames is declaration order, notes excluded", func(t *testing.T) {
		r := mustFixtureRegistry(t)
		assertStringSlice(t, `SectionNames("content")`, r.SectionNames("content"), []string{"columns"})

		// "column" declares no sections: no names, whether nil or empty.
		if got := r.SectionNames("column"); len(got) != 0 {
			t.Errorf(`SectionNames("column") = %v, want no names`, got)
		}
		if got := r.SectionNames("missing"); got != nil {
			t.Errorf(`SectionNames("missing") = %v, want nil`, got)
		}

		// Registration rejects a declared `notes` section, so no normally
		// built registry can hold one. Seed the impossible state to exercise
		// the contract that SectionNames never reports the reserved name.
		seeded := NewRegistry(nil)
		seeded.templates = map[string]*Template{
			"seeded": {
				Name:  "seeded",
				Usage: UsageSlide,
				Sections: []SectionDecl{
					{Name: "intro", Accepted: []string{"x"}},
					{Name: "notes", Accepted: []string{"x"}},
					{Name: "outro", Accepted: []string{"x"}},
				},
			},
		}
		assertStringSlice(t, `SectionNames("seeded")`, seeded.SectionNames("seeded"), []string{"intro", "outro"})
	})

	t.Run("SectionDecl returns accepted, min, max and a fresh copy", func(t *testing.T) {
		r := mustFixtureRegistry(t)

		accepted, min, max, ok := r.SectionDecl("content", "columns")
		if !ok {
			t.Fatal(`SectionDecl("content", "columns") ok = false, want true`)
		}
		assertStringSlice(t, "SectionDecl accepted", accepted, []string{"column"})
		if min != 2 || max != 4 {
			t.Errorf("SectionDecl min/max = %d/%d, want 2/4", min, max)
		}

		// The accepted slice is a fresh copy: mutating it does not corrupt
		// the registry.
		accepted[0] = "mutated"
		again, _, _, _ := r.SectionDecl("content", "columns")
		assertStringSlice(t, "SectionDecl accepted after mutation", again, []string{"column"})

		for _, tt := range []struct {
			tmpl, section string
		}{
			{"content", "missing"},
			{"missing", "columns"},
		} {
			if _, _, _, ok := r.SectionDecl(tt.tmpl, tt.section); ok {
				t.Errorf("SectionDecl(%q, %q) ok = true, want false", tt.tmpl, tt.section)
			}
		}
	})

	t.Run("library-driven registry: parseManifest + NewRegistryFromLibrary", func(t *testing.T) {
		lib, err := LoadLibrary(rootedFS(demoLibraryFS()), TemplatesDir)
		if err != nil {
			t.Fatalf("LoadLibrary() error = %v, want nil", err)
		}

		// kind-by-directory: slides/deck -> KindSlide/UsageSlide,
		// sections/column -> KindSection/UsageSection.
		deckLT, kind, ok := lib.TemplateByName("deck")
		if !ok || kind != KindSlide {
			t.Fatalf("TemplateByName(deck) = (%v, %q, %v), want (non-nil, %q, true)", deckLT, kind, ok, KindSlide)
		}
		colLT, kind, ok := lib.TemplateByName("column")
		if !ok || kind != KindSection {
			t.Fatalf("TemplateByName(column) = (%v, %q, %v), want (non-nil, %q, true)", colLT, kind, ok, KindSection)
		}
		deck, col := deckLT.Definition, colLT.Definition
		if deck == nil || col == nil {
			t.Fatal("checkLibraryBuild did not attach parsed Definitions")
		}
		if deck.Usage != UsageSlide {
			t.Errorf("deck.Usage = %q, want %q", deck.Usage, UsageSlide)
		}
		if col.Usage != UsageSection {
			t.Errorf("col.Usage = %q, want %q", col.Usage, UsageSection)
		}

		// fields/types and field rules: every declared field type round-trips
		// with its rules intact.
		byName := make(map[string]Field, len(deck.Fields))
		for _, f := range deck.Fields {
			byName[f.Name] = f
		}
		wantTypes := map[string]FieldType{
			"title": FieldText, "count": FieldNumber, "when": FieldDate,
			"active": FieldBoolean, "mode": FieldEnum, "img": FieldImage,
			"link": FieldLink, "tags": FieldList, "block": FieldSectionTemplate,
		}
		for name, wantType := range wantTypes {
			f, ok := byName[name]
			if !ok {
				t.Errorf("field %q not decoded", name)
				continue
			}
			if f.Type != wantType {
				t.Errorf("field %q Type = %q, want %q", name, f.Type, wantType)
			}
		}
		if !byName["title"].Required || byName["title"].MaxLength != 50 {
			t.Errorf("title field = %+v, want Required=true MaxLength=50", byName["title"])
		}
		if byName["count"].Min == nil || *byName["count"].Min != 0 || byName["count"].Max == nil || *byName["count"].Max != 100 {
			t.Errorf("count field min/max = %v/%v, want 0/100", byName["count"].Min, byName["count"].Max)
		}
		if got, want := byName["tags"].MinItems, 1; got != want {
			t.Errorf("tags.MinItems = %d, want %d", got, want)
		}
		if got, want := byName["tags"].MaxItems, 3; got != want {
			t.Errorf("tags.MaxItems = %d, want %d", got, want)
		}
		if byName["tags"].Item == nil || byName["tags"].Item.Type != FieldText {
			t.Errorf("tags.Item = %+v, want a text item", byName["tags"].Item)
		}
		if byName["block"].SectionTemplate != "column" {
			t.Errorf("block.SectionTemplate = %q, want %q", byName["block"].SectionTemplate, "column")
		}

		// number/date formats.
		if got, want := byName["count"].Formats, []string{"compact", "exact"}; !reflect.DeepEqual(got, want) {
			t.Errorf("count.Formats = %v, want %v", got, want)
		}
		if got, want := byName["count"].DefaultFormat, "compact"; got != want {
			t.Errorf("count.DefaultFormat = %q, want %q", got, want)
		}
		if got, want := byName["when"].Formats, []string{"long", "short"}; !reflect.DeepEqual(got, want) {
			t.Errorf("when.Formats = %v, want %v", got, want)
		}
		if got, want := byName["when"].MinDate, "2020-01-01"; got != want {
			t.Errorf("when.MinDate = %q, want %q", got, want)
		}
		if got, want := byName["when"].MaxDate, "2030-12-31"; got != want {
			t.Errorf("when.MaxDate = %q, want %q", got, want)
		}

		// variants (template-variants): an enum field's variants and default.
		if got, want := byName["mode"].Variants, []string{"a", "b"}; !reflect.DeepEqual(got, want) {
			t.Errorf("mode.Variants = %v, want %v", got, want)
		}
		if got, want := byName["mode"].Default, "a"; got != want {
			t.Errorf("mode.Default = %v, want %q", got, want)
		}

		// body rules: deck is optional with a word limit; column disallows a
		// body entirely (so it is a legal field-type target).
		if deck.Body.Mode != BodyOptional || deck.Body.MaxWords != 50 {
			t.Errorf("deck.Body = %+v, want optional/max_words=50", deck.Body)
		}
		if col.Body.Mode != BodyDisallowed {
			t.Errorf("col.Body.Mode = %q, want %q", col.Body.Mode, BodyDisallowed)
		}

		// template-composition/repeats: deck declares "columns" accepting
		// column, 1..3 times.
		if len(deck.Sections) != 1 {
			t.Fatalf("deck.Sections = %+v, want exactly one declared section", deck.Sections)
		}
		sd := deck.Sections[0]
		if sd.Name != "columns" || !reflect.DeepEqual(sd.Accepted, []string{"column"}) || sd.Min != 1 || sd.Max != 3 {
			t.Errorf("columns section = %+v, want name=columns accepted=[column] min=1 max=3", sd)
		}

		// The registry built over this library resolves both templates and
		// reports the right usage/kind through the slide.Catalogue surface.
		reg, err := NewRegistryFromLibrary(lib)
		if err != nil {
			t.Fatalf("NewRegistryFromLibrary() error = %v, want nil", err)
		}
		if err := reg.Validate(); err != nil {
			t.Fatalf("Validate() error = %v, want nil", err)
		}
		if usage, internal, ok := reg.LookupSlideTemplate("deck"); !ok || internal || usage != string(UsageSlide) {
			t.Errorf("LookupSlideTemplate(deck) = (%q, %v, %v), want (%q, false, true)", usage, internal, ok, UsageSlide)
		}
		if usage, internal, ok := reg.LookupSlideTemplate("column"); !ok || internal || usage != string(UsageSection) {
			t.Errorf("LookupSlideTemplate(column) = (%q, %v, %v), want (%q, false, true)", usage, internal, ok, UsageSection)
		}
		assertStringSlice(t, `SectionNames("deck")`, reg.SectionNames("deck"), []string{"columns"})

		// section frontmatter / slide sections: the example.md's two
		// "columns" instances validated end to end (checkLibraryExamples,
		// step 7) against the column template's own field schema — proven
		// by LoadLibrary having already returned no error above.
	})

	// parseManifestField accepts the nullable and coerce keys on any
	// descriptor and the inline `type: record` form as a list's `item:`: the
	// record's own `fields:` recurse through parseManifestFields, so a record
	// field may itself be a list whose item is a record (record-item-type).
	// Administering the isItem flag directly pins the two halves of the
	// contract the notes call out — a record is rejected as a field's own
	// `type:` (it is valid only as an item descriptor) and a record item
	// descriptor needs no `name`, while a non-record item still does.
	t.Run("parseManifestField accepts nullable, coerce and an inline record item", func(t *testing.T) {
		const manifestPath = "templates/slides/x/template.yaml"
		parseField := func(t *testing.T, src string, isItem bool) (*Field, error) {
			t.Helper()
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
				t.Fatalf("yaml.Unmarshal(%q) error = %v", src, err)
			}
			return parseManifestField(manifestPath, libraryDocumentMapping(&doc), isItem, nil)
		}

		t.Run("nullable and coerce on a field", func(t *testing.T) {
			f, err := parseField(t, "name: label\ntype: text\nnullable: true\ncoerce: true\n", false)
			if err != nil {
				t.Fatalf("parseManifestField() error = %v, want nil", err)
			}
			if f.Name != "label" || f.Type != FieldText {
				t.Errorf("field = %+v, want name=label type=text", f)
			}
			if !f.Nullable {
				t.Error("Nullable = false, want true")
			}
			if !f.Coerce {
				t.Error("Coerce = false, want true")
			}
		})

		t.Run("inline record item recurses its fields", func(t *testing.T) {
			const src = `type: record
fields:
  - name: label
    type: text
  - name: entries
    type: list
    item:
      type: record
      fields:
        - name: key
          type: text
          nullable: true
        - name: value
          type: text
          coerce: true
`
			f, err := parseField(t, src, true)
			if err != nil {
				t.Fatalf("parseManifestField() error = %v, want nil", err)
			}
			// A record item descriptor needs no `name`: it is described by
			// its `fields:`.
			if f.Name != "" {
				t.Errorf("Name = %q, want empty for a record item descriptor", f.Name)
			}
			if f.Type != FieldRecord {
				t.Fatalf("Type = %q, want %q", f.Type, FieldRecord)
			}
			if len(f.Fields) != 2 {
				t.Fatalf("len(Fields) = %d, want 2", len(f.Fields))
			}
			if got := f.Fields[0]; got.Name != "label" || got.Type != FieldText {
				t.Errorf("Fields[0] = %+v, want name=label type=text", got)
			}
			entries := f.Fields[1]
			if entries.Name != "entries" || entries.Type != FieldList || entries.Item == nil {
				t.Fatalf("Fields[1] = %+v, want name=entries type=list with an item", entries)
			}
			// The list item recurses into a record — itself unnamed — whose
			// own fields carry the nullable/coerce keys.
			inner := entries.Item
			if inner.Name != "" {
				t.Errorf("inner record item Name = %q, want empty", inner.Name)
			}
			if inner.Type != FieldRecord {
				t.Fatalf("inner record item Type = %q, want %q", inner.Type, FieldRecord)
			}
			if len(inner.Fields) != 2 {
				t.Fatalf("len(inner.Fields) = %d, want 2", len(inner.Fields))
			}
			if k := inner.Fields[0]; k.Name != "key" || k.Type != FieldText || !k.Nullable {
				t.Errorf("inner.Fields[0] = %+v, want name=key type=text nullable=true", k)
			}
			if v := inner.Fields[1]; v.Name != "value" || v.Type != FieldText || !v.Coerce {
				t.Errorf("inner.Fields[1] = %+v, want name=value type=text coerce=true", v)
			}
		})

		t.Run("record is rejected as a field type", func(t *testing.T) {
			_, err := parseField(t, "name: x\ntype: record\nfields:\n  - name: y\n    type: text\n", false)
			if err == nil {
				t.Fatal("parseManifestField() error = nil, want a record-as-field-type rejection")
			}
			if want := `field "x": type "record" is only valid as a list item descriptor`; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want it to contain %q", err, want)
			}
		})

		t.Run("record item requires fields and a non-record item requires a name", func(t *testing.T) {
			_, err := parseField(t, "type: record\n", true)
			if err == nil {
				t.Fatal("parseManifestField() error = nil, want a missing-fields rejection")
			}
			if want := `field "": type "record" requires key "fields"`; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want it to contain %q", err, want)
			}

			_, err = parseField(t, "type: text\n", true)
			if err == nil {
				t.Fatal("parseManifestField() error = nil, want a missing-name rejection")
			}
			if want := `field: missing required key "name"`; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want it to contain %q", err, want)
			}
		})
	})
}

// mustParseDefinitions parses a library.yaml document carrying the optional
// named-definition sections (groups:, field_sets:, types:) into the
// *LibraryDefinitions a template's type:/include:/@group references resolve
// against at parseManifest time (named-library-definitions). It reads only
// those three sections, so the rest of the document (name, format, …) is
// irrelevant to it; a malformed section fails the test. A document with none
// of the three sections yields a nil *LibraryDefinitions.
func mustParseDefinitions(t *testing.T, libraryYAML string) *LibraryDefinitions {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(libraryYAML), &doc); err != nil {
		t.Fatalf("yaml.Unmarshal(library.yaml) error = %v, want nil", err)
	}
	defs, err := loadLibraryDefinitions(LibraryFile, &doc)
	if err != nil {
		t.Fatalf("loadLibraryDefinitions() error = %v, want nil", err)
	}
	return defs
}

// TestRegistryRejections gives one failing fixture registry per build-time
// rejection and asserts the specific error text. Some fixtures are rejected at
// Register (local definition) and some only at Validate (cross-template and
// structured-example checks); buildAndValidate runs both exactly as the
// built-ins loader does.
func TestRegistryRejections(t *testing.T) {
	zero := 0.0

	reservedField := regSlide("resv")
	reservedField.Fields = []Field{{Name: "body", Type: FieldText}}

	formatField := regSlide("resvfmt")
	formatField.Fields = []Field{{Name: "date_format", Type: FieldText}}

	notesSection := regSlide("resvnotes")
	notesSection.Sections = []SectionDecl{{Name: "notes", Accepted: []string{"col"}}}

	bodySection := regSlide("resvbody")
	bodySection.Sections = []SectionDecl{{Name: "body", Accepted: []string{"col"}}}

	// `deck` and `slide` are the reserved top-level template-context names
	// (template-context-reserved): neither is declarable as a field or a
	// section, alongside `body`, `notes` and the `_format` suffix.
	reservedDeckField := regSlide("resvdeck")
	reservedDeckField.Fields = []Field{{Name: "deck", Type: FieldText}}

	reservedSlideField := regSlide("resvslide")
	reservedSlideField.Fields = []Field{{Name: "slide", Type: FieldText}}

	deckSection := regSlide("resvdecksec")
	deckSection.Sections = []SectionDecl{{Name: "deck", Accepted: []string{"col"}}}

	slideSection := regSlide("resvslidesec")
	slideSection.Sections = []SectionDecl{{Name: "slide", Accepted: []string{"col"}}}

	cycleA := regSection("cyclea")
	cycleA.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"cycleb"}}}
	cycleB := regSection("cycleb")
	cycleB.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"cyclea"}}}

	slideTmpl := regSlide("badslide")
	acceptsSlide := regSlide("host")
	acceptsSlide.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"badslide"}}}

	needsBody := regSection("needsbody")
	needsBody.Body = BodyRule{Mode: BodyRequired}
	bodyAsType := regSlide("host2")
	bodyAsType.Fields = []Field{{Name: "block", Type: FieldSectionTemplate, SectionTemplate: "needsbody"}}

	missingRequired := regSlide("ex", Field{Name: "title", Type: FieldText, Required: true})

	ruleViolation := regSlide("ex2", Field{Name: "n", Type: FieldNumber, Min: &zero})
	ruleViolation.Example.Frontmatter = map[string]any{"n": -1}

	undeclaredSection := regSlide("ex3")
	undeclaredSection.Example.Sections = []ExampleSection{{Name: "ghost"}}

	col := regSection("col")
	belowMin := regSlide("ex4")
	belowMin.Sections = []SectionDecl{{Name: "cols", Accepted: []string{"col"}, Min: 2, Max: 4}}
	belowMin.Example.Sections = []ExampleSection{{Name: "cols"}}

	tests := []struct {
		name      string
		templates []*Template
		want      string
	}{
		{
			name:      "reference cycle",
			templates: []*Template{cycleA, cycleB},
			want:      `template "cyclea": section template reference cycle: sections/cyclea → sections/cycleb → sections/cyclea`,
		},
		{
			name:      "section accepts a slide-usage template",
			templates: []*Template{slideTmpl, acceptsSlide},
			want:      `template "host": section "slot" accepts "badslide", which is a slide-usage template; sections accept only section-usage templates`,
		},
		{
			name:      "required-body template used as a field type",
			templates: []*Template{needsBody, bodyAsType},
			want:      `template "host2": field "block" uses section template "needsbody" as a field type, but that template requires a body`,
		},
		{
			name:      "reserved field name body",
			templates: []*Template{reservedField},
			want:      `template "resv": field name "body" is reserved and cannot be declared`,
		},
		{
			name:      "reserved field name _format suffix",
			templates: []*Template{formatField},
			want:      `template "resvfmt": field name "date_format" uses the reserved _format suffix`,
		},
		{
			name:      "reserved section name notes",
			templates: []*Template{notesSection},
			want:      `template "resvnotes": section name "notes" is reserved and cannot be declared`,
		},
		{
			name:      "reserved section name body",
			templates: []*Template{bodySection},
			want:      `template "resvbody": section name "body" is reserved and cannot be declared`,
		},
		{
			name:      "reserved field name deck",
			templates: []*Template{reservedDeckField},
			want:      `template "resvdeck": field name "deck" is reserved and cannot be declared`,
		},
		{
			name:      "reserved field name slide",
			templates: []*Template{reservedSlideField},
			want:      `template "resvslide": field name "slide" is reserved and cannot be declared`,
		},
		{
			name:      "reserved section name deck",
			templates: []*Template{deckSection},
			want:      `template "resvdecksec": section name "deck" is reserved and cannot be declared`,
		},
		{
			name:      "reserved section name slide",
			templates: []*Template{slideSection},
			want:      `template "resvslidesec": section name "slide" is reserved and cannot be declared`,
		},
		{
			name:      "example missing a required field",
			templates: []*Template{missingRequired},
			want:      `template "ex": example frontmatter: title: required: field "title" is required but missing — add a title: value`,
		},
		{
			name:      "example violates a field rule",
			templates: []*Template{ruleViolation},
			want:      `template "ex2": example frontmatter: n: min: -1 is below the minimum 0 — use a value of at least 0`,
		},
		{
			name:      "example declares an undeclared section",
			templates: []*Template{undeclaredSection},
			want:      `template "ex3": example declares section "ghost", which the template does not declare`,
		},
		{
			name:      "example section below its minimum",
			templates: []*Template{col, belowMin},
			want:      `template "ex4": example: cols: min_sections: section "cols" has 1 instance(s), minimum is 2 — add at least 1 "cols" section(s)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := buildAndValidate(tt.templates...)
			if err == nil {
				t.Fatalf("buildAndValidate() = nil, want error %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("buildAndValidate() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}

	// The remaining cases exercise the library-driven path: a manifest
	// decoding problem (parseManifest, local to one template.yaml) or a
	// library-wide build-check rejection (checkLibraryBuild, over the whole
	// registry Validate builds from NewRegistryFromLibrary), each surfaced as
	// a positioned *LibraryError instead of a plain Registry error.

	t.Run("malformed manifest is positioned", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "fields: [\n"), nil)
		libErr := asLibraryError(t, err)
		if libErr.Path == "" {
			t.Error("Path is empty, want the offending template.yaml")
		}
	})

	t.Run("unknown top-level manifest key suggests a valid one", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "descriptoin: x\n"), nil)
		libErr := asLibraryError(t, err)
		if libErr.Suggestion != "description" {
			t.Errorf("Suggestion = %q, want %q", libErr.Suggestion, "description")
		}
	})

	t.Run("unknown field type suggests a valid one", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide,
			"fields:\n  - name: title\n    type: txet\n"), nil)
		libErr := asLibraryError(t, err)
		if libErr.Suggestion != "text" {
			t.Errorf("Suggestion = %q, want %q", libErr.Suggestion, "text")
		}
		if !strings.Contains(libErr.Message, `unknown type "txet"`) {
			t.Errorf("Message = %q, want it to name the unknown type", libErr.Message)
		}
	})

	t.Run("field missing required name", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "fields:\n  - type: text\n"), nil)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, `missing required key "name"`) {
			t.Errorf("Message = %q, want it to say name is missing", libErr.Message)
		}
	})

	t.Run("field missing required type", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "fields:\n  - name: title\n"), nil)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, `missing required key "type"`) {
			t.Errorf("Message = %q, want it to say type is missing", libErr.Message)
		}
	})

	t.Run("unknown body mode suggests a valid one", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "body:\n  mode: requird\n"), nil)
		libErr := asLibraryError(t, err)
		if libErr.Suggestion != "required" {
			t.Errorf("Suggestion = %q, want %q", libErr.Suggestion, "required")
		}
	})

	// named-library-definitions (plan.phase-04.task-15): a template's `type:`
	// and `- include:` references and a section's `@group` `accepted:` entry
	// resolve against the OWNING layer's parsed LibraryDefinitions
	// (parseManifest's defs argument), and an unknown name of any of the three
	// kinds is a positioned templates error carrying a closest-match
	// suggestion. defs mirrors the sections a library.yaml declares: groups
	// (accept-lists), field_sets and types.
	defs := mustParseDefinitions(t, `name: rej
format: 1
groups:
  accents:
    - emphasis
    - note
field_sets:
  meta:
    - name: subtitle
      type: text
    - name: weight
      type: number
types:
  colour:
    type: enum
    variants: [red, green, blue]
    description: a named colour
`)

	t.Run("a named type expands into the referencing field", func(t *testing.T) {
		def := mustParseManifest(t, "host", KindSlide,
			"fields:\n  - name: accent\n    type: colour\n    required: true\n    description: the slide accent\n"+
				"  - name: tint\n    type: colour\n", defs)
		if len(def.Fields) != 2 {
			t.Fatalf("len(Fields) = %d, want 2 (both reference the named type colour)", len(def.Fields))
		}
		accent := def.Fields[0]
		if accent.Name != "accent" {
			t.Errorf("Fields[0].Name = %q, want the referencing field's name %q (the named type's own name is not kept)", accent.Name, "accent")
		}
		if accent.Type != FieldEnum {
			t.Errorf("Fields[0].Type = %q, want %q (the named type's type)", accent.Type, FieldEnum)
		}
		if !reflect.DeepEqual(accent.Variants, []string{"red", "green", "blue"}) {
			t.Errorf("Fields[0].Variants = %v, want the named type's variants", accent.Variants)
		}
		if !accent.Required {
			t.Error("Fields[0].Required = false, want true: the referencing field's own required key overrides")
		}
		if accent.Description != "the slide accent" {
			t.Errorf("Fields[0].Description = %q, want the referencing field's own description (it overrides)", accent.Description)
		}
		tint := def.Fields[1]
		if tint.Name != "tint" || tint.Type != FieldEnum {
			t.Errorf("Fields[1] = %+v, want name=tint type=enum", tint)
		}
		if tint.Description != "a named colour" {
			t.Errorf("Fields[1].Description = %q, want the named type's description (the referencing field omits it)", tint.Description)
		}
		if tint.Required {
			t.Error("Fields[1].Required = true, want false: the named type's own required is kept when the referencing field omits it")
		}
	})

	t.Run("an include expands the named field set in place", func(t *testing.T) {
		def := mustParseManifest(t, "host", KindSlide,
			"fields:\n  - include: meta\n  - name: title\n    type: text\n", defs)
		if len(def.Fields) != 3 {
			t.Fatalf("len(Fields) = %d, want 3 (two from the meta field set, then title)", len(def.Fields))
		}
		if def.Fields[0].Name != "subtitle" || def.Fields[0].Type != FieldText {
			t.Errorf("Fields[0] = %+v, want the field set's subtitle:text", def.Fields[0])
		}
		if def.Fields[1].Name != "weight" || def.Fields[1].Type != FieldNumber {
			t.Errorf("Fields[1] = %+v, want the field set's weight:number", def.Fields[1])
		}
		if def.Fields[2].Name != "title" || def.Fields[2].Type != FieldText {
			t.Errorf("Fields[2] = %+v, want the literal title:text after the expanded set", def.Fields[2])
		}
	})

	t.Run("an @group expands into accepted in place with literal names", func(t *testing.T) {
		def := mustParseManifest(t, "host", KindSlide,
			"sections:\n  - name: asides\n    accepted: [\"@accents\", literal]\n", defs)
		if len(def.Sections) != 1 {
			t.Fatalf("len(Sections) = %d, want 1", len(def.Sections))
		}
		want := []string{"emphasis", "note", "literal"}
		if got := def.Sections[0].Accepted; !reflect.DeepEqual(got, want) {
			t.Errorf("Accepted = %v, want the group expanded in place then the literal name, %v", got, want)
		}
	})

	t.Run("a named type used as a list item descriptor still requires a name", func(t *testing.T) {
		// A named type expanded into a top-level field takes the referencing
		// field's `name:`, and a record item needs none; a non-record item
		// descriptor (here naming the type colour) is still a field and still
		// needs its own `name:`.
		_, err := parseManifest(manifestLibraryTemplate("host", KindSlide,
			"fields:\n  - name: items\n    type: list\n    item:\n      type: colour\n"), defs)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, `missing required key "name"`) {
			t.Errorf("Message = %q, want it to require a name on the non-record item descriptor", libErr.Message)
		}
	})

	t.Run("unknown named type suggests the closest type", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("host", KindSlide,
			"fields:\n  - name: accent\n    type: colur\n"), defs)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, `unknown type "colur"`) {
			t.Errorf("Message = %q, want it to name the unknown type", libErr.Message)
		}
		if libErr.Suggestion != "colour" {
			t.Errorf("Suggestion = %q, want the declared named type %q", libErr.Suggestion, "colour")
		}
	})

	t.Run("unknown include suggests the closest field set", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("host", KindSlide,
			"fields:\n  - include: metaa\n"), defs)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, `unknown field set "metaa"`) {
			t.Errorf("Message = %q, want it to name the unknown field set", libErr.Message)
		}
		if libErr.Suggestion != "meta" {
			t.Errorf("Suggestion = %q, want the declared field set %q", libErr.Suggestion, "meta")
		}
	})

	t.Run("unknown @group suggests the closest group", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("host", KindSlide,
			"sections:\n  - name: asides\n    accepted: [\"@acents\"]\n"), defs)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, `unknown group "acents"`) {
			t.Errorf("Message = %q, want it to name the unknown group", libErr.Message)
		}
		if libErr.Suggestion != "accents" {
			t.Errorf("Suggestion = %q, want the declared group %q", libErr.Suggestion, "accents")
		}
	})

	// libraryRejection builds a library over slideBody/sectionBody library
	// entries under demoLibraryFS() (whose slides/deck + sections/column are
	// dropped and replaced with the given manifests), returning the
	// *LibraryError LoadLibrary reports.
	libraryRejection := func(t *testing.T, slideManifest, sectionManifest string) *LibraryError {
		t.Helper()
		fsys := fstest.MapFS{
			"library.yaml":                     {Data: []byte("name: rej\nformat: 1\n")},
			"slides/badslide/template.yaml":    {Data: []byte(slideManifest)},
			"slides/badslide/layout.html.tmpl": {Data: []byte("<section></section>")},
			"slides/badslide/example.md":       {Data: []byte("---\n---\n")},
			"sections/badsec/template.yaml":    {Data: []byte(sectionManifest)},
			"sections/badsec/layout.html.tmpl": {Data: []byte("<div></div>")},
			"sections/badsec/example.md":       {Data: []byte("```\n```\n")},
		}
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		return asLibraryError(t, err)
	}

	t.Run("section accepting a templates/slides template", func(t *testing.T) {
		// "slot" accepts "other", a slide-usage template, which sections may
		// never accept.
		fsys := fstest.MapFS{
			"library.yaml":                  {Data: []byte("name: rej\nformat: 1\n")},
			"slides/host/template.yaml":     {Data: []byte("sections:\n  - name: slot\n    accepted: [other]\n")},
			"slides/host/layout.html.tmpl":  {Data: []byte("<section></section>")},
			"slides/host/example.md":        {Data: []byte("---\n---\n")},
			"slides/other/template.yaml":    {Data: []byte("")},
			"slides/other/layout.html.tmpl": {Data: []byte("<section></section>")},
			"slides/other/example.md":       {Data: []byte("---\n---\n")},
		}
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, "slide-usage template") {
			t.Errorf("Message = %q, want it to say the accepted template is slide-usage", libErr.Message)
		}
	})

	t.Run("slide template used as a section-template field type", func(t *testing.T) {
		fsys := fstest.MapFS{
			"library.yaml":                  {Data: []byte("name: rej\nformat: 1\n")},
			"slides/host/template.yaml":     {Data: []byte("fields:\n  - name: block\n    type: section-template\n    section_template: other\n")},
			"slides/host/layout.html.tmpl":  {Data: []byte("<section></section>")},
			"slides/host/example.md":        {Data: []byte("---\n---\n")},
			"slides/other/template.yaml":    {Data: []byte("")},
			"slides/other/layout.html.tmpl": {Data: []byte("<section></section>")},
			"slides/other/example.md":       {Data: []byte("---\n---\n")},
		}
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, "slide-usage template") {
			t.Errorf("Message = %q, want it to say the field names a slide-usage template", libErr.Message)
		}
	})

	t.Run("required-body template used as a section-template field type", func(t *testing.T) {
		fsys := fstest.MapFS{
			"library.yaml":                        {Data: []byte("name: rej\nformat: 1\n")},
			"slides/host/template.yaml":           {Data: []byte("fields:\n  - name: block\n    type: section-template\n    section_template: needsbody\n")},
			"slides/host/layout.html.tmpl":        {Data: []byte("<section></section>")},
			"slides/host/example.md":              {Data: []byte("---\n---\n")},
			"sections/needsbody/template.yaml":    {Data: []byte("body:\n  mode: required\n")},
			"sections/needsbody/layout.html.tmpl": {Data: []byte("<div></div>")},
			"sections/needsbody/example.md":       {Data: []byte("```\n```\n")},
		}
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, "requires a body") {
			t.Errorf("Message = %q, want it to say the section template requires a body", libErr.Message)
		}
	})

	t.Run("reserved _format suffix field name", func(t *testing.T) {
		libErr := libraryRejection(t, "fields:\n  - name: date_format\n    type: text\n", "")
		if !strings.Contains(libErr.Message, "_format suffix") {
			t.Errorf("Message = %q, want it to mention the reserved _format suffix", libErr.Message)
		}
	})

	t.Run("reserved field name notes, body, deck, slide and item", func(t *testing.T) {
		for _, name := range []string{"notes", "body", "deck", "slide", "item"} {
			libErr := libraryRejection(t, "fields:\n  - name: "+name+"\n    type: text\n", "")
			if !strings.Contains(libErr.Message, "reserved") {
				t.Errorf("field %q: Message = %q, want it to say the name is reserved", name, libErr.Message)
			}
		}
	})

	t.Run("reserved section name notes, body, deck, slide and item", func(t *testing.T) {
		for _, name := range []string{"notes", "body", "deck", "slide", "item"} {
			libErr := libraryRejection(t, "sections:\n  - name: "+name+"\n    accepted: [badsec]\n", "")
			if !strings.Contains(libErr.Message, "reserved") {
				t.Errorf("section %q: Message = %q, want it to say the name is reserved", name, libErr.Message)
			}
		}
	})

	t.Run("undefined template reference", func(t *testing.T) {
		libErr := libraryRejection(t, "sections:\n  - name: slot\n    accepted: [ghost]\n", "")
		if !strings.Contains(libErr.Message, "not a defined template") {
			t.Errorf("Message = %q, want it to say ghost is not defined", libErr.Message)
		}
		if strings.Contains(libErr.Message, "did you mean") {
			t.Errorf("Message = %q, want no suggestion since %q is not close to any defined template", libErr.Message, "ghost")
		}
	})

	t.Run("undefined template reference close to a defined name suggests it", func(t *testing.T) {
		libErr := libraryRejection(t, "sections:\n  - name: slot\n    accepted: [badsecc]\n", "")
		if !strings.Contains(libErr.Message, "not a defined template") {
			t.Errorf("Message = %q, want it to say badsecc is not defined", libErr.Message)
		}
		if !strings.Contains(libErr.Message, `did you mean "badsec"?`) {
			t.Errorf("Message = %q, want it to suggest %q", libErr.Message, `did you mean "badsec"?`)
		}
	})

	t.Run("section template reference cycle", func(t *testing.T) {
		fsys := fstest.MapFS{
			"library.yaml":                     {Data: []byte("name: rej\nformat: 1\n")},
			"sections/cyclea/template.yaml":    {Data: []byte("sections:\n  - name: slot\n    accepted: [cycleb]\n")},
			"sections/cyclea/layout.html.tmpl": {Data: []byte("<div></div>")},
			"sections/cyclea/example.md":       {Data: []byte("```\n```\n")},
			"sections/cycleb/template.yaml":    {Data: []byte("sections:\n  - name: slot\n    accepted: [cyclea]\n")},
			"sections/cycleb/layout.html.tmpl": {Data: []byte("<div></div>")},
			"sections/cycleb/example.md":       {Data: []byte("```\n```\n")},
		}
		// Validate names the root template it was walking (`template
		// "cyclea": …`), so checkLibraryBuild path-qualifies the cycle to that
		// template's template.yaml, and SectionCycleError.Error renders the
		// chain section-qualified
		// (requirements.constraint.section-cycle-error-chain).
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if want := "templates/sections/cyclea/template.yaml"; libErr.Path != want {
			t.Errorf("Path = %q, want %q", libErr.Path, want)
		}
		want := `template "cyclea": section template reference cycle: sections/cyclea → sections/cycleb → sections/cyclea`
		if libErr.Message != want {
			t.Errorf("Message = %q, want %q", libErr.Message, want)
		}
	})

	t.Run("a section template declaring nested sections loads", func(t *testing.T) {
		// A section template may declare `sections:` with the slide schema, so
		// composition nests (template-composition). outer holds one `items`
		// child accepting the inner section template. Its example.md nests a
		// child instance under a top-level `# items` heading, which phase-1
		// example validation accepts.
		fsys := fstest.MapFS{
			"library.yaml": {Data: []byte("name: nested\nformat: 1\n")},
			"sections/outer/template.yaml": {Data: []byte("description: a container section\n" +
				"sections:\n  - name: items\n    accepted: [inner]\n    min: 1\n    max: 4\n")},
			"sections/outer/layout.html.tmpl": {Data: []byte("<div>{{range .items}}<span>{{.}}</span>{{end}}</div>")},
			"sections/outer/example.md":       {Data: []byte("# items\n")},
			"sections/inner/template.yaml":    {Data: []byte("description: an inner child\n")},
			"sections/inner/layout.html.tmpl": {Data: []byte("<span>{{.body}}</span>")},
			"sections/inner/example.md":       {Data: []byte("\n")},
		}
		lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		if err != nil {
			t.Fatalf("LoadLibrary() error = %v, want nil for a nested composition", err)
		}
		outer, kind, ok := lib.TemplateByName("outer")
		if !ok || kind != KindSection {
			t.Fatalf("TemplateByName(outer) = (%v, %q, %v), want (non-nil, %q, true)", outer, kind, ok, KindSection)
		}
		if outer.Definition == nil {
			t.Fatal("checkLibraryBuild left outer.Definition nil")
		}
		got := outer.Definition.Sections
		if len(got) != 1 || got[0].Name != "items" || !reflect.DeepEqual(got[0].Accepted, []string{"inner"}) {
			t.Errorf("outer.Sections = %+v, want one %q section accepting inner", got, "items")
		}
		if _, _, ok := lib.TemplateByName("inner"); !ok {
			t.Error("section template inner not loaded")
		}
	})

	t.Run("a composition chain deeper than six heading levels is positioned", func(t *testing.T) {
		// cha → … → chg is seven composition levels: one more than the six
		// Markdown heading levels the bound allows
		// (requirements.constraint.section-depth-limit). The walk reports it
		// as a step-4 templates error path-qualified to the root template's
		// template.yaml.
		fsys := fstest.MapFS{"library.yaml": {Data: []byte("name: deep\nformat: 1\n")}}
		chain := []string{"cha", "chb", "chc", "chd", "che", "chf", "chg"}
		for i, name := range chain {
			manifest := ""
			if i+1 < len(chain) {
				manifest = "sections:\n  - name: slot\n    accepted: [" + chain[i+1] + "]\n"
			}
			fsys["sections/"+name+"/template.yaml"] = &fstest.MapFile{Data: []byte(manifest)}
			fsys["sections/"+name+"/layout.html.tmpl"] = &fstest.MapFile{Data: []byte("<div></div>")}
			fsys["sections/"+name+"/example.md"] = &fstest.MapFile{Data: []byte("\n")}
		}
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if want := "templates/sections/cha/template.yaml"; libErr.Path != want {
			t.Errorf("Path = %q, want %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, "7 levels deep") {
			t.Errorf("Message = %q, want it to report the over-deep chain", libErr.Message)
		}
		if slug := "cha → chb → chc → chd → che → chf → chg"; !strings.Contains(libErr.Message, slug) {
			t.Errorf("Message = %q, want the whole chain %q", libErr.Message, slug)
		}
		if !strings.Contains(libErr.Message, "6-heading-level limit") {
			t.Errorf("Message = %q, want the six-heading-level limit", libErr.Message)
		}
	})

	t.Run("bad variant: enum with no variants", func(t *testing.T) {
		libErr := libraryRejection(t, "fields:\n  - name: mode\n    type: enum\n", "")
		if !strings.Contains(libErr.Message, "no variants") {
			t.Errorf("Message = %q, want it to say the enum field has no variants", libErr.Message)
		}
	})

	// The bad-variant and cross-field-bound build-time checks reach a field
	// inside an inline record item exactly as they do at the top level
	// (plan.phase-03.task-8, record-item-type): checkFieldVariants recurses
	// into a record item's fields, so a nested enum's variants remain the only
	// variant mechanism at that depth; and checkDefinition resolves each list
	// bound in the field's own scope — a leading `/name` against the template's
	// top-level fields, a bare name against the enclosing record's siblings —
	// rejecting an unresolvable reference, or one naming a non-list field, as a
	// positioned templates error.

	t.Run("a nested enum inside an inline record item is accepted", func(t *testing.T) {
		fsys := fstest.MapFS{
			"library.yaml":                 {Data: []byte("name: rec\nformat: 1\n")},
			"slides/host/template.yaml":    {Data: []byte("fields:\n  - name: items\n    type: list\n    item:\n      type: record\n      fields:\n        - name: mode\n          type: enum\n          variants: [a, b]\n")},
			"slides/host/layout.html.tmpl": {Data: []byte("<section></section>")},
			"slides/host/example.md":       {Data: []byte("---\n---\n")},
		}
		if _, err := LoadLibrary(rootedFS(fsys), TemplatesDir); err != nil {
			t.Fatalf("LoadLibrary() error = %v, want nil: an enum inside a record item with variants is accepted", err)
		}
	})

	t.Run("bad variant: enum inside a record item with no variants", func(t *testing.T) {
		libErr := libraryRejection(t,
			"fields:\n  - name: items\n    type: list\n    item:\n      type: record\n      fields:\n        - name: mode\n          type: enum\n", "")
		if !strings.Contains(libErr.Message, "no variants") {
			t.Errorf("Message = %q, want it to say the nested enum field has no variants", libErr.Message)
		}
		if !strings.Contains(libErr.Message, `field "mode"`) {
			t.Errorf("Message = %q, want it to name the nested field %q", libErr.Message, "mode")
		}
	})

	t.Run("bad variant: nested enum default not among its variants", func(t *testing.T) {
		libErr := libraryRejection(t,
			"fields:\n  - name: items\n    type: list\n    item:\n      type: record\n      fields:\n        - name: mode\n          type: enum\n          variants: [a, b]\n          default: c\n", "")
		if !strings.Contains(libErr.Message, "not one of its variants") {
			t.Errorf("Message = %q, want it to say the nested enum default is not among its variants", libErr.Message)
		}
		if !strings.Contains(libErr.Message, `field "mode"`) {
			t.Errorf("Message = %q, want it to name the nested field %q", libErr.Message, "mode")
		}
	})

	t.Run("unresolvable max_items_from: /name names no top-level field", func(t *testing.T) {
		libErr := libraryRejection(t,
			"fields:\n  - name: items\n    type: list\n    item:\n      type: record\n      fields:\n        - name: a\n          type: list\n          item:\n            name: i\n            type: text\n          max_items_from: /ghost\n", "")
		if !strings.Contains(libErr.Message, "not a declared field") {
			t.Errorf("Message = %q, want it to say /ghost names no top-level field", libErr.Message)
		}
		if !strings.Contains(libErr.Message, `"/ghost"`) {
			t.Errorf("Message = %q, want it to quote the unresolvable reference %q", libErr.Message, "/ghost")
		}
	})

	t.Run("unresolvable min_items_from: bare name names no record sibling", func(t *testing.T) {
		libErr := libraryRejection(t,
			"fields:\n  - name: items\n    type: list\n    item:\n      type: record\n      fields:\n        - name: a\n          type: list\n          item:\n            name: i\n            type: text\n          min_items_from: ghost\n", "")
		if !strings.Contains(libErr.Message, "not a declared field") {
			t.Errorf("Message = %q, want it to say ghost names no sibling in the enclosing record", libErr.Message)
		}
		if !strings.Contains(libErr.Message, `"ghost"`) {
			t.Errorf("Message = %q, want it to quote the unresolvable reference %q", libErr.Message, "ghost")
		}
	})

	t.Run("max_items_from names a field that is not a list", func(t *testing.T) {
		libErr := libraryRejection(t,
			"fields:\n  - name: items\n    type: list\n    item:\n      type: record\n      fields:\n        - name: a\n          type: list\n          item:\n            name: i\n            type: text\n          max_items_from: b\n        - name: b\n          type: text\n", "")
		if !strings.Contains(libErr.Message, "not a list field") {
			t.Errorf("Message = %q, want it to say the referenced field b is not a list", libErr.Message)
		}
	})

	t.Run("bad example: violates the field schema", func(t *testing.T) {
		fsys := fstest.MapFS{
			"library.yaml":                 {Data: []byte("name: rej\nformat: 1\n")},
			"slides/host/template.yaml":    {Data: []byte("fields:\n  - name: title\n    type: text\n    required: true\n")},
			"slides/host/layout.html.tmpl": {Data: []byte("<section></section>")},
			"slides/host/example.md":       {Data: []byte("---\n---\n")}, // missing required title
		}
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, "required") {
			t.Errorf("Message = %q, want it to say the required field is missing", libErr.Message)
		}
	})

	t.Run("cross-kind name collision", func(t *testing.T) {
		fsys := fstest.MapFS{
			"library.yaml":                  {Data: []byte("name: rej\nformat: 1\n")},
			"slides/dup/template.yaml":      {Data: []byte("")},
			"slides/dup/layout.html.tmpl":   {Data: []byte("<section></section>")},
			"slides/dup/example.md":         {Data: []byte("---\n---\n")},
			"sections/dup/template.yaml":    {Data: []byte("")},
			"sections/dup/layout.html.tmpl": {Data: []byte("<div></div>")},
			"sections/dup/example.md":       {Data: []byte("```\n```\n")},
		}
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, "declared in both") {
			t.Errorf("Message = %q, want it to say dup is declared in both slides and sections", libErr.Message)
		}
	})
}

// TestRegistryRegister checks duplicate-name rejection, that a definition
// rejected at registration is never stored, and that a nil registry is
// rejectable rather than panicking.
func TestRegistryRegister(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.Register(regSlide("dup")); err != nil {
		t.Fatalf("Register(dup) error = %v, want nil", err)
	}
	// A distinct template object with the same name is still a duplicate.
	err := r.Register(regSlide("dup"))
	if want := `template "dup" is already registered`; err == nil || err.Error() != want {
		t.Errorf("second Register(dup) error = %v, want %q", err, want)
	}

	bad := regSlide("bad", Field{Name: "body", Type: FieldText})
	if err := r.Register(bad); err == nil {
		t.Error("Register(bad) error = nil, want the reserved-name rejection")
	}
	if _, ok := r.Lookup("bad"); ok {
		t.Error(`Lookup("bad") ok = true, want a rejected definition not to be stored`)
	}

	if err := r.Register(nil); err == nil {
		t.Error("Register(nil) error = nil, want a definition error")
	}
}

// TestRegistryValidateOrder proves Validate's fixed, deterministic order: it
// walks templates sorted by name, and runs the cross-template stages in the
// documented sequence (section checks, then field-type checks, then the
// composition walk, then the structured-example checks) before returning the
// first failure. It also proves the composition-depth bound the walk enforces
// — a six-deep chain validates, a seven-deep chain fails as *SectionDepthError
// with the whole chain, and a section-stage failure still wins over it.
// Repeated calls are stable.
func TestRegistryValidateOrder(t *testing.T) {
	t.Run("templates are checked in sorted order within a stage", func(t *testing.T) {
		slide := regSlide("s")
		first := regSlide("aaa")
		first.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"s"}}}
		last := regSlide("zzz")
		last.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"s"}}}

		err := buildAndValidate(first, last, slide)
		want := `template "aaa": section "slot" accepts "s", which is a slide-usage template; sections accept only section-usage templates`
		if err == nil || err.Error() != want {
			t.Errorf("Validate() error = %v, want the alphabetically first template's %q", err, want)
		}
	})

	t.Run("section checks precede the composition walk", func(t *testing.T) {
		// aaa and bbb form a cycle (caught by the walk), but zzz's
		// slide-accepting section is caught by an earlier stage, so it wins.
		a := regSection("aaa")
		a.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"bbb"}}}
		b := regSection("bbb")
		b.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"aaa"}}}
		slide := regSlide("s")
		host := regSlide("zzz")
		host.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"s"}}}

		err := buildAndValidate(a, b, slide, host)
		want := `template "zzz": section "slot" accepts "s", which is a slide-usage template; sections accept only section-usage templates`
		if err == nil || err.Error() != want {
			t.Errorf("Validate() error = %v, want the section-stage error %q", err, want)
		}
	})

	t.Run("composition walk precedes example checks", func(t *testing.T) {
		a := regSection("aaa")
		a.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"bbb"}}}
		b := regSection("bbb")
		b.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"aaa"}}}
		c := regSlide("ccc", Field{Name: "title", Type: FieldText, Required: true})

		err := buildAndValidate(a, b, c)
		var cycle *SectionCycleError
		if !errors.As(err, &cycle) {
			t.Fatalf("Validate() error = %v, want *SectionCycleError", err)
		}
		if want := []string{"aaa", "bbb", "aaa"}; !reflect.DeepEqual(cycle.Path, want) {
			t.Errorf("cycle.Path = %v, want %v", cycle.Path, want)
		}
		// Path keeps the plain template names; Error renders them
		// section-qualified (requirements.constraint.section-cycle-error-chain).
		if got, want := cycle.Error(), "section template reference cycle: sections/aaa → sections/bbb → sections/aaa"; got != want {
			t.Errorf("cycle.Error() = %q, want %q", got, want)
		}
	})

	t.Run("a six-deep composition chain validates", func(t *testing.T) {
		// a → b → c → d → e → f is exactly the six Markdown heading levels
		// the composition bound allows
		// (requirements.constraint.section-depth-limit), so it validates.
		chain := chainSections("a", "b", "c", "d", "e", "f")
		if err := buildAndValidate(chain...); err != nil {
			t.Fatalf("buildAndValidate() error = %v, want nil for a six-deep chain", err)
		}
	})

	t.Run("a seven-deep composition chain fails as *SectionDepthError", func(t *testing.T) {
		// One more level than the six heading levels breaks the bound: the
		// walk reports the whole over-deep chain and the limit.
		chain := chainSections("a", "b", "c", "d", "e", "f", "g")
		err := buildAndValidate(chain...)
		var depth *SectionDepthError
		if !errors.As(err, &depth) {
			t.Fatalf("buildAndValidate() error = %v, want *SectionDepthError", err)
		}
		if depth.Limit != 6 {
			t.Errorf("depth.Limit = %d, want 6", depth.Limit)
		}
		if want := []string{"a", "b", "c", "d", "e", "f", "g"}; !reflect.DeepEqual(depth.Path, want) {
			t.Errorf("depth.Path = %v, want %v", depth.Path, want)
		}
		// Validate names the template the walk started from so the loader can
		// path-qualify the failure to that template.yaml.
		if !strings.Contains(err.Error(), `template "a":`) {
			t.Errorf("error = %q, want the walk failure named to the root template", err)
		}
	})

	t.Run("the depth error surfaces after the section stage", func(t *testing.T) {
		// The chain is seven deep (the walk's failure), but zzz's
		// slide-accepting section is caught by the earlier section stage, so
		// the section-stage error wins regardless of the walk bound.
		slide := regSlide("s")
		host := regSlide("zzz")
		host.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"s"}}}
		chain := chainSections("a", "b", "c", "d", "e", "f", "g")

		err := buildAndValidate(append(chain, slide, host)...)
		want := `template "zzz": section "slot" accepts "s", which is a slide-usage template; sections accept only section-usage templates`
		if err == nil || err.Error() != want {
			t.Errorf("Validate() error = %v, want the section-stage error %q", err, want)
		}
	})

	t.Run("Validate is stable across calls", func(t *testing.T) {
		a := regSection("aaa")
		a.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"bbb"}}}
		b := regSection("bbb")
		b.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"aaa"}}}

		r := NewRegistry(nil)
		for _, tmpl := range []*Template{a, b} {
			if err := r.Register(tmpl); err != nil {
				t.Fatalf("Register(%q) error = %v", tmpl.Name, err)
			}
		}
		first := r.Validate()
		if first == nil {
			t.Fatal("Validate() = nil, want the cycle error")
		}
		for i := 0; i < 5; i++ {
			got := r.Validate()
			if got == nil || got.Error() != first.Error() {
				t.Fatalf("Validate() call %d = %v, want the stable error %q", i+1, got, first.Error())
			}
		}
	})
}

// ---------------------------------------------------------------------------
// plan.phase-02.task-17: the layer-aware registry.
//
// Two things are under test here (library-layer-references):
//
//   - resolveMergedSectionTarget (library.go): the one lookup that pairs a
//     section name in a merged Library with the *LibraryLayer that contributed
//     it, by identity — so a name carried by two layers is never mistaken for
//     its owner, and a Library that carries no layers still resolves its
//     sections (with a nil layer).
//   - NewRegistryFromLibrary's owning-layer provenance and
//     Registry.checkSections' merged namespace (registrylibrary.go,
//     registry.go): a registry built over a two-layer merged Library records
//     every registered template's owning layer through Registry.LayerOf, and
//     its static section-helper checks resolve a call against the WHOLE merged
//     namespace — a local template's call to an external section resolves —
//     while the same checks still reject an unknown name, an undeclared literal
//     dict key, an omitted required field and a min>0 child section across
//     layers as well as within one.
//
// The two-layer inputs are hand-built exactly as library_test.go's merge tests
// build them: testLayer/addTestSlide/addTestSection give each layer the shape
// loadLibrary returns, and mergeLibraryLayers unions them into one Library.
// Each template carries a parseable manifest (ManifestBytes) and, where a
// helper call matters, a layout (LayoutText), so NewRegistryFromLibrary parses
// and registers it without any content filesystem. Everything is in-process
// and runs under -short.

// layeredSection adds a section-usage LibraryTemplate to layer through
// addTestSection (library_test.go) and gives it the manifest bytes and layout
// text that parseManifest and Registry.checkSections read.
func layeredSection(layer *LibraryLayer, name, manifestYAML, layout string) *LibraryTemplate {
	lt := addTestSection(layer, name)
	lt.ManifestBytes = []byte(manifestYAML)
	lt.LayoutText = layout
	return lt
}

// layeredSlide is the slide-usage counterpart of layeredSection.
func layeredSlide(layer *LibraryLayer, name, manifestYAML, layout string) *LibraryTemplate {
	lt := addTestSlide(layer, name)
	lt.ManifestBytes = []byte(manifestYAML)
	lt.LayoutText = layout
	return lt
}

// layeredLibrary builds a merged Library from one external and one local layer,
// each populated by the corresponding callback (either may be nil for an empty
// layer). The external layer is first, matching ResolveLibraryLayers' order.
func layeredLibrary(external, local func(*LibraryLayer)) *Library {
	ext := testLayer("/deck/external")
	loc := testLayer("/deck/templates")
	if external != nil {
		external(ext)
	}
	if local != nil {
		local(loc)
	}
	return mergeLibraryLayers([]*LibraryLayer{ext, loc})
}

// validateMergedLibrary builds a registry over lib and returns the first error
// either NewRegistryFromLibrary or Registry.Validate reports.
func validateMergedLibrary(t *testing.T, lib *Library) error {
	t.Helper()
	reg, err := NewRegistryFromLibrary(lib)
	if err != nil {
		return err
	}
	return reg.Validate()
}

// TestResolveMergedSectionTarget proves the merged-library section lookup: a
// nil library, an unknown name and a nil section entry all report no target;
// a one-layer library resolves its section to that layer; the owning layer is
// identified by pointer identity, so a same-named template carried by another
// layer is never mistaken for the owner; and a library that carries no layers
// still resolves the target, with a nil layer.
func TestResolveMergedSectionTarget(t *testing.T) {
	t.Run("a nil library reports no target", func(t *testing.T) {
		target, layer, ok := resolveMergedSectionTarget(nil, "footer")
		if ok || target != nil || layer != nil {
			t.Errorf("resolveMergedSectionTarget(nil, footer) = (%p, %p, %v), want (nil, nil, false)", target, layer, ok)
		}
	})

	t.Run("an unknown name reports no target", func(t *testing.T) {
		lib := layeredLibrary(func(ext *LibraryLayer) {
			layeredSection(ext, "footer", "", "")
		}, nil)
		target, layer, ok := resolveMergedSectionTarget(lib, "missing")
		if ok || target != nil || layer != nil {
			t.Errorf("resolveMergedSectionTarget(…, missing) = (%p, %p, %v), want (nil, nil, false)", target, layer, ok)
		}
	})

	t.Run("a nil section entry reports no target", func(t *testing.T) {
		lib := &Library{Sections: map[string]*LibraryTemplate{"footer": nil}}
		target, layer, ok := resolveMergedSectionTarget(lib, "footer")
		if ok || target != nil || layer != nil {
			t.Errorf("resolveMergedSectionTarget(…, footer) = (%p, %p, %v), want (nil, nil, false)", target, layer, ok)
		}
	})

	t.Run("a one-layer library returns its own layer", func(t *testing.T) {
		owner := testLayer("/deck/templates")
		section := layeredSection(owner, "footer", "", "")
		lib := mergeLibraryLayers([]*LibraryLayer{owner})

		target, layer, ok := resolveMergedSectionTarget(lib, "footer")
		if !ok || target != section || layer != owner {
			t.Errorf("resolveMergedSectionTarget = (%p, %p, %v), want (%p, %p, true)", target, layer, ok, section, owner)
		}
	})

	t.Run("the owning layer is found by identity, not by name", func(t *testing.T) {
		// The decoy layer carries a DIFFERENT *LibraryTemplate under the same
		// name and comes first in Layers, so a lookup by name alone would
		// return it; only the pointer identity match returns the true owner.
		decoy := testLayer("/deck/decoy")
		layeredSection(decoy, "footer", "", "")
		owner := testLayer("/deck/owner")
		section := layeredSection(owner, "footer", "", "")

		lib := &Library{
			Sections: map[string]*LibraryTemplate{"footer": section},
			Layers:   []*LibraryLayer{decoy, owner},
		}

		target, layer, ok := resolveMergedSectionTarget(lib, "footer")
		if !ok {
			t.Fatal("resolveMergedSectionTarget ok = false, want true")
		}
		if target != section {
			t.Errorf("target = %p, want the merged pointer %p", target, section)
		}
		if layer != owner {
			t.Errorf("layer = %p, want the identity-matched owner %p (not the same-named decoy %p)", layer, owner, decoy)
		}
	})

	t.Run("a library with no layers resolves with a nil layer", func(t *testing.T) {
		section := &LibraryTemplate{Name: "footer", Kind: KindSection}
		lib := &Library{Sections: map[string]*LibraryTemplate{"footer": section}}

		target, layer, ok := resolveMergedSectionTarget(lib, "footer")
		if !ok || target != section || layer != nil {
			t.Errorf("resolveMergedSectionTarget = (%p, %p, %v), want (%p, nil, true)", target, layer, ok, section)
		}
	})
}

// TestNewRegistryFromLibraryLayerProvenance proves NewRegistryFromLibrary
// records each registered template's owning layer — sections and slides alike —
// looked up by identity over a two-layer merged Library, and that every path
// that cannot identify an owner records none: an unknown name, a registry
// built in code, and a Library that carries no layers all report nil through
// Registry.LayerOf.
func TestNewRegistryFromLibraryLayerProvenance(t *testing.T) {
	external := testLayer("/deck/external")
	local := testLayer("/deck/templates")
	extSection := layeredSection(external, "ext-section", "", "")
	localSection := layeredSection(local, "local-section", "", "")
	localSlide := layeredSlide(local, "local-slide", "", "")

	merged := mergeLibraryLayers([]*LibraryLayer{external, local})
	reg, err := NewRegistryFromLibrary(merged)
	if err != nil {
		t.Fatalf("NewRegistryFromLibrary() error = %v, want nil", err)
	}

	t.Run("sections and slides resolve to their owning layer", func(t *testing.T) {
		tests := []struct {
			name string
			want *LibraryLayer
			tmpl *LibraryTemplate
		}{
			{"ext-section", external, extSection},
			{"local-section", local, localSection},
			{"local-slide", local, localSlide},
		}
		for _, tt := range tests {
			if got := reg.LayerOf(tt.name); got != tt.want {
				t.Errorf("LayerOf(%q) = %p, want %p", tt.name, got, tt.want)
			}
			// The merged map must hold the very pointer whose layer was
			// asserted, or the identity lookup proved nothing.
			if merged.Sections[tt.name] != tt.tmpl && merged.Slides[tt.name] != tt.tmpl {
				t.Errorf("merged library does not hold the %q pointer whose layer was asserted", tt.name)
			}
		}
	})

	t.Run("the owning layers are distinct between the two layers", func(t *testing.T) {
		if reg.LayerOf("ext-section") == reg.LayerOf("local-section") {
			t.Error("LayerOf(ext-section) and LayerOf(local-section) returned the same layer, want the two contributing layers")
		}
	})

	t.Run("an unknown name reports no layer", func(t *testing.T) {
		if got := reg.LayerOf("missing"); got != nil {
			t.Errorf("LayerOf(missing) = %p, want nil", got)
		}
	})

	t.Run("a registry built in code carries no provenance", func(t *testing.T) {
		codeReg := NewRegistry(nil)
		if err := codeReg.Register(regSlide("code-slide")); err != nil {
			t.Fatalf("Register(code-slide) error = %v, want nil", err)
		}
		if got := codeReg.LayerOf("code-slide"); got != nil {
			t.Errorf("LayerOf(code-slide) = %p, want nil for a code-built registry", got)
		}
	})

	t.Run("a library that carries no layers records no provenance", func(t *testing.T) {
		unowned := &Library{
			Sections: map[string]*LibraryTemplate{
				"ghost": {Name: "ghost", Kind: KindSection},
			},
		}
		r, err := NewRegistryFromLibrary(unowned)
		if err != nil {
			t.Fatalf("NewRegistryFromLibrary() error = %v, want nil", err)
		}
		if got := r.LayerOf("ghost"); got != nil {
			t.Errorf("LayerOf(ghost) = %p, want nil when the Library carries no layers", got)
		}
	})

	t.Run("a nil library builds an empty provenance-less registry", func(t *testing.T) {
		r, err := NewRegistryFromLibrary(nil)
		if err != nil {
			t.Fatalf("NewRegistryFromLibrary(nil) error = %v, want nil", err)
		}
		if got := r.LayerOf("anything"); got != nil {
			t.Errorf("LayerOf(anything) = %p, want nil", got)
		}
	})
}

// TestRegistryCheckSectionsAcrossMergedLayers proves Registry.checkSections
// resolves through the merged namespace a two-layer registry is built over:
// a local template's section-helper call to an external section resolves, and
// the identical call is rejected when the registry is built over the external
// layer alone. It then proves the load-time checks are unchanged across a
// layer boundary — an omitted required field, an undeclared literal dict key
// and a min>0 child section of the EXTERNAL target are all still reported,
// attributed to the CALLING local template.
func TestRegistryCheckSectionsAcrossMergedLayers(t *testing.T) {
	const (
		requiredTitle = "fields:\n  - name: title\n    type: text\n    required: true\n"
		optionalTitle = "fields:\n  - name: title\n    type: text\n"
		childMin      = "sections:\n  - name: widgets\n    accepted: [ext-child]\n    min: 1\n"
	)

	t.Run("a local template's call to an external section resolves", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) { layeredSection(ext, "ext-footer", optionalTitle, "") },
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-footer" }}`)
			},
		)
		if err := validateMergedLibrary(t, lib); err != nil {
			t.Fatalf("Validate() error = %v, want nil: the merged namespace resolves the external section", err)
		}
	})

	t.Run("the external-only registry rejects a call to a local-only section", func(t *testing.T) {
		external := testLayer("/deck/external")
		layeredSlide(external, "ext-slide", "", `{{ section "local-only" }}`)
		// A one-layer library: the registry sees only the external layer's
		// namespace, so the local-only name is undefined.
		lib := mergeLibraryLayers([]*LibraryLayer{external})

		want := `template "ext-slide": section helper call on layout line 1 names "local-only", which is not a defined template`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("over the merged namespace the same external call resolves", func(t *testing.T) {
		// The contrast for the previous case: in the two-layer merge the local
		// layer carries local-only, so the external template's call resolves
		// and the merged pass does not re-check it (the external layer is only
		// self-contained-checked when loaded standalone).
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSlide(ext, "ext-slide", "", `{{ section "local-only" }}`)
			},
			func(loc *LibraryLayer) { layeredSection(loc, "local-only", "", "") },
		)
		if err := validateMergedLibrary(t, lib); err != nil {
			t.Fatalf("Validate() error = %v, want nil: the merged namespace carries local-only", err)
		}
	})

	t.Run("a required field of an external section is enforced for a local caller", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) { layeredSection(ext, "ext-footer", requiredTitle, "") },
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-footer" }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1 omits required field "title" of section template "ext-footer", which has no default`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("the required field is satisfied when the local caller supplies it", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) { layeredSection(ext, "ext-footer", requiredTitle, "") },
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-footer" (dict "title" "Hi") }}`)
			},
		)
		if err := validateMergedLibrary(t, lib); err != nil {
			t.Fatalf("Validate() error = %v, want nil", err)
		}
	})

	t.Run("an undeclared dict key of an external section is rejected for a local caller", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) { layeredSection(ext, "ext-footer", optionalTitle, "") },
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-footer" (dict "nope" "v") }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1 supplies field "nope", which section template "ext-footer" does not declare`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("a min>0 child section of an external target is rejected for a local caller", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-child", "", "")
				layeredSection(ext, "ext-parent", childMin, "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1 names section template "ext-parent", which declares child section "widgets" with a minimum of 1; the section helper passes no child sections`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})
}

// TestRegistryCheckSectionsChildChecksAcrossMergedLayers proves
// Registry.checkSections applies the load-time child checks
// (requirements.requirement.section-helper-load-checks,
// requirement.child-constructor, requirement.children-forwarding,
// requirement.nested-section-validation, requirement.library-layer-references)
// across a two-layer merged Library, exactly as it applies the section-helper
// target/field checks: the CALLING template is a local slide, its target is an
// EXTERNAL section, and the target's declared child sections, accepted
// templates and instantiated templates all resolve through the merged
// namespace, so every child failure is attributed to the local caller — the
// same shape as TestRegistryCheckSectionsAcrossMergedLayers just above.
//
// It covers each static child rejection the child constructor introduces:
//
//   - a child naming a section the external target does not declare;
//   - a `template` selector the external declaration does not accept;
//   - a literal child field the instantiated template does not declare, and an
//     undefaulted required field the child omits;
//   - a literal body heading that names a declared child section, both on the
//     external target itself and on an instantiated child template;
//   - a statically-known literal child set below the external target's per-
//     section minimum and above its maximum; and
//
// it proves the passing side: a literal child set exactly within the declared
// bounds loads, and — the cross-layer case — a child whose instantiated
// template lives in the LOCAL layer while its declaring target lives in the
// EXTERNAL layer resolves in the merged namespace. For that last case the
// resolved child's own SectionCall.Layer is asserted to be the instantiated
// template's owning local layer, independently of the external target's layer,
// and Registry.LayerOf pins both provenances (library-layer-references).
//
// The two-layer inputs are hand-built exactly as the merged-layer test above:
// testLayer/addTestSlide/addTestSection give each layer loadLibrary's shape and
// mergeLibraryLayers unions them. Each fixture carries a parseable manifest
// (ManifestBytes) and layout (LayoutText), so NewRegistryFromLibrary registers
// it with no content filesystem; registerLibraryTemplate defers the example
// check, so a declared minimum does not trip the separate min-repeat rule.
// Everything is in-process and runs under -short.
func TestRegistryCheckSectionsChildChecksAcrossMergedLayers(t *testing.T) {
	// parentDecl builds an external target manifest declaring one child section
	// "widgets" that accepts accepted and carries the given repeat bounds (0 is
	// omitted: min 0 and an absent max both mean no bound).
	parentDecl := func(accepted string, min, max int) string {
		var b strings.Builder
		b.WriteString("sections:\n  - name: widgets\n    accepted: [")
		b.WriteString(accepted)
		b.WriteString("]\n")
		if min != 0 {
			fmt.Fprintf(&b, "    min: %d\n", min)
		}
		if max != 0 {
			fmt.Fprintf(&b, "    max: %d\n", max)
		}
		return b.String()
	}

	t.Run("a child naming a section the external target does not declare is rejected", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-widget", "", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 0, 0), "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "ghost") }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1: child names section "ghost", which section template "ext-parent" does not declare`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("a child selector the external declaration does not accept is rejected", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-widget", "", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 0, 0), "")
			},
			func(loc *LibraryLayer) {
				// other is a defined local section, yet it is not among the
				// external declaration's accepted set, so the rejection is the
				// acceptance check and not an undefined name.
				layeredSection(loc, "other", "", "")
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "widgets" (dict "template" "other")) }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1: child section "widgets" of section template "ext-parent" does not accept template "other" (accepts ext-widget)`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("a literal child field the instantiated template does not declare is rejected", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-widget", "fields:\n  - name: title\n    type: text\n", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 0, 0), "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "widgets" (dict "nope" "v")) }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1: child section "widgets" supplies field "nope", which section template "ext-widget" does not declare`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("an undefaulted required field of the instantiated template is enforced", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-widget", "fields:\n  - name: title\n    type: text\n    required: true\n", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 0, 0), "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "widgets") }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1: child section "widgets" omits required field "title" of section template "ext-widget", which has no default`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("a literal body heading naming the external target's child section is rejected", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-widget", "", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 0, 0), "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" (dict) "# widgets" }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1 supplies a body whose heading names child section "widgets" of section template "ext-parent"; the section helper passes no child sections`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("a child body heading naming the instantiated template's child section is rejected", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-gadget", "", "")
				layeredSection(ext, "ext-widget", "sections:\n  - name: gadgets\n    accepted: [ext-gadget]\n", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 0, 0), "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "widgets" nil "## gadgets") }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1: child section "widgets" supplies a body whose heading names child section "gadgets" of section template "ext-widget"; a child body carries no child sections`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("a statically-known child set below the external target's min is rejected", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-widget", "", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 2, 0), "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "widgets") }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1: section template "ext-parent" declares child section "widgets" with a minimum of 2, but the call passes 1`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("a statically-known child set above the external target's max is rejected", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-widget", "", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 0, 1), "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "widgets") (child "widgets") }}`)
			},
		)
		want := `template "local-slide": section helper call on layout line 1: section template "ext-parent" declares child section "widgets" with a maximum of 1, but the call passes 2`
		if err := validateMergedLibrary(t, lib); err == nil || err.Error() != want {
			t.Fatalf("Validate() error = %v, want %q", err, want)
		}
	})

	t.Run("a literal child set exactly within the external target's bounds loads", func(t *testing.T) {
		lib := layeredLibrary(
			func(ext *LibraryLayer) {
				layeredSection(ext, "ext-widget", "", "")
				layeredSection(ext, "ext-parent", parentDecl("ext-widget", 2, 2), "")
			},
			func(loc *LibraryLayer) {
				layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "widgets") (child "widgets") }}`)
			},
		)
		if err := validateMergedLibrary(t, lib); err != nil {
			t.Fatalf("Validate() error = %v, want nil: the literal child set is exactly at the declared min and max", err)
		}
	})

	t.Run("a child whose instantiated template lives in the other layer resolves in the merged namespace", func(t *testing.T) {
		// The target is EXTERNAL and accepts a child template that lives in the
		// LOCAL layer: the child's instantiated template is cross-layer. The
		// merged namespace resolves it, and the resolved child records the
		// instantiated template's OWN owning layer, not the target's.
		ext := testLayer("/deck/external")
		loc := testLayer("/deck/templates")
		layeredSection(ext, "ext-parent", parentDecl("local-widget", 0, 0), "")
		layeredSection(loc, "local-widget", "", "")
		layeredSlide(loc, "local-slide", "", `{{ section "ext-parent" nil nil (child "widgets") }}`)
		lib := mergeLibraryLayers([]*LibraryLayer{ext, loc})

		reg, err := NewRegistryFromLibrary(lib)
		if err != nil {
			t.Fatalf("NewRegistryFromLibrary() error = %v, want nil", err)
		}
		if err := reg.Validate(); err != nil {
			t.Fatalf("Validate() error = %v, want nil: the child's instantiated template resolves in the merged namespace", err)
		}

		if got := reg.LayerOf("local-widget"); got != loc {
			t.Errorf("LayerOf(local-widget) = %p, want the local layer %p", got, loc)
		}
		if got := reg.LayerOf("ext-parent"); got != ext {
			t.Errorf("LayerOf(ext-parent) = %p, want the external layer %p", got, ext)
		}

		// Resolution agrees with the load check: the child instantiates the
		// local template and carries the local layer on its own SectionCall,
		// independently of the external target's layer.
		call, err := ResolveSectionCallLayered(reg.Lookup, reg.LayerOf, "ext-parent", nil, "", nil, &ChildRequest{Section: "widgets"})
		if err != nil {
			t.Fatalf("ResolveSectionCallLayered() error = %v, want nil", err)
		}
		if len(call.Children) != 1 {
			t.Fatalf("len(call.Children) = %d, want the one resolved child", len(call.Children))
		}
		child := call.Children[0].Call
		if got := child.Item["template"]; got != "local-widget" {
			t.Errorf("child .item.template = %v, want the cross-layer instantiated template %q", got, "local-widget")
		}
		if child.Layer != loc {
			t.Errorf("child SectionCall.Layer = %p, want the instantiated template's own local layer %p", child.Layer, loc)
		}
	})
}
