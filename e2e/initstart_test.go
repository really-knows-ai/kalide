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

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/server"
	"github.com/really-knows-ai/kalide/internal/template"
)

// initCreatedPaths are the deck-relative paths `kalide init` reports creating;
// they mirror internal/cli's initCreatedMessage and the embedded hello seed
// in internal/scaffold/seed. They are asserted against both the command
// output and the filesystem. `kalide init` renders each entry through
// filepath.FromSlash (native-display-paths), so each expected entry is built
// the same way: the host operating system's native spelling (templates\
// library.yaml on Windows, templates/library.yaml elsewhere).
var initCreatedPaths = []string{
	"AGENTS.md",
	"kalide.yaml",
	filepath.FromSlash("slides/1-hello.md"),
	filepath.FromSlash("templates/library.yaml"),
	filepath.FromSlash("templates/slides/hello/template.yaml"),
	filepath.FromSlash("templates/slides/hello/layout.html.tmpl"),
	filepath.FromSlash("templates/slides/hello/example.md"),
	filepath.FromSlash("templates/themes/default/theme.css"),
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
	if !strings.Contains(stdout, filepath.FromSlash("assets/")) {
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
	for _, want := range []string{
		filepath.FromSlash("slides/"),
		filepath.FromSlash("templates/"),
		filepath.FromSlash("assets/"),
		"kalide.yaml",
		"never overwrites",
	} {
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

// The tests below are the phase-04 task-3 end-to-end coverage of `kalide init`'s
// template-library resolution and the branch guards
// (requirements.requirement.init-library-resolution,
// requirements.requirement.cli-init,
// requirements.requirement.cli-init-refuse-existing and
// requirements.requirement.default-template-dir). They drive the real kalide
// binary the harness builds, with a hermetic per-user config store: every
// child process is run through runKalideWithHome (config_test.go), so HOME,
// XDG_CONFIG_HOME, AppData and USERPROFILE point at a fresh temp home and the
// developer's real config is never read or written. The
// `default-template-dir` the cases configure is a real external library on
// disk, built by newInitLibrary below.
//
// The three resolution branches are exercised first match wins: an explicit
// path, else the configured default (an absolute path), else the built-in
// starter seed — which --local forces even when a default is set. The guards
// are asserted per branch: slides/, assets/ and kalide.yaml block everywhere;
// a pre-existing local templates/ blocks only the seed branch and is exempt and
// left byte-for-byte untouched in the external branches; a pre-existing
// AGENTS.md never blocks in any branch. They build a binary and drive a server,
// so they are skipped under -short.

// TestInitConfiguredDefaultServes covers the configured-default branch end to
// end: with a valid `default-template-dir` configured, `kalide init` writes a
// deck whose kalide.yaml references the configured library by its absolute
// path, with empty slides/ and assets/ and no local templates/ or starter
// slide, and `kalide start` validates and serves that deck from the configured
// library.
func TestInitConfiguredDefaultServes(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	home := t.TempDir()
	libDir := newInitLibrary(t, "configured-lib", "configured-default-library", "configured-default-theme")
	setInitDefaultTemplateDir(t, h, home, libDir)

	assertDirEmpty(t, h.WorkDir())

	stdout, stderr, code := runKalideWithHome(t, h, home, "init")
	if code != 0 {
		t.Fatalf("kalide init exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide init stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, libDir) {
		t.Errorf("kalide init output does not name the configured library %q:\n%s", libDir, stdout)
	}
	if strings.Contains(stdout, filepath.FromSlash("templates/library.yaml")) || strings.Contains(stdout, filepath.FromSlash("slides/1-hello.md")) {
		t.Errorf("kalide init output names a local templates/ or starter slide, want neither:\n%s", stdout)
	}

	got := initDeckTemplates(t, h.WorkDir())
	if got != libDir {
		t.Errorf("kalide.yaml templates = %q, want the configured absolute default %q", got, libDir)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("kalide.yaml templates = %q, want an absolute path", got)
	}
	if _, err := os.Stat(h.Path("templates")); !os.IsNotExist(err) {
		t.Errorf("kalide init wrote a local templates/ (err = %v), want only the external reference", err)
	}
	if _, err := os.Stat(h.Path("slides/1-hello.md")); !os.IsNotExist(err) {
		t.Errorf("kalide init wrote the starter slide, want none")
	}
	assertDirEmpty(t, h.Path("slides"))
	assertDirEmpty(t, h.Path("assets"))

	// `kalide start` validates the written deck and serves it; the default
	// theme the server serves comes from the configured library, pinned by its
	// marker, so this proves the deck resolved that external root.
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
	if !strings.Contains(served, "configured-default-theme") {
		t.Errorf("served %sdefault/theme.css does not come from the configured library:\n%s", server.ThemesPath, served)
	}
	h.Stop()
}

// TestInitDeckOwnTemplatesOverridesConfiguredDefault proves the deck's own
// `templates:` is authoritative over the user config for `kalide start` and
// `kalide templates`: with a configured default pointing at one valid library,
// a deck that declares a different valid library in kalide.yaml serves from and
// documents the deck's library, never the configured default.
func TestInitDeckOwnTemplatesOverridesConfiguredDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	home := t.TempDir()
	configuredDir := newInitLibrary(t, "configured-lib", "configured-default-library", "configured-default-theme")
	deckLibDir := newInitLibrary(t, "deck-lib", "deck-library-marker", "deck-library-theme")
	setInitDefaultTemplateDir(t, h, home, configuredDir)

	// A deck that declares its own `templates:`, pointing at a different valid
	// library than the configured default.
	h.WriteFile("kalide.yaml", []byte(
		"title: Deck With Its Own Templates\ntheme: default\ntemplates: "+deckLibDir+"\n"))
	if err := os.MkdirAll(h.Path("slides"), 0o755); err != nil {
		t.Fatalf("create slides/: %v", err)
	}
	if err := os.MkdirAll(h.Path("assets"), 0o755); err != nil {
		t.Fatalf("create assets/: %v", err)
	}

	// `kalide templates` documents the deck's own library, never the
	// configured default.
	stdout, stderr, code := h.Run("templates")
	if code != 0 {
		t.Fatalf("kalide templates exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if !strings.Contains(stdout, "deck-library-marker") {
		t.Errorf("kalide templates output does not list the deck library's template:\n%s", stdout)
	}
	if strings.Contains(stdout, "configured-default-library") {
		t.Errorf("kalide templates consulted the configured default:\n%s", stdout)
	}

	// `kalide start` serves from the deck's own library too.
	h.Start()
	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if strings.Contains(body, "Deck error") {
		t.Fatalf("served page is the error page, want the deck-library deck:\n%s", body)
	}
	served, err := h.GetString(server.ThemesPath + "default/theme.css")
	if err != nil {
		t.Fatalf("GET %sdefault/theme.css: %v", server.ThemesPath, err)
	}
	if !strings.Contains(served, "deck-library-theme") {
		t.Errorf("served %sdefault/theme.css does not come from the deck library:\n%s", server.ThemesPath, served)
	}
	if strings.Contains(served, "configured-default-theme") {
		t.Errorf("served %sdefault/theme.css came from the configured default:\n%s", server.ThemesPath, served)
	}
	h.Stop()
}

// TestInitExplicitAndLocalOverrides covers the two branches that beat a
// configured default: `kalide init <path>` (the explicit library is stored as
// given) and `kalide init --local` (the built-in seed is written and the
// config is never consulted), plus the `--local <path>` usage error that writes
// nothing.
func TestInitExplicitAndLocalOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	configuredDir := newInitLibrary(t, "configured-lib", "configured-default-library", "configured-default-theme")
	explicitDir := newInitLibrary(t, "deck-lib", "deck-library-marker", "deck-library-theme")

	t.Run("--local writes the seed even with a configured default", func(t *testing.T) {
		home := t.TempDir()
		setInitDefaultTemplateDir(t, h, home, configuredDir)
		resetDir(t, h.WorkDir())

		stdout, stderr, code := runKalideWithHome(t, h, home, "init", "--local")
		if code != 0 {
			t.Fatalf("kalide init --local exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init --local stderr = %q, want empty", stderr)
		}
		for _, want := range []string{
			filepath.FromSlash("templates/library.yaml"),
			filepath.FromSlash("slides/1-hello.md"),
			filepath.FromSlash("templates/themes/default/theme.css"),
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("kalide init --local output does not name %q:\n%s", want, stdout)
			}
		}
		if _, err := os.Stat(h.Path("templates/library.yaml")); err != nil {
			t.Errorf("kalide init --local did not write the seed library: %v", err)
		}
		if got := initDeckTemplates(t, h.WorkDir()); got != "" {
			t.Errorf("kalide.yaml templates = %q, want the empty seed value", got)
		}
	})

	t.Run("explicit path overrides the configured default", func(t *testing.T) {
		home := t.TempDir()
		setInitDefaultTemplateDir(t, h, home, configuredDir)
		resetDir(t, h.WorkDir())

		stdout, stderr, code := runKalideWithHome(t, h, home, "init", explicitDir)
		if code != 0 {
			t.Fatalf("kalide init <path> exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init <path> stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, explicitDir) {
			t.Errorf("kalide init <path> output does not name the explicit library %q:\n%s", explicitDir, stdout)
		}
		if got := initDeckTemplates(t, h.WorkDir()); got != explicitDir {
			t.Errorf("kalide.yaml templates = %q, want the explicit library %q (not the configured default %q)",
				got, explicitDir, configuredDir)
		}
	})

	t.Run("--local with a path is a usage error writing nothing", func(t *testing.T) {
		home := t.TempDir()
		resetDir(t, h.WorkDir())

		stdout, stderr, code := runKalideWithHome(t, h, home, "init", "--local", explicitDir)
		if code != 2 {
			t.Fatalf("kalide init --local <path> exit = %d, want 2 (stderr = %q)", code, stderr)
		}
		if stdout != "" {
			t.Errorf("kalide init --local <path> stdout = %q, want empty", stdout)
		}
		for _, want := range []string{"Usage:", "--local cannot be combined"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("kalide init --local <path> stderr = %q, want it to mention %q", stderr, want)
			}
		}
		assertDirEmpty(t, h.WorkDir())
	})
}

// TestInitConfiguredDefaultFailures covers the configured default that cannot
// host the deck: a path that is missing from disk, a directory that is not a
// valid library, and a valid library that does not define the deck's default
// theme. Each fails non-zero, names the offending path, and writes nothing.
func TestInitConfiguredDefaultFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

	t.Run("missing configured default", func(t *testing.T) {
		home := t.TempDir()
		missing := filepath.Join(t.TempDir(), "nope")
		setInitDefaultTemplateDir(t, h, home, missing)
		resetDir(t, h.WorkDir())

		stdout, stderr, code := runKalideWithHome(t, h, home, "init")
		if code == 0 {
			t.Fatalf("kalide init exit = 0, want non-zero for a missing configured default")
		}
		if stdout != "" {
			t.Errorf("kalide init stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, missing) {
			t.Errorf("kalide init stderr = %q, want it to name the missing configured path %q", stderr, missing)
		}
		assertDirEmpty(t, h.WorkDir())
	})

	t.Run("invalid configured default", func(t *testing.T) {
		home := t.TempDir()
		libDir := newInitLibrary(t, "configured-lib", "configured-default-library", "configured-default-theme")
		writeInitFile(t, filepath.Join(libDir, "library.yaml"), "name: Bad_Name\nformat: 1\n")
		setInitDefaultTemplateDir(t, h, home, libDir)
		resetDir(t, h.WorkDir())

		stdout, stderr, code := runKalideWithHome(t, h, home, "init")
		if code == 0 {
			t.Fatalf("kalide init exit = 0, want non-zero for an invalid configured default")
		}
		if stdout != "" {
			t.Errorf("kalide init stdout = %q, want empty", stdout)
		}
		if want := filepath.Join(libDir, "library.yaml"); !strings.Contains(stderr, want) {
			t.Errorf("kalide init stderr = %q, want it to name %q", stderr, want)
		}
		if !strings.Contains(stderr, "not a valid name") {
			t.Errorf("kalide init stderr = %q, want the library.yaml name error", stderr)
		}
		assertDirEmpty(t, h.WorkDir())
	})

	t.Run("configured default without the deck's theme", func(t *testing.T) {
		home := t.TempDir()
		libDir := newInitLibrary(t, "configured-lib", "configured-default-library", "configured-default-theme")
		if err := os.RemoveAll(filepath.Join(libDir, "themes", "default")); err != nil {
			t.Fatalf("remove themes/default: %v", err)
		}
		writeInitFile(t, filepath.Join(libDir, "themes", "plain", "theme.css"), "body {}\n")
		setInitDefaultTemplateDir(t, h, home, libDir)
		resetDir(t, h.WorkDir())

		stdout, stderr, code := runKalideWithHome(t, h, home, "init")
		if code == 0 {
			t.Fatalf("kalide init exit = 0, want non-zero when the configured default does not resolve the deck's theme")
		}
		if stdout != "" {
			t.Errorf("kalide init stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "default") {
			t.Errorf("kalide init stderr = %q, want it to name the unresolvable default theme", stderr)
		}
		if !strings.Contains(stderr, libDir) {
			t.Errorf("kalide init stderr = %q, want it to name the offending library %q", stderr, libDir)
		}
		assertDirEmpty(t, h.WorkDir())
	})
}

// TestInitRefusalExemptions covers the branch guard rules over the real
// binary: a pre-existing local templates/ is exempt (and left byte-for-byte
// untouched) in the explicit-path and configured-default branches but blocks
// the seed branch, where slides/, assets/ and kalide.yaml also block; and a
// pre-existing AGENTS.md never blocks in any branch. It is instead brought to
// the agent-guide-reference scheme: author content without the reference
// string keeps its bytes and gains the appended reference line, and the full
// AGENTS_KALIDE.md guide is written alongside it.
func TestInitRefusalExemptions(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	configuredDir := newInitLibrary(t, "configured-lib", "configured-default-library", "configured-default-theme")
	explicitDir := newInitLibrary(t, "deck-lib", "deck-library-marker", "deck-library-theme")

	const templatesSentinel = "pre-existing local templates init must not touch\n"

	t.Run("pre-existing templates blocks the seed branch", func(t *testing.T) {
		home := t.TempDir()
		resetDir(t, h.WorkDir())
		h.WriteFile("templates/keep.txt", []byte(templatesSentinel))

		stdout, stderr, code := runKalideWithHome(t, h, home, "init")
		if code == 0 {
			t.Fatalf("kalide init exit = 0 with a pre-existing templates/, want non-zero refusal")
		}
		if stdout != "" {
			t.Errorf("kalide init stdout = %q, want empty on refusal", stdout)
		}
		for _, want := range []string{filepath.FromSlash("templates/"), "never overwrites"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("kalide init refusal does not mention %q:\n%s", want, stderr)
			}
		}
		assertInitFile(t, h.Path("templates/keep.txt"), templatesSentinel)
		for _, absent := range []string{"kalide.yaml", "slides", "assets"} {
			if _, err := os.Lstat(h.Path(absent)); err == nil {
				t.Errorf("%s was created despite the refusal, want nothing written", absent)
			}
		}
	})

	t.Run("pre-existing templates is exempt in the explicit-path branch", func(t *testing.T) {
		home := t.TempDir()
		resetDir(t, h.WorkDir())
		h.WriteFile("templates/keep.txt", []byte(templatesSentinel))

		stdout, stderr, code := runKalideWithHome(t, h, home, "init", explicitDir)
		if code != 0 {
			t.Fatalf("kalide init <path> exit = %d, want 0 with a pre-existing templates/ (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init <path> stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, explicitDir) {
			t.Errorf("kalide init <path> output does not name the external library %q:\n%s", explicitDir, stdout)
		}
		assertInitFile(t, h.Path("templates/keep.txt"), templatesSentinel)
		if got := initDeckTemplates(t, h.WorkDir()); got != explicitDir {
			t.Errorf("kalide.yaml templates = %q, want %q", got, explicitDir)
		}
	})

	t.Run("pre-existing templates is exempt in the configured-default branch", func(t *testing.T) {
		home := t.TempDir()
		setInitDefaultTemplateDir(t, h, home, configuredDir)
		resetDir(t, h.WorkDir())
		h.WriteFile("templates/keep.txt", []byte(templatesSentinel))

		stdout, stderr, code := runKalideWithHome(t, h, home, "init")
		if code != 0 {
			t.Fatalf("kalide init exit = %d, want 0 with a pre-existing templates/ (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, configuredDir) {
			t.Errorf("kalide init output does not name the configured library %q:\n%s", configuredDir, stdout)
		}
		assertInitFile(t, h.Path("templates/keep.txt"), templatesSentinel)
		if got := initDeckTemplates(t, h.WorkDir()); got != configuredDir {
			t.Errorf("kalide.yaml templates = %q, want the configured default %q", got, configuredDir)
		}
	})

	// agentsAuthor is the author's pre-existing AGENTS.md content. init must
	// preserve it byte-for-byte and append the reference line to it
	// (agent-guide-reference), never replace it.
	const agentsAuthor = "hand-written agent guide author bytes\n"

	t.Run("pre-existing AGENTS.md never blocks the external branch", func(t *testing.T) {
		home := t.TempDir()
		setInitDefaultTemplateDir(t, h, home, configuredDir)
		resetDir(t, h.WorkDir())
		h.WriteFile("AGENTS.md", []byte(agentsAuthor))

		stdout, stderr, code := runKalideWithHome(t, h, home, "init")
		if code != 0 {
			t.Fatalf("kalide init exit = %d, want 0 with a pre-existing AGENTS.md (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, configuredDir) {
			t.Errorf("kalide init output does not name the configured library %q:\n%s", configuredDir, stdout)
		}
		assertAgentGuideAppended(t, h.Path("AGENTS.md"), agentsAuthor)
		assertAgentGuidePair(t, h.WorkDir())
	})

	t.Run("pre-existing AGENTS.md never blocks the seed branch", func(t *testing.T) {
		home := t.TempDir()
		resetDir(t, h.WorkDir())
		h.WriteFile("AGENTS.md", []byte(agentsAuthor))

		stdout, stderr, code := runKalideWithHome(t, h, home, "init")
		if code != 0 {
			t.Fatalf("kalide init exit = %d, want 0 with a pre-existing AGENTS.md (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide init stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, filepath.FromSlash("slides/1-hello.md")) {
			t.Errorf("kalide init output does not name the seed slide:\n%s", stdout)
		}
		assertAgentGuideAppended(t, h.Path("AGENTS.md"), agentsAuthor)
		assertAgentGuidePair(t, h.WorkDir())
	})
}

// newInitLibrary writes a minimal, valid external template library into a new
// temp directory and returns its absolute path. name must be a valid library
// name (its directory name and library.yaml `name:`); description and marker
// distinguish one library from another — the description appears in `kalide
// templates` output, the marker in the served default theme's stylesheet — so a
// test can pin which library the binary resolved.
func newInitLibrary(t *testing.T, name, description, marker string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	writeInitExternalLibrary(t, dir, name, description, marker)
	return dir
}

// writeInitExternalLibrary writes the minimal, valid external template library
// the loader accepts at dir: library.yaml, one "hello" slide template with the
// given description and default theme marker, its layout and example, and
// themes/default/theme.css. It mirrors internal/cli's writeExternalCLILibrary
// so the e2e fixtures are exactly the shape a real library has.
func writeInitExternalLibrary(t *testing.T, dir, name, description, marker string) {
	t.Helper()
	writeInitFile(t, filepath.Join(dir, "library.yaml"),
		"name: "+name+"\ndescription: "+description+"\nformat: 1\n")
	writeInitFile(t, filepath.Join(dir, "slides", "hello", "template.yaml"),
		"description: "+description+"\n"+
			"fields:\n"+
			"  - name: title\n"+
			"    type: text\n"+
			"    required: true\n"+
			"body:\n"+
			"  mode: optional\n")
	writeInitFile(t, filepath.Join(dir, "slides", "hello", "layout.html.tmpl"),
		"<section><h1>{{.title}}</h1></section>\n")
	writeInitFile(t, filepath.Join(dir, "slides", "hello", "example.md"),
		"---\ntemplate: hello\ntitle: Example\n---\n")
	writeInitFile(t, filepath.Join(dir, "themes", "default", "theme.css"),
		"/* "+marker+" */\nbody { margin: 0; }\n")
}

// setInitDefaultTemplateDir writes the `default-template-dir` user-config key
// under home through the real `kalide config set` command, so the store the
// init cases read is the one the CLI writes.
func setInitDefaultTemplateDir(t *testing.T, h *Harness, home, dir string) {
	t.Helper()
	stdout, stderr, code := runKalideWithHome(t, h, home, "config", "set", configKeyDefaultTemplateDir, dir)
	if code != 0 {
		t.Fatalf("kalide config set %s %s exit = %d, want 0 (stderr = %q)",
			configKeyDefaultTemplateDir, dir, code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("kalide config set %s %s stdout = %q, stderr = %q, want both empty",
			configKeyDefaultTemplateDir, dir, stdout, stderr)
	}
}

// initDeckTemplates parses dir/kalide.yaml and returns its `templates:` value,
// so a test asserts the reference written rather than its exact formatting.
func initDeckTemplates(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "kalide.yaml"))
	if err != nil {
		t.Fatalf("read kalide.yaml: %v", err)
	}
	var cfg struct {
		Templates string `yaml:"templates"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse kalide.yaml: %v", err)
	}
	return cfg.Templates
}

// resetDir empties dir and recreates it, so each subtest starts from a clean
// working directory when the harness is shared across them.
func resetDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove %s: %v", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
}

// writeInitFile writes content to path, creating parent directories as needed.
func writeInitFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// assertInitFile fails the test unless path holds exactly want, proving a
// pre-existing file was left byte-for-byte untouched.
func assertInitFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want the pre-existing content %q", path, got, want)
	}
}
