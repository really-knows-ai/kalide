package render

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// The section-context-probe library adds a slide template declaring a `footer`
// section (the motivating `{{.slide.number}} / {{.slide.total}}` footer) and a
// `columns` section accepting a `probecolumn` section template, which itself
// declares a `people` child section accepting `probeperson` one level deeper —
// so this test drives the reserved `.deck`/`.slide` context AND the reserved
// `.item` descriptor (item-context) into every section instance, at every
// depth, through the real library→parser→render pipeline. Every section layout
// reads `.item` directly: the load-time example context supplies the one-item
// descriptor (checkLibraryExample), exactly as the real renderer does, so the
// layouts need no guard. A slide layout, by contrast, has no `.item`.
const (
	sectionProbeSlideTemplateYAML = `description: slide with a footer section and a nested column/people/person section
fields:
  - name: title
    type: text
    required: true
sections:
  - name: footer
    accepted:
      - probefooter
    max: 1
  - name: columns
    accepted:
      - probecolumn
    max: 4
body:
  mode: optional
`

	sectionProbeSlideLayout = `<section>
  <h1>{{.title}}</h1>
  {{ range .footer }}{{ . }}{{ end }}
  {{ range .columns }}{{ . }}{{ end }}
</section>
`

	sectionProbeSlideExample = `---
template: sectioncontextprobe
title: Example
---
An example slide with sections.

# footer
` + "```\ntemplate: probefooter\n```" + `

# columns
` + "```\ntemplate: probecolumn\ntitle: Example column\n```" + `

## people
` + "```\ntemplate: probeperson\nname: Ada\n```" + `
`

	sectionProbeFooterTemplateYAML = `description: footer section probing the reserved slide-position context
body:
  mode: disallowed
`

	sectionProbeFooterLayout = `<footer class="probe-footer" data-index="{{ .item.index }}" data-depth="{{ .item.depth }}" data-number="{{ .item.number }}" data-count="{{ .item.count }}" data-first="{{ .item.first }}" data-last="{{ .item.last }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}" data-parent="{{ if .item.parent }}present{{ else }}absent{{ end }}">{{ .slide.number }} / {{ .slide.total }}</footer>`

	sectionProbeFooterExample = "```\n```\n"

	sectionProbeColumnTemplateYAML = `description: column section holding a nested people child section
fields:
  - name: title
    type: text
sections:
  - name: people
    accepted:
      - probeperson
    max: 4
body:
  mode: optional
`

	sectionProbeColumnLayout = `<div class="probe-column" data-index="{{ .item.index }}" data-depth="{{ .item.depth }}" data-number="{{ .item.number }}" data-count="{{ .item.count }}" data-first="{{ .item.first }}" data-last="{{ .item.last }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}" data-parent="{{ if .item.parent }}present{{ else }}absent{{ end }}"><span class="column-title">{{ .title }}</span><span class="column-deck-title">{{ .deck.title }}</span><span class="column-slide-number">{{ .slide.number }}</span>{{ range .people }}{{ . }}{{ end }}</div>`

	sectionProbeColumnExample = "```\ntitle: Example column\n```\n# people\n```\ntemplate: probeperson\nname: Ada\n```\n"

	sectionProbePersonTemplateYAML = `description: person section nested one level deeper inside a column's people section
fields:
  - name: name
    type: text
    required: true
body:
  mode: disallowed
`

	sectionProbePersonLayout = `<span class="person-name">{{ .name }}</span><span class="person-deck-title">{{ .deck.title }}</span><span class="person-slide-number">{{ .slide.number }}</span><span class="person-item" data-index="{{ .item.index }}" data-depth="{{ .item.depth }}" data-number="{{ .item.number }}" data-count="{{ .item.count }}" data-first="{{ .item.first }}" data-last="{{ .item.last }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}" data-parent-title="{{ .item.parent.title }}"></span>`

	sectionProbePersonExample = "```\nname: Ada\n```\n"

	// The repeated-template probe: a slide template declaring a `loop` section
	// accepting the `probeloop` section template, which itself declares the
	// same `loop` section accepting `probeloop` — a self-accepting (cyclic)
	// declaration. The chain is built directly in the test (Register, no
	// Validate) so the render-time `item.depth` at each repeat can be pinned
	// while the loader still rejects the cycle (item-context,
	// bounded-self-nesting; phase 7 adds max_depth and the loadable fixture).
	// Every layout is a single compact line so the fragment can be asserted
	// exactly, and the loop layout prints its per-level `.item` descriptor.
	repeatProbeSlideLayout = `<section class="repeat-slide">{{range .loop}}{{.}}{{end}}</section>`

	repeatProbeLoopLayout = `<div class="repeat-loop" data-depth="{{.item.depth}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}present{{else}}absent{{end}}" data-parent-name="{{if .item.parent}}{{.item.parent.name}}{{end}}"><span class="repeat-name">{{.name}}</span>{{range .loop}}{{.}}{{end}}</div>`
)

// TestSectionInstanceContextPipeline drives the reserved `.deck`/`.slide`
// context (template-context, section-template-context) AND the reserved `.item`
// descriptor (item-context) through the library→parser→render pipeline into a
// slide layout and every section instance it declares, at every depth: the
// motivating footer section renders `{{.slide.number}} / {{.slide.total}}`
// (e.g. `2a / 3`); a top-level section instance reads `.deck.title`,
// `.slide.number` and its own `.item` (index/depth/number/count/first/last/
// section/template, with a nil parent, since a top-level instance's parent is
// the slide); and a nested child section reads the same `.item` one level down
// plus `.item.parent.title`, the enclosing instance's field values.
//
// `item.depth` is the count of the instance's enclosing instances whose
// resolved template repeats its own template (item-context,
// bounded-self-nesting): 0 at the top level and +1 per self-nested level whose
// resolved template repeats. The disk fixture here composes four DISTINCT
// templates (footer, columns/probecolumn, people/probeperson), so every level's
// depth is 0 — the repeat case is pinned below by a directly-built
// self-nesting chain, because a self-accepting declaration is a composition
// cycle that LoadLibrary/Registry.Validate reject until phase 7 adds max_depth.
//
// It reads the real filesystem, so it is skipped under -short.
func TestSectionInstanceContextPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writeSectionProbeDeck(t,
		"title: Section Context Deck\n"+
			"theme: plain\n",
		map[string]string{
			"1-intro.md":   sectionProbeSlide("Intro"),
			"2-main.md":    sectionProbeSlide("Main"),
			"2a-detail.md": sectionProbeSlide("Detail"),
		})
	page := renderSectionProbeDeck(t, dir)

	for _, want := range []string{
		// The motivating footer: string position label over the integer total,
		// plus its top-level `.item` (one of one; parent absent).
		`<footer class="probe-footer" data-index="0" data-depth="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="footer" data-template="probefooter" data-parent="absent">2a / 3</footer>`,
		// .deck.title and .slide.number from the top-level column and the
		// person section nested one level deeper inside the column.
		`<span class="column-deck-title">Section Context Deck</span>`,
		`<span class="person-deck-title">Section Context Deck</span>`,
		`<span class="column-slide-number">2a</span>`,
		`<span class="person-slide-number">2a</span>`,
		// Section instance fields stay directly addressable alongside
		// deck/slide/item.
		`<span class="column-title">First column</span>`,
		`<span class="column-title">Second column</span>`,
		`<span class="person-name">Ada</span>`,
		`<span class="person-name">Grace</span>`,
		`<span class="person-name">Linus</span>`,
		// Top-level `.item`: index/number/count/first/last/section/template,
		// with a nil parent (the parent is the slide).
		`<div class="probe-column" data-index="0" data-depth="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="columns" data-template="probecolumn" data-parent="absent"><span class="column-title">First column</span>`,
		`<div class="probe-column" data-index="1" data-depth="0" data-number="2" data-count="2" data-first="false" data-last="true" data-section="columns" data-template="probecolumn" data-parent="absent"><span class="column-title">Second column</span>`,
		// Nested `.item`: the same descriptor one level down, where
		// `.item.parent.title` resolves the enclosing column instance's field
		// values.
		`<span class="person-item" data-index="0" data-depth="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="people" data-template="probeperson" data-parent-title="First column"></span>`,
		`<span class="person-item" data-index="1" data-depth="0" data-number="2" data-count="2" data-first="false" data-last="true" data-section="people" data-template="probeperson" data-parent-title="First column"></span>`,
		`<span class="person-item" data-index="0" data-depth="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="people" data-template="probeperson" data-parent-title="Second column"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}

	// The DISTINCT-template levels above pin depth 0. A level whose resolved
	// template REPEATS needs a self-nesting declaration, which is a composition
	// cycle: template.LoadLibrary (step 4, checkLibraryBuild) and
	// Registry.Validate reject it as a *SectionCycleError until phase 7 adds
	// max_depth to a bounded self-accepting declaration. The render pipeline
	// itself (RenderSlide → slideData → renderSection → renderInstance) does not
	// validate, so a self-nesting chain built directly here — Register, no
	// Validate — genuinely renders, and pins the +1-per-repeat rule: the outer
	// `loop` instance is depth 0, the `loop` nested in it depth 1 and the third
	// depth 2, each carrying its own item.section and item.parent (nil at the
	// top level, the enclosing instance's field values below). The on-disk,
	// loadable repeat case is pinned later by phase 7 task 21
	// (TestRenderDeckNestedSections), once max_depth makes the chain loadable.
	t.Run("repeat adds one depth per self-nested level", func(t *testing.T) {
		reg := template.NewRegistry(nil)
		for _, tmpl := range []*template.Template{
			{
				Name:     "repeatprobeslide",
				Usage:    template.UsageSlide,
				Sections: []template.SectionDecl{{Name: "loop", Accepted: []string{"probeloop"}, Max: 4}},
				Body:     template.BodyRule{Mode: template.BodyOptional},
				Layout:   template.Layout{Text: repeatProbeSlideLayout},
			},
			{
				Name:     "probeloop",
				Usage:    template.UsageSection,
				Fields:   []template.Field{{Name: "name", Type: template.FieldText, Required: true}},
				Sections: []template.SectionDecl{{Name: "loop", Accepted: []string{"probeloop"}, Max: 4}},
				Body:     template.BodyRule{Mode: template.BodyDisallowed},
				Layout:   template.Layout{Text: repeatProbeLoopLayout},
			},
		} {
			if err := reg.Register(tmpl); err != nil {
				t.Fatalf("register template %q: %v", tmpl.Name, err)
			}
		}

		// One `loop` instance nested three levels deep, every level resolving
		// the SAME template (probeloop) through the self-accepting `loop`
		// declaration, so the enclosing chain a level's depth is measured
		// against repeats at each step.
		inner := slide.Section{Name: "loop", Level: 3, Index: 0, Template: "probeloop", Frontmatter: map[string]any{"name": "inner"}}
		mid := slide.Section{Name: "loop", Level: 2, Index: 0, Template: "probeloop", Frontmatter: map[string]any{"name": "mid"}, Children: []slide.Section{inner}}
		outer := slide.Section{Name: "loop", Level: 1, Index: 0, Template: "probeloop", Frontmatter: map[string]any{"name": "outer"}, Children: []slide.Section{mid}}
		parsed := &slide.Slide{
			File:        "slides/1-repeat.md",
			Template:    "repeatprobeslide",
			Frontmatter: map[string]any{},
			Sections:    []slide.Section{outer},
		}

		rendered, err := RenderSlide(parsed, "repeat", &deck.Config{Title: "Repeat Deck"}, deck.Slide{Number: 1, Label: "repeat"}, 1, reg, template.LayoutFuncMap(nil, ""))
		if err != nil {
			t.Fatalf("RenderSlide: %v", err)
		}
		page := string(rendered)

		for _, want := range []string{
			// Top level: the slide is not an instance of any template, so the
			// outermost `loop` is depth 0 with a nil parent.
			`<div class="repeat-loop" data-depth="0" data-section="loop" data-template="probeloop" data-parent="absent" data-parent-name=""><span class="repeat-name">outer</span>`,
			// First repeat: one enclosing same-template ancestor (outer).
			`<div class="repeat-loop" data-depth="1" data-section="loop" data-template="probeloop" data-parent="present" data-parent-name="outer"><span class="repeat-name">mid</span>`,
			// Second repeat: two enclosing same-template ancestors (outer, mid).
			`<div class="repeat-loop" data-depth="2" data-section="loop" data-template="probeloop" data-parent="present" data-parent-name="mid"><span class="repeat-name">inner</span>`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("repeated-template page does not contain %q:\n%s", want, page)
			}
		}
	})
}

// sectionProbeSlide is one slide file using the section-context-probe layout,
// declaring a footer section and two column sections, the first holding two
// nested people child sections and the second one, so the `.item` descriptor's
// index/number/count/first/last are exercised at two depths.
func sectionProbeSlide(title string) string {
	return "---\ntemplate: sectioncontextprobe\ntitle: " + title + "\n---\n" +
		"# footer\n```\ntemplate: probefooter\n```\n" +
		"\n# columns\n```\ntemplate: probecolumn\ntitle: First column\n```\n" +
		"## people\n```\ntemplate: probeperson\nname: Ada\n```\n" +
		"## people\n```\ntemplate: probeperson\nname: Grace\n```\n" +
		"\n# columns\n```\ntemplate: probecolumn\ntitle: Second column\n```\n" +
		"## people\n```\ntemplate: probeperson\nname: Linus\n```\n"
}

// writeSectionProbeDeck writes a fresh temporary deck directory: kalideYAML as
// kalide.yaml, each slide under slides/, a copy of the fixture library's
// templates/ tree, and the section-context-probe slide/section templates the
// slides use.
func writeSectionProbeDeck(t *testing.T, kalideYAML string, slides map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{"kalide.yaml": kalideYAML}
	for name, body := range slides {
		files[filepath.Join("slides", name)] = body
	}
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	copyTemplatesDir(t, filepath.Join(fixtureLibraryDir, "templates"), filepath.Join(dir, "templates"))

	writeProbeTemplate(t, dir, "slides", "sectioncontextprobe", sectionProbeSlideTemplateYAML, sectionProbeSlideLayout, sectionProbeSlideExample)
	writeProbeTemplate(t, dir, "sections", "probefooter", sectionProbeFooterTemplateYAML, sectionProbeFooterLayout, sectionProbeFooterExample)
	writeProbeTemplate(t, dir, "sections", "probecolumn", sectionProbeColumnTemplateYAML, sectionProbeColumnLayout, sectionProbeColumnExample)
	writeProbeTemplate(t, dir, "sections", "probeperson", sectionProbePersonTemplateYAML, sectionProbePersonLayout, sectionProbePersonExample)

	return dir
}

// writeProbeTemplate writes one template's manifest, layout and example under
// dir/templates/<kind>/<name>/.
func writeProbeTemplate(t *testing.T, dir, kind, name, templateYAML, layout, example string) {
	t.Helper()
	probeDir := filepath.Join(dir, "templates", kind, name)
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", probeDir, err)
	}
	for fname, data := range map[string]string{
		"template.yaml":    templateYAML,
		"layout.html.tmpl": layout,
		"example.md":       example,
	} {
		if err := os.WriteFile(filepath.Join(probeDir, fname), []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", filepath.Join(probeDir, fname), err)
		}
	}
}

// renderSectionProbeDeck loads and renders one temporary deck directory
// through the library→parser→render pipeline: theme.LoadDir, a registry built
// over the on-disk section-context-probe library with template.LoadLibrary
// followed by template.NewRegistryFromLibrary, deck.LoadConfig,
// deck.LoadSlides, slide.Parse and RenderDeck. It returns the page HTML,
// failing the test on any error.
func renderSectionProbeDeck(t *testing.T, dir string) string {
	t.Helper()
	fsys := os.DirFS(dir)

	themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}

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
	funcMap := template.LayoutFuncMap(nil, "")

	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themeReg)
	if err != nil {
		t.Fatalf("deck.LoadConfig: %v", err)
	}
	d, err := deck.LoadSlides(fsys, deck.SlidesDir)
	if err != nil {
		t.Fatalf("deck.LoadSlides: %v", err)
	}

	var parsed []*slide.Slide
	for i := range d.Stacks {
		stack := &d.Stacks[i]
		ordered := append([]deck.Slide{stack.Slide}, stack.Vertical...)
		for _, s := range ordered {
			src, err := fs.ReadFile(fsys, s.Path)
			if err != nil {
				t.Fatalf("read %s: %v", s.Path, err)
			}
			ps, err := slide.Parse(s.Path, src, reg)
			if err != nil {
				t.Fatalf("slide.Parse(%s): %v", s.Path, err)
			}
			parsed = append(parsed, ps)
		}
	}

	pageHTML, err := RenderDeck(cfg, d, parsed, reg, themeReg, funcMap)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	return string(pageHTML)
}

// ---------------------------------------------------------------------------
// plan.phase-02.task-6: the reserved context of helper-instantiated children
// over a REAL on-disk LAYERED library.
//
// The unit tier (sectionhelper_test.go, render_test.go) drives the helper
// children path with in-code registries and with fake layered libraries; the
// real layered loader is exercised end to end here, through the same
// library→deck→parse→render pipeline the whole-deck tests use. A two-layer deck
// (the deck-root `templates/` local layer plus a configured external `templates:`
// root) is loaded by template.LoadDeckLibrary, which records each layer's role
// (external first, local second) and validates the merged set, then rendered by
// RenderDeck.
//
// The slide layout makes ONE `{{ section … }}` call whose child arguments are a
// literal `child "external"` (whose template lives in the OTHER, external layer)
// and a literal `child "local"` (a local template) carrying a nested
// `child "grand"` grandchild (local). It pins the reserved context at every
// depth the helper instantiates:
//
//   - each helper child reads its own `.item.*` descriptor (index/number/count/
//     first/last/section/template and, for the target's fields, parent), its own
//     `.raw.*` source view and its own `.data.*` view — the local child's `.data`
//     holds its grandchild, so the grandchild renders through the child's OWN
//     template (section-data-context);
//   - the reserved `.deck`/`.slide` entries reach every helper child and
//     grandchild (template-context, section-template-context);
//   - the target reads its own `.data.<child>` view (len over the passed set),
//     while the call adds no key to the CALLER's `.data` (section-data-context);
//   - a child whose template lives in the OTHER library layer resolves its
//     `media` helper in ITS OWN layer: the external-layer child emits the
//     per-layer external URL `/assets/templates/external/media/…` from the
//     external media filesystem, while the local children keep the primary
//     `/assets/templates/media/…` URL from the local media filesystem
//     (library-layer-references).
//
// It reads a real deck and two real library directories from disk, so it is
// skipped under -short like its neighbours. It reuses writeLayerFile /
// writeLayerManifest / writeLayerMedia / writeLayerTheme (sectionhelper_test.go,
// deck_integration_test.go) and renderLayeredDeckPipeline (deck_integration_test.go).
func TestSectionInstanceContextHelperChildrenLayered(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads real deck and layered library directories from disk")
	}

	deckRoot, extRoot := writeHelperChildLayeredDeck(t)
	localRoot := filepath.Join(deckRoot, template.TemplatesDir)

	lib, page := renderLayeredDeckPipeline(t, deckRoot, extRoot, localRoot)

	// The deck is genuinely two-layered: the configured external root first,
	// the deck-root templates/ second. The external child template is owned by
	// the external layer only, so the per-layer media binding below is anchored
	// to a real layer boundary rather than a merged namespace.
	if len(lib.Layers) != 2 {
		t.Fatalf("len(Layers) = %d, want 2 (external + local)", len(lib.Layers))
	}
	if lib.Layers[0].Role != template.LayerRoleExternal {
		t.Errorf("Layers[0].Role = %v, want LayerRoleExternal", lib.Layers[0].Role)
	}
	if lib.Layers[1].Role != template.LayerRoleLocal {
		t.Errorf("Layers[1].Role = %v, want LayerRoleLocal", lib.Layers[1].Role)
	}
	if _, ok := lib.Layers[0].Sections["externalchild"]; !ok {
		t.Errorf("the external layer does not carry externalchild")
	}
	if _, ok := lib.Layers[1].Sections["externalchild"]; ok {
		t.Errorf("the local layer unexpectedly carries externalchild; it must be external-only so the layer binding is observable")
	}
	if _, err := lib.Layers[0].Media.Open("ext-logo.svg"); err != nil {
		t.Errorf("the external layer media does not hold ext-logo.svg: %v", err)
	}
	if _, err := lib.Layers[1].Media.Open("ext-logo.svg"); err == nil {
		t.Error("the local layer media holds ext-logo.svg, want it external-only so the fixture proves the layer binding")
	}

	// The literal CHILD whose template lives in the OTHER (external) layer: it
	// reads its own `.item`/`.raw`/`.data`/`.deck`/`.slide` and its `media`
	// helper resolves in the external layer — URL and external media filesystem.
	for _, want := range []string{
		// The target's own `.data.<child>` view holds the passed set (one
		// external child and one local child), and its rendered child lists
		// splice them.
		`<div class="child-wrapper" data-external="1" data-local="1" data-external-data="1" data-local-data="1"><span class="wrapper-heading">Wrapper H</span>`,
		// The external-layer child: its own one-item descriptor, its `.raw`
		// source fields/body, its own (empty) `.data`, the reserved
		// `.deck`/`.slide` entries, and the external per-layer media URL.
		`<span class="external-child" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="external" data-template="externalchild" data-parent="yes" data-parent-heading="Wrapper H">` +
			`<span class="external-child-label">Ext child</span>` +
			`<span class="external-child-raw-label">Ext child</span>` +
			`<span class="external-child-raw-body">Ext child body</span>` +
			`<span class="external-child-deck">Helper Child Deck</span>` +
			`<span class="external-child-slide">1</span>` +
			`<span class="external-child-data-keys"></span>` +
			`<img class="external-child-media" src="/assets/templates/external/media/ext-logo.svg"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}

	// The LOCAL child and its GRANDCHILD: each reads its own reserved context at
	// its own depth; the local child's `.data` holds the grandchild and its
	// rendered `grand` list splices it, so the grandchild is instantiated by the
	// child's OWN template. Both resolve their `media` helper in the primary
	// (local) layer.
	for _, want := range []string{
		// The local child: descriptor, `.raw`, its own `.data.grand` view, the
		// deck/slide entries, and the primary media URL.
		`<span class="local-child" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="local" data-template="localchild" data-parent="yes" data-parent-heading="Wrapper H">` +
			`<span class="local-child-label">Local child</span>` +
			`<span class="local-child-raw-label">Local child</span>` +
			`<span class="local-child-raw-body">Local child body</span>` +
			`<span class="local-child-deck">Helper Child Deck</span>` +
			`<span class="local-child-slide">1</span>` +
			`<span class="local-child-data-keys">grand;</span>` +
			`<span class="local-child-grand-count">1</span>` +
			`<img class="local-child-media" src="/assets/templates/media/local-logo.svg">`,
		// The grandchild nested under the local child: its own descriptor (its
		// `.item.parent` is the enclosing local child's fields), its own `.raw`,
		// its own empty `.data`, the deck/slide entries, and the primary media
		// URL — one level deeper.
		`<span class="local-grand" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="grand" data-template="localgrand" data-parent="yes" data-parent-label="Local child">` +
			`<span class="local-grand-name">Grandchild</span>` +
			`<span class="local-grand-raw-name">Grandchild</span>` +
			`<span class="local-grand-deck">Helper Child Deck</span>` +
			`<span class="local-grand-slide">1</span>` +
			`<span class="local-grand-data-keys"></span>` +
			`<img class="local-grand-media" src="/assets/templates/media/local-grand.svg"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}

	// The helper call contributes to its TARGET only: the caller's `.data` stays
	// empty (it authored no top-level sections), so neither the target name nor
	// any child leaked into it.
	if !strings.Contains(page, `<span class="slide-data-keys"></span>`) {
		t.Errorf("helper call leaked into the caller's .data, want an empty slide-data-keys:\n%s", page)
	}
	if strings.Contains(page, "childwrapper;") || strings.Contains(page, "externalchild;") || strings.Contains(page, "localchild;") {
		t.Errorf("a helper target or child leaked into the caller's .data:\n%s", page)
	}
}

// helperChild slide/section manifests, layouts and examples. Every layout is a
// single line so the rendered page carries no incidental whitespace between the
// spans the assertions match, and every optional context read is guarded so the
// same layout executes at load-time step 7 (where a schema-free or empty example
// supplies no children) as at render time.
const (
	helperChildSlideLayout = `<section class="helper-child-slide">` +
		`<h1 class="slide-title">{{.deck.title}}</h1>` +
		`<span class="slide-number">{{.slide.number}}</span>` +
		`<span class="slide-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`{{ section "childwrapper" (dict "heading" "Wrapper H") nil ` +
		`(child "external" (dict "label" "Ext child") "Ext child body") ` +
		`(child "local" (dict "label" "Local child") "Local child body" (child "grand" (dict "name" "Grandchild"))) }}` +
		`</section>`

	helperChildWrapperManifest = "name: childwrapper\n" +
		"fields:\n" +
		"  - name: heading\n" +
		"    type: text\n" +
		"sections:\n" +
		"  - name: external\n" +
		"    accepted:\n" +
		"      - externalchild\n" +
		"    max: 4\n" +
		"  - name: local\n" +
		"    accepted:\n" +
		"      - localchild\n" +
		"    max: 4\n" +
		"body:\n" +
		"  mode: optional\n"

	helperChildWrapperLayout = `<div class="child-wrapper" ` +
		`data-external="{{if .external}}{{len .external}}{{else}}0{{end}}" ` +
		`data-local="{{if .local}}{{len .local}}{{else}}0{{end}}" ` +
		`data-external-data="{{if .data.external}}{{len .data.external}}{{else}}0{{end}}" ` +
		`data-local-data="{{if .data.local}}{{len .data.local}}{{else}}0{{end}}">` +
		`<span class="wrapper-heading">{{.heading}}</span>` +
		`{{range .external}}{{.}}{{end}}{{range .local}}{{.}}{{end}}</div>`

	helperChildLocalManifest = "name: localchild\n" +
		"fields:\n" +
		"  - name: label\n" +
		"    type: text\n" +
		"    required: true\n" +
		"sections:\n" +
		"  - name: grand\n" +
		"    accepted:\n" +
		"      - localgrand\n" +
		"    max: 4\n" +
		"body:\n" +
		"  mode: optional\n"

	helperChildLocalLayout = `<span class="local-child" ` +
		`data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" ` +
		`data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" ` +
		`data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" ` +
		`data-parent-heading="{{if .item.parent}}{{.item.parent.heading}}{{end}}">` +
		`<span class="local-child-label">{{.label}}</span>` +
		`<span class="local-child-raw-label">{{if .raw.label}}{{.raw.label}}{{else}}absent{{end}}</span>` +
		`<span class="local-child-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="local-child-deck">{{.deck.title}}</span>` +
		`<span class="local-child-slide">{{.slide.number}}</span>` +
		`<span class="local-child-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`<span class="local-child-grand-count">{{if .data.grand}}{{len .data.grand}}{{else}}0{{end}}</span>` +
		`<img class="local-child-media" src="{{ media "local-logo.svg" }}">` +
		`{{range .grand}}{{.}}{{end}}</span>`

	helperChildLocalGrandManifest = "name: localgrand\n" +
		"fields:\n" +
		"  - name: name\n" +
		"    type: text\n" +
		"    required: true\n" +
		"body:\n" +
		"  mode: disallowed\n"

	helperChildLocalGrandLayout = `<span class="local-grand" ` +
		`data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" ` +
		`data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" ` +
		`data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" ` +
		`data-parent-label="{{if .item.parent}}{{.item.parent.label}}{{end}}">` +
		`<span class="local-grand-name">{{.name}}</span>` +
		`<span class="local-grand-raw-name">{{if .raw.name}}{{.raw.name}}{{else}}absent{{end}}</span>` +
		`<span class="local-grand-deck">{{.deck.title}}</span>` +
		`<span class="local-grand-slide">{{.slide.number}}</span>` +
		`<span class="local-grand-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`<img class="local-grand-media" src="{{ media "local-grand.svg" }}"></span>`

	helperChildExternalManifest = "name: externalchild\n" +
		"fields:\n" +
		"  - name: label\n" +
		"    type: text\n" +
		"    required: true\n" +
		"body:\n" +
		"  mode: optional\n"

	helperChildExternalLayout = `<span class="external-child" ` +
		`data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" ` +
		`data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" ` +
		`data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" ` +
		`data-parent-heading="{{if .item.parent}}{{.item.parent.heading}}{{end}}">` +
		`<span class="external-child-label">{{.label}}</span>` +
		`<span class="external-child-raw-label">{{if .raw.label}}{{.raw.label}}{{else}}absent{{end}}</span>` +
		`<span class="external-child-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="external-child-deck">{{.deck.title}}</span>` +
		`<span class="external-child-slide">{{.slide.number}}</span>` +
		`<span class="external-child-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`<img class="external-child-media" src="{{ media "ext-logo.svg" }}"></span>`
)

// writeHelperChildLayeredDeck writes a real two-layer deck: the deck-root local
// layer carries the calling slide and the local target/child/grandchild
// templates plus their media and the deck theme, and a fresh external root
// carries the external child template and its external-only media. Every media
// file lives in exactly one layer, so a media URL reveals which layer's
// filesystem resolved it. It returns (deckRoot, extRoot) — the arguments
// LoadDeckLibrary takes — for renderLayeredDeckPipeline.
func writeHelperChildLayeredDeck(t *testing.T) (deckRoot, extRoot string) {
	t.Helper()

	deckRoot = t.TempDir()
	extRoot = t.TempDir()
	localRoot := filepath.Join(deckRoot, template.TemplatesDir)

	writeLayerManifest(t, localRoot, "local-child-lib")
	writeLayerTheme(t, localRoot, "plain", "body { margin: 0; }\n")
	writeLayerMedia(t, localRoot, "local-logo.svg", "<svg/>")
	writeLayerMedia(t, localRoot, "local-grand.svg", "<svg/>")
	writeHelperChildTemplate(t, localRoot, template.SlidesDir, "sectioncontextchildprobe",
		"name: sectioncontextchildprobe\n", helperChildSlideLayout, "# probe\n")
	writeHelperChildTemplate(t, localRoot, template.SectionsDir, "childwrapper",
		helperChildWrapperManifest, helperChildWrapperLayout, "")
	writeHelperChildTemplate(t, localRoot, template.SectionsDir, "localchild",
		helperChildLocalManifest, helperChildLocalLayout, "```\nlabel: Example\n```\n")
	writeHelperChildTemplate(t, localRoot, template.SectionsDir, "localgrand",
		helperChildLocalGrandManifest, helperChildLocalGrandLayout, "```\nname: N\n```\n")

	writeLayerManifest(t, extRoot, "ext-child-lib")
	writeLayerMedia(t, extRoot, "ext-logo.svg", "<svg/>")
	writeHelperChildTemplate(t, extRoot, template.SectionsDir, "externalchild",
		helperChildExternalManifest, helperChildExternalLayout, "```\nlabel: E\n```\n")

	writeLayerFile(t, filepath.Join(deckRoot, deck.ConfigFile),
		"title: Helper Child Deck\ntheme: plain\ntemplates: \""+extRoot+"\"\n")
	writeLayerFile(t, filepath.Join(deckRoot, deck.SlidesDir, "1-child.md"),
		"---\ntemplate: sectioncontextchildprobe\n---\n")
	return deckRoot, extRoot
}

// writeHelperChildTemplate writes one template's manifest, layout and example
// under root/<kindDir>/<name>/, for the layered helper-child fixture. The
// example may be empty: a template declaring sections but no instances leaves
// every declaration at its min of zero, which the loader accepts.
func writeHelperChildTemplate(t *testing.T, root, kindDir, name, manifest, layout, example string) {
	t.Helper()
	base := filepath.Join(root, kindDir, name)
	writeLayerFile(t, filepath.Join(base, template.ManifestFile), manifest)
	writeLayerFile(t, filepath.Join(base, template.LayoutFile), layout)
	writeLayerFile(t, filepath.Join(base, template.ExampleFile), example)
}
