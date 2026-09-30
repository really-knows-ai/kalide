package template

import "testing"

// This file is the unit-test deliverable for plan.phase-02.task-6: section
// helper target resolution at load time
// (requirements.requirement.section-helper-load-checks, AC1–AC2).
//
// A `{{ section "<name>" }}` call is a static reference, so the same build-time
// pass that validates a template's declared sections resolves the call's target
// among the defined templates and rejects a call that cannot resolve. Every
// case here is a Validate-stage failure — the fixture registers cleanly and it
// is Registry.checkSections, reached through Registry.Validate, that reports it
// — and proves:
//
//   - an unknown target fails with a closest-match suggestion;
//   - an unknown target far from every defined name fails without one;
//   - a slide-usage target fails, because the section helper requires a
//     section-usage template; and
//   - a non-literal target name (e.g. `{{ section .x }}`) is rejected, because
//     the load check needs a target resolved statically by name.
//
// Everything is in-process with no content filesystem: the fixtures carry their
// layout source in Layout.Text, so the tests run under -short. It reuses the
// registry fixtures declared in registry_test.go (regSlide, regSection).

// regLayout returns a slide-usage fixture whose layout source is layout. A
// section-helper call is only visible to the build-time checks through the
// layout text, so these fixtures carry it directly rather than loading it from
// a content filesystem.
func regLayout(name, layout string) *Template {
	tmpl := regSlide(name)
	tmpl.Layout = Layout{Name: name, Text: layout}
	return tmpl
}

// TestSectionHelperTargetResolution is table-driven over the target-resolution
// failures: each fixture registers without error (Register never inspects a
// layout), then Validate reports the specific checkSections error. The exact
// error text is asserted so the closest-match suggestion — and its absence when
// nothing is close — are both pinned.
func TestSectionHelperTargetResolution(t *testing.T) {
	tests := []struct {
		name      string
		templates []*Template
		want      string
	}{
		{
			name: "unknown target suggests the closest defined template",
			templates: []*Template{
				regLayout("host", `{{ section "footerr" }}`),
				regSection("footer"),
			},
			want: `template "host": section helper call on layout line 1 names "footerr", which is not a defined template: did you mean "footer"?`,
		},
		{
			name: "unknown target far from every defined name has no suggestion",
			templates: []*Template{
				regLayout("host", `{{ section "zzzz" }}`),
				regSection("footer"),
			},
			want: `template "host": section helper call on layout line 1 names "zzzz", which is not a defined template`,
		},
		{
			name: "slide-usage target is rejected",
			templates: []*Template{
				regLayout("host", `{{ section "hero" }}`),
				regSlide("hero"),
			},
			want: `template "host": section helper call on layout line 1 names "hero", which is a slide-usage template; the section helper requires a section-usage template`,
		},
		{
			name: "non-literal target name is rejected",
			templates: []*Template{
				regLayout("host", `{{ section .x }}`),
				regSection("footer"),
			},
			want: `template "host": layout line 1: section helper target name must be a string literal, got .x`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry(nil)
			for _, tmpl := range tt.templates {
				if err := r.Register(tmpl); err != nil {
					t.Fatalf("Register(%q) error = %v, want nil (the check fires at Validate, not Register)", tmpl.Name, err)
				}
			}

			err := r.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("Validate() error = %q, want %q", err.Error(), tt.want)
			}

			// The offending template stays registered: the failure is the
			// cross-template target resolution, not a rejected definition.
			if _, ok := r.Lookup("host"); !ok {
				t.Error(`Lookup("host") ok = false, want the host template registered`)
			}
		})
	}
}

// TestSectionHelperTargetResolutionIsValidateStage proves a call naming an
// unknown target is accepted by Register alone and only rejected once the whole
// registry is validated: the check needs the full set of defined templates to
// resolve (and suggest) against.
func TestSectionHelperTargetResolutionIsValidateStage(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.Register(regLayout("host", `{{ section "missing" }}`)); err != nil {
		t.Fatalf("Register(host) error = %v, want nil", err)
	}
	if _, ok := r.Lookup("host"); !ok {
		t.Fatal(`Lookup("host") ok = false, want the template stored by Register`)
	}
	if err := r.Validate(); err == nil {
		t.Fatal("Validate() = nil, want the unresolved target to fail validation")
	}
}
