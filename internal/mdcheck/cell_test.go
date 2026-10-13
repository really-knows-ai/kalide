package mdcheck

import (
	"reflect"
	"testing"
)

// cellTestSpecs is the declared cell-attribute set the decoder tests resolve
// against: two boolean attributes and one enum with two variants. Every decoded
// Attrs map keys all three, with nil for each attribute the cell leaves unset.
var cellTestSpecs = []CellAttributeSpec{
	{Name: "bold", Kind: CellAttributeBoolean},
	{Name: "italic", Kind: CellAttributeBoolean},
	{Name: "highlight", Kind: CellAttributeEnum, Variants: []string{"green", "red"}},
}

// cellTestAttrs builds the expected decoded attribute map: one entry per
// declared attribute, with the given set values and nil for the rest, matching
// decodeCellAttributes' "present as nil rather than absent" contract.
func cellTestAttrs(set map[string]any) map[string]any {
	attrs := map[string]any{"bold": nil, "italic": nil, "highlight": nil}
	for name, value := range set {
		attrs[name] = value
	}
	return attrs
}

// TestDecodeCellAttributes covers the two cell extensions and the two literal
// contexts: `{br}` splits the cell into line segments, a `{...}` group is
// stripped and sets its attributes, and both are recognised only in ordinary
// inline text — a backslash-escaped brace, a brace inside a code span and a
// brace inside a link's text or destination are all literal. The Attrs map
// carries a nil entry for every declared attribute the cell leaves unset.
func TestDecodeCellAttributes(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		lines []string
		attrs map[string]any
	}{
		{
			name:  "br splits the cell into the expected line segments",
			text:  "a{br}b{br}c",
			lines: []string{"a", "b", "c"},
			attrs: cellTestAttrs(nil),
		},
		{
			name:  "a backslash-escaped brace is a literal brace and declares nothing",
			text:  `a \{b} c`,
			lines: []string{`a \{b} c`},
			attrs: cellTestAttrs(nil),
		},
		{
			name:  "a brace group inside an inline code span is literal text",
			text:  "`{bold}`",
			lines: []string{"`{bold}`"},
			attrs: cellTestAttrs(nil),
		},
		{
			name:  "a brace group inside a link's text is literal",
			text:  "[a{bold}](https://example.com)",
			lines: []string{"[a{bold}](https://example.com)"},
			attrs: cellTestAttrs(nil),
		},
		{
			name:  "a brace group inside a link's destination is literal",
			text:  "[a](https://example.com/{bold})",
			lines: []string{"[a](https://example.com/{bold})"},
			attrs: cellTestAttrs(nil),
		},
		{
			name:  "a group outside a link is stripped and sets its attribute",
			text:  "[a](https://example.com){bold}",
			lines: []string{"[a](https://example.com)"},
			attrs: cellTestAttrs(map[string]any{"bold": true}),
		},
		{
			name:  "a comma group is stripped from the content and sets every attribute",
			text:  "hello {bold,highlight:green}",
			lines: []string{"hello "},
			attrs: cellTestAttrs(map[string]any{"bold": true, "highlight": "green"}),
		},
		{
			name:  "every declared attribute the cell leaves unset keeps a nil entry",
			text:  "text {bold}",
			lines: []string{"text "},
			attrs: cellTestAttrs(map[string]any{"bold": true}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeCellAttributes(TableCell{Text: tt.text, Line: 1, Column: 1}, cellTestSpecs)
			if err != nil {
				t.Fatalf("decodeCellAttributes(%q) error = %v, want none", tt.text, err)
			}
			if !reflect.DeepEqual(got.Lines, tt.lines) {
				t.Fatalf("decodeCellAttributes(%q).Lines = %#v, want %#v", tt.text, got.Lines, tt.lines)
			}
			if !reflect.DeepEqual(got.Attrs, tt.attrs) {
				t.Fatalf("decodeCellAttributes(%q).Attrs = %#v, want %#v", tt.text, got.Attrs, tt.attrs)
			}
		})
	}
}

// TestDecodeCellAttributesHeaderIdentical pins the contract that the decode
// applies to a header cell exactly as to a body cell: the same cell text decodes
// identically whether it sits in the header row or in a body row.
func TestDecodeCellAttributesHeaderIdentical(t *testing.T) {
	header := TableCell{Text: "a{br}{bold}", Line: 1, Column: 3}
	body := TableCell{Text: "a{br}{bold}", Line: 2, Column: 3}

	gotHeader, err := decodeCellAttributes(header, cellTestSpecs)
	if err != nil {
		t.Fatalf("decodeCellAttributes(header) error = %v, want none", err)
	}
	gotBody, err := decodeCellAttributes(body, cellTestSpecs)
	if err != nil {
		t.Fatalf("decodeCellAttributes(body) error = %v, want none", err)
	}
	if !reflect.DeepEqual(gotHeader, gotBody) {
		t.Fatalf("header decode = %#v, body decode = %#v, want identical", gotHeader, gotBody)
	}
	if want := []string{"a", ""}; !reflect.DeepEqual(gotHeader.Lines, want) {
		t.Fatalf("header decode Lines = %#v, want %#v", gotHeader.Lines, want)
	}
	if want := cellTestAttrs(map[string]any{"bold": true}); !reflect.DeepEqual(gotHeader.Attrs, want) {
		t.Fatalf("header decode Attrs = %#v, want %#v", gotHeader.Attrs, want)
	}
}

// TestDecodeTableCellMatchesDecoder pins the exported seam internal/render
// depends on: DecodeTableCell returns exactly the DecodedCell the unexported
// decoder produces, and the same error, so the renderer never re-implements the
// `{br}`/`{...}` stripping.
func TestDecodeTableCellMatchesDecoder(t *testing.T) {
	cell := TableCell{Text: "hi{br}{bold,highlight:green}", Line: 4, Column: 7}

	exported, errExported := DecodeTableCell(cell, cellTestSpecs)
	unexported, errUnexported := decodeCellAttributes(cell, cellTestSpecs)
	if errExported != nil || errUnexported != nil {
		t.Fatalf("errors: DecodeTableCell = %v, decodeCellAttributes = %v, want none", errExported, errUnexported)
	}
	if !reflect.DeepEqual(exported, unexported) {
		t.Fatalf("DecodeTableCell = %#v, decodeCellAttributes = %#v, want identical", exported, unexported)
	}
	if want := []string{"hi", ""}; !reflect.DeepEqual(exported.Lines, want) {
		t.Fatalf("DecodeTableCell Lines = %#v, want %#v", exported.Lines, want)
	}
	if want := cellTestAttrs(map[string]any{"bold": true, "highlight": "green"}); !reflect.DeepEqual(exported.Attrs, want) {
		t.Fatalf("DecodeTableCell Attrs = %#v, want %#v", exported.Attrs, want)
	}
}
