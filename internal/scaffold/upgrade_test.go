package scaffold

// This file hosts the internal unit tests for scaffold.Upgrade
// (internal/scaffold/upgrade.go), the `kalide upgrade` orchestration entry point
// (requirements.requirement.deck-library-upgrade). UpgradeReport and Upgrade are
// covered at the command surface by internal/cli's handler tests and end to end
// by e2e; this file owns the Upgrade-internal case the core implementer's scope
// (no *_test.go) could not add: the feedback-26 item-4 proof that Upgrade undoes
// the MIGRATION half — not only the refresh half — when the post-refresh+migrate
// end-to-end validation fails
// (global.constraint.upgrade-never-breaks-project).
//
// The cases are unit tier: every fixture is built in a t.TempDir, the tests
// reach no network, no server and no git, and they run under -short.
//
// The tests reuse, and do not restate, the migration half's fixtures and helpers
// in migrate_test.go — migrateLibraryFixture builds a real format-1 library,
// migrateSetTarget raises migrationTargetFormat, migrateAssertSameTree and
// snapshot compare trees byte-for-byte, and migrateLibraryWith is the seam that
// drives a chain of synthetic migrations. Upgrade's own test-overridable seams
// are the package-level vars upgradeMigrate and upgradeValidate in upgrade.go:
// production always runs their defaults (migrateLibrary and
// validateUpgradedProject), only test files replace them, and no fake migration
// is ever added to registeredMigrations() (TestRegisteredMigrationsStaysEmpty
// guards that; migrateLibraryWith is the only test entry for a synthetic step).

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/really-knows-ai/kalide/internal/displaypath"
)

// TestUpgradeRestoresMigratedThenInvalidLibrary proves Upgrade's end-to-end undo
// covers the migration half, not only the refresh half: a library whose
// migration half actually commits a change and whose post-refresh+migrate
// end-to-end validation then fails is restored byte-for-byte to its pre-run
// state, and the migration's file is gone
// (global.constraint.upgrade-never-breaks-project).
//
// It drives the migrateLibraryWith seam through upgradeMigrate with a synthetic
// step that writes migrated.txt and raises the target format with
// migrateSetTarget, then forces the post-migration failure through
// upgradeValidate; both seams are restored via t.Cleanup.
func TestUpgradeRestoresMigratedThenInvalidLibrary(t *testing.T) {
	dir := migrateLibraryFixture(t)

	// Raise the migration target above the library's format (1) so the chain
	// actually runs; migrateSetTarget restores the production value on cleanup.
	migrateSetTarget(t, 2)

	migrated := filepath.Join(dir, "migrated.txt")

	// Replace Upgrade's migration half with a closure driving the
	// migrateLibraryWith seam: one synthetic step at source format 1 that
	// commits a change (writes migrated.txt). The step leaves library.yaml's
	// format anchor untouched, because the template loader only accepts
	// format 1 — the point here is a migration that truly commits, not a
	// format bump. No fake migration is added to registeredMigrations().
	originalMigrate := upgradeMigrate
	upgradeMigrate = func(root string) error {
		steps := []formatMigration{{sourceFormat: 1, apply: func(root string) error {
			return os.WriteFile(filepath.Join(root, "migrated.txt"), []byte("migrated\n"), 0o644)
		}}}
		return migrateLibraryWith(root, steps)
	}
	t.Cleanup(func() { upgradeMigrate = originalMigrate })

	// Force the post-refresh+migrate end-to-end validation to fail. Assert
	// in flight that the migration half did commit, so the undo this test
	// checks really covers a completed migration rather than an empty one.
	sentinel := errors.New("synthetic end-to-end validation failure")
	originalValidate := upgradeValidate
	upgradeValidate = func(root string, _ projectKind) error {
		if _, err := os.Stat(migrated); err != nil {
			t.Errorf("upgradeValidate: %s not present when validation ran: %v; the migration half did not commit a change", migrated, err)
		}
		return sentinel
	}
	t.Cleanup(func() { upgradeValidate = originalValidate })

	before := snapshot(t, dir)

	report, err := Upgrade(dir)
	if err == nil {
		t.Fatalf("Upgrade(%s) error = nil, want the forced end-to-end validation failure", dir)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Upgrade(%s) error = %v, want it to be the forced validation failure", dir, err)
	}
	if report != nil {
		t.Errorf("Upgrade(%s) report = %+v, want nil on failure", dir, report)
	}

	// The migration's committed file is gone, and the whole tree — including
	// everything the refresh half may have rewritten — is byte-for-byte the
	// pre-run tree: Upgrade's snapshotTree undo covers the migration half.
	if _, statErr := os.Lstat(migrated); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("Lstat(%s) err = %v, want fs.ErrNotExist: Upgrade must remove the migration's file", migrated, statErr)
	}
	migrateAssertSameTree(t, dir, before)
}

// TestUpgradeMigratesAgentGuide proves the known-released-guide migration runs
// through the full scaffold.Upgrade orchestration, not only refreshScaffold
// (requirements.requirement.agent-guide-reference,
// requirements.requirement.deck-library-upgrade): a project whose AGENTS.md is
// byte-identical to a catalogued released full guide is migrated to the short
// pointer and the current AGENTS_KALIDE.md full guide is written, with the
// report naming the migrated path; the upgraded project still validates
// end-to-end. The released full guide is presented through the refreshSetCatalog
// seam, because the embedded catalogue holds digests only (never historical
// contents).
func TestUpgradeMigratesAgentGuide(t *testing.T) {
	t.Run("deck", func(t *testing.T) {
		root := refreshSeedDeck(t)
		pointer := refreshAbs(root, agentsGuideName)
		full := refreshAbs(root, agentGuideFullGuideName)
		// The migration scenario: a released full guide at AGENTS.md and no
		// AGENTS_KALIDE.md, so Upgrade both migrates the pointer and writes the
		// current full guide.
		if err := os.Remove(full); err != nil {
			t.Fatalf("remove %s: %v", agentGuideFullGuideName, err)
		}
		old := []byte("# an older released kalide deck full guide\n\nOld guidance.\n")
		refreshWrite(t, pointer, old)
		refreshSetCatalog(t, refreshCatalogFixture{deckGuideIdentity: {old}})

		report, err := Upgrade(root)
		if err != nil {
			t.Fatalf("Upgrade(%s) error = %v, want the deck upgraded", root, err)
		}
		if report.Kind != "deck" {
			t.Errorf("Upgrade(%s) report.Kind = %q, want %q", root, report.Kind, "deck")
		}
		if got, want := refreshRead(t, pointer), mustReadSeed(agentsGuideName); !bytes.Equal(got, want) {
			t.Errorf("released full guide at %s was not migrated to the short pointer", agentsGuideName)
		}
		if got, want := refreshRead(t, full), mustReadSeed(agentGuideFullGuideName); !bytes.Equal(got, want) {
			t.Errorf("the current %s full guide was not written", agentGuideFullGuideName)
		}
		for _, rel := range []string{agentsGuideName, agentGuideFullGuideName} {
			if !refreshIn(report.Refreshed, rel) {
				t.Errorf("report.Refreshed = %v, want it to name the migrated/written %s", report.Refreshed, rel)
			}
		}
	})

	t.Run("library", func(t *testing.T) {
		root := refreshLibrary(t)
		pointer := refreshAbs(root, agentsGuideName)
		full := refreshAbs(root, agentGuideFullGuideName)
		if err := os.Remove(full); err != nil {
			t.Fatalf("remove %s: %v", agentGuideFullGuideName, err)
		}
		old := []byte("# an older released kalide library full guide\n\nOld guidance.\n")
		refreshWrite(t, pointer, old)
		refreshSetCatalog(t, refreshCatalogFixture{libraryGuideIdentity: {old}})

		report, err := Upgrade(root)
		if err != nil {
			t.Fatalf("Upgrade(%s) error = %v, want the library upgraded", root, err)
		}
		if report.Kind != "library" {
			t.Errorf("Upgrade(%s) report.Kind = %q, want %q", root, report.Kind, "library")
		}
		if got, want := refreshRead(t, pointer), mustReadLibraryGuide(); !bytes.Equal(got, want) {
			t.Errorf("released full guide at %s was not migrated to the short pointer", agentsGuideName)
		}
		if got, want := refreshRead(t, full), mustReadLibraryFullGuide(); !bytes.Equal(got, want) {
			t.Errorf("the current %s full guide was not written", agentGuideFullGuideName)
		}
		for _, rel := range []string{agentsGuideName, agentGuideFullGuideName} {
			if !refreshIn(report.Refreshed, rel) {
				t.Errorf("report.Refreshed = %v, want it to name the migrated/written %s", report.Refreshed, rel)
			}
		}
	})
}

// TestRefreshUndoRevertNativePath pins the rollback error's path spelling: a
// refreshed path the undo never recorded is reported in the host OS's native
// separator, not the forward-slash identity path the undo keys by
// (native-display-paths). It drives refreshUndo.revert directly, since the
// unrecorded path is the reachable failure at this point.
func TestRefreshUndoRevertNativePath(t *testing.T) {
	root := t.TempDir()
	undo := &refreshUndo{root: root, originals: map[string]undoOriginal{}}
	const rel = "templates/slides/hello/template.yaml"
	cause := errors.New("synthetic refresh failure")

	err := undo.revert([]string{rel}, cause)
	if err == nil {
		t.Fatalf("revert(%q) error = nil, want the unrecorded path reported", rel)
	}
	if !errors.Is(err, cause) {
		t.Errorf("revert(%q) error = %v, want it to wrap the cause", rel, err)
	}
	if want := displaypath.Display("", rel); !strings.Contains(err.Error(), want) {
		t.Errorf("revert error = %q, want it to name the native path %q", err, want)
	}
	if !strings.Contains(err.Error(), "not recorded before the refresh") {
		t.Errorf("revert error = %q, want the unrecorded-path reason", err)
	}
}

// TestRefreshUndoRevertRestoreErrorNativePath pins the rollback error's spelling
// when a restore actually fails on disk: the blocked parent directory is named
// in the host OS's native separator, never slash-normalised
// (native-display-paths).
func TestRefreshUndoRevertRestoreErrorNativePath(t *testing.T) {
	root := t.TempDir()
	const rel = "templates/slides/hello/template.yaml"
	// Block the member's parent directory with a regular file, so the restore's
	// MkdirAll fails and the failure names the native parent path.
	blocker := filepath.Join(root, "templates", "slides")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	undo := &refreshUndo{root: root, originals: map[string]undoOriginal{
		rel: {existed: true, data: []byte("old\n"), mode: 0o644, modTime: time.Now()},
	}}
	cause := errors.New("synthetic refresh failure")

	err := undo.revert([]string{rel}, cause)
	if err == nil {
		t.Fatalf("revert(%q) error = nil, want the blocked restore reported", rel)
	}
	if !errors.Is(err, cause) {
		t.Errorf("revert(%q) error = %v, want it to wrap the cause", rel, err)
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if want := "create " + filepath.Dir(target); !strings.Contains(err.Error(), want) {
		t.Errorf("revert error = %q, want it to name the native parent %q", err, want)
	}
}
