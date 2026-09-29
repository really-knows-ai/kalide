package e2e

// This file is the phase-04 task-10 end-to-end test file for `kalide upgrade`
// (requirements.requirement.deck-library-upgrade,
// global.constraint.upgrade-offline, global.constraint.upgrade-never-clobbers
// and global.constraint.upgrade-never-breaks-project). It drives the real
// kalide binary the harness builds (harness.go) through the whole upgrade path
// an author takes:
//
//   - `kalide init` scaffolds the no-arg seed deck; `kalide init-library
//     ../shared-lib` scaffolds a real library; `kalide init ../shared-lib`
//     scaffolds a deck configured to use that library (`templates:
//     ../shared-lib`);
//   - `kalide upgrade` runs as a bare invocation (no path, no --force) in the
//     project's directory and prints its report: the detected form plus every
//     refreshed path and every left-alone/skipped path;
//   - a second run is a no-op that exits 0, and an author-edited owned file is
//     skipped, named and exits 0;
//   - an external-library deck's configured `templates:` library is left
//     byte-for-byte (and mtime-) untouched and no local templates/ is created;
//   - the upgraded deck is then served by the real binary (`kalide start
//     --no-open`), and the whole flow is offline: the closed-loopback proxy
//     environment for every upgrade run and the assertOffline OS-connection
//     sampler for the served-deck leg (as e2e/external_library_test.go does);
//   - one upgrade run is made with a PATH that holds no git binary, proving
//     upgrade never shells out to git.
//
// This task creates the file's shared scaffolding only: the report parsers, the
// project fixtures, the offline/run helpers and the tree snapshot/unchanged
// assertions below. The concrete assertions of e2e.TestUpgrade are authored by
// plan.phase-04.task-11, which completes the placeholder at the bottom of this
// file; the helpers are named for the cases that task names.
//
// It builds a binary and drives a real server, so it is skipped under -short.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// upgradeDeckMemberPaths is the kalide-owned deck member set refreshScaffold
// visits, in visit order (internal/scaffold/refresh.go deckOwnedMembers): the
// deck-root guide, the local library.yaml, the hello template's three files, the
// default theme and the starter slide. It is the deck form's report path set the
// e2e test asserts `kalide upgrade` names.
var upgradeDeckMemberPaths = []string{
	"AGENTS.md",
	template.TemplatesDir + "/" + template.LibraryFile,
	template.TemplatesDir + "/" + template.SlidesDir + "/hello/" + template.ExampleFile,
	template.TemplatesDir + "/" + template.SlidesDir + "/hello/" + template.LayoutFile,
	template.TemplatesDir + "/" + template.SlidesDir + "/hello/" + template.ManifestFile,
	template.TemplatesDir + "/" + template.ThemesDir + "/" + theme.DefaultName + "/" + template.ThemeStylesheet,
	template.SlidesDir + "/1-hello.md",
}

// upgradeLibraryMemberPaths is the library-owned member set refreshScaffold
// visits, in visit order (internal/scaffold/refresh.go libraryOwnedMembers): the
// library-root guide and the seeded default theme. It is the library form's
// report path set.
var upgradeLibraryMemberPaths = []string{
	"AGENTS.md",
	template.ThemesDir + "/" + theme.DefaultName + "/" + template.ThemeStylesheet,
}

// assertUpgradeReportCounts asserts the printed report's refreshed and skipped
// headings carry the expected counts, pinning the full report shape rather than
// a bare substring.
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

// initUpgradeSeedDeck runs `kalide init` in the harness's clean working
// directory, writing the embedded no-arg hello seed deck, and asserts it exits 0
// with empty stderr. The deck's kalide.yaml carries no `templates:` key, so the
// deck owns its local templates/ seed tree — the deck form `kalide upgrade`
// refreshes.
func initUpgradeSeedDeck(t *testing.T, h *Harness) {
	t.Helper()

	_, stderr, code := h.Run("init")
	if code != 0 {
		t.Fatalf("kalide init exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide init stderr = %q, want empty", stderr)
	}
}

// initUpgradeLibrary runs `kalide init-library rel` (rel relative to the
// harness working directory, e.g. "../shared-lib") and asserts it exits 0 with
// empty stderr. It returns the library root as an absolute path. The library is
// the library form `kalide upgrade` refreshes: a library.yaml plus the seeded
// default theme.
func initUpgradeLibrary(t *testing.T, h *Harness, rel string) string {
	t.Helper()

	_, stderr, code := h.Run("init-library", rel)
	if code != 0 {
		t.Fatalf("kalide init-library %s exit = %d, want 0 (stderr = %q)", rel, code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide init-library %s stderr = %q, want empty", rel, stderr)
	}
	return h.Path(rel)
}

// initUpgradeExternalDeck scaffolds a library at rel (scaffold via
// `kalide init-library`) and, in the harness working directory, a deck
// configured to use it (`kalide init rel`, whose kalide.yaml carries
// `templates: rel`). It returns the deck root and the library root, so a case
// can run `kalide upgrade` in the deck and assert the configured library is
// byte-for-byte and mtime-untouched and no local templates/ is created.
func initUpgradeExternalDeck(t *testing.T, h *Harness, rel string) (deckDir, libDir string) {
	t.Helper()

	libDir = initUpgradeLibrary(t, h, rel)
	if _, stderr, code := h.Run("init", rel); code != 0 {
		t.Fatalf("kalide init %s exit = %d, want 0 (stderr = %q)", rel, code, stderr)
	}
	return h.WorkDir(), libDir
}

// runUpgrade runs the bare `kalide upgrade` invocation in the harness working
// directory and returns its stdout, stderr and exit code. `kalide upgrade` takes
// no path and no options, so no extra arguments are passed; the caller asserts
// the exit code and the report (or the refusal message).
func runUpgrade(t *testing.T, h *Harness) (stdout, stderr string, code int) {
	t.Helper()
	return h.Run("upgrade")
}

// runUpgradeOffline arms the harness's closed-loopback proxy environment and
// then runs `kalide upgrade`. Harness.Command appends extraEnv, so the proxy
// variables reach h.Run("upgrade") exactly as they reach Start; an HTTP(S)
// client in the built binary that honours them fails its connection instead of
// reaching the network. Asserting the injected-offline result is still the
// caller's job.
func runUpgradeOffline(t *testing.T, h *Harness) (stdout, stderr string, code int) {
	t.Helper()

	h.EnableOfflineProxy()
	return runUpgrade(t, h)
}

// runUpgradeWithoutGit runs `kalide upgrade` with PATH pointing at an empty
// directory, which holds no git binary at all. A run that still exits 0 and
// refreshes/skips as before proves `kalide upgrade` never shells out to git
// (were git required, the lookup would fail). It returns the stdout, stderr and
// exit code for the caller to assert.
func runUpgradeWithoutGit(t *testing.T, h *Harness) (stdout, stderr string, code int) {
	t.Helper()

	cmd := h.Command("upgrade")
	cmd.Env = envWith(cmd.Env, "PATH", t.TempDir())
	return runUpgradeCommand(t, cmd)
}

// runUpgradeCommand runs an already-built upgrade command to completion and
// returns its stdout, stderr and exit code, mirroring Harness.Run for the
// helpers that must vary the command's environment (the no-git run).
func runUpgradeCommand(t *testing.T, cmd *exec.Cmd) (stdout, stderr string, code int) {
	t.Helper()

	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	if err == nil {
		return out.String(), errOut.String(), 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), errOut.String(), ee.ExitCode()
	}
	t.Fatalf("e2e: run kalide upgrade: %v", err)
	return "", "", -1
}

// serveUpgradeDeckOffline starts `kalide start --no-open` on the upgraded
// project and arms the assertOffline OS-connection sampler for the run, as
// e2e/external_library_test.go does. The sampler's Stop is registered as a test
// cleanup, so a case that forgets it still reports any outbound connection; it
// is returned so a case can also stop the run explicitly. The proxy environment
// set by an earlier runUpgradeOffline persists in the harness and reaches Start
// too.
func serveUpgradeDeckOffline(t *testing.T, h *Harness) *Offline {
	t.Helper()

	h.Start()
	off := assertOffline(t, h.PID())
	t.Cleanup(off.Stop)

	if got := h.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") || !strings.HasSuffix(got, "/") {
		t.Fatalf("kalide start printed URL %q, want http://127.0.0.1:<port>/", got)
	}
	return off
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
// to prove an external-library deck's configured library was neither read nor
// written, and that a skipped author-edited file stayed byte-for-byte (and
// mtime-) identical.
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
// modification-time identical, so the tree was left untouched.
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

// TestUpgrade is the end-to-end test for `kalide upgrade`
// (upgrade/plan.phase-04.task-11): it drives the real built binary through
// `kalide init`/`kalide init-library`, one or more `kalide upgrade` runs and a
// final `kalide start --no-open`, offline, asserting the report, idempotence,
// that author-edited files are skipped and that an external-library deck's
// configured library is left untouched.
//
// It is three cases, each with its own Harness (and therefore its own clean
// working directory and its own build of cmd/kalide):
//
//   - "seed deck": `kalide init` in the working directory; `kalide upgrade`
//     twice (the report names every owned path skipped and the second run is a
//     byte-for-byte no-op), an author-edited owned file skipped and left
//     untouched, a run with a PATH holding no git, and `kalide start --no-open`
//     serving the upgraded deck under the assertOffline connection sampler;
//   - "scaffolded library": `kalide init-library ../shared-lib`, the reachable
//     refresh write (the absent library guide) and the second-run no-op, run in
//     the library directory through runUpgradeCommand;
//   - "external-library deck": `kalide init-library ../shared-lib` plus
//     `kalide init ../shared-lib`, then `kalide upgrade` in the deck, asserting
//     the configured library is byte-for-byte and mtime-untouched and no local
//     templates/ is created.
//
// Every upgrade run is proxy-armed (runUpgradeOffline / EnableOfflineProxy), so
// a network-honouring client fails its connection, and the served-deck leg adds
// the OS-connection sampler; the PATH-without-git run proves upgrade never
// shells out to git (global.constraint.upgrade-offline).
func TestUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	// Case 1: the no-arg seed deck, scaffolded and served from the harness
	// working directory.
	t.Run("seed deck", func(t *testing.T) {
		h := NewHarness(t)
		initUpgradeSeedDeck(t, h)

		// The freshly scaffolded deck already carries the running binary's
		// version of every owned member, so the first run refreshes nothing and
		// reports every member left alone, naming each one.
		stdout, stderr, code := runUpgradeOffline(t, h)
		if code != 0 {
			t.Fatalf("kalide upgrade exit = %d, want 0 (stdout = %q, stderr = %q)", code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide upgrade stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "kalide upgrade: deck") {
			t.Fatalf("kalide upgrade stdout = %q, want the deck report header", stdout)
		}
		assertUpgradeReportCounts(t, stdout, 0, len(upgradeDeckMemberPaths))
		assertUpgradeReportNamesPaths(t, stdout, upgradeDeckMemberPaths)

		// A second run is a no-op: exit 0, the same report, and the deck tree
		// byte-for-byte and mtime-identical (idempotence).
		before := snapshotUpgradeTree(t, h.WorkDir())
		stdout, stderr, code = runUpgradeOffline(t, h)
		if code != 0 {
			t.Fatalf("second kalide upgrade exit = %d, want 0 (stdout = %q, stderr = %q)", code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("second kalide upgrade stderr = %q, want empty", stderr)
		}
		assertUpgradeReportCounts(t, stdout, 0, len(upgradeDeckMemberPaths))
		assertUpgradeTreeUnchanged(t, h.WorkDir(), before)

		// An author-edited owned member is skipped, named and exits 0, left
		// byte-for-byte untouched (never clobbered).
		authorGuide := []byte("# my own deck guide\n\nDo not overwrite me.\n")
		h.WriteFile("AGENTS.md", authorGuide)
		before = snapshotUpgradeTree(t, h.WorkDir())
		stdout, stderr, code = runUpgradeOffline(t, h)
		if code != 0 {
			t.Fatalf("kalide upgrade over an author-edited guide exit = %d, want 0 (stdout = %q, stderr = %q)",
				code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide upgrade over an author-edited guide stderr = %q, want empty", stderr)
		}
		assertUpgradeReportCounts(t, stdout, 0, len(upgradeDeckMemberPaths))
		if !upgradeReportNamesPath(stdout, "AGENTS.md") {
			t.Errorf("kalide upgrade stdout = %q, want the skipped author-edited AGENTS.md named", stdout)
		}
		if got, err := os.ReadFile(h.Path("AGENTS.md")); err != nil {
			t.Fatalf("read author-edited AGENTS.md: %v", err)
		} else if !bytes.Equal(got, authorGuide) {
			t.Errorf("author-edited AGENTS.md = %q, want the author bytes untouched", got)
		}
		assertUpgradeTreeUnchanged(t, h.WorkDir(), before)

		// One run with a PATH that holds no git at all: upgrade still exits 0
		// and reports exactly as before, proving it never shells out to git.
		stdout, stderr, code = runUpgradeWithoutGit(t, h)
		if code != 0 {
			t.Fatalf("kalide upgrade without git on PATH exit = %d, want 0 (stdout = %q, stderr = %q)",
				code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide upgrade without git on PATH stderr = %q, want empty", stderr)
		}
		assertUpgradeReportCounts(t, stdout, 0, len(upgradeDeckMemberPaths))

		// The upgraded deck still validates and serves. The proxy environment
		// set by runUpgradeOffline persists in the harness and reaches Start;
		// assertOffline samples the process's OS connections for the run.
		off := serveUpgradeDeckOffline(t, h)
		body, err := h.GetString("/")
		if err != nil {
			t.Fatalf("GET / on the upgraded deck: %v", err)
		}
		if strings.Contains(body, "Deck error") {
			t.Fatalf("served page is the error page, want the upgraded deck:\n%s", body)
		}
		if !strings.Contains(body, "My presentation") {
			t.Errorf("served upgraded deck page does not carry the deck title:\n%s", body)
		}
		h.Stop()
		off.Stop()
	})

	// Case 2: a scaffolded template library, upgraded in its own directory. The
	// library guide is removed first so the run has the one refresh write
	// reachable from the built binary (an absent AGENTS.md is written); the
	// second run is then the no-op idempotence case.
	t.Run("scaffolded library", func(t *testing.T) {
		h := NewHarness(t)
		libDir := initUpgradeLibrary(t, h, "../shared-lib")

		guide, err := os.ReadFile(filepath.Join(libDir, "AGENTS.md"))
		if err != nil {
			t.Fatalf("read library AGENTS.md: %v", err)
		}
		if err := os.Remove(filepath.Join(libDir, "AGENTS.md")); err != nil {
			t.Fatalf("remove library AGENTS.md: %v", err)
		}

		h.EnableOfflineProxy()
		cmd := h.Command("upgrade")
		cmd.Dir = libDir
		stdout, stderr, code := runUpgradeCommand(t, cmd)
		if code != 0 {
			t.Fatalf("kalide upgrade in library exit = %d, want 0 (stdout = %q, stderr = %q)", code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide upgrade in library stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "kalide upgrade: library") {
			t.Fatalf("kalide upgrade in library stdout = %q, want the library report header", stdout)
		}
		assertUpgradeReportCounts(t, stdout, 1, len(upgradeLibraryMemberPaths)-1)
		assertUpgradeReportNamesPaths(t, stdout, upgradeLibraryMemberPaths)
		if got, err := os.ReadFile(filepath.Join(libDir, "AGENTS.md")); err != nil {
			t.Fatalf("read written library AGENTS.md: %v", err)
		} else if !bytes.Equal(got, guide) {
			t.Errorf("written library AGENTS.md does not equal the scaffolded library guide")
		}

		// The second run changes nothing.
		before := snapshotUpgradeTree(t, libDir)
		cmd = h.Command("upgrade")
		cmd.Dir = libDir
		stdout, stderr, code = runUpgradeCommand(t, cmd)
		if code != 0 {
			t.Fatalf("second kalide upgrade in library exit = %d, want 0 (stdout = %q, stderr = %q)",
				code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("second kalide upgrade in library stderr = %q, want empty", stderr)
		}
		assertUpgradeReportCounts(t, stdout, 0, len(upgradeLibraryMemberPaths))
		assertUpgradeTreeUnchanged(t, libDir, before)
	})

	// Case 3: a deck configured to use an external library. Upgrade runs in the
	// deck; its only owned member is the deck-root guide, and the configured
	// library is never touched.
	t.Run("external-library deck", func(t *testing.T) {
		h := NewHarness(t)
		_, libDir := initUpgradeExternalDeck(t, h, "../shared-lib")

		before := snapshotUpgradeTree(t, libDir)

		stdout, stderr, code := runUpgradeOffline(t, h)
		if code != 0 {
			t.Fatalf("kalide upgrade in external-library deck exit = %d, want 0 (stdout = %q, stderr = %q)",
				code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("kalide upgrade in external-library deck stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "kalide upgrade: deck") {
			t.Fatalf("kalide upgrade in external-library deck stdout = %q, want the deck report header", stdout)
		}
		// An external-library deck owns only its deck-root guide.
		assertUpgradeReportCounts(t, stdout, 0, 1)
		if !upgradeReportNamesPath(stdout, "AGENTS.md") {
			t.Errorf("kalide upgrade stdout = %q, want the skipped deck guide named", stdout)
		}

		// The configured `templates:` library is byte-for-byte and
		// mtime-identical: nothing under it was written, refreshed or deleted.
		assertUpgradeTreeUnchanged(t, libDir, before)

		// No local templates/ is created for an external-library deck.
		if _, err := os.Lstat(h.Path(template.TemplatesDir)); !os.IsNotExist(err) {
			t.Errorf("Lstat(%s) err = %v, want the external-library deck to have no local templates/",
				template.TemplatesDir, err)
		}

		// A second run is the no-op idempotence case, and the configured library
		// stays untouched.
		deckBefore := snapshotUpgradeTree(t, h.WorkDir())
		stdout, stderr, code = runUpgradeOffline(t, h)
		if code != 0 {
			t.Fatalf("second kalide upgrade in external-library deck exit = %d, want 0 (stdout = %q, stderr = %q)",
				code, stdout, stderr)
		}
		if stderr != "" {
			t.Errorf("second kalide upgrade in external-library deck stderr = %q, want empty", stderr)
		}
		assertUpgradeReportCounts(t, stdout, 0, 1)
		assertUpgradeTreeUnchanged(t, h.WorkDir(), deckBefore)
		assertUpgradeTreeUnchanged(t, libDir, before)
	})
}

// assertUpgradeReportNamesPaths asserts the printed report names every rel in
// rels as a path line, so a case pins the full refreshed/skipped path set rather
// than a single path.
func assertUpgradeReportNamesPaths(t *testing.T, stdout string, rels []string) {
	t.Helper()

	for _, rel := range rels {
		if !upgradeReportNamesPath(stdout, rel) {
			t.Errorf("upgrade report = %q, want the path %q named", stdout, rel)
		}
	}
}
