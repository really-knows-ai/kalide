package template

import (
	"reflect"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-02.task-8 and
// plan.phase-01.task-17: the child-section and body constraints a `section`
// helper call imposes on its target at load time
// (requirements.requirement.section-helper-load-checks,
// requirement.nested-section-validation, requirement.body-rules,
// requirement.child-constructor, requirement.children-forwarding).
//
// A `section` call passes child sections only through its trailing `child` and
// `children` arguments, so the load-time check rejects a target whose
// composition cannot be honored:
//
//   - a target declaring a child section with min > 0 is rejected when the call
//     passes no child sections at all, because that minimum can never be met;
//     once the call passes literal `child` arguments for the section the
//     minimum is satisfied (the count is enforced over the literal set when the
//     whole set is statically known — see TestSectionHelperChildLiteralSetRange),
//     and the same target with min 0 loads;
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
// check that Validate also runs stays silent; a fixture whose declared section
// has a non-zero minimum (which the example's own min-repeat rule would reject)
// marks its example Deferred, because that rule is a separate check and only
// checkSections is under test here. Everything is in-process with no content
// filesystem and no ports, so the tests run under -short. It reuses the registry
// fixtures declared in registry_test.go (regSection), regLayout from
// sectionhelper_target_test.go, and the childSectionTarget / validateRegistered
// helpers declared below.

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
// child-section constraint for a target the call leaves entirely unfilled: a
// target that declares a child section with min > 0 is rejected at load — a call
// passing no child sections can never meet that minimum — while the identical
// target with min 0 validates cleanly. It also covers the counterpart the child
// constructors introduce: once the call passes a literal `child` for that
// section the minimum is satisfied and the target loads, so the rejection is
// about the unfilled set, not the declaration by itself.
func TestSectionHelperTargetChildSectionMin(t *testing.T) {
	tests := []struct {
		name   string
		layout string
		min    int
		want   string
	}{
		{
			name:   "child section with min > 0 and no child arguments is rejected",
			layout: `{{ section "footer" }}`,
			min:    1,
			want:   `template "host": section helper call on layout line 1 names section template "footer", which declares child section "widgets" with a minimum of 1; the section helper passes no child sections`,
		},
		{
			name:   "child section with min 0 and no child arguments loads",
			layout: `{{ section "footer" }}`,
			min:    0,
			want:   "",
		},
		{
			name:   "child section with min > 0 satisfied by a literal child loads",
			layout: `{{ section "footer" nil nil (child "widgets") }}`,
			min:    1,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			target, accepted := childSectionTarget("footer", "widgets", tt.min)
			// The example's own min-repeat rule is a separate check
			// (CheckRepeats, step 5); defer it so a non-zero minimum does not
			// make the passing case fail outside the check under test.
			target.Example.Deferred = true

			err := validateRegistered(t, host, target, accepted)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil: the call passes enough literal children for the declared minimum (or the minimum is 0)", err)
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

// TestSectionHelperChildLiteralSetRange proves the min/max half of the
// child-section constraint over a STATICALLY KNOWN literal child set
// (requirements.requirement.section-helper-load-checks,
// requirement.nested-section-validation): when every child argument is a
// literal `child`, each of the target's declared sections has its min/max
// enforced over the literal set, and a violation is a Validate-stage error
// naming the declaration and the target — `child section "widgets"` of `section
// template "footer"` — so the loader can path-qualify it to the calling
// template. The exactly-at-boundary cases load, so the limits are inclusive.
// Every fixture registers cleanly: Register never inspects a layout, so it is
// Registry.checkSections, reached through Registry.Validate, that reports the
// error. It reuses childSectionTarget, validateRegistered and regLayout.
func TestSectionHelperChildLiteralSetRange(t *testing.T) {
	tests := []struct {
		name   string
		layout string
		min    int
		max    int
		want   string
	}{
		{
			name:   "below min fails naming the declaration and the target",
			layout: `{{ section "footer" nil nil (child "widgets") }}`,
			min:    2,
			want:   `template "host": section helper call on layout line 1: section template "footer" declares child section "widgets" with a minimum of 2, but the call passes 1`,
		},
		{
			name:   "above max fails naming the declaration and the target",
			layout: `{{ section "footer" nil nil (child "widgets") (child "widgets") }}`,
			max:    1,
			want:   `template "host": section helper call on layout line 1: section template "footer" declares child section "widgets" with a maximum of 1, but the call passes 2`,
		},
		{
			name:   "exactly at min loads",
			layout: `{{ section "footer" nil nil (child "widgets") (child "widgets") }}`,
			min:    2,
			want:   "",
		},
		{
			name:   "exactly at max loads",
			layout: `{{ section "footer" nil nil (child "widgets") (child "widgets") }}`,
			max:    2,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			target, accepted := childSectionTarget("footer", "widgets", tt.min)
			target.Sections[0].Max = tt.max
			// The example's own min-repeat rule is a separate check
			// (CheckRepeats, step 5); defer it so the at-min case passes
			// checkSections and is not caught by the example rule instead.
			target.Example.Deferred = true

			err := validateRegistered(t, host, target, accepted)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil: the literal child set is within the declared min/max", err)
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

// TestSectionHelperChildMixedSetSkipsRange proves the parent min/max deferral
// for a MIXED child set (requirements.requirement.children-forwarding,
// requirement.nested-section-validation): when any child argument is a
// `children` forwarding or another non-literal expression, the full set is known
// only at render, so the load-time check validates each literal child
// individually and skips the target's min/max entirely. Each case declares a
// minimum well above the one literal child, so the call would be rejected if the
// counts were enforced; it validates instead, and the extracted call still
// carries both child arguments, so the case cannot pass by the child arguments
// having been dropped. It reuses childSectionTarget, validateRegistered and
// regLayout.
func TestSectionHelperChildMixedSetSkipsRange(t *testing.T) {
	tests := []struct {
		name   string
		layout string
	}{
		{
			name:   "literal child plus a children forwarding",
			layout: `{{ section "footer" nil nil (child "widgets") (children "widgets" .raw.widgets) }}`,
		},
		{
			name:   "literal child plus a non-literal child argument",
			layout: `{{ section "footer" nil nil (child "widgets") .items }}`,
		},
		{
			name:   "literal child with non-literal fields plus a literal child",
			layout: `{{ section "footer" nil nil (child "widgets" .raw) (child "widgets") }}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			// A minimum above the one literal child: enforcing it would reject
			// the call, so a nil error proves the count was deferred to render.
			target, accepted := childSectionTarget("footer", "widgets", 5)
			// The example's own min-repeat rule is a separate check
			// (CheckRepeats, step 5); defer it so the deferred min/max is what
			// the nil error attests, not a skipped example.
			target.Example.Deferred = true

			if err := validateRegistered(t, host, target, accepted); err != nil {
				t.Fatalf("Validate() error = %v, want nil: a mixed child set defers the target's min/max to render", err)
			}

			calls, err := NewSection(nil).HelperCalls(host)
			if err != nil {
				t.Fatalf("HelperCalls() error = %v, want nil", err)
			}
			if len(calls) != 1 || len(calls[0].Children) != 2 {
				t.Fatalf("HelperCalls() = %+v, want one call carrying the two child arguments", calls)
			}
		})
	}
}

// TestSectionHelperChildBodyNamesChildSection proves a literal `child` body is
// rejected when a heading names one of the instantiated child template's own
// declared child sections (requirements.requirement.body-rules,
// requirement.child-constructor): a child body carries no child sections either,
// so its nested children pass only through nested `child` arguments, never
// through the body. The failing case is a Validate-stage error naming the child
// section `"widgets"`, the heading's section `"gadgets"` and the instantiated
// template `"widgets-tpl"`; a heading naming a section the child template does
// not declare, a heading inside a code fence (not a heading) and a body with no
// heading all load. The instantiated child template's own declared section makes
// the nested rejection reachable; its accepted `gadgets-tpl` must also resolve,
// so the fixture registers it too.
func TestSectionHelperChildBodyNamesChildSection(t *testing.T) {
	const wantGadgets = `template "host": section helper call on layout line 1: child section "widgets" supplies a body whose heading names child section "gadgets" of section template "widgets-tpl"; a child body carries no child sections`

	tests := []struct {
		name   string
		layout string
		want   string
	}{
		{
			name:   "a child body heading naming the child template's declared section is rejected",
			layout: `{{ section "footer" nil nil (child "widgets" nil "## gadgets") }}`,
			want:   wantGadgets,
		},
		{
			name:   "a deeper child body heading naming the declared section is rejected",
			layout: `{{ section "footer" nil nil (child "widgets" nil "### gadgets") }}`,
			want:   wantGadgets,
		},
		{
			name:   "a child body heading naming an undeclared section loads",
			layout: `{{ section "footer" nil nil (child "widgets" nil "## other") }}`,
			want:   "",
		},
		{
			name:   "a child body heading naming a declared section inside a code fence is ignored",
			layout: "{{ section \"footer\" nil nil (child \"widgets\" nil \"```\\n## gadgets\\n```\") }}",
			want:   "",
		},
		{
			name:   "a child body with no heading loads",
			layout: `{{ section "footer" nil nil (child "widgets" nil "just prose") }}`,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			target, accepted := childSectionTarget("footer", "widgets", 0)
			// The instantiated child template declares its own child section, so
			// a heading naming it is the nested-section rejection under test.
			accepted.Sections = []SectionDecl{{Name: "gadgets", Accepted: []string{"gadgets-tpl"}}}
			gadgetsTpl := regSection("gadgets-tpl")

			err := validateRegistered(t, host, target, accepted, gadgetsTpl)
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
