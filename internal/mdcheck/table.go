// This file implements the GFM table parser: the syntax layer that reads the
// GitHub Flavored Markdown tables a template body may contain once the body's
// template opts in through its `body.tables` option (gfm-tables, table-syntax,
// table-limits).
//
// A table is a header row, a delimiter row and zero or more body rows, with
// optional leading and trailing pipes and every cell trimmed of surrounding
// spaces. The delimiter row fixes each column's alignment: `---`, `:---` and
// `---:` select none, left and right, and `:---:` centres the column. A
// backslash-escaped `\|` is a literal pipe character, never a cell separator;
// an *unescaped* `|` separates cells even inside an inline code span, so a pipe
// written in a code span must be escaped to stay literal.
//
// The parser is strict about column counts. Every body row must have exactly as
// many cells as the header row, and the delimiter row must agree with the
// header row too: a count mismatch is a table error, never a paragraph. Because
// a table is recognised by its delimiter row, a header/delimiter mismatch is
// reported against the table rather than falling back to prose.
//
// The effective limits — the fewest and most tables a body may contain, and the
// fewest and most columns and rows a table may have — are carried in as
// TableOptions, mdcheck's own plain limits type so the parser imports no schema
// package. Each parsed table is returned as a Table of header cells,
// per-column alignment and body rows of TableCell, and each breached count or
// limit is returned as one positioned Issue naming the limit and the actual
// count.
//
// Every cell records the 1-based line and column of its trimmed
// inline-Markdown source within the body, so a violation can be positioned at
// the offending row or cell rather than at the start of the table.

package mdcheck

import (
	"fmt"
	"strings"
)

// TableOptions carries the effective GFM-table limits for a body: the fewest
// and most tables it may contain, and the fewest and most columns and rows one
// of its tables may have. It is mdcheck's own plain limits type, so the parser
// and the validator can carry limits without importing a schema package
// (table-limits).
//
// The values are the effective limits: the caller — the template loader or the
// validator converting a template's TableRule — supplies them, and mdcheck
// applies no defaults of its own beyond what the caller passes. The zero value
// therefore imposes no bound at all: a `Min*` field of 0 is no minimum and a
// `Max*` field of 0 is unbounded.
type TableOptions struct {
	// MinTables and MaxTables bound the number of tables in one body.
	// MinTables 0 is no minimum; MaxTables 0 is unbounded.
	MinTables int
	MaxTables int

	// MinColumns and MaxColumns bound one table's column count, the header
	// row included. MinColumns 0 is no minimum; MaxColumns 0 is unbounded.
	MinColumns int
	MaxColumns int

	// MinRows and MaxRows bound one table's body rows, the header row
	// excluded. MinRows 0 is no minimum; MaxRows 0 is unbounded.
	MinRows int
	MaxRows int
}

// TableCell is one cell of a parsed GFM table: the cell's trimmed
// inline-Markdown source together with the 1-based position at which that text
// begins in the body the table was parsed from.
//
// Text holds exactly the source between the cell's separators, with the
// surrounding spaces trimmed — it is *inline* Markdown, not a rendered
// fragment, so a later layer renders it (and decodes any cell extensions)
// rather than this parser. One piece of table syntax is resolved here: a
// backslash-escaped pipe, `\|`, is stored as a plain `|`, because the backslash
// belongs to the table grammar — it stops the pipe from separating two cells —
// and not to the cell's own Markdown. Every other backslash escape is left
// untouched for the inline renderer.
//
// Line and Column are 1-based and point at the first character of Text within
// the body, so a violation can be positioned at the offending cell rather than
// at the table or its row. Column counts bytes from the start of Line. A cell
// whose position could not be derived carries zero.
type TableCell struct {
	// Text is the cell's trimmed inline-Markdown source. A table-syntax `\|`
	// has already been turned into a literal `|`; every other escape is
	// preserved for the inline renderer.
	Text string

	// Line is the 1-based line in the body at which Text begins.
	Line int

	// Column is the 1-based byte column within Line at which Text begins.
	Column int
}

// Table is one parsed GFM table: its header cells, the alignment of each
// column, its body rows of cells, and the 1-based body line of its header row
// (gfm-tables, table-syntax).
//
// A table is recognised by its delimiter row and parsed in full — header, then
// delimiter, then body rows — so the header cells and the per-column alignment
// are always present for a returned table. The parser is strict about column
// counts (table-limits): the delimiter row agrees with the header row and every
// body row has exactly as many cells as the header, so Headers, Align and every
// row in Rows share one length.
type Table struct {
	// Headers is the header row's cells, one per column, in column order.
	Headers []TableCell

	// Align is the per-column alignment taken from the delimiter row, one
	// entry per column in the same order as Headers. Each entry is a plain
	// string: "" for no alignment, "left", "center" or "right" for `:---`,
	// `:---:` and `---:` respectively.
	Align []string

	// Rows is the body rows, source-ordered and including no header row. Each
	// row holds one TableCell per column, in the same column order as Headers.
	Rows [][]TableCell

	// Line is the 1-based line in the body at which the header row begins.
	Line int
}

// splitTableRow splits one table row into its cells under the GFM table row
// grammar: an optional leading pipe and an optional trailing pipe are dropped,
// cells are separated by unescaped pipes, and each cell's surrounding spaces
// and tabs are trimmed. A backslash-escaped pipe, `\|`, is a literal `|` inside
// a cell rather than a separator, and — per GFM — an *unescaped* pipe separates
// cells even inside an inline code span, so a pipe in a code span must be
// escaped to stay literal. Every other backslash escape is left untouched for
// the inline renderer.
//
// Empty cells are kept: `a || b` is three cells with an empty one in the middle.
// The optional leading and trailing pipes add no empty cell, so `| a | b |` is
// two cells, not four.
//
// Text is the cell's trimmed inline-Markdown source with each table-syntax `\|`
// already turned into a plain `|`. Column is the 1-based byte column within
// line of the first non-space byte of that trimmed text, so a violation can be
// positioned at the cell. Line is left zero: the caller knows which line the row
// came from and sets it on the returned cells.
//
// splitTableRow performs no validation — it neither checks the delimiter row's
// alignment grammar nor enforces the strict per-column count. It is the
// syntactic splitter the table parser builds on.
func splitTableRow(line string) []TableCell {
	n := len(line)

	// A leading pipe is optional and adds no empty first cell.
	start := 0
	i := 0
	for i < n && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i < n && line[i] == '|' {
		start = i + 1
	}

	// A trailing pipe is optional and adds no empty last cell. Trailing
	// spaces and tabs sit outside it, so scan back over them first.
	end := n
	j := n - 1
	for j >= start && (line[j] == ' ' || line[j] == '\t') {
		j--
	}
	if j >= start && line[j] == '|' && !pipeEscapedAt(line, j) {
		end = j
	}

	var cells []TableCell
	cellStart := start
	for k := start; k < end; k++ {
		if line[k] == '|' && !pipeEscapedAt(line, k) {
			cells = append(cells, tableRowCell(line, cellStart, k))
			cellStart = k + 1
		}
	}
	cells = append(cells, tableRowCell(line, cellStart, end))
	return cells
}

// tableRowCell builds the TableCell for the source line[from:to]: it trims the
// segment's surrounding spaces and tabs, resolves every table-syntax `\|` to a
// literal `|`, and records the 1-based byte column of the trimmed text. An
// empty cell records the column just past its (empty) trimmed span.
func tableRowCell(line string, from, to int) TableCell {
	for from < to && (line[from] == ' ' || line[from] == '\t') {
		from++
	}
	for to > from && (line[to-1] == ' ' || line[to-1] == '\t') {
		to--
	}
	return TableCell{Text: unescapeTablePipes(line[from:to]), Column: from + 1}
}

// pipeEscapedAt reports whether the pipe at index i in line is escaped by a
// preceding backslash. A pipe is escaped when the run of backslashes
// immediately before it has odd length: `\|` and `\\\|` are literal pipes,
// while in `\\|` the second backslash is itself escaped and the pipe separates
// cells.
func pipeEscapedAt(line string, i int) bool {
	if i < 0 || i >= len(line) || line[i] != '|' {
		return false
	}
	backslashes := 0
	for k := i - 1; k >= 0 && line[k] == '\\'; k-- {
		backslashes++
	}
	return backslashes%2 == 1
}

// unescapeTablePipes turns every table-syntax `\|` in a cell's source into a
// literal `|`, leaving every other backslash escape untouched for the inline
// renderer. Only a backslash immediately preceding a pipe is consumed; a pair
// of backslashes stays as written.
func unescapeTablePipes(s string) string {
	if !strings.Contains(s, `\|`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '|' {
			b.WriteByte('|')
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// KindTable classifies an issue raised by this GFM-table parser and the table
// pass built on it: a strict column-count violation, a breached table limit or
// a cell-attribute violation (gfm-tables, table-errors, table-limits). It marks
// the issues a table pass surfaces so they stay distinct from the ordinary
// Markdown-subset checks and are reported through the table path.
const KindTable Kind = "table"

// parseTable parses the GFM table whose header row is lines[start]. The caller
// has already established — through the delimiter-row test — that lines[start+1]
// is a delimiter row. lines is the body split into lines and firstLine is the
// 1-based number of the line formed by lines[0], so the line of lines[i] is
// firstLine+i.
//
// The header cells come from lines[start] and the per-column alignment from
// lines[start+1], whose cells are each `:?-+:?` at least one hyphen long:
// `:---` is left, `:---:` centre, `---:` right and `---` none. The body rows
// continue from lines[start+2] until the first line that begins another block —
// a blank line, an ATX heading, a block quote, a fence, a list item, a thematic
// break or an HTML block — or the end of the body (table-syntax).
//
// The parse is strict about column counts (table-limits): the delimiter row
// must have the header's cell count and every body row must have the header's
// cell count exactly. A header/delimiter mismatch is one Issue positioned at
// the delimiter row; a body row of the wrong width is one Issue positioned at
// that row and naming it with its 1-based body-row index, for example
// `rows[2]: 3 cells, header has 4`. A mismatch never demotes the table to a
// paragraph.
//
// It returns the parsed Table (every cell's Line set from firstLine and its
// Column from the row source), the Issues in source order — nil when the table
// is sound — and the index of the first line after the table, so the caller can
// resume scanning there. parseTable sets no Issue File: the caller supplies the
// body's own identity. When an Issue is returned the table is malformed, so its
// Rows may hold rows of differing widths; only a sound table has Headers, Align
// and every row the same length.
func parseTable(lines []string, start int, firstLine int) (Table, []Issue, int) {
	if start < 0 || start+1 >= len(lines) {
		return Table{}, nil, start + 1
	}

	headers := splitTableRow(lines[start])
	for i := range headers {
		headers[i].Line = firstLine + start
	}
	delimCells := splitTableRow(lines[start+1])

	// The header fixes the column count, so every alignment belongs to a
	// header column: a delimiter cell past the header's count is dropped and a
	// missing one leaves that column with no alignment.
	align := make([]string, len(headers))
	for i := range align {
		if i < len(delimCells) {
			align[i] = tableDelimiterAlign(delimCells[i].Text)
		}
	}

	table := Table{Headers: headers, Align: align, Line: firstLine + start}

	var issues []Issue
	if len(delimCells) != len(headers) {
		issues = append(issues, Issue{
			Line:    firstLine + start + 1,
			Col:     rowColumn(lines[start+1]),
			Kind:    KindTable,
			Message: fmt.Sprintf("header has %d cells, delimiter row has %d", len(headers), len(delimCells)),
		})
	}

	i := start + 2
	rowIndex := 0
	for ; i < len(lines); i++ {
		if tableBlockStart(lines[i]) {
			break
		}
		rowIndex++
		cells := splitTableRow(lines[i])
		for j := range cells {
			cells[j].Line = firstLine + i
		}
		table.Rows = append(table.Rows, cells)
		if len(cells) != len(headers) {
			issues = append(issues, Issue{
				Line:    firstLine + i,
				Col:     rowColumn(lines[i]),
				Kind:    KindTable,
				Message: fmt.Sprintf("rows[%d]: %d cells, header has %d", rowIndex, len(cells), len(headers)),
			})
		}
	}
	return table, issues, i
}

// rowColumn returns the 1-based byte column of a row's first non-space, non-tab
// byte, or 1 for a blank row. It is where parseTable positions a row-level
// Issue, so the line points into the offending row rather than at the table.
func rowColumn(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i + 1
		}
	}
	return 1
}

// tableDelimiterAlign reads one delimiter-row cell's alignment: `:---` is
// left, `:---:` centre, `---:` right and `---` none. The cell has already been
// trimmed by splitTableRow. A cell that is not a valid `:?-+:?` delimiter cell
// yields no alignment; the caller has already established that the row is a
// delimiter row, so its own test owns that rejection.
func tableDelimiterAlign(cell string) string {
	s := cell
	if s == "" {
		return ""
	}
	left := s[0] == ':'
	right := s[len(s)-1] == ':' && len(s) > 1
	mid := s
	if left {
		mid = mid[1:]
	}
	if right && len(mid) > 0 {
		mid = mid[:len(mid)-1]
	}
	if mid == "" || strings.Trim(mid, "-") != "" {
		return ""
	}
	switch {
	case left && right:
		return "center"
	case left:
		return "left"
	case right:
		return "right"
	}
	return ""
}

// tableBlockStart reports whether line begins a block other than the table's
// own body rows, ending the table (table-syntax): a blank line, an ATX heading,
// a block quote, a fenced code block, a list item, a thematic break or an HTML
// block. Leading spaces and tabs are ignored, so an indented block still ends
// the table.
func tableBlockStart(line string) bool {
	s := strings.TrimLeft(line, " \t")
	if s == "" {
		return true // blank line
	}
	switch {
	case strings.HasPrefix(s, "#"): // ATX heading
		return true
	case strings.HasPrefix(s, ">"): // block quote
		return true
	case strings.HasPrefix(s, "```"), strings.HasPrefix(s, "~~~"): // fenced code
		return true
	case tableListMarkerAt(s) >= 0: // list item
		return true
	case strings.HasPrefix(s, "<"): // HTML block
		return true
	}
	_, ok := thematicBreakMarker([]byte(line))
	return ok
}

// tableListMarkerAt reports whether s begins a CommonMark list item — a bullet
// `-`, `*` or `+`, or an ordered marker of up to nine digits followed by `.` or
// `)` — and returns the marker's byte length, or -1 when s does not begin a
// list item. The marker must be followed by a space, a tab or the end of the
// line, so a table row such as `- | -` still ends the table while `-|-` does
// not.
func tableListMarkerAt(s string) int {
	if s == "" {
		return -1
	}
	switch s[0] {
	case '-', '*', '+':
		if len(s) == 1 || s[1] == ' ' || s[1] == '\t' {
			return 1
		}
		return -1
	}
	digits := 0
	for digits < len(s) && s[digits] >= '0' && s[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits > 9 {
		return -1
	}
	if digits >= len(s) || (s[digits] != '.' && s[digits] != ')') {
		return -1
	}
	if digits+1 < len(s) && s[digits+1] != ' ' && s[digits+1] != '\t' {
		return -1
	}
	return digits + 1
}
