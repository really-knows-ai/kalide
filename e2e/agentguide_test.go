package e2e

// This file is the phase-01 task-25 end-to-end test for the kalide-owned
// author-guide pair delivered by every scaffolding command
// (requirements.requirement.agent-authoring,
// requirements.requirement.agent-guide-reference).
//
// It drives the real kalide binary the harness builds (harness.go) through the
// three scaffolding surfaces and asserts each delivers the pair — the full
// AGENTS_KALIDE.md guide plus an AGENTS.md pointer that references it — and
// that the scaffolded result is still valid and servable:
//
//   - `kalide init` (the built-in starter seed) writes the pair and the seed
//     still loads and serves;
//   - `kalide init <path>` (an external template library) writes the pair at
//     the deck root and the deck still validates and serves from the library;
//   - `kalide init-library <path>` writes the pair at the library root and the
//     library still loads cleanly.
//
// The pair is inert: it neither blocks the scaffold nor changes what the loader
// validates. It builds a binary, so it is skipped under -short.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/server"
)

// agentGuideFullGuideName is the full author-guide file every scaffolding
// command writes next to its AGENTS.md pointer, and the target of the pointer's
// reference line.
const agentGuideFullGuideName = "AGENTS_KALIDE.md"

// agentGuideReferenceLine is the exact line an AGENTS.md pointer carries once
// it references the full guide (agent-guide-reference): the marker a test uses
// to recognise a delivered pointer.
const agentGuideReferenceLine = "Kalide Reference: ./AGENTS_KALIDE.md"

// TestAgentGuidePairDelivered asserts the three scaffolding surfaces deliver
// the AGENTS_KALIDE.md + AGENTS.md author-guide pair and leave a valid,
// servable result.
func TestAgentGuidePairDelivered(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	t.Run("init seed writes the pair and serves", func(t *testing.T) {
		h := NewHarness(t)
		home := t.TempDir()

		stdout, stderr, code := runKalideWithHome(t, h, home, "init")
		if code != 0 {
			t.Fatalf("kalide init exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init stderr = %q, want empty", stderr)
		}
		for _, want := range []string{agentGuideFullGuideName, "AGENTS.md"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("kalide init output does not name %q:\n%s", want, stdout)
			}
		}

		// The seed carries the pair and is inert to the loader.
		assertAgentGuidePair(t, h.WorkDir())
		assertSeedLoads(t, h.WorkDir())

		// `kalide start` validates and serves the seed written alongside the
		// pair, so the pair changed nothing the server needs.
		h.Start()
		body, err := h.GetString("/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		if !strings.Contains(body, "Hello, world") {
			t.Errorf("served seed deck does not carry the hello slide's title:\n%s", body)
		}
		h.Stop()
	})

	t.Run("init path writes the pair and serves the deck", func(t *testing.T) {
		h := NewHarness(t)
		home := t.TempDir()
		libDir := newInitLibrary(t, "pair-lib", "pair-delivered-library", "pair-delivered-theme")

		stdout, stderr, code := runKalideWithHome(t, h, home, "init", libDir)
		if code != 0 {
			t.Fatalf("kalide init <path> exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init <path> stderr = %q, want empty", stderr)
		}
		for _, want := range []string{agentGuideFullGuideName, "AGENTS.md"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("kalide init <path> output does not name %q:\n%s", want, stdout)
			}
		}

		// The pair lands at the deck root, not in the referenced library.
		assertAgentGuidePair(t, h.WorkDir())

		// `kalide start` validates the deck written alongside the pair and
		// serves it from the external library, pinned by that library's theme
		// marker.
		h.Start()
		body, err := h.GetString("/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		if strings.Contains(body, "Deck error") {
			t.Fatalf("served page is the error page, want the external-library deck:\n%s", body)
		}
		served, err := h.GetString(server.ThemesPath + "default/theme.css")
		if err != nil {
			t.Fatalf("GET %sdefault/theme.css: %v", server.ThemesPath, err)
		}
		if !strings.Contains(served, "pair-delivered-theme") {
			t.Errorf("served %sdefault/theme.css does not come from the external library:\n%s", server.ThemesPath, served)
		}
		h.Stop()
	})

	t.Run("init-library writes the pair and loads", func(t *testing.T) {
		h := NewHarness(t)

		stdout, stderr, code := h.Run("init-library", "pair-lib")
		if code != 0 {
			t.Fatalf("kalide init-library pair-lib exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init-library pair-lib stderr = %q, want empty", stderr)
		}
		for _, want := range []string{agentGuideFullGuideName, "AGENTS.md"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("kalide init-library output does not name %q:\n%s", want, stdout)
			}
		}

		// The library carries the pair at its root and still loads through the
		// same template loader path `kalide start` uses.
		libDir := h.Path("pair-lib")
		assertAgentGuidePair(t, libDir)
		assertLibraryLoads(t, libDir)
	})
}

// assertAgentGuidePair asserts dir carries the delivered author-guide pair: a
// non-empty AGENTS_KALIDE.md full guide and an AGENTS.md whose bytes contain
// the reference line.
func assertAgentGuidePair(t *testing.T, dir string) {
	t.Helper()

	full := filepath.Join(dir, agentGuideFullGuideName)
	info, err := os.Stat(full)
	if err != nil {
		t.Fatalf("%s was not written at %s: %v", agentGuideFullGuideName, dir, err)
	}
	if info.IsDir() || info.Size() == 0 {
		t.Errorf("%s is empty or a directory, want the full embedded guide", full)
	}

	pointerPath := filepath.Join(dir, "AGENTS.md")
	pointer, err := os.ReadFile(pointerPath)
	if err != nil {
		t.Fatalf("%s was not written at %s: %v", "AGENTS.md", dir, err)
	}
	if !strings.Contains(string(pointer), agentGuideReferenceLine) {
		t.Errorf("%s does not reference the full guide %q:\n%s", pointerPath, agentGuideReferenceLine, pointer)
	}
}

// assertAgentGuideAppended asserts the AGENTS.md at path holds the author's
// bytes verbatim with only the reference line appended (a single newline
// separating them when the author content does not end in one), the
// agent-guide-reference outcome for author content without the reference
// string.
func assertAgentGuideAppended(t *testing.T, path, author string) {
	t.Helper()

	want := author
	if want != "" && !strings.HasSuffix(want, "\n") {
		want += "\n"
	}
	want += agentGuideReferenceLine + "\n"

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want the author bytes with the appended reference line %q", path, got, want)
	}
}

// rescan marker
