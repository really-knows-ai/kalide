package cli

// This file is the in-process command-surface test file for `kalide upgrade`
// (plan.phase-04.task-8): requirements.requirement.deck-library-upgrade,
// global.constraint.upgrade-never-clobbers and global.constraint.upgrade-offline.
//
// It exercises Run's `upgrade` route and the runUpgrade handler against real
// project trees written to t.TempDir — a no-arg seed deck (scaffold.Init), a
// template library (scaffold.InitLibrary) and an external-library deck
// (scaffold.InitExternal) — rather than scaffold's internals, so it is an
// integration test and is skipped under -short. Every case runs in its own temp
// directory (t.Chdir), so the package directory is never written to.
//
// This task creates the file's shared scaffolding only: the fixtures, the
// handler runners, the tree snapshot/unchanged assertions and the loader
// validators below. The concrete assertions of cli.TestRunUpgrade are authored
// by plan.phase-04.task-9, which completes the placeholder at the bottom of this
// file; the helpers are named for the cases that task names.

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/scaffold"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// upgradeDeckMemberPaths is the kalide-owned deck member set refreshScaffold
// visits, in visit order (internal/scaffold/refresh.go deckOwnedMembers): the
// deck-root guide, the local library.yaml, the hello template's three files, the
// default theme and the starter slide. It is the deck form's report path set.
var upgradeDeckMemberPaths = []string{
	"AGENTS.md",
	template.TemplatesDir + "/" + template.LibraryFile,
	template.TemplatesDir + "/" + template.SlidesDir + "/" + "hello/" + template.ExampleFile,
	template.TemplatesDir + "/" + template.SlidesDir + "/" + "hello/" + template.LayoutFile,
	template.TemplatesDir + "/" + template.SlidesDir + "/" + "hello/" + template.ManifestFile,
	template.TemplatesDir + "/" + template.ThemesDir + "/" + theme.DefaultName + "/" + template.ThemeStylesheet,
	template.SlidesDir + "/" + "1-hello.md",
}

// upgradeLibraryMemberPaths is the library-owned deck member set refreshScaffold
// visits, in visit order (internal/scaffold/refresh.go libraryOwnedMembers): the
// library-root guide and the seeded default theme.
var upgradeLibraryMemberPaths = []string{
	"AGENTS.md",
	template.ThemesDir + "/" + theme.DefaultName + "/" + template.ThemeStylesheet,
}

// assertUpgradeReportCounts asserts the printed report's refreshed and skipped
// headings carry the expected counts, pinning the full report shape.
func assertUpgradeReportCounts(t *testing.T, stdout string, refreshed, skipped int) {
	t.Helper()
	if want := fmt.Sprintf("refreshed (%d):", refreshed); !strings.Contains(stdout, want) {
		t.Errorf("upgrade report = %q, want the heading %q", stdout, want)
	}
	if want := fmt.Sprintf("skipped (left alone) (%d):", skipped); !strings.Contains(stdout, want) {
		t.Errorf("upgrade report = %q, want the heading %q", stdout, want)
	}
}

// upgradeReportNamesPath reports whether the printed report names rel as a path
// line ("  <rel>"), so a case can assert a refreshed or skipped path instead of
// a bare substring.
func upgradeReportNamesPath(stdout, rel string) bool {
	return strings.Contains(stdout, "  "+rel+"\n")
}

// TestRunUpgrade is the command-surface suite for `kalide upgrade`
// (upgrade/plan.phase-04.task-9): requirements.requirement.deck-library-upgrade,
// global.constraint.upgrade-never-clobbers,
// global.constraint.upgrade-unknown-format-reported,
// global.constraint.upgrade-never-breaks-project and
// global.constraint.upgrade-offline. It drives runUpgrade — and, for the route,
// cli.Run's `upgrade` case — against real project trees written to a t.TempDir
// and asserts exit codes, stdout/stderr text and tree bytes plus mtimes.
//
// COVERAGE BOUNDARY: the refresh engine's "present and byte-identical to a
// catalogued released version is rewritten" branch presents an OLD released
// version. The catalogue holds digests only, never historical contents
// (global.constraint.upgrade-known-version-catalog), and the embedded seed ships
// only the running binary's own bytes, so that old-bytes state cannot be
// constructed from package cli — the in-package catalogue seam (refreshSetCatalog)
// lives in internal/scaffold's refresh_test.go and is unexported here. The
// reachable refresh-half write at this surface is the absent-author-guide
// exception (an absent AGENTS.md is written), which the clean-run, library-run,
// idempotence and unknown-format cases below use; an already-current owned file
// is skipped, and an author-edited one is skipped and named.
func TestRunUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: `kalide upgrade` writes real project trees to disk")
	}

	t.Run("Run dispatches upgrade to the handler", func(t *testing.T) {
		dir := newUpgradeDeck(t)
		t.Chdir(dir)

		code, stdout, stderr := runUpgradeCLI()
		if code != 0 {
			t.Fatalf("Run(upgrade) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(upgrade) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "kalide upgrade: deck") {
			t.Fatalf("Run(upgrade) stdout = %q, want the deck report header", stdout)
		}
	})

	t.Run("a path argument is a usage error: exit 2, usage printed, stdout empty, nothing written", func(t *testing.T) {
		dir := newUpgradeDeck(t)
		before := snapshotUpgradeTree(t, dir)

		stdout, stderr := runUpgradeExpect(t, dir, 2, "../deck")
		if stdout != "" {
			t.Errorf("runUpgrade(../deck) stdout = %q, want empty", stdout)
		}
		for _, want := range []string{"unexpected argument", "\"../deck\"", "Usage:"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("runUpgrade(../deck) stderr = %q, want it to contain %q", stderr, want)
			}
		}
		assertUpgradeTreeUnchanged(t, dir, before)
	})

	t.Run("a --force-style flag is a usage error: exit 2, usage printed, stdout empty, nothing written", func(t *testing.T) {
		dir := newUpgradeDeck(t)
		before := snapshotUpgradeTree(t, dir)

		stdout, stderr := runUpgradeExpect(t, dir, 2, "--force")
		if stdout != "" {
			t.Errorf("runUpgrade(--force) stdout = %q, want empty", stdout)
		}
		for _, want := range []string{"unexpected argument", "\"--force\"", "Usage:"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("runUpgrade(--force) stderr = %q, want it to contain %q", stderr, want)
			}
		}
		assertUpgradeTreeUnchanged(t, dir, before)
	})

	t.Run("the Run route rejects --force with exit 2 and usage", func(t *testing.T) {
		t.Chdir(t.TempDir())

		code, stdout, stderr := runUpgradeCLI("--force")
		if code != 2 {
			t.Fatalf("Run(upgrade --force) exit = %d, want 2", code)
		}
		if stdout != "" {
			t.Errorf("Run(upgrade --force) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(upgrade --force) stderr = %q, want the usage text", stderr)
		}
	})

	t.Run("both manifests is a detection error naming found vs expected and writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		writeUpgradeFile(t, dir, deck.ConfigFile, "title: Both\n")
		writeUpgradeFile(t, dir, template.LibraryFile, "name: shared-lib\nformat: 1\n")
		before := snapshotUpgradeTree(t, dir)

		stdout, stderr := runUpgradeExpect(t, dir, 1)
		if stdout != "" {
			t.Errorf("runUpgrade(both manifests) stdout = %q, want empty", stdout)
		}
		for _, want := range []string{deck.ConfigFile, template.LibraryFile, "both", "expected exactly one"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("runUpgrade(both manifests) stderr = %q, want it to contain %q", stderr, want)
			}
		}
		assertUpgradeTreeUnchanged(t, dir, before)
	})

	t.Run("neither manifest is a detection error naming found vs expected and writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		before := snapshotUpgradeTree(t, dir)

		stdout, stderr := runUpgradeExpect(t, dir, 1)
		if stdout != "" {
			t.Errorf("runUpgrade(neither manifest) stdout = %q, want empty", stdout)
		}
		for _, want := range []string{deck.ConfigFile, template.LibraryFile, "neither", "expected exactly one"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("runUpgrade(neither manifest) stderr = %q, want it to contain %q", stderr, want)
			}
		}
		assertUpgradeTreeUnchanged(t, dir, before)
	})

	t.Run("a clean deck run refreshes the absent guide and reports every owned path", func(t *testing.T) {
		dir := newUpgradeDeck(t)
		guide := readUpgradeFile(t, dir, "AGENTS.md")
		if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
			t.Fatalf("remove AGENTS.md: %v", err)
		}

		stdout, stderr := runUpgradeExpect(t, dir, 0)
		if stderr != "" {
			t.Errorf("runUpgrade(clean deck) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "kalide upgrade: deck") {
			t.Errorf("runUpgrade(clean deck) stdout = %q, want the deck report header", stdout)
		}
		assertUpgradeReportCounts(t, stdout, 1, len(upgradeDeckMemberPaths)-1)
		if !upgradeReportNamesPath(stdout, "AGENTS.md") {
			t.Errorf("runUpgrade(clean deck) stdout = %q, want the refreshed AGENTS.md named", stdout)
		}
		for _, rel := range upgradeDeckMemberPaths[1:] {
			if !upgradeReportNamesPath(stdout, rel) {
				t.Errorf("runUpgrade(clean deck) stdout = %q, want the skipped path %q named", stdout, rel)
			}
		}
		if got := readUpgradeFile(t, dir, "AGENTS.md"); !bytes.Equal(got, guide) {
			t.Errorf("written AGENTS.md does not equal the scaffolded deck guide")
		}

		// The refreshed deck still loads through the loader `kalide start` uses.
		assertUpgradeDeckLoads(t, dir)
	})

	t.Run("a second deck run is a no-op exit 0 and writes nothing", func(t *testing.T) {
		dir := newUpgradeDeck(t)
		if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
			t.Fatalf("remove AGENTS.md: %v", err)
		}
		// The first run writes the absent guide, so the second has nothing to do.
		runUpgradeExpect(t, dir, 0)

		before := snapshotUpgradeTree(t, dir)
		stdout, stderr := runUpgradeExpect(t, dir, 0)
		if stderr != "" {
			t.Errorf("second runUpgrade stderr = %q, want empty", stderr)
		}
		assertUpgradeReportCounts(t, stdout, 0, len(upgradeDeckMemberPaths))
		if !strings.Contains(stdout, "(none)") {
			t.Errorf("second runUpgrade stdout = %q, want an empty refreshed list", stdout)
		}
		assertUpgradeTreeUnchanged(t, dir, before)
	})

	t.Run("an author-edited owned file is skipped, named and exits 0", func(t *testing.T) {
		dir := newUpgradeDeck(t)
		themeRel := template.TemplatesDir + "/" + template.ThemesDir + "/" + theme.DefaultName + "/" + template.ThemeStylesheet
		author := "/* my own theme */\n:root { --accent: rebeccapurple; }\n"
		writeUpgradeFile(t, dir, themeRel, author)
		before := snapshotUpgradeTree(t, dir)

		stdout, stderr := runUpgradeExpect(t, dir, 0)
		if stderr != "" {
			t.Errorf("runUpgrade(author-edited) stderr = %q, want empty", stderr)
		}
		assertUpgradeReportCounts(t, stdout, 0, len(upgradeDeckMemberPaths))
		if !upgradeReportNamesPath(stdout, themeRel) {
			t.Errorf("runUpgrade(author-edited) stdout = %q, want the skipped %q named", stdout, themeRel)
		}
		if got := string(readUpgradeFile(t, dir, themeRel)); got != author {
			t.Errorf("%s bytes changed, want the author content untouched", themeRel)
		}
		assertUpgradeTreeUnchanged(t, dir, before)

		assertUpgradeDeckLoads(t, dir)
	})

	t.Run("an external-library deck leaves its configured library byte-for-byte untouched and still loads", func(t *testing.T) {
		deckDir, libDir := newUpgradeExternalDeck(t, "shared-lib")
		before := snapshotUpgradeTree(t, libDir)

		stdout, stderr := runUpgradeExpect(t, deckDir, 0)
		if stderr != "" {
			t.Errorf("runUpgrade(external-library deck) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "kalide upgrade: deck") {
			t.Errorf("runUpgrade(external-library deck) stdout = %q, want the deck report header", stdout)
		}
		// An already-current external-library deck refreshes nothing; its only
		// owned member is the deck-root guide, reported skipped.
		assertUpgradeReportCounts(t, stdout, 0, 1)
		if !upgradeReportNamesPath(stdout, "AGENTS.md") {
			t.Errorf("runUpgrade(external-library deck) stdout = %q, want the skipped deck guide named", stdout)
		}

		// The configured `templates:` library is byte-for-byte and mtime-untouched.
		assertUpgradeTreeUnchanged(t, libDir, before)

		// No local templates/ is created for an external-library deck.
		if _, err := os.Lstat(filepath.Join(deckDir, template.TemplatesDir)); !os.IsNotExist(err) {
			t.Errorf("Lstat(%s/templates) err = %v, want the external-library deck to have no local templates/", deckDir, err)
		}

		assertUpgradeDeckLoads(t, deckDir)
	})

	t.Run("a library run exits 0 and reports the library-owned refreshed and skipped paths", func(t *testing.T) {
		libDir := newUpgradeLibrary(t, "shared-lib")
		guide := readUpgradeFile(t, libDir, "AGENTS.md")
		if err := os.Remove(filepath.Join(libDir, "AGENTS.md")); err != nil {
			t.Fatalf("remove library AGENTS.md: %v", err)
		}

		stdout, stderr := runUpgradeExpect(t, libDir, 0)
		if stderr != "" {
			t.Errorf("runUpgrade(library) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "kalide upgrade: library") {
			t.Errorf("runUpgrade(library) stdout = %q, want the library report header", stdout)
		}
		assertUpgradeReportCounts(t, stdout, 1, len(upgradeLibraryMemberPaths)-1)
		if !upgradeReportNamesPath(stdout, "AGENTS.md") {
			t.Errorf("runUpgrade(library) stdout = %q, want the refreshed library guide named", stdout)
		}
		if !upgradeReportNamesPath(stdout, upgradeLibraryMemberPaths[1]) {
			t.Errorf("runUpgrade(library) stdout = %q, want the skipped library theme named", stdout)
		}
		if got := readUpgradeFile(t, libDir, "AGENTS.md"); !bytes.Equal(got, guide) {
			t.Errorf("written library AGENTS.md does not equal the scaffolded library guide")
		}

		assertUpgradeLibraryLoads(t, libDir)
	})

	for _, format := range []int{0, 2} {
		format := format
		t.Run(fmt.Sprintf("a library declaring format %d is refused untouched", format), func(t *testing.T) {
			libDir := newUpgradeLibrary(t, "shared-lib")
			writeUpgradeLibraryFormat(t, libDir, format)
			// Remove the guide so the refresh half WOULD write it if it ran: the
			// format check must refuse before any refresh write, leaving nothing
			// on disk (the reachable proxy for a member the refresh half would
			// otherwise have refreshed).
			if err := os.Remove(filepath.Join(libDir, "AGENTS.md")); err != nil {
				t.Fatalf("remove library AGENTS.md: %v", err)
			}
			before := snapshotUpgradeTree(t, libDir)

			stdout, stderr := runUpgradeExpect(t, libDir, 1)
			if stdout != "" {
				t.Errorf("runUpgrade(format %d) stdout = %q, want empty", format, stdout)
			}
			for _, want := range []string{
				"unsupported library format",
				fmt.Sprintf("found format %d", format),
				"supported format 1",
			} {
				if !strings.Contains(stderr, want) {
					t.Errorf("runUpgrade(format %d) stderr = %q, want it to contain %q", format, stderr, want)
				}
			}

			// The whole tree is byte-for-byte and mtime identical, and no
			// refresh-half write (the absent AGENTS.md) was left behind.
			assertUpgradeTreeUnchanged(t, libDir, before)
			if _, err := os.Lstat(filepath.Join(libDir, "AGENTS.md")); !os.IsNotExist(err) {
				t.Errorf("Lstat(library AGENTS.md) err = %v, want the format refusal to leave no refresh write on disk", err)
			}
		})
	}

	t.Run("a post-refresh validation failure exits non-zero and restores the project", func(t *testing.T) {
		dir := newUpgradeDeck(t)
		// A pre-existing author error that survives detection and refresh but
		// fails the end-to-end validation: a deck referencing an unknown theme.
		writeUpgradeFile(t, dir, deck.ConfigFile, "title: Broken\ntheme: no-such-theme\n")
		// Remove the deck guide so the refresh half creates it; the undo must
		// remove that created file when validation fails.
		if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
			t.Fatalf("remove AGENTS.md: %v", err)
		}
		before := snapshotUpgradeTree(t, dir)

		stdout, stderr := runUpgradeExpect(t, dir, 1)
		if stdout != "" {
			t.Errorf("runUpgrade(invalid deck) stdout = %q, want empty", stdout)
		}
		for _, want := range []string{"validation failed", deck.ConfigFile} {
			if !strings.Contains(stderr, want) {
				t.Errorf("runUpgrade(invalid deck) stderr = %q, want it to contain %q", stderr, want)
			}
		}

		// The project is exactly as it was: no file added, removed or rewritten,
		// and the newly created AGENTS.md is gone.
		assertUpgradeTreeUnchanged(t, dir, before)
		if _, err := os.Lstat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
			t.Errorf("Lstat(AGENTS.md) err = %v, want the undo to remove the file Upgrade created", err)
		}
	})
}

// runUpgradeInDir runs runUpgrade with args in dir and returns its exit code and
// captured output. t.Chdir makes dir the working directory for the handler —
// `kalide upgrade` always resolves the project in the current directory and
// takes no path — and restores the previous working directory when the test
// ends. args is empty for the accepted bare invocation; a path or flag makes the
// handler return its usage error.
func runUpgradeInDir(t *testing.T, dir string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	code = runUpgrade(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// runUpgradeExpect runs runUpgrade in dir and asserts the exit code is want,
// returning the captured stdout and stderr for the caller's further assertions
// (the report text, or the refusal/usage message).
func runUpgradeExpect(t *testing.T, dir string, want int, args ...string) (stdout, stderr string) {
	t.Helper()
	code, stdout, stderr := runUpgradeInDir(t, dir, args...)
	if code != want {
		t.Fatalf("runUpgrade(%v) in %s exit = %d, want %d (stdout = %q, stderr = %q)",
			args, dir, code, want, stdout, stderr)
	}
	return stdout, stderr
}

// runUpgradeCLI routes `kalide upgrade` with the given trailing arguments
// through Run (cli.Run's dispatch table), so a case exercises the route as well
// as the handler.
func runUpgradeCLI(args ...string) (int, string, string) {
	return runCLI(append([]string{"upgrade"}, args...)...)
}

// newUpgradeDeck scaffolds the no-arg `kalide init` seed deck with scaffold.Init
// in a fresh temp directory and returns the deck root. Its kalide.yaml carries
// no `templates:` key, so the deck owns its local templates/ seed tree.
func newUpgradeDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := scaffold.Init(dir); err != nil {
		t.Fatalf("scaffold.Init(%q): %v", dir, err)
	}
	return dir
}

// newUpgradeLibrary scaffolds a template library with scaffold.InitLibrary at
// <fresh temp dir>/<name> and returns the library root. name becomes the
// directory base name and must be a valid library name ([a-z0-9][a-z0-9-]*).
func newUpgradeLibrary(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := scaffold.InitLibrary(dir); err != nil {
		t.Fatalf("scaffold.InitLibrary(%q): %v", dir, err)
	}
	return dir
}

// newUpgradeExternalDeck scaffolds a template library at <parent>/<libName>
// (scaffold.InitLibrary) and, beside it, a deck at <parent>/deck whose
// kalide.yaml configures `templates: ../<libName>` (scaffold.InitExternal). It
// returns the deck root and the library root, so a case can run the handler in
// the deck and assert the configured library is byte-for-byte untouched.
func newUpgradeExternalDeck(t *testing.T, libName string) (deckDir, libDir string) {
	t.Helper()
	parent := t.TempDir()
	libDir = filepath.Join(parent, libName)
	if err := scaffold.InitLibrary(libDir); err != nil {
		t.Fatalf("scaffold.InitLibrary(%q): %v", libDir, err)
	}
	deckDir = filepath.Join(parent, "deck")
	if err := scaffold.InitExternal(deckDir, "../"+libName); err != nil {
		t.Fatalf("scaffold.InitExternal(%q): %v", deckDir, err)
	}
	return deckDir, libDir
}

// writeUpgradeFile writes content to <dir>/<rel>, creating parent directories
// as needed, so a fixture can stage an author-edited file or a pre-existing
// manifest.
func writeUpgradeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readUpgradeFile reads <dir>/<rel> and fails the test on error, so a case can
// compare an owned member's bytes without repeating os.ReadFile error handling.
func readUpgradeFile(t *testing.T, dir, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return data
}

// writeUpgradeLibraryFormat rewrites a scaffolded library's library.yaml to
// declare the given `format:` (and a valid name derived from the directory base
// name), leaving the rest of the library as scaffold.InitLibrary wrote it, so a
// case can drive the unknown/newer-format refusal at the command surface.
func writeUpgradeLibraryFormat(t *testing.T, libDir string, format int) {
	t.Helper()
	writeUpgradeFile(t, libDir, template.LibraryFile,
		fmt.Sprintf("name: %s\nformat: %d\n", filepath.Base(libDir), format))
}

// upgradeFileState is one regular file's observed state for the unchanged-tree
// assertions: its exact bytes and modification time.
type upgradeFileState struct {
	data    []byte
	modTime time.Time
}

// snapshotUpgradeTree records every regular file under root, keyed by
// slash-separated path relative to root, with its bytes and modification time.
// The snapshot is what assertUpgradeTreeUnchanged compares a later state against
// to prove a refused or rolled-back run left the project exactly as it was.
func snapshotUpgradeTree(t *testing.T, root string) map[string]upgradeFileState {
	t.Helper()
	states := make(map[string]upgradeFileState)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		states[filepath.ToSlash(rel)] = upgradeFileState{data: data, modTime: info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return states
}

// assertUpgradeTreeUnchanged asserts root's regular files are exactly the paths
// captured in before — none added, none removed — and each is byte-for-byte and
// modification-time identical, so the project was left untouched.
func assertUpgradeTreeUnchanged(t *testing.T, root string, before map[string]upgradeFileState) {
	t.Helper()
	after := snapshotUpgradeTree(t, root)
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("%s was removed, want the tree untouched", path)
		}
	}
	for path, state := range after {
		want, ok := before[path]
		if !ok {
			t.Errorf("%s was created, want the tree untouched", path)
			continue
		}
		if !bytes.Equal(state.data, want.data) {
			t.Errorf("%s bytes changed, want byte-for-byte untouched", path)
		}
		if !state.modTime.Equal(want.modTime) {
			t.Errorf("%s mtime = %v, want unchanged %v", path, state.modTime, want.modTime)
		}
	}
}

// assertUpgradeDeckLoads validates dir exactly as `kalide start` validates a
// deck before serving: it resolves and loads the deck's template library
// (honouring a configured `templates:` path), builds the theme and template
// registries and runs the whole-deck validator, failing the test on the first
// error. It proves an upgraded deck still serves.
func assertUpgradeDeckLoads(t *testing.T, dir string) {
	t.Helper()
	deckFS := os.DirFS(dir)
	configured := ""
	if data, err := fs.ReadFile(deckFS, deck.ConfigFile); err == nil {
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) == nil {
			configured, _ = raw["templates"].(string)
		}
	}
	library, err := template.LoadDeckLibrary(dir, configured)
	if err != nil {
		t.Fatalf("upgraded deck %s: template library does not load: %v", dir, err)
	}
	themes, err := theme.LoadDir(os.DirFS(library.RootPath), template.ThemesDir)
	if err != nil {
		t.Fatalf("upgraded deck %s: theme registry: %v", dir, err)
	}
	registry, err := template.NewRegistryFromLibrary(library)
	if err != nil {
		t.Fatalf("upgraded deck %s: template registry: %v", dir, err)
	}
	if verr, invalid := validate.Validate(deckFS, registry, themes); invalid {
		t.Fatalf("upgraded deck %s does not validate: %s", dir, validate.Format(verr))
	}
}

// assertUpgradeLibraryLoads loads dir as the template library `kalide start`
// would, failing the test when the upgraded library does not load. A library IS
// a template root, so it is loaded at "." exactly as scaffold.Upgrade validates
// it.
func assertUpgradeLibraryLoads(t *testing.T, dir string) {
	t.Helper()
	if _, err := template.LoadDeckLibrary(dir, "."); err != nil {
		t.Fatalf("upgraded library %s does not load: %v", dir, err)
	}
}
