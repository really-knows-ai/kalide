package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for plan.phase-02.task-7: the
// RETURNING `kalide start` and `kalide templates` paths over a library resolved
// from an external `templates:` root (external-template-library).
//
// It does NOT call the blocking runStart serve loop: only the paths that fail
// before binding are exercised (a missing/invalid configured library, and a
// library that does not resolve the deck's default `default` theme), plus
// `kalide templates` listing and showing a template from the external resolved
// root and erroring per templates-dir-required when none resolves. Serving and
// live reload over the external root are covered by the phase-05 e2e
// (task-1) and the server int test (task-8).
//
// It writes real decks and external libraries to temporary directories and
// chdirs into the deck (runStart/runTemplates resolve the deck as the current
// working directory), so it is an integration test and is skipped under
// -short. It reuses runCLI (cli_test.go).
func TestStartExternalLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test writes real decks and external libraries to disk")
	}

	t.Run("start fails naming the resolved path when the configured path is missing", func(t *testing.T) {
		parent, deckDir, _ := newExternalCLIDeck(t)
		writeExternalFile(t, filepath.Join(deckDir, "kalide.yaml"),
			"title: External Deck\ntheme: default\ntemplates: ../nope\n")

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatalf("Run(start) exit = 0, want non-zero for a missing configured templates: path")
		}
		if stdout != "" {
			t.Fatalf("Run(start) stdout = %q, want empty (nothing served)", stdout)
		}
		want := filepath.Join(parent, "nope")
		if !strings.Contains(stderr, want) {
			t.Fatalf("Run(start) stderr = %q, want it to name the resolved path %q", stderr, want)
		}
		if !strings.Contains(stderr, "not found") {
			t.Fatalf("Run(start) stderr = %q, want it to say the directory was not found", stderr)
		}
	})

	t.Run("start fails naming the resolved path when the configured path is not a valid library", func(t *testing.T) {
		_, _, libDir := newExternalCLIDeck(t)
		writeExternalFile(t, filepath.Join(libDir, "library.yaml"), "name: Bad_Name\nformat: 1\n")

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatalf("Run(start) exit = 0, want non-zero for an invalid external library")
		}
		if stdout != "" {
			t.Fatalf("Run(start) stdout = %q, want empty (nothing served)", stdout)
		}
		want := filepath.Join(libDir, "library.yaml")
		if !strings.Contains(stderr, want) {
			t.Fatalf("Run(start) stderr = %q, want it to name the resolved path %q", stderr, want)
		}
		if !strings.Contains(stderr, "not a valid name") {
			t.Fatalf("Run(start) stderr = %q, want the external library.yaml's name error", stderr)
		}
	})

	t.Run("start fails when the library has no default theme", func(t *testing.T) {
		// The deck omits `theme`, so it defaults to `default`; the external
		// library's themes/ has only `plain`, so the deck's default theme
		// does not resolve in the resolved library and validation fails
		// before anything binds.
		_, _, libDir := newExternalCLIDeck(t)
		if err := os.RemoveAll(filepath.Join(libDir, "themes", "default")); err != nil {
			t.Fatalf("remove themes/default: %v", err)
		}
		writeExternalFile(t, filepath.Join(libDir, "themes", "plain", "theme.css"), "body {}\n")

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatalf("Run(start) exit = 0, want non-zero when the default theme does not resolve")
		}
		if stdout != "" {
			t.Fatalf("Run(start) stdout = %q, want empty (nothing served)", stdout)
		}
		if !strings.Contains(stderr, "default") {
			t.Fatalf("Run(start) stderr = %q, want it to name the unresolvable default theme", stderr)
		}
		if !strings.Contains(stderr, "kalide.yaml") {
			t.Fatalf("Run(start) stderr = %q, want the positioned deck error", stderr)
		}
	})

	t.Run("start fails naming the resolved path when the default theme's stylesheet is missing", func(t *testing.T) {
		// The deck's default `default` theme exists in the external library
		// but without its required theme.css, so the resolved library cannot
		// supply it; the loader reports the resolved theme path before
		// anything binds.
		_, _, libDir := newExternalCLIDeck(t)
		if err := os.Remove(filepath.Join(libDir, "themes", "default", "theme.css")); err != nil {
			t.Fatalf("remove default theme.css: %v", err)
		}

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatalf("Run(start) exit = 0, want non-zero when the default theme's stylesheet is missing")
		}
		if stdout != "" {
			t.Fatalf("Run(start) stdout = %q, want empty (nothing served)", stdout)
		}
		want := filepath.Join(libDir, "themes", "default", "theme.css")
		if !strings.Contains(stderr, want) {
			t.Fatalf("Run(start) stderr = %q, want it to name the resolved theme path %q", stderr, want)
		}
	})

	t.Run("templates lists every template from the external resolved root", func(t *testing.T) {
		newExternalCLIDeck(t)

		code, stdout, stderr := runCLI("templates")
		if code != 0 {
			t.Fatalf("Run(templates) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates) stderr = %q, want empty", stderr)
		}
		for _, want := range []string{
			"Templates:",
			"hello",
			"slide",
			"external hello description",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(templates) stdout = %q, want %q from the external library", stdout, want)
			}
		}
	})

	t.Run("templates shows a template from the external resolved root", func(t *testing.T) {
		newExternalCLIDeck(t)

		code, stdout, stderr := runCLI("templates", "hello")
		if code != 0 {
			t.Fatalf("Run(templates hello) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates hello) stderr = %q, want empty", stderr)
		}
		for _, want := range []string{
			"hello (slide)",
			"external hello description",
			"Fields:",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(templates hello) stdout = %q, want %q from the external library", stdout, want)
			}
		}
	})

	t.Run("templates errors per templates-dir-required when no library resolves", func(t *testing.T) {
		parent, deckDir, _ := newExternalCLIDeck(t)
		writeExternalFile(t, filepath.Join(deckDir, "kalide.yaml"),
			"title: External Deck\ntheme: default\ntemplates: ../nope\n")

		code, stdout, stderr := runCLI("templates")
		if code == 0 {
			t.Fatalf("Run(templates) exit = 0, want non-zero when no library resolves")
		}
		if stdout != "" {
			t.Fatalf("Run(templates) stdout = %q, want empty", stdout)
		}
		want := filepath.Join(parent, "nope")
		if !strings.Contains(stderr, want) {
			t.Fatalf("Run(templates) stderr = %q, want it to name the resolved path %q", stderr, want)
		}
	})
}

// newExternalCLIDeck writes a real deck at <parent>/deck whose kalide.yaml sets
// `templates: ../shared-lib` (and which has no local templates/) plus a valid
// external library at the sibling <parent>/shared-lib, then chdirs into the
// deck so runStart/runTemplates resolve it as the current working directory.
// It returns the parent directory, the deck directory and the external library
// directory.
func newExternalCLIDeck(t *testing.T) (parent, deckDir, libDir string) {
	t.Helper()
	parent = t.TempDir()
	deckDir = filepath.Join(parent, "deck")
	libDir = filepath.Join(parent, "shared-lib")

	writeExternalFile(t, filepath.Join(deckDir, "kalide.yaml"),
		"title: External Deck\ntheme: default\ntemplates: ../shared-lib\n")
	writeExternalFile(t, filepath.Join(deckDir, "slides", "1-hello.md"),
		"---\ntemplate: hello\ntitle: Hi\n---\n")
	writeExternalCLILibrary(t, libDir, "shared-lib", "external hello description",
		"<section><h1>{{.title}}</h1></section>\n")

	t.Chdir(deckDir)
	return parent, deckDir, libDir
}

// writeExternalCLILibrary writes a minimal, valid external template library to
// dir: library.yaml, one slide template (hello) with the given manifest
// description and layout, themes/default/theme.css and media/logo.svg.
func writeExternalCLILibrary(t *testing.T, dir, name, description, layout string) {
	t.Helper()
	writeExternalFile(t, filepath.Join(dir, "library.yaml"),
		"name: "+name+"\ndescription: "+description+"\nformat: 1\n")
	writeExternalFile(t, filepath.Join(dir, "slides", "hello", "template.yaml"),
		"description: "+description+"\n"+
			"fields:\n"+
			"  - name: title\n"+
			"    type: text\n"+
			"    required: true\n"+
			"body:\n"+
			"  mode: optional\n")
	writeExternalFile(t, filepath.Join(dir, "slides", "hello", "layout.html.tmpl"), layout)
	writeExternalFile(t, filepath.Join(dir, "slides", "hello", "example.md"),
		"---\ntemplate: hello\ntitle: Example\n---\n")
	writeExternalFile(t, filepath.Join(dir, "themes", "default", "theme.css"), "body { margin: 0; }\n")
	writeExternalFile(t, filepath.Join(dir, "media", "logo.svg"), "<svg/>\n")
}

// writeExternalFile writes content to path, creating parent directories as
// needed and failing the test on error.
func writeExternalFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
