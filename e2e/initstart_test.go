package e2e

// This file is the phase-8 task-9 end-to-end test for the starter-deck flow:
// `eypres init` followed by `eypres start`. It drives the real eypres binary the
// harness builds (harness.go) in a clean, empty working directory, so it
// exercises the whole path an author takes:
//
//   - `eypres init` writes the embedded starter deck, prints the paths it
//     created and exits 0;
//   - `eypres start --no-open` validates the scaffolded deck cleanly, prints the
//     127.0.0.1 URL it bound, and serves the deck page;
//   - the served page carries both starter slides' content;
//   - every /assets/… URL the page references resolves with a 200 from the
//     binary's embedded tree (nothing is on disk under assets/);
//   - a graceful stop exits 0, releases the port and leaves no child behind
//     (the harness asserts the last two in Stop);
//   - a second `eypres init` in the same directory refuses with a non-zero
//     exit, prints the refusal, and leaves every file byte-for-byte unchanged.
//
// The flow needs no network. Offline enforcement (blocking network syscalls) is
// phase 9 and is deliberately out of scope here: this test only observes that
// the flow completes without reaching out.
//
// It builds a binary and drives a real server, so it is skipped under -short.

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// initCreatedPaths are the deck-relative paths `eypres init` reports creating;
// they mirror internal/cli's initCreatedMessage. They are asserted against both
// the command output and the filesystem.
var initCreatedPaths = []string{
	"eypres.yaml",
	"slides/1-title.md",
	"slides/2-content.md",
}

// starterSlideContent are distinctive strings from the two embedded starter
// slides (internal/assets/starter): slide 1 is a `title` slide and slide 2 a
// `content` slide with two `column` instances. Finding all of them in the served
// page proves both slides were rendered.
var starterSlideContent = []string{
	"My presentation",  // slide 1 heading (and deck title)
	"A short subtitle", // slide 1 subtitle
	"Key messages",     // slide 2 heading
	"First point",      // slide 2 first column
	"Second point",     // slide 2 second column
}

// assetRefRE pulls the /assets/… URLs out of a served HTML document. A served
// page never contains a bare quoted asset URL in its own text, so the
// trailing-quote exclusion is exact.
var assetRefRE = regexp.MustCompile(`"(/assets/[^"'\s)]+)`)

// TestInitStart is the end-to-end init + start test described above.
func TestInitStart(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real eypres binary")
	}

	h := NewHarness(t)

	// The harness working directory is clean: init must start from a directory
	// that holds no deck entries.
	assertDirEmpty(t, h.WorkDir())

	// `eypres init` scaffolds the starter deck, reports the created paths and
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
	if info, err := os.Stat(h.Path("assets")); err != nil {
		t.Errorf("eypres init did not create assets/: %v", err)
	} else if !info.IsDir() {
		t.Errorf("assets/ exists but is not a directory")
	}

	// Snapshot the scaffolded tree so the second init can be proven not to have
	// touched it.
	before := snapshotTree(t, h.WorkDir())

	// `eypres start --no-open` validates the scaffolded deck and serves it on
	// 127.0.0.1 only.
	h.Start()

	if got := h.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") || !strings.HasSuffix(got, "/") {
		t.Fatalf("start printed URL %q, want http://127.0.0.1:<port>/", got)
	}
	if !strings.Contains(h.Stdout(), "Serving slides at "+h.URL()) {
		t.Errorf("stdout does not name the served URL %q:\n%s", h.URL(), h.Stdout())
	}

	// GET / serves the deck page carrying both starter slides.
	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	for _, want := range starterSlideContent {
		if !strings.Contains(body, want) {
			t.Errorf("served deck page is missing starter slide content %q:\n%s", want, body)
		}
	}

	// Every /assets/… URL the page references resolves from the binary. The
	// scaffolded assets/ directory is empty, so a 200 can only come from the
	// embedded tree.
	refs := assetRefs(body)
	if len(refs) == 0 {
		t.Errorf("served deck page references no /assets/… URLs:\n%s", body)
	}
	for _, want := range []string{"/assets/theme.css", "/assets/reveal/dist/reveal.js"} {
		if !contains(refs, want) {
			t.Errorf("served deck page does not reference %s (references %v)", want, refs)
		}
	}
	for _, ref := range refs {
		asset, err := h.GetOK(ref)
		if err != nil {
			t.Errorf("served deck page references %s, which did not resolve: %v", ref, err)
			continue
		}
		if len(asset) == 0 {
			t.Errorf("referenced asset %s resolved with an empty body", ref)
		}
	}

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

	// A second `eypres init` in the same directory refuses: non-zero exit, the
	// refusal message naming the blocking paths, and nothing written.
	stdout, stderr, code = h.Run("init")
	if code == 0 {
		t.Fatalf("second eypres init exit = 0, want non-zero (stdout = %q)", stdout)
	}
	if stdout != "" {
		t.Errorf("second eypres init stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"slides/", "assets/", "eypres.yaml", "never overwrites"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("second eypres init refusal does not mention %q:\n%s", want, stderr)
		}
	}

	after := snapshotTree(t, h.WorkDir())
	if !reflect.DeepEqual(before, after) {
		t.Errorf("second eypres init modified the directory:\nbefore = %v\nafter  = %v", before, after)
	}
}

// assertDirEmpty fails the test unless dir holds no entries.
func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read harness work dir %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("harness work dir %s is not empty: %v", dir, entries)
	}
}

// snapshotTree returns every path under root (files and directories, relative
// and slash-separated) mapped to its content; a directory maps to "<dir>". It is
// how the second init is proven not to have changed anything.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()

	snap := make(map[string]string)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			snap[rel] = "<dir>"
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		snap[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snap
}

// assetRefs returns the distinct /assets/… URLs referenced by doc, in order of
// first appearance.
func assetRefs(doc string) []string {
	seen := make(map[string]struct{})
	var refs []string
	for _, m := range assetRefRE.FindAllStringSubmatch(doc, -1) {
		ref := m[1]
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

// contains reports whether ss holds s.
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
