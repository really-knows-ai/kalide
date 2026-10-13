package mdcheck

import (
	"reflect"
	"testing"
)

// TestParseTablesBlockStartHeader covers the feedback-16 fix: a line that
// begins another block is never a table header row, even when a delimiter row
// follows it. GFM reads such a construct as the other block, so ParseTables
// must yield no table at all (table-syntax).
//
// Before the fix, ParseTables only tested that the header candidate was
// non-blank, so `# a | b`, `> a | b`, `- a | b` and `1. a | b` were each read
// as a header row when `---|---` followed, stealing the line from the ordinary
// Markdown walk (and from the opt-out refusal in checkDisallowed).
func TestParseTablesBlockStartHeader(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
	}{
		{"ATX heading", "# a | b"},
		{"block quote", "> a | b"},
		{"bullet list item", "- a | b"},
		{"ordered list item", "1. a | b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.candidate + "\n---|---\n"
			tables, issues := ParseTables(body, 1, TableOptions{})
			if len(tables) != 0 || len(issues) != 0 {
				t.Fatalf("ParseTables(%q) = tables %#v, issues %#v; want no table (a %s begins another block)",
					body, tables, issues, tt.name)
			}
		})
	}
}

// TestParseTablesLessThanRows covers the two halves of the feedback-16 HTML
// fix: a `<` that does not begin a CommonMark HTML block (`< 5 | x`) is
// ordinary table text — as a header candidate it founds the table, as a body
// row it is kept — while a real HTML block (`<div>`) still ends the table
// (table-syntax).
func TestParseTablesLessThanRows(t *testing.T) {
	t.Run("less-than header candidate founds the table", func(t *testing.T) {
		tables, issues := ParseTables("< 5 | x\n---|---\n", 1, TableOptions{})
		want := []Table{{
			Headers: []TableCell{tcell("< 5", 1, 1), tcell("x", 1, 7)},
			Align:   []string{"", ""},
			Line:    1,
		}}
		if !reflect.DeepEqual(tables, want) {
			t.Fatalf("tables = %#v, want %#v", tables, want)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %#v, want none", issues)
		}
	})

	t.Run("less-than body row stays in the table", func(t *testing.T) {
		body := "| a | b |\n| --- | --- |\n< 5 | x\n"
		tables, issues := ParseTables(body, 1, TableOptions{})
		want := []Table{{
			Headers: []TableCell{tcell("a", 1, 3), tcell("b", 1, 7)},
			Align:   []string{"", ""},
			Rows:    [][]TableCell{{tcell("< 5", 3, 1), tcell("x", 3, 7)}},
			Line:    1,
		}}
		if !reflect.DeepEqual(tables, want) {
			t.Fatalf("tables = %#v, want %#v", tables, want)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %#v, want none", issues)
		}
	})

	t.Run("real HTML block body row ends the table", func(t *testing.T) {
		body := "| a | b |\n| --- | --- |\n<div>\n"
		tables, issues := ParseTables(body, 1, TableOptions{})
		if len(tables) != 1 {
			t.Fatalf("tables = %#v, want exactly one table", tables)
		}
		if len(tables[0].Rows) != 0 {
			t.Fatalf("rows = %#v, want none (<div> is an HTML block and ends the table)", tables[0].Rows)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %#v, want none", issues)
		}
	})
}

// TestCheckTableOptOutBlockStart covers the walk-side half of feedback-16: the
// opt-out refusal (Options.Tables nil) is driven by hasTableDelimiterRow, which
// now scans a paragraph from its second line. A delimiter row with no header
// row before it in the same paragraph is therefore not refused, while a real
// two-line table still is.
func TestCheckTableOptOutBlockStart(t *testing.T) {
	t.Run("heading-delimiter pair gives only the heading error", func(t *testing.T) {
		// `# a | b` is an ATX heading and `---|---` a plain paragraph, not a
		// table: the heading is refused on its own and no table refusal fires.
		src := "# a | b\n---|---\n"
		got := Check(testFile, []byte(src), Options{Mode: BodyMode})
		want := []Issue{issue(1, 3, KindUnsupportedConstruct,
			"heading level 1 is not allowed",
			"use ## or ###; # is reserved for slide sections and ####+ is not supported")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Check(%q) = %#v, want only the heading error", src, got)
		}
	})

	t.Run("a lone delimiter row is not a table", func(t *testing.T) {
		// `---|---` alone is a paragraph: with no header line before it there
		// is nothing to refuse as a table.
		src := "---|---\n"
		got := Check(testFile, []byte(src), Options{Mode: BodyMode})
		for _, iss := range got {
			if iss.Message == "tables are not allowed" {
				t.Fatalf("Check(%q) = %#v, want no table refusal", src, got)
			}
		}
	})

	t.Run("header-delimiter pair is still refused as a table", func(t *testing.T) {
		src := "a | b\n---|---\n"
		got := Check(testFile, []byte(src), Options{Mode: BodyMode})
		want := []Issue{issue(2, 1, KindDisallowedConstruct,
			"tables are not allowed",
			"opt in to tables with the `tables` option in the template's body rule, or use a list instead")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Check(%q) = %#v, want the table refusal at the delimiter row", src, got)
		}
	})
}
