package render

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
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
  - name: subtitle
    type: text
    nullable: true
  - name: summary
    type: text
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
		`<span class="slide-raw-keys">{{range $k, $v := .raw}}{{$k}};{{end}}</span>` +
		`<span class="slide-subtitle">{{if .subtitle}}present{{else}}nil{{end}}</span>` +
		`<span class="slide-summary">{{if .summary}}present{{else}}nil{{end}}</span>` +
		`<span class="slide-blocks-count">{{if .blocks}}{{len .blocks}}{{else}}0{{end}}</span>` +
		`<span class="slide-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`<span class="slide-data-block-count">{{if .data.blocks}}{{len .data.blocks}}{{else}}0{{end}}</span>` +
		`{{range .blocks}}{{.}}{{end}}` +
		`{{range .data.blocks}}` +
		`<span class="data-block" data-index="{{.item.index}}" data-depth="{{.item.depth}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" data-raw-keys="{{range $k, $v := .raw}}{{$k}};{{end}}">` +
		`<span class="data-block-raw-title">{{.raw.title}}</span>` +
		`<span class="data-block-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="data-block-leaf-count">{{if .data.leaves}}{{len .data.leaves}}{{else}}0{{end}}</span>` +
		`{{range .data.leaves}}` +
		`<span class="data-leaf" data-index="{{.item.index}}" data-depth="{{.item.depth}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}">` +
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
  - name: subtitle
    type: text
    nullable: true
  - name: summary
    type: text
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
		`<span class="block-raw-keys">{{range $k, $v := .raw}}{{$k}};{{end}}</span>` +
		`<span class="block-subtitle">{{if .subtitle}}present{{else}}nil{{end}}</span>` +
		`<span class="block-summary">{{if .summary}}present{{else}}nil{{end}}</span>` +
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

	// The repeated-template probe: a slide template declaring a `loop` section
	// accepting the `repeatdataloop` section template, which itself declares the
	// same `loop` section accepting `repeatdataloop` — a self-accepting (cyclic)
	// declaration. The chain is built directly in the test (Register, no
	// Validate) so the render-time `.data` entry's per-level `item.depth` can be
	// pinned while the loader still rejects the cycle (item-context,
	// bounded-self-nesting; phase 7 adds max_depth and the loadable fixture).
	// The slide layout reads the top-level `.data.loop` entry's depth, and the
	// loop layout reads its own `.data.loop` entry's depth, so the three
	// self-nested levels read 0/1/2 from the `.data` view alone.
	repeatDataSlideLayout = `<section class="repeat-data-slide">` +
		`{{range .data.loop}}<span class="data-loop" data-depth="{{.item.depth}}" data-section="{{.item.section}}" data-template="{{.item.template}}"><span class="data-loop-name">{{.raw.name}}</span></span>{{end}}` +
		`{{range .loop}}{{.}}{{end}}` +
		`</section>`

	repeatDataLoopLayout = `<div class="repeat-data-loop" data-depth="{{.item.depth}}" data-section="{{.item.section}}" data-template="{{.item.template}}">` +
		`<span class="repeat-data-name">{{.name}}</span>` +
		`{{range .data.loop}}<span class="data-loop" data-depth="{{.item.depth}}" data-section="{{.item.section}}" data-template="{{.item.template}}"><span class="data-loop-name">{{.raw.name}}</span></span>{{end}}` +
		`{{range .loop}}{{.}}{{end}}` +
		`</div>`
)

// rawProbeSlide is the deck slide the test writes: one slide using the
// raw-context-probe layout, with two authored `blocks` instances — the first
// carrying two nested `leaves` — so the reserved `.raw`/`.data`/`.item` context
// is exercised at two depths and with same-name siblings. The field values
// carry Markdown so the converted value and its `.raw` source are visibly
// different.
//
// The null/absent contract is authored here: the slide and the first block each
// set `subtitle:` as an explicit YAML null (a nullable declared field), while
// neither sets `summary` (an optional declared field) — so the render-time `.raw`
// must carry `subtitle` as a present nil entry and omit `summary`, and the
// converted `.subtitle`/`.summary` both read nil. The second block omits both.
func rawProbeSlide() string {
	return "---\ntemplate: rawprobe\ntitle: '**Slide** title'\nsubtitle:\n---\n" +
		"Slide *body* source.\n" +
		"# blocks\n" +
		"```\ntemplate: rawprobeblock\ntitle: '**First** block'\nsubtitle:\n```\n" +
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
// at every depth, and its `depth` counts the enclosing same-template ancestors,
// 0 at every level of this DISTINCT-template fixture; and a `{{ section … }}`
// call adds no entry to the caller's `.data` and no element to its rendered
// section list, while its target still sees its own `.raw`.
//
// Two further contracts are pinned:
//
//   - the render-time `.raw`/`.data` null contract (raw-source-context): an
//     explicitly null declared field (`subtitle:`, a nullable field) is a
//     PRESENT nil entry of the instance's `.raw` and of its `.data` entry's
//     `.raw` — the `.raw` key listing carries `subtitle` — while an omitted
//     optional field (`summary`) is ABSENT from `.raw`; the corresponding
//     converted `.subtitle`/`.summary` both read nil, so an explicit null and an
//     absent optional are indistinguishable as field values but distinct in
//     `.raw`;
//   - the render-time `.data` per-level `item.depth` (item-context,
//     bounded-self-nesting): the loadable DISTINCT-template fixture shows depth
//     0 at every level, and a self-nesting chain built directly with Register
//     (no Validate, as Validate rejects the cycle until phase 7) reads 0/1/2
//     from the `.data` view of the slide and each nested template.
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
		// The render-time `.raw` null contract: the explicit-null `subtitle`
		// is a PRESENT nil entry (it appears in the key listing) while the
		// omitted `summary` is ABSENT (it does not), and both converted fields
		// read nil. The `template:` selector and the raw body's key are the
		// only other entries; `template` is undeclared so it is excluded.
		`<span class="slide-raw-keys">body;subtitle;title;</span>`,
		`<span class="slide-subtitle">nil</span>`,
		`<span class="slide-summary">nil</span>`,
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

	// The explicit-null `subtitle` converts to nil, never a "present" value, at
	// either the slide or a section instance; likewise the omitted `summary`.
	// Both are declared fields, so `present` can only appear if a source value
	// leaked into the converted field.
	for _, never := range []string{
		`<span class="slide-subtitle">present</span>`,
		`<span class="block-subtitle">present</span>`,
		`<span class="slide-summary">present</span>`,
		`<span class="block-summary">present</span>`,
	} {
		if strings.Contains(page, never) {
			t.Errorf("page contains %q, want the explicit null/absent field converted to nil:\n%s", never, page)
		}
	}

	// The nested section template (`rawprobeblock`) reads the same reserved
	// `.raw` source next to its converted fields, at composition depth. The
	// first block sets `subtitle:` null and omits `summary`, so its own `.raw`
	// carries `subtitle` (present nil) and no `summary`, while the converted
	// `.subtitle`/`.summary` read nil.
	for _, want := range []string{
		`<span class="block-title"><strong>First</strong> block</span>`,
		`<span class="block-raw-title">**First** block</span>`,
		`<div class="block-body"><p>First block body source.</p>`,
		`<span class="block-raw-body">First block body source.`,
		`<span class="block-raw-keys">body;subtitle;title;</span>`,
		`<span class="block-subtitle">nil</span>`,
		`<span class="block-summary">nil</span>`,
		`<span class="block-leaf-count">2</span>`,
		// The second block omits both, so its own `.raw` carries only the
		// authored `title`.
		`<span class="block-raw-keys">title;</span>`,
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
	//
	// Every level resolves a DISTINCT template (rawprobele leaves under
	// rawprobeblock blocks under the rawprobe slide), so no enclosing
	// same-template ancestor ever repeats and each `.item.depth` is 0
	// (item-context). The entry's `.raw` carries the same explicit-null/absent
	// contract as the instance's own `.raw`: the first block's `data-raw-keys`
	// includes `subtitle` (present nil) but not `summary`, the second's carries
	// only `title`.
	for _, want := range []string{
		`<span class="data-block" data-index="0" data-depth="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="blocks" data-template="rawprobeblock" data-parent="no" data-raw-keys="body;subtitle;title;">`,
		`<span class="data-block" data-index="1" data-depth="0" data-number="2" data-count="2" data-first="false" data-last="true" data-section="blocks" data-template="rawprobeblock" data-parent="no" data-raw-keys="title;">`,
		`<span class="data-block-raw-title">**First** block</span>`,
		`<span class="data-block-raw-body">First block body source.`,
		`<span class="data-block-raw-title">Second block</span>`,
		`<span class="data-block-raw-body">absent</span>`,
		`<span class="data-block-leaf-count">2</span>`,
		`<span class="data-block-leaf-count">0</span>`,
		`<span class="data-leaf" data-index="0" data-depth="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="leaves" data-template="rawprobele" data-parent="yes">`,
		`<span class="data-leaf" data-index="1" data-depth="0" data-number="2" data-count="2" data-first="false" data-last="true" data-section="leaves" data-template="rawprobele" data-parent="yes">`,
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

	// The DISTINCT-template fixture above pins `.data` item.depth 0 (no
	// enclosing same-template ancestor ever repeats). A level whose resolved
	// template REPEATS needs a self-nesting declaration, which is a composition
	// cycle that template.LoadLibrary (step 4, checkLibraryBuild) and
	// Registry.Validate reject until phase 7 adds max_depth. The render
	// pipeline itself (RenderSlide → slideData → renderSection) never
	// validates, so a self-nesting chain built directly here — Register, no
	// Validate, exactly as TestSectionInstanceContextPipeline's repeat subtest
	// does for the executed item descriptor — genuinely renders and pins the
	// `.data` entry's +1-per-repeat rule: the slide's `.data.loop` entry (the
	// outermost instance) is depth 0, the outer loop layout's `.data.loop`
	// entry (one same-template ancestor) depth 1, and the mid loop layout's
	// `.data.loop` entry (two) depth 2.
	t.Run("a self-nested chain carries per-level item.depth in .data", func(t *testing.T) {
		reg := template.NewRegistry(nil)
		for _, tmpl := range []*template.Template{
			{
				Name:     "repeatdata",
				Usage:    template.UsageSlide,
				Sections: []template.SectionDecl{{Name: "loop", Accepted: []string{"repeatdataloop"}, Max: 4}},
				Body:     template.BodyRule{Mode: template.BodyOptional},
				Layout:   template.Layout{Text: repeatDataSlideLayout},
			},
			{
				Name:     "repeatdataloop",
				Usage:    template.UsageSection,
				Fields:   []template.Field{{Name: "name", Type: template.FieldText, Required: true}},
				Sections: []template.SectionDecl{{Name: "loop", Accepted: []string{"repeatdataloop"}, Max: 4}},
				Body:     template.BodyRule{Mode: template.BodyDisallowed},
				Layout:   template.Layout{Text: repeatDataLoopLayout},
			},
		} {
			if err := reg.Register(tmpl); err != nil {
				t.Fatalf("register template %q: %v", tmpl.Name, err)
			}
		}

		// One `loop` instance nested three levels deep, every level resolving
		// the SAME template (`repeatdataloop`) through the self-accepting `loop`
		// declaration, so the enclosing chain each `.data` entry's depth is
		// measured against repeats at each step.
		inner := slide.Section{Name: "loop", Level: 3, Index: 0, Template: "repeatdataloop", Frontmatter: map[string]any{"name": "inner"}}
		mid := slide.Section{Name: "loop", Level: 2, Index: 0, Template: "repeatdataloop", Frontmatter: map[string]any{"name": "mid"}, Children: []slide.Section{inner}}
		outer := slide.Section{Name: "loop", Level: 1, Index: 0, Template: "repeatdataloop", Frontmatter: map[string]any{"name": "outer"}, Children: []slide.Section{mid}}
		parsed := &slide.Slide{
			File:        "slides/1-repeat-data.md",
			Template:    "repeatdata",
			Frontmatter: map[string]any{},
			Sections:    []slide.Section{outer},
		}

		rendered, err := RenderSlide(parsed, "repeat-data", &deck.Config{Title: "Repeat Data Deck"}, deck.Slide{Number: 1, Label: "repeat-data"}, 1, reg, template.LayoutFuncMap(nil, ""))
		if err != nil {
			t.Fatalf("RenderSlide: %v", err)
		}
		repeatPage := string(rendered)

		for _, want := range []string{
			// The slide's `.data.loop` entry: the outermost instance, no
			// enclosing same-template ancestor, so depth 0.
			`<span class="data-loop" data-depth="0" data-section="loop" data-template="repeatdataloop"><span class="data-loop-name">outer</span></span>`,
			// The outer loop layout's `.data.loop` entry: `mid`, one enclosing
			// same-template ancestor (`outer`), so depth 1.
			`<span class="data-loop" data-depth="1" data-section="loop" data-template="repeatdataloop"><span class="data-loop-name">mid</span></span>`,
			// The mid loop layout's `.data.loop` entry: `inner`, two enclosing
			// same-template ancestors (`outer`, `mid`), so depth 2.
			`<span class="data-loop" data-depth="2" data-section="loop" data-template="repeatdataloop"><span class="data-loop-name">inner</span></span>`,
		} {
			if !strings.Contains(repeatPage, want) {
				t.Errorf("self-nested .data page does not contain %q:\n%s", want, repeatPage)
			}
		}
	})
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

// ---------------------------------------------------------------------------
// plan.phase-02.task-7: the load-time and render-time `.raw`/`.data` views
// AGREE for helper-derived children FORWARDED through `children`.
//
// The unit tier (sectionhelper_test.go, render_test.go) drives the render-time
// `children` forwarding with an in-code registry; the sibling int test
// (section_context_integration_test.go) drives literal `child` arguments over a
// real layered library. This test drives the `children "<section>"
// .data.<section>` forwarding over a REAL on-disk library through the same
// library→deck→parse→render pipeline the whole-deck tests use, and pins the
// raw-source-context / section-data-context / section-template-context
// agreement:
//
//   - the rawchildhost slide layout forwards its OWN authored `cards` entries
//     with `children "cards" .data.cards` to the rawchildwrapper target;
//   - each forwarded entry re-instantiates into a child whose reserved `.raw`
//     (the authored field values and raw body) and whose nested `.data` equal
//     the authored source entry's, with the instantiated template's default
//     and the `template` selector never leaking into `.raw`;
//   - the helper call contributes to its TARGET only: the caller's `.data`
//     gains no key (no `rawchildwrapper;`) and the caller's rendered `cards`
//     list gains no element (still exactly the two authored instances).
//
// Load-time agreement, given no source seam. template.checkLibraryExample
// (templates-dir-validation step 7) DISCARDS the bytes it executes and is
// unexported in internal/template, which internal/render cannot import the
// other way, so the load-time-executed HTML cannot be captured from this
// package. As the sibling context_agreement_integration_test.go does, the test
// proves the two halves agree through the shared machinery they cannot avoid:
//
//   - the LOAD-TIME half is driven by template.LoadLibrary, whose step 7
//     executes the rawchildhost example layout UNGUARDED against the reserved
//     `.raw`/`.data` context and the load-time `children` forwarding, so a
//     successful load proves that half supplied the context and re-instantiated
//     the forwarded children;
//   - the RENDER-TIME half is driven by the full RenderDeck pipeline;
//   - the load-time-EQUIVALENT forwarding is rebuilt from the authored entries
//     with the SAME exported resolution and shared builders both halves use —
//     template.DataContext over the authored tree, template.ChildrenFunc, then
//     template.ResolveSectionCallLayered (exactly what exampleSectionHelper and
//     renderer.sectionHelper both call) — and its `.raw`/`.data` are required to
//     equal the authored source entry's and to appear VERBATIM in the
//     render-time page.
//
// Only `.raw`/`.data`/`.item` source values are compared: the converted field
// values legitimately differ (the render half runs text fields through inline
// Markdown) so they are never part of the compared fragment.
const (
	// rawChildForwardSource is the authored host slide. It is written BOTH as
	// the deck slide (slides/1-raw-child-forward.md) and as the rawchildhost
	// example.md, so the load-time example execution and the render-time slide
	// execution run the same layout against the same authored source. Two
	// `cards` instances are authored, the first carrying a nested `tags`
	// instance, so the `children` forwarding has a nested `.data` tree to
	// re-instantiate. The single-quoted YAML scalars keep the `**` Markdown
	// markers visible in `.raw`, distinct from the converted values.
	rawChildForwardSource = "---\n" +
		"template: rawchildhost\n" +
		"title: '**Host** title'\n" +
		"---\n" +
		"Host *body* source.\n" +
		"# cards\n" +
		"```\n" +
		"template: rawchildcard\n" +
		"text: '**First** card'\n" +
		"```\n" +
		"First card body source.\n" +
		"## tags\n" +
		"```\n" +
		"template: rawchildtag\n" +
		"name: '**Ada**'\n" +
		"```\n" +
		"# cards\n" +
		"```\n" +
		"template: rawchildcard\n" +
		"text: Second card\n" +
		"```\n"

	// rawChildForwardHostTemplateYAML declares the `cards` child section whose
	// authored entries the layout forwards, and a `subtitle` field carrying a
	// default that must never appear in `.raw`.
	rawChildForwardHostTemplateYAML = `description: host slide forwarding its authored cards as helper children
fields:
  - name: title
    type: text
    required: true
  - name: subtitle
    type: text
    default: HostDefaulted
sections:
  - name: cards
    accepted:
      - rawchildcard
    max: 4
body:
  mode: optional
`

	// rawChildForwardHostLayout reports the caller's own `.raw`, its authored
	// `.data` shape and its rendered `cards` list length (none of which the
	// helper call may change), then forwards its authored `cards` entries with
	// `children "cards" .data.cards`.
	rawChildForwardHostLayout = `<section class="raw-child-host">` +
		`<h1 class="host-title">{{.title}}</h1>` +
		`<span class="host-raw-title">{{.raw.title}}</span>` +
		`<span class="host-raw-subtitle">{{if .raw.subtitle}}{{.raw.subtitle}}{{else}}absent{{end}}</span>` +
		`<span class="host-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="host-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`<span class="host-data-card-count">{{if .data.cards}}{{len .data.cards}}{{else}}0{{end}}</span>` +
		`<span class="host-cards-count">{{if .cards}}{{len .cards}}{{else}}0{{end}}</span>` +
		`{{range .cards}}{{.}}{{end}}` +
		`{{ section "rawchildwrapper" (dict "heading" "Wrapper H") nil (children "cards" .data.cards) }}` +
		`</section>`

	// rawChildForwardWrapperTemplateYAML is the `section` call's target: it
	// declares the `cards` child section the forwarded entries fill.
	rawChildForwardWrapperTemplateYAML = `description: wrapper target holding forwarded cards children
fields:
  - name: heading
    type: text
sections:
  - name: cards
    accepted:
      - rawchildcard
    max: 4
body:
  mode: optional
`

	// rawChildForwardWrapperLayout is the `section` call's target: it reports
	// the length of its own `.data.cards` view and, in parallel, the reserved
	// `.raw`/`.item`/nested `.data` of each forwarded entry. It prints ONLY the
	// reserved views — never a converted field — so the load-time-equivalent
	// execution and the render-time execution produce identical bytes.
	rawChildForwardWrapperLayout = `<div class="raw-child-wrapper" data-heading="{{.heading}}" data-cards-data="{{if .data.cards}}{{len .data.cards}}{{else}}0{{end}}">` +
		`<span class="wrapper-heading">{{.heading}}</span>` +
		`<span class="wrapper-raw-heading">{{.raw.heading}}</span>` +
		`{{range .data.cards}}` +
		`<span class="wrapper-card" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}">` +
		`<span class="wrapper-card-raw-text">{{.raw.text}}</span>` +
		`<span class="wrapper-card-raw-subtitle">{{if .raw.subtitle}}{{.raw.subtitle}}{{else}}absent{{end}}</span>` +
		`<span class="wrapper-card-raw-template">{{if .raw.template}}{{.raw.template}}{{else}}absent{{end}}</span>` +
		`<span class="wrapper-card-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="wrapper-card-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`{{range .data.tags}}<span class="wrapper-card-tag-raw-name">{{.raw.name}}</span>{{end}}` +
		`</span>` +
		`{{end}}` +
		`</div>`

	// rawChildForwardCardTemplateYAML declares the `tags` child section and a
	// `subtitle` default that must never leak into a forwarded child's `.raw`.
	rawChildForwardCardTemplateYAML = `description: card section forwarded as a helper child
fields:
  - name: text
    type: text
    required: true
  - name: subtitle
    type: text
    default: CardDefaulted
sections:
  - name: tags
    accepted:
      - rawchildtag
    max: 4
body:
  mode: optional
`

	// rawChildForwardCardLayout prints the card's converted text next to its
	// reserved `.raw`/`.data`/`.item`, so the authored (rendered `cards` list)
	// instances and the forwarded (data view) instances are both observable.
	rawChildForwardCardLayout = `<div class="raw-child-card" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}">` +
		`<span class="card-text">{{.text}}</span>` +
		`<span class="card-raw-text">{{if .raw.text}}{{.raw.text}}{{else}}absent{{end}}</span>` +
		`<span class="card-raw-subtitle">{{if .raw.subtitle}}{{.raw.subtitle}}{{else}}absent{{end}}</span>` +
		`<span class="card-raw-template">{{if .raw.template}}{{.raw.template}}{{else}}absent{{end}}</span>` +
		`<span class="card-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="card-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`{{range .data.tags}}<span class="card-tag-raw-name">{{.raw.name}}</span>{{end}}` +
		`{{range .tags}}{{.}}{{end}}` +
		`</div>`

	rawChildForwardTagTemplateYAML = `description: tag section nested one level deeper
fields:
  - name: name
    type: text
    required: true
body:
  mode: disallowed
`

	rawChildForwardTagLayout = `<span class="raw-child-tag" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}">` +
		`<span class="tag-name">{{.name}}</span>` +
		`<span class="tag-raw-name">{{.raw.name}}</span>` +
		`</span>`

	rawChildForwardHostExample = rawChildForwardSource

	rawChildForwardWrapperExample = "```\nheading: Wrapper example\n```\n"

	rawChildForwardCardExample = "```\ntext: Sample card\n```\n" +
		"Sample card body.\n" +
		"# tags\n" +
		"```\ntemplate: rawchildtag\nname: Sample tag\n```\n"

	rawChildForwardTagExample = "```\nname: Sample tag\n```\n"
)

// TestRenderChildForwardRawDataAgreement is the integration-test deliverable
// for plan.phase-02.task-7. It reads a real on-disk library and deck, so it is
// skipped under -short.
//
// It pins (raw-source-context, section-data-context, section-template-context):
// a `children "<section>" .data.<section>` forwarding re-instantiates each
// authored `.data` entry as a child whose reserved `.raw` field values and raw
// body plus its nested `.data` equal the authored source — with no default and
// no `template` selector leaking into `.raw` — and the helper call adds no key
// to the caller's `.data` and no element to the caller's rendered section list.
func TestRenderChildForwardRawDataAgreement(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writeRawChildForwardDeck(t)
	fsys := os.DirFS(dir)

	// LOAD-TIME half: LoadLibrary's step 7 executes the rawchildhost example
	// layout UNGUARDED — it reads `.raw`, `.data.cards`, forwards them through
	// `children`, and reads each forwarded child's `.raw`/`.data` — so a
	// successful load is itself the assertion that the load-time half supplied
	// the reserved context and re-instantiated the forwarded children.
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v (the unguarded forwarded-children example must load)", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("template.Registry.Validate: %v", err)
	}

	// The deck slide and the template's own example.md are the SAME authored
	// content, so both halves run the same layout against the same source.
	slideBytes, err := os.ReadFile(filepath.Join(dir, "slides", "1-raw-child-forward.md"))
	if err != nil {
		t.Fatalf("read deck slide: %v", err)
	}
	host := lib.Slides["rawchildhost"]
	if host == nil {
		t.Fatal(`TemplateByName("rawchildhost") not loaded`)
	}
	if !bytes.Equal(slideBytes, host.ExampleBytes) {
		t.Fatalf("deck slide and rawchildhost example.md must be the same authored content:\nslide:\n%s\nexample:\n%s", slideBytes, host.ExampleBytes)
	}

	// RENDER-TIME half: the full library→parser→render pipeline on the same
	// authored content.
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
	if len(parsed) != 1 {
		t.Fatalf("parsed %d slides, want the one authored host slide", len(parsed))
	}
	pageHTML, err := RenderDeck(cfg, d, parsed, reg, themeReg, funcMap)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	page := string(pageHTML)

	// The load-time-EQUIVALENT authored `cards` entries, built with the SAME
	// shared builder (template.DataContext) the load-time example execution
	// uses, over the neutral ContextNode tree the test's own adapter builds from
	// the parsed deck slide (the exact authored tree both halves see, since the
	// example bytes are the slide bytes).
	loadData := template.DataContext(agreementNodes(reg, parsed[0].Sections), nil, nil)
	authCards, ok := loadData["cards"].([]map[string]any)
	if !ok || len(authCards) != 2 {
		t.Fatalf("load-time-equivalent .data.cards = %#v (%T), want two authored entries", loadData["cards"], loadData["cards"])
	}

	// Forward exactly those authored entries, and resolve with the SAME
	// exported resolution both the load-time and render-time helpers call, so the
	// resolved children are what either half would instantiate.
	req, err := template.ChildrenFunc("cards", loadData["cards"])
	if err != nil {
		t.Fatalf("template.ChildrenFunc: %v", err)
	}
	call, err := template.ResolveSectionCallLayered(reg.Lookup, reg.LayerOf, "rawchildwrapper",
		map[string]any{"heading": "Wrapper H"}, "", nil, nil, req)
	if err != nil {
		t.Fatalf("template.ResolveSectionCallLayered: %v", err)
	}
	if len(call.Children) != 2 {
		t.Fatalf("resolved %d forwarded children, want 2", len(call.Children))
	}

	// AGREEMENT: each forwarded entry re-instantiates into a child whose
	// reserved `.raw` (authored field values and raw body) and nested `.data`
	// equal the authored source entry's, because both are built by the shared
	// builders over the same resolved child.
	for i, child := range call.Children {
		entry := authCards[i]
		raw := template.RawContext(child.Source, child.Call.Template, child.Call.Body)
		if !reflect.DeepEqual(raw, entry["raw"]) {
			t.Errorf("forwarded child %d .raw =\n  %#v\nwant the authored source entry raw\n  %#v", i, raw, entry["raw"])
		}
		gotData := template.DataContext(childNodes(child.Children), nil, raw)
		if !reflect.DeepEqual(gotData, entry["data"]) {
			t.Errorf("forwarded child %d .data =\n  %#v\nwant the authored source entry data\n  %#v", i, gotData, entry["data"])
		}
		// Neither a materialised default nor the `template` selector is an
		// authored source value, so neither leaks into `.Source` or `.raw`; the
		// raw body travels as call.Body, not as a `.Source` field.
		for _, absent := range []string{"subtitle", "template"} {
			if _, has := child.Source[absent]; has {
				t.Errorf("forwarded child %d .Source = %#v, want %q excluded (default/selector is not an authored field)", i, child.Source, absent)
			}
			if _, has := raw[absent]; has {
				t.Errorf("forwarded child %d .raw = %#v, want %q excluded (default/selector is not authored source)", i, raw, absent)
			}
		}
		if _, has := child.Source["body"]; has {
			t.Errorf("forwarded child %d .Source = %#v, want body excluded (the raw body travels as call.Body)", i, child.Source)
		}
	}

	// The load-time-EQUIVALENT target fragment: execute the wrapper layout with
	// the context the shared builders give it and require the render-time page to
	// carry it verbatim. A divergent `.raw`/`.data`/`.item` — a leaked default
	// or selector, a differently keyed or parented nested tree — would break this
	// containment.
	wrapper := lib.Sections["rawchildwrapper"]
	if wrapper == nil {
		t.Fatal(`TemplateByName("rawchildwrapper") not loaded`)
	}
	callRaw := template.RawContext(call.Source, call.Template, call.Body)
	wrapperCtx := make(map[string]any, len(call.Values)+3)
	for k, v := range call.Values {
		wrapperCtx[k] = v
	}
	wrapperCtx["item"] = call.Item
	wrapperCtx["raw"] = callRaw
	wrapperCtx["data"] = template.DataContext(childNodes(call.Children), nil, callRaw)
	wrapperHTML := executeStandalone(t, "rawchildwrapper", wrapper.LayoutText, template.LayoutFuncMap(nil, ""), wrapperCtx)
	if !strings.Contains(page, wrapperHTML) {
		t.Errorf("render-time page does not embed the load-time-equivalent forwarded-child fragment:\nwant:\n%s\npage:\n%s", wrapperHTML, page)
	}

	// The concrete forwarded-child values, in the render-time page: each entry's
	// own sibling descriptor, its `.raw` source (no default, no selector) and its
	// nested `.data` tag raw value.
	for _, want := range []string{
		`<div class="raw-child-wrapper" data-heading="Wrapper H" data-cards-data="2">`,
		`<span class="wrapper-heading">Wrapper H</span>`,
		`<span class="wrapper-raw-heading">Wrapper H</span>`,
		`<span class="wrapper-card" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="cards" data-template="rawchildcard" data-parent="yes">`,
		`<span class="wrapper-card-raw-text">**First** card</span>`,
		`<span class="wrapper-card-raw-subtitle">absent</span>`,
		`<span class="wrapper-card-raw-template">absent</span>`,
		`<span class="wrapper-card-raw-body">First card body source.</span>`,
		`<span class="wrapper-card-data-keys">tags;</span>`,
		`<span class="wrapper-card-tag-raw-name">**Ada**</span>`,
		`<span class="wrapper-card" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="cards" data-template="rawchildcard" data-parent="yes">`,
		`<span class="wrapper-card-raw-text">Second card</span>`,
		`<span class="wrapper-card-raw-body">absent</span>`,
		`<span class="wrapper-card-data-keys"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain the forwarded-child value %q:\n%s", want, page)
		}
	}

	// NON-CONTRIBUTION: the helper call contributes to its TARGET only. The
	// caller's `.raw` and `.data` are unchanged (the only `.data` key is the
	// authored `cards`, never the helper target), and its rendered `cards` list
	// still holds exactly the two authored instances — no third element.
	for _, want := range []string{
		`<h1 class="host-title"><strong>Host</strong> title</h1>`,
		`<span class="host-raw-title">**Host** title</span>`,
		`<span class="host-raw-subtitle">absent</span>`,
		`<span class="host-raw-body">Host *body* source.</span>`,
		`<span class="host-data-keys">cards;</span>`,
		`<span class="host-data-card-count">2</span>`,
		`<span class="host-cards-count">2</span>`,
		// The two AUTHORED cards still render through the card layout; the
		// helper's forwarded children are the target's data view, not extra
		// elements of the caller's list.
		`<span class="card-raw-text">**First** card</span>`,
		`<span class="card-raw-text">Second card</span>`,
		`<span class="card-tag-raw-name">**Ada**</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain the caller value %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "rawchildwrapper;") {
		t.Errorf("the helper call leaked its target into the caller's .data:\n%s", page)
	}
	if got := strings.Count(page, `<div class="raw-child-wrapper"`); got != 1 {
		t.Errorf("rendered helper target count = %d, want 1:\n%s", got, page)
	}
	if got := strings.Count(page, `<div class="raw-child-card"`); got != 2 {
		t.Errorf("rendered authored raw-child-card count = %d, want 2 (the helper adds no element to the caller's list):\n%s", got, page)
	}
	if strings.Contains(page, "map[") {
		t.Errorf("page carries a section data map rather than rendered values:\n%s", page)
	}
}

// writeRawChildForwardDeck writes a fresh temporary deck directory: kalide.yaml,
// one slide under slides/, a copy of the fixture templates/ tree, and the
// raw-child-forward host/wrapper/card/tag templates the slide uses.
func writeRawChildForwardDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for name, data := range map[string]string{
		"kalide.yaml":                   "title: Raw Child Forward Deck\ntheme: plain\n",
		"slides/1-raw-child-forward.md": rawChildForwardSource,
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

	writeProbeTemplate(t, dir, "slides", "rawchildhost", rawChildForwardHostTemplateYAML, rawChildForwardHostLayout, rawChildForwardHostExample)
	writeProbeTemplate(t, dir, "sections", "rawchildwrapper", rawChildForwardWrapperTemplateYAML, rawChildForwardWrapperLayout, rawChildForwardWrapperExample)
	writeProbeTemplate(t, dir, "sections", "rawchildcard", rawChildForwardCardTemplateYAML, rawChildForwardCardLayout, rawChildForwardCardExample)
	writeProbeTemplate(t, dir, "sections", "rawchildtag", rawChildForwardTagTemplateYAML, rawChildForwardTagLayout, rawChildForwardTagExample)

	return dir
}
