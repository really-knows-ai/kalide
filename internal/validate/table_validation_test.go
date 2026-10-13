package validate

import "testing"

// This file hosts the validate half of the GFM-table deliverable (table-errors,
// gfm-tables). TestCheckTables pins the error adaptation internal/validate
// performs in checkTables: every mdcheck table issue (parse, strict column
// count, declared limit, unknown/cell attribute) becomes a positioned
// ValidationError carrying the file, the table's line, the within-body path
// (body, rows[i], cell) and a fix. TestTableValidationInt drives that adaptation
// through the real validate pipeline (Validate / ValidateAll) over an on-disk
// deck, guarded by testing.Short per the repo's integration convention.
//
// Both are skeletons here and are filled in by gfm-tables/plan.phase-04.task-11
// (TestCheckTables) and task-12 (TestTableValidationInt); the fixtures and the
// full assertions land in those tasks.

// TestCheckTables pins validate.checkTables' adaptation of mdcheck's table
// issues into positioned ValidationErrors.
func TestCheckTables(t *testing.T) {
	// A template that does not opt in to tables (a nil rule) has no table pass
	// to run: checkTables reports nothing, so a table's refusal stays the
	// body's ordinary Markdown-subset check (mdcheck.checkDisallowed).
	if got := checkTables("slides/1-a.md", "| a | b |\n| - | - |\n", 1, nil, nil); got != nil {
		t.Fatalf("checkTables with a nil table rule = %v, want nil", got)
	}
}

// TestTableValidationInt drives table validation through the real validate
// pipeline over an on-disk deck. It reads the real filesystem, so it is skipped
// under -short like its siblings.
func TestTableValidationInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: table validation through the real validate pipeline")
	}

	// The on-disk table fixtures and the Validate / ValidateAll assertions land
	// with gfm-tables/plan.phase-04.task-12.
}
