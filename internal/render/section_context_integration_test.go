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
// reads `.item` through a `{{ with .item }}` guard: the descriptor is present
// when the real renderer executes the layout, but a library example's
// load-time context carries no `item` (checkLibraryExample), so the guard keeps
// each layout executable in isolation too (template-language).
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

	sectionProbeFooterLayout = `<footer class="probe-footer"{{ with .item }} data-index="{{ .index }}" data-number="{{ .number }}" data-count="{{ .count }}" data-first="{{ .first }}" data-last="{{ .last }}" data-section="{{ .section }}" data-template="{{ .template }}" data-parent="{{ if .parent }}present{{ else }}absent{{ end }}"{{ end }}>{{ .slide.number }} / {{ .slide.total }}</footer>`

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

	sectionProbeColumnLayout = `<div class="probe-column"{{ with .item }} data-index="{{ .index }}" data-number="{{ .number }}" data-count="{{ .count }}" data-first="{{ .first }}" data-last="{{ .last }}" data-section="{{ .section }}" data-template="{{ .template }}" data-parent="{{ if .parent }}present{{ else }}absent{{ end }}"{{ end }}><span class="column-title">{{ .title }}</span><span class="column-deck-title">{{ .deck.title }}</span><span class="column-slide-number">{{ .slide.number }}</span>{{ range .people }}{{ . }}{{ end }}</div>`

	sectionProbeColumnExample = "```\ntitle: Example column\n```\n# people\n```\ntemplate: probeperson\nname: Ada\n```\n"

	sectionProbePersonTemplateYAML = `description: person section nested one level deeper inside a column's people section
fields:
  - name: name
    type: text
    required: true
body:
  mode: disallowed
`

	sectionProbePersonLayout = `<span class="person-name">{{ .name }}</span><span class="person-deck-title">{{ .deck.title }}</span><span class="person-slide-number">{{ .slide.number }}</span>{{ with .item }}<span class="person-item" data-index="{{ .index }}" data-number="{{ .number }}" data-count="{{ .count }}" data-first="{{ .first }}" data-last="{{ .last }}" data-section="{{ .section }}" data-template="{{ .template }}" data-parent-title="{{ .parent.title }}"></span>{{ end }}`

	sectionProbePersonExample = "```\nname: Ada\n```\n"
)

// TestSectionInstanceContextPipeline drives the reserved `.deck`/`.slide`
// context (template-context, section-template-context) AND the reserved `.item`
// descriptor (item-context) through the library→parser→render pipeline into a
// slide layout and every section instance it declares, at every depth: the
// motivating footer section renders `{{.slide.number}} / {{.slide.total}}`
// (e.g. `2a / 3`); a top-level section instance reads `.deck.title`,
// `.slide.number` and its own `.item` (index/number/count/first/last/section/
// template, with a nil parent, since a top-level instance's parent is the
// slide); and a nested child section reads the same `.item` one level down plus
// `.item.parent.title`, the enclosing instance's field values. It reads the
// real filesystem, so it is skipped under -short.
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
		`<footer class="probe-footer" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="footer" data-template="probefooter" data-parent="absent">2a / 3</footer>`,
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
		`<div class="probe-column" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="columns" data-template="probecolumn" data-parent="absent"><span class="column-title">First column</span>`,
		`<div class="probe-column" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="columns" data-template="probecolumn" data-parent="absent"><span class="column-title">Second column</span>`,
		// Nested `.item`: the same descriptor one level down, where
		// `.item.parent.title` resolves the enclosing column instance's field
		// values.
		`<span class="person-item" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="people" data-template="probeperson" data-parent-title="First column"></span>`,
		`<span class="person-item" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="people" data-template="probeperson" data-parent-title="First column"></span>`,
		`<span class="person-item" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="people" data-template="probeperson" data-parent-title="Second column"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
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
