package render

import (
	"bytes"
	htmltmpl "html/template"
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

// This file is the integration-test deliverable for
// section-helpers/plan.phase-07.task-5: the reserved `.raw`/`.data` context
// that one shared template.RawContext/template.DataContext now feeds both the
// LOAD-TIME example execution (templates-dir-validation step 7,
// checkLibraryExample) and the RENDER-TIME execution (internal/render) agrees
// for the SAME authored content.
//
// Strategy and limitation. checkLibraryExample is unexported in
// internal/template and DISCARDS the bytes it executes (it builds a
// strings.Builder and returns only an error), and internal/template cannot
// import internal/render; there is therefore no way to capture the load-time
// executed HTML from a cross-package test without adding a source seam. So the
// test, which must live in internal/render (the only package that can reach
// both halves):
//
//   - drives the real LOAD-TIME half through template.LoadLibrary, whose step 7
//     executes each example layout — here UNGUARDED — against the reserved
//     `.raw`/`.data` context, so a successful load proves that half supplied the
//     context (and the same shape);
//   - drives the real RENDER-TIME half through the full library→parser→render
//     pipeline (deck.LoadConfig/LoadSlides, slide.Parse, RenderDeck);
//   - builds the load-time-EQUIVALENT context for the identical authored content
//     with the SAME shared builders checkLibraryExample uses
//     (template.RawContext and template.DataContext over a neutral
//     template.ContextNode tree adapted from the example bytes), executes the
//     same layout with it, and requires the result to appear VERBATIM in the
//     render-time page.
//
// The equality is on OBSERVABLE OUTPUT of `.raw`/`.data`/`.item` source values,
// not the discarded load-time bytes: if the render half had derived either view
// from anything other than the shared builders, the fragment would differ. The
// limitation is exactly that checkLibraryExample's own executed bytes are not
// captured; the load-time half is proved to run the identical layout against
// the shared builders, and the render-time half is proved to reproduce their
// output byte for byte.
//
// Only `.raw` (the author's original source values) and `.data` (the authored
// section tree as data, each entry carrying its own `.raw`/`.item`/`.data`) are
// compared: the CONVERTED field values legitimately differ — the render half
// runs text fields through inline Markdown while the load-time example context
// carries the raw string — so the agreement under test is the source view, as
// the CR specifies.

const (
	// agreementSource is the authored slide: it is written BOTH as the deck
	// slide (slides/1-agree.md) and as the slide template's own example.md, so
	// the load-time example execution and the render-time slide execution run
	// the same layout against the same source values. The single-quoted YAML
	// scalars keep the `**`/`*` Markdown markers visible in `.raw`, distinct
	// from the converted values.
	agreementSource = "---\n" +
		"template: agreehost\n" +
		"title: '**Host** title'\n" +
		"---\n" +
		"Host *body* source.\n" +
		"# blocks\n" +
		"```\n" +
		"template: agreeblock\n" +
		"text: '**First** block'\n" +
		"```\n" +
		"First block body source.\n" +
		"## leaves\n" +
		"```\n" +
		"template: agreeleaf\n" +
		"name: '**Ada**'\n" +
		"```\n" +
		"## leaves\n" +
		"```\n" +
		"template: agreeleaf\n" +
		"name: Grace\n" +
		"```\n" +
		"# blocks\n" +
		"```\n" +
		"template: agreeblock\n" +
		"text: Second block\n" +
		"```\n"

	agreementHostTemplateYAML = `description: agreement host slide reading the reserved raw/data context unguarded
fields:
  - name: title
    type: text
    required: true
  - name: subtitle
    type: text
    default: Defaulted
sections:
  - name: blocks
    accepted:
      - agreeblock
    max: 4
body:
  mode: optional
`

	// agreementHostLayout prints ONLY the reserved source views — its own `.raw`
	// and its authored children as `.data` — never a converted field or the
	// rendered section list, so the load-time-equivalent execution (which has no
	// converted values) and the render-time execution produce identical bytes.
	agreementHostLayout = `<div class="agree-host">` +
		`<span class="host-raw-title">{{ .raw.title }}</span>` +
		`<span class="host-raw-body">{{ .raw.body }}</span>` +
		`{{ range .data.blocks }}` +
		`<span class="host-block" data-index="{{ .item.index }}" data-number="{{ .item.number }}" data-count="{{ .item.count }}" data-first="{{ .item.first }}" data-last="{{ .item.last }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}" data-parent="{{ if .item.parent }}present{{ else }}absent{{ end }}">` +
		`<span class="host-block-raw-text">{{ .raw.text }}</span>` +
		`<span class="host-block-raw-body">{{ .raw.body }}</span>` +
		`{{ range .data.leaves }}` +
		`<span class="host-leaf" data-index="{{ .item.index }}" data-count="{{ .item.count }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}" data-parent-text="{{ .item.parent.text }}">` +
		`<span class="host-leaf-raw-name">{{ .raw.name }}</span>` +
		`</span>` +
		`{{ end }}` +
		`</span>` +
		`{{ end }}` +
		`</div>`

	agreementBlockTemplateYAML = `description: agreement block section carrying a nested leaves child section
fields:
  - name: text
    type: text
    required: true
sections:
  - name: leaves
    accepted:
      - agreeleaf
    max: 4
body:
  mode: optional
`

	agreementBlockLayout = `<div class="agree-block">` +
		`<span class="agree-block-text">{{ .text }}</span>` +
		`</div>`

	agreementBlockExample = "```\n" +
		"text: Sample block\n" +
		"```\n" +
		"Sample block body.\n" +
		"# leaves\n" +
		"```\n" +
		"template: agreeleaf\n" +
		"name: Sample leaf\n" +
		"```\n"

	agreementLeafTemplateYAML = `description: agreement leaf section reading the reserved raw context unguarded
fields:
  - name: name
    type: text
    required: true
body:
  mode: disallowed
`

	agreementLeafLayout = `<span class="agree-leaf"><span class="leaf-raw-name">{{ .raw.name }}</span></span>`

	agreementLeafExample = "```\n" +
		"name: Sample leaf\n" +
		"```\n"
)

// TestLoadAndRenderRawDataContextAgree asserts that load-time and render-time
// `.raw`/`.data` AGREE for the same authored content, because the shared
// template.RawContext/template.DataContext builders feed both halves. It is an
// integration test: a real on-disk library and deck are loaded by the real
// loader (LoadLibrary, which runs checkLibraryExample's unguarded example
// execution) and rendered by the real pipeline (RenderDeck). It reads the real
// filesystem, so it is skipped under -short.
//
// It pins (raw-source-context, section-data-context, item-context,
// template-context): the slide's `.raw.title`/`.raw.body` are the author's
// source, not the converted values; `.data.blocks` carries one entry per
// authored instance, each with its own `.raw`/`.item`, and a nested instance's
// children are reached through its entry's `.data` with `.item.parent` the
// enclosing source values; and the render-time page reproduces the
// load-time-equivalent fragment byte for byte.
func TestLoadAndRenderRawDataContextAgree(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writeAgreementDeck(t)
	fsys := os.DirFS(dir)

	// LOAD-TIME half: LoadLibrary runs checkLibraryExample (step 7), which
	// builds the reserved `.raw`/`.data` context with the shared builders and
	// executes each example layout. The agreehost example layout reads `.raw`
	// and `.data` UNGUARDED, so a successful load is itself the regression
	// assertion that the load-time half supplied the reserved context.
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v (the unguarded .raw/.data example layout must load)", err)
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
	slideBytes, err := fs.ReadFile(fsys, "slides/1-agree.md")
	if err != nil {
		t.Fatalf("read deck slide: %v", err)
	}
	host, _, ok := lib.TemplateByName("agreehost")
	if !ok || host == nil {
		t.Fatal(`TemplateByName("agreehost") not loaded`)
	}
	if !bytes.Equal(slideBytes, host.ExampleBytes) {
		t.Fatalf("deck slide and agreehost example.md must be the same authored content:\nslide:\n%s\nexample:\n%s", slideBytes, host.ExampleBytes)
	}

	// Rebuild the LOAD-TIME context for that content with the SAME shared
	// builders checkLibraryExample uses. The example bytes are slide-shaped, so
	// parse them and adapt the authored tree into the neutral
	// template.ContextNode tree the shared template.DataContext walks.
	exampleSlide, err := slide.Parse(host.ExamplePath, host.ExampleBytes, reg)
	if err != nil {
		t.Fatalf("slide.Parse(agreehost example): %v", err)
	}
	hostDef, _ := reg.Lookup("agreehost")
	loadRaw := template.RawContext(exampleSlide.Frontmatter, hostDef, exampleSlide.Body)
	loadData := template.DataContext(agreementNodes(reg, exampleSlide.Sections), nil)

	funcMap := template.LayoutFuncMap(lib.Media, "/assets/templates/media")
	loadHostHTML := executeStandalone(t, "agreehost", host.LayoutText, funcMap, map[string]any{
		"raw":  loadRaw,
		"data": loadData,
	})

	// The concrete load-time values the comparison pins: the source view of the
	// slide and of both authored `blocks` instances (with the first block's two
	// nested `leaves`). If LoadLibrary's step-7 context or the shared builders
	// changed, this fragment changes.
	for _, want := range []string{
		`<span class="host-raw-title">**Host** title</span>`,
		`<span class="host-raw-body">Host *body* source.`,
		`data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="blocks" data-template="agreeblock" data-parent="absent"`,
		`data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="blocks" data-template="agreeblock" data-parent="absent"`,
		`<span class="host-block-raw-text">**First** block</span>`,
		`<span class="host-block-raw-body">First block body source.`,
		`<span class="host-block-raw-text">Second block</span>`,
		`data-index="0" data-count="2" data-section="leaves" data-template="agreeleaf" data-parent-text="**First** block"`,
		`data-index="1" data-count="2" data-section="leaves" data-template="agreeleaf" data-parent-text="**First** block"`,
		`<span class="host-leaf-raw-name">**Ada**</span>`,
		`<span class="host-leaf-raw-name">Grace</span>`,
	} {
		if !strings.Contains(loadHostHTML, want) {
			t.Errorf("load-time-equivalent slide fragment does not contain %q:\n%s", want, loadHostHTML)
		}
	}

	// RENDER-TIME half: the full library→parser→render pipeline on the same
	// authored content.
	themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
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

	// AGREE: the render-time page embeds the load-time-context execution
	// byte for byte. A divergent `.raw`/`.data`/`.item` on either side — a
	// different source view, a missing item descriptor, a child tree keyed or
	// parented differently — would break this containment.
	if !strings.Contains(page, loadHostHTML) {
		t.Errorf("render-time page does not embed the load-time-equivalent .raw/.data fragment:\nwant (load-time context execution):\n%s\npage:\n%s", loadHostHTML, page)
	}
}

// agreementNodes adapts a parsed slide/example section tree into the neutral
// template.ContextNode tree the shared template.DataContext walks, exactly as
// the load-time exampleContextNodes and render-time renderer.contextNodes
// adapters do: the declared name, the sibling index, the resolved template
// (looked up in the registry, or a name-only stand-in when unresolvable), the
// verbatim source frontmatter and body, and the recursed children. It is the
// test's own "equivalent input" on the load-time side, so it does not depend on
// either production adapter.
func agreementNodes(reg *template.Registry, sections []slide.Section) []template.ContextNode {
	if len(sections) == 0 {
		return nil
	}
	nodes := make([]template.ContextNode, len(sections))
	for i := range sections {
		sec := &sections[i]
		tmpl, _ := reg.Lookup(sec.Template)
		if tmpl == nil && sec.Template != "" {
			tmpl = &template.Template{Name: sec.Template}
		}
		nodes[i] = template.ContextNode{
			Name:     sec.Name,
			Index:    sec.Index,
			Template: tmpl,
			Source:   sec.Frontmatter,
			Body:     sec.Body,
			Children: agreementNodes(reg, sec.Children),
		}
	}
	return nodes
}

// executeStandalone parses and executes one layout source with funcMap and ctx,
// returning the HTML the layout emits. It executes the exact layout text the
// render-time pipeline parses, so the result is comparable byte for byte.
func executeStandalone(t *testing.T, name, layoutText string, funcMap htmltmpl.FuncMap, ctx map[string]any) string {
	t.Helper()
	layout, err := htmltmpl.New(name).Funcs(funcMap).Parse(layoutText)
	if err != nil {
		t.Fatalf("parse %s layout: %v", name, err)
	}
	var buf strings.Builder
	if err := layout.Execute(&buf, ctx); err != nil {
		t.Fatalf("execute %s layout: %v", name, err)
	}
	return buf.String()
}

// writeAgreementDeck writes a fresh temporary deck directory: kalide.yaml, the
// authored slide under slides/, a copy of the fixture library's templates/ tree
// for the plain theme and media, and the agreement slide/section templates.
// The agreehost example.md is written with the SAME bytes as the deck slide.
func writeAgreementDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for name, data := range map[string]string{
		"kalide.yaml":       "title: Agreement Deck\ntheme: plain\n",
		"slides/1-agree.md": agreementSource,
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

	writeProbeTemplate(t, dir, "slides", "agreehost", agreementHostTemplateYAML, agreementHostLayout, agreementSource)
	writeProbeTemplate(t, dir, "sections", "agreeblock", agreementBlockTemplateYAML, agreementBlockLayout, agreementBlockExample)
	writeProbeTemplate(t, dir, "sections", "agreeleaf", agreementLeafTemplateYAML, agreementLeafLayout, agreementLeafExample)

	return dir
}
