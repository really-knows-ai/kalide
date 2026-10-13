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
// package. A template's declared cell attributes are likewise carried in as
// CellAttributeSpec, the plain descriptor the cell-extension decoder resolves
// a cell's `{...}` group against. Each parsed table is returned as a Table of
// header cells, per-column alignment and body rows of TableCell, and each
// breached count or limit is returned as one positioned Issue naming the limit
// and the actual count.
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

// CellAttributeSpec is one cell attribute a template declares for its GFM
// tables, mdcheck's own plain descriptor for an entry of the
// `body.tables.cell_attributes` list (table-cell-attributes). It is the
// mdcheck-side counterpart of the template schema's CellAttribute: the
// template loader and the validator convert their CellAttribute declarations
// into a []CellAttributeSpec, so the cell-extension decoder can resolve a
// cell's `{...}` attribute group without importing a schema package.
//
// Kind is one of the CellAttributeBoolean and CellAttributeEnum constants —
// plain strings, so mdcheck imports no schema package. A boolean attribute is
// set by presence (a bare `{name}` is `{name:true}`); an enum attribute is set
// to one of Variants, the closed set of its accepted values. The reserved name
// `br` is never a declared attribute.
type CellAttributeSpec struct {
	// Name is the attribute name an author writes in a cell's `{...}`
	// group or as a bare-name short-hand, e.g. `bold` or `highlight`. The
	// reserved name `br` is never a declared attribute.
	Name string

	// Kind is the attribute's value type: CellAttributeBoolean or
	// CellAttributeEnum. It is a plain string so mdcheck imports no schema
	// package; the two constants mirror the manifest's `boolean` and `enum`
	// field-type names.
	Kind string

	// Variants, for an enum attribute, is the closed set of accepted
	// values. A bare name that is a variant of exactly one declared enum
	// attribute sets that attribute by mutex (table-cell-attributes). It is
	// empty for a boolean attribute.
	Variants []string
}

// CellAttributeSpec.Kind values: the value type of one declared cell
// attribute. A cell attribute is either a boolean — set by presence, `{name}`
// or `{name:true}` — or an enum, set to one of the spec's Variants. The values
// are plain strings so mdcheck imports no schema package; they mirror the
// manifest's `boolean` and `enum` field-type names (table-cell-attributes).
const (
	// CellAttributeBoolean is a boolean cell attribute: a bare `{name}`
	// is `{name:true}`, and `{name:false}` unsets it.
	CellAttributeBoolean = "boolean"

	// CellAttributeEnum is an enum cell attribute: `{name:variant}` sets
	// it to one of the spec's Variants.
	CellAttributeEnum = "enum"
)

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

// ParseTables scans a body's Markdown source for GFM tables and returns the
// tables it finds together with one positioned Issue per count or limit breach
// (gfm-tables, table-syntax, table-limits).
//
// body is the body's Markdown source and startLine is the 1-based line in the
// containing file at which body begins, so every returned Table, TableCell and
// Issue is positioned file-absolutely — the same offset Options.StartLine
// carries into Check. A startLine below 1 is treated as 1.
//
// The scan is line-based. It skips fenced code blocks (``` or ~~~, ended by a
// run of the same character at least as long as the opener) and indented code
// (a line indented four spaces or a tab), so a table-shaped pair of lines
// inside a code block is never read as a table. Outside code, a table is
// detected wherever a non-blank line is immediately followed by a delimiter row
// (isTableDelimiterRow): the non-blank line is the header row and the next line
// the delimiter row. parseTable then parses the table in full — header,
// delimiter and body rows — ending it at the first blank line or other block's
// start (table-syntax), and the scan resumes past it. A line that merely looks
// like a header but has no delimiter row after it is left to the ordinary
// Markdown walk.
//
// The effective limits are carried by opts (table-limits). Zero imposes no
// bound: a Min* field of 0 is no minimum and a Max* field of 0 is unbounded.
// Each table is checked against MinColumns/MaxColumns (its header included) and
// MinRows/MaxRows (its body rows, the header excluded), and the whole body
// against MinTables/MaxTables. Every breach is one Issue of KindTable: a
// per-table breach sits at the table's own header line and a whole-body breach
// at the first table's line, or at the body's first line when the body carries
// no table. Each message names the limit and the actual count, in the form
// `max_columns: table has 12 columns, maximum is 10` (table-errors).
//
// The returned Issues are ordered: the strict-column-count Issues parseTable
// reports for each table in source order, then the per-table limit Issues in
// table order, then the whole-body Issues. ParseTables sets no Issue File: the
// caller supplies the body's own identity.
func ParseTables(body string, startLine int, opts TableOptions) ([]Table, []Issue) {
	if startLine < 1 {
		startLine = 1
	}
	lines := strings.Split(body, "\n")

	var tables []Table
	var issues []Issue

	for i := 0; i < len(lines); {
		if marker, count, ok := tableFenceOpen(lines[i]); ok {
			i = tableSkipFence(lines, i, marker, count)
			continue
		}
		if tableIndentedCode(lines[i]) {
			i++
			continue
		}
		if strings.TrimSpace(lines[i]) != "" &&
			i+1 < len(lines) && isTableDelimiterRow(lines[i+1]) {
			table, tableIssues, next := parseTable(lines, i, startLine)
			tables = append(tables, table)
			issues = append(issues, tableIssues...)
			i = next
			continue
		}
		i++
	}

	for _, table := range tables {
		issues = append(issues, tableLimitIssues(table, opts)...)
	}
	issues = append(issues, bodyTableLimitIssues(tables, startLine, opts)...)

	return tables, issues
}

// tableLimitIssues checks one parsed table against the per-table column and row
// limits and returns one Issue per breach, positioned at the table's header
// line (table-limits). Columns count the header row's cells; rows count body
// rows only, the header excluded. A Min* value of 0 is no minimum and a Max*
// value of 0 is unbounded, so a zero option yields no Issue.
func tableLimitIssues(table Table, opts TableOptions) []Issue {
	columns := len(table.Headers)
	rows := len(table.Rows)

	var issues []Issue
	if opts.MaxColumns > 0 && columns > opts.MaxColumns {
		issues = append(issues, tableLimitIssue(table.Line,
			fmt.Sprintf("max_columns: table has %d columns, maximum is %d", columns, opts.MaxColumns),
			fmt.Sprintf("use at most %d columns", opts.MaxColumns)))
	}
	if opts.MinColumns > 0 && columns < opts.MinColumns {
		issues = append(issues, tableLimitIssue(table.Line,
			fmt.Sprintf("min_columns: table has %d columns, minimum is %d", columns, opts.MinColumns),
			fmt.Sprintf("use at least %d columns", opts.MinColumns)))
	}
	if opts.MaxRows > 0 && rows > opts.MaxRows {
		issues = append(issues, tableLimitIssue(table.Line,
			fmt.Sprintf("max_rows: table has %d rows, maximum is %d", rows, opts.MaxRows),
			fmt.Sprintf("use at most %d body rows", opts.MaxRows)))
	}
	if opts.MinRows > 0 && rows < opts.MinRows {
		issues = append(issues, tableLimitIssue(table.Line,
			fmt.Sprintf("min_rows: table has %d rows, minimum is %d", rows, opts.MinRows),
			fmt.Sprintf("use at least %d body rows", opts.MinRows)))
	}
	return issues
}

// bodyTableLimitIssues checks the whole body's table count against MinTables and
// MaxTables and returns one Issue per breach (table-limits). The Issue sits at
// the first table's line, or at the body's first line (startLine) when the body
// carries no table. A MinTables value of 0 is no minimum and a MaxTables value
// of 0 is unbounded, so a zero option yields no Issue.
func bodyTableLimitIssues(tables []Table, startLine int, opts TableOptions) []Issue {
	count := len(tables)
	line := startLine
	if count > 0 {
		line = tables[0].Line
	}

	var issues []Issue
	if opts.MaxTables > 0 && count > opts.MaxTables {
		issues = append(issues, tableLimitIssue(line,
			fmt.Sprintf("max_tables: body has %d tables, maximum is %d", count, opts.MaxTables),
			fmt.Sprintf("use at most %d tables", opts.MaxTables)))
	}
	if opts.MinTables > 0 && count < opts.MinTables {
		issues = append(issues, tableLimitIssue(line,
			fmt.Sprintf("min_tables: body has %d tables, minimum is %d", count, opts.MinTables),
			fmt.Sprintf("use at least %d tables", opts.MinTables)))
	}
	return issues
}

// tableLimitIssue builds a KindTable limit Issue: the message names the limit
// and the actual count, and the guidance is the fix (table-errors).
func tableLimitIssue(line int, message, guidance string) Issue {
	return Issue{
		Line:     line,
		Kind:     KindTable,
		Message:  message,
		Guidance: guidance,
	}
}

// tableFenceOpen reports whether line opens a fenced code block and, if so, the
// fence character (` or ~) and its run length. A fence is indented by at most
// three spaces; four or more spaces is indented code, not a fence. A backtick
// fence's info string may not contain a backtick, matching CommonMark.
func tableFenceOpen(line string) (marker byte, count int, ok bool) {
	i := 0
	for i < len(line) && i < 4 && line[i] == ' ' {
		i++
	}
	if i >= 4 {
		return 0, 0, false
	}
	rest := line[i:]
	if len(rest) < 3 {
		return 0, 0, false
	}
	c := rest[0]
	if c != '`' && c != '~' {
		return 0, 0, false
	}
	for count < len(rest) && rest[count] == c {
		count++
	}
	if count < 3 {
		return 0, 0, false
	}
	if c == '`' && strings.IndexByte(rest[count:], '`') >= 0 {
		return 0, 0, false
	}
	return c, count, true
}

// tableFenceClose reports whether line closes the fence opened with marker and
// count: the same character repeated at least count times, indented by at most
// three spaces and followed only by spaces or tabs.
func tableFenceClose(line string, marker byte, count int) bool {
	i := 0
	for i < len(line) && i < 4 && line[i] == ' ' {
		i++
	}
	if i >= 4 {
		return false
	}
	rest := line[i:]
	run := 0
	for run < len(rest) && rest[run] == marker {
		run++
	}
	if run < count {
		return false
	}
	for k := run; k < len(rest); k++ {
		if rest[k] != ' ' && rest[k] != '\t' && rest[k] != '\r' {
			return false
		}
	}
	return true
}

// tableSkipFence returns the index of the first line after the fenced code
// block opened at lines[start], or len(lines) when the fence is never closed.
func tableSkipFence(lines []string, start int, marker byte, count int) int {
	for i := start + 1; i < len(lines); i++ {
		if tableFenceClose(lines[i], marker, count) {
			return i + 1
		}
	}
	return len(lines)
}

// tableIndentedCode reports whether line is indented code — indented four
// spaces or starting with a tab — and so cannot be a table's header row.
func tableIndentedCode(line string) bool {
	if line == "" {
		return false
	}
	if line[0] == '\t' {
		return true
	}
	return strings.HasPrefix(line, "    ")
}
