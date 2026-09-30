package template

import (
	"reflect"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-02.task-8: the
// child-section and body constraints a `section` helper call imposes on its
// target at load time
// (requirements.requirement.section-helper-load-checks, AC5–AC7).
//
// A helper passes no child sections through to its target, so the load-time
// check rejects a target whose composition cannot be honored:
//
//   - a target declaring a child section with min > 0 is rejected, because the
//     helper cannot satisfy the minimum; the same target with min 0 loads;
//   - a literal body whose heading names a declared child section of the target
//     is rejected — at any heading depth — because that heading would be a
//     child instance the helper does not pass; a heading inside a fenced code
//     block is not a heading and must not count; and
//   - a non-literal body expression (a variable, a range value or a
//     `.raw.body` pass-through) is a permitted pass-through and loads, its body
//     checked against the target's body rule when the target renders.
//
// Every failing case is a Validate-stage failure: Register never inspects a
// layout, so the fixture registers cleanly and it is Registry.checkSections,
// reached through Registry.Validate, that reports the specific error — the
// exact text is asserted so the child section or the body heading is pinned.
// A passing target carries no required fields, so the step-5 structured-example
// check that Validate also runs stays silent. Everything is in-process with no
// content filesystem and no ports, so the tests run under -short. It reuses the
// registry fixtures declared in registry_test.go (regSection) and regLayout
// from sectionhelper_target_test.go.

// childSectionTarget returns a section-usage fixture named name that declares a
// single child section child, accepting the section-usage template
// child+"-tpl" with the given minimum, plus that accepted child fixture. A
// helper call targets the returned target; the accepted child is returned
// separately because the registry's declared-section half of checkSections
// requires every accepted template to resolve, even for the case whose helper
// call is expected to load.
func childSectionTarget(name, child string, min int) (target, accepted *Template) {
	acceptedName := child + "-tpl"
	target = &Template{
		Name:  name,
		Usage: UsageSection,
		Sections: []SectionDecl{{
			Name:     child,
			Accepted: []string{acceptedName},
			Min:      min,
		}},
	}
	return target, regSection(acceptedName)
}

// validateRegistered registers every fixture and returns Validate's error,
// failing the test if any Register call itself fails: Register never inspects a
// layout, so a non-nil Validate error is the check under test and not a
// rejected definition.
func validateRegistered(t *testing.T, templates ...*Template) error {
	t.Helper()
	r := NewRegistry(nil)
	for _, tmpl := range templates {
		if err := r.Register(tmpl); err != nil {
			t.Fatalf("Register(%q) error = %v, want nil (the check fires at Validate)", tmpl.Name, err)
		}
	}
	return r.Validate()
}

// TestSectionHelperTargetChildSectionMin proves the min-repeat half of the
// child-section constraint: a target that declares a child section with min > 0
// is rejected at load — the section helper passes no child sections, so that
// minimum can never be met — while the identical target with min 0 validates
// cleanly.
func TestSectionHelperTargetChildSectionMin(t *testing.T) {
	tests := []struct {
		name string
		min  int
		want string
	}{
		{
			name: "child section with min > 0 is rejected",
			min:  1,
			want: `template "host": section helper call on layout line 1 names section template "footer", which declares child section "widgets" with a minimum of 1; the section helper passes no child sections`,
		},
		{
			name: "child section with min 0 loads",
			min:  0,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", `{{ section "footer" }}`)
			target, accepted := childSectionTarget("footer", "widgets", tt.min)

			err := validateRegistered(t, host, target, accepted)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil: a child section with min 0 requires no instances, so the helper passes none", err)
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

// TestSectionHelperLiteralBodyChildHeading is table-driven over literal-body
// headings: a heading naming a declared child section of the target is
// rejected whatever its depth, while a heading naming a section the target does
// not declare, a heading inside a fenced code block (not a heading), and a body
// with no heading at all all load.
func TestSectionHelperLiteralBodyChildHeading(t *testing.T) {
	const wantWidgets = `template "host": section helper call on layout line 1 supplies a body whose heading names child section "widgets" of section template "footer"; the section helper passes no child sections`

	tests := []struct {
		name   string
		layout string
		want   string
	}{
		{
			name:   "level-1 heading naming a declared child section is rejected",
			layout: `{{ section "footer" (dict) "# widgets" }}`,
			want:   wantWidgets,
		},
		{
			name:   "deeper heading naming a declared child section is rejected",
			layout: `{{ section "footer" (dict) "### widgets" }}`,
			want:   wantWidgets,
		},
		{
			name:   "deepest heading naming a declared child section is rejected",
			layout: `{{ section "footer" (dict) "###### widgets" }}`,
			want:   wantWidgets,
		},
		{
			name:   "heading naming an undeclared section loads",
			layout: `{{ section "footer" (dict) "## other" }}`,
			want:   "",
		},
		{
			// The layout text carries a `\n` escape so the decoded body is
			// three lines: a fence, the heading, a fence. A `## widgets`
			// inside a fenced code block is code, not a heading.
			name:   "heading naming a declared child section inside a code fence is ignored",
			layout: "{{ section \"footer\" (dict) \"```\\n## widgets\\n```\" }}",
			want:   "",
		},
		{
			name:   "body with no heading loads",
			layout: `{{ section "footer" (dict) "just prose" }}`,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			target, accepted := childSectionTarget("footer", "widgets", 0)

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

// TestSectionHelperNonLiteralBodyPassThrough proves a non-literal body
// expression is a permitted pass-through, not a load error: each layout
// validates and its single extracted call carries the non-literal body markers
// (HasBody true, BodyLiteral false, Body "") so the registry knows the body
// cannot be inspected until the target renders. It covers a variable, a range
// value and a `.raw.body` pass-through, and a literal `dict` fields argument
// paired with a non-literal body to show the two arguments are marked
// independently.
func TestSectionHelperNonLiteralBodyPassThrough(t *testing.T) {
	tests := []struct {
		name   string
		layout string
		target *Template
		want   HelperCall
	}{
		{
			name:   "item.parent fields and .raw.body body",
			layout: `{{ section "footer" .item.parent .raw.body }}`,
			target: regSection("footer"),
			want:   HelperCall{Name: "footer", HasFields: true, HasBody: true, Template: "host", Line: 1},
		},
		{
			name:   "variable body",
			layout: `{{ $b := "x" }}{{ section "footer" .raw $b }}`,
			target: regSection("footer"),
			want:   HelperCall{Name: "footer", HasFields: true, HasBody: true, Template: "host", Line: 1},
		},
		{
			name:   "range value body",
			layout: `{{ range .items }}{{ section "footer" .raw . }}{{ end }}`,
			target: regSection("footer"),
			want:   HelperCall{Name: "footer", HasFields: true, HasBody: true, Template: "host", Line: 1},
		},
		{
			name:   "literal dict fields with a non-literal body",
			layout: `{{ section "footer" (dict "k" "v") .raw.body }}`,
			target: regSection("footer", Field{Name: "k", Type: FieldText}),
			want: HelperCall{
				Name:          "footer",
				HasFields:     true,
				FieldsLiteral: true,
				FieldKeys:     []string{"k"},
				HasBody:       true,
				Template:      "host",
				Line:          1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			if err := validateRegistered(t, host, tt.target); err != nil {
				t.Fatalf("Validate() error = %v, want nil: a non-literal body expression is a permitted pass-through", err)
			}

			calls, err := NewSection(nil).HelperCalls(host)
			if err != nil {
				t.Fatalf("HelperCalls() error = %v, want nil", err)
			}
			if len(calls) != 1 {
				t.Fatalf("HelperCalls() = %d calls, want 1: %+v", len(calls), calls)
			}
			if !reflect.DeepEqual(calls[0], tt.want) {
				t.Errorf("HelperCalls()[0] = %+v, want %+v", calls[0], tt.want)
			}
		})
	}
}
