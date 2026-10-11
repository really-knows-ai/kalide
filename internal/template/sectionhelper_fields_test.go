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

// ---------------------------------------------------------------------------
// plan.phase-01.task-16: child fields at load time.
//
// A literal `child "<section>" [fields]` supplies fields to the template its
// declaration instantiates, checked statically exactly like the `section`
// helper's own literal `dict` (field-rules, child-constructor, dict-list-helpers):
//
//   - a literal key the instantiated template does not declare is rejected
//     naming the key and the instantiated template;
//   - the reserved `template` selector key is consumed before the undeclared-key
//     check, so it is never counted as a field even though no instantiated
//     template declares one by that name;
//   - a required field carrying a default is satisfied when the child omits it,
//     an undefaulted required field the child omits is rejected, and one the
//     child supplies is satisfied; and
//   - a non-literal fields expression — a variable, `.raw`, `.item.parent` or a
//     range value — is a permitted pass-through that loads, its keys checked
//     against the instantiated template when the target renders.
//
// Every failing case is a Validate-stage failure: Register never inspects a
// layout, so the fixture registers cleanly and it is Registry.checkSections,
// reached through Registry.Validate, that reports the specific error — the exact
// text is asserted so the named key and the named required field are pinned.
// childSectionTarget's accepted `widgets-tpl` template declares no fields, so a
// case that needs a declared field sets it on the returned accepted fixture,
// exactly as regSection lets a registry fixture carry fields. Everything is
// in-process with no content filesystem, so the tests run under -short.

// TestSectionHelperChildLiteralFieldChecks is table-driven over the literal
// child-field shapes: each fixture registers cleanly and Validate reports the
// specific checkSections error for the violating cases, while the satisfying
// shapes validate. The instantiated `widgets-tpl` template's fields are set on
// childSectionTarget's accepted fixture.
func TestSectionHelperChildLiteralFieldChecks(t *testing.T) {
	tests := []struct {
		name    string
		layout  string
		fields  []Field        // the fields the instantiated widgets-tpl template declares
		example map[string]any // the instantiated template's own example frontmatter, if its required fields need one
		want    string
	}{
		{
			name:   "undeclared literal dict key fails naming the key",
			layout: `{{ section "footer" nil nil (child "widgets" (dict "nope" "v")) }}`,
			fields: []Field{{Name: "title", Type: FieldText}},
			want:   `template "host": section helper call on layout line 1: child section "widgets" supplies field "nope", which section template "widgets-tpl" does not declare`,
		},
		{
			name:   "undefaulted required field omitted fails",
			layout: `{{ section "footer" nil nil (child "widgets") }}`,
			fields: []Field{{Name: "title", Type: FieldText, Required: true}},
			want:   `template "host": section helper call on layout line 1: child section "widgets" omits required field "title" of section template "widgets-tpl", which has no default`,
		},
		{
			name:    "undefaulted required field supplied is satisfied",
			layout:  `{{ section "footer" nil nil (child "widgets" (dict "title" "Hi")) }}`,
			fields:  []Field{{Name: "title", Type: FieldText, Required: true}},
			example: map[string]any{"title": "Example"},
		},
		{
			name:    "required field with a default omitted is satisfied",
			layout:  `{{ section "footer" nil nil (child "widgets") }}`,
			fields:  []Field{{Name: "title", Type: FieldText, Required: true, Default: "Untitled"}},
			example: map[string]any{"title": "Example"},
		},
		{
			name:    "literal empty dict with a defaulted required field is satisfied",
			layout:  `{{ section "footer" nil nil (child "widgets" (dict)) }}`,
			fields:  []Field{{Name: "title", Type: FieldText, Required: true, Default: "Untitled"}},
			example: map[string]any{"title": "Example"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			target, accepted := childSectionTarget("footer", "widgets", 0)
			accepted.Fields = tt.fields
			accepted.Example = Example{Frontmatter: tt.example}

			err := validateRegistered(t, host, target, accepted)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("Validate() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// TestSectionHelperChildTemplateSelectorConsumed proves the reserved `template`
// key is captured as part of the child's literal fields yet consumed before the
// undeclared-key check: the layout validates even though the instantiated
// template declares no field by that name, and HelperCalls shows the selector
// present in the extracted child fields — so the check must have removed it, not
// failed to see it.
func TestSectionHelperChildTemplateSelectorConsumed(t *testing.T) {
	const layout = `{{ section "footer" nil nil (child "widgets" (dict "template" "widgets-tpl" "title" "Hi")) }}`
	host := regLayout("host", layout)
	target, accepted := childSectionTarget("footer", "widgets", 0)
	accepted.Fields = []Field{{Name: "title", Type: FieldText}}

	if err := validateRegistered(t, host, target, accepted); err != nil {
		t.Fatalf("Validate() error = %v, want nil: the template selector is consumed, not counted as an undeclared field", err)
	}

	calls, err := NewSection(nil).HelperCalls(host)
	if err != nil {
		t.Fatalf("HelperCalls() error = %v, want nil", err)
	}
	if len(calls) != 1 || len(calls[0].Children) != 1 {
		t.Fatalf("HelperCalls() = %+v, want one call carrying one child", calls)
	}
	child := calls[0].Children[0]
	if !child.Literal || !child.FieldsLiteral {
		t.Fatalf("extracted child = %+v, want a literal child with literal fields", child)
	}
	if got, ok := child.Fields["template"]; !ok || got != "widgets-tpl" {
		t.Errorf("extracted child fields = %v, want the selector %q present", child.Fields, "widgets-tpl")
	}
	if got := child.Fields["title"]; got != "Hi" {
		t.Errorf("extracted child fields = %v, want the declared title %q", child.Fields, "Hi")
	}
}

// TestSectionHelperChildNonLiteralFieldsPassThrough proves a non-literal child
// fields expression is a permitted pass-through, not a load error: each layout
// validates and its single extracted child carries the non-literal markers
// (Literal false, NonLiteral true, HasFields true, FieldsLiteral false,
// Fields nil) while its literal section name is still captured, so the registry
// knows it cannot check the keys until the target renders.
func TestSectionHelperChildNonLiteralFieldsPassThrough(t *testing.T) {
	tests := []struct {
		name   string
		layout string
	}{
		{"variable", `{{ $f := dict "k" "v" }}{{ section "footer" nil nil (child "widgets" $f) }}`},
		{"dot raw", `{{ section "footer" nil nil (child "widgets" .raw) }}`},
		{"item parent", `{{ section "footer" nil nil (child "widgets" .item.parent) }}`},
		{"range value", `{{ range .items }}{{ section "footer" nil nil (child "widgets" .) }}{{ end }}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			target, accepted := childSectionTarget("footer", "widgets", 0)

			if err := validateRegistered(t, host, target, accepted); err != nil {
				t.Fatalf("Validate() error = %v, want nil: a non-literal child fields expression is a permitted pass-through", err)
			}

			calls, err := NewSection(nil).HelperCalls(host)
			if err != nil {
				t.Fatalf("HelperCalls() error = %v, want nil", err)
			}
			if len(calls) != 1 || len(calls[0].Children) != 1 {
				t.Fatalf("HelperCalls() = %+v, want one call carrying one child", calls)
			}
			child := calls[0].Children[0]
			if child.Literal || !child.NonLiteral || child.Forward {
				t.Errorf("extracted child = %+v, want Literal=false NonLiteral=true Forward=false", child)
			}
			if !child.HasFields || child.FieldsLiteral || child.Fields != nil {
				t.Errorf("extracted child = %+v, want HasFields=true FieldsLiteral=false Fields=nil", child)
			}
			if child.Section != "widgets" {
				t.Errorf("extracted child Section = %q, want the literal section %q captured", child.Section, "widgets")
			}
		})
	}
}
