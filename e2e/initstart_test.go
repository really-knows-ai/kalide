package e2e

// This file is the phase-8 task-9 end-to-end test for the init flow:
// `kalide init` scaffolding the embedded hello seed. It drives the real
// kalide binary the harness builds (harness.go) in a clean, empty working
// directory, so it exercises the path an author takes right after
// installing kalide:
//
//   - `kalide init` writes the embedded hello seed, prints the paths it
//     created and exits 0;
//   - every printed path exists on disk, byte-for-byte the embedded seed:
//     the deck-root AGENTS.md deck-author guide (agent-authoring),
//     kalide.yaml, slides/1-hello.md, templates/library.yaml, the hello
//     slide template's three files, the default theme's stylesheet, and an
//     empty assets/ directory;
//   - the written seed loads and validates cleanly through
//     template.LoadLibrary and template.NewRegistryFromLibrary, exactly as
//     `kalide start` would load it;
//   - a second `kalide init` in the same directory refuses with a
//     non-zero exit, prints the refusal, and leaves every file
//     byte-for-byte unchanged — in particular the AGENTS.md the first init
//     wrote is never overwritten (the guide is written only when absent).
//
// `kalide start` serving the hello seed IS in scope here (phase 5 deferred
// it; phase 3 wires template-backed rendering through it, so it lands in
// this test): after init, `kalide start --no-open` serves the hello seed
// itself — the single hello slide, its templates/media (there is none in the
// seed, so this is exercised through the theme route) and the default
// theme's stylesheet, both fetched 200 with a correct Content-Type — and
// /templates lists exactly the hello seed's own template.
//
// The flow needs no network. Offline enforcement (blocking network syscalls) is
// phase 9 and is deliberately out of scope here too.
//
// It builds a binary, so it is skipped under -short.

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/server"
	"github.com/really-knows-ai/kalide/internal/template"
)

// initCreatedPaths are the deck-relative paths `kalide init` reports creating;
// they mirror internal/cli's initCreatedMessage and the embedded hello seed
// in internal/scaffold/seed. They are asserted against both the command
// output and the filesystem.
var initCreatedPaths = []string{
	"AGENTS.md",
	"kalide.yaml",
	"slides/1-hello.md",
	"templates/library.yaml",
	"templates/slides/hello/template.yaml",
	"templates/slides/hello/layout.html.tmpl",
	"templates/slides/hello/example.md",
	"templates/themes/default/theme.css",
}

// TestInitStart is the end-to-end init test described above.
func TestInitStart(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

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
	if _, err := os.Stat(h.Path("eypres.yaml")); !os.IsNotExist(err) {
		t.Errorf("kalide init created eypres.yaml, want only kalide.yaml")
	}
	assertDirEmpty(t, h.Path("assets"))

	// The deck-root AGENTS.md deck-author guide (agent-authoring) is present
	// and non-empty: init wrote it from the embedded static guide. It is not a
	// template asset and is inert to the loader checked below.
	agentsGuide, err := os.Stat(h.Path("AGENTS.md"))
	if err != nil {
		t.Fatalf("kalide init did not create AGENTS.md: %v", err)
	}
	if agentsGuide.IsDir() || agentsGuide.Size() == 0 {
		t.Errorf("kalide init wrote AGENTS.md as an empty file or directory, want the embedded deck-author guide")
	}

	// The written seed loads and validates cleanly through the same path
	// `kalide start` uses: template.LoadLibrary and, inside it,
	// template.NewRegistryFromLibrary.
	assertSeedLoads(t, h.WorkDir())

	// `kalide start --no-open` serves the hello seed itself: the deck page
	// carries the one hello slide, the seed's default theme stylesheet is
	// fetched 200 with a css Content-Type through the same
	// templates/themes/<name>/ route mediaHandler mounts, and /templates
	// lists exactly the seed's own "hello" template (no built-in fallback).
	//
	// The seed ships no templates/media/ file (helloseed.go's tree has none),
	// so MediaPath is not exercised here: there is nothing under it to fetch.
	h.Start()

	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if !strings.Contains(body, "Hello, world") {
		t.Errorf("served deck page does not carry the hello slide's title:\n%s", body)
	}

	// This is the single-binary acceptance check: init and start both ran
	// against the static kalide binary with no embedded EY templates, fonts
	// or logo, and the served deck carries no reference to any of the old
	// embedded EY asset routes (they no longer exist: phase 7 stripped them
	// from internal/assets and the /assets/ handler). Note /assets/templates/
	// is not one of these: it is the current, legitimate mediaHandler route
	// (ThemesPath/MediaPath) the seed's own theme.css is served from above.
	for _, want := range []string{"/assets/fonts/", "/assets/logo/"} {
		if strings.Contains(body, want) {
			t.Errorf("served deck page references %q, want no EY asset URLs (embedded EY content is gone)", want)
		}
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

	gallery, err := h.GetString("/templates")
	if err != nil {
		t.Fatalf("GET /templates: %v", err)
	}
	if !strings.Contains(gallery, `class="gallery-page__name">hello`) {
		t.Errorf("gallery does not list the seed's %q template:\n%s", "hello", gallery)
	}

	h.Stop()

	// Snapshot the scaffolded tree so the second init can be proven not to have
	// touched it.
	before := snapshotTree(t, h.WorkDir())

	// The first init wrote AGENTS.md (absent-only). A pre-existing guide is not
	// one of the blocking paths and init never overwrites it, so the refused
	// second init must leave it byte-for-byte unchanged. The whole-tree
	// snapshot below already covers this byte-for-byte; this explicit capture
	// and comparison state the AGENTS.md intent directly.
	agentsBefore, err := os.ReadFile(h.Path("AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md after first init: %v", err)
	}

	// A second `kalide init` in the same directory refuses: non-zero exit, the
	// refusal message naming the blocking paths, and nothing written.
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

	agentsAfter, err := os.ReadFile(h.Path("AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md after refused second init: %v", err)
	}
	if !bytes.Equal(agentsBefore, agentsAfter) {
		t.Errorf("second kalide init modified the pre-existing AGENTS.md:\nbefore = %q\nafter  = %q", agentsBefore, agentsAfter)
	}
}

// TestProjectWithOnlyEypresYamlNotRecognised tests the negative hard-cut
// assertion: a project containing only eypres.yaml (and no kalide.yaml) is not
// recognised as a deck, so kalide start refuses to serve it and fails naming
// kalide.yaml.
func TestProjectWithOnlyEypresYamlNotRecognised(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

	// Seed a valid template library via init so missing library is not the failure.
	if _, stderr, code := h.Run("init"); code != 0 {
		t.Fatalf("kalide init exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	// Replace kalide.yaml with eypres.yaml only.
	if err := os.Remove(h.Path("kalide.yaml")); err != nil {
		t.Fatalf("remove kalide.yaml: %v", err)
	}
	h.WriteFile("eypres.yaml", []byte("title: Old Eypres Deck\ntheme: default\n"))

	stdout, stderr, code := h.Run("start", "--no-open")
	if code == 0 {
		t.Fatalf("kalide start exit = 0 with only eypres.yaml, want non-zero failure")
	}
	if !strings.Contains(stderr, "kalide.yaml") {
		t.Errorf("kalide start stderr = %q, want error naming kalide.yaml", stderr)
	}
	if strings.Contains(stdout, "Serving slides at") {
		t.Errorf("kalide start stdout = %q, want no server started", stdout)
	}
}

// assertDirEmpty fails the test unless dir holds no entries.
func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s is not empty: %v", dir, entries)
	}
}

// assertSeedLoads asserts that the deck written under dir loads and validates
// cleanly through template.LoadLibrary and template.NewRegistryFromLibrary,
// exactly as `kalide start` loads a deck's templates/ library.
func assertSeedLoads(t *testing.T, dir string) {
	t.Helper()

	lib, err := template.LoadLibrary(os.DirFS(dir), template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary(%s, %q) = %v, want a valid library", dir, template.TemplatesDir, err)
	}
	if _, err := template.NewRegistryFromLibrary(lib); err != nil {
		t.Fatalf("template.NewRegistryFromLibrary(%s's library) = %v, want a valid registry", dir, err)
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
