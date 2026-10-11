package template

import (
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for plan.phase-01.task-7: the
// load-time example check (templates-dir-validation step 7) executes a
// local-layer layout that includes an EXTERNAL-layer layout by the target's own
// root-qualified layout name against the merged library layout namespace
// (example-layout-composition, library-layer-references).
//
// It drives the real layered loader (LoadDeckLibrary) over a two-layer deck on
// disk: an external library at a temp root plus the deck-root templates/ local
// layer, each with its own media/ tree. The local slide `host` composes the
// EXTERNAL layer's `ext-badge` section layout with the standard
// `{{ template "<external root>/sections/ext-badge/layout.html.tmpl" . }}`
// call — exactly the layout name loadlibrary.go builds
// (`path.Join(root, layoutRel)`, loadlibrary.go:709) and exampleLayoutNamespace
// keys every library layout under — and calls that external section with
// `{{ section "ext-badge" }}`.
//
// Media binds per owning layer (library-layer-references): the local layout's
// own `{{ media "local.png" }}` resolves in the LOCAL layer's media, while the
// external layout's `{{ media … }}` resolves in the EXTERNAL layer's media.
// Each layout's media file lives in one layer only, so a successful load proves
// which layer's filesystem the binding resolved against. Because the `media`
// helper is bound once per namespace — the way render's namespaceForLayer binds
// the executing layout's layer — a `{{ template }}` include executes the invoked
// layout in the INVOKING (local) layer's namespace, so an invoked layout's own
// `{{ media … }}` binds to the invoking layer. The external layout's direct
// media reference is therefore guarded so the slide-context include does not
// re-resolve the external-only file against the local layer, and its media is
// observed in the two contexts where it does bind to the external layer: the
// external layout's own step-7 run (where it is the example-checked template)
// and the `{{ section }}` target call (where exampleSectionRunner.render
// re-binds the target's own owning layer). The include additionally splices in
// external-layer media through that section path, so external media is rendered
// by the cross-layer include itself.
//
// These tests touch the real filesystem, so they are guarded with
// testing.Short() and skipped under `go test -short`. They reuse
// writeSectionHelperIntRoot, slideIntFiles, sectionIntFiles, mergeIntFiles and
// the step-7 binding idiom the neighbouring integration tests use
// (sectionhelper_int_test.go, library_int_test.go).

// TestLoadDeckLibraryCrossLayerLayoutIncludeInt proves LoadDeckLibrary composes
// a two-layer deck whose local-layer layout includes an external-layer layout by
// the external layer's own root-qualified layout name, at the step-7 example
// check (example-layout-composition, library-layer-references):
//
//   - LoadDeckLibrary succeeds: the local host's
//     `{{ template "<external root>/sections/ext-badge/layout.html.tmpl" . }}`
//     include resolves against the merged layout namespace and executes the
//     external layout's markup — the capability this phase adds, where parsing
//     the checked layout's text alone failed "no such template";
//   - the local layout's own `{{ media "local.png" }}` resolves in the LOCAL
//     layer's media, and the external layout's `{{ media … }}` resolves in the
//     EXTERNAL layer's media;
//   - the include splices in the external layer's media through the external
//     layout's `{{ section "ext-icon" }}` call, which the load-time example
//     helper renders from the external target's own owning layer;
//   - the local host's `{{ section "ext-badge" }}` call executes through the
//     load-time example helper (exampleSectionHelper -> exampleSectionRunner),
//     re-binding the external target's own owning layer so the external
//     section's media renders from the external layer at step 7.
//
// LoadDeckLibrary discards step 7's rendered bytes, so the test re-runs the
// exact bindings to observe the composed output: host under its own local layer
// (exampleLayoutNamespace(lib, hostLayer, nil)) and the external section under
// its own external layer, mirroring checkLibraryExamples.
func TestLoadDeckLibraryCrossLayerLayoutIncludeInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// The external root must exist before the local layout can name the external
	// layout's root-qualified path, so create it first and write the two layers
	// directly rather than through the combined helper.
	externalRoot := t.TempDir()
	extBadgeLayout := path.Join(externalRoot, SectionsDir, "ext-badge", LayoutFile)

	// ext-badge is the external-layer layout the local host includes. It
	// declares a field, so its own step-7 run binds `.item` and parses its
	// example. Its direct media reference is rendered in that section context;
	// the `.item` guard keeps the slide-context `{{ template }}` include — which
	// carries no `.item` — from re-resolving the external-only media file
	// against the local layer. Its `{{ section "ext-icon" }}` call is rendered
	// by the load-time example helper, which binds the target's own external
	// layer, so external media is spliced into the include itself.
	const extBadgeBody = `<b class="ext-badge">{{ if .item }}{{ media "badge.png" }}{{ end }}{{ section "ext-icon" }}</b>`

	localFiles := mergeIntFiles(
		slideIntFiles("host",
			`<section class="local">{{ media "local.png" }}{{ template "`+extBadgeLayout+`" . }}{{ section "ext-badge" }}</section>`),
		map[string]string{"media/local.png": "<svg class=\"local\"/>"},
	)
	externalFiles := mergeIntFiles(
		sectionIntFiles("ext-badge",
			"name: ext-badge\nfields:\n  - name: title\n    type: text\n",
			extBadgeBody,
		),
		sectionIntFiles("ext-icon", "name: ext-icon\n",
			`<i class="ext-icon">{{ media "icon.png" }}</i>`),
		map[string]string{
			"media/badge.png": "<svg class=\"badge\"/>",
			"media/icon.png":  "<svg class=\"icon\"/>",
		},
	)
	// A field-bearing section's example is parsed at step 7; supply the declared
	// optional field so the example is well-formed.
	externalFiles[SectionsDir+"/ext-badge/"+ExampleFile] = "```\ntitle: Hi\n```\n"

	deckRoot := t.TempDir()
	localRoot := filepath.Join(deckRoot, TemplatesDir)
	writeSectionHelperIntRoot(t, localRoot, "local", localFiles)
	writeSectionHelperIntRoot(t, externalRoot, "external", externalFiles)

	lib, err := LoadDeckLibrary(deckRoot, externalRoot)
	if err != nil {
		t.Fatalf("LoadDeckLibrary() error = %v, want nil: a local layout may include an external layout by the external root-qualified name", err)
	}
	if lib == nil {
		t.Fatal("LoadDeckLibrary() returned a nil Library with a nil error")
	}

	// The external layer is first and the local layer second, each with its own
	// media tree.
	if len(lib.Layers) != 2 {
		t.Fatalf("len(Layers) = %d, want 2", len(lib.Layers))
	}
	if lib.Layers[0].RootPath != externalRoot || lib.Layers[0].Role != LayerRoleExternal {
		t.Errorf("Layers[0] = {%q, %v}, want the external layer %q", lib.Layers[0].RootPath, lib.Layers[0].Role, externalRoot)
	}
	if lib.Layers[1].RootPath != localRoot || lib.Layers[1].Role != LayerRoleLocal {
		t.Errorf("Layers[1] = {%q, %v}, want the local layer %q", lib.Layers[1].RootPath, lib.Layers[1].Role, localRoot)
	}

	// Media lives in one layer only, so resolving it proves which media
	// filesystem the binding used: badge.png and icon.png stay external,
	// local.png stays local.
	for _, name := range []string{"badge.png", "icon.png"} {
		if _, err := fs.Stat(lib.Layers[0].Media, name); err != nil {
			t.Errorf("external layer media does not hold %s: %v", name, err)
		}
		if _, err := fs.Stat(lib.Layers[1].Media, name); err == nil {
			t.Errorf("local layer media holds %s, want it external-only", name)
		}
	}
	if _, err := fs.Stat(lib.Layers[1].Media, "local.png"); err != nil {
		t.Errorf("local layer media does not hold local.png: %v", err)
	}
	if _, err := fs.Stat(lib.Layers[0].Media, "local.png"); err == nil {
		t.Error("external layer media holds local.png, want it local-only")
	}

	host, _, ok := lib.TemplateByName("host")
	if !ok || host == nil {
		t.Fatal(`TemplateByName("host") not found in the merged library`)
	}
	// Confirm the exact layout name the include literal names is the external
	// layout's own loader-built, root-qualified name.
	badge, badgeLayer, ok := resolveMergedSectionTarget(lib, "ext-badge")
	if !ok || badge == nil || badge.Definition == nil {
		t.Fatal(`resolveMergedSectionTarget("ext-badge") not found with a Definition`)
	}
	if badge.Layout.Name() != extBadgeLayout {
		t.Fatalf("ext-badge layout name = %q, want the include literal %q", badge.Layout.Name(), extBadgeLayout)
	}

	// Re-run host's step-7 binding: the merged namespace built for host's own
	// (local) layer, with `section` overridden by the load-time example helper,
	// exactly as checkLibraryExample does.
	hostNS, err := exampleLayoutNamespace(lib, resolveMergedSlideLayer(lib, "host"), nil)
	if err != nil {
		t.Fatalf("exampleLayoutNamespace(host) error = %v", err)
	}
	var hostOut strings.Builder
	if err := hostNS.ExecuteTemplate(&hostOut, host.Layout.Name(), map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}); err != nil {
		t.Fatalf("execute host under the merged example binding: %v, want nil (LoadDeckLibrary's own step-7 execution succeeded)", err)
	}
	got := hostOut.String()

	// The local layout's own media resolved in the local layer.
	if !strings.Contains(got, `/media/local.png`) {
		t.Errorf("host rendered = %q, want the local layout's media reference resolved in the local layer (/media/local.png)", got)
	}
	// The cross-layer include executed the external layout; because the invoking
	// slide carries no .item its direct media reference is skipped, and the
	// include's splice of external media comes through the external layout's
	// `{{ section "ext-icon" }}` call, rendered from that target's own external
	// layer (icon.png exists only there; a local binding would have failed).
	if !strings.Contains(got, `<b class="ext-badge"><i class="ext-icon">/media/icon.png</i></b>`) {
		t.Errorf("host rendered = %q, want the cross-layer `{{ template }}` include to have executed the external layout and spliced in external-layer media via its section call", got)
	}
	// The section helper executed the external target and rendered its media,
	// which resolved in the EXTERNAL layer's media (badge.png exists only there;
	// a local binding would have failed the load).
	if !strings.Contains(got, `<b class="ext-badge">/media/badge.png<i class="ext-icon">/media/icon.png</i></b>`) {
		t.Errorf("host rendered = %q, want the `{{ section \"ext-badge\" }}` call to render the external section and its media from the external layer", got)
	}

	// Re-run the external section's own step-7 binding under its own external
	// layer: the external layout resolves its own media (and its section call's
	// media) in the external layer.
	badgeNS, err := exampleLayoutNamespace(lib, badgeLayer, map[string]any{"title": "Hi"})
	if err != nil {
		t.Fatalf("exampleLayoutNamespace(ext-badge) error = %v", err)
	}
	var badgeOut strings.Builder
	if err := badgeNS.ExecuteTemplate(&badgeOut, badge.Layout.Name(), map[string]any{
		"title": "Hi",
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
		"item": map[string]any{
			"index":    0,
			"number":   1,
			"count":    1,
			"first":    true,
			"last":     true,
			"section":  "ext-badge",
			"template": "ext-badge",
			"parent":   map[string]any(nil),
		},
	}); err != nil {
		t.Fatalf("execute ext-badge under its own external example binding: %v", err)
	}
	if got := badgeOut.String(); !strings.Contains(got, `<b class="ext-badge">/media/badge.png<i class="ext-icon">/media/icon.png</i></b>`) {
		t.Errorf("ext-badge rendered = %q, want its media resolved in the external layer", got)
	}
}

// TestLoadDeckLibraryExternalToLocalLayoutIncludeErrorInt is the negative,
// external-to-local counterpart of TestLoadDeckLibraryCrossLayerLayoutIncludeInt
// (templates-dir-validation step 6a, example-layout-composition,
// library-layer-references).
//
// A reference runs one way only. The merged-namespace composition the phase
// adds lets a LOCAL layer's layout include an EXTERNAL layer's layout by the
// external root-qualified name, because the local layer is loaded deferred and
// the merged pass parses every layer's layouts into one namespace. The reverse
// direction is not granted: a layered deck's external `templates:` layer is
// loaded first and FULLY (libraryLayerFull), validated as a self-contained
// standalone library before any merge, so its step-7 example check runs against
// a namespace holding only the external layer's own layouts. The external layer
// may never depend on the local one.
//
// This test drives the real layered loader (LoadDeckLibrary) over a two-layer
// deck on disk whose EXTERNAL-layer slide `ext-host` includes a LOCAL-layer
// layout by the local root-qualified name
// `{{ template "<deckRoot>/templates/slides/local-target/layout.html.tmpl" . }}`
// — exactly the name loadlibrary.go builds for the local layer
// (`path.Join(root, layoutRel)`, loadlibrary.go:709) — while the local layer
// really carries that layout. The name is real in the deck but absent from the
// external layer's standalone namespace, so the external layer's own step-7
// example execution fails "no such template" and LoadDeckLibrary returns that
// failure as a *LibraryError positioned at the EXTERNAL layout's own file
// (never the local target's), with the underlying html/template execute error —
// which names the missing local layout — in the Message.
//
// It touches the real filesystem, so it is guarded with testing.Short() and
// skipped under `go test -short`, like its sibling. It reuses
// writeSectionHelperIntRoot and slideIntFiles.
func TestLoadDeckLibraryExternalToLocalLayoutIncludeErrorInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// The deck root must exist before the external layout text can name the
	// local layer's root-qualified layout path, so create it first and compute
	// the local root (deckRoot/templates) and the exact layout name the local
	// layer's loader will build.
	deckRoot := t.TempDir()
	localRoot := filepath.Join(deckRoot, TemplatesDir)
	localTargetLayout := path.Join(localRoot, SlidesDir, "local-target", LayoutFile)

	// ext-host is the external-layer slide that makes the cross-layer
	// (external→local) include. The include is unguarded, so the external
	// layer's own step-7 execution reaches it. The local name is real in the
	// deck; it is simply not in the external layer's standalone namespace.
	extHostLayout := `{{ template ` + strconv.Quote(localTargetLayout) + ` . }}`
	externalFiles := slideIntFiles("ext-host", extHostLayout)

	// The local layer really carries the named layout, so the failure is the
	// external layer's missing namespace — a name that resolves in the merged
	// deck but not against the external layer alone — rather than a typo.
	localFiles := slideIntFiles("local-target", `<section class="local-target"></section>`)

	externalRoot := t.TempDir()
	writeSectionHelperIntRoot(t, externalRoot, "external", externalFiles)
	writeSectionHelperIntRoot(t, localRoot, "local", localFiles)

	// LoadDeckLibrary loads the external layer first and fully — standalone,
	// before the merge — so the external layout's include cannot see the local
	// layer's layouts and step 7 must fail at the external layout's own file.
	lib, err := LoadDeckLibrary(deckRoot, externalRoot)
	if lib != nil {
		t.Fatalf("LoadDeckLibrary() Library = %+v, want nil: the external layout's include names a local-only layout", lib)
	}
	libErr := asLibraryError(t, err)
	wantPath := path.Join(externalRoot, SlidesDir, "ext-host", LayoutFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want the external layer's own layout %q", libErr.Path, wantPath)
	}
	if libErr.Line != 0 {
		t.Errorf("Line = %d, want 0 (a whole-layout step-7 execute failure)", libErr.Line)
	}
	if !strings.Contains(libErr.Message, "execute") {
		t.Errorf("Message = %q, want a step-7 execute failure", libErr.Message)
	}
	if !strings.Contains(libErr.Message, localTargetLayout) {
		t.Errorf("Message = %q, want it to name the missing local layout %q", libErr.Message, localTargetLayout)
	}

	// Loading the external root ALONE fails identically, pinning the failure to
	// the external layer's own standalone load rather than to the merge: the
	// external layer is validated as a self-contained library and may never
	// depend on the deck's local layer.
	extLib, err := LoadLibraryAt(externalRoot)
	if extLib != nil {
		t.Fatalf("LoadLibraryAt(external) Library = %+v, want nil", extLib)
	}
	extErr := asLibraryError(t, err)
	if extErr.Path != wantPath {
		t.Errorf("LoadLibraryAt Path = %q, want the external layout %q", extErr.Path, wantPath)
	}
	if !strings.Contains(extErr.Message, "execute") ||
		!strings.Contains(extErr.Message, localTargetLayout) {
		t.Errorf("LoadLibraryAt Message = %q, want the same step-7 execute failure naming %q", extErr.Message, localTargetLayout)
	}
}
