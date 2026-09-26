package e2e

// This file is the phase-8 task-9 end-to-end test for the init flow:
// `eypres init` scaffolding the embedded hello seed. It drives the real
// eypres binary the harness builds (harness.go) in a clean, empty working
// directory, so it exercises the path an author takes right after
// installing eypres:
//
//   - `eypres init` writes the embedded hello seed, prints the paths it
//     created and exits 0;
//   - every printed path exists on disk, byte-for-byte the embedded seed:
//     eypres.yaml, slides/1-hello.md, templates/library.yaml, the hello
//     slide template's three files, the default theme's stylesheet, and an
//     empty assets/ directory;
//   - the written seed loads and validates cleanly through
//     template.LoadLibrary and template.NewRegistryFromLibrary, exactly as
//     `eypres start` would load it;
//   - a second `eypres init` in the same directory refuses with a
//     non-zero exit, prints the refusal, and leaves every file
//     byte-for-byte unchanged.
//
// `eypres start` serving the hello seed is deliberately out of scope here:
// start still validates against the builtin (EY) starter content until
// phase 3 wires template-backed rendering through it, so a start/serve
// assertion on the hello seed belongs to that later phase, not this one.
//
// The flow needs no network. Offline enforcement (blocking network syscalls) is
// phase 9 and is deliberately out of scope here too.
//
// It builds a binary, so it is skipped under -short.

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/template"
)

// initCreatedPaths are the deck-relative paths `eypres init` reports creating;
// they mirror internal/cli's initCreatedMessage and the embedded hello seed
// in internal/scaffold/seed. They are asserted against both the command
// output and the filesystem.
var initCreatedPaths = []string{
	"eypres.yaml",
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
		t.Skip("e2e test builds and drives the real eypres binary")
	}

	h := NewHarness(t)

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
	// `eypres start` uses: template.LoadLibrary and, inside it,
	// template.NewRegistryFromLibrary.
	assertSeedLoads(t, h.WorkDir())

	// Snapshot the scaffolded tree so the second init can be proven not to have
	// touched it.
	before := snapshotTree(t, h.WorkDir())

	// A second `eypres init` in the same directory refuses: non-zero exit, the
	// refusal message naming the blocking paths, and nothing written.
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
// exactly as `eypres start` loads a deck's templates/ library.
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
