package scaffold

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// TestInit covers internal/scaffold.Init on real temporary directories
// (t.TempDir): a clean directory receives the embedded hello seed verbatim
// plus an empty assets/; any of the four deck-owned paths already present
// makes Init refuse, name every blocker and write nothing, whether it is a
// file or a directory; unrelated entries (.git, a README, a lock file) never
// block; and the scaffolded deck passes the deck loaders and whole-deck
// validation with zero errors, using the registry built from the seed's own
// templates/ library (template.LoadLibrary + template.NewRegistryFromLibrary)
// rather than the Go-builtins registry, which has no "hello" template. The seed
// includes the deck-root AGENTS.md guide (agent-authoring), written verbatim
// and byte-for-byte; a pre-existing AGENTS.md never blocks and is left
// untouched.
func TestInit(t *testing.T) {
	t.Run("clean directory creates the starter deck", testInitClean)
	t.Run("existing block path refuses and writes nothing", testInitRefuses)
	t.Run("multiple block paths are all named", testInitNamesAllConflicts)
	t.Run("unrelated entries do not block", testInitUnrelated)
	t.Run("existing eypres.yaml does not block init and init never writes eypres.yaml", testInitExistingEypresYAML)
	t.Run("scaffolded deck passes load and whole-deck validation", testInitScaffoldValidates)
	t.Run("pre-existing AGENTS.md is left untouched", testInitPreservesExistingAgentsGuide)
	t.Run("seed carries no EY branding", testSeedHasNoEYReferences)
}

// wantSeedTree is the exact set of deck-relative slash paths (files and
// directories) a clean Init must produce: the embedded hello seed's tree
// plus the empty assets/ directory Init creates itself.
var wantSeedTree = []string{
	"AGENTS.md",
	"assets",
	"kalide.yaml",
	"slides",
	"slides/1-hello.md",
	"templates",
	"templates/library.yaml",
	"templates/slides",
	"templates/slides/hello",
	"templates/slides/hello/example.md",
	"templates/slides/hello/layout.html.tmpl",
	"templates/slides/hello/template.yaml",
	"templates/themes",
	"templates/themes/default",
	"templates/themes/default/theme.css",
}

// wantSeedFiles are the embedded seed's files, checked byte-for-byte against
// what Init writes.
var wantSeedFiles = []string{
	"AGENTS.md",
	"kalide.yaml",
	"slides/1-hello.md",
	"templates/library.yaml",
	"templates/slides/hello/template.yaml",
	"templates/slides/hello/layout.html.tmpl",
	"templates/slides/hello/example.md",
	"templates/themes/default/theme.css",
}

// testInitClean asserts Init on a fresh directory writes exactly the embedded
// hello seed tree — no more, no less — byte-for-byte, plus an empty assets/
// directory; no sections, fonts, logos or media appear anywhere.
func testInitClean(t *testing.T) {
	dir := t.TempDir()

	if err := Init(dir); err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}

	for _, name := range wantSeedFiles {
		assertFileEqualsSeed(t, dir, name)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "assets"))
	if err != nil {
		t.Fatalf("ReadDir(assets/): %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("assets/ contains %d entries, want an empty directory", len(entries))
	}

	if got := listTree(t, dir); !reflect.DeepEqual(got, wantSeedTree) {
		t.Fatalf("scaffolded tree = %v, want %v", got, wantSeedTree)
	}
}

// testInitRefuses asserts that each of slides/, templates/, assets/ and
// kalide.yaml, present as either a file or a directory, makes Init fail
// naming that path and that the directory is byte-for-byte unchanged (no
// partial deck, no overwrite).
func testInitRefuses(t *testing.T) {
	cases := []struct {
		name    string
		block   string // path pre-created under the target directory
		isDir   bool   // whether the block is a directory
		display string // the name Init's message must carry
	}{
		{"existing slides/ directory", "slides", true, "slides/"},
		{"existing slides file", "slides", false, "slides/"},
		{"existing templates/ directory", "templates", true, "templates/"},
		{"existing templates file", "templates", false, "templates/"},
		{"existing assets/ directory", "assets", true, "assets/"},
		{"existing assets file", "assets", false, "assets/"},
		{"existing kalide.yaml file", "kalide.yaml", false, "kalide.yaml"},
		{"existing kalide.yaml directory", "kalide.yaml", true, "kalide.yaml"},
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
// the fixed slides/, templates/, assets/, kalide.yaml order, and still writes
// nothing.
func testInitNamesAllConflicts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kalide.yaml"), "title: mine\n")
	mkdir(t, filepath.Join(dir, "slides"))
	mkdir(t, filepath.Join(dir, "templates"))
	mkdir(t, filepath.Join(dir, "assets"))

	before := snapshot(t, dir)

	err := Init(dir)
	if err == nil {
		t.Fatal("Init() error = nil, want a refusal naming all four blocking paths")
	}
	msg := err.Error()
	for _, want := range []string{"slides/", "templates/", "assets/", "kalide.yaml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Init() error = %q, want it to name %q", msg, want)
		}
	}

	si := strings.Index(msg, "slides/")
	ti := strings.Index(msg, "templates/")
	ai := strings.Index(msg, "assets/")
	yi := strings.Index(msg, "kalide.yaml")
	if si < 0 || ti < 0 || ai < 0 || yi < 0 || !(si < ti && ti < ai && ai < yi) {
		t.Errorf("Init() error = %q, want all paths in order slides/, templates/, assets/, kalide.yaml", msg)
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
	writeFile(t, filepath.Join(dir, ".kalide.lock"), "lock\n")

	if err := Init(dir); err != nil {
		t.Fatalf("Init() error = %v, want nil (unrelated entries must not block)", err)
	}

	if got := readString(t, filepath.Join(dir, "README.md")); got != "# notes\n" {
		t.Errorf("README.md = %q, want it unchanged", got)
	}
	if got := readString(t, filepath.Join(dir, ".git", "config")); got != "[core]\n" {
		t.Errorf(".git/config = %q, want it unchanged", got)
	}
	for _, name := range wantSeedFiles {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Errorf("expected %s to exist after Init: %v", name, err)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "assets")); err != nil || !info.IsDir() {
		t.Errorf("expected assets/ to be a directory after Init, stat err = %v", err)
	}
}

// testInitExistingEypresYAML asserts that an existing eypres.yaml in the
// directory does not block init (Init succeeds and writes kalide.yaml) and
// that Init never writes eypres.yaml.
func testInitExistingEypresYAML(t *testing.T) {
	dir := t.TempDir()
	eypresContent := "title: Old Eypres Deck\n"
	writeFile(t, filepath.Join(dir, "eypres.yaml"), eypresContent)

	if err := Init(dir); err != nil {
		t.Fatalf("Init() error = %v, want nil (eypres.yaml must not block init)", err)
	}

	if got := readString(t, filepath.Join(dir, "eypres.yaml")); got != eypresContent {
		t.Errorf("eypres.yaml modified by init: got %q, want %q", got, eypresContent)
	}
	if _, err := os.Stat(filepath.Join(dir, "kalide.yaml")); err != nil {
		t.Fatalf("kalide.yaml was not written by init: %v", err)
	}

	cleanDir := t.TempDir()
	if err := Init(cleanDir); err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(cleanDir, "eypres.yaml")); err == nil {
		t.Errorf("Init unexpectedly created eypres.yaml in clean dir")
	}
}

// testInitPreservesExistingAgentsGuide asserts a pre-existing deck-root
// AGENTS.md never blocks the no-arg seed form — neither Init(dir) nor
// Init(dir, "") — and is left byte-for-byte untouched, while the rest of the
// starter deck is still written (agent-authoring).
func testInitPreservesExistingAgentsGuide(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"Init(dir)", nil},
		{`Init(dir, "")`, []string{""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			const sentinel = "# my own deck guide\n\nkeep me exactly as I am.\n"
			writeFile(t, filepath.Join(dir, "AGENTS.md"), sentinel)

			if err := Init(dir, tc.args...); err != nil {
				t.Fatalf("Init(dir, %v) error = %v, want nil (a pre-existing AGENTS.md must not block)", tc.args, err)
			}

			if got := readString(t, filepath.Join(dir, "AGENTS.md")); got != sentinel {
				t.Errorf("AGENTS.md = %q, want the pre-existing guide byte-for-byte unchanged", got)
			}

			// The rest of the seed is still written around the kept guide.
			for _, name := range wantSeedFiles {
				if name == "AGENTS.md" {
					continue
				}
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
					t.Errorf("expected %s after Init: %v", name, err)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "assets")); err != nil {
				t.Errorf("expected assets/ after Init: %v", err)
			}
		})
	}
}

// testInitScaffoldValidates loads the scaffolded deck through internal/deck
// (LoadConfig + LoadSlides) and runs the whole-deck validator using the
// registry and theme registry built from the scaffolded templates/ library
// itself — via template.LoadLibrary, template.NewRegistryFromLibrary and
// theme.LoadDir — not template.Builtins/theme.Builtin, which carry no
// "hello" template and would validate nothing meaningful about this seed.
func testInitScaffoldValidates(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	fsys := os.DirFS(dir)

	themes, err := theme.LoadDir(fsys, filepath.ToSlash(filepath.Join(template.TemplatesDir, template.ThemesDir)))
	if err != nil {
		t.Fatalf("theme.LoadDir() error = %v", err)
	}
	if _, err := themes.Lookup(theme.DefaultName); err != nil {
		t.Fatalf("seed's default theme does not resolve: %v", err)
	}

	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themes)
	if err != nil {
		t.Fatalf("deck.LoadConfig() error = %v", err)
	}
	if strings.TrimSpace(cfg.Title) == "" {
		t.Error("scaffolded config title is empty, want a non-empty title")
	}
	if _, err := deck.LoadSlides(fsys, deck.SlidesDir); err != nil {
		t.Fatalf("deck.LoadSlides() error = %v", err)
	}

	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary() error = %v", err)
	}
	if len(lib.Sections) != 0 {
		t.Errorf("scaffolded library has %d sections, want none", len(lib.Sections))
	}
	if _, ok := lib.Slides["hello"]; !ok {
		t.Errorf("scaffolded library has no %q slide template, want exactly one", "hello")
	}
	if len(lib.Slides) != 1 {
		t.Errorf("scaffolded library has %d slide templates, want exactly 1 (hello)", len(lib.Slides))
	}

	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary() error = %v", err)
	}
	verr, invalid := validate.Validate(fsys, reg, themes)
	if invalid {
		t.Fatalf("validate.Validate() invalid = true with %q, want a valid deck", validate.Format(verr))
	}
	if !reflect.DeepEqual(verr, validate.ValidationError{}) {
		t.Fatalf("validate.Validate() error = %#v, want the zero ValidationError", verr)
	}
}

// testSeedHasNoEYReferences asserts none of the embedded seed's files
// mention EY branding: no "EY" substring, no EY font or logo reference,
// anywhere in the seed tree Init writes.
func testSeedHasNoEYReferences(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(data), "EY") {
			rel, _ := filepath.Rel(dir, p)
			t.Errorf("%s contains \"EY\", want the seed unbranded", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}

// assertFileEqualsSeed reads name from the embedded hello seed (seedRoot, this
// package's own embedded fs.FS) and asserts the scaffolded file is non-empty
// and byte-for-byte identical to it.
func assertFileEqualsSeed(t *testing.T, dir, name string) {
	t.Helper()

	want, err := fs.ReadFile(seedRoot, name)
	if err != nil {
		t.Fatalf("read embedded seed %s: %v", name, err)
	}
	if len(want) == 0 {
		t.Fatalf("embedded seed %s is empty", name)
	}

	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read scaffolded %s: %v", name, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scaffolded %s differs from the embedded seed:\n got %q\nwant %q", name, got, want)
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

// externalDeckTree is the exact deck-relative set of entries InitExternal
// writes: kalide.yaml and the empty slides/ and assets/ directories, and
// deliberately no local templates/ and no starter slide. It is the external
// counterpart of wantSeedTree.
var externalDeckTree = []string{
	"AGENTS.md",
	"assets",
	"kalide.yaml",
	"slides",
}

// TestInitExternal covers the external-library form of Init (phase-03 task-7):
// requirements.requirement.cli-init and
// requirements.requirement.cli-init-refuse-existing.
//
// It exercises Init's optional external path (which delegates to InitExternal)
// and InitExternal directly on real temporary directories: a valid external
// library whose default theme resolves writes `templates: <path>`, an empty
// slides/ with no starter slide, an empty assets/ and no local templates/; the
// refusal matrix still blocks slides/, assets/ and kalide.yaml while a
// pre-existing local templates/ is exempt and left untouched; an invalid or
// missing library, or one whose default theme does not resolve, aborts with
// nothing written; the no-arg hello-seed path is unchanged, including the
// empty-string path; and a pre-existing deck-root AGENTS.md never blocks and is
// left byte-for-byte untouched (agent-authoring).
func TestInitExternal(t *testing.T) {
	t.Run("valid external library writes an external deck", testInitExternalWrites)
	t.Run("absolute external path is written as given", testInitExternalAbsolutePath)
	t.Run("missing external library aborts with nothing written", testInitExternalMissingLibrary)
	t.Run("invalid external library aborts with nothing written", testInitExternalInvalidLibrary)
	t.Run("unresolvable default theme aborts with nothing written", testInitExternalUnresolvableTheme)
	t.Run("refusal matrix blocks deck paths", testInitExternalRefusalMatrix)
	t.Run("pre-existing local templates is exempt and left untouched", testInitExternalTemplatesExempt)
	t.Run("pre-existing AGENTS.md is left untouched", testInitExternalPreservesExistingAgentsGuide)
	t.Run("more than one external path is rejected", testInitExternalRejectsExtraPath)
	t.Run("empty external path is rejected by InitExternal", testInitExternalEmptyPath)
	t.Run("no-arg hello-seed path is unchanged", testInitNoArgSeed)
}

// testInitExternalWrites asserts Init(dir, "../shared-lib") on a clean
// directory whose sibling shared-lib is a valid external library writes
// kalide.yaml with templates: ../shared-lib, an empty slides/ and an empty
// assets/, and no local templates/ or starter slide.
func testInitExternalWrites(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "deck")
	writeExternalLibrary(t, filepath.Join(parent, "shared-lib"), "shared-lib")

	if err := Init(dir, "../shared-lib"); err != nil {
		t.Fatalf("Init(dir, ../shared-lib) error = %v, want nil", err)
	}

	title, templates := externalConfigOf(t, dir)
	if strings.TrimSpace(title) == "" {
		t.Error("external deck title is empty, want a non-empty title")
	}
	if templates != "../shared-lib" {
		t.Errorf("kalide.yaml templates = %q, want the external path %q written verbatim", templates, "../shared-lib")
	}

	if got := listTree(t, dir); !reflect.DeepEqual(got, externalDeckTree) {
		t.Fatalf("external deck tree = %v, want %v (no local templates/, no starter slide)", got, externalDeckTree)
	}
	// Both init forms write the identical deck-root agent guide, verbatim
	// from the embedded seed (agent-authoring).
	assertFileEqualsSeed(t, dir, "AGENTS.md")
	for _, name := range []string{"slides", "assets"} {
		entries, err := os.ReadDir(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("ReadDir(%s/): %v", name, err)
		}
		if len(entries) != 0 {
			t.Errorf("%s/ contains %d entries, want an empty directory", name, len(entries))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "templates")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("local templates/ present after external init (stat err = %v), want none", err)
	}
}

// testInitExternalAbsolutePath asserts an absolute external path is written to
// kalide.yaml as given and validated directly (no resolution against the deck
// root), producing the same external deck tree.
func testInitExternalAbsolutePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deck")
	lib := t.TempDir() // absolute, unrelated to dir
	writeExternalLibrary(t, lib, "shared-lib")

	if err := InitExternal(dir, lib); err != nil {
		t.Fatalf("InitExternal(dir, %s) error = %v, want nil", lib, err)
	}

	if _, templates := externalConfigOf(t, dir); templates != lib {
		t.Errorf("kalide.yaml templates = %q, want the absolute path %q verbatim", templates, lib)
	}
	if got := listTree(t, dir); !reflect.DeepEqual(got, externalDeckTree) {
		t.Fatalf("external deck tree = %v, want %v", got, externalDeckTree)
	}
}

// testInitExternalMissingLibrary asserts a configured external path that does
// not exist aborts naming the resolved path and creates nothing at all.
func testInitExternalMissingLibrary(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "deck")

	err := Init(dir, "../nope")
	if err == nil {
		t.Fatal("Init(dir, ../nope) error = nil, want a missing-library error")
	}
	if want := filepath.Join(parent, "nope"); !strings.Contains(err.Error(), want) {
		t.Errorf("Init error = %q, want it to name the resolved path %q", err, want)
	}
	assertNothingWritten(t, dir)
}

// testInitExternalInvalidLibrary asserts an external path that exists but is
// not a valid template library aborts naming the offending library.yaml and
// creates nothing at all.
func testInitExternalInvalidLibrary(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "deck")
	lib := filepath.Join(parent, "bad-lib")
	writeExternalFile(t, filepath.Join(lib, "library.yaml"), "name: Bad_Name\nformat: 1\n")

	err := Init(dir, "../bad-lib")
	if err == nil {
		t.Fatal("Init(dir, ../bad-lib) error = nil, want an invalid-library error")
	}
	if want := filepath.Join(lib, "library.yaml"); !strings.Contains(err.Error(), want) {
		t.Errorf("Init error = %q, want it to name the offending %q", err, want)
	}
	if !strings.Contains(err.Error(), "not a valid name") {
		t.Errorf("Init error = %q, want the library.yaml name error", err)
	}
	assertNothingWritten(t, dir)
}

// testInitExternalUnresolvableTheme asserts the deck's default theme must
// resolve in the external library: a library with no themes/ at all, and one
// whose only theme is not `default`, both abort naming the library path and
// create nothing.
func testInitExternalUnresolvableTheme(t *testing.T) {
	t.Run("no themes directory", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "deck")
		lib := filepath.Join(parent, "no-themes-lib")
		// A valid library with no themes/ entry: LoadLibrary accepts it, but
		// the deck's default theme cannot resolve.
		writeExternalFile(t, filepath.Join(lib, "library.yaml"), "name: no-themes-lib\nformat: 1\n")

		err := Init(dir, "../no-themes-lib")
		if err == nil {
			t.Fatal("Init(dir, ../no-themes-lib) error = nil, want a themes error")
		}
		if !strings.Contains(err.Error(), filepath.Join("themes")) {
			t.Errorf("Init error = %q, want it to name the missing themes/ directory", err)
		}
		assertNothingWritten(t, dir)
	})

	t.Run("only a non-default theme", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "deck")
		lib := filepath.Join(parent, "plain-lib")
		writeExternalFile(t, filepath.Join(lib, "library.yaml"), "name: plain-lib\nformat: 1\n")
		writeExternalFile(t, filepath.Join(lib, "themes", "plain", "theme.css"), "body {}\n")

		err := Init(dir, "../plain-lib")
		if err == nil {
			t.Fatal("Init(dir, ../plain-lib) error = nil, want an unresolvable default theme error")
		}
		if !strings.Contains(err.Error(), "default") {
			t.Errorf("Init error = %q, want it to name the unresolvable default theme", err)
		}
		if !strings.Contains(err.Error(), "plain-lib") {
			t.Errorf("Init error = %q, want it to name the external library path", err)
		}
		assertNothingWritten(t, dir)
	})
}

// testInitExternalRefusalMatrix asserts slides/, assets/ and kalide.yaml — as a
// file or a directory — still block the external form, naming the path and
// writing nothing, even though the external library itself is valid.
func testInitExternalRefusalMatrix(t *testing.T) {
	lib := filepath.Join(t.TempDir(), "shared-lib")
	writeExternalLibrary(t, lib, "shared-lib")

	cases := []struct {
		name    string
		block   string
		isDir   bool
		display string
	}{
		{"existing slides directory", "slides", true, "slides/"},
		{"existing slides file", "slides", false, "slides/"},
		{"existing assets directory", "assets", true, "assets/"},
		{"existing assets file", "assets", false, "assets/"},
		{"existing kalide.yaml file", "kalide.yaml", false, "kalide.yaml"},
		{"existing kalide.yaml directory", "kalide.yaml", true, "kalide.yaml"},
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

			err := Init(dir, lib)
			if err == nil {
				t.Fatalf("Init(dir, %s) error = nil, want a refusal naming %q", lib, tc.display)
			}
			if !strings.Contains(err.Error(), tc.display) {
				t.Errorf("Init error = %q, want it to name %q", err, tc.display)
			}

			if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
				t.Fatalf("Init modified the directory on refusal:\n before %v\n after  %v", before, after)
			}
		})
	}
}

// testInitExternalTemplatesExempt asserts a pre-existing local templates/ is
// exempt for the external form: it is left byte-for-byte untouched while the
// external deck is written. The no-arg form still blocks on it, so the
// exemption is specific to the external path.
func testInitExternalTemplatesExempt(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "deck")
	writeExternalLibrary(t, filepath.Join(parent, "shared-lib"), "shared-lib")

	mkdir(t, filepath.Join(dir, "templates"))
	const sentinel = "local templates content that external init must not touch\n"
	writeFile(t, filepath.Join(dir, "templates", "keep.txt"), sentinel)

	if err := Init(dir, "../shared-lib"); err != nil {
		t.Fatalf("Init(dir, ../shared-lib) error = %v, want nil with a pre-existing local templates/", err)
	}

	if got := readString(t, filepath.Join(dir, "templates", "keep.txt")); got != sentinel {
		t.Errorf("templates/keep.txt = %q, want the pre-existing content unchanged", got)
	}
	if _, templates := externalConfigOf(t, dir); templates != "../shared-lib" {
		t.Errorf("kalide.yaml templates = %q, want %q", templates, "../shared-lib")
	}
	for _, name := range []string{"kalide.yaml", "slides", "assets"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("stat %s after external init: %v, want created", name, err)
		}
	}

	// The no-arg form still refuses a pre-existing templates/ (the exemption
	// is not a general relaxation).
	noArg := t.TempDir()
	mkdir(t, filepath.Join(noArg, "templates"))
	before := snapshot(t, noArg)
	if err := Init(noArg); err == nil {
		t.Error("no-arg Init on a directory with templates/ error = nil, want a refusal")
	}
	if after := snapshot(t, noArg); !reflect.DeepEqual(after, before) {
		t.Errorf("no-arg Init modified the directory on refusal:\n before %v\n after  %v", before, after)
	}
}

// testInitExternalPreservesExistingAgentsGuide asserts a pre-existing deck-root
// AGENTS.md never blocks either external form — Init(dir, lib) and
// InitExternal(dir, lib) — and is left byte-for-byte untouched, while the
// external deck is still written (agent-authoring). AGENTS.md is not a
// deck-owned blockPath, so this is not a relaxation of the refusal matrix.
func testInitExternalPreservesExistingAgentsGuide(t *testing.T) {
	lib := filepath.Join(t.TempDir(), "shared-lib")
	writeExternalLibrary(t, lib, "shared-lib")

	cases := []struct {
		name string
		init func(dir string) error
	}{
		{"Init(dir, lib)", func(dir string) error { return Init(dir, lib) }},
		{"InitExternal(dir, lib)", func(dir string) error { return InitExternal(dir, lib) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "deck")
			mkdir(t, dir)
			const sentinel = "# my own deck guide\n\nkeep me exactly as I am.\n"
			writeFile(t, filepath.Join(dir, "AGENTS.md"), sentinel)

			if err := tc.init(dir); err != nil {
				t.Fatalf("%s error = %v, want nil (a pre-existing AGENTS.md must not block)", tc.name, err)
			}

			if got := readString(t, filepath.Join(dir, "AGENTS.md")); got != sentinel {
				t.Errorf("AGENTS.md = %q, want the pre-existing guide byte-for-byte unchanged", got)
			}

			for _, name := range []string{"kalide.yaml", "slides", "assets"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Errorf("expected %s after %s: %v", name, tc.name, err)
				}
			}
			if _, templates := externalConfigOf(t, dir); templates != lib {
				t.Errorf("kalide.yaml templates = %q, want %q", templates, lib)
			}
		})
	}
}

// testInitExternalRejectsExtraPath asserts Init rejects more than one optional
// path as a programming error, writing nothing.
func testInitExternalRejectsExtraPath(t *testing.T) {
	dir := t.TempDir()
	before := snapshot(t, dir)

	err := Init(dir, "a", "b")
	if err == nil {
		t.Fatal("Init(dir, a, b) error = nil, want the at-most-one-path error")
	}
	if !strings.Contains(err.Error(), "at most one") {
		t.Errorf("Init error = %q, want it to say at most one path is allowed", err)
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
		t.Errorf("Init(dir, a, b) modified the directory:\n before %v\n after  %v", before, after)
	}
}

// testInitExternalEmptyPath asserts InitExternal rejects an empty external path
// directly, writing nothing. (Through Init, an empty path selects the no-arg
// seed instead — see testInitNoArgSeed.)
func testInitExternalEmptyPath(t *testing.T) {
	dir := t.TempDir()
	before := snapshot(t, dir)

	err := InitExternal(dir, "")
	if err == nil {
		t.Fatal("InitExternal(dir, \"\") error = nil, want an empty-path error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("InitExternal error = %q, want it to say the external library path is empty", err)
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
		t.Errorf("InitExternal(dir, \"\") modified the directory:\n before %v\n after  %v", before, after)
	}
}

// testInitNoArgSeed asserts the no-arg hello-seed path is unchanged: both
// Init(dir) and Init(dir, "") write exactly wantSeedTree (including the local
// templates/ library) and a kalide.yaml with no `templates:` key.
func testInitNoArgSeed(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no argument", nil},
		{"empty external path", []string{""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			if err := Init(dir, tc.args...); err != nil {
				t.Fatalf("Init(dir, %v) error = %v, want the hello seed", tc.args, err)
			}
			if got := listTree(t, dir); !reflect.DeepEqual(got, wantSeedTree) {
				t.Fatalf("seed tree = %v, want %v", got, wantSeedTree)
			}
			if _, templates := externalConfigOf(t, dir); templates != "" {
				t.Errorf("seed kalide.yaml templates = %q, want empty (no external library)", templates)
			}
		})
	}
}

// writeExternalLibrary writes a minimal valid external template library at dir:
// library.yaml with the given (valid) name and format 1, and a default theme
// with its required theme.css. A library with no slides/sections/media is
// valid, and themes/default is exactly what InitExternal needs to resolve the
// deck's default theme.
func writeExternalLibrary(t *testing.T, dir, name string) {
	t.Helper()
	writeExternalFile(t, filepath.Join(dir, "library.yaml"), "name: "+name+"\nformat: 1\n")
	writeExternalFile(t, filepath.Join(dir, "themes", "default", "theme.css"), "body { margin: 0; }\n")
}

// writeExternalFile is writeFile with parent directories created, for the
// nested files an external library needs.
func writeExternalFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// externalConfigOf parses kalide.yaml under dir and returns its title and
// templates values, so a test asserts the written configuration rather than
// its exact formatting.
func externalConfigOf(t *testing.T, dir string) (title, templates string) {
	t.Helper()
	var cfg struct {
		Title     string `yaml:"title"`
		Templates string `yaml:"templates"`
	}
	if err := yaml.Unmarshal([]byte(readString(t, filepath.Join(dir, "kalide.yaml"))), &cfg); err != nil {
		t.Fatalf("parse %s: %v", filepath.Join(dir, "kalide.yaml"), err)
	}
	return cfg.Title, cfg.Templates
}

// assertNothingWritten asserts dir was not created, so a failed init wrote
// nothing at all.
func assertNothingWritten(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("directory %s exists after a failed init (stat err = %v), want nothing written", dir, err)
	}
}
