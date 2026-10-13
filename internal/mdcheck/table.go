// This file implements the GFM table parser: the syntax layer that reads the
// GitHub Flavored Markdown tables a template body may contain once the body's
// template opts in through its `body.tables` option (gfm-tables, table-syntax,
// table-limits).
//
// A table is a header row, a delimiter row and zero or more body rows, with
// optional leading and trailing pipes and every cell trimmed of surrounding
// spaces. The delimiter row fixes each column's alignment: `---`, `:---` and
// `---:` select none, left and right, and `:---:` centres the column. A
// backslash-escaped `\|` is a literal pipe character, never a cell separator,
// and is kept literal even inside an inline code span (where an unescaped `|`
// is likewise not a separator).
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
