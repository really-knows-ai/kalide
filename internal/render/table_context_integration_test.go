package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// This file is the integration-test deliverable for
// gfm-tables/plan.phase-05.task-7. It drives the reserved table context
// (table-layout-context, gfm-tables) through the whole library→deck→parse→render
// pipeline on a REAL on-disk library and deck, the same shape the other render
// integration tests use (LoadLibrary + Registry.Validate + deck.LoadConfig/
// LoadSlides + slide.Parse + RenderDeck). It reads the real filesystem, so it is
// skipped under -short.
//
// The fixture is a table-OPT-IN library whose templates declare
//
//	body:
//	  mode: optional
//	  tables:
//	    max_tables: 2
//	    cell_attributes: [bold bool, highlight enum green|red]
//
// on the slide (`tabprobe`), the authored child section (`tabcard`) and the
// section-helper target (`tabhelper`), so each context's tables are parsed
// against the template's own declared limits and cell attributes.
//
// It pins:
//
//   - PRESENT and AGREEING at slide level and in EVERY section instance:
//     `.table` and `.tables` are always present (`.table` is the body's first
//     table or the typed nil map that reads falsy, `.tables` the source-ordered
//     list, empty when the body carries none), and `.table` AGREES with
//     `.tables[0]` — the slide's first header `A1`, the authored card's `C1`,
//     the section-helper target's `HH`, each read once through `.table` and
//     once through `.tables[0]`. The authored child section (`tabcard`) and the
//     section-helper target (`tabhelper`) carry their OWN body's tables, parsed
//     against their OWN template's declared cell attributes.
//   - a TWO-TABLE body orders `.tables` by source: the slide body's first table
//     (headers `A1`/`B1`) precedes the second (headers `A2`/`B2`) in `.tables`,
//     and `.table` is the first of them (table-limits, gfm-tables).
//   - `.body` carries ONLY the non-table content (table-layout-context,
//     body-rules): the slide body's surrounding paragraphs render, both tables
//     are omitted; the authored card's body renders its paragraph with its
//     table omitted; and the section-helper target's table-only body renders an
//     EMPTY body.
//   - the cell extensions reach the table context through the real renderer:
//     `{bold}` sets the declared boolean, `{highlight:green}` the declared enum,
//     and `{br}` joins the cell's rendered segments with a literal `<br>`
//     (table-cell-attributes).
//
// A successful template.LoadLibrary is itself part of the assertion: its step-7
// example execution runs each unguarded `.table`/`.tables` read against the
// load-time example context (checkLibraryExample, exampleSectionRunner.render),
// so the fixture's layouts are only loadable if that context is supplied too.
const (
	tableProbeSlideTemplateYAML = `description: slide with a two-table body, an authored card section and a table-bearing section-helper target
fields:
  - name: title
    type: text
    required: true
sections:
  - name: cards
    accepted:
      - tabcard
    max: 4
body:
  mode: optional
  tables:
    max_tables: 2
    cell_attributes:
      - name: bold
        type: boolean
      - name: highlight
        type: enum
        variants:
          - green
          - red
`

	// tableProbeSlideLayout renders the reserved context three ways so the test
	// can compare them: `.table`'s first header (tab-first), `.tables[0]`'s
	// first header (tab-tables-first) and every table in `.tables` in order
	// (tab-t, with its headers, alignments and rows). It also renders `.body`
	// so the table omission is observable, and it calls the section-helper
	// target `tabhelper` with a LITERAL table body, then splices its authored
	// `cards` children.
	tableProbeSlideLayout = `<section class="tab-slide">` +
		`<h1 class="tab-title">{{ .title }}</h1>` +
		`<div class="tab-body">{{ .body }}</div>` +
		`<span class="tab-table">{{ if .table }}table{{ else }}none{{ end }}</span>` +
		`<span class="tab-tables-count">{{ len .tables }}</span>` +
		`<span class="tab-first" data-first="{{ if .table }}{{ (index .table.headers 0).html }}{{ end }}"></span>` +
		`<span class="tab-tables-first" data-first="{{ if .tables }}{{ (index (index .tables 0).headers 0).html }}{{ end }}"></span>` +
		`{{ range $i, $t := .tables }}` +
		`<span class="tab-t" data-index="{{ $i }}" data-first-header="{{ (index $t.headers 0).html }}" data-aligns="{{ range $t.align }}[{{ if . }}{{ . }}{{ else }}none{{ end }}]{{ end }}">` +
		`{{ range $t.headers }}<span class="tab-h" data-bold="{{ if index .attrs "bold" }}true{{ else }}false{{ end }}" data-hl="{{ with index .attrs "highlight" }}{{ . }}{{ else }}none{{ end }}">{{ .html }}</span>{{ end }}` +
		`{{ range $t.rows }}<span class="tab-row">{{ range . }}<span class="tab-cell" data-bold="{{ if index .attrs "bold" }}true{{ else }}false{{ end }}" data-hl="{{ with index .attrs "highlight" }}{{ . }}{{ else }}none{{ end }}">{{ .html }}</span>{{ end }}</span>{{ end }}` +
		`</span>` +
		`{{ end }}` +
		`{{ section "tabhelper" nil "| HH | HV |\n| --- | --- |\n| hh | hv |" }}` +
		`{{ range .cards }}{{ . }}{{ end }}` +
		`</section>`

	tableProbeSlideExample = "---\n" +
		"template: tabprobe\n" +
		"title: Example\n" +
		"---\n" +
		"Example before.\n" +
		"\n" +
		"| E1 | E2 |\n" +
		"| --- | --- |\n" +
		"| e1 | e2 |\n" +
		"\n" +
		"Example after.\n"

	tableProbeCardTemplateYAML = `description: authored card section carrying its own GFM table
fields:
  - name: name
    type: text
    required: true
body:
  mode: optional
  tables:
    max_tables: 2
    cell_attributes:
      - name: bold
        type: boolean
      - name: highlight
        type: enum
        variants:
          - green
          - red
`

	// tableProbeCardLayout renders the authored card's own `.table`/`.tables`
	// agreement (tab-card-first vs tab-card-tables-first) and its `.body`, so a
	// body with a table and a body without one are both observable.
	tableProbeCardLayout = `<div class="tab-card">` +
		`<span class="tab-card-name">{{ .name }}</span>` +
		`<span class="tab-card-table">{{ if .table }}table{{ else }}none{{ end }}</span>` +
		`<span class="tab-card-tables-count">{{ len .tables }}</span>` +
		`<span class="tab-card-first" data-first="{{ if .table }}{{ (index .table.headers 0).html }}{{ end }}"></span>` +
		`<span class="tab-card-tables-first" data-first="{{ if .tables }}{{ (index (index .tables 0).headers 0).html }}{{ end }}"></span>` +
		`<span class="tab-card-body">{{ .body }}</span>` +
		`</div>`

	tableProbeCardExample = "```\n" +
		"name: Example card\n" +
		"```\n" +
		"Example card body.\n"

	tableProbeHelperTemplateYAML = `description: section-helper target carrying a table supplied by the call body
body:
  mode: optional
  tables:
    max_tables: 2
    cell_attributes:
      - name: bold
        type: boolean
      - name: highlight
        type: enum
        variants:
          - green
          - red
`

	// tableProbeHelperLayout renders the helper target's own `.table`/`.tables`
	// agreement (tab-helper-first vs tab-helper-tables-first) and its `.body`,
	// which is EMPTY because the call body is a table only.
	tableProbeHelperLayout = `<div class="tab-helper">` +
		`<span class="tab-helper-table">{{ if .table }}table{{ else }}none{{ end }}</span>` +
		`<span class="tab-helper-tables-count">{{ len .tables }}</span>` +
		`<span class="tab-helper-first" data-first="{{ if .table }}{{ (index .table.headers 0).html }}{{ end }}"></span>` +
		`<span class="tab-helper-tables-first" data-first="{{ if .tables }}{{ (index (index .tables 0).headers 0).html }}{{ end }}"></span>` +
		`<span class="tab-helper-body">{{ .body }}</span>` +
		`</div>`

	tableProbeHelperExample = "```\n" +
		"```\n" +
		"Helper example body.\n"
)

// tableProbeSlide is the deck slide: a two-table body (headers A1/B1 then
// A2/B2), two authored `cards` instances — the first with its own table
// (headers C1/C2), the second with none — and the layout's section-helper call
// to `tabhelper`. The tables deliberately carry a `{bold}` attribute, a
// `{highlight:green}` enum and a `{br}` break, so the cell extensions are
// observable through the rendered page, and each table's delimiter row selects a
// distinct alignment.
const tableProbeSlide = "---\n" +
	"template: tabprobe\n" +
	"title: Table Context\n" +
	"---\n" +
	"Before table one.\n" +
	"\n" +
	"| A1 | B1{bold} |\n" +
	"| :--- | ---: |\n" +
	"| a1 | b1{highlight:green} |\n" +
	"\n" +
	"Between the tables.\n" +
	"\n" +
	"| A2 | B2 |\n" +
	"| :---: | --- |\n" +
	"| a2{br}next | b2 |\n" +
	"\n" +
	"After table two.\n" +
	"\n" +
	"# cards\n" +
	"```\n" +
	"template: tabcard\n" +
	"name: Card one\n" +
	"```\n" +
	"Card one body.\n" +
	"\n" +
	"| C1 | C2 |\n" +
	"| --- | --- |\n" +
	"| c1 | c2 |\n" +
	"\n" +
	"# cards\n" +
	"```\n" +
	"template: tabcard\n" +
	"name: Card two\n" +
	"```\n" +
	"Card two body with no table.\n"

// TestRenderTableContextPipeline is the integration test: a real on-disk deck
// (with its own copy of the fixture templates/ library plus the table-probe
// templates) is loaded by the real loader, validated, parsed and rendered by
// RenderDeck, and the rendered page is asserted for the reserved table context
// at the slide level and in every section instance.
func TestRenderTableContextPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writeTableProbeDeck(t)
	fsys := os.DirFS(dir)

	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v (the unguarded .table/.tables example layouts must load)", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("template.Registry.Validate: %v", err)
	}
	themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}
	funcMap := template.LayoutFuncMap(lib.Media, "/assets/templates/media")

	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themeReg)
	if err != nil {
		t.Fatalf("deck.LoadConfig: %v", err)
	}
	d, err := deck.LoadSlides(fsys, deck.SlidesDir)
	if err != nil {
		t.Fatalf("deck.LoadSlides: %v", err)
	}
	parsed := parsePipelineSlides(t, fsys, d, reg)

	pageHTML, err := RenderDeck(cfg, d, parsed, reg, themeReg, funcMap)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	page := string(pageHTML)

	// -----------------------------------------------------------------------
	// Slide level: `.table`/`.tables` present, agreeing, and source-ordered.
	// -----------------------------------------------------------------------

	// The reserved entries are present: two tables, and `.table` is the FIRST.
	for _, want := range []string{
		`<span class="tab-table">table</span>`,
		`<span class="tab-tables-count">2</span>`,
		// `.table` and `.tables[0]` AGREE on the body's first table: both read
		// the first header `A1`.
		`<span class="tab-first" data-first="A1"></span>`,
		`<span class="tab-tables-first" data-first="A1"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("slide-level table context does not contain %q:\n%s", want, page)
		}
	}

	// `.tables` is ordered by source: the first body table (header `A1`) comes
	// before the second (header `A2`).
	firstAt := strings.Index(page, `<span class="tab-t" data-index="0" data-first-header="A1"`)
	secondAt := strings.Index(page, `<span class="tab-t" data-index="1" data-first-header="A2"`)
	switch {
	case firstAt < 0 || secondAt < 0:
		t.Errorf("slide .tables does not carry both source-ordered tables (index 0 first header A1, index 1 first header A2):\n%s", page)
	case firstAt > secondAt:
		t.Errorf("slide .tables is not source-ordered: the A1 table (index %d) appears after the A2 table (index %d):\n%s", firstAt, secondAt, page)
	}

	// The first table's headers, alignments and rows, including the decoded cell
	// attributes and the `{br}` break.
	for _, want := range []string{
		`data-index="0" data-first-header="A1" data-aligns="[left][right]"`,
		`<span class="tab-h" data-bold="false" data-hl="none">A1</span>`,
		`<span class="tab-h" data-bold="true" data-hl="none">B1</span>`,
		`<span class="tab-cell" data-bold="false" data-hl="none">a1</span>`,
		`<span class="tab-cell" data-bold="false" data-hl="green">b1</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("slide first table does not contain %q:\n%s", want, page)
		}
	}

	// The second table's headers, alignments and its `{br}` cell.
	for _, want := range []string{
		`data-index="1" data-first-header="A2" data-aligns="[center][none]"`,
		`<span class="tab-h" data-bold="false" data-hl="none">A2</span>`,
		`<span class="tab-h" data-bold="false" data-hl="none">B2</span>`,
		`<span class="tab-cell" data-bold="false" data-hl="none">a2<br>next</span>`,
		`<span class="tab-cell" data-bold="false" data-hl="none">b2</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("slide second table does not contain %q:\n%s", want, page)
		}
	}

	// `.body` carries ONLY the non-table content: the three surrounding
	// paragraphs render, and neither table's cells leak into it.
	slideBody := renderElementInner(t, page, `<div class="tab-body">`)
	for _, want := range []string{
		"<p>Before table one.</p>",
		"<p>Between the tables.</p>",
		"<p>After table two.</p>",
	} {
		if !strings.Contains(slideBody, want) {
			t.Errorf("slide .body does not carry the non-table paragraph %q:\n%s", want, slideBody)
		}
	}
	for _, never := range []string{"a1", "b1", "a2", "b2", "A1", "B1", "A2", "B2", "|"} {
		if strings.Contains(slideBody, never) {
			t.Errorf("slide .body carries the table content %q, want only the non-table content:\n%s", never, slideBody)
		}
	}

	// -----------------------------------------------------------------------
	// The AUTHORED child section instance: its own body's table context.
	// -----------------------------------------------------------------------

	if got := strings.Count(page, `<div class="tab-card">`); got != 2 {
		t.Errorf("rendered authored tab-card count = %d, want 2:\n%s", got, page)
	}

	// Card one carries a table: its reserved context is present and `.table`
	// agrees with `.tables[0]` (both `C1`).
	for _, want := range []string{
		`<span class="tab-card-name">Card one</span>`,
		`<span class="tab-card-table">table</span>`,
		`<span class="tab-card-tables-count">1</span>`,
		`<span class="tab-card-first" data-first="C1"></span>`,
		`<span class="tab-card-tables-first" data-first="C1"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("authored card table context does not contain %q:\n%s", want, page)
		}
	}

	// Card one's `.body` is its paragraph only; the C1/C2 table is omitted. The
	// exact span pins the whole body, so a leaked table would break it.
	if want := `<span class="tab-card-body"><p>Card one body.</p>` + "\n" + `</span>`; !strings.Contains(page, want) {
		t.Errorf("authored card .body does not omit its table:\nwant: %q\npage:\n%s", want, page)
	}

	// Card two carries NO table: the reserved entries are still PRESENT, as the
	// typed nil `.table` (read `none`) and the empty `.tables` list, and its
	// body is its paragraph.
	for _, want := range []string{
		`<span class="tab-card-name">Card two</span>`,
		`<span class="tab-card-table">none</span>`,
		`<span class="tab-card-tables-count">0</span>`,
		`<span class="tab-card-first" data-first=""></span>`,
		`<span class="tab-card-tables-first" data-first=""></span>`,
		`<span class="tab-card-body"><p>Card two body with no table.</p>` + "\n" + `</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("no-table authored card context does not contain %q:\n%s", want, page)
		}
	}

	// -----------------------------------------------------------------------
	// The SECTION-HELPER target: its own call body's table context.
	// -----------------------------------------------------------------------

	if got := strings.Count(page, `<div class="tab-helper">`); got != 1 {
		t.Errorf("rendered section-helper target count = %d, want 1:\n%s", got, page)
	}
	for _, want := range []string{
		`<span class="tab-helper-table">table</span>`,
		`<span class="tab-helper-tables-count">1</span>`,
		// `.table` and `.tables[0]` agree on the call body's first table (`HH`).
		`<span class="tab-helper-first" data-first="HH"></span>`,
		`<span class="tab-helper-tables-first" data-first="HH"></span>`,
		// The call body is a table only, so the target's `.body` carries ONLY
		// the non-table content: nothing.
		`<span class="tab-helper-body"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("section-helper target table context does not contain %q:\n%s", want, page)
		}
	}

	// The page carries rendered values, never an execution data map.
	if strings.Contains(page, "map[") {
		t.Errorf("page carries a section data map rather than rendered values:\n%s", page)
	}
}

// renderElementInner returns the content between an element's opening tag and
// its matching closing tag, failing when the opening tag is absent or
// unterminated. It is used to scope a `.body` div assertion to its own element
// (the body contains bare <p> children, so the closing tag searched for is the
// div's own) so table content elsewhere on the page cannot satisfy it.
func renderElementInner(t *testing.T, page, open string) string {
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

// writeTableProbeDeck writes a fresh temporary deck directory: kalide.yaml, the
// table-probe slide under slides/, a copy of the fixture templates/ tree, and
// the table-probe slide/section templates (tabprobe, tabcard, tabhelper).
func writeTableProbeDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for name, data := range map[string]string{
		"kalide.yaml":        "title: Table Context Deck\ntheme: plain\n",
		"slides/1-tables.md": tableProbeSlide,
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	copyTemplatesDir(t, filepath.Join(fixtureLibraryDir, "templates"), filepath.Join(dir, "templates"))

	writeProbeTemplate(t, dir, "slides", "tabprobe", tableProbeSlideTemplateYAML, tableProbeSlideLayout, tableProbeSlideExample)
	writeProbeTemplate(t, dir, "sections", "tabcard", tableProbeCardTemplateYAML, tableProbeCardLayout, tableProbeCardExample)
	writeProbeTemplate(t, dir, "sections", "tabhelper", tableProbeHelperTemplateYAML, tableProbeHelperLayout, tableProbeHelperExample)

	return dir
}
