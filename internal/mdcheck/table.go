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
