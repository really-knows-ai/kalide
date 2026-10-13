package mdcheck

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestGFMTableExamples is the conformance test for the GFM table parser
// (gfm-tables, table-syntax): each fixture under testdata/gfm-tables
// reproduces the Markdown input of a GFM specification example verbatim, and
// the test asserts what ParseTables produces for it with zero TableOptions.
//
// The fixtures cover GFM 0.29-gfm, section 4.10 "Tables (extension)",
// examples 198-205:
//
//	198  a basic table (foo / bar, baz / bim)
//	199  per-column alignment with no leading or trailing pipes
//	200  a backslash-escaped pipe, `\|`, is a literal cell character
//	201  a blank line ends the table
//	202  a block quote begins another block and ends the table
//	203  the header and delimiter rows differ in cell count
//	204  a short body row and a body row with too many cells
//	205  a header and delimiter row with no body rows
//
// Kalide's parser deliberately differs from the specification in the examples
// that turn on the column count. GFM pads a short row with empty cells and
// drops a long row's extra cells, and it does not recognize a table at all
// when the header and delimiter rows disagree; Kalide keeps the strict-count
// rule the requirements fix (requirements.requirement.table-limits), so those
// examples must instead produce the parseTable count Issue. Each such
// deviation is called out on its case below.
func TestGFMTableExamples(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    []Table
		issues  []Issue
	}{
		{
			// GFM spec example 198. Nothing here turns on a count, so the
			// parse matches the specification's own reading.
			name:    "example 198 basic table",
			fixture: "example-198.md",
			want: []Table{{
				Headers: []TableCell{tcell("foo", 1, 3), tcell("bar", 1, 9)},
				Align:   []string{"", ""},
				Rows:    [][]TableCell{{tcell("baz", 3, 3), tcell("bim", 3, 9)}},
				Line:    1,
			}},
		},
		{
			// GFM spec example 199: `:-:` centers the first column, `---:`
			// right-aligns the second, and the header and body rows carry no
			// leading or trailing pipe. The column counts agree, so the parse
			// matches the specification.
			name:    "example 199 alignment and no leading or trailing pipes",
			fixture: "example-199.md",
			want: []Table{{
				Headers: []TableCell{tcell("abc", 1, 3), tcell("defghi", 1, 9)},
				Align:   []string{"center", "right"},
				Rows:    [][]TableCell{{tcell("bar", 3, 1), tcell("baz", 3, 7)}},
				Line:    1,
			}},
		},
		{
			// GFM spec example 200: `\|` is a literal pipe — even inside an
			// inline code span or a strong span — and the parser stores it as
			// the plain `|` the cell's inline Markdown renders.
			name:    "example 200 escaped pipe is a literal cell character",
			fixture: "example-200.md",
			want: []Table{{
				Headers: []TableCell{tcell("f|oo", 1, 3)},
				Align:   []string{""},
				Rows: [][]TableCell{
					{tcell("b `|` az", 3, 3)},
					{tcell("b **|** im", 4, 3)},
				},
				Line: 1,
			}},
		},
		{
			// GFM spec example 201. GFM ends the table at the blank line and
			// pads the short body row `| bar |` with an empty cell. Kalide's
			// strict column-count rule is the deviation: the short row is one
			// error, not a padded cell.
			name:    "example 201 blank line ends the table and a short row is a strict-count error",
			fixture: "example-201.md",
			want: []Table{{
				Headers: []TableCell{tcell("abc", 1, 3), tcell("def", 1, 9)},
				Align:   []string{"", ""},
				Rows: [][]TableCell{
					{tcell("bar", 3, 3), tcell("baz", 3, 9)},
					{tcell("bar", 4, 3)},
				},
				Line: 1,
			}},
			issues: []Issue{tcount(4, 1, "rows[2]: 1 cells, header has 2")},
		},
		{
			// GFM spec example 202: the block quote starts another block, so
			// it ends the table and is not absorbed as a body row. The table
			// itself is well formed, so the parse matches the specification.
			name:    "example 202 block quote ends the table",
			fixture: "example-202.md",
			want: []Table{{
				Headers: []TableCell{tcell("abc", 1, 3), tcell("def", 1, 9)},
				Align:   []string{"", ""},
				Rows:    [][]TableCell{{tcell("bar", 3, 3), tcell("baz", 3, 9)}},
				Line:    1,
			}},
		},
		{
			// GFM spec example 203. GFM does not recognize a table when the
			// header (`abc | def`) and delimiter (`---`) rows differ in cell
			// count, and renders the three lines as a paragraph. Kalide's
			// deviation: a header/delimiter mismatch is reported against the
			// table, never demoted to a paragraph — so a table is returned,
			// carrying the mismatch Issue on the delimiter row plus the
			// resulting short body row's Issue.
			name:    "example 203 header/delimiter mismatch is an error, not a paragraph",
			fixture: "example-203.md",
			want: []Table{{
				Headers: []TableCell{tcell("abc", 1, 3), tcell("def", 1, 9)},
				Align:   []string{"", ""},
				Rows:    [][]TableCell{{tcell("bar", 3, 3)}},
				Line:    1,
			}},
			issues: []Issue{
				tcount(2, 1, "header has 2 cells, delimiter row has 1"),
				tcount(3, 1, "rows[1]: 1 cells, header has 2"),
			},
		},
		{
			// GFM spec example 204. GFM pads `| bar |` with an empty cell and
			// ignores the excess in `| bar | baz | boo |`. Kalide's deviation:
			// each mismatched row is one strict-count error, so both rows are
			// flagged and neither is padded or truncated.
			name:    "example 204 short and long body rows are strict-count errors",
			fixture: "example-204.md",
			want: []Table{{
				Headers: []TableCell{tcell("abc", 1, 3), tcell("def", 1, 9)},
				Align:   []string{"", ""},
				Rows: [][]TableCell{
					{tcell("bar", 3, 3)},
					{tcell("bar", 4, 3), tcell("baz", 4, 9), tcell("boo", 4, 15)},
				},
				Line: 1,
			}},
			issues: []Issue{
				tcount(3, 1, "rows[1]: 1 cells, header has 2"),
				tcount(4, 1, "rows[2]: 3 cells, header has 2"),
			},
		},
		{
			// GFM spec example 205: a header and delimiter row with no body
			// rows. The table parses with an empty body, matching the
			// specification (which omits the <tbody>).
			name:    "example 205 no body rows",
			fixture: "example-205.md",
			want: []Table{{
				Headers: []TableCell{tcell("abc", 1, 3), tcell("def", 1, 9)},
				Align:   []string{"", ""},
				Line:    1,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := readFixture(t, filepath.Join("gfm-tables", tt.fixture))
			tables, issues := ParseTables(string(src), 1, TableOptions{})
			if !reflect.DeepEqual(tables, tt.want) {
				t.Fatalf("ParseTables(%s) tables\n got: %#v\nwant: %#v", tt.fixture, tables, tt.want)
			}
			if !reflect.DeepEqual(issues, tt.issues) {
				t.Fatalf("ParseTables(%s) issues\n got: %#v\nwant: %#v", tt.fixture, issues, tt.issues)
			}
		})
	}
}
