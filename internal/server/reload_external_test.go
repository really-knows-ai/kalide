package server

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// This file is the integration-test deliverable for plan.phase-02.task-8: the
// server reload pipeline over a library resolved from an external `templates:`
// root (external-template-library). It proves that, with `templates:
// ../shared-lib` and no local templates/, the built-in pipeline loads,
// validates and renders the deck from the resolved root; the /templates gallery
// and the media/theme routes serve files from that root; each reload
// re-resolves the root from kalide.yaml, so a library edit and a retargeted
// `templates:` path are picked up without restarting the server; and an invalid
// resolved library publishes the positioned error page naming the resolved
// path while the server keeps running and recovers.
//
// It writes a real deck and real external libraries to temporary directories
// and drives NewReloader/(Reloader).Reload directly (no watcher, no SSE
// stream), so it is an integration test and is skipped under -short. It reuses
// the helpers declared in reload_int_test.go (writeFile, readFile) and
// server_test.go (mustListen, testPage, getBody, rootPath).
func TestReloaderExternalLibraryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads real decks and libraries from disk")
	}

	parent := t.TempDir()
	deckDir := filepath.Join(parent, "deck")
	libDir := filepath.Join(parent, "shared-lib")
	otherLibDir := filepath.Join(parent, "other-lib")

	writeFile(t, filepath.Join(deckDir, "kalide.yaml"),
		"title: External Deck\ntheme: default\ntemplates: ../shared-lib\n")
	writeFile(t, filepath.Join(deckDir, "slides", "1-hello.md"),
		"---\ntemplate: hello\ntitle: Hi\n---\n")
	writeExternalLibrary(t, libDir, "shared-lib", "external hello description",
		"<section><h1>{{.title}}</h1></section>\n")
	writeExternalLibrary(t, otherLibDir, "other-lib", "other hello description",
		"<section><h1 class=\"other\">{{.title}}</h1></section>\n")

	// Load the external library, registry and themes the way `kalide start`
	// does, and hand them to Listen so the /templates gallery and the
	// media/theme routes serve from the resolved root.
	lib, err := template.LoadDeckLibrary(deckDir, "../shared-lib")
	if err != nil {
		t.Fatalf("template.LoadDeckLibrary: %v", err)
	}
	if lib.RootPath != libDir {
		t.Fatalf("Library.RootPath = %q, want the resolved external %q", lib.RootPath, libDir)
	}
	themes, err := theme.LoadDir(os.DirFS(lib.RootPath), template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}

	srv := mustListen(t, Options{Root: deckDir, Library: lib, Themes: themes})

	rl, err := NewReloader(ReloadOptions{Server: srv, Root: deckDir, Title: "External", Log: io.Discard})
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}
	if err := rl.Start(); err != nil {
		t.Fatalf("Reloader.Start: %v", err)
	}
	t.Cleanup(rl.Stop)

	// The built-in pipeline resolves ../shared-lib and renders the deck from
	// it: the external library's hello template drives the page.
	body := getBody(t, srv, rootPath)
	if !strings.Contains(body, "<h1>Hi</h1>") {
		t.Fatalf("initial page does not render the external library's hello template: %q", body)
	}

	// The /templates gallery documents the resolved external library.
	gallery := getBody(t, srv, galleryPath)
	if !strings.Contains(gallery, "hello") || !strings.Contains(gallery, "external hello description") {
		t.Fatalf("gallery does not document the external library's hello template: %q", gallery)
	}

	// Media and theme files serve from the resolved external root.
	if got := getBody(t, srv, MediaPath+"logo.svg"); !strings.Contains(got, "<svg/>") {
		t.Fatalf("media logo.svg did not serve from the resolved root: %q", got)
	}
	if got := getBody(t, srv, ThemesPath+"default/theme.css"); !strings.Contains(got, "margin: 0") {
		t.Fatalf("theme default/theme.css did not serve from the resolved root: %q", got)
	}

	// A library edit is picked up on the very next reload, with no restart.
	writeFile(t, filepath.Join(libDir, "slides", "hello", "layout.html.tmpl"),
		"<section><h1 class=\"edited\">{{.title}}</h1></section>\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, `<h1 class="edited">Hi</h1>`) {
		t.Fatalf("reload did not pick up the edited external library layout: %q", body)
	}

	// Retargeting `templates:` is re-resolved from kalide.yaml on the next
	// reload: the page now comes from the other external library.
	writeFile(t, filepath.Join(deckDir, "kalide.yaml"),
		"title: External Deck\ntheme: default\ntemplates: ../other-lib\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, `<h1 class="other">Hi</h1>`) {
		t.Fatalf("reload did not re-resolve the retargeted templates: path: %q", body)
	}

	// An invalid resolved library publishes the positioned error page naming
	// the resolved path, and the server keeps running (still serving "/").
	writeFile(t, filepath.Join(otherLibDir, "library.yaml"), "name: Bad_Name\nformat: 1\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, "Deck error") {
		t.Fatalf("invalid external library did not publish the error page: %q", body)
	}
	if want := filepath.Join(otherLibDir, "library.yaml"); !strings.Contains(body, want) {
		t.Fatalf("error page does not name the resolved path %q: %q", want, body)
	}

	// Restoring the library recovers the deck without a restart.
	writeFile(t, filepath.Join(otherLibDir, "library.yaml"),
		"name: other-lib\ndescription: other hello description\nformat: 1\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") {
		t.Fatalf("restored external library still serves the error page: %q", body)
	}
	if !strings.Contains(body, `<h1 class="other">Hi</h1>`) {
		t.Fatalf("restored external library did not restore the deck page: %q", body)
	}
}

// writeExternalLibrary writes a minimal, valid external template library to
// dir: library.yaml, one slide template (hello) with the given manifest
// description and layout, themes/default/theme.css and media/logo.svg — enough
// for the loader, the validator, the renderer, the gallery and the media/theme
// routes to use it.
func writeExternalLibrary(t *testing.T, dir, name, description, layout string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "library.yaml"),
		"name: "+name+"\ndescription: "+description+"\nformat: 1\n")
	writeFile(t, filepath.Join(dir, "slides", "hello", "template.yaml"),
		"description: "+description+"\n"+
			"fields:\n"+
			"  - name: title\n"+
			"    type: text\n"+
			"    required: true\n"+
			"body:\n"+
			"  mode: optional\n")
	writeFile(t, filepath.Join(dir, "slides", "hello", "layout.html.tmpl"), layout)
	writeFile(t, filepath.Join(dir, "slides", "hello", "example.md"),
		"---\ntemplate: hello\ntitle: Example\n---\n")
	writeFile(t, filepath.Join(dir, "themes", "default", "theme.css"), "body { margin: 0; }\n")
	writeFile(t, filepath.Join(dir, "media", "logo.svg"), "<svg/>\n")
}
