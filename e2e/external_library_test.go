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
//   - the run is offline (EnableOfflineProxy plus the assertOffline connection
//     sampler) and stops gracefully (h.Stop's exit-0 / port-released /
//     no-leftover-children post-conditions).
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

	// A graceful stop exits 0, releases the port and leaves no child behind;
	// the harness asserts all three in Stop. off.Stop reports any outbound
	// connection the whole run made.
	h.Stop()
	off.Stop()
}
