package render

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/assets"
	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// TestRenderDeckPipeline is the whole-deck integration test: a real deck
// directory on disk (with its own copy of the phase-3 fixture templates/
// library) is loaded by internal/deck (LoadConfig, LoadSlides), a project
// theme registry is built by theme.LoadDir over templates/themes, checked by
// internal/validate.Validate against the library-built registry, and
// rendered by RenderDeck into the complete offline page. It reads the real
// filesystem, so it is skipped under -short.
//
// It asserts the full page HTML the pipeline produces: the .reveal >
// .slides shell, the anchor ids taken from the slide labels, the
// navigationMode from kalide.yaml, the registry-resolved theme stylesheet
// served from templates/themes/<name>, the served templates/media URL a
// layout's `media` call resolves to, and that every /assets/ path the page
// references — reveal.js's offline asset set — actually resolves in the
// embedded internal/assets FS.
func TestRenderDeckPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writePipelineDeck(t)
	fsys := os.DirFS(dir)

	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}
	funcMap := template.LayoutFuncMap(lib.Media, "/assets/templates/media")

	// Deck loader: kalide.yaml then slides/, in the validator's fail-fast order.
	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themeReg)
	if err != nil {
		t.Fatalf("deck.LoadConfig: %v", err)
	}
	if cfg.Title != "Integration Deck" {
		t.Errorf("cfg.Title = %q, want %q", cfg.Title, "Integration Deck")
	}
	if cfg.Navigation != deck.NavigationGrid {
		t.Errorf("cfg.Navigation = %q, want %q", cfg.Navigation, deck.NavigationGrid)
	}

	d, err := deck.LoadSlides(fsys, deck.SlidesDir)
	if err != nil {
		t.Fatalf("deck.LoadSlides: %v", err)
	}
	if len(d.Stacks) != 1 {
		t.Fatalf("deck has %d horizontal stacks, want 1", len(d.Stacks))
	}
	if got := d.Stacks[0].Slide.Label; got != "hello" {
		t.Errorf("first stack label = %q, want %q", got, "hello")
	}

	// Validator: the same real directory, through the library-built registry.
	if verr, invalid := validate.Validate(fsys, reg, themeReg); invalid {
		t.Fatalf("validate.Validate rejected the deck: %s", validate.Format(verr))
	}

	parsed := parsePipelineSlides(t, fsys, d, reg)

	pageHTML, err := RenderDeck(cfg, d, parsed, reg, themeReg, funcMap)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	page := string(pageHTML)

	if !strings.Contains(page, "<!DOCTYPE html>") {
		t.Errorf("page does not carry the HTML5 doctype:\n%s", page)
	}
	if !strings.Contains(page, `<html lang="en">`) {
		t.Errorf("page does not carry the html element:\n%s", page)
	}
	if !strings.Contains(page, "<title>Integration Deck</title>") {
		t.Errorf("page does not carry the deck title:\n%s", page)
	}
	if !strings.Contains(page, `<div class="reveal">`) || !strings.Contains(page, `<div class="slides">`) {
		t.Errorf("page is missing the .reveal > .slides container:\n%s", page)
	}
	if !strings.Contains(page, "hash: true") {
		t.Errorf("Reveal.initialize does not enable hash anchors:\n%s", page)
	}
	if !strings.Contains(page, `id="hello"`) {
		t.Errorf("page is missing anchor id %q:\n%s", "hello", page)
	}

	// Rendered slide content from the fixture hello layout.
	if !strings.Contains(page, `<h1>Integration Deck</h1>`) {
		t.Errorf("page is missing the rendered title heading:\n%s", page)
	}

	// The layout's `media "logo.svg"` call resolves to the served
	// templates/media URL.
	if !strings.Contains(page, `src="/assets/templates/media/logo.svg"`) {
		t.Errorf("page does not reference the served templates/media logo URL:\n%s", page)
	}

	// navigationMode is passed straight through from kalide.yaml.
	if !strings.Contains(page, `navigationMode: "grid"`) {
		t.Errorf("page does not carry navigation grid:\n%s", page)
	}

	if strings.Contains(strings.ToLower(page), "eypres") {
		t.Errorf("RenderDeck output contains 'eypres':\n%s", page)
	}

	// Assert RenderDeck empty-title fallback is kalide and output has no 'eypres'.
	cfgEmpty := *cfg
	cfgEmpty.Title = ""
	emptyHTML, err := RenderDeck(&cfgEmpty, d, parsed, reg, themeReg, funcMap)
	if err != nil {
		t.Fatalf("RenderDeck with empty title: %v", err)
	}
	emptyPage := string(emptyHTML)
	if !strings.Contains(emptyPage, "<title>kalide</title>") {
		t.Errorf("RenderDeck empty-title fallback is not <title>kalide</title>:\n%s", emptyPage)
	}
	if strings.Contains(strings.ToLower(emptyPage), "eypres") {
		t.Errorf("RenderDeck empty-title output contains 'eypres':\n%s", emptyPage)
	}

	// The project theme resolves through the theme registry to its served
	// stylesheet URL.
	if !strings.Contains(page, `<link rel="stylesheet" href="/assets/templates/themes/plain/theme.css">`) {
		t.Errorf("page does not link the resolved theme stylesheet:\n%s", page)
	}

	// Every /assets/reveal/... URL the page references resolves in the
	// embedded FS, and the page asset + theme stylesheet refs resolve via
	// the served templates/ paths on disk.
	assertPageAssetRefsResolve(t, page)
	assertServedTemplatesRefsResolve(t, dir, page)
}

// The nested-pipeline probe library is a two-level section set added to the
// fixture library copy of a temporary deck: a slide template declaring a
// `blocks` section accepting `pipelineblock`, whose layout splices its child
// `leaves` (accepting `pipelineleaf`) as trusted HTML. Every layout is a single
// compact line, so the page carries an exactly assertable fragment proving the
// rendered section markup — and its deepest-first nesting — rather than section
// data maps (template-language, item-context).
const (
	nestedPipelineSlideTemplateYAML = `description: slide with a nested block/leaf section
fields:
  - name: title
    type: text
    required: true
sections:
  - name: blocks
    accepted:
      - pipelineblock
    max: 4
body:
  mode: optional
`

	nestedPipelineSlideLayout = `<section class="pipeline-slide"><h1>{{ .title }}</h1>{{ range .blocks }}{{ . }}{{ end }}</section>`

	nestedPipelineSlideExample = `---
template: pipelinesections
title: Example
---

# blocks
` + "```\ntemplate: pipelineblock\ntitle: Outer\n```" + `
## leaves
` + "```\ntemplate: pipelineleaf\nname: Inner\n```" + `
`

	nestedPipelineBlockTemplateYAML = `description: outer pipeline section wrapping a leaf child
fields:
  - name: title
    type: text
sections:
  - name: leaves
    accepted:
      - pipelineleaf
    max: 4
body:
  mode: disallowed
`

	nestedPipelineBlockLayout = `<div class="pipeline-block" data-block-title="{{ .title }}">{{ range .leaves }}{{ . }}{{ end }}</div>`

	nestedPipelineBlockExample = "```\ntitle: Outer\n```\n# leaves\n```\ntemplate: pipelineleaf\nname: Inner\n```\n"

	nestedPipelineLeafTemplateYAML = `description: inner pipeline section
fields:
  - name: name
    type: text
    required: true
body:
  mode: disallowed
`

	nestedPipelineLeafLayout = `<span class="pipeline-leaf">{{ .name }}</span>`

	nestedPipelineLeafExample = "```\nname: Inner\n```\n"
)

// nestedPipelineDeckFiles is the real deck written for the nested-section
// pipeline test: kalide.yaml plus one slide whose `blocks` section is a
// `pipelineblock` that holds one nested `pipelineleaf` child.
var nestedPipelineDeckFiles = map[string]string{
	"kalide.yaml": "title: Nested Deck\n" +
		"theme: plain\n",
	"slides/1-nested.md": `---
template: pipelinesections
title: Nested
---

# blocks
` + "```\ntemplate: pipelineblock\ntitle: Outer\n```" + `
## leaves
` + "```\ntemplate: pipelineleaf\nname: Inner\n```" + `
`,
}

// TestRenderDeckNestedSections is the whole-deck integration test for nested
// sections: a real deck directory on disk (with its own copy of the fixture
// templates/ library plus the nested-pipeline probe templates) is loaded,
// validated and rendered by the same pipeline as TestRenderDeckPipeline. It
// asserts that the page carries RENDERED section markup — the deepest child
// leaf's HTML spliced INSIDE its parent block's wrapper, which is itself
// inside the slide's wrapper — rather than section data maps, i.e. the
// bottom-up, deepest-first composition (template-language, item-context). It
// reads the real filesystem, so it is skipped under -short.
func TestRenderDeckNestedSections(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writeNestedPipelineDeck(t)
	fsys := os.DirFS(dir)

	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}
	funcMap := template.LayoutFuncMap(lib.Media, "/assets/templates/media")

	if verr, invalid := validate.Validate(fsys, reg, themeReg); invalid {
		t.Fatalf("validate.Validate rejected the deck: %s", validate.Format(verr))
	}

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

	// The deepest section renders first, then its markup is handed to the
	// parent as one trusted-HTML list element and spliced inside the block's
	// own wrapper, which is itself inside the slide's wrapper: the whole
	// fragment must appear verbatim, deepest-first.
	wantSlide := `<section id="nested" class="pipeline-slide"><h1>Nested</h1>` +
		`<div class="pipeline-block" data-block-title="Outer"><span class="pipeline-leaf">Inner</span></div>` +
		`</section>`
	if !strings.Contains(page, wantSlide) {
		t.Errorf("page does not contain the nested rendered section markup:\nwant: %s\npage:\n%s", wantSlide, page)
	}

	// The page carries the sections' rendered HTML, never their execution
	// data maps (which would print as `map[...`).
	if strings.Contains(page, "map[") {
		t.Errorf("page carries a section data map rather than rendered section markup:\n%s", page)
	}

	// plan.phase-07.task-20 (bounded-self-nesting, item-context): a real deck
	// whose library carries ONE section template (`bstloop`) that declares a
	// self-accepting `loop` section bounded at `max_depth: 1`, plus a slide
	// template (`bstevents`) whose `events` section accepts it. The slide
	// authors `# events` (resolving `bstloop`) with two nested `## loop`
	// children, both resolving `bstloop` again: the events instance is the
	// template's outermost level (`.item.depth` 0) and each nested `loop`
	// instance has exactly one enclosing same-template ancestor, so
	// `.item.depth` 1. The whole bounded tree renders end to end, and each
	// instance reads its own `.item` — `.item.section` (`events`/`loop`),
	// `.item.index`/`.item.count` its real position among its `loop` siblings
	// and `.item.parent.name` the enclosing events instance's fields.
	t.Run("bounded self-nesting renders each level's item context", func(t *testing.T) {
		dir := writeBoundedSelfNestingDeck(t)
		fsys := os.DirFS(dir)

		lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
		if err != nil {
			t.Fatalf("template.LoadLibrary: %v", err)
		}
		reg, err := template.NewRegistryFromLibrary(lib)
		if err != nil {
			t.Fatalf("template.NewRegistryFromLibrary: %v", err)
		}
		themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
		if err != nil {
			t.Fatalf("theme.LoadDir: %v", err)
		}
		funcMap := template.LayoutFuncMap(lib.Media, "/assets/templates/media")

		if verr, invalid := validate.Validate(fsys, reg, themeReg); invalid {
			t.Fatalf("validate.Validate rejected the deck: %s", validate.Format(verr))
		}

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

		// The whole bounded self-nested tree, deepest-first: the events
		// instance (depth 0, no section parent) wrapping its two loop
		// children (depth 1 each, sibling positions 0/1 of 2), each reading
		// `.item.parent.name` from the enclosing events instance's fields.
		wantTree := `<div class="bst-loop" data-name="Events" data-depth="0" data-section="events" data-template="bstloop" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-parent="absent" data-parent-name="">` +
			`<div class="bst-loop" data-name="First" data-depth="1" data-section="loop" data-template="bstloop" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-parent="present" data-parent-name="Events"></div>` +
			`<div class="bst-loop" data-name="Second" data-depth="1" data-section="loop" data-template="bstloop" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-parent="present" data-parent-name="Events"></div>` +
			`</div>`
		if !strings.Contains(page, wantTree) {
			t.Errorf("page does not contain the bounded self-nested tree:\nwant: %s\npage:\n%s", wantTree, page)
		}

		// As above, the page carries rendered section markup, never section
		// data maps.
		if strings.Contains(page, "map[") {
			t.Errorf("page carries a section data map rather than rendered section markup:\n%s", page)
		}
	})

	// plan.phase-07.task-21 (bounded-self-nesting,
	// requirements.note.self-nesting-depth): a real deck nesting the
	// two-template A→B→A→B chain — section template a's `b` declaration
	// accepts b bounded at max_depth: 1, section template b's `a` declaration
	// accepts a with no max_depth — is parsed and rendered, and EVERY
	// instance's render-time `.item.depth` equals the parse-time count of its
	// enclosing same-template ancestors computed INDEPENDENTLY here from the
	// parsed Section tree (counting same-template ancestors along the tree,
	// not hard-coded only). The second a and the second b are each depth 1
	// (their one enclosing same-template ancestor); the first a and first b are
	// depth 0. The 4-level deck is fully accepted, so the refuse case needs its
	// OWN deck: adding `##### a` › `###### b` makes a third b instance with two
	// enclosing b ancestors (depth 2), which the parser refuses as
	// `section "b": nesting deeper than max_depth 1`, path-qualified to the
	// slide file with its containment chain. Together the two halves pin
	// slide.checkNestedDepth's count and the render side's
	// (template.InstanceDepth) value so they cannot drift.
	t.Run("parse-time and render-time depth agree on the a/b cycle", func(t *testing.T) {
		dir := writeAbCycleDeck(t, abCycleDeckFiles)
		fsys := os.DirFS(dir)

		lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
		if err != nil {
			t.Fatalf("template.LoadLibrary: %v", err)
		}
		reg, err := template.NewRegistryFromLibrary(lib)
		if err != nil {
			t.Fatalf("template.NewRegistryFromLibrary: %v", err)
		}
		themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
		if err != nil {
			t.Fatalf("theme.LoadDir: %v", err)
		}
		funcMap := template.LayoutFuncMap(lib.Media, "/assets/templates/media")

		if verr, invalid := validate.Validate(fsys, reg, themeReg); invalid {
			t.Fatalf("validate.Validate rejected the a/b cycle deck: %s", validate.Format(verr))
		}

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
			t.Fatalf("parsed %d slides, want 1 (the a/b cycle slide)", len(parsed))
		}

		pageHTML, err := RenderDeck(cfg, d, parsed, reg, themeReg, funcMap)
		if err != nil {
			t.Fatalf("RenderDeck: %v", err)
		}
		page := string(pageHTML)

		// Independent parse-time depth: walk the parsed Section tree in
		// pre-order and count each instance's enclosing same-template ancestors
		// (ancestors whose resolved template equals its own). This never calls
		// slide.checkNestedDepth or template.InstanceDepth — it re-derives the
		// depth rule from the tree — so it is an independent witness for both
		// halves. The documented sequence for `# a` › `## b` › `### a` ›
		// `#### b` is first a 0, first b 0, second a 1, second b 1.
		type abSample struct {
			template string
			depth    int
		}
		wantDepths := []abSample{{"a", 0}, {"b", 0}, {"a", 1}, {"b", 1}}
		var parseDepths []abSample
		var walk func(secs []slide.Section, ancestors []string)
		walk = func(secs []slide.Section, ancestors []string) {
			for i := range secs {
				sec := secs[i]
				depth := 0
				for _, ancestor := range ancestors {
					if ancestor == sec.Template {
						depth++
					}
				}
				parseDepths = append(parseDepths, abSample{sec.Template, depth})
				childAncestors := append(append([]string{}, ancestors...), sec.Template)
				walk(sec.Children, childAncestors)
			}
		}
		walk(parsed[0].Sections, nil)

		if len(parseDepths) != len(wantDepths) {
			t.Fatalf("parsed %d instances, want %d (a › b › a › b): %+v", len(parseDepths), len(wantDepths), parseDepths)
		}
		for i, want := range wantDepths {
			if parseDepths[i] != want {
				t.Errorf("parse-time instance %d = %+v, want %+v (the a/b cycle's documented per-template depths)", i, parseDepths[i], want)
			}
		}

		// Render-time depth: each instance's layout prints its own resolved
		// template and `.item.depth`, so the page carries the instances in
		// document (pre-)order. The printed sequence must equal the parse-time
		// sequence instance for instance.
		var renderDepths []abSample
		for _, m := range abDepthSampleRE.FindAllStringSubmatch(page, -1) {
			depth, err := strconv.Atoi(m[2])
			if err != nil {
				t.Fatalf("render-time depth %q is not an integer: %v", m[2], err)
			}
			renderDepths = append(renderDepths, abSample{m[1], depth})
		}
		if len(renderDepths) != len(parseDepths) {
			t.Fatalf("page printed %d instance depths, want %d (a › b › a › b):\n%s", len(renderDepths), len(parseDepths), page)
		}
		for i := range parseDepths {
			if renderDepths[i] != parseDepths[i] {
				t.Errorf("instance %d render-time (template, depth) = %+v, want the parse-time %+v: slide.checkNestedDepth and the render side's template.InstanceDepth drifted", i, renderDepths[i], parseDepths[i])
			}
		}

		// The 4-level deck is fully accepted, so the refusal needs its own
		// deck: `##### a` › `###### b` makes a third b instance whose enclosing
		// chain holds two b ancestors (depth 2), past a's `b` declaration's
		// max_depth: 1. The parser refuses it path-qualified to the slide file
		// with the containment chain, while accepting the second b at the
		// bound in the deck above.
		overflowDir := writeAbCycleDeck(t, abCycleOverflowDeckFiles)
		const overflowName = "slides/1-overflow.md"
		src, err := fs.ReadFile(os.DirFS(overflowDir), overflowName)
		if err != nil {
			t.Fatalf("read %s: %v", overflowName, err)
		}
		_, perr := slide.Parse(overflowName, src, reg)
		if perr == nil {
			t.Fatal("slide.Parse accepted a `##### a` › `###### b` continuation past max_depth: 1, want the depth refusal")
		}
		parseErr, ok := perr.(*slide.ParseError)
		if !ok {
			t.Fatalf("slide.Parse error = %T (%v), want *slide.ParseError", perr, perr)
		}
		const wantChain = "a[0] › b[0] › a[0] › b[0] › a[0] › b[0]"
		if parseErr.File != overflowName {
			t.Errorf("refusal File = %q, want %q", parseErr.File, overflowName)
		}
		if parseErr.Line != 25 {
			t.Errorf("refusal Line = %d, want 25 (the `###### b` heading)", parseErr.Line)
		}
		if parseErr.Path != wantChain {
			t.Errorf("refusal Path = %q, want the containment chain %q", parseErr.Path, wantChain)
		}
		if want := `section "b": nesting deeper than max_depth 1`; parseErr.Msg != want {
			t.Errorf("refusal Msg = %q, want %q", parseErr.Msg, want)
		}
		if !strings.Contains(parseErr.Error(), wantChain) {
			t.Errorf("refusal Error() = %q, want the containment chain %q rendered in it", parseErr.Error(), wantChain)
		}
	})
}

// writeNestedPipelineDeck writes nestedPipelineDeckFiles to a fresh
// t.TempDir(), copies the fixture templates/ library, and writes the
// nested-pipeline probe templates (pipelinesections, pipelineblock,
// pipelineleaf) into that copied library, returning the directory.
func writeNestedPipelineDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range nestedPipelineDeckFiles {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	copyTemplatesDir(t, filepath.Join(fixtureLibraryDir, "templates"), filepath.Join(dir, "templates"))
	writeProbeTemplate(t, dir, "slides", "pipelinesections", nestedPipelineSlideTemplateYAML, nestedPipelineSlideLayout, nestedPipelineSlideExample)
	writeProbeTemplate(t, dir, "sections", "pipelineblock", nestedPipelineBlockTemplateYAML, nestedPipelineBlockLayout, nestedPipelineBlockExample)
	writeProbeTemplate(t, dir, "sections", "pipelineleaf", nestedPipelineLeafTemplateYAML, nestedPipelineLeafLayout, nestedPipelineLeafExample)
	return dir
}

// The bounded-self-nesting probe library has ONE section template (`bstloop`)
// declaring a self-accepting `loop` section bounded at `max_depth: 1`, plus a
// slide template (`bstevents`) whose `events` section accepts it. The slide
// authors `# events` (an instance of `bstloop`) holding two nested `## loop`
// instances (also `bstloop`), so the events instance is the template's
// outermost level and each loop instance is its first self-nested level — the
// one-template case of bounded-self-nesting's "one template that declares
// `loop` accepting itself with max_depth: 1, rendering a top `events` instance
// holding `loop` children that hold no further `loop` children". Every layout
// is a single compact line so the page carries an exactly assertable fragment
// of rendered section markup (not data maps), and each instance prints its own
// reserved `.item` descriptor.
const (
	boundedSelfNestingSlideTemplateYAML = `description: slide hosting a bounded self-nesting events tree
sections:
  - name: events
    accepted:
      - bstloop
body:
  mode: optional
`

	boundedSelfNestingSlideLayout = `<section class="bst-slide">{{range .events}}{{.}}{{end}}</section>`

	boundedSelfNestingSlideExample = "---\ntemplate: bstevents\n---\n"

	boundedSelfNestingLoopTemplateYAML = `description: bounded self-nesting loop section
fields:
  - name: name
    type: text
    required: true
sections:
  - name: loop
    accepted:
      - bstloop
    max_depth: 1
body:
  mode: optional
`

	// The loop layout prints its own `.item` at each level: depth, section,
	// resolved template, sibling index/number/count/first/last, and the
	// parent instance's `name` field when there is one, then recurses into its
	// authored `loop` children.
	boundedSelfNestingLoopLayout = `<div class="bst-loop" data-name="{{.name}}" data-depth="{{.item.depth}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-parent="{{if .item.parent}}present{{else}}absent{{end}}" data-parent-name="{{if .item.parent}}{{.item.parent.name}}{{end}}">{{range .loop}}{{.}}{{end}}</div>`

	// The loop template's example authors the one nested `loop` instance its
	// declaration accepts (max_depth: 1), so step 7 validates the bounded
	// instance too.
	boundedSelfNestingLoopExample = "```\nname: Example\n```\n# loop\n```\nname: Inner\n```\n"
)

// boundedSelfNestingDeckFiles is the real deck written for the bounded
// self-nesting pipeline test: kalide.yaml plus one slide using `bstevents`
// whose `# events` section resolves `bstloop` and holds two `## loop`
// children, both resolving `bstloop` (the nested level bounded by max_depth 1).
var boundedSelfNestingDeckFiles = map[string]string{
	"kalide.yaml": "title: Bounded Self-Nesting Deck\n" +
		"theme: plain\n",
	"slides/1-events.md": `---
template: bstevents
---

# events
` + "```\nname: Events\n```" + `
## loop
` + "```\nname: First\n```" + `
## loop
` + "```\nname: Second\n```" + `
`,
}

// writeBoundedSelfNestingDeck writes boundedSelfNestingDeckFiles to a fresh
// t.TempDir(), copies the fixture templates/ library, and writes the
// bounded-self-nesting probe templates (bstevents, bstloop) into that copied
// library, returning the directory.
func writeBoundedSelfNestingDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range boundedSelfNestingDeckFiles {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	copyTemplatesDir(t, filepath.Join(fixtureLibraryDir, "templates"), filepath.Join(dir, "templates"))
	writeProbeTemplate(t, dir, "slides", "bstevents", boundedSelfNestingSlideTemplateYAML, boundedSelfNestingSlideLayout, boundedSelfNestingSlideExample)
	writeProbeTemplate(t, dir, "sections", "bstloop", boundedSelfNestingLoopTemplateYAML, boundedSelfNestingLoopLayout, boundedSelfNestingLoopExample)
	return dir
}

// The a/b-cycle probe library is the two-template declared-section cycle of
// requirements.requirement.bounded-self-nesting: section template `a` declares
// a `b` section accepting template `b` with max_depth: 1 (the one declaration
// that bounds the cycle), and section template `b` declares an `a` section
// accepting template `a` with no max_depth (so it bounds nothing; the a
// instances are limited only by a's `b` bound). The slide template `abhost` has
// a top section slot `a` accepting template `a`, so a deck can author `# a`
// (an instance of template a) › `## b` › `### a` › `#### b` at four heading
// levels. Every layout is a single compact line printing its own reserved
// `.item.template` and `.item.depth`, so the page carries one exactly
// assertable fragment per instance and the render-time depths can be compared
// with the independently computed parse-time depths instance for instance.
const (
	abCycleHostTemplateYAML = `description: slide hosting the a/b bounded cycle
sections:
  - name: a
    accepted:
      - a
body:
  mode: optional
`

	abCycleHostLayout = `<section class="ab-slide">{{range .a}}{{.}}{{end}}</section>`

	abCycleHostExample = "---\ntemplate: abhost\n---\n"

	abCycleATemplateYAML = `description: a/b cycle template a, bounding its b section at depth 1
fields:
  - name: name
    type: text
sections:
  - name: b
    accepted:
      - b
    max_depth: 1
body:
  mode: disallowed
`

	abCycleALayout = `<div class="ab-a" data-name="{{.name}}" data-template="{{.item.template}}" data-depth="{{.item.depth}}">{{range .b}}{{.}}{{end}}</div>`

	// Template a's example authors the one `b` instance its declaration
	// accepts at depth 0, so load-time example validation exercises the bound.
	abCycleAExample = "```\nname: A0\n```\n# b\n```\nname: B0\n```\n"

	abCycleBTemplateYAML = `description: a/b cycle template b, accepting a unbounded
fields:
  - name: name
    type: text
sections:
  - name: a
    accepted:
      - a
body:
  mode: disallowed
`

	abCycleBLayout = `<div class="ab-b" data-name="{{.name}}" data-template="{{.item.template}}" data-depth="{{.item.depth}}">{{range .a}}{{.}}{{end}}</div>`

	abCycleBExample = "```\nname: B0\n```\n# a\n```\nname: A0\n```\n"
)

// abDepthSampleRE captures each rendered a/b-cycle instance's resolved template
// and printed self-nesting depth (`data-template` immediately followed by
// `data-depth`, both emitted by the a and b layouts). FindAllStringSubmatch
// returns the instances in document (pre-)order, matching the parse-time walk.
var abDepthSampleRE = regexp.MustCompile(`data-template="([^"]*)"\s+data-depth="(\d+)"`)

// abCycleDeckFiles is the accepted a/b-cycle deck: `# a` (template a) › `## b`
// (template b) › `### a` (template a) › `#### b` (template b). The second b,
// with one enclosing b ancestor (self-nesting depth 1), sits exactly at a's
// `b` declaration's max_depth: 1 and is accepted.
var abCycleDeckFiles = map[string]string{
	"kalide.yaml": "title: A/B Cycle Deck\n" +
		"theme: plain\n",
	"slides/1-cycle.md": `---
template: abhost
---

# a
` + "```\nname: A1\n```" + `
## b
` + "```\nname: B1\n```" + `
### a
` + "```\nname: A2\n```" + `
#### b
` + "```\nname: B2\n```" + `
`,
}

// abCycleOverflowDeckFiles is the a/b-cycle deck that runs past the bound: it
// adds `##### a` › `###### b`, so the third b instance has two enclosing b
// ancestors (self-nesting depth 2) and the parser refuses it with
// `section "b": nesting deeper than max_depth 1`. It is a deck of its own
// because the 4-level deck above is fully accepted and a slide.Parse failure
// cannot share a deck with it.
var abCycleOverflowDeckFiles = map[string]string{
	"kalide.yaml": "title: A/B Cycle Overflow Deck\n" +
		"theme: plain\n",
	"slides/1-overflow.md": `---
template: abhost
---

# a
` + "```\nname: A1\n```" + `
## b
` + "```\nname: B1\n```" + `
### a
` + "```\nname: A2\n```" + `
#### b
` + "```\nname: B2\n```" + `
##### a
` + "```\nname: A3\n```" + `
###### b
` + "```\nname: B3\n```" + `
`,
}

// writeAbCycleDeck writes files to a fresh t.TempDir(), copies the fixture
// templates/ library, and writes the a/b-cycle probe templates (the abhost
// slide template and the a and b section templates) into that copied library,
// returning the directory.
func writeAbCycleDeck(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	copyTemplatesDir(t, filepath.Join(fixtureLibraryDir, "templates"), filepath.Join(dir, "templates"))
	writeProbeTemplate(t, dir, "slides", "abhost", abCycleHostTemplateYAML, abCycleHostLayout, abCycleHostExample)
	writeProbeTemplate(t, dir, "sections", "a", abCycleATemplateYAML, abCycleALayout, abCycleAExample)
	writeProbeTemplate(t, dir, "sections", "b", abCycleBTemplateYAML, abCycleBLayout, abCycleBExample)
	return dir
}

// pipelineDeckFiles is the real deck written to a t.TempDir(): kalide.yaml,
// one slide using the fixture library's hello template, and the fixture
// templates/ library itself copied alongside it.
var pipelineDeckFiles = map[string]string{
	"kalide.yaml": "title: Integration Deck\n" +
		"author: Ada Lovelace\n" +
		"date: 2026-09-25\n" +
		"theme: plain\n" +
		"navigation: grid\n",
	"slides/1-hello.md": `---
template: hello
title: Integration Deck
---
`,
}

// writePipelineDeck writes pipelineDeckFiles to a fresh t.TempDir(), plus a
// copy of the phase-3 fixture templates/ library, and returns the directory.
func writePipelineDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range pipelineDeckFiles {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	copyTemplatesDir(t, filepath.Join(fixtureLibraryDir, "templates"), filepath.Join(dir, "templates"))
	return dir
}

// copyTemplatesDir recursively copies src to dst, both real directories.
func copyTemplatesDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy templates dir %s -> %s: %v", src, dst, err)
	}
}

// parsePipelineSlides parses every slide of d in model order (a horizontal slide
// before its vertical children) from fsys, keyed by the slide's deck path.
func parsePipelineSlides(t *testing.T, fsys fs.FS, d *deck.Deck, reg *template.Registry) []*slide.Slide {
	t.Helper()
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
	return parsed
}

// assetRefRE captures the embedded path of every href/src the page points at
// under the /assets/reveal/ mount.
var assetRefRE = regexp.MustCompile(`(?:href|src)="/assets/reveal/([^"]+)"`)

// assertPageAssetRefsResolve checks that every /assets/reveal/ path
// referenced by the page exists in the embedded reveal.js asset FS, and that
// the offline reveal.js assets the deck page is expected to load are all
// referenced.
func assertPageAssetRefsResolve(t *testing.T, page string) {
	t.Helper()

	if _, err := fs.Stat(assets.DeckPage(), "deck.html.tmpl"); err != nil {
		t.Errorf("embedded deck page does not resolve: %v", err)
	}

	matches := assetRefRE.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		t.Fatalf("page references no /assets/reveal/ paths:\n%s", page)
	}

	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if _, err := fs.Stat(assets.Reveal(), name); err != nil {
			t.Errorf("page references %q, which does not resolve in the embedded reveal FS: %v", name, err)
		}
	}

	for _, name := range []string{
		"dist/reset.css",
		"dist/reveal.css",
		"dist/reveal.js",
		"dist/plugin/notes.js",
	} {
		if !seen[name] {
			t.Errorf("page does not reference expected embedded asset %q", name)
		}
	}
}

// servedTemplatesRefRE captures a /assets/templates/... URL the page
// references (media or theme paths served from the project templates/ tree).
var servedTemplatesRefRE = regexp.MustCompile(`(?:href|src)="/assets/templates/([^"]+)"`)

// assertServedTemplatesRefsResolve checks that every /assets/templates/...
// URL the page references (a page asset or the theme stylesheet) resolves to
// a real file under dir/templates on disk — the tree internal/server's
// mediaHandler serves.
func assertServedTemplatesRefsResolve(t *testing.T, dir, page string) {
	t.Helper()

	matches := servedTemplatesRefRE.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		t.Fatal("page references no /assets/templates/ paths")
	}
	for _, m := range matches {
		p := filepath.Join(dir, "templates", filepath.FromSlash(m[1]))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("page references %q, which does not resolve on disk at %s: %v", m[1], p, err)
		}
	}
}

// TestRenderDeckLayeredSectionMedia is the whole-deck integration test for the
// layered media-URL contract (library-layer-references): a real two-layer deck
// on disk — an external `templates:` library and the deck-root templates/
// layer — is loaded by template.LoadDeckLibrary, validated and rendered by
// RenderDeck, the same library→deck→parse→render pipeline the other
// whole-deck tests drive. It is the RenderDeck (page-level) counterpart of the
// RenderSlide unit test in sectionhelper_test.go and reuses that file's
// layering fixtures (layeredMediaFixture, writeSoleLayerLibrary, the
// writeLayer* writers) rather than rebuilding them.
//
// It pins the full-page outcome of the per-layer binding:
//
//   - a LOCAL slide template's `section` helper invokes an EXTERNAL section,
//     whose `media` reference resolves in the external layer — both the URL
//     (/assets/templates/external/media/…) and the external media filesystem —
//     while the local slide's own `media` reference keeps the primary
//     /assets/templates/media/… URL and the local media filesystem;
//   - the two prefixes never cross (a local file is never addressed as
//     external, and vice versa);
//   - the deck theme resolves from the external layer's own themes/ registry
//     and links the deck-level theme URL;
//   - a deck that only uses local templates, and a deck whose whole library is
//     a single external root, both render with the unchanged, unprefixed
//     /assets/templates/media/… URLs and never emit an external prefix.
//
// It reads real deck and library directories from disk, so it is skipped under
// -short.
func TestRenderDeckLayeredSectionMedia(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	t.Run("a two-layer deck binds an external section's media to the external layer", func(t *testing.T) {
		deckRoot, extRoot := writeLayeredRenderDeck(t)
		lib, page := renderLayeredDeckPipeline(t, deckRoot, extRoot, extRoot)

		// LoadDeckLibrary marks the configured external root first, the
		// deck-root templates/ second.
		if len(lib.Layers) != 2 {
			t.Fatalf("len(Layers) = %d, want 2 (external + local)", len(lib.Layers))
		}
		if lib.Layers[0].Role != template.LayerRoleExternal {
			t.Errorf("Layers[0].Role = %v, want LayerRoleExternal", lib.Layers[0].Role)
		}
		if lib.Layers[1].Role != template.LayerRoleLocal {
			t.Errorf("Layers[1].Role = %v, want LayerRoleLocal", lib.Layers[1].Role)
		}

		// The external section the local slide invokes through the helper
		// renders with its media resolved in the EXTERNAL layer.
		if want := `<div class="ext-section"><img src="/assets/templates/external/media/ext-logo.svg"></div>`; !strings.Contains(page, want) {
			t.Errorf("page does not contain the external section's external-prefixed media:\nwant: %s\npage:\n%s", want, page)
		}
		// The local slide template's own media keeps the primary base.
		if want := `<img class="own" src="/assets/templates/media/local-logo.svg">`; !strings.Contains(page, want) {
			t.Errorf("page does not contain the local slide's primary media URL:\nwant: %s\npage:\n%s", want, page)
		}
		// The prefixes never cross.
		if strings.Contains(page, "/assets/templates/external/media/local-logo.svg") {
			t.Errorf("local media was addressed with the external prefix:\n%s", page)
		}
		if strings.Contains(page, "/assets/templates/media/ext-logo.svg") {
			t.Errorf("external media was addressed with the primary prefix:\n%s", page)
		}
		// The deck theme resolves from the external layer's themes/ registry and
		// links the single/primary theme URL (the per-layer external theme route
		// lands in phase 4).
		if want := `<link rel="stylesheet" href="/assets/templates/themes/plain/theme.css">`; !strings.Contains(page, want) {
			t.Errorf("page does not link the deck theme resolved from the external layer:\nwant: %s\npage:\n%s", want, page)
		}
	})

	t.Run("a local-only deck renders unchanged unprefixed media URLs", func(t *testing.T) {
		deckRoot := writeLocalOnlyRenderDeck(t)
		_, page := renderLayeredDeckPipeline(t, deckRoot, "", filepath.Join(deckRoot, template.TemplatesDir))

		if want := `<img src="/assets/templates/media/solo.svg">`; !strings.Contains(page, want) {
			t.Errorf("local-only deck page does not contain the primary media URL:\nwant: %s\npage:\n%s", want, page)
		}
		if strings.Contains(page, "/assets/templates/external/") {
			t.Errorf("local-only deck page emitted an external prefix:\n%s", page)
		}
	})

	t.Run("an external-only library renders unchanged unprefixed media URLs", func(t *testing.T) {
		deckRoot, extRoot := writeExternalOnlyRenderDeck(t)
		lib, page := renderLayeredDeckPipeline(t, deckRoot, extRoot, extRoot)

		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1 (the external root is the deck's whole library)", len(lib.Layers))
		}
		if lib.Layers[0].Role != template.LayerRoleSole {
			t.Errorf("Layers[0].Role = %v, want LayerRoleSole", lib.Layers[0].Role)
		}
		if want := `<img src="/assets/templates/media/ext-logo.svg">`; !strings.Contains(page, want) {
			t.Errorf("external-only deck page does not contain the primary media URL:\nwant: %s\npage:\n%s", want, page)
		}
		if strings.Contains(page, "/assets/templates/external/") {
			t.Errorf("external-only deck page emitted an external prefix:\n%s", page)
		}
	})
}

// writeLayeredRenderDeck writes a full two-layer deck on real temp dirs: the
// shared layeredMediaFixture layers (an external library carrying the extmedia
// section and ext-logo.svg, a deck-root templates/ layer carrying localslide —
// which resolves its own local-logo.svg and calls extmedia through the
// `section` helper — plus localchild) extended with the deck files and the
// external layer's theme. It returns the deck root and the external root,
// exactly the (deckRoot, configured) arguments LoadDeckLibrary takes.
func writeLayeredRenderDeck(t *testing.T) (deckRoot, extRoot string) {
	t.Helper()
	deckRoot, extRoot = layeredMediaFixture(t)
	// The deck-level theme lives in the external layer, so the theme registry
	// is built from the external root — showing a theme reference resolving in
	// the external layer's own themes/.
	writeLayerTheme(t, extRoot, "plain", "body { margin: 0; }\n")
	writeLayerFile(t, filepath.Join(deckRoot, deck.ConfigFile),
		"title: Layered Deck\ntheme: plain\ntemplates: \""+extRoot+"\"\n")
	writeLayerFile(t, filepath.Join(deckRoot, deck.SlidesDir, "1-local.md"),
		"---\ntemplate: localslide\n---\n")
	return deckRoot, extRoot
}

// writeLocalOnlyRenderDeck writes a full deck whose whole library is the
// deck-root templates/ layer (no `templates:` key): writeSoleLayerLibrary's
// soloslide, which resolves its own solo.svg, plus the deck files and theme.
func writeLocalOnlyRenderDeck(t *testing.T) string {
	t.Helper()
	deckRoot := writeSoleLayerLibrary(t)
	localRoot := filepath.Join(deckRoot, template.TemplatesDir)
	writeLayerTheme(t, localRoot, "plain", "body { margin: 0; }\n")
	writeLayerFile(t, filepath.Join(deckRoot, deck.ConfigFile),
		"title: Solo Deck\ntheme: plain\n")
	writeLayerFile(t, filepath.Join(deckRoot, deck.SlidesDir, "1-solo.md"),
		"---\ntemplate: soloslide\n---\n")
	return deckRoot
}

// writeExternalOnlyRenderDeck writes a full deck whose whole library is a
// single external root configured through `templates:`: the deck root carries
// no templates/ directory, so the external root is the deck's only (sole)
// layer. It returns the deck root and the external root.
func writeExternalOnlyRenderDeck(t *testing.T) (deckRoot, extRoot string) {
	t.Helper()
	extRoot = t.TempDir()
	writeLayerManifest(t, extRoot, "ext-lib")
	writeLayerTemplate(t, extRoot, template.SlidesDir, "extslide",
		`<section class="ext-slide"><img src="{{ media "ext-logo.svg" }}"></section>`)
	writeLayerMedia(t, extRoot, "ext-logo.svg", "<svg/>")
	writeLayerTheme(t, extRoot, "plain", "body { margin: 0; }\n")

	// Deliberately no deck-root templates/: the external root is the library.
	deckRoot = t.TempDir()
	writeLayerFile(t, filepath.Join(deckRoot, deck.ConfigFile),
		"title: External Deck\ntheme: plain\ntemplates: \""+extRoot+"\"\n")
	writeLayerFile(t, filepath.Join(deckRoot, deck.SlidesDir, "1-ext.md"),
		"---\ntemplate: extslide\n---\n")
	return deckRoot, extRoot
}

// writeLayerTheme writes one layer theme's required theme.css under
// root/themes/<name>/.
func writeLayerTheme(t *testing.T, root, name, css string) {
	t.Helper()
	writeLayerFile(t, filepath.Join(root, template.ThemesDir, name, template.ThemeStylesheet), css)
}

// renderLayeredDeckPipeline runs the whole library→deck→parse→render pipeline
// over a layered deck: theme.LoadDir over themeRoot's own themes/, the merged
// library through template.LoadDeckLibrary(deckRoot, configured), the registry
// built from it, validate.Validate, deck.LoadConfig/LoadSlides, slide.Parse and
// RenderDeck (with `media` bound to the primary served base, which RenderSlide
// overrides per layer). It returns the merged library and the rendered page
// HTML, failing the test on any load/validate/render error.
func renderLayeredDeckPipeline(t *testing.T, deckRoot, configured, themeRoot string) (*template.Library, string) {
	t.Helper()
	fsys := os.DirFS(deckRoot)

	themeReg, err := theme.LoadDir(os.DirFS(themeRoot), template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir(%s): %v", themeRoot, err)
	}

	lib, err := template.LoadDeckLibrary(deckRoot, configured)
	if err != nil {
		t.Fatalf("template.LoadDeckLibrary(%s, %s): %v", deckRoot, configured, err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}

	if verr, invalid := validate.Validate(fsys, reg, themeReg); invalid {
		t.Fatalf("validate.Validate rejected the layered deck: %s", validate.Format(verr))
	}

	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themeReg)
	if err != nil {
		t.Fatalf("deck.LoadConfig: %v", err)
	}
	d, err := deck.LoadSlides(fsys, deck.SlidesDir)
	if err != nil {
		t.Fatalf("deck.LoadSlides: %v", err)
	}
	parsed := parsePipelineSlides(t, fsys, d, reg)

	funcMap := template.LayoutFuncMap(lib.Media, mediaURLPrefix)
	pageHTML, err := RenderDeck(cfg, d, parsed, reg, themeReg, funcMap)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	return lib, string(pageHTML)
}
