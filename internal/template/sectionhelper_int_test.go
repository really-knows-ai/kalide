package template

import (
	htmltemplate "html/template"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for plan.phase-02.task-10:
// LoadLibrary over a real on-disk library (os.DirFS over t.TempDir()), proving
// that every load-time `section` helper check failure is path-qualified to the
// offending template's template.yaml at templates-dir-validation step 4
// (checkLibraryBuild -> Registry.Validate -> checkSections / Section.Walk)
// (requirements.requirement.section-helper-load-checks,
// requirement template-build-checks).
//
// The unit tests (sectionhelper_*_test.go) drive Registry.Validate directly
// with in-process fixtures; this file drives the whole loader on real files,
// so it also pins the path-qualification contract the unit tier cannot:
// Registry.Validate prefixes each helper failure with `template "<name>":`,
// and checkLibraryBuild maps that name to the template's ManifestPath, so the
// returned *LibraryError.Path is the offending template.yaml — not the layout
// the call was written in, nor the library root.
//
// The cases cover each helper check:
//   - an unknown target and a slide-usage target;
//   - a literal dict key the target does not declare;
//   - a required target field the call omits;
//   - a target declaring a child section with min > 0;
//   - a literal body heading naming a declared child section of the target;
//   - a helper-closed reference cycle.
//
// A happy-path library whose layouts use the helpers loads cleanly
// (TestLoadLibrarySectionHelpersHappyPathInt).
//
// It also carries plan.phase-02.task-19's integration coverage of the layered
// load over real temp dirs (library-layer-references): LoadDeckLibrary's layer
// roles (sole / external+local), a local→external section-helper call passing
// validation AND example execution while the local layer's own per-layer load
// defers those checks, a local call to an unresolved section still failing at
// the local manifest, the external root loaded alone through LoadLibraryAt with
// a reference to a local-only name rejected at the external file, and
// helper-closed cycles / over-deep chains still rejected once the layers merge.
//
// These tests touch the real filesystem, so they are guarded with
// testing.Short() and skipped under `go test -short`. They reuse writeFile
// (library_int_test.go) and asLibraryError (library_test.go).

// writeSectionHelperIntLibrary writes a real on-disk library under a fresh
// temp dir: library.yaml plus the given files, keyed by their path relative to
// templates/ (with "/" separators). It returns the project root to pass to
// os.DirFS.
func writeSectionHelperIntLibrary(t *testing.T, files map[string]string) string {
	t.Helper()
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, TemplatesDir)
	writeFile(t, filepath.Join(root, LibraryFile), []byte("name: demo\nformat: 1\n"))
	for rel, data := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), []byte(data))
	}
	return projectDir
}

// templateIntFiles returns the three required files of one on-disk template
// directory, kindDir/name, as a path-keyed map relative to templates/.
func templateIntFiles(kindDir, name, manifest, layout, example string) map[string]string {
	return map[string]string{
		kindDir + "/" + name + "/" + ManifestFile: manifest,
		kindDir + "/" + name + "/" + LayoutFile:   layout,
		kindDir + "/" + name + "/" + ExampleFile:  example,
	}
}

// slideIntFiles returns the three files of a schema-free slide-usage template.
// The placeholder "# name" example is never parsed: checkLibraryExample skips
// example validation for a template declaring no fields and no sections.
func slideIntFiles(name, layout string) map[string]string {
	return templateIntFiles(SlidesDir, name, "name: "+name+"\n", layout, "# "+name+"\n")
}

// sectionIntFiles returns the three files of a section-usage template with the
// given manifest and layout. The failing helper cases all stop at step 4,
// before example validation (step 7) runs, so the placeholder example is never
// parsed there; the happy path builds its section example explicitly.
func sectionIntFiles(name, manifest, layout string) map[string]string {
	return templateIntFiles(SectionsDir, name, manifest, layout, "# "+name+"\n")
}

// mergeIntFiles merges path-keyed file maps, later maps winning.
func mergeIntFiles(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for rel, data := range m {
			out[rel] = data
		}
	}
	return out
}

const (
	// sectionHelperIntFooterField is a footer section declaring one optional
	// text field, title.
	sectionHelperIntFooterField = "name: footer\nfields:\n  - name: title\n    type: text\n"
	// sectionHelperIntFooterRequired declares title required with no default,
	// so a helper call omitting it must fail.
	sectionHelperIntFooterRequired = "name: footer\nfields:\n  - name: title\n    type: text\n    required: true\n"
)

// TestLoadLibrarySectionHelperChecksInt is table-driven over the six
// section-helper check failures. Each library is written to a real temp dir,
// loaded through os.DirFS, and must fail with a *LibraryError whose Path is the
// offending template's template.yaml and whose Message is exactly the
// Registry.Validate helper error (with its `template "<name>":` prefix). That
// is the path-qualification checkLibraryBuild performs at step 4.
func TestLoadLibrarySectionHelperChecksInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	const hostManifest = TemplatesDir + "/" + SlidesDir + "/host/" + ManifestFile

	tests := []struct {
		name        string
		files       map[string]string
		wantPath    string
		wantMessage string
	}{
		{
			name: "unknown helper target is path-qualified to the calling template",
			files: mergeIntFiles(
				slideIntFiles("host", `{{ section "zzzz" }}`),
				sectionIntFiles("footer", sectionHelperIntFooterField, "<div></div>"),
			),
			wantPath:    hostManifest,
			wantMessage: `template "host": section helper call on layout line 1 names "zzzz", which is not a defined template`,
		},
		{
			name: "slide-usage helper target is path-qualified to the calling template",
			files: mergeIntFiles(
				slideIntFiles("host", `{{ section "hero" }}`),
				slideIntFiles("hero", "<section></section>"),
			),
			wantPath:    hostManifest,
			wantMessage: `template "host": section helper call on layout line 1 names "hero", which is a slide-usage template; the section helper requires a section-usage template`,
		},
		{
			name: "undeclared literal dict key is path-qualified to the calling template",
			files: mergeIntFiles(
				slideIntFiles("host", `{{ section "footer" (dict "nope" "v") }}`),
				sectionIntFiles("footer", sectionHelperIntFooterField, "<div></div>"),
			),
			wantPath:    hostManifest,
			wantMessage: `template "host": section helper call on layout line 1 supplies field "nope", which section template "footer" does not declare`,
		},
		{
			name: "omitted required target field is path-qualified to the calling template",
			files: mergeIntFiles(
				slideIntFiles("host", `{{ section "footer" }}`),
				sectionIntFiles("footer", sectionHelperIntFooterRequired, "<div></div>"),
			),
			wantPath:    hostManifest,
			wantMessage: `template "host": section helper call on layout line 1 omits required field "title" of section template "footer", which has no default`,
		},
		{
			name: "target with a min>0 child section is path-qualified to the calling template",
			files: mergeIntFiles(
				slideIntFiles("host", `{{ section "footer" }}`),
				sectionIntFiles("footer",
					"name: footer\nsections:\n  - name: widgets\n    accepted: [widget]\n    min: 1\n",
					"<div></div>"),
				sectionIntFiles("widget", "name: widget\n", "<div></div>"),
			),
			wantPath:    hostManifest,
			wantMessage: `template "host": section helper call on layout line 1 names section template "footer", which declares child section "widgets" with a minimum of 1; the section helper passes no child sections`,
		},
		{
			name: "literal body heading naming a child section is path-qualified to the calling template",
			files: mergeIntFiles(
				slideIntFiles("host", `{{ section "footer" (dict) "# widgets" }}`),
				sectionIntFiles("footer",
					"name: footer\nsections:\n  - name: widgets\n    accepted: [widget]\n",
					"<div></div>"),
				sectionIntFiles("widget", "name: widget\n", "<div></div>"),
			),
			wantPath:    hostManifest,
			wantMessage: `template "host": section helper call on layout line 1 supplies a body whose heading names child section "widgets" of section template "footer"; the section helper passes no child sections`,
		},
		{
			name: "helper-closed reference cycle is path-qualified to the root template",
			files: mergeIntFiles(
				sectionIntFiles("a", "name: a\n", `{{ section "b" }}`),
				sectionIntFiles("b", "name: b\n", `{{ section "a" }}`),
			),
			wantPath:    TemplatesDir + "/" + SectionsDir + "/a/" + ManifestFile,
			wantMessage: `template "a": section template reference cycle: sections/a → sections/b → sections/a`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := writeSectionHelperIntLibrary(t, tt.files)
			lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
			if err == nil {
				t.Fatalf("LoadLibrary() = %+v, nil; want the helper check to fail", lib)
			}
			if lib != nil {
				t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
			}
			libErr := asLibraryError(t, err)
			if libErr.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q (the offending template.yaml)", libErr.Path, tt.wantPath)
			}
			if libErr.Line != 0 {
				t.Errorf("Line = %d, want 0 (a whole-manifest build-check failure)", libErr.Line)
			}
			if libErr.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", libErr.Message, tt.wantMessage)
			}
		})
	}
}

// TestLoadLibrarySectionHelpersHappyPathInt proves a library whose host slide
// layout calls the `section` helper unconditionally loads cleanly and that the
// step-7 example execution actually renders the target's HTML. The layout
// calls `media`, `section` (with a literal dict supplying the target's
// required field) and `list`, and the section target's own example and layout
// are valid. This is the passing side of the same step-4 analysis the failure
// cases exercise, so it guards against over-rejection.
//
// The `{{ section "footer" (dict "title" "Hi") }}` call carries no
// `{{ with .body }}` guard, so executing it is unconditional. checkLibraryExample
// (step 7) re-parses the host layout under LayoutFuncMap with its `section`
// entry overridden by the load-time exampleSectionHelper, so the host layout
// executes the helper and renders the footer target rather than hitting
// LayoutFuncMap's parse-only stub (which errors with "no render-time
// implementation bound").
//
// LoadLibrary discards the rendered bytes — it returns the *Library, not the
// output — so there is no public API through which to observe the HTML step 7
// produced. This test therefore repeats step 7's exact binding in-package:
// LayoutFuncMap with `section` overridden by exampleSectionHelper(lib, nil)
// (host is a slide layout, so its call's .item.parent is nil), then parses
// host's LayoutText, executes it against the same reserved deck/slide example
// context and asserts the captured output contains the target's
// `<footer>Hi</footer>` and carries neither the stub's error nor any other
// execute failure.
//
// A second, failing case proves step-7 execution is a real gate over and above
// the step-4 static analysis: `(dict "title" 42)` passes the static check (the
// key is declared and the required field is supplied; step 4 checks keys, not
// value types) but the number violates the target's text field only when the
// call executes, so LoadLibrary fails with a step-7 execute error.
func TestLoadLibrarySectionHelpersHappyPathInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	const hostLayout = "<section>{{ media \"logo.svg\" }}\n" +
		"{{ section \"footer\" (dict \"title\" \"Hi\") }}\n" +
		"<ul>{{ range list \"a\" \"b\" }}<li>{{ . }}</li>{{ end }}</ul></section>\n"

	files := mergeIntFiles(
		templateIntFiles(SlidesDir, "host", "name: host\n", hostLayout, "# host\n"),
		templateIntFiles(SectionsDir, "footer", sectionHelperIntFooterRequired,
			"<footer>{{ .title }}</footer>", "```\ntitle: Hi\n```\nDetails.\n"),
		map[string]string{"media/logo.svg": "<svg/>"},
	)

	projectDir := writeSectionHelperIntLibrary(t, files)
	lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: a helper-using library loads", err)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}

	for _, name := range []string{"host", "footer"} {
		lt, _, ok := lib.TemplateByName(name)
		if !ok || lt == nil {
			t.Fatalf("TemplateByName(%q) not found", name)
		}
		if lt.Definition == nil {
			t.Errorf("checkLibraryBuild left %q.Definition nil", name)
		}
	}

	// Prove the unconditionally-executed call was still statically extracted:
	// the loaded host definition carries the
	// `{{ section "footer" (dict "title" "Hi") }}` helper call, with its
	// literal field key, for Section.HelperCalls (the input to
	// Registry.checkSections). A library would not load if that call were
	// invalid.
	host, _, ok := lib.TemplateByName("host")
	if !ok || host == nil || host.Definition == nil {
		t.Fatal(`TemplateByName("host") definition not loaded`)
	}
	calls, err := NewSection(nil).HelperCalls(host.Definition)
	if err != nil {
		t.Fatalf("HelperCalls(host) error = %v, want nil", err)
	}
	if len(calls) != 1 {
		t.Fatalf("HelperCalls(host) = %d calls, want 1: %+v", len(calls), calls)
	}
	if call := calls[0]; call.Name != "footer" || !call.FieldsLiteral ||
		len(call.FieldKeys) != 1 || call.FieldKeys[0] != "title" {
		t.Errorf("HelperCalls(host)[0] = %+v, want name=footer FieldsLiteral FieldKeys=[title]", call)
	}

	// LoadLibrary discards step 7's rendered bytes, so repeat step 7's exact
	// binding here to observe what it executed: LayoutFuncMap with its
	// `section` entry overridden by the load-time exampleSectionHelper, bound
	// to nil caller fields because host is a slide layout.
	step7FuncMap := LayoutFuncMap(lib.Media, "/"+MediaDir)
	step7FuncMap["section"] = exampleSectionHelper(lib, nil)
	step7Layout, err := htmltemplate.New("host").Funcs(step7FuncMap).Parse(host.LayoutText)
	if err != nil {
		t.Fatalf("parse host layout under the step-7 section binding: %v", err)
	}
	var rendered strings.Builder
	if err := step7Layout.Execute(&rendered, map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}); err != nil {
		t.Fatalf("execute host layout under the step-7 section binding: %v, want nil (LoadLibrary's own step-7 execution succeeded)", err)
	}
	got := rendered.String()
	if !strings.Contains(got, "<footer>Hi</footer>") {
		t.Errorf("step-7 rendered host layout = %q, want it to contain the target's HTML <footer>Hi</footer>", got)
	}
	if strings.Contains(got, "no render-time implementation bound") {
		t.Errorf("step-7 rendered host layout = %q contains the parse-only section stub's error; the load-time helper must be bound", got)
	}

	// Step-7 execution is a real gate beyond the step-4 static analysis: a call
	// that passes the key-only static check can still violate the target's
	// field rules when it executes. `(dict "title" 42)` supplies the declared
	// required field (so step 4 accepts it) but gives the target's text field a
	// number, which CheckValues rejects at example time.
	t.Run("a literal field value of the wrong type fails at step 7", func(t *testing.T) {
		badFiles := mergeIntFiles(
			templateIntFiles(SlidesDir, "host", "name: host\n",
				`{{ section "footer" (dict "title" 42) }}`, "# host\n"),
			templateIntFiles(SectionsDir, "footer", sectionHelperIntFooterRequired,
				"<footer>{{ .title }}</footer>", "```\ntitle: Hi\n```\nDetails.\n"),
		)
		projectDir := writeSectionHelperIntLibrary(t, badFiles)
		badLib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
		if err == nil {
			t.Fatalf("LoadLibrary() = %+v, nil; want the wrong-typed field value to fail the step-7 example execution", badLib)
		}
		if badLib != nil {
			t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", badLib)
		}
		libErr := asLibraryError(t, err)
		if want := TemplatesDir + "/" + SlidesDir + "/host/" + LayoutFile; libErr.Path != want {
			t.Errorf("Path = %q, want the offending host layout %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, "execute:") || !strings.Contains(libErr.Message, "expected a text field") {
			t.Errorf("Message = %q, want a step-7 execute failure naming the text-field type error", libErr.Message)
		}
	})
}

// writeSectionHelperIntRoot writes library.yaml (named name) plus the given
// path-keyed files directly at root — an arbitrary library directory — and
// returns root. It is writeSectionHelperIntLibrary's counterpart for a layer
// that is not the deck-root templates/: the same path-keyed fixture shape
// (relative to the layer's templates/ root), written to an external library
// root so a test can build both layers of a layered deck.
func writeSectionHelperIntRoot(t *testing.T, root, name string, files map[string]string) string {
	t.Helper()
	writeFile(t, filepath.Join(root, LibraryFile), []byte("name: "+name+"\nformat: 1\n"))
	for rel, data := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), []byte(data))
	}
	return root
}

// writeSectionHelperIntLayeredDeck writes a real two-layer deck: a local
// library at deckRoot/templates/ and an external library at a fresh temp dir,
// each from a path-keyed file map relative to that layer's own templates/
// root. It returns the deck root and the external root — exactly the
// (deckRoot, configured) arguments LoadDeckLibrary takes.
func writeSectionHelperIntLayeredDeck(t *testing.T, localFiles, externalFiles map[string]string) (deckRoot, externalRoot string) {
	t.Helper()
	deckRoot = t.TempDir()
	externalRoot = t.TempDir()
	writeSectionHelperIntRoot(t, filepath.Join(deckRoot, TemplatesDir), "local", localFiles)
	writeSectionHelperIntRoot(t, externalRoot, "external", externalFiles)
	return deckRoot, externalRoot
}

// TestLoadDeckLibraryLayerRolesInt proves LoadDeckLibrary records each loaded
// layer's Role from the resolution (layered-template-library): a single layer —
// the deck-root templates/ alone or a configured external root alone — is sole,
// while a two-layer deck's first layer (the external `templates:` root) is
// external and its second (the deck-root templates/) is local. The role is what
// render.RenderSlide reads through reg.LayerOf to derive each layer's served
// URL prefixes, so it must be on the merged Library's Layers, set before the
// merge.
func TestLoadDeckLibraryLayerRolesInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	t.Run("a sole local layer is sole", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := writeSectionHelperIntRoot(t, filepath.Join(deckRoot, TemplatesDir), "local",
			slideIntFiles("host", "<section></section>"))

		lib, err := LoadDeckLibrary(deckRoot, "")
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1 (the deck-root templates/ is the only layer)", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != localRoot {
			t.Errorf("Layers[0].RootPath = %q, want %q", lib.Layers[0].RootPath, localRoot)
		}
		if lib.Layers[0].Role != LayerRoleSole {
			t.Errorf("Layers[0].Role = %v, want LayerRoleSole", lib.Layers[0].Role)
		}
	})

	t.Run("a sole external layer is sole", func(t *testing.T) {
		deckRoot := t.TempDir() // deliberately no deck-root templates/
		externalRoot := t.TempDir()
		writeSectionHelperIntRoot(t, externalRoot, "external",
			slideIntFiles("host", "<section></section>"))

		lib, err := LoadDeckLibrary(deckRoot, externalRoot)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1 (the external root is the deck's only layer)", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != externalRoot {
			t.Errorf("Layers[0].RootPath = %q, want %q", lib.Layers[0].RootPath, externalRoot)
		}
		if lib.Layers[0].Role != LayerRoleSole {
			t.Errorf("Layers[0].Role = %v, want LayerRoleSole (a lone configured root is the deck's whole library)", lib.Layers[0].Role)
		}
	})

	t.Run("a two-layer deck marks the external then the local layer", func(t *testing.T) {
		deckRoot, externalRoot := writeSectionHelperIntLayeredDeck(t,
			slideIntFiles("host", "<section></section>"),
			slideIntFiles("ext", "<section></section>"))
		localRoot := filepath.Join(deckRoot, TemplatesDir)

		lib, err := LoadDeckLibrary(deckRoot, externalRoot)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if len(lib.Layers) != 2 {
			t.Fatalf("len(Layers) = %d, want 2", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != externalRoot {
			t.Errorf("Layers[0].RootPath = %q, want the external layer %q", lib.Layers[0].RootPath, externalRoot)
		}
		if lib.Layers[0].Role != LayerRoleExternal {
			t.Errorf("Layers[0].Role = %v, want LayerRoleExternal", lib.Layers[0].Role)
		}
		if lib.Layers[1].RootPath != localRoot {
			t.Errorf("Layers[1].RootPath = %q, want the local layer %q", lib.Layers[1].RootPath, localRoot)
		}
		if lib.Layers[1].Role != LayerRoleLocal {
			t.Errorf("Layers[1].Role = %v, want LayerRoleLocal", lib.Layers[1].Role)
		}
	})
}

// TestLoadDeckLibraryCrossLayerSectionHelperInt proves a local-layer template's
// `section` helper call resolves an external-layer section through the merged
// library (library-layer-references), over a real two-layer deck:
//
//   - LoadDeckLibrary succeeds when a local slide calls an external section,
//     and the merged load's step-7 example execution actually renders the
//     external section's HTML — the call's build check AND example run pass;
//   - the local layer's own per-layer load defers exactly those two checks
//     (libraryLayerDeferred): the same local root loaded in full mode
//     (libraryLayerFull) rejects the call, because the external section is not
//     in the local namespace, while the deferred load accepts it and leaves
//     the checks to the merge;
//   - a local call to an unresolved section name still fails, path-qualified
//     to the local calling template's template.yaml, proving the merged pass
//     is not a blanket amnesty.
func TestLoadDeckLibraryCrossLayerSectionHelperInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// newDeck builds a two-layer deck whose local slide host calls the given
	// section name; the external layer carries the section ext-section.
	newDeck := func(t *testing.T, callName string) (deckRoot, externalRoot, localRoot string) {
		t.Helper()
		localFiles := slideIntFiles("host", `<section>{{ section "`+callName+`" }}</section>`)
		externalFiles := sectionIntFiles("ext-section", "name: ext-section\n", "<footer>external</footer>")
		deckRoot, externalRoot = writeSectionHelperIntLayeredDeck(t, localFiles, externalFiles)
		return deckRoot, externalRoot, filepath.Join(deckRoot, TemplatesDir)
	}

	t.Run("a local template's external section call loads and executes", func(t *testing.T) {
		deckRoot, externalRoot, _ := newDeck(t, "ext-section")

		lib, err := LoadDeckLibrary(deckRoot, externalRoot)
		if err != nil {
			t.Fatalf("LoadDeckLibrary() error = %v, want nil: a local template may call an external section once the layers are merged", err)
		}

		host, _, ok := lib.TemplateByName("host")
		if !ok || host == nil {
			t.Fatal(`TemplateByName("host") not found in the merged library`)
		}
		// LoadDeckLibrary discards step 7's rendered bytes, so repeat its exact
		// binding — LayoutFuncMap with `section` overridden by the merged
		// exampleSectionHelper — and assert the external section's HTML is
		// spliced into the host's output, proving the cross-layer call executed
		// rather than merely passing the static check.
		step7 := LayoutFuncMap(lib.Media, "/"+MediaDir)
		step7["section"] = exampleSectionHelper(lib, nil)
		layout, err := htmltemplate.New("host").Funcs(step7).Parse(host.LayoutText)
		if err != nil {
			t.Fatalf("parse host layout under the merged example binding: %v", err)
		}
		var rendered strings.Builder
		if err := layout.Execute(&rendered, map[string]any{
			"deck":  emptyExampleDeckContext(),
			"slide": emptyExampleSlideContext(),
		}); err != nil {
			t.Fatalf("execute host layout under the merged example binding: %v, want nil (LoadDeckLibrary's own step-7 execution succeeded)", err)
		}
		if got := rendered.String(); !strings.Contains(got, "<footer>external</footer>") {
			t.Errorf("merged example execution rendered %q, want the external section's HTML <footer>external</footer>", got)
		}
	})

	t.Run("the local layer's per-layer load defers the cross-template checks", func(t *testing.T) {
		_, _, localRoot := newDeck(t, "ext-section")

		// Full mode — what a standalone/external root uses — rejects the call:
		// ext-section is not in the local layer's own namespace.
		if layer, err := loadLibrary(os.DirFS(localRoot), ".", localRoot, libraryLayerFull); err == nil {
			t.Fatalf("loadLibrary(local, libraryLayerFull) = %+v, nil; want the external-section call rejected in the local namespace", layer)
		}

		// Deferred mode — what LoadDeckLibrary uses for a layered deck's local
		// layer — accepts it: the cross-template checks are deferred to the
		// merged load.
		layer, err := loadLibrary(os.DirFS(localRoot), ".", localRoot, libraryLayerDeferred)
		if err != nil {
			t.Fatalf("loadLibrary(local, libraryLayerDeferred) error = %v, want nil: the local layer's cross-template checks are deferred", err)
		}
		if layer == nil {
			t.Fatal("loadLibrary(local, libraryLayerDeferred) returned a nil layer with a nil error")
		}
	})

	t.Run("a local call to an unresolved section still fails at the local manifest", func(t *testing.T) {
		deckRoot, externalRoot, localRoot := newDeck(t, "nope")

		lib, err := LoadDeckLibrary(deckRoot, externalRoot)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		wantPath := path.Join(localRoot, SlidesDir, "host", ManifestFile)
		if libErr.Path != wantPath {
			t.Errorf("Path = %q, want the local calling template's manifest %q", libErr.Path, wantPath)
		}
		if libErr.Line != 0 {
			t.Errorf("Line = %d, want 0 (a whole-manifest build-check failure)", libErr.Line)
		}
		if !strings.Contains(libErr.Message, `"nope"`) {
			t.Errorf("Message = %q, want it to name the unresolved section %q", libErr.Message, "nope")
		}
		if !strings.Contains(libErr.Message, "not a defined template") {
			t.Errorf("Message = %q, want the unresolved-name rejection", libErr.Message)
		}
	})
}

// TestLoadLibraryAtExternalStandaloneInt proves a layered deck's external
// `templates:` layer, loaded ALONE through template.LoadLibraryAt (loadLibrary
// in libraryLayerFull mode — the mode LoadDeckLibrary loads the external layer
// in), is validated as a self-contained standalone library
// (external-template-library, library-layer-references):
//
//   - an external library whose slide calls its own section loads;
//   - an external slide's call to a section only the deck's local layer carries
//     is rejected, path-qualified to the external calling template's
//     template.yaml — and LoadDeckLibrary rejects the same deck at the same
//     external error, because the external layer is validated fully before the
//     merge. An external layer may never depend on the local one.
func TestLoadLibraryAtExternalStandaloneInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	t.Run("a self-contained external library validates standalone", func(t *testing.T) {
		root := t.TempDir()
		writeSectionHelperIntRoot(t, root, "external", mergeIntFiles(
			slideIntFiles("host", `<section>{{ section "ext-section" }}</section>`),
			sectionIntFiles("ext-section", "name: ext-section\n", "<footer>external</footer>"),
		))

		lib, err := LoadLibraryAt(root)
		if err != nil {
			t.Fatalf("LoadLibraryAt() error = %v, want nil: every reference resolves in the external layer itself", err)
		}
		if lib.Meta.Name != "external" {
			t.Errorf("Meta.Name = %q, want %q", lib.Meta.Name, "external")
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1", len(lib.Layers))
		}
		if lib.Layers[0].Role != LayerRoleSole {
			t.Errorf("Layers[0].Role = %v, want LayerRoleSole (a single-root load leaves the zero-value role)", lib.Layers[0].Role)
		}
	})

	t.Run("an external reference to a local-only section fails at the external file", func(t *testing.T) {
		// A two-layer deck whose local layer carries the section the external
		// layer's slide mistakenly calls: the name is real in the deck, but not
		// in the external layer alone.
		localFiles := sectionIntFiles("local-section", "name: local-section\n", "<div></div>")
		externalFiles := slideIntFiles("host", `<section>{{ section "local-section" }}</section>`)
		deckRoot, externalRoot := writeSectionHelperIntLayeredDeck(t, localFiles, externalFiles)

		lib, err := LoadLibraryAt(externalRoot)
		if lib != nil {
			t.Fatalf("LoadLibraryAt() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		wantPath := path.Join(externalRoot, SlidesDir, "host", ManifestFile)
		if libErr.Path != wantPath {
			t.Errorf("Path = %q, want the external calling template's manifest %q", libErr.Path, wantPath)
		}
		if libErr.Line != 0 {
			t.Errorf("Line = %d, want 0", libErr.Line)
		}
		if !strings.Contains(libErr.Message, `"local-section"`) {
			t.Errorf("Message = %q, want it to name the local-only section %q", libErr.Message, "local-section")
		}
		if !strings.Contains(libErr.Message, "not a defined template") {
			t.Errorf("Message = %q, want the self-containment rejection", libErr.Message)
		}

		// The same deck cannot load through LoadDeckLibrary either: the external
		// layer is validated fully before any merge, so its dependence on the
		// local layer fails at the same external file.
		deckLib, err := LoadDeckLibrary(deckRoot, externalRoot)
		if deckLib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", deckLib)
		}
		if deckErr := asLibraryError(t, err); deckErr.Path != wantPath {
			t.Errorf("LoadDeckLibrary Path = %q, want the external manifest %q", deckErr.Path, wantPath)
		}
	})
}

// TestLoadDeckLibraryCrossLayerCycleAndDepthInt proves helper-closed reference
// cycles and over-deep declared-section chains are still rejected once a
// layered deck's layers are merged.
//
// A true cycle SPANNING the external and local layers is not constructible: a
// cross-layer cycle would need both a local→external edge and an external→local
// edge, and the external layer is loaded fully and validated standalone before
// the merge, so its reference to any local-only name (helper call or declared
// section) is rejected at the external file
// (TestLoadLibraryAtExternalStandaloneInt). The covered cycles are therefore
// the two representable shapes inside a two-layer deck — a local↔local cycle,
// caught by the merged pass after the local layer's deferred load skips step 4,
// and an external-internal cycle, caught at the external layer's own full load
// — plus a local declared-section chain deeper than the six-heading-level limit,
// likewise caught by the merged pass.
func TestLoadDeckLibraryCrossLayerCycleAndDepthInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// cycleSections is a pair of sections that call each other, closing a
	// helper-reference cycle a → b → a.
	cycleSections := mergeIntFiles(
		sectionIntFiles("a", "name: a\n", `{{ section "b" }}`),
		sectionIntFiles("b", "name: b\n", `{{ section "a" }}`),
	)
	const wantCycle = `template "a": section template reference cycle: sections/a → sections/b → sections/a`

	t.Run("a local↔local helper-closed cycle is rejected at the merged pass", func(t *testing.T) {
		// The local layer is loaded deferred, so its per-layer step-4 build
		// check is skipped; the merged pass catches the cycle.
		deckRoot, externalRoot := writeSectionHelperIntLayeredDeck(t,
			cycleSections,
			sectionIntFiles("ext-section", "name: ext-section\n", "<footer>external</footer>"))
		localRoot := filepath.Join(deckRoot, TemplatesDir)

		lib, err := LoadDeckLibrary(deckRoot, externalRoot)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		if wantPath := path.Join(localRoot, SectionsDir, "a", ManifestFile); libErr.Path != wantPath {
			t.Errorf("Path = %q, want the local cycle root's manifest %q", libErr.Path, wantPath)
		}
		if libErr.Message != wantCycle {
			t.Errorf("Message = %q, want %q", libErr.Message, wantCycle)
		}
	})

	t.Run("an external-internal cycle is rejected at the external standalone load", func(t *testing.T) {
		deckRoot, externalRoot := writeSectionHelperIntLayeredDeck(t,
			slideIntFiles("host", "<section></section>"),
			cycleSections)

		lib, err := LoadDeckLibrary(deckRoot, externalRoot)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		if wantPath := path.Join(externalRoot, SectionsDir, "a", ManifestFile); libErr.Path != wantPath {
			t.Errorf("Path = %q, want the external cycle root's manifest %q", libErr.Path, wantPath)
		}
		if libErr.Message != wantCycle {
			t.Errorf("Message = %q, want %q", libErr.Message, wantCycle)
		}
	})

	t.Run("a local declared-section chain deeper than the heading limit is rejected at the merged pass", func(t *testing.T) {
		names := []string{"d1", "d2", "d3", "d4", "d5", "d6", "d7"}
		localFiles := map[string]string{}
		for i, name := range names {
			manifest := "name: " + name + "\n"
			if i+1 < len(names) {
				child := names[i+1]
				manifest += "sections:\n  - name: " + child + "\n    accepted: [" + child + "]\n"
			}
			localFiles = mergeIntFiles(localFiles, sectionIntFiles(name, manifest, "<div></div>"))
		}
		deckRoot, externalRoot := writeSectionHelperIntLayeredDeck(t,
			localFiles,
			sectionIntFiles("ext-section", "name: ext-section\n", "<footer>external</footer>"))
		localRoot := filepath.Join(deckRoot, TemplatesDir)

		lib, err := LoadDeckLibrary(deckRoot, externalRoot)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		if wantPath := path.Join(localRoot, SectionsDir, "d1", ManifestFile); libErr.Path != wantPath {
			t.Errorf("Path = %q, want the over-deep local chain's root manifest %q", libErr.Path, wantPath)
		}
		if !strings.Contains(libErr.Message, "7 levels deep") {
			t.Errorf("Message = %q, want it to report the 7-level chain", libErr.Message)
		}
		if !strings.Contains(libErr.Message, "exceeding the 6-heading-level limit") {
			t.Errorf("Message = %q, want it to name the six-heading-level limit", libErr.Message)
		}
	})
}
