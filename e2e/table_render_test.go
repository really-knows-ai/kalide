package e2e

// This file is the gfm-tables plan.phase-05 task-8 end-to-end test for the
// reserved table render context (requirements.requirement.table-layout-context,
// requirements.requirement.gfm-tables). It drives the real kalide binary the
// harness builds (harness.go) through Harness.Run and Harness.Start, so a
// table-opt-in template's body table is observed the way an author sees it:
//
//   - `kalide render <slide> --fragment` prints the slide's `<section>` markup
//     only, and `kalide render <slide> --full` the complete standalone page that
//     embeds that fragment byte for byte; and
//   - `kalide start` serves the same slide markup from `/`, so the browser and
//     the one-shot render agree on the table context.
//
// The fixture is a single table-OPT-IN slide template (`tabrender`) whose
// manifest declares `body.tables` (max_columns 4, plus the boolean `bold` and
// enum `highlight` cell attributes). Its layout renders the body's table from
// the reserved `.table.rows` itself — with an UNGUARDED `{{ range .table.rows }}`
// so a body with no table is a no-op — emitting each cell's rendered `.html`
// and a cell attribute the layout reads through `index .attrs`. Its example.md
// is table-free, so the library loads cleanly and each read against the
// load-time example context stays a no-op.
//
// The deck slide's body carries a GFM table whose cells exercise the phase-3
// cell extensions through the real renderer: `{br}` becomes a literal `<br>` in
// the cell's html, and `{bold}`/`{highlight:green}` set the declared attributes
// the layout reflects into its markup. The body's surrounding paragraphs must
// reach `.body` while the table itself is OMITTED from `.body` — the table
// appears only through `.table`, never through the rendered body.
//
// The tests build a binary and drive a real server, so they are skipped under
// -short.

import (
	"strings"
	"testing"
)

// tableRenderDeckConfig is the fixture deck's kalide.yaml: a single local
// templates/ layer and the `plain` theme it carries.
const tableRenderDeckConfig = `title: Table Render Deck
author: Ada Lovelace
theme: plain
`

// tableRenderManifest is the table-opt-in slide template: its body rule carries
// the `tables` mapping (a maximum of four columns) and the two declared cell
// attributes (`bold` boolean, `highlight` enum with two variants) the layout
// reflects and the slide's cells set.
const tableRenderManifest = `description: a slide whose body opts in to tables
fields:
  - name: title
    type: text
    required: true
body:
  mode: optional
  tables:
    max_columns: 4
    cell_attributes:
      - name: bold
        type: boolean
      - name: highlight
        type: enum
        variants: [red, green]
`

// tableRenderLayout renders, in a single `<section>` a deck can anchor:
//
//   - the slide's `title`; and
//   - the body's table from the reserved `.table.rows` context — the layout
//     renders the table itself, with an UNGUARDED `{{ range .table.rows }}` (a
//     body with no table is a no-op on the typed-nil `.table`) — emitting each
//     cell's rendered `.html` and, as the cell attribute the layout uses, the
//     declared `bold` boolean and `highlight` enum read through `index .attrs`;
//   - the body in its own `.tabrender-body` element so the table's omission from
//     `.body` is observable.
//
// The markup is deliberate and whitespace-free so the test can pin exact cell
// strings.
const tableRenderLayout = `<section class="tabrender">` +
	`<h1 class="tabrender-title">{{ .title }}</h1>` +
	`<div class="tabrender-body">{{ .body }}</div>` +
	`<table class="tabrender-table">` +
	`{{ range .table.rows }}<tr>{{ range . }}` +
	`<td class="tabrender-cell" data-bold="{{ if index .attrs "bold" }}true{{ else }}false{{ end }}" data-hl="{{ with index .attrs "highlight" }}{{ . }}{{ else }}none{{ end }}">{{ .html }}</td>` +
	`{{ end }}</tr>{{ end }}` +
	`</table>` +
	`</section>` + "\n"

// tableRenderExample is the template's table-free example: the loader executes
// the layout's unguarded `{{ range .table.rows }}` against the example context,
// where `.table` is the typed nil map and the range is a no-op.
const tableRenderExample = `---
template: tabrender
title: Example
---

Example body.
`

// tableRenderSlide is the deck's slide: a table-opt-in `tabrender` body whose
// table cells exercise `{br}` (a literal `<br>` in the cell html), `{bold}` (the
// declared boolean) and `{highlight:green}` (the declared enum), with plain
// paragraphs before and after so `.body`'s table omission is observable.
const tableRenderSlide = `---
template: tabrender
title: Rendered Table
---

Body before.

| H1 | H2 |
| --- | --- |
| alpha | beta{br}gamma{bold} |
| delta{highlight:green} | epsilon |

Body after.
`

// The exact `<td>` markup the layout emits for each body cell, pinning the
// rendered `{br}` (`beta<br>gamma`), the `{bold}` attribute (`data-bold="true"`)
// and the `{highlight:green}` enum (`data-hl="green"`). The header row has no
// `<td>` of its own because the layout renders `.table.rows` only.
const (
	tableRenderCellAlpha   = `<td class="tabrender-cell" data-bold="false" data-hl="none">alpha</td>`
	tableRenderCellBeta    = `<td class="tabrender-cell" data-bold="true" data-hl="none">beta<br>gamma</td>`
	tableRenderCellDelta   = `<td class="tabrender-cell" data-bold="false" data-hl="green">delta</td>`
	tableRenderCellEpsilon = `<td class="tabrender-cell" data-bold="false" data-hl="none">epsilon</td>`
)

// writeTableRenderLibrary lays down the fixture's local templates/ layer: the
// `tabrender` slide template, its layout and its table-free example, plus the
// `plain` theme the deck selects.
func writeTableRenderLibrary(t *testing.T, h *Harness) {
	t.Helper()

	h.WriteFile("templates/library.yaml",
		[]byte("name: table-render-fixture\nformat: 1\n"))
	h.WriteFile("templates/themes/plain/theme.css", []byte("body { margin: 0; }\n"))
	h.WriteFile("templates/slides/tabrender/template.yaml", []byte(tableRenderManifest))
	h.WriteFile("templates/slides/tabrender/layout.html.tmpl", []byte(tableRenderLayout))
	h.WriteFile("templates/slides/tabrender/example.md", []byte(tableRenderExample))
}

// TestTableRenderEndToEnd is the gfm-tables plan.phase-05 task-8 end-to-end
// test described at the top of this file: a table-opt-in template's deck
// renders its table through `kalide render --fragment` and `--full` and through
// the live server, the served slide markup carrying the table's cells (the
// `{br}` break and the layout-read cell attributes) and the `.body` excluding
// the table.
func TestTableRenderEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	h.EnableOfflineProxy()

	h.WriteFile("kalide.yaml", []byte(tableRenderDeckConfig))
	writeTableRenderLibrary(t, h)
	h.WriteFile("slides/1-tabrender.md", []byte(tableRenderSlide))

	// Every table cell the layout must have rendered from `.table.rows`.
	cellMarkup := []string{
		tableRenderCellAlpha,
		tableRenderCellBeta,
		tableRenderCellDelta,
		tableRenderCellEpsilon,
	}

	// --- kalide render <slide> --fragment: only the slide's <section> markup,
	// carrying the table rendered from .table.rows. ---
	fragment, stderr, code := h.Run("render", "1", "--fragment")
	if code != 0 {
		t.Fatalf("kalide render 1 --fragment exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide render 1 --fragment stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, fragment, stderr)
	if !strings.HasPrefix(fragment, `<section id="tabrender"`) {
		t.Errorf("kalide render 1 --fragment does not begin with the slide's anchored <section>:\n%s", fragment)
	}
	if !strings.Contains(fragment, `<h1 class="tabrender-title">Rendered Table</h1>`) {
		t.Errorf("kalide render 1 --fragment is missing the slide's rendered title:\n%s", fragment)
	}
	for _, want := range append([]string{
		`<table class="tabrender-table">`,
	}, cellMarkup...) {
		if !strings.Contains(fragment, want) {
			t.Errorf("rendered fragment does not contain the table markup %q:\n%s", want, fragment)
		}
	}

	// `.body` excludes the table: the surrounding paragraphs render in the
	// body element, and no cell or table content leaks into it.
	fragmentBody := tableRenderElementInner(t, fragment, `<div class="tabrender-body">`)
	for _, want := range []string{"<p>Body before.</p>", "<p>Body after.</p>"} {
		if !strings.Contains(fragmentBody, want) {
			t.Errorf(".body does not carry the non-table paragraph %q:\n%s", want, fragmentBody)
		}
	}
	for _, never := range []string{"alpha", "beta", "gamma", "delta", "epsilon", "<td", "<table", "|"} {
		if strings.Contains(fragmentBody, never) {
			t.Errorf(".body carries table content %q, want only the non-table content:\n%s", never, fragmentBody)
		}
	}

	// --- kalide render <slide> --full: the complete page embeds the fragment
	// byte for byte, so the table context is identical in both modes. ---
	full, stderr, code := h.Run("render", "1", "--full")
	if code != 0 {
		t.Fatalf("kalide render 1 --full exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide render 1 --full stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, full, stderr)
	for _, want := range []string{"<!DOCTYPE html>", "Reveal.initialize"} {
		if !strings.Contains(full, want) {
			t.Errorf("kalide render 1 --full page is missing the page shell %q:\n%s", want, full)
		}
	}
	if !strings.Contains(full, fragment) {
		t.Errorf("kalide render 1 --full page does not embed its --fragment output:\n  full:     %s\n  fragment: %s", full, fragment)
	}

	// --- kalide start: the served slide markup carries the same table context,
	// and its .body excludes the table too. ---
	h.Start()

	page, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if strings.Contains(page, "Deck error") {
		t.Fatalf("served page is the error page, want the valid deck:\n%s", page)
	}
	if !strings.Contains(page, `<section id="tabrender"`) {
		t.Errorf("served page is missing the table-opt-in slide's anchored <section>:\n%s", page)
	}
	for _, want := range append([]string{
		`<table class="tabrender-table">`,
		`<h1 class="tabrender-title">Rendered Table</h1>`,
	}, cellMarkup...) {
		if !strings.Contains(page, want) {
			t.Errorf("served slide markup does not contain %q:\n%s", want, page)
		}
	}

	// The served `.body` excludes the table exactly as the rendered fragment's
	// did: the same paragraphs, none of the table content.
	servedBody := tableRenderElementInner(t, page, `<div class="tabrender-body">`)
	for _, want := range []string{"<p>Body before.</p>", "<p>Body after.</p>"} {
		if !strings.Contains(servedBody, want) {
			t.Errorf("served .body does not carry the non-table paragraph %q:\n%s", want, servedBody)
		}
	}
	for _, never := range []string{"alpha", "beta", "gamma", "delta", "epsilon", "<td", "<table", "|"} {
		if strings.Contains(servedBody, never) {
			t.Errorf("served .body carries table content %q, want only the non-table content:\n%s", never, servedBody)
		}
	}

	// A graceful stop exits 0, releases the port and leaves no child behind; the
	// harness asserts those post-conditions in Stop.
	h.Stop()
}

// tableRenderElementInner returns the content between an element's opening tag
// and its matching closing tag, failing when the opening tag is absent or
// unterminated. The body element's own children are bare <p> elements, so the
// closing tag searched for is the div's own; this scopes a `.body` assertion to
// its own element so table markup elsewhere on the page cannot satisfy it.
func tableRenderElementInner(t *testing.T, page, open string) string {
	t.Helper()

	i := strings.Index(page, open)
	if i < 0 {
		t.Fatalf("page does not contain %q:\n%s", open, page)
	}
	rest := page[i+len(open):]
	j := strings.Index(rest, "</div>")
	if j < 0 {
		t.Fatalf("page's %q is not closed:\n%s", open, page)
	}
	return rest[:j]
}
