package template

import (
	"errors"
	"reflect"
	"strings"
	"testing"
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
