package template

import (
	"strings"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-07.task-10: the
// `template "<name>":` prefix on a Registry.Validate failure is SINGLE, not
// doubled.
//
// The unconditional half asserts the REACHABLE path: a checkSections /
// HelperCalls failure surfaced at Validate step 2 (the static section-helper
// analysis) carries exactly one `template "<name>":` prefix, and
// manifestTemplateErrorName extracts that name. The prefix is measured with
// strings.Count on the rendered error, not merely by prefix, so a doubled
// prefix fails the test.
//
// The conditional half (the Walk/HelperRefs error path that registry.go's
// step-4 wrap special-cases) is covered by the accompanying unreachability
// proof: see TestValidateWalkHelperRefsPathIsRejectedBeforeWalk.

// TestValidateHelperCallErrorCarriesSingleTemplatePrefix pins the reachable
// single-prefix contract on a checkSections/HelperCalls error at Validate
// step 2. A non-literal `{{ section .name }}` target is rejected while the
// layout is re-parsed (HelperCalls), inside checkSections, so Validate returns
// it before the composition walk of step 4. The error is compared exactly, its
// `template "host":` prefix is counted (must be exactly one), and
// manifestTemplateErrorName must recover "host" so checkLibraryBuild can
// path-qualify it to templates/host/template.yaml.
func TestValidateHelperCallErrorCarriesSingleTemplatePrefix(t *testing.T) {
	r := NewRegistry(nil)
	host := regLayout("host", `{{ section .name }}`)
	if err := r.Register(host); err != nil {
		t.Fatalf("Register(host) error = %v, want nil (Register never inspects a layout)", err)
	}

	err := r.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want the non-literal section-helper target rejected")
	}

	const want = `template "host": layout line 1: section helper target name must be a string literal, got .name`
	if got := err.Error(); got != want {
		t.Errorf("Validate() error = %q, want %q", got, want)
	}

	const prefix = `template "host":`
	if got := strings.Count(err.Error(), prefix); got != 1 {
		t.Errorf("strings.Count(Validate() error, %q) = %d, want exactly 1 (a doubled prefix must fail)", prefix, got)
	}

	name, ok := manifestTemplateErrorName(err)
	if !ok {
		t.Fatalf("manifestTemplateErrorName(%q) ok = false, want true", err.Error())
	}
	if name != "host" {
		t.Errorf("manifestTemplateErrorName(%q) name = %q, want %q", err.Error(), name, "host")
	}
}

// TestValidateWalkHelperRefsPathIsRejectedBeforeWalk is the reachability proof
// for the task's conditional half. Registry.Validate's step-4 walk wrap
// (registry.go) has a branch that returns a HelperRefs error unchanged because
// it already carries the `template "<name>":` prefix; this test shows that
// branch cannot be reached through Validate.
//
// Both candidate triggers the task names are rejected earlier:
//
//   - a non-literal `{{ section .name }}` target fails HelperCalls, which
//     checkSections (step 2) calls for every registered template, and
//   - a layout that does not parse with the canonical LayoutFuncMap also fails
//     that same step-2 HelpersCalls re-parse — and, on the loader path, fails
//     the step-3 layout parse in loadLibraryTemplate before Validate even runs.
//
// So by the time the step-4 walk loop starts, every registered template's
// HelperCalls has already succeeded and HelperRefs (built on HelperCalls)
// cannot fail. The test proves this by asserting that Validate returns exactly
// the step-2 checkSections error for each trigger, and by showing what a direct
// walk would have surfaced (an already-prefixed HelperRefs error) — the input
// the defensive branch is written for, which Validate never reaches because
// step 2 returns first.
func TestValidateWalkHelperRefsPathIsRejectedBeforeWalk(t *testing.T) {
	t.Run("a non-literal target is rejected at step 2", func(t *testing.T) {
		r := NewRegistry(nil)
		host := regLayout("host", `{{ section .name }}`)
		if err := r.Register(host); err != nil {
			t.Fatalf("Register(host) error = %v, want nil", err)
		}

		step2 := r.checkSections(host)
		if step2 == nil {
			t.Fatal("checkSections(host) = nil, want the HelperCalls rejection")
		}
		const wantStep2 = `template "host": layout line 1: section helper target name must be a string literal, got .name`
		if got := step2.Error(); got != wantStep2 {
			t.Errorf("checkSections(host) error = %q, want %q", got, wantStep2)
		}

		validate := r.Validate()
		if validate == nil {
			t.Fatal("Validate() = nil, want the step-2 error")
		}
		if validate.Error() != step2.Error() {
			t.Errorf("Validate() error = %q, want the identical step-2 checkSections error %q", validate.Error(), step2.Error())
		}

		// A direct walk bypassing step 2 does reach HelperRefs and surfaces the
		// same already-prefixed error — the input the step-4 wrap special-cases
		// — which confirms the branch is defensive-only, not dead code, and that
		// Validate pre-empts it at step 2.
		walked, walkErr := NewSection(r.Lookup).Walk(host)
		if walkErr == nil {
			t.Fatalf("Walk(host) = %v, nil; want the direct HelperRefs rejection the step-4 branch is written for", walked)
		}
		if got := walkErr.Error(); got != wantStep2 {
			t.Errorf("Walk(host) error = %q, want %q", got, wantStep2)
		}
		if got := strings.Count(walkErr.Error(), `template "host":`); got != 1 {
			t.Errorf("strings.Count(Walk(host) error, %q) = %d, want exactly 1", `template "host":`, got)
		}
	})

	t.Run("an unparseable layout is rejected at step 2", func(t *testing.T) {
		r := NewRegistry(nil)
		broken := regLayout("broken", `{{ section "footer"`)
		if err := r.Register(broken); err != nil {
			t.Fatalf("Register(broken) error = %v, want nil", err)
		}

		step2 := r.checkSections(broken)
		if step2 == nil {
			t.Fatal("checkSections(broken) = nil, want the HelperCalls re-parse rejection")
		}
		const wantStep2 = `template "broken": parse layout: template: broken:1: unclosed action`
		if got := step2.Error(); got != wantStep2 {
			t.Errorf("checkSections(broken) error = %q, want %q", got, wantStep2)
		}

		validate := r.Validate()
		if validate == nil {
			t.Fatal("Validate() = nil, want the step-2 error")
		}
		if validate.Error() != step2.Error() {
			t.Errorf("Validate() error = %q, want the identical step-2 checkSections error %q", validate.Error(), step2.Error())
		}
		if got := strings.Count(validate.Error(), `template "broken":`); got != 1 {
			t.Errorf("strings.Count(Validate() error, %q) = %d, want exactly 1", `template "broken":`, got)
		}
	})
}
