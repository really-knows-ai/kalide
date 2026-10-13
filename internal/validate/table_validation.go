package validate

import (
	"fmt"
	"strings"

	"github.com/really-knows-ai/kalide/internal/mdcheck"
	"github.com/really-knows-ai/kalide/internal/template"
)

// This file implements the table-error adaptation for the validator
// (table-errors, gfm-tables). It is the seam between internal/mdcheck, which
// owns the GFM-table syntax, its strict column counts, its declared limits and
// its cell attributes, and the validator's author-facing ValidationError
// (validate.go): validate owns no table rule of its own — it converts the
// enclosing template's decoded `body.tables` option into mdcheck's plain
// options, runs mdcheck's table pass, and turns each positioned table issue
// into a ValidationError.
//
// The enclosing template's decoded option reaches this file as a
// *template.TableRule (its CellAttributes included). It is translated into
// mdcheck's own plain types — mdcheck.TableOptions (mdcheck.Options.Tables) and
// []mdcheck.CellAttributeSpec (mdcheck.Options.CellAttributes) — by
// mdcheckTableOptions and mdcheckCellAttributeSpecs, so the table pass carries
// the effective limits and declared attributes without validate (or mdcheck)
// importing a schema package.
//
// checkTables is the single entry: it runs mdcheck.Check for the body with the
// converted option and adapts every KindTable issue. The wiring of that call
// into checkBody is a separate task; this file only provides the helper.

// tableBodySegment is the within-body path segment every table violation
// carries: a table error is always inside the body, and its path then names the
// offending table position. The enclosing instance chain (a nested section's
// containment path), when present, is prepended by the caller's prefix.
const tableBodySegment = "body"

// tableCellSegment is the within-body path segment a cell-level table
// violation carries — a cell-attribute violation positioned at the offending
// cell (table-cell-attributes). A row-level violation names its row instead.
const tableCellSegment = "cell"

// mdcheckTableOptions converts a template's decoded TableRule into mdcheck's
// own plain TableOptions, the effective limits mdcheck's table parser and table
// pass consume (mdcheck.Options.Tables). A nil rule — a template that does not
// opt in to tables — yields the zero TableOptions; the caller must not pass it
// to mdcheck.Check as a non-nil Tables, which is what opts the body in
// (table-opt-in). Every limit maps one to one.
func mdcheckTableOptions(rule *template.TableRule) mdcheck.TableOptions {
	if rule == nil {
		return mdcheck.TableOptions{}
	}
	return mdcheck.TableOptions{
		MinTables:  rule.MinTables,
		MaxTables:  rule.MaxTables,
		MinColumns: rule.MinColumns,
		MaxColumns: rule.MaxColumns,
		MinRows:    rule.MinRows,
		MaxRows:    rule.MaxRows,
	}
}

// mdcheckCellAttributeSpecs converts a template's decoded CellAttribute
// declarations into mdcheck's own plain CellAttributeSpec descriptors, the
// closed set mdcheck's cell-extension decoder resolves a cell's `{...}` group
// against (mdcheck.Options.CellAttributes). It is a thin wrapper over the
// shared template.CellAttributeSpecs, keeping the historical helper name for
// its existing callers while the conversion body lives in exactly one place.
func mdcheckCellAttributeSpecs(attrs []template.CellAttribute) []mdcheck.CellAttributeSpec {
	return template.CellAttributeSpecs(attrs)
}

// checkTables runs the GFM-table pass for one Markdown body against the
// enclosing template's decoded `body.tables` option and returns every table
// violation as a positioned ValidationError (table-errors). It is the helper
// the body walk (validate.checkBody) calls; wiring it in is a separate task.
//
// body is the body's Markdown source, startLine the 1-based line in file at
// which the body begins (so every violation is file-absolute), and file the
// body's identity. rule is the enclosing template's decoded TableRule — nil
// when the template does not opt in, in which case checkTables reports nothing:
// a table in a non-opted-in body is the opt-out refusal internal/mdcheck emits
// through its ordinary Markdown-subset check (checkDisallowed), not a table
// error. prefix, when non-empty, is the enclosing section instance's
// containment chain and is prepended to each violation's path
// (nested-section-validation).
//
// The rule is translated into mdcheck's plain options by mdcheckTableOptions and
// mdcheckCellAttributeSpecs, and mdcheck.Check is run in BodyMode with them, so
// the body's tables are parsed, their strict column counts and declared limits
// enforced, and every cell's `{...}` group resolved against the declared cell
// attributes. Only KindTable issues are adapted — the opt-out refusal and every
// other Markdown-subset issue belong to the body's ordinary check. Each table
// issue becomes one ValidationError carrying:
//
//   - the file and the issue's line, which points into the table: at the
//     offending body row, at the delimiter row for a header/delimiter mismatch,
//     at the offending cell for a cell-attribute violation, or at the table's
//     first line for a whole-table limit;
//   - the within-body path: `body` for a whole-table limit or the
//     header/delimiter mismatch, `body › rows[i]` for a body row whose cell
//     count differs from the header's, and `body › cell` for a cell-attribute
//     violation (the enclosing instance chain, when any, prepends these). A
//     body row's row index is 0-based over the body rows with the header
//     excluded, so the first body row after the delimiter is rows[0];
//   - the issue's message as What, except that a strict-column-count issue's
//     redundant leading `rows[i]: ` segment — the row index the path already
//     carries — is dropped, so the index renders exactly once; and
//   - the fix as Fix. A limit and a cell-attribute issue carry mdcheck's own
//     guidance; the strict-column-count issues the parser raises carry none, so
//     checkTables supplies one naming the cell count to match the header.
//
// The returned errors are in mdcheck's issue order (the strict column counts in
// source order, then the limits, then the cell attributes), so the same body
// and rule always yield the same ordered list.
func checkTables(file, body string, startLine int, rule *template.TableRule, prefix []string) []ValidationError {
	if rule == nil {
		return nil
	}

	opts := mdcheckTableOptions(rule)
	issues := mdcheck.Check(file, []byte(body), mdcheck.Options{
		Mode:           mdcheck.BodyMode,
		StartLine:      startLine,
		Tables:         &opts,
		CellAttributes: mdcheckCellAttributeSpecs(rule.CellAttributes),
	})
	if len(issues) == 0 {
		return nil
	}

	rows, delimiters := tableStructure(body, startLine)
	var errs []ValidationError
	for _, iss := range issues {
		if iss.Kind != mdcheck.KindTable {
			continue
		}
		errs = append(errs, adaptTableIssue(iss, rows, delimiters, prefix))
	}
	return errs
}

// tableRow is one of a body's table body rows, located within its table: index
// is the row's 0-based position among its table's body rows, the header
// excluded, so the first body row after the delimiter is index 0 (matching the
// index mdcheck's strict-column-count message names), and headerCount is that
// table's header cell count, the cell count the row must match.
type tableRow struct {
	index       int
	headerCount int
}

// tableStructure indexes a body's parsed tables by source line so a table issue
// can be given its within-body path and, for a strict-column-count issue with
// no fix of its own, the header count its message would name. rows maps each
// body row's line to that row's tableRow, whose index is 0-based over the body
// rows with the header excluded (the first body row after the delimiter is
// rows[0]); delimiters maps each table's delimiter-row line to its header cell
// count. The body is parsed with zero TableOptions — the limits do not affect
// what parses — and only the parsed tables are used, never the issues that
// parse also yields.
func tableStructure(body string, startLine int) (rows map[int]tableRow, delimiters map[int]int) {
	tables, _ := mdcheck.ParseTables(body, startLine, mdcheck.TableOptions{})
	rows = make(map[int]tableRow)
	delimiters = make(map[int]int, len(tables))
	for _, t := range tables {
		// parseTable always parses the delimiter row immediately after the
		// header row, so the delimiter's line is the table's line plus one.
		delimiters[t.Line+1] = len(t.Headers)
		for i, row := range t.Rows {
			if len(row) == 0 {
				continue
			}
			rows[row[0].Line] = tableRow{index: i, headerCount: len(t.Headers)}
		}
	}
	return rows, delimiters
}

// adaptTableIssue adapts one KindTable mdcheck.Issue into a ValidationError: the
// file and line carry across, tableIssuePath derives the within-body path,
// tableIssueWhat supplies the author-facing message and tableIssueFix supplies
// the fix (the issue's own guidance, or one checkTables supplies for a
// strict-column-count issue that carries none).
func adaptTableIssue(iss mdcheck.Issue, rows map[int]tableRow, delimiters map[int]int, prefix []string) ValidationError {
	return New(iss.File, iss.Line, tableIssuePath(iss, rows, prefix), tableIssueWhat(iss, rows),
		tableIssueFix(iss, rows, delimiters))
}

// tableIssueWhat returns the author-facing message for a KindTable issue. It is
// the issue's own message, except for a strict-column-count body-row issue:
// mdcheck names the offending row with a leading `rows[i]: ` segment, and
// tableIssuePath already carries that same row as the error's last path
// segment, so keeping both would render the index twice — `rows[0]: rows[0]: 3
// cells, header has 4`. The redundant leading segment is dropped, leaving the
// index to render exactly once, as the path's `rows[0]` followed by the count;
// the path and the message therefore agree. The row is looked up by the issue's
// line, exactly as in tableIssuePath, and a strict-column-count issue that is
// not a body row (the header/delimiter mismatch, positioned on the delimiter
// row) carries no such segment and is returned unchanged.
func tableIssueWhat(iss mdcheck.Issue, rows map[int]tableRow) string {
	if iss.Guidance != "" {
		return iss.Message
	}
	row, ok := rows[iss.Line]
	if !ok {
		return iss.Message
	}
	return strings.TrimPrefix(iss.Message, fmt.Sprintf("rows[%d]: ", row.index))
}

// tableIssuePath returns the within-body path of a KindTable issue: the
// enclosing instance chain (prefix) followed by `body` and then, when the issue
// is positioned within the table, the offending row or cell.
//
// A limit issue and a header/delimiter mismatch are whole-table violations and
// carry only `body`: mdcheck raises both without a row position, a limit issue
// with column 0 and the mismatch on the delimiter row. A body row whose cell
// count differs from the header's is a row violation and carries `rows[i]`, the
// 0-based row index over the body rows with the header excluded, so the first
// body row after the delimiter is rows[0] (matching the index mdcheck's
// message names; tableIssueWhat then drops that index from the message so it
// renders once, on the path). A cell-attribute issue is positioned at the
// offending cell and carries `cell`.
//
// The classification keys on mdcheck's own issue shape: a strict-column-count
// issue is the one the parser raises without guidance (Guidance == ""), while
// limit and cell-attribute issues both carry guidance; among the latter, a
// cell-attribute issue is the only one positioned at a column (Col > 0), a
// limit issue carrying column 0. The row is looked up by the issue's line,
// which the parser sets to the offending row's line.
func tableIssuePath(iss mdcheck.Issue, rows map[int]tableRow, prefix []string) []string {
	path := make([]string, 0, len(prefix)+3)
	path = append(path, prefix...)
	path = append(path, tableBodySegment)

	switch {
	case iss.Guidance == "":
		// A strict-column-count issue from the parser. A body-row mismatch
		// names and is positioned at its row; the header/delimiter mismatch is
		// positioned on the delimiter row and names no row.
		if row, ok := rows[iss.Line]; ok {
			path = append(path, fmt.Sprintf("rows[%d]", row.index))
		}
	case iss.Col > 0:
		// A cell-attribute issue, positioned at the offending cell.
		path = append(path, tableCellSegment)
	}
	return path
}

// tableIssueFix returns the fix for a KindTable issue: the issue's own guidance
// when mdcheck supplied one (a limit or a cell-attribute violation), otherwise
// the fix checkTables must supply for a strict-column-count issue, which the
// parser raises without one. A body-row mismatch is fixed by matching the
// header's cell count, for example `make every row have 4 cells to match the
// header`; a header/delimiter mismatch by matching the delimiter row to the
// header's count. The counts come from the parsed structure (tableStructure);
// when the issue cannot be located, a generic fix is returned rather than none,
// so a table violation always carries a fix (table-errors).
func tableIssueFix(iss mdcheck.Issue, rows map[int]tableRow, delimiters map[int]int) string {
	if iss.Guidance != "" {
		return iss.Guidance
	}
	if row, ok := rows[iss.Line]; ok {
		return fmt.Sprintf("make every row have %d cells to match the header", row.headerCount)
	}
	if n, ok := delimiters[iss.Line]; ok {
		return fmt.Sprintf("give the delimiter row %d cells to match the header", n)
	}
	return "make the table's rows match the header's cell count"
}
