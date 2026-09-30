package template

import (
	"os"
	"path/filepath"
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

// TestLoadLibrarySectionHelpersHappyPathInt proves a library that uses the
// helpers loads cleanly: the slide layout calls `media`, `section` (with a
// literal dict supplying the target's required field) and `list`, and the
// section target's own example and layout are valid. This is the passing side
// of the same step-4 analysis the failure cases exercise, so it guards against
// over-rejection.
//
// The section call is guarded by `{{ with .body }}`. The load-time
// example smoke-execution (checkLibraryExamples, step 7) runs a layout with
// only the reserved deck/slide context and binds the `section` helper to
// LayoutFuncMap's parse-resolvable stub, which is not executable; the guard
// keeps that pass from invoking the stub while the call is still statically
// extracted and checked by checkSections, and a slide with a body renders the
// footer in a deck (slideData sets `.body` only when the slide has one).
func TestLoadLibrarySectionHelpersHappyPathInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	const hostLayout = "<section>{{ media \"logo.svg\" }}\n" +
		"{{ with .body }}{{ section \"footer\" (dict \"title\" \"Hi\") }}{{ end }}\n" +
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

	// Prove the guard did not hide the call from the checks: the loaded host
	// definition still carries the `{{ section "footer" (dict "title" "Hi") }}`
	// helper call, with its literal field key, for Section.HelperCalls (the
	// input to Registry.checkSections). A library would not load if that call
	// were invalid.
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
}
