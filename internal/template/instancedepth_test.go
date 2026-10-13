package template

import "testing"

// This file is the unit-test deliverable for typed-records/plan.phase-06.task-26:
// the shared self-nesting depth rule template.InstanceDepth (context.go), the
// single computation of an instance's `.item.depth` both the render-time and the
// load-time example halves use.
//
// It pins the rule directly — independent of any context builder or adapter —
// so the tests in the sibling files that reach depth through DataContext,
// ResolveChildCall, ResolveSectionCallLayered or the example runner have a
// focused, isolated statement of the contract they rely on.

// TestInstanceDepth pins the shared self-nesting depth rule (item-context,
// bounded-self-nesting) with a table. InstanceDepth(ancestors, resolved) counts
// how many entries of the FULL enclosing ancestor chain — the resolved template
// names above the instance — equal the instance's own resolved template name,
// not merely whether the immediate parent does.
//
// The table covers the four shapes the rule must get right:
//
//   - a top-level instance, whose ancestor chain is empty, is 0;
//   - same-template self-nesting, 0 / 1 / 2 as each successive ancestor repeats
//     the template (the outermost instance, the first self-nested instance, and
//     the second);
//   - the A→B→A→B chain, where the count must reach the FULL chain rather than
//     the immediate parent alone, so the inner a is 1 (its only enclosing a is
//     the outer a) and the second b is 1 (its only enclosing b is the first b),
//     while the outer a and the first b are each 0;
//   - an empty resolved name, which is not a template, is always 0 — even when
//     the ancestor chain is non-empty or itself contains empty entries.
//
// Everything is an in-memory literal, so the test runs under -short.
func TestInstanceDepth(t *testing.T) {
	tests := []struct {
		name      string
		ancestors []string
		resolved  string
		want      int
	}{
		{
			name:      "a top-level instance with a nil ancestor chain is depth 0",
			ancestors: nil,
			resolved:  "block",
			want:      0,
		},
		{
			name:      "an empty ancestor chain is depth 0",
			ancestors: []string{},
			resolved:  "block",
			want:      0,
		},
		{
			name:      "the outermost instance of a self-nesting template is depth 0",
			ancestors: nil,
			resolved:  "block",
			want:      0,
		},
		{
			name:      "the first same-template self-nested instance is depth 1",
			ancestors: []string{"block"},
			resolved:  "block",
			want:      1,
		},
		{
			name:      "the second same-template self-nested instance is depth 2",
			ancestors: []string{"block", "block"},
			resolved:  "block",
			want:      2,
		},
		{
			name:      "in A->B->A->B the first b has no enclosing b and is depth 0",
			ancestors: []string{"a"},
			resolved:  "b",
			want:      0,
		},
		{
			name:      "in A->B->A->B the inner a is depth 1 (its only enclosing a is the outer a)",
			ancestors: []string{"a", "b"},
			resolved:  "a",
			want:      1,
		},
		{
			name:      "in A->B->A->B the second b is depth 1 (its only enclosing b is the first b)",
			ancestors: []string{"a", "b", "a"},
			resolved:  "b",
			want:      1,
		},
		{
			name:      "only same-template ancestors count, not every ancestor",
			ancestors: []string{"a", "b", "c"},
			resolved:  "b",
			want:      1,
		},
		{
			name:      "an empty resolved name is depth 0 even with a non-empty ancestor chain",
			ancestors: []string{"block", "block"},
			resolved:  "",
			want:      0,
		},
		{
			name:      "an empty resolved name is depth 0 even when an ancestor is also empty",
			ancestors: []string{"", "a"},
			resolved:  "",
			want:      0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InstanceDepth(tt.ancestors, tt.resolved); got != tt.want {
				t.Errorf("InstanceDepth(%q, %q) = %d, want %d", tt.ancestors, tt.resolved, got, tt.want)
			}
		})
	}
}
