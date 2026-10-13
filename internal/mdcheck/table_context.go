// This file implements the shared table render-context builder: the ONE place a
// body's parsed GFM tables are turned into the `.table`/`.tables` context
// entries a template context exposes, with each cell a `{html, attrs}` map
// (gfm-tables, table-layout-context, table-cell-attributes).
//
// It lives in mdcheck rather than being mirrored in internal/render and
// internal/template because mdcheck already owns both halves it needs: the
// table parser (ParseTables) and the cell-extension decoder (DecodeTableCell).
// internal/render cannot be imported by internal/template (it would create an
// import cycle), so the two previously carried duplicated copies of this
// building; mdcheck.TableContext is the single implementation both sides now
// build through.
//
// Rendering one cell's inline Markdown to HTML is deliberately left to the
// caller: the builder takes a render-inline function parameter because
// internal/render and internal/template each have their own goldmark-backed
// renderInline. What is shared is the `{br}`/`{...}` stripping, the
// `{html, attrs}` cell shape and the `.table`/`.tables` pair — never the
// caller's HTML renderer.

package mdcheck

import (
	htmltemplate "html/template"
	"strings"
)

// TableContext parses body's GFM tables and returns the two reserved table
// context entries a template exposes (gfm-tables, table-layout-context): table
// is the body's FIRST parsed table as the table map, or a TYPED nil map
// (map[string]any(nil)) when the body carries no table, and tables is every
// parsed table in source order, as a non-nil but empty list when the body
// carries none.
//
// body is the body's Markdown source and specs are the enclosing template's
// declared cell attributes, the set each cell's `{...}` groups are decoded
// against (table-cell-attributes). The pair is returned rather than a single map
// so a caller can set the two reserved entries directly (data["table"],
// data["tables"]) and keep the always-present shape explicit: table is always
// the first table or a typed nil map and tables is always a (possibly empty)
// list, in a template that does not declare `tables` and in any body that
// carries no table, so a layout reads the entries without a guard.
//
// The no-table value is deliberately a typed nil map rather than a bare nil
// interface: `{{ if .table }}` is then falsy (a nil map has length 0), while
// `{{ .table.rows }}` and `{{ range .table.rows }}` evaluate the absent key to
// an invalid value with no error — a no-op range — instead of failing with
// "nil pointer evaluating interface {}.rows", which a bare nil interface
// produces. That keeps the documented shape `{{ range .table.rows }}` a no-op
// on a body with no table.
//
// renderInline renders one inline-Markdown string — a cell's raw text or one of
// its `{br}` segments — to HTML without the paragraph wrapper a block renderer
// would add. It is the caller's renderer (internal/render's renderInline,
// internal/template's exampleRenderInline), so the shared builder never
// re-implements HTML rendering. Its result is an html/template.HTML, the trusted
// type a template injects unescaped.
//
// Parsing is permissive: TableContext passes the zero TableOptions (no limit
// bounds) and startLine 1, because by the time a context is built validation has
// already accepted the body and enforced the template's declared table limits
// (table-limits); this parse only re-derives the tables to render. Any issues
// the parser returns are therefore ignored, and a body validation accepted
// yields none.
//
// Each table is a map with:
//
//   - headers: the header cells in column order, each a TableCellContext map
//     ({html, attrs});
//   - align: the per-column alignments in column order, each "left", "center",
//     "right" or "" for none, copied so the table map aliases nothing;
//   - rows: the body rows source-ordered, each a list of cells in column order,
//     each cell a TableCellContext map.
func TableContext(body string, specs []CellAttributeSpec, renderInline func(string) htmltemplate.HTML) (table any, tables []any) {
	parsed, _ := ParseTables(body, 1, TableOptions{})

	tables = make([]any, len(parsed))
	for i, t := range parsed {
		headers := make([]any, len(t.Headers))
		for j, cell := range t.Headers {
			headers[j] = TableCellContext(cell, specs, renderInline)
		}
		rows := make([]any, len(t.Rows))
		for r, row := range t.Rows {
			cells := make([]any, len(row))
			for c, cell := range row {
				cells[c] = TableCellContext(cell, specs, renderInline)
			}
			rows[r] = cells
		}
		align := make([]string, len(t.Align))
		copy(align, t.Align)
		tables[i] = map[string]any{
			"headers": headers,
			"align":   align,
			"rows":    rows,
		}
	}

	if len(parsed) > 0 {
		table = tables[0]
	} else {
		// A typed nil map, not a bare nil: see the doc comment — it keeps
		// `{{ if .table }}` falsy while `{{ range .table.rows }}` stays a
		// no-op instead of erroring.
		table = map[string]any(nil)
	}
	return table, tables
}

// TableCellContext builds one parsed GFM table cell's `{html, attrs}` map, the
// cell shape TableContext exposes and the single implementation render and the
// load-time example context build through (table-layout-context,
// table-cell-attributes).
//
// html is the cell's visible content as inline HTML: the cell's `{br}` and
// `{...}` extensions are decoded by DecodeTableCell — the exported seam the
// renderer and the example context already consume, so neither re-implements
// the extension stripping — and the decoded cell's Lines are rendered segment by
// segment through renderInline and joined with a literal `<br>`. A `{br}` is a
// segment boundary and never a raw `<br>` in the Markdown: a raw `<br>` handed
// to an inline renderer would be escaped.
//
// attrs is keyed by each cell attribute the enclosing template declares (specs),
// with a nil entry for every declared attribute the cell leaves unset (and no
// entry for an undeclared name). When specs declares none, attrs is empty.
//
// Validation guarantees a cell decodes without error by the time a context is
// built (the decoder's unknown-attribute and wrong-type errors are reported by
// the table check, not here). Should a decode still fail, the cell degrades
// gracefully: its raw source is rendered with renderInline and every declared
// attribute is left nil, so building a context never fails on a cell.
func TableCellContext(cell TableCell, specs []CellAttributeSpec, renderInline func(string) htmltemplate.HTML) map[string]any {
	decoded, err := DecodeTableCell(cell, specs)
	if err != nil {
		attrs := make(map[string]any, len(specs))
		for _, spec := range specs {
			attrs[spec.Name] = nil
		}
		return map[string]any{
			"html":  renderInline(cell.Text),
			"attrs": attrs,
		}
	}
	segments := make([]string, len(decoded.Lines))
	for i, line := range decoded.Lines {
		segments[i] = string(renderInline(line))
	}
	return map[string]any{
		"html":  htmltemplate.HTML(strings.Join(segments, "<br>")),
		"attrs": decoded.Attrs,
	}
}
