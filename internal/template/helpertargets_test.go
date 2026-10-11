package template

import "testing"

// This file is the unit-test deliverable for plan.phase-07.task-4:
// Registry.HelperTargets — the registry-level, lazily memoised accessor over
// Section.HelperRefs that the render path uses to decide which layouts belong
// in a slide's shared namespace.
//
// It pins three properties with an in-process *Library built by hand and fed
// through NewRegistryFromLibrary:
//
//   - the value is LAZY, not validate-time: the registry is built and used
//     without ever calling Validate, and the unexported helperTargets cache is
//     nil before the first call and holds the template's key only after it;
//   - repeated calls are MEMOISED: they return the identical backing slice
//     rather than re-parsing the layout (see the identity note below);
//   - the names are de-duplicated and in source order, and an unknown name
//     yields nil (and caches nothing), while a layout with no helper calls
//     caches a nil result.
//
// Identity-assertion technique: Registry exposes no injectable counter or
// indirection to observe Section.HelperRefs call counts, and this task must not
// invent one. Memoisation is therefore observed structurally instead:
//
//	(1) the unexported reg.helperTargets map is nil before the first
//	    HelperTargets call and contains the template's key after it, which is
//	    the direct evidence the value is computed on first use (lazy) rather
//	    than at construction or Validate; and
//	(2) two consecutive calls return the same backing array, proved with
//	    &again[0] == &first[0]. Two freshly parsed slices would be distinct
//	    allocations, so equal element addresses can only mean the second call
//	    served the cached slice.
//
// A template with no helper calls returns a nil slice; the per-element address
// check is trivial there (a nil slice has no element), so that case proves
// caching by the presence of the cache key holding nil — the state the doc
// comment calls "computed, empty" as distinct from an absent key.

// helperTargetsTestLibrary builds a *Library by hand for NewRegistryFromLibrary:
// a "hero" slide whose layout calls two helpers with a repeated target, a
// "plain" slide with no helper calls, and the "footer"/"note" section targets
// it references. Only manifests and layout text are needed — HelperTargets
// re-parses the layout text, it does not execute it.
func helperTargetsTestLibrary() *Library {
	const manifest = "description: HelperTargets fixture\n"
	return &Library{
		Slides: map[string]*LibraryTemplate{
			"hero": {
				Name:          "hero",
				Kind:          KindSlide,
				ManifestPath:  "slides/hero/template.yaml",
				ManifestBytes: []byte(manifest),
				LayoutText:    `{{ section "footer" }}{{ section "note" }}{{ section "footer" }}`,
			},
			"plain": {
				Name:          "plain",
				Kind:          KindSlide,
				ManifestPath:  "slides/plain/template.yaml",
				ManifestBytes: []byte(manifest),
				LayoutText:    `<p>no helpers here</p>`,
			},
		},
		Sections: map[string]*LibraryTemplate{
			"footer": {
				Name:          "footer",
				Kind:          KindSection,
				ManifestPath:  "sections/footer/template.yaml",
				ManifestBytes: []byte(manifest),
				LayoutText:    `<footer>{{ .title }}</footer>`,
			},
			"note": {
				Name:          "note",
				Kind:          KindSection,
				ManifestPath:  "sections/note/template.yaml",
				ManifestBytes: []byte(manifest),
				LayoutText:    `<aside>{{ .title }}</aside>`,
			},
		},
	}
}

// TestRegistryHelperTargets proves HelperTargets over a registry built by
// NewRegistryFromLibrary that is deliberately never Validated: it is lazy, it
// memoises, it de-duplicates in source order, and an unknown name is nil.
func TestRegistryHelperTargets(t *testing.T) {
	reg, err := NewRegistryFromLibrary(helperTargetsTestLibrary())
	if err != nil {
		t.Fatalf("NewRegistryFromLibrary() error = %v, want nil", err)
	}
	// Deliberately no reg.Validate() anywhere in this test: the served
	// registry is built by NewRegistryFromLibrary and never validated, so
	// HelperTargets must compute lazily rather than depend on Validate.

	t.Run("lazily computed on first use, de-duplicated, in source order", func(t *testing.T) {
		if reg.helperTargets != nil {
			t.Fatalf("helperTargets cache = %v before the first call, want nil (lazy, not validate-time)", reg.helperTargets)
		}

		got := reg.HelperTargets("hero")
		assertStringSlice(t, "HelperTargets(hero)", got, []string{"footer", "note"})

		if _, cached := reg.helperTargets["hero"]; !cached {
			t.Fatalf("helperTargets[hero] absent after the first call, want the computed result cached")
		}
	})

	t.Run("repeated calls return the identical memoised slice", func(t *testing.T) {
		first := reg.HelperTargets("hero")
		again := reg.HelperTargets("hero")

		if len(first) == 0 {
			t.Fatal("HelperTargets(hero) first call is empty, want the helper target names")
		}
		if len(again) != len(first) {
			t.Fatalf("HelperTargets(hero) second call has %d names, want %d", len(again), len(first))
		}
		// Same backing array is the memoisation evidence: a re-parse would
		// allocate a fresh slice with a different first-element address.
		if &again[0] != &first[0] {
			t.Errorf("HelperTargets(hero) second call backing array %p differs from the first %p, want the identical memoised slice",
				&again[0], &first[0])
		}
	})

	t.Run("unknown name returns nil and caches nothing", func(t *testing.T) {
		if got := reg.HelperTargets("missing"); got != nil {
			t.Errorf("HelperTargets(missing) = %v, want nil", got)
		}
		if _, cached := reg.helperTargets["missing"]; cached {
			t.Errorf("helperTargets[missing] present after an unknown-name call, want no cache entry")
		}
	})

	t.Run("a layout with no helper calls caches a nil result", func(t *testing.T) {
		if got := reg.HelperTargets("plain"); got != nil {
			t.Errorf("HelperTargets(plain) = %v, want nil", got)
		}

		refs, cached := reg.helperTargets["plain"]
		if !cached {
			t.Fatalf("helperTargets[plain] absent, want an empty result cached (re-parsed at most once)")
		}
		if refs != nil {
			t.Errorf("helperTargets[plain] = %v, want nil", refs)
		}
		if again := reg.HelperTargets("plain"); again != nil {
			t.Errorf("HelperTargets(plain) second call = %v, want nil", again)
		}
	})
}

// This block (fixture + test) is the unit-test deliverable for
// plan.phase-01.task-19: a `section` helper call's child arguments are reference
// edges, so the registry's helper-target set must include the templates a
// `child`/`children` argument reaches — the child's instantiated template
// (child-constructor), the accepted templates of a forwarded declared section
// (children-forwarding) and, recursively, nested children. It builds on the
// registry-level accessor above: HelperTargets is the lazily memoised wrapper
// over Section.HelperRefs, so the child edges are asserted through both.
//
// The fixture is an in-process *Library fed through NewRegistryFromLibrary and
// deliberately never Validated, matching the served registry that
// helperTargetsTestLibrary establishes. Per the phase-01.task-18 note, a
// template reachable only through a child edge can be unconstructible in the
// cycle tests; HelperRefs/HelperTargets expose the child target directly, so
// this test asserts the reference set itself rather than driving a render or a
// validation walk.
//
// helperTargetsChildLibrary's slide layouts each isolate one edge shape so the
// expected set is exact:
//
//   - "plaincall": `{{ section "host" }}` carries no child, so only the call's
//     own target is a reference — the baseline the child cases extend;
//   - "literal": `(child "slot")` fills host's single-accepted `slot`, so the
//     template it instantiates (box) joins the set;
//   - "selector": `(child "row" (dict "template" "tile"))` fills host's
//     multi-accepted `row`, so the `template:` selector picks tile (not the
//     other accepted template);
//   - "forward": `(children "row" .data.row)` forwards host's `row`, so every
//     accepted template (panel and tile) joins the set;
//   - "nested": `(child "slot" nil nil (child "inner"))` nests a child, so box's
//     own declared `inner` instantiates leaf, which joins the set recursively.
//
// host declares `slot` (accepted: box) and `row` (accepted: panel, tile); box
// declares `inner` (accepted: leaf). The manifests are parsed by parseManifest;
// NewRegistryFromLibrary registers but never Validates, so the accepted
// references need not resolve through a full composition walk.
func helperTargetsChildLibrary() *Library {
	const leafManifest = "description: child helper-target fixture leaf\n"
	leaf := func(name string) *LibraryTemplate {
		return &LibraryTemplate{
			Name:          name,
			Kind:          KindSection,
			ManifestPath:  "sections/" + name + "/template.yaml",
			ManifestBytes: []byte(leafManifest),
			LayoutText:    `<div>{{ .title }}</div>`,
		}
	}
	slide := func(name, layout string) *LibraryTemplate {
		return &LibraryTemplate{
			Name:          name,
			Kind:          KindSlide,
			ManifestPath:  "slides/" + name + "/template.yaml",
			ManifestBytes: []byte("description: child helper-target fixture slide\n"),
			LayoutText:    layout,
		}
	}
	return &Library{
		Slides: map[string]*LibraryTemplate{
			"plaincall": slide("plaincall", `{{ section "host" }}`),
			"literal":   slide("literal", `{{ section "host" nil nil (child "slot") }}`),
			"selector":  slide("selector", `{{ section "host" nil nil (child "row" (dict "template" "tile")) }}`),
			"forward":   slide("forward", `{{ section "host" nil nil (children "row" .data.row) }}`),
			"nested":    slide("nested", `{{ section "host" nil nil (child "slot" nil nil (child "inner")) }}`),
		},
		Sections: map[string]*LibraryTemplate{
			"host": {
				Name:          "host",
				Kind:          KindSection,
				ManifestPath:  "sections/host/template.yaml",
				ManifestBytes: []byte("name: host\nsections:\n  - name: slot\n    accepted: [box]\n  - name: row\n    accepted: [panel, tile]\n"),
				LayoutText:    `<section>{{ .title }}</section>`,
			},
			"box": {
				Name:          "box",
				Kind:          KindSection,
				ManifestPath:  "sections/box/template.yaml",
				ManifestBytes: []byte("name: box\nsections:\n  - name: inner\n    accepted: [leaf]\n"),
				LayoutText:    `<div>{{ .title }}</div>`,
			},
			"leaf":  leaf("leaf"),
			"panel": leaf("panel"),
			"tile":  leaf("tile"),
		},
	}
}

// TestRegistryHelperTargetsChildRefs proves the registry's helper-target set
// includes the templates a `section` call's child arguments reference, not just
// the call's own target. For each slide layout it asserts the exact set through
// Registry.HelperTargets AND independently through Section.HelperRefs, so the
// child edges are pinned both at the memoised registry surface the render path
// consumes and at the underlying reference-graph accessor.
func TestRegistryHelperTargetsChildRefs(t *testing.T) {
	reg, err := NewRegistryFromLibrary(helperTargetsChildLibrary())
	if err != nil {
		t.Fatalf("NewRegistryFromLibrary() error = %v, want nil", err)
	}
	// Deliberately no reg.Validate(): the served registry is built by
	// NewRegistryFromLibrary and never validated, so HelperTargets must expose
	// the child edges without a validation walk.

	tests := []struct {
		name string
		want []string
		why  string
	}{
		{
			name: "plaincall",
			want: []string{"host"},
			why:  "a call with no child argument references only its own target",
		},
		{
			name: "literal",
			want: []string{"host", "box"},
			why:  "a literal child adds the template its declared section instantiates",
		},
		{
			name: "selector",
			want: []string{"host", "tile"},
			why:  "a literal child's `template:` selector picks the one instantiated template",
		},
		{
			name: "forward",
			want: []string{"host", "panel", "tile"},
			why:  "a children forwarding adds every accepted template of the forwarded section",
		},
		{
			name: "nested",
			want: []string{"host", "box", "leaf"},
			why:  "a nested child adds its instantiated template recursively",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.HelperTargets(tt.name)
			assertStringSlice(t, "HelperTargets("+tt.name+")", got, tt.want)

			tmpl, ok := reg.Lookup(tt.name)
			if !ok || tmpl == nil {
				t.Fatalf("Lookup(%q) = %v, %v; want the registered fixture template", tt.name, tmpl, ok)
			}
			refs, err := NewSection(reg.Lookup).HelperRefs(tmpl)
			if err != nil {
				t.Fatalf("HelperRefs(%q) error = %v, want nil", tt.name, err)
			}
			assertStringSlice(t, "HelperRefs("+tt.name+")", refs, tt.want)

			if tt.name != "plaincall" && len(got) == 1 {
				t.Errorf("HelperTargets(%s) = %v, want the child target(s) beyond the call target: %s", tt.name, got, tt.why)
			}
		})
	}
}
