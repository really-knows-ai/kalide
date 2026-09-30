package template

import (
	"reflect"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-02.task-7: the literal
// `dict` fields of a `section` helper call at load time
// (requirements.requirement.section-helper-load-checks, AC3–AC4, AC9).
//
// It proves the field half of the static helper analysis: a literal `dict` key
// the target does not declare is rejected naming the key; an omitted required
// field with no default is rejected whether the call passes a literal `dict`
// (which omits it) or omits the fields argument altogether; and a required
// field carrying a default is satisfied. It then proves the pass-through half:
// a non-literal fields expression — a variable, `.raw`, `.item.parent` or a
// range value — is not an error at load, and a layout mixing the `media`,
// `dict` and `section` helpers parses so Section.HelperCalls can extract its
// calls with the literal/non-literal markers, which is what proves HelperCalls
// re-parses Layout.Text with the canonical template.LayoutFuncMap.
//
// Every failing case is a Validate-stage failure: Register never inspects a
// layout, so the fixture registers cleanly and it is Registry.checkSections,
// reached through Registry.Validate, that reports the specific error. A passing
// target carries a valid Example.Frontmatter because Validate also runs the
// step-5 structured-example check, which is orthogonal to the helper-field
// check under test. Everything is in-process with no content filesystem, so the
// tests run under -short. It reuses the registry fixtures declared in
// registry_test.go (regSlide, regSection) and regLayout from
// sectionhelper_target_test.go.

// validateHelperFixture registers target then the layout-carrying host and
// returns Validate's error. Register must accept both — a layout is not
// inspected until the cross-template pass — so a non-nil error is
// checkSections' helper-field check and not a rejected definition.
func validateHelperFixture(t *testing.T, host, target *Template) error {
	t.Helper()
	r := NewRegistry(nil)
	for _, tmpl := range []*Template{target, host} {
		if err := r.Register(tmpl); err != nil {
			t.Fatalf("Register(%q) error = %v, want nil (the check fires at Validate)", tmpl.Name, err)
		}
	}
	return r.Validate()
}

// TestSectionHelperLiteralFieldChecks is table-driven over the literal-fields
// rejections: each fixture registers cleanly and Validate reports the specific
// checkSections error, whose exact text is asserted so the named key and the
// named required field are both pinned.
func TestSectionHelperLiteralFieldChecks(t *testing.T) {
	tests := []struct {
		name   string
		layout string
		target *Template
		want   string
	}{
		{
			name:   "undeclared literal dict key fails naming the key",
			layout: `{{ section "footer" (dict "nope" "v") }}`,
			target: regSection("footer", Field{Name: "title", Type: FieldText}),
			want:   `template "host": section helper call on layout line 1 supplies field "nope", which section template "footer" does not declare`,
		},
		{
			name:   "literal dict omitting an undefaulted required field fails",
			layout: `{{ section "footer" (dict "subtitle" "v") }}`,
			target: regSection("footer",
				Field{Name: "subtitle", Type: FieldText},
				Field{Name: "title", Type: FieldText, Required: true}),
			want: `template "host": section helper call on layout line 1 omits required field "title" of section template "footer", which has no default`,
		},
		{
			name:   "omitted fields argument with an undefaulted required field fails",
			layout: `{{ section "footer" }}`,
			target: regSection("footer", Field{Name: "title", Type: FieldText, Required: true}),
			want:   `template "host": section helper call on layout line 1 omits required field "title" of section template "footer", which has no default`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHelperFixture(t, regLayout("host", tt.layout), tt.target)
			if err == nil {
				t.Fatalf("Validate() = nil, want error %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("Validate() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// TestSectionHelperRequiredFieldsSatisfied is table-driven over the passing
// required-field shapes: a required field with a default is satisfied whether
// the call omits the fields argument, omits the field from a literal dict, or
// supplies it; and a required field with no default is satisfied when a literal
// dict supplies it.
func TestSectionHelperRequiredFieldsSatisfied(t *testing.T) {
	tests := []struct {
		name   string
		layout string
		target *Template
	}{
		{
			name:   "defaulted required field omitted with the fields argument",
			layout: `{{ section "footer" }}`,
			target: &Template{
				Name:    "footer",
				Usage:   UsageSection,
				Fields:  []Field{{Name: "title", Type: FieldText, Required: true, Default: "Untitled"}},
				Example: Example{Frontmatter: map[string]any{"title": "Example"}},
			},
		},
		{
			name:   "defaulted required field omitted by a literal dict",
			layout: `{{ section "footer" (dict "subtitle" "v") }}`,
			target: &Template{
				Name:  "footer",
				Usage: UsageSection,
				Fields: []Field{
					{Name: "subtitle", Type: FieldText},
					{Name: "title", Type: FieldText, Required: true, Default: "Untitled"},
				},
				Example: Example{Frontmatter: map[string]any{"title": "Example"}},
			},
		},
		{
			name:   "undefaulted required field supplied by a literal dict",
			layout: `{{ section "footer" (dict "title" "Hello") }}`,
			target: &Template{
				Name:    "footer",
				Usage:   UsageSection,
				Fields:  []Field{{Name: "title", Type: FieldText, Required: true}},
				Example: Example{Frontmatter: map[string]any{"title": "Example"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateHelperFixture(t, regLayout("host", tt.layout), tt.target); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

// TestSectionHelperNonLiteralFieldsPassThrough proves a non-literal fields
// expression is a permitted pass-through, not a load error: each layout
// validates and its single extracted call carries the non-literal markers
// (FieldsLiteral false, FieldKeys nil) so the registry knows it cannot check
// the keys until the target renders.
func TestSectionHelperNonLiteralFieldsPassThrough(t *testing.T) {
	tests := []struct {
		name   string
		layout string
	}{
		{"variable", `{{ $f := dict "k" "v" }}{{ section "t" $f }}`},
		{"dot raw", `{{ section "t" .raw }}`},
		{"item parent", `{{ section "t" .item.parent }}`},
		{"range value", `{{ range .items }}{{ section "t" . }}{{ end }}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			target := regSection("t", Field{Name: "k", Type: FieldText})
			if err := validateHelperFixture(t, host, target); err != nil {
				t.Fatalf("Validate() error = %v, want nil: a non-literal fields expression is a permitted pass-through", err)
			}

			calls, err := NewSection(nil).HelperCalls(host)
			if err != nil {
				t.Fatalf("HelperCalls() error = %v, want nil", err)
			}
			if len(calls) != 1 {
				t.Fatalf("HelperCalls() = %d calls, want 1: %+v", len(calls), calls)
			}
			call := calls[0]
			if call.Name != "t" || !call.HasFields || call.FieldsLiteral || call.FieldKeys != nil {
				t.Errorf("HelperCalls()[0] = %+v, want name=t HasFields=true FieldsLiteral=false FieldKeys=nil", call)
			}
		})
	}
}

// TestSectionHelperMixedHelpersLayout proves a layout that mixes the `media`,
// `dict` and `section` helpers parses with the canonical
// template.LayoutFuncMap and validates cleanly, and that Section.HelperCalls
// extracts its `section` calls in source order with the right literal markers:
// the `(dict "k" "v")` call is FieldsLiteral with FieldKeys ["k"], the `.raw`
// pass-through is non-literal. That re-parse is what proves HelperCalls works
// from Layout.Text alone (a registry template stores no parse tree).
func TestSectionHelperMixedHelpersLayout(t *testing.T) {
	const layout = "{{ media \"img/logo.png\" }}\n" +
		"{{ section \"x\" (dict \"k\" \"v\") }}\n" +
		"{{ section \"x\" .raw }}\n"
	host := regLayout("host", layout)
	target := regSection("x", Field{Name: "k", Type: FieldText})

	if err := validateHelperFixture(t, host, target); err != nil {
		t.Fatalf("Validate() error = %v, want nil: a layout mixing media, dict and section loads", err)
	}

	calls, err := NewSection(nil).HelperCalls(host)
	if err != nil {
		t.Fatalf("HelperCalls() error = %v, want nil", err)
	}
	want := []HelperCall{
		{
			Name:          "x",
			HasFields:     true,
			FieldsLiteral: true,
			FieldKeys:     []string{"k"},
			Template:      "host",
			Line:          2,
		},
		{
			Name:      "x",
			HasFields: true,
			Template:  "host",
			Line:      3,
		},
	}
	if len(calls) != len(want) {
		t.Fatalf("HelperCalls() = %d calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i := range want {
		if !reflect.DeepEqual(calls[i], want[i]) {
			t.Errorf("HelperCalls()[%d] = %+v, want %+v", i, calls[i], want[i])
		}
	}
}
