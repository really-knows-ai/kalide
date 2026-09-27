package e2e

// This file is the phase-05 task-2 end-to-end test for `kalide init-library`
// (requirements.requirement.init-library). It drives the real kalide binary the
// harness builds (harness.go) and asserts the command surface an author sees,
// not scaffold's internals:
//
//   - `kalide init-library my-lib` scaffolds the deck-ready empty library and
//     reports the created paths: library.yaml whose name is the target base
//     name and whose format is 1, the empty slides/, sections/ and media/
//     directories and themes/default/theme.css;
//   - the created library loads and validates cleanly through the same
//     template loader path `kalide start` uses, so a deck whose theme defaults
//     to `default` resolves in it;
//   - a second `kalide init-library my-lib` refuses to clobber the existing
//     library (non-zero exit, naming the blocking entry) and changes nothing;
//   - an invalid target base name and a target that exists as a non-directory
//     are both refused with a clear message and nothing written;
//   - a missing <path> is a usage error (usage to stderr, exit 2).
//
// It builds a binary, so it is skipped under -short.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/template"
)

// TestInitLibrary is the end-to-end init-library test described above.
func TestInitLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds the real kalide binary")
	}

	h := NewHarness(t)

	// A fresh target scaffolds the deck-ready empty library and reports the
	// created paths.
	stdout, stderr, code := h.Run("init-library", "my-lib")
	if code != 0 {
		t.Fatalf("kalide init-library my-lib exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide init-library my-lib stderr = %q, want empty", stderr)
	}
	for _, want := range []string{"my-lib", "library.yaml", "slides/", "sections/", "media/", "themes/default/theme.css"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("kalide init-library output does not name %q:\n%s", want, stdout)
		}
	}

	libDir := h.Path("my-lib")
	name, format := readLibraryMeta(t, filepath.Join(libDir, "library.yaml"))
	if name != "my-lib" {
		t.Errorf("library.yaml name = %q, want %q (the target base name)", name, "my-lib")
	}
	if format != 1 {
		t.Errorf("library.yaml format = %d, want 1", format)
	}
	for _, dir := range []string{"slides", "sections", "media"} {
		assertDirEmpty(t, filepath.Join(libDir, dir))
	}
	stylePath := filepath.Join(libDir, "themes", "default", "theme.css")
	css, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatalf("read %s: %v", stylePath, err)
	}
	if len(css) == 0 {
		t.Error("themes/default/theme.css is empty, want the minimal default theme")
	}

	// The scaffolded library loads cleanly through the template loader, so a
	// deck whose theme defaults to `default` resolves in it.
	assertLibraryLoads(t, libDir)

	// Clobbering refusal: a second `kalide init-library my-lib` exits non-zero
	// naming the existing library and changes nothing.
	before := snapshotTree(t, libDir)
	stdout, stderr, code = h.Run("init-library", "my-lib")
	if code == 0 {
		t.Fatalf("second kalide init-library my-lib exit = 0, want non-zero (stdout = %q)", stdout)
	}
	if stdout != "" {
		t.Errorf("second kalide init-library stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"init-library:", "library.yaml", "already present", "never overwrites"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("second kalide init-library refusal does not mention %q:\n%s", want, stderr)
		}
	}
	after := snapshotTree(t, libDir)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("second kalide init-library modified the library:\nbefore = %v\nafter  = %v", before, after)
	}

	// An invalid target base name is refused: non-zero exit, the offending
	// name in the message, and nothing written.
	stdout, stderr, code = h.Run("init-library", "My Lib")
	if code == 0 {
		t.Fatalf("kalide init-library \"My Lib\" exit = 0, want non-zero (stdout = %q)", stdout)
	}
	if stdout != "" {
		t.Errorf("kalide init-library \"My Lib\" stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "not a valid library name") || !strings.Contains(stderr, "My Lib") {
		t.Errorf("kalide init-library \"My Lib\" stderr = %q, want an invalid-name error naming the name", stderr)
	}
	if _, err := os.Stat(h.Path("My Lib")); !os.IsNotExist(err) {
		t.Errorf("kalide init-library \"My Lib\" wrote the target (err = %v), want nothing written", err)
	}

	// A target that exists as a non-directory is refused and left untouched.
	const sentinel = "i am a file, not a library directory\n"
	h.WriteFile("not-a-dir", []byte(sentinel))
	stdout, stderr, code = h.Run("init-library", "not-a-dir")
	if code == 0 {
		t.Fatalf("kalide init-library not-a-dir exit = 0, want non-zero (stdout = %q)", stdout)
	}
	if stdout != "" {
		t.Errorf("kalide init-library not-a-dir stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "is not a directory") {
		t.Errorf("kalide init-library not-a-dir stderr = %q, want a non-directory error", stderr)
	}
	if got, err := os.ReadFile(h.Path("not-a-dir")); err != nil || string(got) != sentinel {
		t.Errorf("not-a-dir = %q, err = %v; want it unchanged", got, err)
	}

	// A missing <path> is a usage error: exit 2, usage to stderr, nothing
	// written.
	stdout, stderr, code = h.Run("init-library")
	if code != 2 {
		t.Fatalf("kalide init-library (no path) exit = %d, want 2 (stderr = %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("kalide init-library (no path) stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "expected exactly one library path") {
		t.Errorf("kalide init-library (no path) stderr = %q, want an argument-count error", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("kalide init-library (no path) stderr = %q, want usage text", stderr)
	}
}

// readLibraryMeta parses dir/library.yaml and returns its name and format, so
// the test asserts the written configuration rather than its exact formatting.
func readLibraryMeta(t *testing.T, path string) (name string, format int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg struct {
		Name   string `yaml:"name"`
		Format int    `yaml:"format"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cfg.Name, cfg.Format
}

// assertLibraryLoads asserts that dir loads and validates cleanly through
// template.LoadLibrary and template.NewRegistryFromLibrary, exactly as
// `kalide start` loads a deck's resolved template library.
func assertLibraryLoads(t *testing.T, dir string) {
	t.Helper()

	lib, err := template.LoadLibrary(os.DirFS(dir), ".")
	if err != nil {
		t.Fatalf("template.LoadLibrary(%s, %q) = %v, want a valid library", dir, ".", err)
	}
	if _, err := template.NewRegistryFromLibrary(lib); err != nil {
		t.Fatalf("template.NewRegistryFromLibrary(%s's library) = %v, want a valid registry", dir, err)
	}
}
