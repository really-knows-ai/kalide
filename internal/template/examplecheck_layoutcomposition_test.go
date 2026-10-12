package template

import (
	"strconv"
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
// exact name loadlibrary.go builds for it — its layer-relative forward-slash
// path slides/hello/layout.html.tmpl (loadlibrary.go:714) — which is the name
// exampleLayoutNamespace registers every library layout under, identical on
// every OS and wherever the layer sits on disk. The library is single-layer and
// in-memory; LoadLibrary must succeed, whereas executing outer's layout text
// alone in a fresh namespace (the pre-fix step-7 behaviour) failed "no such
// template" because no other layout was registered for it to invoke.
func TestCheckLibraryExampleComposesLibraryLayout(t *testing.T) {
	// hello's layout carries a marker so the test proves the include renders,
	// not merely that the load succeeds.
	const helloBody = "hello-composed-body"
	fsys := baseLibraryFS()
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<section class="hello">` + helloBody + `</section>`),
	}
	fsys["slides/outer/template.yaml"] = &fstest.MapFile{
		Data: []byte("name: outer\ndescription: composes hello\n"),
	}
	// The exact layer-relative layout name loadlibrary.go builds and
	// registers: slides/<name>/layout.html.tmpl.
	helloLayoutName := SlidesDir + "/hello/" + LayoutFile
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
	// Confirm the exact layer-relative layout name loadlibrary.go builds
	// for outer: slides/outer/layout.html.tmpl.
	if got, want := outer.Layout.Name(), SlidesDir+"/outer/"+LayoutFile; got != want {
		t.Errorf("outer.Layout.Name() = %q, want %q (the layer-relative layout name loadlibrary.go builds)", got, want)
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
//     the exact layer-relative name loadlibrary.go builds for it —
//     sections/card/layout.html.tmpl (loadlibrary.go:714), the name
//     exampleLayoutNamespace registers every library layout under. Pre-fix,
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
		// The exact layer-relative layout name loadlibrary.go builds and
		// registers for card: sections/card/layout.html.tmpl.
		cardLayoutName = SectionsDir + "/card/" + LayoutFile
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
	// includes card's layout by its layer-relative name and calls the section
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
	// Confirm the exact layer-relative layout name loadlibrary.go builds
	// for panel: sections/panel/layout.html.tmpl.
	if got, want := panel.Layout.Name(), SectionsDir+"/panel/"+LayoutFile; got != want {
		t.Errorf("panel.Layout.Name() = %q, want %q (the layer-relative layout name loadlibrary.go builds)", got, want)
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

// TestCheckLibraryExampleUnknownLayoutNameError (task 6) is the negative
// counterpart to TestCheckLibraryExampleComposesLibraryLayout: LoadLibrary's
// step-7 example check must still fail, as a positioned *LibraryError, when a
// layout's standard `{{ template "<name>" . }}` composition call names no
// library layout. Merging the layout set into one namespace lets a composition
// call resolve to any library layout; it does not make an unknown name
// resolve, and the failure must remain a templates error naming the offending
// layout file.
//
// "hello" is a slide whose layout calls `{{ template "hello" . }}`. The merged
// namespace (exampleLayoutNamespace) registers every library layout under the
// layer-relative name loadlibrary.go builds — slides/hello/layout.html.tmpl —
// never under the bare directory name, so "hello" names no template and the
// include cannot resolve. The library is single-layer and in-memory; executing
// hello's layout must fail step 7, and LoadLibrary must return that failure as
// a *LibraryError positioned at hello's own on-disk layout path
// (templates/slides/hello/layout.html.tmpl, the display-only Layout.Path), with
// the underlying html/template error — which names the missing "hello" — in the
// Message.
func TestCheckLibraryExampleUnknownLayoutNameError(t *testing.T) {
	// The bare template name. It names no library layout: every layout is
	// registered under its layer-relative path
	// (slides/hello/layout.html.tmpl), not its directory name.
	const missingLayoutName = "hello"

	fsys := baseLibraryFS()
	// hello's layout is the fixture's only layout, so it is the layout step 7
	// executes and, unambiguously, the layout the failure is attributed to.
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<section>{{ template "` + missingLayoutName + `" . }}</section>`),
	}

	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err == nil {
		t.Fatalf("LoadLibrary() = %+v, nil; want step 7 to reject the layout whose `{{ template %q . }}` names no library layout", lib, missingLayoutName)
	}
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
	}

	libErr := asLibraryError(t, err)
	// The failure is positioned at the layout that made the bad call, not at
	// the (absent) include target — the same layout path loadlibrary.go builds:
	// path.Join(root, slides/hello/layout.html.tmpl), root == TemplatesDir.
	if want := TemplatesDir + "/" + SlidesDir + "/hello/" + LayoutFile; libErr.Path != want {
		t.Errorf("Path = %q, want the offending layout %q", libErr.Path, want)
	}
	if !strings.Contains(libErr.Message, "execute") {
		t.Errorf("Message = %q, want a step-7 execute failure", libErr.Message)
	}
	if !strings.Contains(libErr.Message, missingLayoutName) {
		t.Errorf("Message = %q, want it to name the missing template %q", libErr.Message, missingLayoutName)
	}
}

// TestLayoutErrorReportsOnDiskPath (task 18) pins the load-time error-position
// contract for layout failures: a Go html/template error is reported at the
// invoking layout's ON-DISK path:line — the display-only Layout.Path — never at
// the layout's layer-relative name, which is a namespace key, not a file, and a
// quoted `{{ template }}` target name stays verbatim.
//
// Two failures are covered: an unresolved `{{ template "<name>" . }}` include
// reached at the step-7 example execution, and a layout that fails to parse at
// step 3. Each failure is a *LibraryError whose Path is the layout's on-disk
// path and whose Line is the line the Go error reports; the rendered Error()
// starts with that on-disk position, and no layer-relative name is ever printed
// as a file location. The unresolved case additionally quotes the missing
// target exactly as written.
func TestLayoutErrorReportsOnDiskPath(t *testing.T) {
	// A layer-relative name that is not registered (no such template).
	const missingTarget = SectionsDir + "/nope/" + LayoutFile

	t.Run("an unresolved template include reports at the on-disk path", func(t *testing.T) {
		fsys := baseLibraryFS()
		fsys["slides/outer/template.yaml"] = &fstest.MapFile{Data: []byte("name: outer\n")}
		fsys["slides/outer/layout.html.tmpl"] = &fstest.MapFile{
			Data: []byte(`<div>{{ template "` + missingTarget + `" . }}</div>`),
		}
		fsys["slides/outer/example.md"] = &fstest.MapFile{Data: []byte("# outer\n")}

		lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		if err == nil {
			t.Fatalf("LoadLibrary() = %+v, nil; want the unresolved include to fail step 7", lib)
		}
		libErr := asLibraryError(t, err)

		wantPath := TemplatesDir + "/" + SlidesDir + "/outer/" + LayoutFile
		assertOnDiskLayoutError(t, libErr, wantPath, 1)
		// The missing target is quoted exactly as written.
		if quoted := strconv.Quote(missingTarget); !strings.Contains(libErr.Message, quoted) {
			t.Errorf("Message = %q, want the target name quoted exactly as written %s", libErr.Message, quoted)
		}
		assertNoLayerRelativeLocation(t, libErr.Error(), wantPath, SlidesDir+"/outer/"+LayoutFile, missingTarget)
	})

	t.Run("a layout parse failure reports at the on-disk path", func(t *testing.T) {
		fsys := baseLibraryFS()
		fsys["slides/broken/template.yaml"] = &fstest.MapFile{Data: []byte("name: broken\n")}
		// An unclosed action: the layout fails to parse at step 3.
		fsys["slides/broken/layout.html.tmpl"] = &fstest.MapFile{Data: []byte(`{{ section "footer"`)}
		fsys["slides/broken/example.md"] = &fstest.MapFile{Data: []byte("# broken\n")}

		lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		if err == nil {
			t.Fatalf("LoadLibrary() = %+v, nil; want the unparseable layout to fail step 3", lib)
		}
		libErr := asLibraryError(t, err)

		wantPath := TemplatesDir + "/" + SlidesDir + "/broken/" + LayoutFile
		assertOnDiskLayoutError(t, libErr, wantPath, 1)
		assertNoLayerRelativeLocation(t, libErr.Error(), wantPath, SlidesDir+"/broken/"+LayoutFile)
	})
}

// TestLayoutErrorInIncludedLayoutReportsOnDiskPath pins the case feedback-7
// calls out: an error raised INSIDE an included layout, not in the including
// one. Slide layout A executes `{{ template "sections/b/layout.html.tmpl" . }}`;
// the included section layout B fails at execute time on its own line (a builtin
// `len` applied to a missing value). Step 7 executes A's layout against the
// merged library layout namespace, so Go's html/template attributes the failure
// to B's namespace key `sections/b/layout.html.tmpl`; layoutTemplateError must
// resolve that key through libraryLayoutPathResolver to B's ON-DISK Layout.Path,
// never fall back to the invoking A's path, and never print B's layer-relative
// name as a file location.
func TestLayoutErrorInIncludedLayoutReportsOnDiskPath(t *testing.T) {
	// The failing action sits on line 2 of B, while A's include sits on its own
	// line 1, so a position attributed to the wrong layout cannot coincide.
	fsys := baseLibraryFS()
	fsys["sections/b/template.yaml"] = &fstest.MapFile{Data: []byte("name: b\n")}
	fsys["sections/b/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte("<div>\n  {{ len .missing }}\n</div>\n"),
	}
	fsys["sections/b/example.md"] = &fstest.MapFile{Data: []byte("# b\n")}

	// A is a slide whose layout includes B by B's layer-relative layout name,
	// unguarded so step 7 reaches the include.
	fsys["slides/a/template.yaml"] = &fstest.MapFile{Data: []byte("name: a\n")}
	fsys["slides/a/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<div>{{ template "sections/b/layout.html.tmpl" . }}</div>`),
	}
	fsys["slides/a/example.md"] = &fstest.MapFile{Data: []byte("# a\n")}

	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err == nil {
		t.Fatalf("LoadLibrary() = %+v, nil; want the included layout's execute failure at step 7", lib)
	}
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
	}
	libErr := asLibraryError(t, err)

	// The failure is positioned at the INCLUDED layout B's on-disk file:line,
	// never at the including layout A's.
	includedPath := TemplatesDir + "/" + SectionsDir + "/b/" + LayoutFile
	includingPath := TemplatesDir + "/" + SlidesDir + "/a/" + LayoutFile
	assertOnDiskLayoutError(t, libErr, includedPath, 2)
	if libErr.Path == includingPath {
		t.Errorf("Path = %q, want the included layout, not the including layout %q", libErr.Path, includingPath)
	}
	// Neither B's nor A's layer-relative name may be printed as a file
	// location; B's on-disk path legitimately ends with its layer-relative
	// name, so assertNoLayerRelativeLocation strips it first.
	assertNoLayerRelativeLocation(t, libErr.Error(), includedPath,
		SectionsDir+"/b/"+LayoutFile, SlidesDir+"/a/"+LayoutFile)
}

// assertOnDiskLayoutError asserts libErr reports the on-disk layout path:line
// position: Path is wantPath, Line is wantLine, and Error() begins with
// "<wantPath>:<wantLine>: ".
func assertOnDiskLayoutError(t *testing.T, libErr *LibraryError, wantPath string, wantLine int) {
	t.Helper()
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want the on-disk layout path %q", libErr.Path, wantPath)
	}
	if libErr.Line != wantLine {
		t.Errorf("Line = %d, want %d", libErr.Line, wantLine)
	}
	if prefix := wantPath + ":" + strconv.Itoa(wantLine) + ": "; !strings.HasPrefix(libErr.Error(), prefix) {
		t.Errorf("Error() = %q, want it to begin with the on-disk position %q", libErr.Error(), prefix)
	}
}

// assertNoLayerRelativeLocation asserts no layer-relative name in names is
// printed as a file location. The on-disk path in onDiskPath legitimately ends
// with the layout's layer-relative name, so that occurrence is removed first;
// what remains must not carry "<name>:<line>" as a location.
func assertNoLayerRelativeLocation(t *testing.T, errStr, onDiskPath string, names ...string) {
	t.Helper()
	rest := strings.ReplaceAll(errStr, onDiskPath, "")
	for _, name := range names {
		if strings.Contains(rest, name+":") {
			t.Errorf("Error() = %q contains %q as a file location, want the on-disk path only", errStr, name+":")
		}
	}
}
