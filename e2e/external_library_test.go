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
	"os"
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
