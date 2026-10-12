package template

import (
	"path/filepath"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for plan.phase-01.task-8: a
// library layout's name is its layer-relative forward-slash path, so the SAME
// fixture library loaded from two DIFFERENT absolute roots in one run yields
// identical layout names and renders its cross-layer composition identically.
// Only the display-only on-disk LayoutPath differs per root; the name — the
// namespace key and the string an author writes in `{{ template "<name>" . }}`
// — does not carry the root (portable-layout-names,
// example-layout-composition).
//
// It drives the real layered loader (LoadDeckLibrary) over a two-layer deck on
// disk, written twice under two different absolute temp parents: an external
// layer contributing section ext-card and a local layer contributing slide host,
// whose layout composes ext-card by its layer-relative name. The tests touch the
// real filesystem, so they are guarded with testing.Short() and skipped under
// `go test -short`. They reuse writeSectionHelperIntRoot, sectionIntFiles,
// slideIntFiles and mergeIntFiles.

// twoRootsLayerRelativeResult is what one LoadDeckLibrary + example execution
// observes about the fixture: the merged layer-relative layout names, the
// root-joined on-disk layout paths (which do differ per root) and the composed
// render output.
type twoRootsLayerRelativeResult struct {
	hostName    string
	hostPath    string
	extCardName string
	extCardPath string
	rendered    string
}

// writeTwoRootsLayerRelativeFixture writes the two-layer fixture under parent:
// the external layer at parent/external (section ext-card) and the local layer
// at parent/deck/templates (slide host, whose layout composes ext-card by its
// layer-relative name). It returns the two LoadDeckLibrary arguments. The
// fixture carries no media, so the render is a pure layout composition.
func writeTwoRootsLayerRelativeFixture(t *testing.T, parent string) (deckRoot, externalRoot string) {
	t.Helper()
	externalRoot = filepath.Join(parent, "external")
	deckRoot = filepath.Join(parent, "deck")

	externalFiles := sectionIntFiles("ext-card", "name: ext-card\n",
		`<b class="ext-card">ext-card-body</b>`)
	writeSectionHelperIntRoot(t, externalRoot, "external", externalFiles)

	// host composes ext-card by the exact layer-relative name loadlibrary.go
	// builds, identical on every OS and wherever the layer sits on disk.
	extCardLayout := SectionsDir + "/ext-card/" + LayoutFile
	localFiles := slideIntFiles("host",
		`<section class="host">{{ template "`+extCardLayout+`" . }}</section>`)
	writeSectionHelperIntRoot(t, filepath.Join(deckRoot, TemplatesDir), "local", localFiles)
	return deckRoot, externalRoot
}

// loadTwoRootsLayerRelative loads the fixture from (deckRoot, externalRoot) and
// returns the merged names, on-disk paths and the composed render of host's
// layout, failing the test on any error.
func loadTwoRootsLayerRelative(t *testing.T, deckRoot, externalRoot string) twoRootsLayerRelativeResult {
	t.Helper()
	lib, err := LoadDeckLibrary(deckRoot, externalRoot)
	if err != nil {
		t.Fatalf("LoadDeckLibrary(%q, %q) error = %v, want nil", deckRoot, externalRoot, err)
	}

	host, _, ok := lib.TemplateByName("host")
	if !ok || host == nil || host.Layout == nil {
		t.Fatalf("TemplateByName(host) not found with a parsed layout")
	}
	extCard, _, ok := resolveMergedSectionTarget(lib, "ext-card")
	if !ok || extCard == nil || extCard.Layout == nil {
		t.Fatalf(`resolveMergedSectionTarget("ext-card") not found with a parsed layout`)
	}

	// Re-run host's step-7 example binding to observe the composed output:
	// exampleLayoutNamespace parses every merged layout into one namespace, so
	// host's `{{ template "<ext-card layer-relative name>" . }}` resolves.
	namespace, err := exampleLayoutNamespace(lib, resolveMergedSlideLayer(lib, "host"), nil)
	if err != nil {
		t.Fatalf("exampleLayoutNamespace(host) error = %v", err)
	}
	var buf strings.Builder
	if err := namespace.ExecuteTemplate(&buf, host.Layout.Name(), map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}); err != nil {
		t.Fatalf("ExecuteTemplate(%q) error = %v, want the composition to resolve", host.Layout.Name(), err)
	}

	return twoRootsLayerRelativeResult{
		hostName:    host.Layout.Name(),
		hostPath:    host.LayoutPath,
		extCardName: extCard.Layout.Name(),
		extCardPath: extCard.LayoutPath,
		rendered:    buf.String(),
	}
}

// TestLayerRelativeLayoutNameTwoRootsInt loads the same fixture library from two
// different absolute roots in one run and asserts the layer-relative layout
// names and the composed render are identical, while the root-joined on-disk
// paths differ. A name that carried the root (the pre-fix behaviour) could not
// be identical across roots, so this pins the portability contract.
func TestLayerRelativeLayoutNameTwoRootsInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// Two different absolute parents: same fixture bytes, different roots.
	deckRootA, externalRootA := writeTwoRootsLayerRelativeFixture(t, t.TempDir())
	deckRootB, externalRootB := writeTwoRootsLayerRelativeFixture(t, t.TempDir())

	a := loadTwoRootsLayerRelative(t, deckRootA, externalRootA)
	b := loadTwoRootsLayerRelative(t, deckRootB, externalRootB)

	// The layer-relative names are the loader-built form, identical across the
	// two roots.
	wantHost := SlidesDir + "/host/" + LayoutFile
	wantExtCard := SectionsDir + "/ext-card/" + LayoutFile
	for _, got := range []twoRootsLayerRelativeResult{a, b} {
		if got.hostName != wantHost {
			t.Errorf("host layout name = %q, want the layer-relative %q", got.hostName, wantHost)
		}
		if got.extCardName != wantExtCard {
			t.Errorf("ext-card layout name = %q, want the layer-relative %q", got.extCardName, wantExtCard)
		}
	}
	if a.hostName != b.hostName {
		t.Errorf("host layout name differs across roots: %q vs %q", a.hostName, b.hostName)
	}
	if a.extCardName != b.extCardName {
		t.Errorf("ext-card layout name differs across roots: %q vs %q", a.extCardName, b.extCardName)
	}

	// The root-joined on-disk display paths DO differ, proving the fixture
	// really loaded from two distinct absolute roots (not the same dir twice).
	if a.hostPath == b.hostPath {
		t.Errorf("host on-disk path %q is identical across roots %q and %q, want distinct roots", a.hostPath, deckRootA, deckRootB)
	}
	if a.extCardPath == b.extCardPath {
		t.Errorf("ext-card on-disk path %q is identical across roots, want distinct roots", a.extCardPath)
	}

	// The composition renders identically, and the external layout's marker is
	// actually spliced in (so the render is not vacuously equal).
	if a.rendered != b.rendered {
		t.Errorf("composed render differs across roots:\nA: %q\nB: %q", a.rendered, b.rendered)
	}
	if !strings.Contains(a.rendered, "ext-card-body") {
		t.Errorf("composed render = %q, want the external ext-card layout spliced in", a.rendered)
	}
}
