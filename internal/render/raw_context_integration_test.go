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

// The raw-context-probe library drives the reserved `.raw` and `.data` context
// through renderSection at every depth, on a real on-disk library loaded by the
// real registry (template.LoadLibrary + template.NewRegistryFromLibrary +
// Registry.Validate) and rendered by RenderDeck:
//
//   - `rawprobe` (slide) reads its own `.raw.title`/`.raw.body` (source) next to
//     the converted `.title`/`.body`, its authored `.data.blocks` entries and
//     the rendered `.blocks` list; its layout also makes a `{{ section … }}`
//     call to `rawprobeblock`.
//   - `rawprobeblock` (section) reads its own `.raw.title`/`.raw.body` next to
//     the converted values, and holds the nested `leaves` child section.
//   - `rawprobele` (section) reads its own `.raw.name` and its `.item`, so the
//     descriptor's position among same-name siblings is visible at two depths.
//
// The layouts read `.raw`, `.data` and (for the section templates) `.item`
// directly: checkLibraryExample now supplies those reserved contexts to the
// load-time example execution, exactly as the renderer does, so no guard is
// needed to keep a layout executable in isolation. Guards remain only where
// they select on OPTIONAL CONTENT — an empty body or an absent child/rendered
// section — so the assertions below can tell "absent" from a source value.
const (
	rawProbeSlideTemplateYAML = `description: slide probing the reserved raw/data context through its layout
fields:
  - name: title
    type: text
    required: true
sections:
  - name: blocks
    accepted:
      - rawprobeblock
    max: 4
body:
  mode: optional
`

	rawProbeSlideLayout = `<section class="raw-probe">` +
		`<h1 class="slide-title">{{.title}}</h1>` +
		`<span class="slide-raw-title">{{.raw.title}}</span>` +
		`<div class="slide-body">{{.body}}</div>` +
		`<span class="slide-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="slide-blocks-count">{{if .blocks}}{{len .blocks}}{{else}}0{{end}}</span>` +
		`<span class="slide-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`<span class="slide-data-block-count">{{if .data.blocks}}{{len .data.blocks}}{{else}}0{{end}}</span>` +
		`{{range .blocks}}{{.}}{{end}}` +
		`{{range .data.blocks}}` +
		`<span class="data-block" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}">` +
		`<span class="data-block-raw-title">{{.raw.title}}</span>` +
		`<span class="data-block-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="data-block-leaf-count">{{if .data.leaves}}{{len .data.leaves}}{{else}}0{{end}}</span>` +
		`{{range .data.leaves}}` +
		`<span class="data-leaf" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}">` +
		`<span class="data-leaf-raw-name">{{.raw.name}}</span>` +
		`<span class="data-leaf-parent-raw-title">{{.item.parent.title}}</span>` +
		`</span>` +
		`{{end}}` +
		`</span>` +
		`{{end}}` +
		`{{section "rawprobeblock" (dict "title" "Helper block")}}` +
		`</section>`

	rawProbeSlideExample = "---\ntemplate: rawprobe\ntitle: Example title\n---\n" +
		"Example body source.\n" +
		"# blocks\n" +
		"```\ntemplate: rawprobeblock\ntitle: Example block\n```\n" +
		"Example block body source.\n" +
		"## leaves\n" +
		"```\ntemplate: rawprobele\nname: Ada\n```\n"

	rawProbeBlockTemplateYAML = `description: nested section probing its own reserved raw context
fields:
  - name: title
    type: text
    required: true
sections:
  - name: leaves
    accepted:
      - rawprobele
    max: 4
body:
  mode: optional
`

	rawProbeBlockLayout = `<div class="raw-block" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}">` +
		`<span class="block-title">{{.title}}</span>` +
		`<span class="block-raw-title">{{.raw.title}}</span>` +
		`<div class="block-body">{{if .body}}{{.body}}{{else}}absent{{end}}</div>` +
		`<span class="block-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="block-leaf-count">{{if .leaves}}{{len .leaves}}{{else}}0{{end}}</span>` +
		`{{if .leaves}}{{range .leaves}}{{.}}{{end}}{{end}}` +
		`</div>`

	rawProbeBlockExample = "```\ntitle: Example block\n```\n" +
		"Example block body source.\n" +
		"# leaves\n" +
		"```\ntemplate: rawprobele\nname: Ada\n```\n"

	rawProbeLeafTemplateYAML = `description: deepest section probing its own reserved raw context and item
fields:
  - name: name
    type: text
    required: true
body:
  mode: disallowed
`

	rawProbeLeafLayout = `<span class="raw-leaf" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}">` +
		`<span class="leaf-name">{{.name}}</span>` +
		`<span class="leaf-raw-name">{{.raw.name}}</span>` +
		`<span class="leaf-parent-title">{{.item.parent.title}}</span>` +
		`</span>`

	rawProbeLeafExample = "```\nname: Ada\n```\n"
)

// rawProbeSlide is the deck slide the test writes: one slide using the
// raw-context-probe layout, with two authored `blocks` instances — the first
// carrying two nested `leaves` — so the reserved `.raw`/`.data`/`.item` context
// is exercised at two depths and with same-name siblings. The field values
// carry Markdown so the converted value and its `.raw` source are visibly
// different.
func rawProbeSlide() string {
	return "---\ntemplate: rawprobe\ntitle: '**Slide** title'\n---\n" +
		"Slide *body* source.\n" +
		"# blocks\n" +
		"```\ntemplate: rawprobeblock\ntitle: '**First** block'\n```\n" +
		"First block body source.\n" +
		"## leaves\n" +
		"```\ntemplate: rawprobele\nname: '**Ada**'\n```\n" +
		"## leaves\n" +
		"```\ntemplate: rawprobele\nname: Grace\n```\n" +
		"# blocks\n" +
		"```\ntemplate: rawprobeblock\ntitle: Second block\n```\n"
}

// TestRenderSectionRawDataContextPipeline drives the reserved `.raw`/`.data`
// context through renderSection at every depth on a real on-disk library: a
// real deck directory whose raw-context-probe library is loaded by
// template.LoadLibrary, built into a registry by
// template.NewRegistryFromLibrary and validated by Registry.Validate — the same
// load path kalide start uses — then parsed by internal/slide.Parse and
// rendered by RenderDeck. It reads the real filesystem, so it is skipped under
// -short.
//
// It pins (raw-source-context, section-data-context, section-template-context,
// item-context, template-context): the slide layout and a nested section
// template read `.raw.title`/`.raw.body` (the author's source) alongside the
// converted title/body; `.data.blocks` has one entry per authored instance,
// each with its own `.raw`/`.item`/`.data`; a nested instance's children are
// reached through its entry's `.data`, whose `.item.parent` is the parent's
// source values (note-27); `.item` reflects position among same-name siblings
// at every depth; and a `{{ section … }}` call adds no entry to the caller's
// `.data` and no element to its rendered section list, while its target still
// sees its own `.raw`.
func TestRenderSectionRawDataContextPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writeRawProbeDeck(t)
	fsys := os.DirFS(dir)

	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
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

	// The slide layout: the converted `.title`/`.body` next to the reserved
	// `.raw` source, and the authored `.data` shape (two `blocks` entries, and
	// no key for the helper's target).
	for _, want := range []string{
		`<section id="raw" class="raw-probe">`,
		`<h1 class="slide-title"><strong>Slide</strong> title</h1>`,
		`<span class="slide-raw-title">**Slide** title</span>`,
		`<div class="slide-body"><p>Slide <em>body</em> source.</p>`,
		`<span class="slide-raw-body">Slide *body* source.`,
		// `.data` holds one entry per authored `blocks` instance, and the
		// helper's target contributes no key.
		`<span class="slide-data-keys">blocks;</span>`,
		`<span class="slide-data-block-count">2</span>`,
		`<span class="slide-blocks-count">2</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}

	// The nested section template (`rawprobeblock`) reads the same reserved
	// `.raw` source next to its converted fields, at composition depth.
	for _, want := range []string{
		`<span class="block-title"><strong>First</strong> block</span>`,
		`<span class="block-raw-title">**First** block</span>`,
		`<div class="block-body"><p>First block body source.</p>`,
		`<span class="block-raw-body">First block body source.`,
		`<span class="block-leaf-count">2</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}

	// `.item` reflects position among same-name siblings, in the rendered tree:
	// the two `blocks` instances are 0/2 and 1/2, the two `leaves` under the
	// first block are 0/2 and 1/2. Each rendered leaf's `.item.parent.title` is
	// the enclosing block instance's CONVERTED value.
	for _, want := range []string{
		`<div class="raw-block" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="blocks" data-template="rawprobeblock" data-parent="no">`,
		`<div class="raw-block" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="blocks" data-template="rawprobeblock" data-parent="no">`,
		`<span class="raw-leaf" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="leaves" data-template="rawprobele">`,
		`<span class="raw-leaf" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="leaves" data-template="rawprobele">`,
		`<span class="leaf-name"><strong>Ada</strong></span>`,
		`<span class="leaf-raw-name">**Ada**</span>`,
		`<span class="leaf-parent-title"><strong>First</strong> block</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}

	// The `.data` view of the same tree: one entry per authored instance, each
	// carrying its own `.raw`, `.item` and `.data`; the first block's entry
	// reaches its two nested children through the entry's `.data`, and each
	// nested entry's `.item.parent` is the parent's SOURCE values (note-27).
	for _, want := range []string{
		`<span class="data-block" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="blocks" data-template="rawprobeblock" data-parent="no">`,
		`<span class="data-block" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="blocks" data-template="rawprobeblock" data-parent="no">`,
		`<span class="data-block-raw-title">**First** block</span>`,
		`<span class="data-block-raw-body">First block body source.`,
		`<span class="data-block-raw-title">Second block</span>`,
		`<span class="data-block-raw-body">absent</span>`,
		`<span class="data-block-leaf-count">2</span>`,
		`<span class="data-block-leaf-count">0</span>`,
		`<span class="data-leaf" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="leaves" data-template="rawprobele" data-parent="yes">`,
		`<span class="data-leaf" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="leaves" data-template="rawprobele" data-parent="yes">`,
		`<span class="data-leaf-raw-name">**Ada**</span>`,
		`<span class="data-leaf-raw-name">Grace</span>`,
		`<span class="data-leaf-parent-raw-title">**First** block</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}

	// The section-helper call renders its target (`rawprobeblock`) as a
	// one-item group; the target still sees its own `.raw` source value. Its
	// `.item.section`/`.item.template` are the target name, so it is
	// distinguishable from the two authored `blocks` instances above.
	wantHelper := `<div class="raw-block" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="rawprobeblock" data-template="rawprobeblock" data-parent="no"><span class="block-title">Helper block</span><span class="block-raw-title">Helper block</span>`
	if !strings.Contains(page, wantHelper) {
		t.Errorf("page does not contain the helper-rendered target with its own .raw:\nwant: %s\npage:\n%s", wantHelper, page)
	}

	// The helper call adds no key to the caller's `.data`: the only key is the
	// authored `blocks`, never the helper's target name.
	if strings.Contains(page, "rawprobeblock;") {
		t.Errorf("helper target leaked into the caller's .data:\n%s", page)
	}

	// The helper call adds no element to the caller's rendered section list:
	// the two authored `blocks` render exactly twice, while the helper's target
	// adds a third `raw-block` and exactly two `.data` entries are published.
	if got := strings.Count(page, `<div class="raw-block"`); got != 3 {
		t.Errorf("rendered raw-block count = %d, want 3 (two authored + one helper):\n%s", got, page)
	}
	if got := strings.Count(page, `<span class="data-block"`); got != 2 {
		t.Errorf("rendered data-block entry count = %d, want 2 (authored instances only):\n%s", got, page)
	}

	// The page carries rendered values, never the execution data maps.
	if strings.Contains(page, "map[") {
		t.Errorf("page carries a section data map rather than rendered values:\n%s", page)
	}
}

// writeRawProbeDeck writes a fresh temporary deck directory: kalide.yaml, one
// slide under slides/, a copy of the fixture templates/ tree, and the
// raw-context-probe slide/section templates.
func writeRawProbeDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for name, data := range map[string]string{
		"kalide.yaml":     "title: Raw Context Deck\ntheme: plain\n",
		"slides/1-raw.md": rawProbeSlide(),
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

	writeProbeTemplate(t, dir, "slides", "rawprobe", rawProbeSlideTemplateYAML, rawProbeSlideLayout, rawProbeSlideExample)
	writeProbeTemplate(t, dir, "sections", "rawprobeblock", rawProbeBlockTemplateYAML, rawProbeBlockLayout, rawProbeBlockExample)
	writeProbeTemplate(t, dir, "sections", "rawprobele", rawProbeLeafTemplateYAML, rawProbeLeafLayout, rawProbeLeafExample)

	return dir
}
