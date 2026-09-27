package e2e

// This file is the phase-9 task-2 end-to-end test for the native release
// verification: it runs `kalide init` against the real kalide binary on each
// native runner (macOS arm64, Windows amd64/arm64).
//
// Native CI builds the per-target release binary once and passes it to the
// harness through KALIDE_BINARY (harness.go skips its own build then); when the
// variable is unset — a developer running `go test ./e2e` locally — the harness
// compiles the same CGO-free binary from source. Either way this test drives
// that exact binary.
//
// The sequence:
//
//   - a clean temporary working directory is scaffolded with `kalide init`,
//     which must exit 0 and write the embedded hello seed;
//   - every path init reports exists on disk, and the written seed loads and
//     validates cleanly through template.LoadLibrary and
//     template.NewRegistryFromLibrary;
//   - a second `kalide init` in the same directory refuses without touching
//     anything already on disk.
//
// Serving the scaffolded deck (`kalide start`) and fetching the hello
// slide's own theme stylesheet from templates/themes/default/ are exercised
// below, now that start validates against the hello seed's own templates/
// library. reveal.js and the rest of the embedded assets are crawled by
// TestEmbeddedAssets in internal/server; here the native-release check is
// that the served page carries the reveal.js script tag and the seed's
// stylesheet, both fetched 200.
//
// Phase-06 task-5 finalizes the assertions this file makes about the served
// deck:
//
//   - a single <section> element is served — the seed writes exactly one
//     slide, and its layout already emits its own root <section> so no second
//     wrapper is added (internal/render's addAnchor);
//   - the theme stylesheet fetched from templates/themes/default/theme.css is
//     byte-for-byte the project's own file on disk, never the internal/assets
//     embedded theme.css — mediaHandler's ThemesPath route only ever reads
//     from the project's templates/themes/<name>/ directory;
//   - reveal.js is still fetched from the embedded core
//     (server.AssetsPath+"reveal/dist/reveal.js"), unaffected by the project
//     templates/ library.
//
// Mapping to requirements.requirement.native-ci-verification (e2e steps 1-6)
// and requirements.requirement.supported-platforms:
//
//   - step 1 (clean temp dir, `kalide init`, `kalide start --no-open` in its
//     own process group): assertDirEmpty + h.Run("init") here; h.Start in
//     harness.go (setProcessGroup: Setpgid on macOS, CREATE_NEW_PROCESS_GROUP
//     on Windows — harness_unix.go / harness_windows.go);
//   - step 2 (parse the printed URL): Harness.waitForURL / findServingURL;
//   - step 3 (poll ~30s for HTTP 200 with the single hello slide):
//     Harness.waitReady (readyTimeout = 30s) plus the "Hello, world" and
//     single-<section> assertions here;
//   - step 4 (reveal.js from the embedded core and the theme from
//     templates/themes/default/, both referenced by the page, served 200 by
//     the local server, no external URLs): the page-reference, no-external
//     src/href, reveal.js and theme.css assertions here;
//   - step 5 (offline): h.EnableOfflineProxy before Start plus the
//     assertOffline connection sampler (offline.go) for the whole serve;
//   - step 6 (graceful stop: SIGTERM on macOS, CTRL_BREAK_EVENT to the group
//     on Windows; exit 0, port released, no leftover children; forced kill
//     only as a failing timeout fallback): h.Stop / checkShutdown / forceKill;
//   - supported-platforms: a prebuilt KALIDE_BINARY must be named
//     kalide-<goos>-<goarch> for a supported target (darwin/arm64,
//     windows/amd64, windows/arm64); never darwin/amd64 or Linux.
//
// It builds (or runs) a real binary, so it is skipped under -short.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/server"
)

// supportedTargets are the GOOS/GOARCH pairs of supported-platforms: darwin/arm64
// and windows/amd64 required, windows/arm64 conditional on its native runner.
var supportedTargets = map[string]bool{
	"darwin/arm64":  true,
	"windows/amd64": true,
	"windows/arm64": true,
}

// externalRefPattern matches a src/href attribute that loads from an
// http(s) URL, i.e. anything the deck page would fetch from off the local
// server's relative paths.
var externalRefPattern = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["']?(?:https?:)?//[^"'\s>]+`)

// TestNativeRelease is the end-to-end native release init verification
// described at the top of this file.
func TestNativeRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e native release test builds or runs the real kalide binary")
	}

	h := NewHarness(t)

	// Report which binary this run exercises: native CI feeds the prebuilt
	// per-target binary through KALIDE_BINARY, a local run lets the harness
	// build one.
	if prebuilt := os.Getenv("KALIDE_BINARY"); prebuilt != "" {
		t.Logf("native release: using prebuilt KALIDE_BINARY=%s", prebuilt)
		base := strings.TrimSuffix(filepath.Base(prebuilt), ".exe")
		wantName := fmt.Sprintf("kalide-%s-%s", runtime.GOOS, runtime.GOARCH)
		if base != wantName {
			t.Errorf("prebuilt KALIDE_BINARY %q does not match expected naming %q", base, wantName)
		}
		if !supportedTargets[runtime.GOOS+"/"+runtime.GOARCH] {
			t.Errorf("native release run on %s/%s, which is not a supported target (supported-platforms)",
				runtime.GOOS, runtime.GOARCH)
		}
	} else {
		t.Logf("native release: KALIDE_BINARY unset; harness built %s from source", h.BinaryPath())
	}

	// The harness working directory is clean: init must start from a directory
	// that holds no deck entries.
	assertDirEmpty(t, h.WorkDir())

	// `kalide init` scaffolds the hello seed, reports the created paths and
	// exits 0.
	stdout, stderr, code := h.Run("init")
	if code != 0 {
		t.Fatalf("kalide init exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide init stderr = %q, want empty", stderr)
	}
	for _, rel := range initCreatedPaths {
		if !strings.Contains(stdout, rel) {
			t.Errorf("kalide init output does not name %q:\n%s", rel, stdout)
		}
	}
	if !strings.Contains(stdout, "assets/") {
		t.Errorf("kalide init output does not name the assets/ directory:\n%s", stdout)
	}

	// The reported paths exist on disk, and assets/ is the empty directory init
	// makes explicitly (an empty directory is not embeddable).
	for _, rel := range initCreatedPaths {
		if _, err := os.Stat(h.Path(rel)); err != nil {
			t.Errorf("kalide init did not create %s: %v", rel, err)
		}
	}
	assertDirEmpty(t, h.Path("assets"))

	// The written seed loads and validates cleanly through the same path
	// `kalide start` uses.
	assertSeedLoads(t, h.WorkDir())

	// `kalide start --no-open` serves the hello seed itself: the deck page
	// carries the single hello slide, the embedded reveal.js core is fetched
	// 200, and the seed's own default theme stylesheet
	// (templates/themes/default/theme.css) is fetched 200 from the project
	// library through the same route mediaHandler mounts.
	// Step 5: keep the process offline for the whole serve.
	h.EnableOfflineProxy()
	h.Start()

	off := assertOffline(t, h.PID())
	t.Cleanup(off.Stop)

	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	// Step 4: the page itself references the local reveal.js core and the
	// project theme, and loads nothing from an external URL.
	for _, ref := range []string{server.AssetsPath + "reveal/dist/reveal.js", server.ThemesPath + "default/theme.css"} {
		if !strings.Contains(body, ref) {
			t.Errorf("served deck page does not reference %s:\n%s", ref, body)
		}
	}
	if m := externalRefPattern.FindAllString(body, -1); len(m) > 0 {
		t.Errorf("served deck page references external URLs %q", m)
	}
	if !strings.Contains(body, "Hello, world") {
		t.Errorf("served deck page does not carry the hello slide's title:\n%s", body)
	}
	// The seed writes exactly one slide (slides/1-hello.md); its layout
	// already emits its own root <section>, so addAnchor (internal/render)
	// attaches the slide's anchor id to that same element instead of wrapping
	// a second one around it. A single <section> in the served page is
	// therefore the single-slide assertion.
	if n := strings.Count(body, "<section"); n != 1 {
		t.Errorf("served deck page has %d <section> element(s), want exactly 1 (the single hello slide):\n%s", n, body)
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

	// The served stylesheet is byte-for-byte the project's own
	// templates/themes/default/theme.css on disk (the file init wrote), not
	// the internal/assets embedded theme.css: mediaHandler's ThemesPath route
	// (internal/server/media.go) only ever reads from the project's
	// templates/themes/<name>/ directory, so a match here pins that the
	// stylesheet came from the project, never from the embedded core.
	onDiskTheme, err := os.ReadFile(h.Path("templates/themes/default/theme.css"))
	if err != nil {
		t.Fatalf("read %s: %v", h.Path("templates/themes/default/theme.css"), err)
	}
	if string(themeBody) != string(onDiskTheme) {
		t.Errorf("served %sdefault/theme.css does not match the project's templates/themes/default/theme.css on disk",
			server.ThemesPath)
	}

	h.Stop()
	off.Stop()

	// Snapshot the scaffolded tree so the second init can be proven not to
	// have touched it.
	before := snapshotTree(t, h.WorkDir())

	// A second `kalide init` in the same directory refuses: non-zero exit,
	// the refusal message naming the blocking paths, and nothing written.
	stdout, stderr, code = h.Run("init")
	if code == 0 {
		t.Fatalf("second kalide init exit = 0, want non-zero (stdout = %q)", stdout)
	}
	if stdout != "" {
		t.Errorf("second kalide init stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"slides/", "templates/", "assets/", "kalide.yaml", "never overwrites"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("second kalide init refusal does not mention %q:\n%s", want, stderr)
		}
	}

	after := snapshotTree(t, h.WorkDir())
	if !reflect.DeepEqual(before, after) {
		t.Errorf("second kalide init modified the directory:\nbefore = %v\nafter  = %v", before, after)
	}
}

// TestEypresBinaryIgnored asserts the hard cut: with only EYPRES_BINARY set,
// the harness ignores it and builds cmd/kalide from source.
func TestEypresBinaryIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds the real kalide binary")
	}

	t.Setenv("KALIDE_BINARY", "")
	t.Setenv("EYPRES_BINARY", "/nonexistent/eypres-binary")

	h := NewHarness(t)
	if h.BinaryPath() == "/nonexistent/eypres-binary" {
		t.Fatalf("harness used EYPRES_BINARY, want it ignored")
	}
	if _, err := os.Stat(h.BinaryPath()); err != nil {
		t.Fatalf("harness did not build binary from source: %v", err)
	}
	base := strings.TrimSuffix(filepath.Base(h.BinaryPath()), ".exe")
	if base != "kalide" {
		t.Errorf("built binary name = %q, want %q", base, "kalide")
	}
}
