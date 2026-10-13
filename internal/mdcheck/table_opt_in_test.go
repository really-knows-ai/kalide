package mdcheck

import (
	"reflect"
	"testing"
)

// TestCheckTableOptIn covers the GFM-table opt-in gate as mdcheck.Check exposes
// it (gfm-tables, table-opt-in). A body whose template has not opted in —
// Options.Tables nil — refuses the table at its delimiter row with guidance
// naming the `tables` option; an opted-in body's table is accepted; and the
// whole-body table pass's strict-column-count, limit and unknown-attribute
// violations are positioned at the offending row or cell, the last carrying the
// closest-match suggestion over the declared cell attributes.
func TestCheckTableOptIn(t *testing.T) {
	// The opt-out refusal: with no tables option the whole-body table pass is
	// off and the paragraph that carries the table is refused at its delimiter
	// row — the line that makes it a table — with guidance naming `tables`.
	t.Run("non-opted-in body refuses a table and names the tables option", func(t *testing.T) {
		src := "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
		got := Check(testFile, []byte(src), Options{Mode: BodyMode})
		want := []Issue{issue(2, 1, KindDisallowedConstruct,
			"tables are not allowed",
			"opt in to tables with the `tables` option in the template's body rule, or use a list instead")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Check(%q) with no tables option\n got: %#v\nwant: %#v", src, got, want)
		}
	})

	// An opted-in body accepts a sound table: the opt-out refusal no longer
	// fires and the table pass finds no count, limit or attribute breach.
	t.Run("opted-in body accepts a plain table", func(t *testing.T) {
		src := "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
		opts := Options{Mode: BodyMode, Tables: &TableOptions{}}
		if got := Check(testFile, []byte(src), opts); len(got) != 0 {
			t.Fatalf("Check(%q) with tables opted in = %#v, want no issues", src, got)
		}
	})

	// The same, with declared cell attributes: a cell that sets only declared
	// attributes decodes cleanly, so the table is still accepted.
	t.Run("opted-in body accepts a table using declared cell attributes", func(t *testing.T) {
		src := "| a | b |\n| --- | --- |\n| a {bold} | b {highlight:green} |\n"
		opts := Options{Mode: BodyMode, Tables: &TableOptions{}, CellAttributes: cellTestSpecs}
		if got := Check(testFile, []byte(src), opts); len(got) != 0 {
			t.Fatalf("Check(%q) with declared cell attributes = %#v, want no issues", src, got)
		}
	})

	// The table pass's strict column count: a body row of the wrong width is
	// one KindTable issue positioned at that row.
	t.Run("strict column count is positioned at the offending row", func(t *testing.T) {
		src := "| h1 | h2 |\n| --- | --- |\n| a | b | c |\n"
		got := Check(testFile, []byte(src), Options{Mode: BodyMode, Tables: &TableOptions{}})
		want := []Issue{issue(3, 1, KindTable, "rows[0]: 3 cells, header has 2", "")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Check(%q) strict count\n got: %#v\nwant: %#v", src, got, want)
		}
	})

	// A breached limit is one KindTable issue naming the limit and the actual
	// count, positioned at the table's header row and carrying the fix.
	t.Run("limit breach names the limit and the count", func(t *testing.T) {
		src := "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
		opts := Options{Mode: BodyMode, Tables: &TableOptions{MaxColumns: 1}}
		got := Check(testFile, []byte(src), opts)
		want := []Issue{issue(1, 0, KindTable,
			"max_columns: table has 2 columns, maximum is 1",
			"use at most 1 columns")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Check(%q) with max_columns\n got: %#v\nwant: %#v", src, got, want)
		}
	})

	// An undeclared cell attribute is one KindTable issue positioned at the
	// offending cell and carrying the closest-match suggestion over the
	// template's declared names.
	t.Run("unknown cell attribute carries the closest-match suggestion", func(t *testing.T) {
		src := "| h1 | h2 |\n| --- | --- |\n| a {hilight} | b |\n"
		opts := Options{Mode: BodyMode, Tables: &TableOptions{}, CellAttributes: cellTestSpecs}
		got := Check(testFile, []byte(src), opts)
		want := []Issue{issue(3, 3, KindTable,
			`unknown cell attribute "hilight": did you mean "highlight"?`,
			"use one of the template's declared cell attributes, or remove the {…} group")}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Check(%q) with an unknown cell attribute\n got: %#v\nwant: %#v", src, got, want)
		}
	})
}
