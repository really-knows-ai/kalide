package scaffold

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/assets"
	"github.com/really-knows-ai/ey-present/internal/deck"
	"github.com/really-knows-ai/ey-present/internal/template"
	"github.com/really-knows-ai/ey-present/internal/validate"
)

// TestInit covers internal/scaffold.Init on real temporary directories
// (t.TempDir): a clean directory receives the embedded starter deck verbatim
// plus an empty assets/; any of the three deck-owned paths already present makes
// Init refuse, name every blocker and write nothing, whether it is a file or a
// directory; unrelated entries (.git, a README, a lock file) never block; and
// the scaffolded deck passes the deck loaders and whole-deck validation with
// zero errors.
func TestInit(t *testing.T) {
	t.Run("clean directory creates the starter deck", testInitClean)
	t.Run("existing block path refuses and writes nothing", testInitRefuses)
	t.Run("multiple block paths are all named", testInitNamesAllConflicts)
	t.Run("unrelated entries do not block", testInitUnrelated)
	t.Run("scaffolded deck passes load and whole-deck validation", testInitScaffoldValidates)
}

// testInitClean asserts Init on a fresh directory writes exactly the embedded
// starter tree: slides/ with the two starter slides, eypres.yaml, and an empty
// assets/ directory.
func testInitClean(t *testing.T) {
	dir := t.TempDir()

	if err := Init(dir); err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}

	assertFileEqualsStarter(t, dir, "slides/1-title.md")
	assertFileEqualsStarter(t, dir, "slides/2-content.md")
	assertFileEqualsStarter(t, dir, "eypres.yaml")

	entries, err := os.ReadDir(filepath.Join(dir, "assets"))
	if err != nil {
		t.Fatalf("ReadDir(assets/): %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("assets/ contains %d entries, want an empty directory", len(entries))
	}

	want := []string{"assets", "eypres.yaml", "slides", "slides/1-title.md", "slides/2-content.md"}
	if got := listTree(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("scaffolded tree = %v, want %v", got, want)
	}
}

// testInitRefuses asserts that each of slides/, assets/ and eypres.yaml, present
// as either a file or a directory, makes Init fail naming that path and that the
// directory is byte-for-byte unchanged (no partial deck, no overwrite).
func testInitRefuses(t *testing.T) {
	cases := []struct {
		name    string
		block   string // path pre-created under the target directory
		isDir   bool   // whether the block is a directory
		display string // the name Init's message must carry
	}{
		{"existing slides/ directory", "slides", true, "slides/"},
		{"existing slides file", "slides", false, "slides/"},
		{"existing assets/ directory", "assets", true, "assets/"},
		{"existing assets file", "assets", false, "assets/"},
		{"existing eypres.yaml file", "eypres.yaml", false, "eypres.yaml"},
		{"existing eypres.yaml directory", "eypres.yaml", true, "eypres.yaml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			block := filepath.Join(dir, tc.block)
			if tc.isDir {
				mkdir(t, block)
				writeFile(t, filepath.Join(block, "keep.txt"), "keep")
			} else {
				writeFile(t, block, "sentinel")
			}

			before := snapshot(t, dir)

			err := Init(dir)
			if err == nil {
				t.Fatalf("Init() error = nil, want a refusal naming %q", tc.display)
			}
			if !strings.Contains(err.Error(), tc.display) {
				t.Fatalf("Init() error = %q, want it to name %q", err, tc.display)
			}

			if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
				t.Fatalf("Init() modified the directory on refusal:\n before %v\n after  %v", before, after)
			}
		})
	}
}

// testInitNamesAllConflicts asserts one refusal names every blocking path, in
// the fixed slides/, assets/, eypres.yaml order, and still writes nothing.
func testInitNamesAllConflicts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "eypres.yaml"), "title: mine\n")
	mkdir(t, filepath.Join(dir, "slides"))
	mkdir(t, filepath.Join(dir, "assets"))

	before := snapshot(t, dir)

	err := Init(dir)
	if err == nil {
		t.Fatal("Init() error = nil, want a refusal naming all three blocking paths")
	}
	msg := err.Error()
	for _, want := range []string{"slides/", "assets/", "eypres.yaml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Init() error = %q, want it to name %q", msg, want)
		}
	}

	si := strings.Index(msg, "slides/")
	ai := strings.Index(msg, "assets/")
	yi := strings.Index(msg, "eypres.yaml")
	if si < 0 || ai < 0 || yi < 0 || !(si < ai && ai < yi) {
		t.Errorf("Init() error = %q, want all paths in order slides/, assets/, eypres.yaml", msg)
	}

	if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("Init() modified the directory on refusal:\n before %v\n after  %v", before, after)
	}
}

// testInitUnrelated asserts entries that are not part of a deck — .git, a
// README, an editor lock file — do not block Init and are left untouched.
func testInitUnrelated(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, ".git"))
	writeFile(t, filepath.Join(dir, ".git", "config"), "[core]\n")
	writeFile(t, filepath.Join(dir, "README.md"), "# notes\n")
	writeFile(t, filepath.Join(dir, ".eypres.lock"), "lock\n")

	if err := Init(dir); err != nil {
		t.Fatalf("Init() error = %v, want nil (unrelated entries must not block)", err)
	}

	if got := readString(t, filepath.Join(dir, "README.md")); got != "# notes\n" {
		t.Errorf("README.md = %q, want it unchanged", got)
	}
	if got := readString(t, filepath.Join(dir, ".git", "config")); got != "[core]\n" {
		t.Errorf(".git/config = %q, want it unchanged", got)
	}
	for _, name := range []string{"slides/1-title.md", "slides/2-content.md", "eypres.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Errorf("expected %s to exist after Init: %v", name, err)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "assets")); err != nil || !info.IsDir() {
		t.Errorf("expected assets/ to be a directory after Init, stat err = %v", err)
	}
}

// testInitScaffoldValidates loads the scaffolded deck through internal/deck
// (LoadConfig + LoadSlides) and runs the whole-deck validator with the
// compiled-in template registry, requiring a valid deck and the zero
// ValidationError.
func testInitScaffoldValidates(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	fsys := os.DirFS(dir)

	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile)
	if err != nil {
		t.Fatalf("deck.LoadConfig() error = %v", err)
	}
	if strings.TrimSpace(cfg.Title) == "" {
		t.Error("scaffolded config title is empty, want a non-empty title")
	}
	if _, err := deck.LoadSlides(fsys, deck.SlidesDir); err != nil {
		t.Fatalf("deck.LoadSlides() error = %v", err)
	}

	reg, err := template.Builtins()
	if err != nil {
		t.Fatalf("template.Builtins() error = %v", err)
	}
	verr, invalid := validate.Validate(fsys, reg, nil)
	if invalid {
		t.Fatalf("validate.Validate() invalid = true with %q, want a valid deck", validate.Format(verr))
	}
	if !reflect.DeepEqual(verr, validate.ValidationError{}) {
		t.Fatalf("validate.Validate() error = %#v, want the zero ValidationError", verr)
	}
}

// assertFileEqualsStarter reads name from the embedded starter tree and asserts
// the scaffolded file is non-empty and byte-for-byte identical to it.
func assertFileEqualsStarter(t *testing.T, dir, name string) {
	t.Helper()

	want, err := fs.ReadFile(assets.Starter(), name)
	if err != nil {
		t.Fatalf("read embedded starter %s: %v", name, err)
	}
	if len(want) == 0 {
		t.Fatalf("embedded starter %s is empty", name)
	}

	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read scaffolded %s: %v", name, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scaffolded %s differs from the embedded starter:\n got %q\nwant %q", name, got, want)
	}
}

// listTree returns the sorted deck-relative slash paths of every entry under
// dir, directories included.
func listTree(t *testing.T, dir string) []string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

// snapshot maps every deck-relative slash path under dir to its file contents,
// or "<dir>" for a directory. Comparing two snapshots detects any write.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()

	snap := make(map[string]string)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			snap[rel] = "<dir>"
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		snap[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return snap
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
