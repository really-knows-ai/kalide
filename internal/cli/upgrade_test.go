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
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/scaffold"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// TestRunUpgrade is the command-surface suite for `kalide upgrade`. The
// scaffolding it relies on is in this file (task 8); the cases themselves are
// authored by plan.phase-04.task-9: both/neither-manifest detection errors, the
// usage errors for a path argument and a --force-style flag, a clean refresh and
// report, idempotence, an author-edited file skipped and named, an
// external-library deck's configured library left byte-for-byte untouched, the
// unknown/newer library-format refusal, the post-refresh+migrate validation
// failure and its undo, and the upgraded project still loading through the
// loaders `kalide start` uses. Until then this placeholder keeps the file
// compiling and green.
func TestRunUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: `kalide upgrade` writes real project trees to disk")
	}
	t.Skip("cli.TestRunUpgrade is completed by plan.phase-04.task-9")
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
