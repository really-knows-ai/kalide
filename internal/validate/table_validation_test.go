package validate

import (
	"os"
	"reflect"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// This file hosts the validate half of the GFM-table deliverable (table-errors,
// gfm-tables). TestCheckTables pins the error adaptation internal/validate
// performs in checkTables: every mdcheck table issue (parse, strict column
// count, declared limit, unknown/cell attribute) becomes a positioned
// ValidationError carrying the file, the table's line, the within-body path
// (body, rows[i], cell) and a fix. TestTableValidationInt drives that adaptation
// through the real validate pipeline (Validate / ValidateAll) over an on-disk
// deck, guarded by testing.Short per the repo's integration convention.
//
// TestCheckTables and TestTableValidationInt are filled in by
// gfm-tables/plan.phase-04.task-11 and task-12 respectively.

// TestCheckTables pins validate.checkTables' adaptation of mdcheck's table
// issues into positioned ValidationErrors (table-errors). Each subtest fixes a
// table violation and the exact ValidationError it must become: the file and the
// file-absolute line (into the table), the within-body path (body for a
// whole-table limit or a header/delimiter mismatch, body › rows[i] for a body
// row whose width differs from the header's, body › cell for a cell-attribute
// violation), the message (What) and the fix. Limit and cell-attribute issues
// carry mdcheck's own guidance as the fix; the strict-column-count issues the
// parser raises carry none, so checkTables supplies one naming the cell count to
// match the header. The enclosing instance chain (prefix) prepends the path, and
// startLine offsets every position file-absolutely.
func TestCheckTables(t *testing.T) {
	const file = "slides/1-a.md"

	// A template that does not opt in to tables (a nil rule) has no table pass
	// to run: checkTables reports nothing, so a table's refusal stays the
	// body's ordinary Markdown-subset check (mdcheck.checkDisallowed).
	if got := checkTables(file, "| a | b |\n| - | - |\n", 1, nil, nil); got != nil {
		t.Fatalf("checkTables with a nil table rule = %v, want nil", got)
	}

	// The declared cell attributes the opt-in rule carries: a boolean and an
	// enum with two variants, the candidate set the closest-match suggestion is
	// drawn from.
	cellAttributes := []template.CellAttribute{
		{Name: "bold", Kind: template.FieldBoolean},
		{Name: "highlight", Kind: template.FieldEnum, Variants: []string{"red", "green"}},
	}

	const (
		// A sound table: header and delimiter agree and the body row matches.
		soundTable = "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
		// Header of 2 cells, delimiter of 3: a header/delimiter mismatch.
		mismatchedDelimiter = "| a | b |\n| --- | --- | --- |\n| 1 | 2 |\n"
		// Header of 2 cells, body row of 3: a strict-column-count breach.
		wideRow = "| h1 | h2 |\n| --- | --- |\n| a | b | c |\n"
		// A body cell naming an undeclared attribute, close to `highlight`.
		unknownAttribute = "| h1 | h2 |\n| --- | --- |\n| a {hilight} | b |\n"
	)

	tests := []struct {
		name      string
		body      string
		startLine int
		rule      *template.TableRule
		prefix    []string
		want      []ValidationError
	}{
		{
			name:      "opted-in sound table yields no errors",
			body:      soundTable,
			startLine: 1,
			rule:      &template.TableRule{},
			want:      nil,
		},
		{
			name:      "header/delimiter mismatch is positioned on the delimiter row",
			body:      mismatchedDelimiter,
			startLine: 1,
			rule:      &template.TableRule{},
			want: []ValidationError{New(file, 2, []string{"body"},
				"header has 2 cells, delimiter row has 3",
				"give the delimiter row 2 cells to match the header")},
		},
		{
			name:      "strict column count names the row and supplies the fix",
			body:      wideRow,
			startLine: 1,
			rule:      &template.TableRule{},
			want: []ValidationError{New(file, 3, []string{"body", "rows[0]"},
				"3 cells, header has 2",
				"make every row have 2 cells to match the header")},
		},
		{
			name:      "limit breach names the limit and carries mdcheck's guidance",
			body:      soundTable,
			startLine: 1,
			rule:      &template.TableRule{MaxColumns: 1},
			want: []ValidationError{New(file, 1, []string{"body"},
				"max_columns: table has 2 columns, maximum is 1",
				"use at most 1 columns")},
		},
		{
			name:      "whole-body limit is positioned at the body's first line",
			body:      "just a paragraph\n",
			startLine: 1,
			rule:      &template.TableRule{MinTables: 1},
			want: []ValidationError{New(file, 1, []string{"body"},
				"min_tables: body has 0 tables, minimum is 1",
				"use at least 1 tables")},
		},
		{
			name:      "unknown cell attribute carries the closest-match suggestion",
			body:      unknownAttribute,
			startLine: 1,
			rule:      &template.TableRule{CellAttributes: cellAttributes},
			want: []ValidationError{New(file, 3, []string{"body", "cell"},
				`unknown cell attribute "hilight": did you mean "highlight"?`,
				"use one of the template's declared cell attributes, or remove the {…} group")},
		},
		{
			name:      "prefix prepends the enclosing instance chain",
			body:      wideRow,
			startLine: 1,
			rule:      &template.TableRule{},
			prefix:    []string{"columns[0]", "blocks[0]"},
			want: []ValidationError{New(file, 3, []string{"columns[0]", "blocks[0]", "body", "rows[0]"},
				"3 cells, header has 2",
				"make every row have 2 cells to match the header")},
		},
		{
			name:      "startLine positions the offending row file-absolutely",
			body:      wideRow,
			startLine: 10,
			rule:      &template.TableRule{},
			want: []ValidationError{New(file, 12, []string{"body", "rows[0]"},
				"3 cells, header has 2",
				"make every row have 2 cells to match the header")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkTables(file, tt.body, tt.startLine, tt.rule, tt.prefix)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("checkTables(%q, startLine=%d) =\n  %#v\nwant\n  %#v",
					tt.body, tt.startLine, got, tt.want)
			}
		})
	}
}

// TestTableValidationInt drives table validation through the real validate
// pipeline (Validate / ValidateAll) over on-disk decks whose templates/ library
// holds a slide that opts in to GFM tables (grid: max_columns 3 and the bold /
// highlight cell attributes) beside a slide and a section that do not (flow,
// panel). It reads the real filesystem, so it is skipped under -short like its
// siblings.
//
// It pins:
//
//   - a table in a non-opted-in slide body is refused at its delimiter row by
//     the ordinary Markdown-subset check (mdcheck.checkDisallowed, the single
//     opt-out refusal), with the guidance naming the `tables` option;
//   - a table in a non-opted-in section body is refused the same way, carrying
//     the instance's containment path (`panels[0]`);
//   - an opted-in table's limit, cell-attribute and strict-column-count
//     violations surface as positioned ValidationErrors with the table's line,
//     the body/row/cell path and the fix;
//   - fail-fast (Validate) returns the first error of the fixed order and
//     collect-all (ValidateAll) returns every error; and
//   - each table error appears exactly once under collect-all — no duplicate
//     row for one table construct — because validate.checkBodyRule skips
//     CheckBody's table pass and the deck path's checkBody + checkTables is the
//     single source.
func TestTableValidationInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: table validation through the real validate pipeline")
	}

	// loadDeck writes the table-validation library plus the given slides
	// (keyed by their slides/ basename) to a fresh temp deck and loads it into
	// a template registry and theme registry.
	loadDeck := func(t *testing.T, slides map[string]string) (string, *template.Registry, *theme.Registry) {
		t.Helper()
		files := tableValidationLibraryFiles()
		files["kalide.yaml"] = "title: Table Deck\ntheme: plain\n"
		for name, src := range slides {
			files["slides/"+name] = src
		}
		dir := writeDeck(t, files)
		reg, themes := loadFixtureLibrary(t, dir)
		return dir, reg, themes
	}

	// The single opt-out refusal's message and guidance (mdcheck.checkDisallowed).
	const (
		optOutWhat = "tables are not allowed"
		optOutFix  = "opt in to tables with the `tables` option in the template's body rule, or use a list instead"
	)

	t.Run("a non-opted-in slide body's table is refused", func(t *testing.T) {
		dir, reg, themes := loadDeck(t, map[string]string{"1-a.md": tableSlideFlowNoOptIn})
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the slide body's table to be refused")
		}
		want := displayFile("slides/1-a.md") + ":7: " + optOutWhat + fixSeparator + optOutFix
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})

	t.Run("a non-opted-in section body's table is refused", func(t *testing.T) {
		dir, reg, themes := loadDeck(t, map[string]string{"1-a.md": tableSlideFlowSectionNoOptIn})
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the section body's table to be refused")
		}
		want := displayFile("slides/1-a.md") + ":13" + segmentSeparator + "panels[0]: " +
			optOutWhat + fixSeparator + optOutFix
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})

	t.Run("an opted-in table's limit, attribute and count errors surface", func(t *testing.T) {
		tests := []struct {
			name  string
			slide string
			want  string
		}{
			{
				name:  "limit breach names the limit and carries the guidance",
				slide: tableSlideGridLimit,
				want: displayFile("slides/1-a.md") + ":6" + segmentSeparator +
					"body: max_columns: table has 4 columns, maximum is 3" + fixSeparator + "use at most 3 columns",
			},
			{
				name:  "unknown cell attribute carries the closest-match suggestion",
				slide: tableSlideGridAttribute,
				want: displayFile("slides/1-a.md") + ":8" + segmentSeparator + "body" + segmentSeparator +
					`cell: unknown cell attribute "hilight": did you mean "highlight"?` + fixSeparator +
					"use one of the template's declared cell attributes, or remove the {…} group",
			},
			{
				name:  "strict column count is positioned at the offending row",
				slide: tableSlideGridStrictCount,
				want: displayFile("slides/1-a.md") + ":8" + segmentSeparator + "body" + segmentSeparator +
					"rows[0]: 3 cells, header has 2" + fixSeparator + "make every row have 2 cells to match the header",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dir, reg, themes := loadDeck(t, map[string]string{"1-a.md": tt.slide})
				verr, invalid := Validate(os.DirFS(dir), reg, themes)
				if !invalid {
					t.Fatalf("Validate() invalid = false, want error %q", tt.want)
				}
				if got := Format(verr); got != tt.want {
					t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, tt.want)
				}
			})
		}
	})

	t.Run("fail-fast returns the first error and collect-all returns every error exactly once", func(t *testing.T) {
		dir, reg, themes := loadDeck(t, map[string]string{
			"1-a.md": tableSlideGridMultiError,
			"2-b.md": tableSlideFlowNoOptIn,
			"3-c.md": tableSlideFlowSectionNoOptIn,
		})
		fsys := os.DirFS(dir)

		// The fixed order: slide 1's three table errors in the table pass's
		// order (the limit at line 6 before the same-line cell violation, then
		// the body-row count at line 8), then slide 2's slide-body refusal,
		// then slide 3's section-body refusal.
		want := []ValidationError{
			New("slides/1-a.md", 6, []string{"body"},
				"max_columns: table has 4 columns, maximum is 3", "use at most 3 columns"),
			New("slides/1-a.md", 6, []string{"body", "cell"},
				`unknown cell attribute "hilight": did you mean "highlight"?`,
				"use one of the template's declared cell attributes, or remove the {…} group"),
			New("slides/1-a.md", 8, []string{"body", "rows[0]"},
				"3 cells, header has 4", "make every row have 4 cells to match the header"),
			New("slides/2-b.md", 7, nil, optOutWhat, optOutFix),
			New("slides/3-c.md", 13, []string{"panels[0]"}, optOutWhat, optOutFix),
		}

		all := ValidateAll(fsys, reg, themes)
		if len(all) != len(want) {
			t.Fatalf("ValidateAll() returned %d error(s), want %d:\n  %q", len(all), len(want), formatAll(all))
		}
		// Each table error appears exactly once: no two errors render
		// identically, so CheckBody's suppressed table pass cannot have added a
		// duplicate row for one device.
		seen := make(map[string]bool, len(all))
		for i := range want {
			if !reflect.DeepEqual(all[i], want[i]) {
				t.Errorf("ValidateAll()[%d] =\n  %#v\nwant\n  %#v", i, all[i], want[i])
			}
			got := Format(all[i])
			if seen[got] {
				t.Errorf("ValidateAll()[%d] repeats an earlier error: %q", i, got)
			}
			seen[got] = true
		}

		// Fail-fast returns exactly the earliest error of that order.
		verr, invalid := Validate(fsys, reg, themes)
		if !invalid {
			t.Fatalf("Validate() invalid = false, want the earliest error %q", Format(want[0]))
		}
		if !reflect.DeepEqual(verr, all[0]) {
			t.Fatalf("Validate() error =\n  %#v\nwant ValidateAll()[0]\n  %#v", verr, all[0])
		}
	})
}

// tableSlideFlowNoOptIn is a deck slide on the non-opted-in `flow` template
// whose body is a GFM table: the ordinary Markdown-subset check refuses it at
// the table's delimiter row (file line 7).
const tableSlideFlowNoOptIn = `---
template: flow
title: Flow
---

| a | b |
| --- | --- |
| 1 | 2 |
`

// tableSlideFlowSectionNoOptIn is a deck slide on the non-opted-in `flow`
// template whose `panels` section body is a GFM table: the section template
// `panel` does not opt in, so the table is refused at its delimiter row (file
// line 13) carrying the instance's containment path `panels[0]`.
const tableSlideFlowSectionNoOptIn = `---
template: flow
title: Flow
---

# panels
` + "```" + `
template: panel
label: Sample
` + "```" + `

| a | b |
| --- | --- |
| 1 | 2 |
`

// tableSlideGridLimit is a deck slide on the opted-in `grid` template whose
// table has four columns, one over the declared max_columns of three.
const tableSlideGridLimit = `---
template: grid
title: Grid
---

| a | b | c | d |
| --- | --- | --- | --- |
| 1 | 2 | 3 | 4 |
`

// tableSlideGridAttribute is a deck slide on the opted-in `grid` template with
// a body cell naming the undeclared attribute `hilight`, close to the declared
// `highlight` enum.
const tableSlideGridAttribute = `---
template: grid
title: Grid
---

| h1 | h2 |
| --- | --- |
| a {hilight} | b |
`

// tableSlideGridStrictCount is a deck slide on the opted-in `grid` template
// whose body row has three cells against a two-cell header.
const tableSlideGridStrictCount = `---
template: grid
title: Grid
---

| h1 | h2 |
| --- | --- |
| a | b | c |
`

// tableSlideGridMultiError is a deck slide on the opted-in `grid` template whose
// one table yields three errors at once: a four-column header over max_columns
// 3, an undeclared cell attribute and a three-cell body row against the
// four-cell header. It proves one table construct contributes each of its
// errors exactly once under collect-all.
const tableSlideGridMultiError = `---
template: grid
title: Grid
---

| a {hilight} | b | c | d |
| --- | --- | --- | --- |
| 1 | 2 | 3 |
`

// tableValidationLibraryFiles returns an on-disk templates/ library for the
// table-validation decks: `grid`, a slide template that opts in to GFM tables
// (max_columns 3, the boolean `bold` and enum `highlight` cell attributes);
// `flow`, a slide template that does not opt in and declares a `panels` section
// accepting `panel`; and `panel`, a section template that does not opt in. Each
// template's example is table-free, so the library itself loads cleanly.
func tableValidationLibraryFiles() map[string]string {
	return map[string]string{
		"templates/library.yaml":           "name: table-validation-fixture\nformat: 1\n",
		"templates/themes/plain/theme.css": "body { margin: 0; }\n",

		"templates/slides/grid/template.yaml": "description: a slide whose body opts in to tables\n" +
			"fields:\n  - name: title\n    type: text\n" +
			"body:\n  mode: optional\n  tables:\n    max_columns: 3\n    cell_attributes:\n" +
			"      - name: bold\n        type: boolean\n" +
			"      - name: highlight\n        type: enum\n        variants: [red, green]\n",
		"templates/slides/grid/layout.html.tmpl": "<section>{{.title}}</section>",
		"templates/slides/grid/example.md":       "---\ntemplate: grid\ntitle: Grid\n---\n\nPlain body.\n",

		"templates/slides/flow/template.yaml": "description: a slide whose body does not opt in to tables\n" +
			"fields:\n  - name: title\n    type: text\n" +
			"sections:\n  - name: panels\n    accepted: [panel]\n" +
			"body:\n  mode: optional\n",
		"templates/slides/flow/layout.html.tmpl": "<section>{{.title}}{{range .panels}}<div class=\"panel\">{{.}}</div>{{end}}</section>",
		"templates/slides/flow/example.md": "---\ntemplate: flow\ntitle: Flow\n---\n\n" +
			"# panels\n```\ntemplate: panel\nlabel: Sample\n```\n",

		"templates/sections/panel/template.yaml":    "description: a section whose body does not opt in to tables\nfields:\n  - name: label\n    type: text\nbody:\n  mode: optional\n",
		"templates/sections/panel/layout.html.tmpl": "<div class=\"panel\">{{.label}}</div>",
		"templates/sections/panel/example.md":       "```\nlabel: Sample\n```\n",
	}
}
