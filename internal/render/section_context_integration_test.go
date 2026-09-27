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

// The section-context-probe library adds a slide template declaring two
// sections — a `footer` section (the motivating `{{.slide.number}} / {{.slide.total}}`
// footer) and a `columns` section accepting a `probecolumn` section template,
// which nests a `people` list of `probeperson` section-template items one
// level deeper (section-template-context's composition example) — so this
// test drives the reserved `.deck`/`.slide` context into every section
// instance, at every depth, through the real library→parser→render pipeline.
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
  {{ range .footer }}
  <footer class="probe-footer">{{.slide.number}} / {{.slide.total}}</footer>
  <span class="footer-deck-title">{{.deck.title}}</span>
  {{ end }}
  {{ range .columns }}
  <div class="probe-column">
    <span class="column-title">{{.title}}</span>
    <span class="column-deck-title">{{.deck.title}}</span>
    <span class="column-slide-number">{{.slide.number}}</span>
    {{ range .people }}
    <span class="person-name">{{.name}}</span>
    <span class="person-deck-title">{{.deck.title}}</span>
    <span class="person-slide-number">{{.slide.number}}</span>
    {{ end }}
  </div>
  {{ end }}
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
`

	sectionProbeFooterTemplateYAML = `description: footer section probing the reserved slide-position context
body:
  mode: disallowed
`

	sectionProbeFooterLayout = `<footer class="probe-footer">{{.slide.number}} / {{.slide.total}}</footer>`

	sectionProbeFooterExample = "```\n```\n"

	sectionProbeColumnTemplateYAML = `description: column section nesting a list of person section instances one level deeper
fields:
  - name: title
    type: text
  - name: people
    type: list
    item:
      name: person
      type: section-template
      section_template: probeperson
body:
  mode: optional
`

	sectionProbeColumnLayout = `<div class="probe-column">
  <span class="column-title">{{.title}}</span>
  <span class="column-deck-title">{{.deck.title}}</span>
  <span class="column-slide-number">{{.slide.number}}</span>
  {{ range .people }}
  <span class="person-name">{{.name}}</span>
  <span class="person-deck-title">{{.deck.title}}</span>
  <span class="person-slide-number">{{.slide.number}}</span>
  {{ end }}
</div>
`

	sectionProbeColumnExample = "```\ntitle: Example column\npeople:\n  - name: Ada\n```\n"

	sectionProbePersonTemplateYAML = `description: person section nested one level deeper inside a column's people list
fields:
  - name: name
    type: text
    required: true
body:
  mode: disallowed
`

	sectionProbePersonLayout = `<span class="person-name">{{.name}}</span><span class="person-deck-title">{{.deck.title}}</span><span class="person-slide-number">{{.slide.number}}</span>`

	sectionProbePersonExample = "```\nname: Ada\n```\n"
)

// TestSectionInstanceContextPipeline drives the reserved `.deck`/`.slide`
// context (template-context, section-template-context) through the
// library→parser→render pipeline into a slide layout AND every section
// instance it declares, including a section nested one level deeper: the
// motivating footer section renders `{{.slide.number}} / {{.slide.total}}`
// (e.g. `2a / 3`), `{{.deck.title}}` renders from a top-level section AND
// the deeper-nested person instance, and a section instance's own fields
// (title, name) stay directly addressable alongside the reserved entries. It
// reads the real filesystem, so it is skipped under -short.
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
		// The motivating footer: string position label over the integer total.
		`<footer class="probe-footer">2a / 3</footer>`,
		// .deck.title from the footer section, the column section, and the
		// person section nested one level deeper inside the column.
		`<span class="footer-deck-title">Section Context Deck</span>`,
		`<span class="column-deck-title">Section Context Deck</span>`,
		`<span class="person-deck-title">Section Context Deck</span>`,
		// .slide.number from the column section and the nested person.
		`<span class="column-slide-number">2a</span>`,
		`<span class="person-slide-number">2a</span>`,
		// Section instance fields stay directly addressable alongside deck/slide.
		`<span class="column-title">Example column</span>`,
		`<span class="person-name">Ada</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
}

// sectionProbeSlide is one slide file using the section-context-probe layout,
// declaring both the footer section and a column section with one nested
// person.
func sectionProbeSlide(title string) string {
	return "---\ntemplate: sectioncontextprobe\ntitle: " + title + "\n---\n" +
		"# footer\n```\ntemplate: probefooter\n```\n" +
		"\n# columns\n```\n" +
		"template: probecolumn\n" +
		"title: Example column\n" +
		"people:\n" +
		"  - name: Ada\n" +
		"```\n"
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
