package render

import (
	"github.com/really-knows-ai/kalide/internal/mdcheck"
)

// tableCellContext renders one parsed GFM table cell into the `{html, attrs}`
// map a table render context exposes (gfm-tables, table-cell-attributes): html
// is the cell's visible content as inline HTML and attrs is the cell's decoded
// attribute set keyed by the template's declared cell attributes.
//
// It is a thin adaptor over the shared mdcheck.TableCellContext, the ONE place a
// parsed cell's `{br}`/`{...}` extensions are decoded and its `{html, attrs}`
// map is built: renderInline is passed as the render-inline function the shared
// builder calls, so the extension stripping, the `{br}`-segment rendering joined
// with a literal `<br>` and the attrs map all live in mdcheck and render
// re-implements no cell-map building. A raw `<br>` handed to the inline renderer
// would be escaped, which is why the builder renders each segment separately.
//
// specs is the enclosing template's declared cell attributes; attrs is keyed by
// each declared attribute, with a nil entry for one the cell leaves unset (and no
// entry for an undeclared name). When specs declares none, attrs is empty.
//
// Validation guarantees a cell decodes without error at render time (the
// decoder's unknown-attribute and wrong-type errors are reported by the table
// check, not here). Should a decode still fail, mdcheck.TableCellContext degrades
// gracefully — it renders the cell's raw source through renderInline and leaves
// every declared attribute nil — so rendering never fails on a cell.
func tableCellContext(cell mdcheck.TableCell, specs []mdcheck.CellAttributeSpec) map[string]any {
	return mdcheck.TableCellContext(cell, specs, renderInline)
}

// tableContext parses one body's Markdown source for its GFM tables and returns
// the two reserved table entries a template context exposes
// (table-layout-context, gfm-tables): table is the body's FIRST parsed table as
// the table map, or a TYPED nil map (map[string]any(nil)) when the body carries
// no table, and tables is every parsed table in source order, as a non-nil but
// empty list when the body carries none.
//
// It is a thin caller of the shared mdcheck.TableContext, the ONE table-context
// builder the renderer and the load-time example context build through:
// renderInline is passed as the render-inline function, so the `{br}`/`{...}`
// stripping, the `{html, attrs}` cell shape and the `.table`/`.tables` pair all
// live in mdcheck and render re-implements no table-context building.
//
// body is the body's Markdown source — the same source renderBody renders — and
// specs is the enclosing template's declared cell attributes, the set each
// cell's `{...}` groups are decoded against (table-cell-attributes). The pair is
// returned rather than a single map so the renderer can set the two reserved
// entries directly (data["table"], data["tables"]) and keep the
// always-present shape explicit: table is always the first table or a typed nil
// map and tables is always a (possibly empty) list, in a template that does not
// declare `tables` and in any body that carries no table, so a layout reads the
// entries without a guard.
//
// The no-table value is deliberately a typed nil map rather than a bare nil
// interface: `{{ if .table }}` is then falsy (a nil map has length 0), while
// `{{ .table.rows }}` and `{{ range .table.rows }}` evaluate the absent key to
// an invalid value with no error — a no-op range — instead of failing with
// "nil pointer evaluating interface {}.rows", which a bare nil interface
// produces. That keeps the documented AC shape `{{ range .table.rows }}` a
// no-op on a body with no table.
//
// Parsing is permissive: mdcheck.TableContext passes the zero
// mdcheck.TableOptions (no limit bounds) and startLine 1, because by render time
// validation has already accepted the body and enforced the template's declared
// table limits (table-limits); this parse only re-derives the tables to render.
// Any issues the parser returns are therefore ignored, and a body validation
// accepted yields none.
//
// The returned tables are mdcheck.TableContext's: each table map carries
// headers, align and rows in column order, and each cell is a `{html, attrs}`
// map whose html is the decoded `{br}` segments rendered through renderInline
// and joined with `<br>`.
func tableContext(body string, specs []mdcheck.CellAttributeSpec) (table any, tables []any) {
	return mdcheck.TableContext(body, specs, renderInline)
}
