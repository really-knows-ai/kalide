package e2e

// This file is the phase-9 task-2 end-to-end test for the native release
// verification: it runs `eypres init` against the real eypres binary on each
// native runner (macOS arm64, Windows amd64/arm64).
//
// Native CI builds the per-target release binary once and passes it to the
// harness through EYPRES_BINARY (harness.go skips its own build then); when the
// variable is unset — a developer running `go test ./e2e` locally — the harness
// compiles the same CGO-free binary from source. Either way this test drives
// that exact binary.
//
// The sequence:
//
//   - a clean temporary working directory is scaffolded with `eypres init`,
//     which must exit 0 and write the embedded hello seed;
//   - every path init reports exists on disk, and the written seed loads and
//     validates cleanly through template.LoadLibrary and
//     template.NewRegistryFromLibrary;
//   - a second `eypres init` in the same directory refuses without touching
//     anything already on disk.
//
// Serving the scaffolded deck (`eypres start`), polling the served page for
// starter-slide content, crawling its assets (reveal.js, the theme
// stylesheet, embedded fonts and logos) and asserting the eypres process
// makes no outbound connection are restored once start validates against the
// hello seed's own templates (a task added by the phase-3 writer) and
// finalized in phase-06 task-5. Until then this test only exercises init.
//
// It builds (or runs) a real binary, so it is skipped under -short.

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestNativeRelease is the end-to-end native release init verification
// described at the top of this file.
func TestNativeRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e native release test builds or runs the real eypres binary")
	}

	h := NewHarness(t)

	// Report which binary this run exercises: native CI feeds the prebuilt
	// per-target binary through EYPRES_BINARY, a local run lets the harness
	// build one.
	if prebuilt := os.Getenv("EYPRES_BINARY"); prebuilt != "" {
		t.Logf("native release: using prebuilt EYPRES_BINARY=%s", prebuilt)
	} else {
		t.Logf("native release: EYPRES_BINARY unset; harness built %s from source", h.BinaryPath())
	}

	// The harness working directory is clean: init must start from a directory
	// that holds no deck entries.
	assertDirEmpty(t, h.WorkDir())

	// `eypres init` scaffolds the hello seed, reports the created paths and
	// exits 0.
	stdout, stderr, code := h.Run("init")
	if code != 0 {
		t.Fatalf("eypres init exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("eypres init stderr = %q, want empty", stderr)
	}
	for _, rel := range initCreatedPaths {
		if !strings.Contains(stdout, rel) {
			t.Errorf("eypres init output does not name %q:\n%s", rel, stdout)
		}
	}
	if !strings.Contains(stdout, "assets/") {
		t.Errorf("eypres init output does not name the assets/ directory:\n%s", stdout)
	}

	// The reported paths exist on disk, and assets/ is the empty directory init
	// makes explicitly (an empty directory is not embeddable).
	for _, rel := range initCreatedPaths {
		if _, err := os.Stat(h.Path(rel)); err != nil {
			t.Errorf("eypres init did not create %s: %v", rel, err)
		}
	}
	assertDirEmpty(t, h.Path("assets"))

	// The written seed loads and validates cleanly through the same path
	// `eypres start` uses.
	assertSeedLoads(t, h.WorkDir())

	// Snapshot the scaffolded tree so the second init can be proven not to
	// have touched it.
	before := snapshotTree(t, h.WorkDir())

	// A second `eypres init` in the same directory refuses: non-zero exit,
	// the refusal message naming the blocking paths, and nothing written.
	stdout, stderr, code = h.Run("init")
	if code == 0 {
		t.Fatalf("second eypres init exit = 0, want non-zero (stdout = %q)", stdout)
	}
	if stdout != "" {
		t.Errorf("second eypres init stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"slides/", "templates/", "assets/", "eypres.yaml", "never overwrites"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("second eypres init refusal does not mention %q:\n%s", want, stderr)
		}
	}

	after := snapshotTree(t, h.WorkDir())
	if !reflect.DeepEqual(before, after) {
		t.Errorf("second eypres init modified the directory:\nbefore = %v\nafter  = %v", before, after)
	}
}
