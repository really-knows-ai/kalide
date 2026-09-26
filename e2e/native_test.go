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
// Serving the scaffolded deck (`eypres start`) and fetching the hello
// slide's own theme stylesheet from templates/themes/default/ are exercised
// below, now that start validates against the hello seed's own templates/
// library. reveal.js and the rest of the embedded assets are crawled by
// TestEmbeddedAssets in internal/server; here the native-release check is
// that the served page carries the reveal.js script tag and the seed's
// stylesheet, both fetched 200. Asserting the eypres process makes no
// outbound connection is phase-06 task-5's offline-enforcement pass and stays
// out of scope here.
//
// It builds (or runs) a real binary, so it is skipped under -short.

import (
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/server"
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

	// `eypres start --no-open` serves the hello seed itself: the deck page
	// carries the single hello slide, the embedded reveal.js core is fetched
	// 200, and the seed's own default theme stylesheet
	// (templates/themes/default/theme.css) is fetched 200 from the project
	// library through the same route mediaHandler mounts.
	h.Start()

	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if !strings.Contains(body, "Hello, world") {
		t.Errorf("served deck page does not carry the hello slide's title:\n%s", body)
	}
	if !strings.Contains(body, "reveal.js") {
		t.Errorf("served deck page does not reference reveal.js:\n%s", body)
	}

	revealResp, err := h.Get(server.AssetsPath + "reveal/dist/reveal.js")
	if err != nil {
		t.Fatalf("GET %sreveal/dist/reveal.js: %v", server.AssetsPath, err)
	}
	revealBody, err := io.ReadAll(revealResp.Body)
	_ = revealResp.Body.Close()
	if err != nil {
		t.Fatalf("read reveal.js response body: %v", err)
	}
	if revealResp.StatusCode != http.StatusOK {
		t.Errorf("GET %sreveal/dist/reveal.js status = %d, want 200", server.AssetsPath, revealResp.StatusCode)
	}
	if len(revealBody) == 0 {
		t.Error("GET reveal/dist/reveal.js served an empty body")
	}

	themeResp, err := h.Get(server.ThemesPath + "default/theme.css")
	if err != nil {
		t.Fatalf("GET %sdefault/theme.css: %v", server.ThemesPath, err)
	}
	themeBody, err := io.ReadAll(themeResp.Body)
	_ = themeResp.Body.Close()
	if err != nil {
		t.Fatalf("read theme response body: %v", err)
	}
	if themeResp.StatusCode != http.StatusOK {
		t.Errorf("GET %sdefault/theme.css status = %d, want 200 (body = %q)",
			server.ThemesPath, themeResp.StatusCode, themeBody)
	}
	if ct := themeResp.Header.Get("Content-Type"); !strings.Contains(ct, "css") {
		t.Errorf("GET %sdefault/theme.css Content-Type = %q, want it to mention css", server.ThemesPath, ct)
	}

	h.Stop()

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
