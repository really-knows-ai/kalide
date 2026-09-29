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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
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
