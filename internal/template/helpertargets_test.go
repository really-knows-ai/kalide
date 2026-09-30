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
