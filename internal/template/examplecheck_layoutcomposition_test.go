package template

import (
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-01 tasks 4-6: the
// load-time example check (templates-dir-validation step 7, checkLibraryExample)
// must execute every library layout against the MERGED library layout namespace
// (exampleLayoutNamespace), not the single target layout text alone, so a
// layout's standard `{{ template "<layout name>" . }}` composition call
// resolves (example-layout-composition).
//
// TestCheckLibraryExampleComposesLibraryLayout (task 4): a single-layer,
// in-memory library whose slide "outer" includes slide "hello"'s layout by its
// exact loader-built name loads cleanly, where executing outer's layout in a
// fresh single-layout namespace — the pre-fix behaviour — failed
// "no such template".

// TestCheckLibraryExampleComposesLibraryLayout proves LoadLibrary's step-7
// example check composes a slide layout that includes another library layout.
//
// "outer" is a slide whose layout calls the standard html/template composition
// form `{{ template "<layout name>" . }}`, naming slide "hello"'s layout by the
// exact name loadlibrary.go builds for it — path.Join(root,
// slides/hello/layout.html.tmpl) with root == TemplatesDir (loadlibrary.go:709)
// — which is the name exampleLayoutNamespace registers every library layout
// under. The library is single-layer and in-memory; LoadLibrary must succeed,
// whereas executing outer's layout text alone in a fresh namespace (the
// pre-fix step-7 behaviour) failed "no such template" because no other layout
// was registered for it to invoke.
func TestCheckLibraryExampleComposesLibraryLayout(t *testing.T) {
	// hello's layout carries a marker the composed output can be checked for,
	// so the test proves more than a bare successful load: the include
	// actually renders.
	const helloBody = "hello-composed-body"
	fsys := baseLibraryFS()
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<section class="hello">` + helloBody + `</section>`),
	}
	fsys["slides/outer/template.yaml"] = &fstest.MapFile{
		Data: []byte("name: outer\ndescription: composes hello\n"),
	}
	// The exact layout name loadlibrary.go builds and registers:
	// path.Join(root, slides/<name>/layout.html.tmpl), root == TemplatesDir.
	helloLayoutName := TemplatesDir + "/slides/hello/" + LayoutFile
	fsys["slides/outer/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<div>{{ template "` + helloLayoutName + `" . }}</div>`),
	}
	fsys["slides/outer/example.md"] = &fstest.MapFile{Data: []byte("# outer\n")}

	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: outer's `{{ template %q . }}` include must resolve against the merged library layout namespace", err, helloLayoutName)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}

	outer, _, ok := lib.TemplateByName("outer")
	if !ok || outer == nil {
		t.Fatal(`TemplateByName("outer") not found in loaded library`)
	}
	// Confirm the exact layout name loadlibrary.go builds:
	// path.Join(root, layoutRel) == root/slides/outer/layout.html.tmpl.
	if got, want := outer.Layout.Name(), TemplatesDir+"/slides/outer/"+LayoutFile; got != want {
		t.Errorf("outer.Layout.Name() = %q, want %q (the layout name loadlibrary.go builds)", got, want)
	}

	// Re-run step 7's execution for outer to observe the composed output
	// (LoadLibrary discards the rendered bytes): the merged namespace must hold
	// hello's layout under helloLayoutName, so the include renders it.
	namespace, err := exampleLayoutNamespace(lib, resolveMergedSlideLayer(lib, "outer"), nil)
	if err != nil {
		t.Fatalf("exampleLayoutNamespace() error = %v", err)
	}
	var buf strings.Builder
	if err := namespace.ExecuteTemplate(&buf, outer.Layout.Name(), map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}); err != nil {
		t.Fatalf("ExecuteTemplate(%q) error = %v, want outer's include to resolve", outer.Layout.Name(), err)
	}
	if got := buf.String(); !strings.Contains(got, helloBody) {
		t.Errorf("outer rendered = %q, want it to contain hello's composed layout %q", got, helloBody)
	}
}

// TestCheckLibraryExampleSectionKindLayoutInclude (task 5) proves LoadLibrary's
// step-7 example check resolves a SECTION-kind template's `{{ template }}`
// composition include against the merged library layout namespace while the
// load-time `section` helper override (exampleSectionHelper) and the section
// `.item` context are preserved (example-layout-composition, item-context).
//
// panel is a section-kind template (it declares a field, so checkLibraryExample
// binds its one-item `.item` and parses its example at step 7). Its layout does
// two things the pre-fix fresh single-layout namespace could not survive:
//
//   - `{{ template "<card layout name>" . }}` includes sections/card's layout by
//     the exact root-qualified name loadlibrary.go builds for it — path.Join(root,
//     sections/card/layout.html.tmpl) with root == TemplatesDir (loadlibrary.go:709),
//     the name exampleLayoutNamespace registers every library layout under. Pre-fix,
//     panel's own text was the only template in the namespace, so the include
//     failed "no such template";
//   - `{{ section "badge" }}` calls a section through the load-time
//     exampleSectionHelper override — pre-fix it hit LayoutFuncMap's parse-only
//     stub and failed "no render-time implementation bound". The call is
//     unguarded, so executing it proves the override is bound at step 7, not
//     merely parsed.
//
// The included card layout reads `.item.section`, so the composition also proves
// `.item` is threaded into the included layout through the caller's dot: under
// panel's section context it renders "panel", never card's own "card". card
// declares a field too, so its own step-7 example binds card's own `.item` and
// the include target is a valid section example in its own right.
//
// LoadLibrary must succeed; because it discards the rendered bytes, the test
// re-runs panel's (and card's) step-7 execution through exampleLayoutNamespace
// to observe the composed HTML.
func TestCheckLibraryExampleSectionKindLayoutInclude(t *testing.T) {
	const (
		// The exact layout name loadlibrary.go builds and registers for card:
		// path.Join(root, sections/card/layout.html.tmpl), root == TemplatesDir.
		cardLayoutName = TemplatesDir + "/" + SectionsDir + "/card/" + LayoutFile
		cardBody       = `<i class="card">{{ .item.section }}</i>`
		badgeBody      = `<b class="badge">badge-body</b>`
	)

	fsys := baseLibraryFS()

	// card: the include target. A section-kind template declaring a field so its
	// own example execution binds `.item` (checkLibraryExample); its layout
	// renders `.item.section`, naming whichever section instance's dot invoked it.
	fsys["sections/card/template.yaml"] = &fstest.MapFile{
		Data: []byte("name: card\nfields:\n  - name: title\n    type: text\n"),
	}
	fsys["sections/card/layout.html.tmpl"] = &fstest.MapFile{Data: []byte(cardBody)}
	fsys["sections/card/example.md"] = &fstest.MapFile{Data: []byte("```\ntitle: C\n```\n")}

	// badge: the section panel's `{{ section "badge" }}` call renders through the
	// load-time helper override. Schema-free, so step 7 executes its layout with
	// the empty example context.
	fsys["sections/badge/template.yaml"] = &fstest.MapFile{Data: []byte("name: badge\n")}
	fsys["sections/badge/layout.html.tmpl"] = &fstest.MapFile{Data: []byte(badgeBody)}
	fsys["sections/badge/example.md"] = &fstest.MapFile{Data: []byte("# badge\n")}

	// panel: the section-kind template under test. It declares a field so its
	// step-7 execution binds `.item` and parses its example; its layout both
	// includes card's layout by its root-qualified name and calls the section
	// helper, both unguarded.
	panelBody := `<div class="panel">{{ template "` + cardLayoutName + `" . }}{{ section "badge" }}</div>`
	fsys["sections/panel/template.yaml"] = &fstest.MapFile{
		Data: []byte("name: panel\nfields:\n  - name: title\n    type: text\n"),
	}
	fsys["sections/panel/layout.html.tmpl"] = &fstest.MapFile{Data: []byte(panelBody)}
	fsys["sections/panel/example.md"] = &fstest.MapFile{Data: []byte("```\ntitle: P\n```\n")}

	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: panel's `{{ template %q . }}` include and `{{ section \"badge\" }}` call must both resolve and execute at step 7", err, cardLayoutName)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}

	panel, panelLayer, ok := resolveMergedSectionTarget(lib, "panel")
	if !ok || panel == nil || panel.Definition == nil {
		t.Fatal(`resolveMergedSectionTarget("panel") not found with a Definition`)
	}
	// Confirm the exact layout name loadlibrary.go builds:
	// path.Join(root, sections/panel/layout.html.tmpl) == root/sections/panel/layout.html.tmpl.
	if got, want := panel.Layout.Name(), TemplatesDir+"/"+SectionsDir+"/panel/"+LayoutFile; got != want {
		t.Errorf("panel.Layout.Name() = %q, want %q (the layout name loadlibrary.go builds)", got, want)
	}

	// Re-run step 7's exact binding for panel to observe the composed output:
	// exampleLayoutNamespace parses every library layout into one namespace and
	// overrides `section` with exampleSectionHelper bound to panel's own source
	// fields (callerFields), exactly as checkLibraryExample does.
	callerFields := map[string]any{"title": "P"}
	namespace, err := exampleLayoutNamespace(lib, panelLayer, callerFields)
	if err != nil {
		t.Fatalf("exampleLayoutNamespace() error = %v", err)
	}
	panelCtx := map[string]any{
		"title": "P",
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
		// The one-item `.item` checkLibraryExample binds for a section-kind
		// template's own example execution.
		"item": map[string]any{
			"index":    0,
			"number":   1,
			"count":    1,
			"first":    true,
			"last":     true,
			"section":  "panel",
			"template": "panel",
			"parent":   map[string]any(nil),
		},
	}
	var buf strings.Builder
	if err := namespace.ExecuteTemplate(&buf, panel.Layout.Name(), panelCtx); err != nil {
		t.Fatalf("ExecuteTemplate(%q) error = %v, want panel's include and section call to resolve", panel.Layout.Name(), err)
	}
	got := buf.String()
	if !strings.Contains(got, `<i class="card">panel</i>`) {
		t.Errorf("panel rendered = %q, want the composed card layout reading panel's section `.item` (`<i class=\"card\">panel</i>`)", got)
	}
	if strings.Contains(got, `<i class="card">card</i>`) {
		t.Errorf("panel rendered = %q: the included card layout read card's own `.item`, not the invoking panel's", got)
	}
	if !strings.Contains(got, badgeBody) {
		t.Errorf("panel rendered = %q, want the section-helper override to have rendered badge %q", got, badgeBody)
	}
	if strings.Contains(got, "no render-time implementation bound") {
		t.Errorf("panel rendered = %q contains the parse-only section stub's error; the load-time helper must be bound", got)
	}

	// card's own step-7 example execution binds card's own `.item` (section
	// "card"), so the include target's layout is a valid section example in its
	// own right — `.item` is not leaked from the composition.
	card, cardLayer, ok := resolveMergedSectionTarget(lib, "card")
	if !ok || card == nil || card.Definition == nil {
		t.Fatal(`resolveMergedSectionTarget("card") not found with a Definition`)
	}
	cardNS, err := exampleLayoutNamespace(lib, cardLayer, map[string]any{"title": "C"})
	if err != nil {
		t.Fatalf("exampleLayoutNamespace(card) error = %v", err)
	}
	cardCtx := map[string]any{
		"title": "C",
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
		"item": map[string]any{
			"index":    0,
			"number":   1,
			"count":    1,
			"first":    true,
			"last":     true,
			"section":  "card",
			"template": "card",
			"parent":   map[string]any(nil),
		},
	}
	var cardBuf strings.Builder
	if err := cardNS.ExecuteTemplate(&cardBuf, card.Layout.Name(), cardCtx); err != nil {
		t.Fatalf("ExecuteTemplate(card %q) error = %v", card.Layout.Name(), err)
	}
	if got := cardBuf.String(); !strings.Contains(got, `<i class="card">card</i>`) {
		t.Errorf("card rendered = %q, want card's own section `.item` to render `card`", got)
	}
}
