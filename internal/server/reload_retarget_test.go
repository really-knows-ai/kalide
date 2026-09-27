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

// This file is the plan.phase-02.task-1 deliverable for
// template-retarget-requires-restart: the package plus the shared fixtures the
// retarget unit and integration tests build on. The unit test
// (TestTemplatesRetargeted) drives the pure raw-value predicate
// templatesRetargeted with the same two raw `templates:` values the fixture
// writes into kalide.yaml; the integration test
// (TestReloaderRetargetRequiresRestartIntegration) points the built-in pipeline
// (renderDefault) at the fixture's real deck and libraries and watches a
// mid-session retarget become the full-page restart-required error while the
// process keeps serving.

const (
	// retargetStartup and retargetOther are the two raw top-level `templates:`
	// values the fixture uses: retargetStartup is the session-startup library
	// pinned by NewReloader, and retargetOther is the mid-session retarget
	// target. They are deliberately different spellings of different paths, so
	// the predicate's raw-value comparison sees a change.
	retargetStartup = "../shared-lib"
	retargetOther   = "../other-lib"
)

// retargetFixture is a real deck plus two real external template libraries on
// disk, shared by the retarget unit and integration tests: deckDir's
// kalide.yaml starts at templates: ../shared-lib, libDir is that startup
// library, and otherDir is the retarget target. Building it through
// writeExternalLibrary keeps the two libraries valid for the loader, the
// validator, the renderer and the media/theme routes.
type retargetFixture struct {
	deckDir  string
	libDir   string
	otherDir string
}

// writeRetargetFixture writes retargetFixture to a fresh temporary directory:
// the deck (kalide.yaml + one hello slide) and the two valid external
// libraries. The deck starts at retargetStartup so a later edit to
// retargetOther is a retarget against the pinned baseline.
func writeRetargetFixture(t *testing.T) retargetFixture {
	t.Helper()
	parent := t.TempDir()
	f := retargetFixture{
		deckDir:  filepath.Join(parent, "deck"),
		libDir:   filepath.Join(parent, "shared-lib"),
		otherDir: filepath.Join(parent, "other-lib"),
	}
	writeFile(t, filepath.Join(f.deckDir, "kalide.yaml"),
		retargetDeckYAML("Retarget Deck", retargetStartup))
	writeFile(t, filepath.Join(f.deckDir, "slides", "1-hello.md"),
		"---\ntemplate: hello\ntitle: Hi\n---\n")
	writeExternalLibrary(t, f.libDir, "shared-lib", "shared hello description",
		"<section><h1>{{.title}}</h1></section>\n")
	writeExternalLibrary(t, f.otherDir, "other-lib", "other hello description",
		"<section><h1 class=\"other\">{{.title}}</h1></section>\n")
	return f
}

// retargetDeckYAML renders the fixture's kalide.yaml for the given title and
// raw `templates:` value. An empty templates value omits the key entirely, the
// key-absent spelling (the baseline for a key added later); every other value
// is written verbatim, so a `./`-spelled or retargeted path is preserved
// exactly for the raw-value comparison.
func retargetDeckYAML(title, templates string) string {
	yaml := "title: " + title + "\ntheme: default\n"
	if templates != "" {
		yaml += "templates: " + templates + "\n"
	}
	return yaml
}

// TestTemplatesRetargeted is the unit-tier proof (plan.phase-02.task-2) of the
// pure predicate templatesRetargeted: it compares the raw configured
// `templates:` value exactly as written, so only an byte-identical string is a
// non-retarget. The table covers every documented shape — an equal value, a
// different path, a `./`-prefixed spelling of the same path, the key added,
// the key removed, the key absent throughout, and a value reverted to the
// startup baseline.
func TestTemplatesRetargeted(t *testing.T) {
	cases := []struct {
		name    string
		startup string
		current string
		want    bool
	}{
		{"equal value", "/srv/deck/templates", "/srv/deck/templates", false},
		{"different path", retargetStartup, retargetOther, true},
		{"./-prefixed spelling of the same path", retargetStartup, "./" + retargetStartup, true},
		{"key added", "", retargetStartup, true},
		{"key removed", retargetStartup, "", true},
		{"key absent throughout", "", "", false},
		{"reverted to the startup value", retargetStartup, retargetStartup, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := templatesRetargeted(tc.startup, tc.current); got != tc.want {
				t.Errorf("templatesRetargeted(%q, %q) = %v, want %v", tc.startup, tc.current, got, tc.want)
			}
		})
	}
}

// TestReloaderRetargetRequiresRestartIntegration is the int-tier proof
// (plan.phase-02.task-3) that renderDefault refuses a mid-session retarget of
// the raw `templates:` value with the standard full-page restart-required error
// while the process keeps serving, that reverting the raw value to the startup
// baseline resumes the deck, that editing any other kalide.yaml key (title:)
// with the `templates:` value unchanged re-renders normally, and that a
// library-file edit still live-reloads under the pinned root
// (template-retarget-requires-restart, never-serve-broken-deck).
//
// It writes the real deck and external libraries from writeRetargetFixture to
// temporary directories and drives NewReloader/(Reloader).Reload directly (no
// watcher, no SSE stream), so it is skipped under -short.
func TestReloaderRetargetRequiresRestartIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck and external libraries from disk")
	}

	f := writeRetargetFixture(t)

	// Load the startup external library, registry and themes the way
	// `kalide start` does and hand them to Listen, so the /templates gallery
	// and the media/theme routes keep serving from the pinned root across the
	// retarget.
	lib, err := template.LoadDeckLibrary(f.deckDir, retargetStartup)
	if err != nil {
		t.Fatalf("template.LoadDeckLibrary: %v", err)
	}
	themes, err := theme.LoadDir(os.DirFS(lib.RootPath), template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}

	srv := mustListen(t, Options{Root: f.deckDir, Library: lib, Themes: themes})
	rl, err := NewReloader(ReloadOptions{Server: srv, Root: f.deckDir, Title: "Retarget", Log: io.Discard})
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}
	if err := rl.Start(); err != nil {
		t.Fatalf("Reloader.Start: %v", err)
	}
	t.Cleanup(rl.Stop)

	// The startup value renders the deck from the pinned library.
	body := getBody(t, srv, rootPath)
	if !strings.Contains(body, "<h1>Hi</h1>") {
		t.Fatalf("initial page does not render the startup library's hello template: %q", body)
	}
	if strings.Contains(body, "Deck error") {
		t.Fatalf("initial page unexpectedly serves the error page: %q", body)
	}

	// A mid-session retarget is refused: the full-page restart-required error
	// is published, not the other library's deck, and the process keeps
	// serving (getBody would fail on a non-200).
	writeFile(t, filepath.Join(f.deckDir, "kalide.yaml"),
		retargetDeckYAML("Retarget Deck", retargetOther))
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if !strings.Contains(body, "Deck error") {
		t.Fatalf("mid-session retarget did not serve the error page: %q", body)
	}
	want := restartRequiredError().Error()
	if !strings.Contains(body, want) {
		t.Fatalf("retarget error page does not carry the restart-required error %q: %q", want, body)
	}
	if strings.Contains(body, `<h1 class="other">Hi</h1>`) {
		t.Fatalf("mid-session retarget was applied live instead of requiring a restart: %q", body)
	}

	// Reverting the raw value to the startup baseline resumes the deck.
	writeFile(t, filepath.Join(f.deckDir, "kalide.yaml"),
		retargetDeckYAML("Retarget Deck", retargetStartup))
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") || strings.Contains(body, want) {
		t.Fatalf("reverting the templates value did not resume the deck: %q", body)
	}
	if !strings.Contains(body, "<h1>Hi</h1>") {
		t.Fatalf("reverted page does not render the startup library's hello template: %q", body)
	}

	// Editing another kalide.yaml key with the `templates:` value unchanged is
	// not a retarget: the deck re-renders normally with the new title.
	writeFile(t, filepath.Join(f.deckDir, "kalide.yaml"),
		retargetDeckYAML("Renamed Deck", retargetStartup))
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") || strings.Contains(body, want) {
		t.Fatalf("a title: edit with an unchanged templates value triggered the restart error: %q", body)
	}
	if !strings.Contains(body, "Renamed Deck") {
		t.Fatalf("a title: edit with an unchanged templates value was not re-rendered: %q", body)
	}
	if !strings.Contains(body, "<h1>Hi</h1>") {
		t.Fatalf("a title: edit with an unchanged templates value lost the deck page: %q", body)
	}

	// A library-file edit under the pinned root still live-reloads.
	writeFile(t, filepath.Join(f.libDir, "slides", "hello", "layout.html.tmpl"),
		"<section><h1 class=\"edited\">{{.title}}</h1></section>\n")
	rl.Reload()
	body = getBody(t, srv, rootPath)
	if strings.Contains(body, "Deck error") || strings.Contains(body, want) {
		t.Fatalf("a library-file edit under the pinned root served the error page: %q", body)
	}
	if !strings.Contains(body, `<h1 class="edited">Hi</h1>`) {
		t.Fatalf("a library-file edit under the pinned root did not live-reload: %q", body)
	}
}
