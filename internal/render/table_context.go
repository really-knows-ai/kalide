package render

import (
	"github.com/really-knows-ai/kalide/internal/mdcheck"
)

// tableCellContext renders one parsed GFM table cell into the `{html, attrs}`
// map a table render context exposes (gfm-tables): html is the cell's visible
// content as inline HTML and attrs is the cell's decoded attribute set keyed by
// the template's declared cell attributes.
//
// The cell's `{br}` and `{...}` extensions are decoded by mdcheck's
// DecodeTableCell — the renderer never re-implements the extension stripping.
// A `{br}` is a segment boundary, so the decoded cell's Lines are rendered
// segment by segment through renderInline and joined with a literal `<br>`
// (renderCellLines); a raw `<br>` handed to the inline renderer would be
// escaped.
//
// attrs is keyed by each cell attribute the enclosing template declares, with a
// nil entry for every declared attribute the cell leaves unset (and no entry for
// an undeclared name). specs is the template's declared cell attributes; when it
// declares none, attrs is empty.
//
// Validation guarantees a cell decodes without error at render time (the
// decoder's unknown-attribute and wrong-type errors are reported by the table
// check, not here). Should a decode still fail, the cell degrades gracefully:
// its raw source is rendered with renderInline and every declared attribute is
// left nil, so rendering never fails on a cell.
func tableCellContext(cell mdcheck.TableCell, specs []mdcheck.CellAttributeSpec) map[string]any {
	decoded, err := mdcheck.DecodeTableCell(cell, specs)
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
	return map[string]any{
		"html":  renderCellLines(decoded.Lines),
		"attrs": decoded.Attrs,
	}
}

// tableContext parses one body's Markdown source for its GFM tables and returns
// the two reserved table entries a template context exposes
// (table-layout-context, gfm-tables): table is the body's FIRST parsed table as
// the table map, or a TYPED nil map (map[string]any(nil)) when the body carries
// no table, and tables is every parsed table in source order, as a non-nil but
// empty list when the body carries none.
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
// Parsing is permissive: tableContext passes the zero mdcheck.TableOptions (no
// limit bounds) and startLine 1, because by render time validation has already
// accepted the body and enforced the template's declared table limits
// (table-limits); this parse only re-derives the tables to render. Any issues
// the parser returns are therefore ignored, and a body validation accepted
// yields none.
//
// Each table is a map with:
//
//   - headers: the header cells in column order, each a tableCellContext map
//     ({html, attrs});
//   - align: the per-column alignments in column order, each "left", "center",
//     "right" or "" for none, copied so the table map aliases nothing;
//   - rows: the body rows source-ordered, each a list of cells in column order,
//     each cell a tableCellContext map.
//
// A cell map is built by tableCellContext, so the cell's `{br}`/`{...}`
// extensions are decoded by mdcheck and its visible html is the decoded
// segments joined with `<br>`; render never re-implements the stripping.
func tableContext(body string, specs []mdcheck.CellAttributeSpec) (table any, tables []any) {
	parsed, _ := mdcheck.ParseTables(body, 1, mdcheck.TableOptions{})

	tables = make([]any, len(parsed))
	for i, t := range parsed {
		headers := make([]any, len(t.Headers))
		for j, cell := range t.Headers {
			headers[j] = tableCellContext(cell, specs)
		}
		rows := make([]any, len(t.Rows))
		for r, row := range t.Rows {
			cells := make([]any, len(row))
			for c, cell := range row {
				cells[c] = tableCellContext(cell, specs)
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
