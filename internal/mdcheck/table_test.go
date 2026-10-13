package mdcheck

import (
	htmltemplate "html/template"
	"reflect"
	"strings"
	"testing"
)

// tcell builds an expected parsed TableCell, positioned at a 1-based line and
// byte column.
func tcell(text string, line, col int) TableCell {
	return TableCell{Text: text, Line: line, Column: col}
}

// tsplit builds an expected TableCell from splitTableRow: the splitter leaves
// Line zero and records a 1-based byte column.
func tsplit(text string, col int) TableCell {
	return TableCell{Text: text, Column: col}
}

// tcount builds an expected strict-column-count Issue: KindTable with no
// guidance.
func tcount(line, col int, message string) Issue {
	return Issue{Line: line, Col: col, Kind: KindTable, Message: message}
}

// tlimit builds an expected limit Issue: KindTable carrying the fix guidance.
func tlimit(line int, message, guidance string) Issue {
	return Issue{Line: line, Kind: KindTable, Message: message, Guidance: guidance}
}

// TestSplitTableRow covers the GFM row grammar: optional leading and trailing
// pipes, trimmed cells, empty cells kept, and a backslash-escaped pipe as a
// literal cell character. Per GFM an *unescaped* pipe separates cells even
// inside an inline code span, so a pipe written in a code span must be escaped
// to stay literal.
func TestSplitTableRow(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []TableCell
	}{
		{"optional pipes absent", "a | b", []TableCell{tsplit("a", 1), tsplit("b", 5)}},
		{"leading and trailing pipes", "| a | b |", []TableCell{tsplit("a", 3), tsplit("b", 7)}},
		{"single cell with pipes", "|a|", []TableCell{tsplit("a", 2)}},
		{"no pipes", "just text", []TableCell{tsplit("just text", 1)}},
		{"leading pipe only", "| a | b", []TableCell{tsplit("a", 3), tsplit("b", 7)}},
		{"trailing pipe only", "a | b |", []TableCell{tsplit("a", 1), tsplit("b", 5)}},
		{"empty middle cell kept", "a || b", []TableCell{tsplit("a", 1), tsplit("", 4), tsplit("b", 6)}},
		{"surrounding spaces and tabs trimmed", "  | \ta\t |  ", []TableCell{tsplit("a", 6)}},
		{"escaped pipe is a literal cell character", "a \\| b", []TableCell{tsplit("a | b", 1)}},
		{"escaped pipe inside a code span", "`a \\| b`", []TableCell{tsplit("`a | b`", 1)}},
		{"escaped trailing pipe is not a boundary", "| a \\|", []TableCell{tsplit("a |", 3)}},
		{"unescaped pipe inside a code span splits", "`a | b`", []TableCell{tsplit("`a", 1), tsplit("b`", 6)}},
		{"an even backslash run leaves the pipe a separator", "a \\\\| b", []TableCell{tsplit("a \\\\", 1), tsplit("b", 7)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTableRow(tt.line)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("splitTableRow(%q) = %#v, want %#v", tt.line, got, tt.want)
			}
		})
	}
}

// TestParseTable exercises the single-table parser directly: its return of the
// first line after the table, the out-of-range guard, and the alignment read
// from each delimiter cell (with extras past the header's column count
// dropped) alongside the header/delimiter mismatch Issue.
func TestParseTable(t *testing.T) {
	t.Run("returns the index after the table", func(t *testing.T) {
		lines := []string{"| a |", "| --- |", "| 1 |", "", "after"}
		table, issues, next := parseTable(lines, 0, 1)
		if next != 3 {
			t.Fatalf("next = %d, want 3", next)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %#v, want none", issues)
		}
		want := Table{
			Headers: []TableCell{tcell("a", 1, 3)},
			Align:   []string{""},
			Rows:    [][]TableCell{{tcell("1", 3, 3)}},
			Line:    1,
		}
		if !reflect.DeepEqual(table, want) {
			t.Fatalf("table = %#v, want %#v", table, want)
		}
	})

	t.Run("out of range start", func(t *testing.T) {
		table, issues, next := parseTable([]string{"| a |"}, 0, 1)
		if next != 1 || len(table.Headers) != 0 || len(issues) != 0 {
			t.Fatalf("table = %#v, issues = %#v, next = %d, want empty table, no issues, next 1", table, issues, next)
		}
	})

	t.Run("alignment per column and extras dropped", func(t *testing.T) {
		lines := []string{"| a | b |", "| :-- | --: | --- |", "| 1 | 2 |"}
		table, issues, _ := parseTable(lines, 0, 1)
		if want := []string{"left", "right"}; !reflect.DeepEqual(table.Align, want) {
			t.Fatalf("align = %#v, want %#v", table.Align, want)
		}
		want := []Issue{tcount(2, 1, "header has 2 cells, delimiter row has 3")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})
}

// TestParseTablesAlignment covers the per-column alignment read from the
// delimiter row: `---` none, `:---` left, `:---:` center, `---:` right.
func TestParseTablesAlignment(t *testing.T) {
	body := "| a | b | c | d |\n| --- | :--- | :---: | ---: |\n| 1 | 2 | 3 | 4 |\n"
	tables, issues := ParseTables(body, 1, TableOptions{})
	want := []Table{{
		Headers: []TableCell{tcell("a", 1, 3), tcell("b", 1, 7), tcell("c", 1, 11), tcell("d", 1, 15)},
		Align:   []string{"", "left", "center", "right"},
		Rows:    [][]TableCell{{tcell("1", 3, 3), tcell("2", 3, 7), tcell("3", 3, 11), tcell("4", 3, 15)}},
		Line:    1,
	}}
	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("tables = %#v, want %#v", tables, want)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

// TestParseTablesStrictColumns covers the always-on strict column counts: every
// body row must have exactly the header's cell count, and a header/delimiter
// mismatch is an error rather than a fallback to a paragraph. The body-row
// Issue names the 0-based body-row index (the first body row after the
// delimiter is rows[0]) and both counts.
func TestParseTablesStrictColumns(t *testing.T) {
	t.Run("body row too few cells", func(t *testing.T) {
		body := "| h1 | h2 |\n| --- | --- |\n| a | b |\n| only |\n"
		tables, issues := ParseTables(body, 1, TableOptions{})
		if len(tables) != 1 || len(tables[0].Rows) != 2 {
			t.Fatalf("tables = %#v, want one table with two body rows", tables)
		}
		want := []Issue{tcount(4, 1, "rows[1]: 1 cells, header has 2")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("body row too many cells", func(t *testing.T) {
		body := "| h1 | h2 |\n| --- | --- |\n| a | b | c |\n"
		_, issues := ParseTables(body, 1, TableOptions{})
		want := []Issue{tcount(3, 1, "rows[0]: 3 cells, header has 2")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("header delimiter mismatch is an error not a paragraph", func(t *testing.T) {
		body := "| h1 | h2 | h3 |\n| --- | --- |\n| a | b | c |\n"
		tables, issues := ParseTables(body, 1, TableOptions{})
		if len(tables) != 1 {
			t.Fatalf("tables = %#v, want one table (a mismatch is never a paragraph)", tables)
		}
		if want := []string{"", "", ""}; !reflect.DeepEqual(tables[0].Align, want) {
			t.Fatalf("align = %#v, want %#v (the missing delimiter column has no alignment)", tables[0].Align, want)
		}
		want := []Issue{tcount(2, 1, "header has 3 cells, delimiter row has 2")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})
}

// TestParseTablesPositions covers file-absolute positions: Table, TableCell and
// Issue lines are offset by startLine (the body's first file line), and a
// row-level Issue's column points at the row's first non-blank byte.
func TestParseTablesPositions(t *testing.T) {
	t.Run("cells and table are file-absolute with startLine", func(t *testing.T) {
		body := "\n| a | b |\n| --- | --- |\n| 1 | 2 |\n"
		tables, issues := ParseTables(body, 10, TableOptions{})
		want := []Table{{
			Headers: []TableCell{tcell("a", 11, 3), tcell("b", 11, 7)},
			Align:   []string{"", ""},
			Rows:    [][]TableCell{{tcell("1", 13, 3), tcell("2", 13, 7)}},
			Line:    11,
		}}
		if !reflect.DeepEqual(tables, want) {
			t.Fatalf("tables = %#v, want %#v", tables, want)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %#v, want none", issues)
		}
	})

	t.Run("body-row issue is positioned at the offending row", func(t *testing.T) {
		body := "\n| a | b |\n| --- | --- |\n| only |\n"
		_, issues := ParseTables(body, 10, TableOptions{})
		want := []Issue{tcount(13, 1, "rows[0]: 1 cells, header has 2")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("startLine below one is treated as one", func(t *testing.T) {
		body := "| a |\n| --- |\n"
		tables, _ := ParseTables(body, 0, TableOptions{})
		want := []Table{{
			Headers: []TableCell{tcell("a", 1, 3)},
			Align:   []string{""},
			Line:    1,
		}}
		if !reflect.DeepEqual(tables, want) {
			t.Fatalf("tables = %#v, want %#v", tables, want)
		}
	})
}

// TestParseTablesTermination covers the table ending at the first blank line or
// the start of another block: each terminator line is not absorbed as a body
// row.
func TestParseTablesTermination(t *testing.T) {
	prefix := "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
	tests := []struct {
		name string
		text string
	}{
		{"blank line", ""},
		{"ATX heading", "# Heading"},
		{"block quote", "> quote"},
		{"fenced code", "```\ncode\n```"},
		{"bullet list", "- item"},
		{"star list", "* item"},
		{"ordered list", "1. item"},
		{"thematic break", "---"},
		{"star thematic break", "***"},
		{"HTML block", "<div>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables, issues := ParseTables(prefix+tt.text, 1, TableOptions{})
			if len(tables) != 1 {
				t.Fatalf("tables = %#v, want exactly one table", tables)
			}
			if len(tables[0].Rows) != 1 {
				t.Fatalf("rows = %#v, want one body row (%s ends the table)", tables[0].Rows, tt.name)
			}
			if len(issues) != 0 {
				t.Fatalf("issues = %#v, want none", issues)
			}
		})
	}
}

// TestParseTablesLimits covers TableOptions: per-table column and row limits
// and the whole-body table count, each breach one KindTable Issue naming the
// limit and the actual count. A zero option imposes no bound.
func TestParseTablesLimits(t *testing.T) {
	t.Run("max_columns names the limit and the count", func(t *testing.T) {
		body := strings.Repeat("| a ", 12) + "|\n" + strings.Repeat("| --- ", 12) + "|\n"
		_, issues := ParseTables(body, 1, TableOptions{MaxColumns: 10})
		want := []Issue{tlimit(1, "max_columns: table has 12 columns, maximum is 10", "use at most 10 columns")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("min_columns names the limit and the count", func(t *testing.T) {
		body := "| a |\n| --- |\n"
		_, issues := ParseTables(body, 1, TableOptions{MinColumns: 2})
		want := []Issue{tlimit(1, "min_columns: table has 1 columns, minimum is 2", "use at least 2 columns")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("max_rows counts body rows only", func(t *testing.T) {
		body := "| a |\n| --- |\n| 1 |\n| 2 |\n| 3 |\n"
		_, issues := ParseTables(body, 1, TableOptions{MaxRows: 2})
		want := []Issue{tlimit(1, "max_rows: table has 3 rows, maximum is 2", "use at most 2 body rows")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("min_rows counts body rows only", func(t *testing.T) {
		body := "| a |\n| --- |\n| 1 |\n"
		_, issues := ParseTables(body, 1, TableOptions{MinRows: 2})
		want := []Issue{tlimit(1, "min_rows: table has 1 rows, minimum is 2", "use at least 2 body rows")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("max_tables at the first table's line", func(t *testing.T) {
		body := "| a |\n| --- |\n\n| b |\n| --- |\n"
		_, issues := ParseTables(body, 1, TableOptions{MaxTables: 1})
		want := []Issue{tlimit(1, "max_tables: body has 2 tables, maximum is 1", "use at most 1 tables")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("min_tables at the body's first line when there is no table", func(t *testing.T) {
		body := "just prose\n"
		_, issues := ParseTables(body, 1, TableOptions{MinTables: 1})
		want := []Issue{tlimit(1, "min_tables: body has 0 tables, minimum is 1", "use at least 1 tables")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("whole-body issue honours startLine", func(t *testing.T) {
		body := "\nplain\n"
		_, issues := ParseTables(body, 5, TableOptions{MinTables: 1})
		want := []Issue{tlimit(5, "min_tables: body has 0 tables, minimum is 1", "use at least 1 tables")}
		if !reflect.DeepEqual(issues, want) {
			t.Fatalf("issues = %#v, want %#v", issues, want)
		}
	})

	t.Run("zero options impose no bound", func(t *testing.T) {
		body := "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
		_, issues := ParseTables(body, 1, TableOptions{})
		if len(issues) != 0 {
			t.Fatalf("issues = %#v, want none", issues)
		}
	})
}

// TestParseTablesSkipsCode covers the scanner skipping fenced code blocks and
// indented code, so a table-shaped pair of lines inside code is never read as a
// table, while a table outside a fence is still found.
func TestParseTablesSkipsCode(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"backtick fence", "```\n| a | b |\n| --- | --- |\n```\n"},
		{"tilde fence", "~~~\n| a | b |\n| --- | --- |\n~~~\n"},
		{"indented code", "    | a | b |\n    | --- | --- |\n"},
		{"tab indented code", "\t| a | b |\n\t| --- | --- |\n"},
		{"unclosed fence", "```\n| a | b |\n| --- | --- |\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables, issues := ParseTables(tt.body, 1, TableOptions{})
			if len(tables) != 0 || len(issues) != 0 {
				t.Fatalf("tables = %#v, issues = %#v, want none", tables, issues)
			}
		})
	}

	t.Run("table outside a fence is still found", func(t *testing.T) {
		body := "| a |\n| --- |\n\n```\n| x | y |\n| --- | --- |\n```\n"
		tables, issues := ParseTables(body, 1, TableOptions{})
		if len(tables) != 1 || len(tables[0].Rows) != 0 {
			t.Fatalf("tables = %#v, want one table with no body rows", tables)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %#v, want none", issues)
		}
	})
}

// TestParseTablesNoDelimiterRow covers the negative: a line is a table only when
// a delimiter row immediately follows it, so a lone header-like line stays for
// the ordinary Markdown walk.
func TestParseTablesNoDelimiterRow(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"header only", "| a | b |\n"},
		{"header then prose", "| a | b |\nplain text\n"},
		{"delimiter alone", "| --- | --- |\n"},
		{"empty body", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables, issues := ParseTables(tt.body, 1, TableOptions{})
			if len(tables) != 0 || len(issues) != 0 {
				t.Fatalf("tables = %#v, issues = %#v, want none", tables, issues)
			}
		})
	}
}

// tctxRenderInline is the render-inline function TableContext takes: it records
// every segment it is asked to render and wraps the segment in a deterministic
// marker, so a test can assert both the cell's html and that the CALLER's
// renderer — never a re-implementation inside mdcheck — produced it.
type tctxRenderInline struct {
	seen []string
}

func (r *tctxRenderInline) render(segment string) htmltemplate.HTML {
	r.seen = append(r.seen, segment)
	return htmltemplate.HTML("R(" + segment + ")")
}

// tctxMap asserts v is the map one table-context entry carries.
func tctxMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("table value = %T, want map[string]any", v)
	}
	return m
}

// tctxCells asserts v is the []any a table's headers or one body row carries.
func tctxCells(t *testing.T, v any) []any {
	t.Helper()
	cells, ok := v.([]any)
	if !ok {
		t.Fatalf("cell list = %T, want []any", v)
	}
	return cells
}

// tctxHTML asserts v is a `{html, attrs}` cell map and returns its rendered
// html. The cell map carries exactly the html and attrs keys, and html is an
// html/template.HTML — the trusted type a template injects unescaped.
func tctxHTML(t *testing.T, v any) string {
	t.Helper()
	m := tctxMap(t, v)
	if len(m) != 2 {
		t.Errorf("cell = %v, want exactly the html and attrs keys", m)
	}
	html, ok := m["html"].(htmltemplate.HTML)
	if !ok {
		t.Fatalf("cell html = %T, want html/template.HTML", m["html"])
	}
	if _, ok := m["attrs"].(map[string]any); !ok {
		t.Fatalf("cell attrs = %T, want map[string]any", m["attrs"])
	}
	return string(html)
}

// TestTableContextColumnsInOrder pins the shared TableContext's table-map shape
// (table-layout-context, gfm-tables): headers, align and every body row carry one
// entry per column, in column order, and each entry is a `{html, attrs}` map
// whose html the caller's render-inline function produced.
func TestTableContextColumnsInOrder(t *testing.T) {
	src := "| H1 | H2 | H3 |\n| :--- | :---: | ---: |\n| a | b | c |\n| d | e | f |\n"
	rec := &tctxRenderInline{}
	table, tables := TableContext(src, nil, rec.render)

	if len(tables) != 1 {
		t.Fatalf("TableContext tables = %d entries, want 1", len(tables))
	}
	tm := tctxMap(t, table)

	headers := tctxCells(t, tm["headers"])
	for i, want := range []string{"H1", "H2", "H3"} {
		if got := tctxHTML(t, headers[i]); got != "R("+want+")" {
			t.Errorf("header[%d] html = %q, want %q", i, got, "R("+want+")")
		}
	}

	align, ok := tm["align"].([]string)
	if !ok {
		t.Fatalf("TableContext align = %T, want []string", tm["align"])
	}
	if want := []string{"left", "center", "right"}; !reflect.DeepEqual(align, want) {
		t.Errorf("TableContext align = %v, want %v", align, want)
	}

	rows := tctxCells(t, tm["rows"])
	wantRows := [][]string{{"a", "b", "c"}, {"d", "e", "f"}}
	if len(rows) != len(wantRows) {
		t.Fatalf("TableContext rows = %d entries, want %d", len(rows), len(wantRows))
	}
	for r, wantRow := range wantRows {
		cells := tctxCells(t, rows[r])
		if len(cells) != len(wantRow) {
			t.Fatalf("TableContext row[%d] = %d cells, want %d", r, len(cells), len(wantRow))
		}
		for c, want := range wantRow {
			if got := tctxHTML(t, cells[c]); got != "R("+want+")" {
				t.Errorf("TableContext row[%d] cell[%d] html = %q, want %q", r, c, got, "R("+want+")")
			}
		}
	}
}

// TestTableContextCellsThroughRenderInline pins that TableContext builds every
// cell's `{html, attrs}` map through the render-inline function it is passed
// (table-cell-attributes, table-layout-context): the function receives each
// decoded `{br}` segment of the cell, the rendered segments are joined with a
// literal `<br>`, and attrs carries a nil entry for each declared attribute the
// cell leaves unset.
func TestTableContextCellsThroughRenderInline(t *testing.T) {
	specs := []CellAttributeSpec{
		{Name: "bold", Kind: CellAttributeBoolean},
		{Name: "highlight", Kind: CellAttributeEnum, Variants: []string{"green", "red"}},
	}
	src := "| head |\n| --- |\n| x{bold} y{br}**z** |\n"
	rec := &tctxRenderInline{}
	table, _ := TableContext(src, specs, rec.render)

	rows := tctxCells(t, tctxMap(t, table)["rows"])
	cells := tctxCells(t, rows[0])
	cell := tctxMap(t, cells[0])

	if got, want := string(cell["html"].(htmltemplate.HTML)), "R(x y)<br>R(**z**)"; got != want {
		t.Errorf("cell html = %q, want %q", got, want)
	}
	if got, want := rec.seen, []string{"head", "x y", "**z**"}; !reflect.DeepEqual(got, want) {
		t.Errorf("renderInline segments = %q, want %q", got, want)
	}
	attrs, ok := cell["attrs"].(map[string]any)
	if !ok {
		t.Fatalf("cell attrs = %T, want map[string]any", cell["attrs"])
	}
	if attrs["bold"] != true {
		t.Errorf("attrs[bold] = %v, want true", attrs["bold"])
	}
	if v, present := attrs["highlight"]; !present || v != nil {
		t.Errorf("attrs[highlight] = %v (present %v), want a present nil entry", v, present)
	}
}

// TestTableContextNoTable pins the always-present no-table shape
// (table-layout-context, gfm-tables): a body carrying no table yields table as a
// TYPED nil map — `{{ if .table }}` falsy and `{{ range .table.rows }}` a no-op —
// and tables as a non-nil, empty list rather than a bare nil.
func TestTableContextNoTable(t *testing.T) {
	table, tables := TableContext("A paragraph, and no table at all.\n", nil, (&tctxRenderInline{}).render)

	tm, ok := table.(map[string]any)
	if !ok {
		t.Fatalf("TableContext table = %T, want map[string]any", table)
	}
	if tm != nil {
		t.Errorf("TableContext table = %#v, want a typed nil map", tm)
	}
	if len(tm) != 0 {
		t.Errorf("TableContext len(table) = %d, want 0", len(tm))
	}
	if tables == nil {
		t.Error("TableContext tables = nil, want a non-nil empty list")
	} else if len(tables) != 0 {
		t.Errorf("TableContext tables = %v, want an empty list", tables)
	}
}

// TestTableContextFirstTable pins that .table is the body's FIRST parsed table
// while .tables lists every table in source order (table-layout-context,
// gfm-tables).
func TestTableContextFirstTable(t *testing.T) {
	src := "| A1 | A2 |\n| --- | --- |\n| a | b |\n\nIntro.\n\n| B1 | B2 |\n| --- | --- |\n| c | d |\n"
	table, tables := TableContext(src, nil, (&tctxRenderInline{}).render)

	if len(tables) != 2 {
		t.Fatalf("TableContext tables = %d entries, want 2", len(tables))
	}
	first := tctxCells(t, tctxMap(t, tables[0])["headers"])
	second := tctxCells(t, tctxMap(t, tables[1])["headers"])
	if got := tctxHTML(t, first[0]); got != "R(A1)" {
		t.Errorf("tables[0] first header = %q, want %q", got, "R(A1)")
	}
	if got := tctxHTML(t, second[0]); got != "R(B1)" {
		t.Errorf("tables[1] first header = %q, want %q in source order", got, "R(B1)")
	}
	exposed := tctxCells(t, tctxMap(t, table)["headers"])
	if got := tctxHTML(t, exposed[0]); got != "R(A1)" {
		t.Errorf(".table first header = %q, want the first table's %q", got, "R(A1)")
	}
}
