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
// upgradeValidate; both seams are restored via t.Cleanup. The full body is
// implemented by upgrade/plan.phase-04.task-14, which replaces this placeholder.
func TestUpgradeRestoresMigratedThenInvalidLibrary(t *testing.T) {
	t.Skip("placeholder: implemented by upgrade/plan.phase-04.task-14")
}
