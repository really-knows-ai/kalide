package template

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-02.task-9: the
// composition law a `section` helper call joins at load time
// (requirements.requirement.section-helper-load-checks, AC8;
// requirement template-composition).
//
// A helper call is a composition reference, so Section.Walk follows the
// helper-call edges (HelperRefs) alongside the declared-section edges: two
// section templates whose layouts each call `{{ section "<the other>" }}` close
// a reference cycle and are rejected at load as *SectionCycleError, and a mixed
// cycle — one declared-section edge plus one helper edge — is rejected the same
// way. A layout calling its own template is a cycle too.
//
// The two edge kinds are NOT equivalent for the six-heading-level depth bound
// (requirements.constraint.section-depth-limit): a helper edge is a composition
// reference, not a heading, so it sits at the caller's level and adds no depth,
// while a declared-section acceptance is a heading and descends one level. The
// tests pin that split by loading a helper chain longer than six templates that
// would fail if helper edges counted, and by keeping the declared seven-deep
// chain failing as *SectionDepthError.
//
// Every failure is a Validate-stage failure — Register never inspects a layout,
// so each fixture registers cleanly and the composition walk produces the error
// — and the error is checked with errors.As so the wrapped chain survives
// Validate's `template "<name>":` prefix. Everything is in-process with no
// content filesystem, so the tests run under -short. It reuses the registry
// fixtures declared in registry_test.go (regSection, buildAndValidate,
// chainSections) and provides the section-usage layout fixture these cycle
// cases need.

// regSectionLayout returns a section-usage fixture whose layout source is
// layout. The cycle and helper-depth cases need a section template that both
// targets and makes `section` helper calls; regLayout
// (sectionhelper_target_test.go) is the slide-usage counterpart.
func regSectionLayout(name, layout string) *Template {
	tmpl := regSection(name)
	tmpl.Layout = Layout{Name: name, Text: layout}
	return tmpl
}

// helperChainSections returns a chain of section templates named names in which
// each template's layout calls `{{ section "<next>" }}` through the helper and
// the last makes no call. Every edge is a helper edge, so none is a heading.
func helperChainSections(names ...string) []*Template {
	ts := make([]*Template, len(names))
	for i, name := range names {
		layout := ""
		if i+1 < len(names) {
			layout = fmt.Sprintf(`{{ section %q }}`, names[i+1])
		}
		ts[i] = regSectionLayout(name, layout)
	}
	return ts
}

// mixedCycle returns two section templates, a and b, that close one reference
// cycle from one declared-section edge and one helper edge. With helperFirst
// the helper edge is a → b (b declares `slot` accepting a); otherwise the
// declared edge is a → b (a declares `slot` accepting b, named b calls a).
// Either way the walk from the alphabetically first root, a, reports
// a → b → a.
func mixedCycle(helperFirst bool) []*Template {
	a := regSection("a")
	b := regSection("b")
	if helperFirst {
		a.Layout = Layout{Name: "a", Text: `{{ section "b" }}`}
		b.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"a"}}}
	} else {
		a.Sections = []SectionDecl{{Name: "slot", Accepted: []string{"b"}}}
		b.Layout = Layout{Name: "b", Text: `{{ section "a" }}`}
	}
	return []*Template{a, b}
}

// TestSectionHelperCompositionCycle proves a helper-closed reference cycle and
// a mixed declared/helper cycle are rejected at load as *SectionCycleError, and
// that the error's chain names every template on the cycle — including the
// repeated name that closes it. A layout calling its own template is the
// shortest cycle. Each case registers cleanly and only Validate reports the
// cycle, so the exact rendered message is asserted as well as the parsed chain.
func TestSectionHelperCompositionCycle(t *testing.T) {
	const cycleAB = `template "a": section template reference cycle: sections/a → sections/b → sections/a`

	tests := []struct {
		name      string
		templates []*Template
		wantPath  []string
		want      string
	}{
		{
			name: "two helper calls close a cycle",
			templates: []*Template{
				regSectionLayout("a", `{{ section "b" }}`),
				regSectionLayout("b", `{{ section "a" }}`),
			},
			wantPath: []string{"a", "b", "a"},
			want:     cycleAB,
		},
		{
			name:      "a helper edge and a declared edge close a cycle",
			templates: mixedCycle(true),
			wantPath:  []string{"a", "b", "a"},
			want:      cycleAB,
		},
		{
			name:      "a declared edge and a helper edge close a cycle",
			templates: mixedCycle(false),
			wantPath:  []string{"a", "b", "a"},
			want:      cycleAB,
		},
		{
			name: "a layout calling its own template is a cycle",
			templates: []*Template{
				regSectionLayout("a", `{{ section "a" }}`),
			},
			wantPath: []string{"a", "a"},
			want:     `template "a": section template reference cycle: sections/a → sections/a`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := buildAndValidate(tt.templates...)
			if err == nil {
				t.Fatal("Validate() = nil, want *SectionCycleError")
			}
			var cycle *SectionCycleError
			if !errors.As(err, &cycle) {
				t.Fatalf("Validate() error = %v, want *SectionCycleError", err)
			}
			if !reflect.DeepEqual(cycle.Path, tt.wantPath) {
				t.Errorf("cycle.Path = %v, want %v", cycle.Path, tt.wantPath)
			}
			if err.Error() != tt.want {
				t.Errorf("Validate() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// TestSectionHelperDepthBoundNotExtendedByHelpers proves the depth half of the
// composition law: a helper edge is a reference, not a heading, so it does not
// count toward the six-heading-level bound
// (requirements.constraint.section-depth-limit). A helper chain of eight
// templates — seven helper edges, one more level than the bound would permit if
// they counted — loads, and the walk really traverses it (it reaches every
// later template). A declared-section chain of seven, whose every edge is a
// heading, still fails as *SectionDepthError with the whole chain.
func TestSectionHelperDepthBoundNotExtendedByHelpers(t *testing.T) {
	t.Run("a helper chain deeper than six heading levels loads", func(t *testing.T) {
		chain := helperChainSections("h0", "h1", "h2", "h3", "h4", "h5", "h6", "h7")

		if err := buildAndValidate(chain...); err != nil {
			t.Fatalf("Validate() error = %v, want nil: a section-helper edge is not a heading and adds no depth", err)
		}

		// Prove the helper edges were traversed, not skipped: the walk reaches
		// every later template and reports no depth failure.
		r := NewRegistry(nil)
		for _, tmpl := range chain {
			if err := r.Register(tmpl); err != nil {
				t.Fatalf("Register(%q) error = %v, want nil", tmpl.Name, err)
			}
		}
		got, err := NewSection(r.Lookup).Walk(chain[0])
		if err != nil {
			t.Fatalf("Walk(%q) error = %v, want nil", chain[0].Name, err)
		}
		if want := len(chain) - 1; len(got) != want {
			t.Errorf("Walk(%q) = %d templates, want %d helper-reachable templates", chain[0].Name, len(got), want)
		}
	})

	t.Run("a declared chain of seven still fails as *SectionDepthError", func(t *testing.T) {
		// Every edge here is a declared-section acceptance, i.e. a heading, so
		// the seventh level crosses the bound exactly as before the helper
		// edges joined the walk.
		chain := chainSections("a", "b", "c", "d", "e", "f", "g")
		err := buildAndValidate(chain...)
		var depth *SectionDepthError
		if !errors.As(err, &depth) {
			t.Fatalf("Validate() error = %v, want *SectionDepthError", err)
		}
		if depth.Limit != sectionDepthLimit {
			t.Errorf("depth.Limit = %d, want %d", depth.Limit, sectionDepthLimit)
		}
		if want := []string{"a", "b", "c", "d", "e", "f", "g"}; !reflect.DeepEqual(depth.Path, want) {
			t.Errorf("depth.Path = %v, want %v", depth.Path, want)
		}
	})
}
