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
