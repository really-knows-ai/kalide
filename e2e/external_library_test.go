package e2e

// This file is the phase-05 task-1 end-to-end test for the external template
// library flow (requirements.requirement.external-template-library,
// requirements.requirement.cli-init). It drives the real kalide binary the
// harness builds (harness.go) through the whole path an author takes to point a
// new deck at an existing library sitting next to it:
//
//   - `kalide init-library ../shared-lib` scaffolds the deck-ready empty
//     library at the deck's sibling (library.yaml plus the empty slides/,
//     sections/ and media/ and themes/default/theme.css);
//   - `kalide init ../shared-lib` writes a deck that references that external
//     root (`templates: ../shared-lib`) with an empty slides/ and assets/ and
//     no local templates/;
//   - `kalide start --no-open` serves the deck from the external root: the
//     served page is the deck's own page (never the error page) and the
//     default theme it links is read from ../shared-lib/themes/default/, which
//     is the only themes/default/theme.css on disk since there is no local
//     templates/;
//   - editing a file under the external root (../shared-lib) live-reloads: the
//     SSE stream carries `data: reload` and the served theme then carries the
//     edited content, while the deck stays served;
//   - retargeting the raw `templates:` value to a different external library
//     (../other-lib) is refused live: the next reload serves the standard
//     full-page restart-required error while the process keeps running and the
//     port stays bound;
//   - reverting the raw `templates:` value to ../shared-lib resumes the deck;
//   - with the config pointing at ../other-lib, stopping and restarting the
//     binary (the same Harness, reused across a stop/start cycle) serves the
//     deck from the new root — the theme route serves ../other-lib's marked
//     stylesheet, never the startup library's edited one;
//   - the run is offline (EnableOfflineProxy plus the assertOffline connection
//     sampler, one sampler per run) and each run stops gracefully (h.Stop's
//     exit-0 / port-released / no-leftover-children post-conditions).
//
// It builds a binary and drives a real server, so it is skipped under -short.
//
// The SSE helpers (openSSE, waitLine, drainFor, waitFor) are declared alongside
// the phase-7 live-reload test in livereload_test.go and reused here unchanged.

import (
	"fmt"
	"html"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/really-knows-ai/kalide/internal/server"
)

// otherLibraryThemeMarker is written into the retarget library's
// themes/default/theme.css, so the restarted server's theme route proves which
// root it serves from: the startup library's stylesheet never carries it.
const otherLibraryThemeMarker = "other-library-theme"

// TestExternalLibrary is the end-to-end external-library test described above.
func TestExternalLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

	// `kalide init-library ../shared-lib` scaffolds the deck-ready empty
	// library at the deck's sibling. The harness working directory is a fresh
	// temporary directory, so its parent holds the sibling.
	stdout, stderr, code := h.Run("init-library", "../shared-lib")
	if code != 0 {
		t.Fatalf("kalide init-library ../shared-lib exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide init-library stderr = %q, want empty", stderr)
	}
	for _, want := range []string{"../shared-lib", "library.yaml", "slides/", "sections/", "media/", "themes/default/theme.css"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("kalide init-library output does not name %q:\n%s", want, stdout)
		}
	}

	libDir := h.Path("../shared-lib")
	if _, err := os.Stat(filepath.Join(libDir, "themes", "default", "theme.css")); err != nil {
		t.Fatalf("init-library did not create the default theme in %s: %v", libDir, err)
	}

	// A second, different library sits next to the deck: the retarget target.
	// It is scaffolded the same way, then its default theme is given a marker so
	// the served theme route can pin which root is being served.
	stdout, stderr, code = h.Run("init-library", "../other-lib")
	if code != 0 {
		t.Fatalf("kalide init-library ../other-lib exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide init-library ../other-lib stderr = %q, want empty", stderr)
	}
	otherLibDir := h.Path("../other-lib")
	if _, err := os.Stat(filepath.Join(otherLibDir, "library.yaml")); err != nil {
		t.Fatalf("init-library did not create the retarget library in %s: %v", otherLibDir, err)
	}
	h.WriteFile("../other-lib/themes/default/theme.css",
		[]byte("/* "+otherLibraryThemeMarker+" */\nbody { margin: 0; }\n"))

	// `kalide init ../shared-lib` writes a deck referencing the external root:
	// kalide.yaml carries `templates: ../shared-lib`, slides/ and assets/ are
	// empty, and there is no local templates/ directory.
	stdout, stderr, code = h.Run("init", "../shared-lib")
	if code != 0 {
		t.Fatalf("kalide init ../shared-lib exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide init ../shared-lib stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "../shared-lib") {
		t.Errorf("kalide init output does not name the external library:\n%s", stdout)
	}
	cfg, err := os.ReadFile(h.Path("kalide.yaml"))
	if err != nil {
		t.Fatalf("read kalide.yaml: %v", err)
	}
	if !strings.Contains(string(cfg), "templates: ../shared-lib") {
		t.Errorf("kalide.yaml does not reference the external library:\n%s", cfg)
	}
	if _, err := os.Stat(h.Path("templates")); !os.IsNotExist(err) {
		t.Errorf("kalide init wrote a local templates/ (err = %v), want only the external reference", err)
	}
	assertDirEmpty(t, h.Path("slides"))
	assertDirEmpty(t, h.Path("assets"))

	// Serve offline: the proxy environment is set before Start and the
	// connection sampler is armed once the pid is known.
	h.EnableOfflineProxy()
	h.Start()
	off := assertOffline(t, h.PID())
	t.Cleanup(off.Stop)

	if got := h.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") || !strings.HasSuffix(got, "/") {
		t.Fatalf("start printed URL %q, want http://127.0.0.1:<port>/", got)
	}

	// The deck is served (never the error page) and its page links the default
	// theme under the shared themes route.
	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if strings.Contains(body, "Deck error") {
		t.Fatalf("served page is the error page, want the external-library deck:\n%s", body)
	}
	if !strings.Contains(body, "My presentation") {
		t.Errorf("served deck page does not carry the deck title:\n%s", body)
	}
	if !strings.Contains(body, server.ThemesPath+"default/theme.css") {
		t.Errorf("served deck page does not link the default theme under %s:\n%s", server.ThemesPath, body)
	}

	// The theme route serves the external root's default theme: the only
	// themes/default/theme.css on disk is the one init-library wrote to
	// ../shared-lib (the deck has no local templates/), so a byte-for-byte
	// match pins the served root.
	onDiskTheme, err := os.ReadFile(filepath.Join(libDir, "themes", "default", "theme.css"))
	if err != nil {
		t.Fatalf("read external theme.css: %v", err)
	}
	servedTheme, err := h.GetString(server.ThemesPath + "default/theme.css")
	if err != nil {
		t.Fatalf("GET %sdefault/theme.css: %v", server.ThemesPath, err)
	}
	if servedTheme != string(onDiskTheme) {
		t.Errorf("served %sdefault/theme.css does not match the external root's theme.css on disk", server.ThemesPath)
	}

	// Editing a file under the external root live-reloads. Edits are repeated
	// until the broadcast is observed, covering the small window between the
	// stream's initial `: connected` flush and its subscribe; every edit is a
	// valid library.
	s := openSSE(t, h)
	defer s.Close()
	waitLine(t, s, ": connected", 5*time.Second)

	var marker string
	deadline := time.Now().Add(10 * time.Second)
	for i := 1; ; i++ {
		marker = fmt.Sprintf("external-edit-%d", i)
		h.WriteFile("../shared-lib/themes/default/theme.css",
			[]byte("/* "+marker+" */\nbody { margin: 0; }\n"))
		if drainFor(s, "data: reload", 500*time.Millisecond) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no SSE reload after editing a file under the external root\nstdout:\n%s\nstderr:\n%s",
				h.Stdout(), h.Stderr())
		}
	}
	// The reload re-resolves and re-renders from the external root, so the
	// edited stylesheet is what the theme route now serves...
	waitFor(t, 5*time.Second, "the edited external theme to be served", func() bool {
		b, err := h.GetString(server.ThemesPath + "default/theme.css")
		return err == nil && strings.Contains(b, marker)
	})
	// ...and the deck stays served (the external library still validates).
	waitFor(t, 5*time.Second, "the deck to remain served after the external edit", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, "My presentation") && !strings.Contains(b, "Deck error")
	})
	s.Close()

	// Retargeting the raw `templates:` value is refused live: the built-in
	// pipeline pins the raw startup value, so the next reload publishes the
	// standard full-page restart-required error instead of the other library's
	// deck. The process keeps running and the port stays bound (GetString fails
	// on any non-200), so this is an error page, not a crash.
	startPort := h.Port()
	startConfig, err := os.ReadFile(h.Path("kalide.yaml"))
	if err != nil {
		t.Fatalf("read kalide.yaml: %v", err)
	}
	retargetConfig := []byte(strings.Replace(string(startConfig), "templates: ../shared-lib", "templates: ../other-lib", 1))
	if string(retargetConfig) == string(startConfig) {
		t.Fatalf("kalide.yaml does not carry the startup templates value:\n%s", startConfig)
	}
	h.WriteFile("kalide.yaml", retargetConfig)

	waitFor(t, 10*time.Second, "the restart-required error page on the retarget", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, "Deck error") && strings.Contains(b, "error-page__message")
	})
	body, err = h.GetString("/")
	if err != nil {
		t.Fatalf("GET / (retarget error page): %v", err)
	}
	if !strings.Contains(body, "changed after startup") ||
		!strings.Contains(body, "restart kalide start to use the new template library") {
		t.Errorf("retarget error page does not carry the restart-required error:\n%s", body)
	}
	if got := h.Port(); got != startPort {
		t.Errorf("port changed across the retarget: %d -> %d, want the process still bound to %d",
			startPort, got, startPort)
	}
	// The retarget was not applied live: the theme route still serves the pinned
	// startup root's (edited) stylesheet, never the retarget library's marker.
	pinnedTheme, err := h.GetString(server.ThemesPath + "default/theme.css")
	if err != nil {
		t.Fatalf("GET %sdefault/theme.css (retarget error): %v", server.ThemesPath, err)
	}
	if !strings.Contains(pinnedTheme, marker) {
		t.Errorf("theme route left the pinned startup root during the retarget:\n%s", pinnedTheme)
	}
	if strings.Contains(pinnedTheme, otherLibraryThemeMarker) {
		t.Errorf("retarget library's theme was served live instead of requiring a restart:\n%s", pinnedTheme)
	}

	// Reverting the raw value to the startup library resumes the deck.
	h.WriteFile("kalide.yaml", startConfig)
	waitFor(t, 10*time.Second, "the deck to resume after reverting the templates value", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, "My presentation") && !strings.Contains(b, "Deck error")
	})

	// With the config pointing at the other library, stop and restart the
	// binary. The fresh process pins ../other-lib as its startup library, so the
	// deck is served from the new root: its theme route serves the retarget
	// library's marked stylesheet, never the startup library's edited one. Each
	// run gets its own offline sampler.
	h.WriteFile("kalide.yaml", retargetConfig)
	h.Stop()
	off.Stop()

	h.Start()
	off2 := assertOffline(t, h.PID())
	t.Cleanup(off2.Stop)
	if got := h.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") || !strings.HasSuffix(got, "/") {
		t.Fatalf("restart printed URL %q, want http://127.0.0.1:<port>/", got)
	}
	waitFor(t, 10*time.Second, "the restarted deck to be served from the retargeted root", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, "My presentation") && !strings.Contains(b, "Deck error")
	})
	restartedTheme, err := h.GetString(server.ThemesPath + "default/theme.css")
	if err != nil {
		t.Fatalf("GET %sdefault/theme.css after restart: %v", server.ThemesPath, err)
	}
	if !strings.Contains(restartedTheme, otherLibraryThemeMarker) {
		t.Errorf("restarted deck's theme does not come from the retargeted root (marker %q):\n%s",
			otherLibraryThemeMarker, restartedTheme)
	}
	if strings.Contains(restartedTheme, marker) {
		t.Errorf("restarted deck still serves the startup library's edited theme:\n%s", restartedTheme)
	}

	// The second run stops cleanly too: exit 0, port released, no child behind;
	// off2.Stop reports any outbound connection the restarted run made.
	h.Stop()
	off2.Stop()
}

// layeredExternalLibraryDeckConfig is the two-layer fixture deck: the deck-root
// templates/ directory is the local layer and `templates: ../shared-lib` names
// the external layer next to it, so the merged library is the external layer
// plus the local layer. `theme: default` resolves in the local layer.
const layeredExternalLibraryDeckConfig = `title: Layered External Library Deck
author: Ada Lovelace
theme: default
templates: ../shared-lib
`

// The two layers' metadata names, kept distinct so a failure reads clearly.
const (
	layeredExternalLocalLibName = "local-lib"
	layeredExternalLibName      = "shared-lib"
)

// layeredLocalSlideManifest is the local layer's slide manifest: it requires
// the `heading` field the fixture slide supplies and takes no body.
const layeredLocalSlideManifest = `description: a local slide that invokes an external section
fields:
  - name: heading
    type: text
    required: true
body:
  mode: disallowed
`

// layeredLocalSlideLayout is the local slide's layout. It renders its own
// `local-logo.svg` at the primary media base, includes the EXTERNAL layer's
// `external-include` layout by that layout's own root-qualified name with the
// standard `{{ template "<name>" . }}` composition call (example-layout-
// composition), and then composes the EXTERNAL section `external-notice`
// through the `section` helper: the section call resolves against the MERGED
// section namespace (section-helper, library-layer-references), so the external
// section executes with its own layer's media binding.
//
// externalInclude is the external include layout's root-qualified name exactly
// as the loader builds it: path.Join(layer root, "sections/external-include/
// layout.html.tmpl") (loadlibrary.go). The fixture resolves the running
// process's canonical external root before writing this layout, so the literal
// and the namespace key cannot drift.
func layeredLocalSlideLayout(externalInclude string) string {
	return `<section class="local-slide">
  <h1 class="local-heading">{{ .heading }}</h1>
  <img class="own" src="{{ media "local-logo.svg" }}">
  {{ template "` + externalInclude + `" . }}
  {{ section "external-notice" }}
</section>
`
}

// layeredExternalIncludeManifest is the external layer's include layout's
// manifest: no fields and no body, so the local slide's `{{ template "<name>"
// . }}` include passes only the slide context and the layout renders standalone.
// It validates as part of the external layer's full standalone load.
const layeredExternalIncludeManifest = `description: an external layout included across layers
fields: []
body:
  mode: disallowed
`

// layeredExternalIncludeLayout is the external layout the local slide includes
// with `{{ template "<name>" . }}`. It is deliberately FREE OF MEDIA
// REFERENCES: an included layout executes in the invoking (local) layer's
// namespace, so its `media` helper would resolve against the LOCAL layer's
// media, not its own external layer's. Keeping it media-free makes the include a
// pure cross-layer composition that cannot mis-bind a media route.
const layeredExternalIncludeLayout = `<aside class="external-include">External layout include</aside>` + "\n"

// layeredExternalNoticeManifest is the external layer's section manifest: no
// fields and no body, so the helper call passes nothing and the section renders
// on its own. It validates standalone as part of the external layer's full load.
const layeredExternalNoticeManifest = `description: an external section invoked across layers
fields: []
body:
  mode: disallowed
`

// layeredExternalNoticeLayout renders the external section with the given
// extra class, so an edit under the external root produces a distinctive,
// greppable change in the served page. Its `media` reference resolves in the
// external layer's own root and is served under the external media prefix.
func layeredExternalNoticeLayout(marker string) string {
	class := "external-notice"
	if marker != "" {
		class += " " + marker
	}
	return `<aside class="` + class + `"><img src="{{ media "ext-icon.svg" }}"></aside>` + "\n"
}

// layeredLocalDuplicateManifest and layeredLocalDuplicateLayout are a complete,
// valid local slide template whose directory name `external-notice` duplicates
// the EXTERNAL layer's section name. The merged template namespace is shared by
// slides and sections (library-layer-uniqueness), so this local slide collides
// with the external section the moment both layers are merged — never as a
// within-layer clash.
const (
	layeredLocalDuplicateManifest = `description: a local slide duplicating the external section name
fields: []
body:
  mode: disallowed
`

	layeredLocalDuplicateLayout = `<section class="duplicate-local-slide"></section>` + "\n"
)

// localLogoMarkup is the local layer's media file, written (and rewritten) so
// its own layer's primary media route can be pinned and a watched local file
// can force a reload without changing the deck.
const localLogoMarkup = "<svg class=\"local-logo\"/>\n"

// TestExternalLibraryLayeredOverLocal is the plan.phase-05 task-5 end-to-end
// test, extended by plan.phase-02 task-1, for a deck that draws from BOTH
// resolved library layers (layered-template-library, library-layer-uniqueness,
// library-layer-references, example-layout-composition, live-reload): an
// external `templates: ../shared-lib` library AND a deck-root templates/
// directory. It drives the real kalide binary the harness builds (harness.go),
// so it exercises the whole path an author takes:
//
//   - the merged library serves the deck: the local layer's `local-slide`
//     renders with its own media at the primary /assets/templates/media/ route,
//     includes the EXTERNAL layer's `external-include` layout by that layout's
//     own root-qualified name with the standard `{{ template "<name>" . }}`
//     composition call (example-layout-composition), and its
//     `{{ section "external-notice" }}` call resolves against the merged section
//     namespace and executes the EXTERNAL layer's section, whose own `media`
//     reference is served from the external layer at
//     /assets/templates/external/media/ (cross-layer section call,
//     library-layer-references);
//   - each layer's media file is served from its own root at its own route;
//   - an edit under the external layer root live-reloads: the watcher watches
//     the external root, the merged library is rebuilt and the cross-layer call
//     re-renders with the edit (live-reload);
//   - a local template whose name the external layer also carries is rejected:
//     the running server publishes the full-page error naming the duplicated
//     name `external-notice` and BOTH carrying paths — the external section's
//     manifest and the local slide's (library-layer-uniqueness) — and the deck
//     recovers once the duplicate is removed.
//
// It builds a binary and drives a real server with the real watcher and SSE
// stream, so it is skipped under -short. It reuses the harness and the SSE
// helpers (openSSE, waitLine, drainFor, waitFor) declared alongside the phase-7
// live-reload test in livereload_test.go, and the server package's primary and
// external media route constants.
func TestExternalLibraryLayeredOverLocal(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	writeLayeredExternalLibraryFixture(t, h)

	h.Start()

	if got := h.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") || !strings.HasSuffix(got, "/") {
		t.Fatalf("start printed URL %q, want http://127.0.0.1:<port>/", got)
	}

	// The merged library drives the deck page: the local slide renders, its
	// `{{ template "<external root>/sections/external-include/layout.html.tmpl"
	// . }}` include splices in the EXTERNAL layer's layout, and its
	// `{{ section "external-notice" }}` call splices in the EXTERNAL layer's
	// section, whose media resolves under the external prefix.
	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if strings.Contains(body, "Deck error") {
		t.Fatalf("served page is the error page, want the two-layer deck:\n%s", body)
	}
	assertLayeredExternalPage(t, body)

	// Each layer's media is served from its own root at its own route: the
	// local layer's file at the primary MediaPath and the external layer's at
	// ExternalMediaPath. The external section's rendered URL is therefore
	// fetchable exactly where render emitted it.
	if got, err := h.GetString(server.MediaPath + "local-logo.svg"); err != nil {
		t.Errorf("GET %slocal-logo.svg: %v", server.MediaPath, err)
	} else if !strings.Contains(got, `class="local-logo"`) {
		t.Errorf("primary media route does not serve the local layer's file: %q", got)
	}
	if got, err := h.GetString(server.ExternalMediaPath + "ext-icon.svg"); err != nil {
		t.Errorf("GET %sext-icon.svg: %v", server.ExternalMediaPath, err)
	} else if !strings.Contains(got, `class="ext-icon"`) {
		t.Errorf("external media route does not serve the external layer's file: %q", got)
	}

	// An edit under the EXTERNAL layer root live-reloads. Edits are repeated
	// until the broadcast is observed, covering the small window between the
	// stream's initial `: connected` flush and its subscribe; every edit is a
	// valid external section.
	s := openSSE(t, h)
	waitLine(t, s, ": connected", 5*time.Second)

	var marker string
	deadline := time.Now().Add(10 * time.Second)
	for i := 1; ; i++ {
		marker = fmt.Sprintf("external-section-edit-%d", i)
		h.WriteFile("../shared-lib/sections/external-notice/layout.html.tmpl",
			[]byte(layeredExternalNoticeLayout(marker)))
		if drainFor(s, "data: reload", 500*time.Millisecond) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no SSE reload after editing the external layer's section\nstdout:\n%s\nstderr:\n%s",
				h.Stdout(), h.Stderr())
		}
	}
	// The merged library is re-read and re-rendered before the reload is
	// broadcast, so the cross-layer call now splices in the edited external
	// section while the local slide's own media stays served.
	waitFor(t, 5*time.Second, "the edited external section to be served", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, marker) &&
			strings.Contains(b, server.MediaPath+"local-logo.svg")
	})
	s.Close()

	// Duplicate-name rejection: a local template whose name the external layer
	// also carries is refused by the merged loader, and the running server
	// publishes the full-page error rather than serving a broken or stale deck
	// (never-serve-broken-deck, library-layer-uniqueness). A complete, valid
	// template is written so the collision — not a missing file — is what the
	// loader reports, then a watched local file is rewritten so a reload is
	// forced after the whole duplicate exists.
	h.WriteFile("templates/slides/external-notice/template.yaml", []byte(layeredLocalDuplicateManifest))
	h.WriteFile("templates/slides/external-notice/layout.html.tmpl", []byte(layeredLocalDuplicateLayout))
	h.WriteFile("templates/slides/external-notice/example.md", []byte("# external-notice\n"))
	h.WriteFile("templates/media/local-logo.svg", []byte(localLogoMarkup))

	// The loader builds each carrying path with path.Join from the layer root
	// the running process resolves (getcwd canonicalises macOS /var ->
	// /private/var), so resolve the roots the same way before comparing.
	externalManifest := layeredLayerManifestPath(t, h, "../shared-lib", "sections", "external-notice")
	localManifest := layeredLayerManifestPath(t, h, "templates", "slides", "external-notice")

	waitFor(t, 10*time.Second, "the duplicate-name error page", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(html.UnescapeString(b),
			`template "external-notice" is declared in both`)
	})
	errBody, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET / (duplicate-name error page): %v", err)
	}
	if !strings.Contains(errBody, "Deck error") || !strings.Contains(errBody, "error-page__message") {
		t.Errorf("served page is not the full-page error shell after the duplicate:\n%s", errBody)
	}
	unescaped := html.UnescapeString(errBody)
	for _, want := range []string{
		`template "external-notice" is declared in both`,
		externalManifest,
		localManifest,
	} {
		if !strings.Contains(unescaped, want) {
			t.Errorf("duplicate-name error does not name %q:\n%s", want, unescaped)
		}
	}

	// Removing the duplicate recovers the deck: the reload re-merges both
	// layers and the external section's edit is still served.
	if err := os.RemoveAll(h.Path("templates/slides/external-notice")); err != nil {
		t.Fatalf("remove duplicate local template: %v", err)
	}
	h.WriteFile("templates/media/local-logo.svg", []byte(localLogoMarkup))
	waitFor(t, 10*time.Second, "the deck to recover after removing the duplicate", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, marker) &&
			strings.Contains(b, `class="external-notice `) && !strings.Contains(b, "Deck error")
	})

	// A graceful stop exits 0, releases the port and leaves no child behind;
	// the harness asserts the exit status, the released port and the absence of
	// leftover children in Stop.
	port := h.Port()
	if port == 0 {
		t.Fatal("start did not bind a port")
	}
	h.Stop()
	if _, err := h.Get("/"); err == nil {
		t.Errorf("GET / succeeded after Stop; port %d was not released", port)
	}
}

// writeLayeredExternalLibraryFixture lays down the two-layer deck the test
// serves: kalide.yaml, one slide for the local layer's template, the deck-root
// templates/ local layer (a `local-slide` whose layout includes the external
// `external-include` layout by its root-qualified name and calls the external
// section through the `section` helper, its own media and theme), and the
// external `../shared-lib` layer (the `external-include` and `external-notice`
// sections, its own media and metadata). No name — slide, section, theme or
// media path — is carried by both layers, so the merged library passes
// library-layer-uniqueness.
//
// The local `local-slide` layout must name the external include layout by the
// exact root-qualified name the running process resolves, so the external layer
// is written first, then its canonical root-qualified `external-include` layout
// name is read back and spliced into the local layout before that is written.
func writeLayeredExternalLibraryFixture(t *testing.T, h *Harness) {
	t.Helper()

	h.WriteFile("kalide.yaml", []byte(layeredExternalLibraryDeckConfig))
	h.WriteFile("slides/1-notice.md",
		[]byte("---\ntemplate: local-slide\nheading: Layered Notice\n---\n"))

	// The external `templates: ../shared-lib` layer, written first so its
	// canonical root exists when the local layout names the include layout.
	h.WriteFile("../shared-lib/library.yaml",
		[]byte("name: "+layeredExternalLibName+"\ndescription: external deck layer\nformat: 1\n"))
	h.WriteFile("../shared-lib/sections/external-include/template.yaml", []byte(layeredExternalIncludeManifest))
	h.WriteFile("../shared-lib/sections/external-include/layout.html.tmpl",
		[]byte(layeredExternalIncludeLayout))
	h.WriteFile("../shared-lib/sections/external-include/example.md", []byte("# external-include\n"))
	h.WriteFile("../shared-lib/sections/external-notice/template.yaml", []byte(layeredExternalNoticeManifest))
	h.WriteFile("../shared-lib/sections/external-notice/layout.html.tmpl",
		[]byte(layeredExternalNoticeLayout("")))
	h.WriteFile("../shared-lib/sections/external-notice/example.md", []byte("# external-notice\n"))
	h.WriteFile("../shared-lib/media/ext-icon.svg", []byte("<svg class=\"ext-icon\"/>\n"))

	// The local slide layout includes the external layout by the loader-built
	// root-qualified name (path.Join(external root, sections/<name>/
	// layout.html.tmpl)), resolved against the canonical root the running
	// process's os.Getwd reports.
	externalInclude := layeredLayerLayoutPath(t, h, "../shared-lib", "sections", "external-include")

	// The deck-root templates/ local layer.
	h.WriteFile("templates/library.yaml",
		[]byte("name: "+layeredExternalLocalLibName+"\ndescription: deck-root local layer\nformat: 1\n"))
	h.WriteFile("templates/themes/default/theme.css",
		[]byte("/* local-layer-theme */\nbody { margin: 0; }\n"))
	h.WriteFile("templates/media/local-logo.svg", []byte(localLogoMarkup))
	h.WriteFile("templates/slides/local-slide/template.yaml", []byte(layeredLocalSlideManifest))
	h.WriteFile("templates/slides/local-slide/layout.html.tmpl", []byte(layeredLocalSlideLayout(externalInclude)))
	h.WriteFile("templates/slides/local-slide/example.md",
		[]byte("---\ntemplate: local-slide\nheading: Example\n---\n"))
}

// assertLayeredExternalPage asserts the served two-layer page carries the
// local slide, its own primary-base media, the external layout the local layout
// included cross-layer by its root-qualified name, and the external section the
// local layout invoked cross-layer, with that section's media under the
// external prefix.
func assertLayeredExternalPage(t *testing.T, body string) {
	t.Helper()

	for _, want := range []string{
		"Layered External Library Deck",
		`<h1 class="local-heading">Layered Notice</h1>`,
		`<img class="own" src="` + server.MediaPath + `local-logo.svg">`,
		`<aside class="external-include">External layout include</aside>`,
		`<aside class="external-notice"><img src="` + server.ExternalMediaPath + `ext-icon.svg"></aside>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("served layered deck page is missing %q:\n%s", want, body)
		}
	}
}

// layeredLayerManifestPath returns the manifest path the merged loader reports
// for a template carried by the layer at layerRel (relative to the harness
// working directory): the layer root resolved to the canonical path the running
// binary's os.Getwd reports, joined with the template's kind directory and
// name. The loader builds that path with path.Join, so it always uses forward
// slashes even where the root itself is a Windows path.
func layeredLayerManifestPath(t *testing.T, h *Harness, layerRel, kindDir, name string) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(h.Path(layerRel))
	if err != nil {
		t.Fatalf("resolve library layer root %s: %v", layerRel, err)
	}
	return path.Join(root, kindDir, name, "template.yaml")
}

// layeredLayerLayoutPath returns the root-qualified layout name the loader
// builds for a template carried by the layer at layerRel (relative to the
// harness working directory): the layer root resolved to the canonical path the
// running binary's os.Getwd reports, joined with the template's kind directory,
// name and layout file. It is the name a `{{ template "<name>" . }}`
// composition call must use (loadlibrary.go: path.Join(root, layoutRel)), and
// it is built with path.Join so it always uses forward slashes even where the
// root itself is a Windows path.
func layeredLayerLayoutPath(t *testing.T, h *Harness, layerRel, kindDir, name string) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(h.Path(layerRel))
	if err != nil {
		t.Fatalf("resolve library layer root %s: %v", layerRel, err)
	}
	return path.Join(root, kindDir, name, "layout.html.tmpl")
}
