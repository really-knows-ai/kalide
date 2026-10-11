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

// ---------------------------------------------------------------------------
// plan.phase-01.task-15: child target/section resolution.
//
// A `section` helper call's trailing child arguments (child.go) name a declared
// section of the call's target and resolve exactly like an authored instance of
// that declaration (requirements.requirement.child-constructor,
// requirements.requirement.section-helper,
// requirements.requirement.nested-section-validation). These tests pin that
// resolution through the same two entry points the load-time path uses:
//
//   - Registry.checkSections, reached through Registry.Validate, statically
//     rejects a literal `child` naming a section the target does not declare —
//     a templates error naming the section and the declaring template — and a
//     literal `child` whose reserved `template` selector names a template its
//     declaration does not accept, naming the section, the declaration and the
//     accepted set; and
//   - ResolveSectionCallLayered resolves a call's children, so each resolved
//     child's own `.item.section` is the declared section it fills while
//     `.item.template` is the instantiated template — distinct values when a
//     `template` selector picks among several accepted templates (item-context).
//
// Everything is in-process with no content filesystem, so the tests run under
// -short. It reuses the registry fixtures declared in registry_test.go
// (regSection), regLayout from this file, and the childSectionTarget /
// validateRegistered / resolveSectionCallResolver helpers declared alongside
// the section-helper tests.

// TestSectionHelperChildTargetResolution is table-driven over the static
// child target/section failures: each fixture registers cleanly (Register never
// inspects a layout), so it is Validate, through checkSections, that reports the
// specific error. The exact text is asserted so both the named section and the
// declaration it fills are pinned. A literal child naming a properly declared
// section validates, so the positive case is covered beside the failures.
func TestSectionHelperChildTargetResolution(t *testing.T) {
	tests := []struct {
		name   string
		layout string
		extra  []*Template
		want   string
	}{
		{
			name:   "a child naming an undeclared section is rejected",
			layout: `{{ section "footer" nil nil (child "ghost") }}`,
			want:   `template "host": section helper call on layout line 1: child names section "ghost", which section template "footer" does not declare`,
		},
		{
			name:   "a child selector the declaration does not accept is rejected",
			layout: `{{ section "footer" nil nil (child "widgets" (dict "template" "other")) }}`,
			extra:  []*Template{regSection("other")},
			want:   `template "host": section helper call on layout line 1: child section "widgets" of section template "footer" does not accept template "other" (accepts widgets-tpl)`,
		},
		{
			name:   "a literal child naming a declared section loads",
			layout: `{{ section "footer" nil nil (child "widgets") }}`,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := regLayout("host", tt.layout)
			target, accepted := childSectionTarget("footer", "widgets", 0)
			templates := append([]*Template{host, target, accepted}, tt.extra...)

			err := validateRegistered(t, templates...)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil: the child names a declared section the target accepts", err)
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

// TestSectionHelperChildItemDescriptor proves the resolved child's own `.item`
// descriptor carries the declared section it fills and the template its
// declaration instantiated, reached through ResolveSectionCallLayered's child
// resolution — the same path the load-time example helper and the renderer
// reuse. The two are asserted to be distinct when a multi-template declaration's
// `template` selector picks a template whose name differs from the section's, so
// neither is mistaken for the other (item-context).
func TestSectionHelperChildItemDescriptor(t *testing.T) {
	t.Run("names the declared section and the single instantiated template", func(t *testing.T) {
		target, accepted := childSectionTarget("footer", "widgets", 0)
		resolve := resolveSectionCallResolver(target, accepted)

		call, err := ResolveSectionCallLayered(resolve, nil, "footer", nil, "", nil, &ChildRequest{Section: "widgets"})
		if err != nil {
			t.Fatalf("ResolveSectionCallLayered() error = %v, want nil", err)
		}
		if len(call.Children) != 1 {
			t.Fatalf("len(call.Children) = %d, want the one resolved child", len(call.Children))
		}
		item := call.Children[0].Call.Item
		if got := item["section"]; got != "widgets" {
			t.Errorf("child .item.section = %v, want the declared section %q", got, "widgets")
		}
		if got := item["template"]; got != "widgets-tpl" {
			t.Errorf("child .item.template = %v, want the instantiated template %q", got, "widgets-tpl")
		}
	})

	t.Run("a selector makes .item.template the selected template, distinct from the section", func(t *testing.T) {
		person := &Template{Name: "person", Usage: UsageSection}
		company := &Template{Name: "company", Usage: UsageSection}
		parent := &Template{
			Name:     "cards",
			Usage:    UsageSection,
			Sections: []SectionDecl{{Name: "card", Accepted: []string{"person", "company"}}},
		}
		resolve := resolveSectionCallResolver(parent, person, company)

		req := &ChildRequest{Section: "card", Fields: map[string]any{"template": "company"}}
		call, err := ResolveSectionCallLayered(resolve, nil, "cards", nil, "", nil, req)
		if err != nil {
			t.Fatalf("ResolveSectionCallLayered() error = %v, want nil", err)
		}
		if len(call.Children) != 1 {
			t.Fatalf("len(call.Children) = %d, want the one resolved child", len(call.Children))
		}
		item := call.Children[0].Call.Item
		if got := item["section"]; got != "card" {
			t.Errorf("child .item.section = %v, want the named declared section %q", got, "card")
		}
		if got := item["template"]; got != "company" {
			t.Errorf("child .item.template = %v, want the selected template %q", got, "company")
		}
	})
}
