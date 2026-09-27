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

// contextProbeTemplateYAML, contextProbeLayout and contextProbeExample make up
// a slide template added to the fixture library copy in a temporary deck: its
// layout reads the reserved `.deck` and `.slide` context, so the test can drive
// that context through the real library→parser→render pipeline rather than
// through RenderSlide directly.
const (
	contextProbeTemplateYAML = `description: reserved deck and slide context probe
fields:
  - name: title
    type: text
    required: true
body:
  mode: optional
`

	contextProbeLayout = `<section>
  <h1>{{.title}}</h1>
  <span class="deck-title">{{.deck.title}}</span>
  <span class="deck-author">{{.deck.author}}</span>
  <span class="deck-date">{{.deck.date}}</span>
  <span class="audience">{{index .deck.properties "audience"}}</span>
  <span class="slide-number">{{.slide.number}}</span>
  <span class="slide-total">{{.slide.total}}</span>
  <span class="markup">{{index .deck.properties "markup"}}</span>
  <span class="literal">{{index .deck.properties "literal"}}</span>
  <span class="count-type">{{printf "%T" (index .deck.properties "count")}}</span>
  <span class="draft-type">{{printf "%T" (index .deck.properties "draft")}}</span>
</section>
`

	contextProbeExample = `---
template: contextprobe
title: Example
---
An example probe slide.
`
)

// TestRenderSlideContextPipeline drives the reserved `.deck` (title/author/date
// and author-declared properties) and `.slide` (string position label and
// integer total) context through the library→parser→render pipeline: a real
// deck directory on disk, whose context-reading layout is written to disk as a
// template library, is loaded by internal/deck (LoadConfig, LoadSlides), its
// registry built over the library by template.LoadLibrary followed by
// template.NewRegistryFromLibrary — the same load path kalide start uses —
// parsed by internal/slide.Parse and rendered by RenderDeck. It reads the
// real filesystem, so it is skipped under -short.
//
// template.LoadLibrary's templates-dir-validation step 7 executes each
// template's example layout against a well-formed but empty stand-in `.deck`/
// `.slide` context (checkLibraryExample), so the context-probe layout's
// `{{index .deck.properties …}}` reads execute cleanly there too, against an
// empty properties map, before any real deck exists.
//
// It also pins the typing/escaping clause (deck-properties): a string property
// containing `<`, `<b>` or `**` executes through html/template as escaped or
// literal text — never raw HTML and never Markdown-rendered — while a number or
// boolean property reaches the layout as its typed non-string value.
func TestRenderSlideContextPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	t.Run("deck and slide context render through the pipeline", func(t *testing.T) {
		dir := writeContextProbeDeck(t,
			"title: Context Deck\n"+
				"author: Ada Lovelace\n"+
				"date: 2026-09-25\n"+
				"theme: plain\n"+
				"properties:\n"+
				"  audience: exec\n"+
				"  markup: \"<b>bold</b>\"\n"+
				"  literal: \"**stars**\"\n"+
				"  count: 42\n"+
				"  draft: true\n",
			map[string]string{
				"1-intro.md":   contextProbeSlide("Intro"),
				"2-main.md":    contextProbeSlide("Main"),
				"2a-detail.md": contextProbeSlide("Detail"),
			})
		page := renderContextProbeDeck(t, dir)

		for _, want := range []string{
			`<span class="deck-title">Context Deck</span>`,
			`<span class="deck-author">Ada Lovelace</span>`,
			`<span class="deck-date">2026-09-25</span>`,
			`<span class="audience">exec</span>`,
			`<span class="slide-number">2a</span>`,
			`<span class="slide-total">3</span>`,
			`<span class="markup">&lt;b&gt;bold&lt;/b&gt;</span>`,
			`<span class="literal">**stars**</span>`,
			`<span class="count-type">int</span>`,
			`<span class="draft-type">bool</span>`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("page does not contain %q:\n%s", want, page)
			}
		}

		// The markup stays escaped/literal: neither the raw HTML nor the
		// Markdown rendering of the literal `**stars**` may appear.
		for _, forbidden := range []string{"<b>bold</b>", "<strong>stars</strong>", "<em>stars</em>"} {
			if strings.Contains(page, forbidden) {
				t.Errorf("page contains raw or Markdown-rendered %q:\n%s", forbidden, page)
			}
		}
	})

	t.Run("omitted author and date and an absent properties block render empty", func(t *testing.T) {
		dir := writeContextProbeDeck(t,
			"title: Minimal Deck\n"+
				"theme: plain\n",
			map[string]string{"1-only.md": contextProbeSlide("Only")})
		page := renderContextProbeDeck(t, dir)

		for _, want := range []string{
			`<span class="deck-title">Minimal Deck</span>`,
			`<span class="deck-author"></span>`,
			`<span class="deck-date"></span>`,
			`<span class="audience"></span>`,
			`<span class="slide-number">1</span>`,
			`<span class="slide-total">1</span>`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("page does not contain %q:\n%s", want, page)
			}
		}
	})
}

// contextProbeSlide is one slide file using the context-reading layout.
func contextProbeSlide(title string) string {
	return "---\ntemplate: contextprobe\ntitle: " + title + "\n---\n"
}

// writeContextProbeDeck writes a fresh temporary deck directory: kalideYAML as
// kalide.yaml, each slide under slides/, and a copy of the fixture library's
// templates/ tree plus the context-probe slide template the slides use.
func writeContextProbeDeck(t *testing.T, kalideYAML string, slides map[string]string) string {
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

	probeDir := filepath.Join(dir, "templates", "slides", "contextprobe")
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", probeDir, err)
	}
	for name, data := range map[string]string{
		"template.yaml":    contextProbeTemplateYAML,
		"layout.html.tmpl": contextProbeLayout,
		"example.md":       contextProbeExample,
	} {
		if err := os.WriteFile(filepath.Join(probeDir, name), []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", filepath.Join(probeDir, name), err)
		}
	}
	return dir
}

// renderContextProbeDeck loads and renders one temporary deck directory through
// the library→parser→render pipeline: theme.LoadDir, then a registry built over
// the on-disk context-probe library with template.LoadLibrary followed by
// template.NewRegistryFromLibrary, deck.LoadConfig, deck.LoadSlides,
// slide.Parse and RenderDeck. It returns the page HTML, failing the test on
// any error.
func renderContextProbeDeck(t *testing.T, dir string) string {
	t.Helper()
	fsys := os.DirFS(dir)

	themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}
	reg := contextProbeRegistry(t, fsys)
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

// contextProbeRegistry builds the library registry over the on-disk context
// probe library using template.LoadLibrary — the same load path kalide start
// uses — followed by template.NewRegistryFromLibrary.
func contextProbeRegistry(t *testing.T, fsys fs.FS) *template.Registry {
	t.Helper()

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
	return reg
}
