package validate

import (
	"reflect"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
)

// This file hosts the validate half of the GFM-table deliverable (table-errors,
// gfm-tables). TestCheckTables pins the error adaptation internal/validate
// performs in checkTables: every mdcheck table issue (parse, strict column
// count, declared limit, unknown/cell attribute) becomes a positioned
// ValidationError carrying the file, the table's line, the within-body path
// (body, rows[i], cell) and a fix. TestTableValidationInt drives that adaptation
// through the real validate pipeline (Validate / ValidateAll) over an on-disk
// deck, guarded by testing.Short per the repo's integration convention.
//
// TestCheckTables is filled in by gfm-tables/plan.phase-04.task-11;
// TestTableValidationInt's fixtures and full assertions land with task-12.

// TestCheckTables pins validate.checkTables' adaptation of mdcheck's table
// issues into positioned ValidationErrors (table-errors). Each subtest fixes a
// table violation and the exact ValidationError it must become: the file and the
// file-absolute line (into the table), the within-body path (body for a
// whole-table limit or a header/delimiter mismatch, body › rows[i] for a body
// row whose width differs from the header's, body › cell for a cell-attribute
// violation), the message (What) and the fix. Limit and cell-attribute issues
// carry mdcheck's own guidance as the fix; the strict-column-count issues the
// parser raises carry none, so checkTables supplies one naming the cell count to
// match the header. The enclosing instance chain (prefix) prepends the path, and
// startLine offsets every position file-absolutely.
func TestCheckTables(t *testing.T) {
	const file = "slides/1-a.md"

	// A template that does not opt in to tables (a nil rule) has no table pass
	// to run: checkTables reports nothing, so a table's refusal stays the
	// body's ordinary Markdown-subset check (mdcheck.checkDisallowed).
	if got := checkTables(file, "| a | b |\n| - | - |\n", 1, nil, nil); got != nil {
		t.Fatalf("checkTables with a nil table rule = %v, want nil", got)
	}

	// The declared cell attributes the opt-in rule carries: a boolean and an
	// enum with two variants, the candidate set the closest-match suggestion is
	// drawn from.
	cellAttributes := []template.CellAttribute{
		{Name: "bold", Kind: template.FieldBoolean},
		{Name: "highlight", Kind: template.FieldEnum, Variants: []string{"red", "green"}},
	}

	const (
		// A sound table: header and delimiter agree and the body row matches.
		soundTable = "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
		// Header of 2 cells, delimiter of 3: a header/delimiter mismatch.
		mismatchedDelimiter = "| a | b |\n| --- | --- | --- |\n| 1 | 2 |\n"
		// Header of 2 cells, body row of 3: a strict-column-count breach.
		wideRow = "| h1 | h2 |\n| --- | --- |\n| a | b | c |\n"
		// A body cell naming an undeclared attribute, close to `highlight`.
		unknownAttribute = "| h1 | h2 |\n| --- | --- |\n| a {hilight} | b |\n"
	)

	tests := []struct {
		name      string
		body      string
		startLine int
		rule      *template.TableRule
		prefix    []string
		want      []ValidationError
	}{
		{
			name:      "opted-in sound table yields no errors",
			body:      soundTable,
			startLine: 1,
			rule:      &template.TableRule{},
			want:      nil,
		},
		{
			name:      "header/delimiter mismatch is positioned on the delimiter row",
			body:      mismatchedDelimiter,
			startLine: 1,
			rule:      &template.TableRule{},
			want: []ValidationError{New(file, 2, []string{"body"},
				"header has 2 cells, delimiter row has 3",
				"give the delimiter row 2 cells to match the header")},
		},
		{
			name:      "strict column count names the row and supplies the fix",
			body:      wideRow,
			startLine: 1,
			rule:      &template.TableRule{},
			want: []ValidationError{New(file, 3, []string{"body", "rows[1]"},
				"rows[1]: 3 cells, header has 2",
				"make every row have 2 cells to match the header")},
		},
		{
			name:      "limit breach names the limit and carries mdcheck's guidance",
			body:      soundTable,
			startLine: 1,
			rule:      &template.TableRule{MaxColumns: 1},
			want: []ValidationError{New(file, 1, []string{"body"},
				"max_columns: table has 2 columns, maximum is 1",
				"use at most 1 columns")},
		},
		{
			name:      "whole-body limit is positioned at the body's first line",
			body:      "just a paragraph\n",
			startLine: 1,
			rule:      &template.TableRule{MinTables: 1},
			want: []ValidationError{New(file, 1, []string{"body"},
				"min_tables: body has 0 tables, minimum is 1",
				"use at least 1 tables")},
		},
		{
			name:      "unknown cell attribute carries the closest-match suggestion",
			body:      unknownAttribute,
			startLine: 1,
			rule:      &template.TableRule{CellAttributes: cellAttributes},
			want: []ValidationError{New(file, 3, []string{"body", "cell"},
				`unknown cell attribute "hilight": did you mean "highlight"?`,
				"use one of the template's declared cell attributes, or remove the {…} group")},
		},
		{
			name:      "prefix prepends the enclosing instance chain",
			body:      wideRow,
			startLine: 1,
			rule:      &template.TableRule{},
			prefix:    []string{"columns[0]", "blocks[0]"},
			want: []ValidationError{New(file, 3, []string{"columns[0]", "blocks[0]", "body", "rows[1]"},
				"rows[1]: 3 cells, header has 2",
				"make every row have 2 cells to match the header")},
		},
		{
			name:      "startLine positions the offending row file-absolutely",
			body:      wideRow,
			startLine: 10,
			rule:      &template.TableRule{},
			want: []ValidationError{New(file, 12, []string{"body", "rows[1]"},
				"rows[1]: 3 cells, header has 2",
				"make every row have 2 cells to match the header")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkTables(file, tt.body, tt.startLine, tt.rule, tt.prefix)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("checkTables(%q, startLine=%d) =\n  %#v\nwant\n  %#v",
					tt.body, tt.startLine, got, tt.want)
			}
		})
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
