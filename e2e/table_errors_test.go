package e2e

// This file is the gfm-tables plan.phase-04 task-14 end-to-end test for the
// table-validation error surface (requirements.requirement.table-errors). It
// drives the real kalide binary the harness builds (harness.go) through
// Harness.Run and Harness.Start, so it exercises exactly what an author runs
// from a shell and sees in a browser:
//
//   - `kalide check` reports the deck's table violations in the terminal, in
//     the fixed whole-deck-validation order: fail-fast (exit 1) yields the
//     single first error, `--all` (exit 1) every error, and `--json` carries
//     the same file/line/path/message the text mode prints;
//   - `kalide start --no-open` on the same broken deck prints the identical
//     first table error to the terminal and exits 1 without binding a port
//     (calide never serves a broken deck, cli-start); and
//   - a running `kalide start` whose deck is live-edited into a table error
//     serves the full-page browser error carrying the same identical text,
//     and prints it to the terminal (error-reporting).
//
// The fixture is a two-template library: `grid` opts in to GFM tables
// (max_columns 3 plus the declared boolean `bold` and enum `highlight` cell
// attributes), while `flow` does not opt in. Four slides exercise each error
// class the phase adapts — a strict column count on `grid`, a declared-limit
// breach on `grid`, an unknown cell attribute with a closest-match suggestion
// on `grid`, and a non-opted-in table refused with guidance naming the `tables`
// option on `flow` — so `--all` collects every one exactly once.
//
// The tests build a binary and drive a real server, so they are skipped under
// -short.

import (
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tableErrorsDeckConfig is the fixture deck's kalide.yaml: it draws from a
// single local templates/ layer and selects the `plain` theme that layer
// carries.
const tableErrorsDeckConfig = `title: Table Errors Deck
author: Ada Lovelace
theme: plain
`

// tableErrorsGridManifest is the opted-in slide template: its body
// rule carries the `tables` mapping with a maximum of three columns and two
// declared cell attributes (a boolean and an enum with two variants), the set
// the unknown-attribute closest-match suggestion is drawn from.
const tableErrorsGridManifest = `description: a slide whose body opts in to tables
fields:
  - name: title
    type: text
body:
  mode: optional
  tables:
    max_columns: 3
    cell_attributes:
      - name: bold
        type: boolean
      - name: highlight
        type: enum
        variants: [red, green]
`

// tableErrorsFlowManifest is the non-opted-in slide template: its body rule has
// no `tables` option, so a table in its body is refused.
const tableErrorsFlowManifest = `description: a slide whose body does not opt in to tables
fields:
  - name: title
    type: text
body:
  mode: optional
`

const (
	tableErrorsGridLayout = `<section class="grid">{{.title}}</section>` + "\n"
	tableErrorsFlowLayout = `<section class="flow">{{.title}}</section>` + "\n"

	// tableErrorsGridExample and tableErrorsFlowExample are table-free, so the
	// library itself loads cleanly and the failures under test are deck
	// validation errors, not load errors.
	tableErrorsGridExample = `---
template: grid
title: Grid Example
---

Plain body.
`

	tableErrorsFlowExample = `---
template: flow
title: Flow Example
---

Plain body.
`
)

// tableErrorsStrictCountSlide is a `grid` slide whose body row has three cells
// against a two-cell header: a strict-column-count breach positioned at the
// offending body row (file line 8).
const tableErrorsStrictCountSlide = `---
template: grid
title: Grid
---

| h1 | h2 |
| --- | --- |
| a | b | c |
`

// tableErrorsLimitSlide is a `grid` slide whose table has four columns, one over
// the declared max_columns of three: a limit breach positioned at the table's
// first line (file line 6).
const tableErrorsLimitSlide = `---
template: grid
title: Grid
---

| a | b | c | d |
| --- | --- | --- | --- |
| 1 | 2 | 3 | 4 |
`

// tableErrorsAttributeSlide is a `grid` slide whose body cell names the
// undeclared attribute `hilight`, close to the declared `highlight` enum: an
// unknown cell attribute carrying the closest-match suggestion, positioned at
// the offending body row (file line 8).
const tableErrorsAttributeSlide = `---
template: grid
title: Grid
---

| h1 | h2 |
| --- | --- |
| a {hilight} | b |
`

// tableErrorsOptOutSlide is a `flow` slide whose body is a GFM table, but `flow`
// does not opt in: the ordinary Markdown-subset check refuses it at the table's
// delimiter row (file line 7) with guidance naming the `tables` option.
const tableErrorsOptOutSlide = `---
template: flow
title: Flow
---

| a | b |
| --- | --- |
| 1 | 2 |
`

// tableErrorsValidSlide is a table-free `grid` body: the deck served while the
// browser error page is exercised, before it is live-edited into the strict
// column count.
const tableErrorsValidSlide = `---
template: grid
title: Grid
---

A valid body.
`

// writeTableErrorsLibrary lays down the fixture's local templates/ layer: the
// opted-in `grid` and non-opted-in `flow` slide templates, each with its layout
// and table-free example, plus the `plain` theme the deck selects.
func writeTableErrorsLibrary(t *testing.T, h *Harness) {
	t.Helper()

	h.WriteFile("templates/library.yaml",
		[]byte("name: table-errors-fixture\nformat: 1\n"))
	h.WriteFile("templates/themes/plain/theme.css", []byte("body { margin: 0; }\n"))
	h.WriteFile("templates/slides/grid/template.yaml", []byte(tableErrorsGridManifest))
	h.WriteFile("templates/slides/grid/layout.html.tmpl", []byte(tableErrorsGridLayout))
	h.WriteFile("templates/slides/grid/example.md", []byte(tableErrorsGridExample))
	h.WriteFile("templates/slides/flow/template.yaml", []byte(tableErrorsFlowManifest))
	h.WriteFile("templates/slides/flow/layout.html.tmpl", []byte(tableErrorsFlowLayout))
	h.WriteFile("templates/slides/flow/example.md", []byte(tableErrorsFlowExample))
}

// TestCheckTableErrors is the gfm-tables plan.phase-04 task-14 end-to-end test
// described at the top of this file: `kalide check` (fail-fast, `--all` and
// `--json`) and `kalide start` surface the same table error text in the
// terminal; a live-edited running server surfaces it in the browser error page
// too; and the validation outcome maps to the documented exit codes.
func TestCheckTableErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	// The offline belt: any HTTP(S) client in kalide that honours the proxy
	// environment fails its connection instead of reaching the network. check
	// and the server's local requests never construct one.
	h.EnableOfflineProxy()

	h.WriteFile("kalide.yaml", []byte(tableErrorsDeckConfig))
	writeTableErrorsLibrary(t, h)
	h.WriteFile("slides/1-count.md", []byte(tableErrorsStrictCountSlide))
	h.WriteFile("slides/2-limit.md", []byte(tableErrorsLimitSlide))
	h.WriteFile("slides/3-attr.md", []byte(tableErrorsAttributeSlide))
	h.WriteFile("slides/4-flow.md", []byte(tableErrorsOptOutSlide))

	// The first error of the fixed whole-deck-validation order: slide 1's
	// strict column count, positioned at the offending body row and carrying
	// the parser's fix.
	const (
		wantFirstWhat = "rows[1]: 3 cells, header has 2"
		wantFirstFix  = "make every row have 2 cells to match the header"
	)
	wantFirstPrefix := filepath.FromSlash("slides/1-count.md") + ":8 › body › rows[1]: "

	// --- kalide check (fail-fast): exit 1, the single first table error. ---
	stdout, stderr, code := h.Run("check")
	if code != 1 {
		t.Fatalf("kalide check (table errors) exit = %d, want 1 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "kalide check: 1 error\n" {
		t.Errorf("kalide check (table errors) stdout = %q, want %q", stdout, "kalide check: 1 error\n")
	}
	assertNoServingLine(t, stdout, stderr)
	firstLine := singleLine(t, stderr)
	if !strings.HasPrefix(firstLine, wantFirstPrefix) {
		t.Errorf("kalide check (table errors) stderr = %q, want the first error positioned at %q", firstLine, wantFirstPrefix)
	}
	if !strings.Contains(firstLine, wantFirstWhat) {
		t.Errorf("kalide check (table errors) stderr = %q, want it to carry %q", firstLine, wantFirstWhat)
	}
	if !strings.Contains(firstLine, wantFirstFix) {
		t.Errorf("kalide check (table errors) stderr = %q, want it to carry the fix %q", firstLine, wantFirstFix)
	}

	// --- kalide check --all: exit 1, every table error exactly once, in deck
	// order; the first line is byte-for-byte the fail-fast error. ---
	stdout, stderr, code = h.Run("check", "--all")
	if code != 1 {
		t.Fatalf("kalide check --all (table errors) exit = %d, want 1 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "kalide check: 4 errors\n" {
		t.Errorf("kalide check --all (table errors) stdout = %q, want %q", stdout, "kalide check: 4 errors\n")
	}
	assertNoServingLine(t, stdout, stderr)
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("kalide check --all (table errors) stderr = %q, want exactly 4 errors", stderr)
	}
	if lines[0] != firstLine {
		t.Errorf("kalide check --all first error = %q, want the fail-fast error %q", lines[0], firstLine)
	}
	// The remaining three in fixed deck order: the limit breach, the unknown
	// cell attribute with its closest-match suggestion, and the opt-out refusal.
	remaining := []struct {
		name     string
		prefix   string
		contains []string
	}{
		{
			name:   "limit breach",
			prefix: filepath.FromSlash("slides/2-limit.md") + ":6 › body: ",
			contains: []string{
				"max_columns: table has 4 columns, maximum is 3",
				"use at most 3 columns",
			},
		},
		{
			name:   "unknown cell attribute",
			prefix: filepath.FromSlash("slides/3-attr.md") + ":8 › body › cell: ",
			contains: []string{
				`unknown cell attribute "hilight": did you mean "highlight"?`,
				"remove the {…} group",
			},
		},
		{
			name:   "non-opted-in table",
			prefix: filepath.FromSlash("slides/4-flow.md") + ":7: ",
			contains: []string{
				"tables are not allowed",
				"opt in to tables with the `tables` option in the template's body rule",
			},
		},
	}
	for i, tt := range remaining {
		got := lines[i+1]
		if !strings.HasPrefix(got, tt.prefix) {
			t.Errorf("kalide check --all %s = %q, want it positioned at %q", tt.name, got, tt.prefix)
		}
		for _, want := range tt.contains {
			if !strings.Contains(got, want) {
				t.Errorf("kalide check --all %s = %q, want it to carry %q", tt.name, got, want)
			}
		}
	}

	// --- kalide check --json: the same first error, structured, and its message
	// agrees with the text mode byte-for-byte. ---
	stdout, stderr, code = h.Run("check", "--json")
	if code != 1 {
		t.Fatalf("kalide check --json (table errors) exit = %d, want 1 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide check --json (table errors) stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, stdout, stderr)
	report, _ := decodeCheckReport(t, stdout)
	if report.Summary != "1 error" {
		t.Errorf("kalide check --json (table errors) summary = %q, want %q", report.Summary, "1 error")
	}
	if len(report.Errors) != 1 {
		t.Fatalf("kalide check --json (table errors) errors = %v, want exactly one", report.Errors)
	}
	first := report.Errors[0]
	if first.File != "slides/1-count.md" {
		t.Errorf("kalide check --json (table errors) file = %q, want %q", first.File, "slides/1-count.md")
	}
	if first.Line != 8 {
		t.Errorf("kalide check --json (table errors) line = %d, want 8", first.Line)
	}
	if len(first.Path) != 2 || first.Path[0] != "body" || first.Path[1] != "rows[1]" {
		t.Errorf("kalide check --json (table errors) path = %v, want [body rows[1]]", first.Path)
	}
	wantFirstMessage := wantFirstWhat + " — " + wantFirstFix
	if first.Message != wantFirstMessage {
		t.Errorf("kalide check --json (table errors) message = %q, want %q", first.Message, wantFirstMessage)
	}
	if !strings.HasSuffix(firstLine, ": "+first.Message) {
		t.Errorf("kalide check text error %q and --json message %q disagree", firstLine, first.Message)
	}

	// --- kalide start on the same broken deck: exit 1, the identical first
	// error on the terminal, no server marker and no port bound. ---
	stdout, stderr, code = h.Run("start", "--no-open")
	if code != 1 {
		t.Fatalf("kalide start (table errors) exit = %d, want 1 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("kalide start (table errors) stdout = %q, want empty (no server started)", stdout)
	}
	assertNoServingLine(t, stdout, stderr)
	if startLine := singleLine(t, stderr); startLine != firstLine {
		t.Errorf("kalide start stderr = %q,\nwant the identical error kalide check printed %q", startLine, firstLine)
	}

	// --- A running server live-edited into the table error serves the same
	// text in the browser error page and prints it to the terminal. Make the
	// deck valid first (slide 1 table-free, the other error slides removed), so
	// `kalide start` serves a real deck; then break slide 1 into the strict
	// column count. ---
	for _, gone := range []string{"slides/2-limit.md", "slides/3-attr.md", "slides/4-flow.md"} {
		if err := os.Remove(h.Path(gone)); err != nil {
			t.Fatalf("remove %s: %v", gone, err)
		}
	}
	h.WriteFile("slides/1-count.md", []byte(tableErrorsValidSlide))

	h.Start()
	page, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET / (valid deck): %v", err)
	}
	if strings.Contains(page, "Deck error") {
		t.Fatalf("served page is the error page, want the valid deck:\n%s", page)
	}
	if !strings.Contains(page, `class="grid"`) {
		t.Errorf("valid deck page is missing the grid slide markup:\n%s", page)
	}

	h.WriteFile("slides/1-count.md", []byte(tableErrorsStrictCountSlide))
	waitFor(t, 10*time.Second, "the table error page to be served", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(html.UnescapeString(b), firstLine)
	})

	errBody, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET / (table error page): %v", err)
	}
	if !strings.Contains(errBody, "Deck error") || !strings.Contains(errBody, "error-page__message") {
		t.Errorf("served page is not the full-page error shell after the table error:\n%s", errBody)
	}
	if unescaped := html.UnescapeString(errBody); !strings.Contains(unescaped, firstLine) {
		t.Errorf("browser error page does not carry the same error kalide check printed %q:\n%s", firstLine, unescaped)
	}
	waitFor(t, 5*time.Second, "the table error to reach the terminal", func() bool {
		return strings.Contains(h.Stderr(), firstLine)
	})

	// A graceful stop exits 0, releases the port and leaves no child behind; the
	// harness asserts those post-conditions in Stop.
	h.Stop()
}
