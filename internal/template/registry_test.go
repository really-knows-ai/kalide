package template

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
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
// kind and fails the test on error.
func mustParseManifest(t *testing.T, name string, kind TemplateKind, manifestYAML string) *Template {
	t.Helper()
	def, err := parseManifest(manifestLibraryTemplate(name, kind, manifestYAML))
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

// assertStringSlice compares a []string against an expected literal, reporting
// both the length and the elements on failure.
func assertStringSlice(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// TestRegistry proves the built-in templates load and validate, their
// structured example data passes the schema-only checks, and the four
// Catalogue methods behave.
func TestRegistry(t *testing.T) {
	t.Run("builtins load, validate and carry content", func(t *testing.T) {
		r, err := Builtins()
		if err != nil {
			t.Fatalf("Builtins() error = %v, want nil", err)
		}

		want := []string{"column", "content", "title"}
		assertStringSlice(t, "TemplateNames()", r.TemplateNames(), want)

		for _, tmpl := range r.Templates() {
			if tmpl.Layout.Name != tmpl.Name {
				t.Errorf("template %q: Layout.Name = %q, want the template name", tmpl.Name, tmpl.Layout.Name)
			}
			if strings.TrimSpace(tmpl.Layout.Text) == "" {
				t.Errorf("template %q: Layout.Text is empty, want the embedded layout", tmpl.Name)
			}
			if strings.TrimSpace(tmpl.Example.Markdown) == "" {
				t.Errorf("template %q: Example.Markdown is empty, want the embedded example slide", tmpl.Name)
			}
			// The structured example must satisfy the schema-only checks
			// (frontmatter/field values and declared section instances).
			if err := r.checkExample(tmpl); err != nil {
				t.Errorf("template %q: checkExample() error = %v, want nil", tmpl.Name, err)
			}
			if res := CheckValues(tmpl.Example.Frontmatter, tmpl, r.Lookup); len(res.Errors) != 0 {
				t.Errorf("template %q: example frontmatter errors = %s, want none",
					tmpl.Name, renderErrors(res.Errors))
			}
		}
	})

	t.Run("builtin usages and example shapes", func(t *testing.T) {
		r, err := Builtins()
		if err != nil {
			t.Fatalf("Builtins() error = %v, want nil", err)
		}

		title, ok := r.Lookup("title")
		if !ok {
			t.Fatal("Lookup(title) = not found")
		}
		if title.Usage != UsageSlide {
			t.Errorf("title.Usage = %q, want %q", title.Usage, UsageSlide)
		}
		if got := title.Example.Frontmatter["title"]; got != "Quarterly Business Review" {
			t.Errorf("title example title = %v, want the structured example value", got)
		}
		if len(title.Example.Sections) != 0 {
			t.Errorf("title example sections = %d, want 0", len(title.Example.Sections))
		}

		content, ok := r.Lookup("content")
		if !ok {
			t.Fatal("Lookup(content) = not found")
		}
		if content.Usage != UsageSlide {
			t.Errorf("content.Usage = %q, want %q", content.Usage, UsageSlide)
		}
		if len(content.Example.Sections) != 2 {
			t.Fatalf("content example sections = %d, want the two declared column instances", len(content.Example.Sections))
		}
		for i, sec := range content.Example.Sections {
			if sec.Name != "columns" || sec.Template != "column" {
				t.Errorf("content example section[%d] = %q/%q, want columns/column", i, sec.Name, sec.Template)
			}
		}

		column, ok := r.Lookup("column")
		if !ok {
			t.Fatal("Lookup(column) = not found")
		}
		if column.Usage != UsageSection {
			t.Errorf("column.Usage = %q, want %q", column.Usage, UsageSection)
		}
	})

	t.Run("LookupSlideTemplate reports usage", func(t *testing.T) {
		r, err := Builtins()
		if err != nil {
			t.Fatalf("Builtins() error = %v, want nil", err)
		}
		tests := []struct {
			name      string
			wantUsage string
			wantOK    bool
		}{
			{"title", "slide", true},
			{"content", "slide", true},
			{"column", "section", true},
			{"missing", "", false},
		}
		for _, tt := range tests {
			usage, ok := r.LookupSlideTemplate(tt.name)
			if usage != tt.wantUsage || ok != tt.wantOK {
				t.Errorf("LookupSlideTemplate(%q) = (%q, %v), want (%q, %v)",
					tt.name, usage, ok, tt.wantUsage, tt.wantOK)
			}
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
		r, err := Builtins()
		if err != nil {
			t.Fatalf("Builtins() error = %v, want nil", err)
		}
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
		r, err := Builtins()
		if err != nil {
			t.Fatalf("Builtins() error = %v, want nil", err)
		}

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
		if usage, ok := reg.LookupSlideTemplate("deck"); !ok || usage != string(UsageSlide) {
			t.Errorf("LookupSlideTemplate(deck) = (%q, %v), want (%q, true)", usage, ok, UsageSlide)
		}
		if usage, ok := reg.LookupSlideTemplate("column"); !ok || usage != string(UsageSection) {
			t.Errorf("LookupSlideTemplate(column) = (%q, %v), want (%q, true)", usage, ok, UsageSection)
		}
		assertStringSlice(t, `SectionNames("deck")`, reg.SectionNames("deck"), []string{"columns"})

		// section frontmatter / slide sections: the example.md's two
		// "columns" instances validated end to end (checkLibraryExamples,
		// step 7) against the column template's own field schema — proven
		// by LoadLibrary having already returned no error above.
	})
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
			want:      "section template reference cycle: cyclea → cycleb → cyclea",
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
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "fields: [\n"))
		libErr := asLibraryError(t, err)
		if libErr.Path == "" {
			t.Error("Path is empty, want the offending template.yaml")
		}
	})

	t.Run("unknown top-level manifest key suggests a valid one", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "descriptoin: x\n"))
		libErr := asLibraryError(t, err)
		if libErr.Suggestion != "description" {
			t.Errorf("Suggestion = %q, want %q", libErr.Suggestion, "description")
		}
	})

	t.Run("unknown field type suggests a valid one", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide,
			"fields:\n  - name: title\n    type: txet\n"))
		libErr := asLibraryError(t, err)
		if libErr.Suggestion != "text" {
			t.Errorf("Suggestion = %q, want %q", libErr.Suggestion, "text")
		}
		if !strings.Contains(libErr.Message, `unknown type "txet"`) {
			t.Errorf("Message = %q, want it to name the unknown type", libErr.Message)
		}
	})

	t.Run("field missing required name", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "fields:\n  - type: text\n"))
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, `missing required key "name"`) {
			t.Errorf("Message = %q, want it to say name is missing", libErr.Message)
		}
	})

	t.Run("field missing required type", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "fields:\n  - name: title\n"))
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, `missing required key "type"`) {
			t.Errorf("Message = %q, want it to say type is missing", libErr.Message)
		}
	})

	t.Run("unknown body mode suggests a valid one", func(t *testing.T) {
		_, err := parseManifest(manifestLibraryTemplate("bad", KindSlide, "body:\n  mode: requird\n"))
		libErr := asLibraryError(t, err)
		if libErr.Suggestion != "required" {
			t.Errorf("Suggestion = %q, want %q", libErr.Suggestion, "required")
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

	t.Run("reserved field name notes and body", func(t *testing.T) {
		for _, name := range []string{"notes", "body"} {
			libErr := libraryRejection(t, "fields:\n  - name: "+name+"\n    type: text\n", "")
			if !strings.Contains(libErr.Message, "reserved") {
				t.Errorf("field %q: Message = %q, want it to say the name is reserved", name, libErr.Message)
			}
		}
	})

	t.Run("reserved section name notes and body", func(t *testing.T) {
		for _, name := range []string{"notes", "body"} {
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
		// A section reference cycle surfaces as *SectionCycleError, not a
		// *LibraryError: checkLibraryBuild only re-positions errors whose
		// text names a template (manifestTemplateErrorName), and a cycle's
		// message names a chain, not a single template.
		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		if err == nil {
			t.Fatal("LoadLibrary() error = nil, want a reference-cycle error")
		}
		var cycle *SectionCycleError
		if !errors.As(err, &cycle) {
			t.Fatalf("LoadLibrary() error = %T, want *SectionCycleError", err)
		}
		if !strings.Contains(cycle.Error(), "reference cycle") {
			t.Errorf("Message = %q, want it to say there is a reference cycle", cycle.Error())
		}
	})

	t.Run("bad variant: enum with no variants", func(t *testing.T) {
		libErr := libraryRejection(t, "fields:\n  - name: mode\n    type: enum\n", "")
		if !strings.Contains(libErr.Message, "no variants") {
			t.Errorf("Message = %q, want it to say the enum field has no variants", libErr.Message)
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
// first failure. Repeated calls are stable.
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
