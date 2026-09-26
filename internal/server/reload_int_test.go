package server

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReloaderTemplatesLibraryIntegration is the int-tier proof that the
// built-in Pipeline (renderDefault) reloads the project's templates/ library
// fresh from disk on every Reload rather than caching it at construction
// (commit b7f1a23): editing a slide template's manifest/layout or the
// top-level library.yaml is picked up on the next reload without restarting
// the server; a broken manifest or theme publishes the full-page error
// (never-serve-broken-deck), and fixing it restores the deck.
//
// It writes a real templates/ fixture to a real temporary directory and
// drives NewReloader/(Reloader).Reload directly (no watcher, no SSE stream),
// so it exercises exactly the per-call load path in loadLibrary/renderDefault
// against the real filesystem.
func TestReloaderTemplatesLibraryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real templates/ library from disk")
	}

	dir := t.TempDir()
	writeFixtureDeck(t, dir)

	srv := mustListen(t, Options{Page: testPage("PLACEHOLDER")})
	rl, err := NewReloader(ReloadOptions{Server: srv, Root: dir, Title: "Fallback", Log: io.Discard})
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}
	if err := rl.Start(); err != nil {
		t.Fatalf("Reloader.Start: %v", err)
	}
	t.Cleanup(rl.Stop)

	body := getBody(t, srv, rootPath)
	if !strings.Contains(body, "<h1>Hi</h1>") {
		t.Fatalf("initial page does not render the fixture hello template: %q", body)
	}

	// Editing the slide template's layout must be picked up on the very next
	// reload, with no restart.
	layoutPath := filepath.Join(dir, "templates", "slides", "hello", "layout.html.tmpl")
	writeFile(t, layoutPath, `<section><h1 class="edited">{{.title}}</h1></section>`+"\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, `<h1 class="edited">Hi</h1>`) {
		t.Fatalf("reload did not pick up the edited layout: %q", body)
	}

	// Editing library.yaml (metadata only) must not break the reload.
	libYAML := filepath.Join(dir, "templates", "library.yaml")
	writeFile(t, libYAML, "name: edited-library\ndescription: edited\nformat: 1\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") {
		t.Fatalf("editing library.yaml metadata broke the reload: %q", body)
	}
	if !strings.Contains(body, `<h1 class="edited">Hi</h1>`) {
		t.Fatalf("reload after library.yaml edit lost the deck page: %q", body)
	}

	// A broken slide manifest must publish the error page, never a stale or
	// partially-loaded deck.
	manifestPath := filepath.Join(dir, "templates", "slides", "hello", "template.yaml")
	original := readFile(t, manifestPath)
	writeFile(t, manifestPath, "fields: [not: valid: yaml\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, "Deck error") {
		t.Fatalf("broken template manifest did not serve the error page: %q", body)
	}

	// Fixing the manifest restores the deck.
	writeFile(t, manifestPath, original)
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") {
		t.Fatalf("fixed manifest still serves the error page: %q", body)
	}
	if !strings.Contains(body, `<h1 class="edited">Hi</h1>`) {
		t.Fatalf("fixed manifest did not restore the deck page: %q", body)
	}

	// A broken theme (invalid CSS location aside — break by removing the
	// referenced theme's directory entirely) must also serve the error page,
	// and restoring it recovers.
	themeDir := filepath.Join(dir, "templates", "themes", "plain")
	themeCSS := filepath.Join(themeDir, "theme.css")
	originalTheme := readFile(t, themeCSS)
	if err := os.RemoveAll(themeDir); err != nil {
		t.Fatalf("remove theme dir: %v", err)
	}
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, "Deck error") {
		t.Fatalf("missing theme did not serve the error page: %q", body)
	}

	writeFile(t, themeCSS, originalTheme)
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") {
		t.Fatalf("restored theme still serves the error page: %q", body)
	}
	if !strings.Contains(body, `<h1 class="edited">Hi</h1>`) {
		t.Fatalf("restored theme did not restore the deck page: %q", body)
	}
}

// writeFixtureDeck writes a minimal real deck to dir: kalide.yaml, one slide
// using the hello template, and a templates/ library (slides/hello,
// themes/plain, media/logo.svg, library.yaml) — enough for the built-in
// pipeline to validate and render without any test override.
func writeFixtureDeck(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"kalide.yaml":       "title: Fixture Deck\ntheme: plain\n",
		"slides/1-hello.md": "---\ntemplate: hello\ntitle: Hi\n---\n",

		"templates/library.yaml": "name: fixture-library\ndescription: reload integration fixture\nformat: 1\n",

		"templates/slides/hello/template.yaml": "description: a minimal hello slide with a title\n" +
			"fields:\n" +
			"  - name: title\n" +
			"    type: text\n" +
			"    required: true\n" +
			"body:\n" +
			"  mode: optional\n",
		"templates/slides/hello/layout.html.tmpl": "<section><h1>{{.title}}</h1></section>\n",
		"templates/slides/hello/example.md":       "---\ntemplate: hello\ntitle: Example\n---\n",

		"templates/themes/plain/theme.css": "body { margin: 0; }\n",

		"templates/media/logo.svg": "<svg/>\n",
	}
	for name, data := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), data)
	}
}

// writeFile writes data to path, creating parent directories as needed and
// failing the test on error.
func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readFile reads path, failing the test on error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
