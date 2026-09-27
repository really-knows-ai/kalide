package scaffold

// This file unit-tests the init-library scaffold writers (phase-04 task-7):
// requirements.requirement.init-library.
//
// It exercises scaffold.InitLibrary and DefaultThemeCSS on real temporary
// directories: a fresh target receives the deck-ready layout — library.yaml
// whose name is the target's base name and whose format is 1, the empty
// slides/, sections/ and media/ directories and themes/default/theme.css;
// an invalid base name is a hard error writing nothing; a pre-existing
// library.yaml or layout entry refuses and writes nothing; a non-directory
// target is an error leaving it untouched; unrelated pre-existing entries
// never block and are left untouched; and the created library loads cleanly
// through the template loader (template.LoadLibrary / template.LoadDeckLibrary)
// so a deck whose theme defaults to `default` resolves in it. Every case runs
// in a t.TempDir, so the package directory is never written to.

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// libraryTree is the exact set of library-relative slash paths InitLibrary must
// produce for a fresh target: the metadata file, the three empty content
// directories and the one default theme with its stylesheet. It is deliberately
// minimal — no starter template, no section, no media file, and no nested
// templates/ entry (a library's root IS the templates directory).
var libraryTree = []string{
	"library.yaml",
	"media",
	"sections",
	"slides",
	"themes",
	"themes/default",
	"themes/default/theme.css",
}

// TestInitLibrary covers scaffold.InitLibrary and scaffold.DefaultThemeCSS: the
// fresh layout, the invalid-base-name hard error, the refusal matrix, the
// non-directory target, unrelated entries, a clean load through the template
// loader, and the embedded default theme.
func TestInitLibrary(t *testing.T) {
	t.Run("fresh target creates the deck-ready layout", testInitLibraryFresh)
	t.Run("invalid base names are hard errors writing nothing", testInitLibraryInvalidNames)
	t.Run("refusal matrix blocks every library-owned entry", testInitLibraryRefuses)
	t.Run("multiple blocking entries are all named in order", testInitLibraryNamesAllConflicts)
	t.Run("non-directory target is an error leaving it untouched", testInitLibraryNonDirectory)
	t.Run("unrelated pre-existing entries are left untouched", testInitLibraryUnrelated)
	t.Run("created library loads cleanly through the template loader", testInitLibraryLoads)
	t.Run("DefaultThemeCSS is the embedded default theme", testDefaultThemeCSS)
}

// testInitLibraryFresh asserts InitLibrary on a fresh dir writes exactly
// libraryTree: library.yaml with name = the target base name and format 1 (no
// description), the three empty content directories and
// themes/default/theme.css carrying DefaultThemeCSS.
func testInitLibraryFresh(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "my-lib")

	if err := InitLibrary(dir); err != nil {
		t.Fatalf("InitLibrary(%s) error = %v, want nil", dir, err)
	}

	name, description, format := readLibraryConfig(t, dir)
	if name != "my-lib" {
		t.Errorf("library.yaml name = %q, want %q (the target base name)", name, "my-lib")
	}
	if format != 1 {
		t.Errorf("library.yaml format = %d, want 1", format)
	}
	if description != "" {
		t.Errorf("library.yaml description = %q, want empty (the key is omitted)", description)
	}

	for _, layout := range []string{template.SlidesDir, template.SectionsDir, template.MediaDir} {
		assertLibraryDirEmpty(t, filepath.Join(dir, layout))
	}

	style := filepath.Join(dir, template.ThemesDir, theme.DefaultName, template.ThemeStylesheet)
	if got := readString(t, style); got != DefaultThemeCSS() {
		t.Errorf("themes/default/theme.css = %q, want DefaultThemeCSS", got)
	}
	if DefaultThemeCSS() == "" {
		t.Error("DefaultThemeCSS is empty, want the minimal default theme")
	}

	if got := listTree(t, dir); !reflect.DeepEqual(got, libraryTree) {
		t.Fatalf("scaffolded library tree = %v, want %v", got, libraryTree)
	}
}

// testInitLibraryInvalidNames asserts a target whose base name does not match
// [a-z0-9][a-z0-9-]* is a hard error naming the offending base name, writing
// nothing (the target is never created and the parent stays empty).
func testInitLibraryInvalidNames(t *testing.T) {
	for _, name := range []string{"My Lib", "My-Lib", "LIB", "-lib", "lib_", "lib.v1"} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, name)

			err := InitLibrary(dir)
			if err == nil {
				t.Fatalf("InitLibrary(%s) error = nil, want a base-name error", dir)
			}
			if !strings.Contains(err.Error(), "not a valid library name") {
				t.Errorf("InitLibrary(%s) error = %q, want it to reject the base name", dir, err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("InitLibrary(%s) error = %q, want it to name %q", dir, err, name)
			}

			assertNothingWritten(t, dir)
			entries, rerr := os.ReadDir(parent)
			if rerr != nil {
				t.Fatalf("ReadDir(%s): %v", parent, rerr)
			}
			if len(entries) != 0 {
				t.Errorf("parent contains %v after an invalid-name error, want nothing written", entries)
			}
		})
	}
}

// testInitLibraryRefuses asserts each library-owned path — library.yaml and the
// four layout entries slides/, sections/, themes/, media/ — present as either a
// file or a directory, makes InitLibrary refuse naming that path and leaves the
// target byte-for-byte unchanged.
func testInitLibraryRefuses(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		isDir   bool
		display string
	}{
		{"existing library.yaml file", template.LibraryFile, false, template.LibraryFile},
		{"existing library.yaml directory", template.LibraryFile, true, template.LibraryFile},
		{"existing slides directory", template.SlidesDir, true, template.SlidesDir + "/"},
		{"existing slides file", template.SlidesDir, false, template.SlidesDir + "/"},
		{"existing sections directory", template.SectionsDir, true, template.SectionsDir + "/"},
		{"existing sections file", template.SectionsDir, false, template.SectionsDir + "/"},
		{"existing themes directory", template.ThemesDir, true, template.ThemesDir + "/"},
		{"existing themes file", template.ThemesDir, false, template.ThemesDir + "/"},
		{"existing media directory", template.MediaDir, true, template.MediaDir + "/"},
		{"existing media file", template.MediaDir, false, template.MediaDir + "/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "my-lib")
			mkdir(t, dir)
			block := filepath.Join(dir, tc.entry)
			if tc.isDir {
				mkdir(t, block)
				writeFile(t, filepath.Join(block, "keep.txt"), "keep")
			} else {
				writeFile(t, block, "sentinel")
			}

			before := snapshot(t, dir)

			err := InitLibrary(dir)
			if err == nil {
				t.Fatalf("InitLibrary(%s) error = nil, want a refusal naming %q", dir, tc.display)
			}
			if !strings.Contains(err.Error(), tc.display) {
				t.Errorf("InitLibrary(%s) error = %q, want it to name %q", dir, err, tc.display)
			}
			if !strings.Contains(err.Error(), "never overwrites") {
				t.Errorf("InitLibrary(%s) error = %q, want the never-overwrites refusal", dir, err)
			}

			if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
				t.Fatalf("InitLibrary modified the target on refusal:\n before %v\n after  %v", before, after)
			}
		})
	}
}

// testInitLibraryNamesAllConflicts asserts one refusal names every blocking
// entry, in the fixed library.yaml, slides/, sections/, themes/, media/ order,
// and still writes nothing.
func testInitLibraryNamesAllConflicts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-lib")
	mkdir(t, dir)
	writeFile(t, filepath.Join(dir, template.LibraryFile), "name: other\nformat: 1\n")
	for _, d := range []string{template.SlidesDir, template.SectionsDir, template.ThemesDir, template.MediaDir} {
		mkdir(t, filepath.Join(dir, d))
	}

	before := snapshot(t, dir)

	err := InitLibrary(dir)
	if err == nil {
		t.Fatal("InitLibrary() error = nil, want a refusal naming every blocking entry")
	}
	msg := err.Error()
	want := []string{template.LibraryFile, template.SlidesDir + "/", template.SectionsDir + "/", template.ThemesDir + "/", template.MediaDir + "/"}
	for _, w := range want {
		if !strings.Contains(msg, w) {
			t.Errorf("InitLibrary() error = %q, want it to name %q", msg, w)
		}
	}
	last := -1
	for _, w := range want {
		i := strings.Index(msg, w)
		if i < 0 || i <= last {
			t.Errorf("InitLibrary() error = %q, want %q in the fixed order %v", msg, w, want)
			break
		}
		last = i
	}

	if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("InitLibrary modified the target on refusal:\n before %v\n after  %v", before, after)
	}
}

// testInitLibraryNonDirectory asserts a target that exists as a non-directory
// is an error naming the target, and that the file is left byte-for-byte
// unchanged.
func testInitLibraryNonDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "my-lib")
	const sentinel = "i am a file, not a library directory\n"
	writeFile(t, dir, sentinel)

	before := snapshot(t, parent)

	err := InitLibrary(dir)
	if err == nil {
		t.Fatalf("InitLibrary(%s) error = nil, want a non-directory error", dir)
	}
	if !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("InitLibrary(%s) error = %q, want it to report a non-directory target", dir, err)
	}
	if got := readString(t, dir); got != sentinel {
		t.Errorf("target file = %q, want it unchanged", got)
	}

	if after := snapshot(t, parent); !reflect.DeepEqual(after, before) {
		t.Fatalf("InitLibrary modified the parent on a non-directory target:\n before %v\n after  %v", before, after)
	}
}

// testInitLibraryUnrelated asserts entries that are not part of a library —
// .git/, a README, an editor lock file — do not block InitLibrary and are left
// untouched while the library is written around them.
func testInitLibraryUnrelated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-lib")
	mkdir(t, dir)

	unrelated := map[string]string{
		"README.md":                 "# notes\n",
		".kalide.lock":              "lock\n",
		path.Join(".git", "config"): "[core]\n",
	}
	for rel, content := range unrelated {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		writeFile(t, p, content)
	}

	if err := InitLibrary(dir); err != nil {
		t.Fatalf("InitLibrary(%s) error = %v, want nil (unrelated entries must not block)", dir, err)
	}

	for rel, want := range unrelated {
		if got := readString(t, filepath.Join(dir, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s = %q, want it unchanged", rel, got)
		}
	}
	for _, rel := range libraryTree {
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s after InitLibrary: %v", rel, err)
		}
	}
}

// testInitLibraryLoads asserts the freshly created library passes the loader:
// template.LoadLibrary validates an empty library cleanly and exposes the base
// name, format 1, the single default theme and media/, and the deck-level
// resolver reaches the same directory from the parent via the base name.
func testInitLibraryLoads(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "my-lib")
	if err := InitLibrary(dir); err != nil {
		t.Fatalf("InitLibrary(%s) error = %v", dir, err)
	}

	lib, err := template.LoadLibrary(os.DirFS(dir), ".")
	if err != nil {
		t.Fatalf("template.LoadLibrary() error = %v, want the created library to load cleanly", err)
	}
	if lib.Meta.Name != "my-lib" {
		t.Errorf("loaded library name = %q, want %q", lib.Meta.Name, "my-lib")
	}
	if lib.Meta.Format != 1 {
		t.Errorf("loaded library format = %d, want 1", lib.Meta.Format)
	}
	if lib.Meta.Description != "" {
		t.Errorf("loaded library description = %q, want empty", lib.Meta.Description)
	}
	if got, want := lib.ThemeNames(), []string{theme.DefaultName}; !reflect.DeepEqual(got, want) {
		t.Errorf("loaded theme names = %v, want %v", got, want)
	}
	if got, ok := lib.Themes[theme.DefaultName]; !ok {
		t.Errorf("loaded library has no %q theme", theme.DefaultName)
	} else if string(got.StylesheetBytes) != DefaultThemeCSS() {
		t.Errorf("loaded default theme stylesheet differs from DefaultThemeCSS")
	}
	if len(lib.Slides) != 0 || len(lib.Sections) != 0 {
		t.Errorf("loaded library has %d slide and %d section templates, want none", len(lib.Slides), len(lib.Sections))
	}
	if !lib.HasMedia {
		t.Error("loaded library HasMedia = false, want the empty media/ present")
	}

	root, err := template.ResolveLibraryRoot(parent, "my-lib")
	if err != nil {
		t.Errorf("template.ResolveLibraryRoot(%s, my-lib) error = %v", parent, err)
	} else if root != dir {
		t.Errorf("template.ResolveLibraryRoot() = %q, want %q", root, dir)
	}
	if _, err := template.LoadDeckLibrary(parent, "my-lib"); err != nil {
		t.Errorf("template.LoadDeckLibrary(%s, my-lib) error = %v, want the created library to load", parent, err)
	}
}

// testDefaultThemeCSS asserts DefaultThemeCSS is exactly the embedded no-arg
// `kalide init` seed's default theme, so the two cannot drift and a deck whose
// theme defaults to `default` resolves in a fresh library as it does in a fresh
// deck.
func testDefaultThemeCSS(t *testing.T) {
	want, err := fs.ReadFile(seedRoot, path.Join(
		template.TemplatesDir, template.ThemesDir, theme.DefaultName, template.ThemeStylesheet))
	if err != nil {
		t.Fatalf("read embedded seed default theme: %v", err)
	}
	if len(want) == 0 {
		t.Fatal("embedded seed default theme is empty")
	}
	if DefaultThemeCSS() != string(want) {
		t.Errorf("DefaultThemeCSS differs from the embedded seed's default theme:\n got %q\nwant %q",
			DefaultThemeCSS(), want)
	}
}

// readLibraryConfig parses dir/library.yaml and returns its name, description
// and format, so a test asserts the written configuration rather than its exact
// formatting.
func readLibraryConfig(t *testing.T, dir string) (name, description string, format int) {
	t.Helper()

	data := readString(t, filepath.Join(dir, template.LibraryFile))
	var cfg struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Format      int    `yaml:"format"`
	}
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		t.Fatalf("parse %s: %v", template.LibraryFile, err)
	}
	return cfg.Name, cfg.Description, cfg.Format
}

// assertLibraryDirEmpty asserts dir exists, is a directory, and contains no
// entries.
func assertLibraryDirEmpty(t *testing.T, dir string) {
	t.Helper()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v, want a directory", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("%s contains %v, want an empty directory", dir, entries)
	}
}
