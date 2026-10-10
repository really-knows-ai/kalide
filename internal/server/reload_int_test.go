package server

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
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

// TestReloaderLayeredLibraryIntegration is the int-tier proof
// (plan.phase-04.task-17) that the built-in Pipeline (renderDefault) rebuilds
// the deck's MERGED two-layer library — the external `templates:` layer and the
// deck-root templates/ local layer — atomically on every Reload, and that the
// static surfaces Listen pinned at startup (the /templates gallery and the
// per-layer media/theme routes) keep serving the startup library (the last good
// deck) when only the served deck page goes to an error. It proves:
//
//   - atomic reload: an edit under EITHER resolved layer is picked up on the
//     very next reload, with no restart, and a name carried by both layers —
//     introduced live — rejects the WHOLE merged library and publishes the
//     full-page error naming both layer paths, never a partially-merged deck; a
//     fix recovers.
//   - last-good-deck on error: when a reload fails, only the served deck page
//     shows the error; the pinned gallery and per-layer media/theme routes keep
//     serving the startup library (server.go mounts them once at Listen and
//     never reloads them).
//   - pinned startup surfaces: a mid-session `templates:` retarget is refused
//     with the standard restart-required error on the deck page while the
//     gallery and media/theme routes keep serving the startup library
//     (template-retarget-requires-restart).
//
// It writes a real two-layer deck and real libraries to temporary directories
// and drives NewReloader/(Reloader).Reload directly (no watcher, no SSE
// stream), so it exercises the per-call load path in loadLibrary/renderDefault
// against the real filesystem, and is skipped under -short. It reuses the
// helpers declared in reload_int_test.go (writeFile, readFile,
// writeExternalLibrary) and server_test.go (mustListen, getBody, rootPath,
// galleryPath), plus the retarget fixtures' raw `templates:` values.
func TestReloaderLayeredLibraryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real two-layer deck and libraries from disk")
	}

	parent := t.TempDir()
	deckDir := filepath.Join(parent, "deck")
	sharedDir := filepath.Join(parent, "shared-lib")
	otherDir := filepath.Join(parent, "other-lib")
	localDir := filepath.Join(deckDir, template.TemplatesDir)

	writeLayeredDeckFixture(t, deckDir, sharedDir, otherDir)

	// Load and pin the startup MERGED library (external + local) and the theme
	// registry merged from every layer, the way `kalide start` does, and hand
	// them to Listen so the per-layer media/theme routes and the /templates
	// gallery are pinned to the startup library for the whole session.
	lib, err := template.LoadDeckLibrary(deckDir, retargetStartup)
	if err != nil {
		t.Fatalf("template.LoadDeckLibrary: %v", err)
	}
	if len(lib.Layers) != 2 {
		t.Fatalf("startup library has %d layers, want 2 (external + local)", len(lib.Layers))
	}
	if lib.Layers[0].Role != template.LayerRoleExternal || lib.Layers[1].Role != template.LayerRoleLocal {
		t.Fatalf("startup layer roles = [%v, %v], want [external, local]", lib.Layers[0].Role, lib.Layers[1].Role)
	}
	themes, err := loadLibraryThemeRegistry(lib)
	if err != nil {
		t.Fatalf("loadLibraryThemeRegistry: %v", err)
	}

	srv := mustListen(t, Options{Root: deckDir, Library: lib, Themes: themes})
	rl, err := NewReloader(ReloadOptions{Server: srv, Root: deckDir, Title: "Layered", Log: io.Discard})
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}
	if err := rl.Start(); err != nil {
		t.Fatalf("Reloader.Start: %v", err)
	}
	t.Cleanup(rl.Stop)

	// The merged library drives the deck page: one slide from the external
	// layer and one from the local layer both render.
	body := getBody(t, srv, rootPath)
	if !strings.Contains(body, `<h1 class="external">Hi</h1>`) {
		t.Fatalf("initial page does not render the external layer's hello template: %q", body)
	}
	if !strings.Contains(body, `<h1 class="local">Lo</h1>`) {
		t.Fatalf("initial page does not render the local layer's localonly template: %q", body)
	}

	// The startup /templates gallery documents templates from BOTH layers.
	gallery := getBody(t, srv, galleryPath)
	for _, want := range []string{"hello", "shared external description", "localonly", "local only description"} {
		if !strings.Contains(gallery, want) {
			t.Fatalf("startup gallery does not document %q from the merged library: %q", want, gallery)
		}
	}

	// The pinned per-layer media/theme routes serve each layer from its own
	// startup root, including the unprefixed fallback for an external-only
	// theme (task-24).
	assertLayeredPinnedSurfaces(t, srv)

	// An edit under the EXTERNAL layer is picked up on the next reload.
	writeFile(t, filepath.Join(sharedDir, "slides", "hello", "layout.html.tmpl"),
		`<section><h1 class="external-edited">{{.title}}</h1></section>`+"\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, `<h1 class="external-edited">Hi</h1>`) {
		t.Fatalf("reload did not pick up the edited external layer layout: %q", body)
	}
	if !strings.Contains(body, `<h1 class="local">Lo</h1>`) {
		t.Fatalf("reloading the external layer lost the local layer's slide: %q", body)
	}

	// An edit under the LOCAL layer is likewise picked up on the next reload,
	// and the external layer's edit is retained: the rebuild re-reads every
	// resolved layer.
	writeFile(t, filepath.Join(localDir, "slides", "localonly", "layout.html.tmpl"),
		`<section><h1 class="local-edited">{{.title}}</h1></section>`+"\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, `<h1 class="local-edited">Lo</h1>`) {
		t.Fatalf("reload did not pick up the edited local layer layout: %q", body)
	}
	if !strings.Contains(body, `<h1 class="external-edited">Hi</h1>`) {
		t.Fatalf("reloading the local layer lost the external layer's edit: %q", body)
	}

	// Atomic across layers: a name carried by BOTH layers rejects the whole
	// merged library, path-qualified to each layer. Neither layer's deck is
	// published — the page is the full-page error, never a partially-merged
	// deck.
	localDuplicate := filepath.Join(localDir, "slides", "hello")
	writeLocalDuplicateHello(t, localDir)
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, "Deck error") {
		t.Fatalf("a cross-layer duplicate name did not publish the error page: %q", body)
	}
	externalManifest := filepath.Join(sharedDir, "slides", "hello", "template.yaml")
	localManifest := filepath.Join(localDuplicate, "template.yaml")
	if !strings.Contains(body, externalManifest) {
		t.Fatalf("duplicate-name error page does not name the external path %q: %q", externalManifest, body)
	}
	if !strings.Contains(body, localManifest) {
		t.Fatalf("duplicate-name error page does not name the local path %q: %q", localManifest, body)
	}
	if strings.Contains(body, "external-edited") || strings.Contains(body, "local-edited") {
		t.Fatalf("a rejected merged library leaked a partially-merged deck page: %q", body)
	}

	// last-good-deck on error: the pinned startup surfaces still serve the
	// startup library while only the deck page shows the error.
	assertLayeredPinnedSurfaces(t, srv)
	if g := getBody(t, srv, galleryPath); !strings.Contains(g, "shared external description") {
		t.Fatalf("gallery lost the startup library during a reload error: %q", g)
	}

	// Removing the duplicate recovers the deck without a restart.
	if err := os.RemoveAll(localDuplicate); err != nil {
		t.Fatalf("remove duplicate local template: %v", err)
	}
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") {
		t.Fatalf("removing the cross-layer duplicate did not recover the deck: %q", body)
	}
	if !strings.Contains(body, `<h1 class="external-edited">Hi</h1>`) || !strings.Contains(body, `<h1 class="local-edited">Lo</h1>`) {
		t.Fatalf("recovered deck lost a layer's template: %q", body)
	}

	// Pinned startup surfaces: a mid-session `templates:` retarget is refused
	// with the standard restart-required error on the deck page while the
	// gallery and media/theme routes keep serving the STARTUP library
	// (template-retarget-requires-restart).
	writeFile(t, filepath.Join(deckDir, "kalide.yaml"),
		retargetDeckYAML("Layered Deck", retargetOther))
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, "Deck error") {
		t.Fatalf("mid-session retarget did not serve the error page: %q", body)
	}
	if want := restartRequiredError().Error(); !strings.Contains(body, want) {
		t.Fatalf("retarget error page does not carry the restart-required error %q: %q", want, body)
	}
	if strings.Contains(body, `class="other"`) {
		t.Fatalf("retargeted templates: path was applied live instead of requiring a restart: %q", body)
	}
	gallery = getBody(t, srv, galleryPath)
	if !strings.Contains(gallery, "shared external description") || !strings.Contains(gallery, "local only description") {
		t.Fatalf("gallery left the startup library after a retarget: %q", gallery)
	}
	if strings.Contains(gallery, "other external description") {
		t.Fatalf("gallery followed the retarget instead of staying pinned to the startup library: %q", gallery)
	}
	assertLayeredPinnedSurfaces(t, srv)

	// Reverting the raw `templates:` value to the startup baseline resumes the
	// deck with both layers' edits still applied.
	writeFile(t, filepath.Join(deckDir, "kalide.yaml"),
		retargetDeckYAML("Layered Deck", retargetStartup))
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") {
		t.Fatalf("reverting the templates: value still serves the error page: %q", body)
	}
	if !strings.Contains(body, `<h1 class="external-edited">Hi</h1>`) || !strings.Contains(body, `<h1 class="local-edited">Lo</h1>`) {
		t.Fatalf("reverted deck did not resume both layers' templates: %q", body)
	}
}

// writeLayeredDeckFixture writes a real two-layer deck to deckDir: kalide.yaml
// with `templates: ../shared-lib`, two deck slides (one using the external
// layer's hello template, one using the local layer's localonly template), the
// external library at sharedDir, a second valid external library at otherDir
// (the retarget target, whose content must never be served live), and the
// deck-root templates/ local layer under deckDir. No name is carried by both
// layers, so the merged library is valid.
func writeLayeredDeckFixture(t *testing.T, deckDir, sharedDir, otherDir string) {
	t.Helper()
	writeFile(t, filepath.Join(deckDir, "kalide.yaml"),
		retargetDeckYAML("Layered Deck", retargetStartup))
	writeFile(t, filepath.Join(deckDir, "slides", "1-hello.md"),
		"---\ntemplate: hello\ntitle: Hi\n---\n")
	writeFile(t, filepath.Join(deckDir, "slides", "2-local.md"),
		"---\ntemplate: localonly\ntitle: Lo\n---\n")
	writeExternalLibrary(t, sharedDir, "shared-lib", "shared external description",
		"<section><h1 class=\"external\">{{.title}}</h1></section>\n")
	writeExternalLibrary(t, otherDir, "other-lib", "other external description",
		"<section><h1 class=\"other\">{{.title}}</h1></section>\n")
	writeLayeredLocalLibrary(t, filepath.Join(deckDir, template.TemplatesDir))
}

// writeLayeredLocalLibrary writes the deck-root templates/ local layer to root:
// library.yaml, one slide template (localonly), one theme (localplain) and one
// media file (local.svg). Every name is distinct from the external layer's, so
// the two layers merge without a library-layer-uniqueness collision.
func writeLayeredLocalLibrary(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, template.LibraryFile),
		"name: local-lib\ndescription: local deck library\nformat: 1\n")
	writeFile(t, filepath.Join(root, template.SlidesDir, "localonly", template.ManifestFile),
		"description: local only description\n"+
			"fields:\n"+
			"  - name: title\n"+
			"    type: text\n"+
			"    required: true\n"+
			"body:\n"+
			"  mode: optional\n")
	writeFile(t, filepath.Join(root, template.SlidesDir, "localonly", template.LayoutFile),
		"<section><h1 class=\"local\">{{.title}}</h1></section>\n")
	writeFile(t, filepath.Join(root, template.SlidesDir, "localonly", template.ExampleFile),
		"---\ntemplate: localonly\ntitle: Example\n---\n")
	writeFile(t, filepath.Join(root, template.ThemesDir, "localplain", template.ThemeStylesheet),
		"body { color: #002; }\n")
	writeFile(t, filepath.Join(root, template.MediaDir, "local.svg"), "<svg>local</svg>\n")
}

// writeLocalDuplicateHello writes a local slide template named hello — the same
// name the external layer's hello template carries — under the deck-root
// templates/ local layer, so the next LoadDeckLibrary sees a name carried by
// both layers (library-layer-uniqueness).
func writeLocalDuplicateHello(t *testing.T, localRoot string) {
	t.Helper()
	dir := filepath.Join(localRoot, template.SlidesDir, "hello")
	writeFile(t, filepath.Join(dir, template.ManifestFile),
		"description: local duplicate hello\n"+
			"fields:\n"+
			"  - name: title\n"+
			"    type: text\n"+
			"body:\n"+
			"  mode: optional\n")
	writeFile(t, filepath.Join(dir, template.LayoutFile),
		"<section><h1 class=\"duplicate\">{{.title}}</h1></section>\n")
	writeFile(t, filepath.Join(dir, template.ExampleFile),
		"---\ntemplate: hello\ntitle: Example\n---\n")
}

// assertLayeredPinnedSurfaces asserts the static surfaces Listen mounted from
// the startup library keep serving it: the /templates gallery documents both
// startup layers' templates, and the four per-layer media/theme routes each
// serve a file from its own startup layer — the external prefix from the
// external `templates:` layer, the unprefixed prefix from the primary (local)
// layer, and the unprefixed theme route falling back to the external layer for
// an external-only theme name (task-24). It fails the test on a non-200.
func assertLayeredPinnedSurfaces(t *testing.T, srv *Server) {
	t.Helper()
	if g := getBody(t, srv, galleryPath); !strings.Contains(g, "hello") || !strings.Contains(g, "localonly") {
		t.Fatalf("gallery does not document the startup library's templates: %q", g)
	}
	if got := getBody(t, srv, ExternalMediaPath+"logo.svg"); !strings.Contains(got, "<svg/>") {
		t.Fatalf("external media route does not serve the startup external layer: %q", got)
	}
	if got := getBody(t, srv, MediaPath+"local.svg"); !strings.Contains(got, "<svg>local</svg>") {
		t.Fatalf("primary media route does not serve the startup local layer: %q", got)
	}
	if got := getBody(t, srv, ExternalThemesPath+"default/theme.css"); !strings.Contains(got, "margin: 0") {
		t.Fatalf("external theme route does not serve the startup external layer: %q", got)
	}
	if got := getBody(t, srv, ThemesPath+"default/theme.css"); !strings.Contains(got, "margin: 0") {
		t.Fatalf("unprefixed theme route did not fall back to the startup external layer: %q", got)
	}
	if got := getBody(t, srv, ThemesPath+"localplain/theme.css"); !strings.Contains(got, "color: #002") {
		t.Fatalf("unprefixed theme route does not serve the startup local layer: %q", got)
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
