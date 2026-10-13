package render

// TestRenderSlidePage covers RenderSlidePage (internal/render/deck.go): the
// standalone single-slide page `kalide render <slide> --full` writes. It is the
// same reveal.js page shell RenderDeck serves, holding ONLY the selected
// slide's own <section>, but that slide is rendered from the WHOLE deck's
// identity — total is the deck's slide-file count, meta is the slide's own
// modelled position — so a layout reading the reserved `.slide.number`/
// `.slide.total` sees the same values in the standalone page, the served
// whole-deck page and the `--fragment` markup
// (requirements.requirement.cli-render).
//
// The probe builds an in-code `numbered` slide template whose layout prints the
// reserved slide position label and total (the fixture library's `hello` layout
// does not), following mustSectionRegistry's in-code style so the test needs no
// extra fixture and runs under -short; the theme registry and deck handling
// reuse the existing fixtures (fixtureThemes, renderDeckHTML, simpleDeck's
// model shape).

import (
	htmltmpl "html/template"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
)

// numberedSlideLayout prints the reserved `slide` context RenderSlide threads
// into the layout: its position label (`.slide.number`, the number with the
// lower-cased letter appended for a vertical slide) and the whole deck's
// slide-file count (`.slide.total`).
const numberedSlideLayout = `<section class="numbered"><span class="position">{{.slide.number}}</span> of <span class="total">{{.slide.total}}</span></section>`

// mustNumberedRegistry builds the in-code `numbered` slide template and returns
// the registry plus its layout func map. The registrar is direct (no content
// filesystem), so it needs no disk and runs under -short, exactly like
// mustSectionRegistry.
func mustNumberedRegistry(t *testing.T) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	reg := template.NewRegistry(nil)
	tmpl := &template.Template{
		Name:   "numbered",
		Usage:  template.UsageSlide,
		Body:   template.BodyRule{Mode: template.BodyOptional},
		Layout: template.Layout{Text: numberedSlideLayout},
	}
	if err := reg.Register(tmpl); err != nil {
		t.Fatalf("register numbered template: %v", err)
	}
	return reg, template.LayoutFuncMap(nil, "")
}

// numberedSlideModel builds the parsed model of a `numbered`-template slide.
// RenderSlide/RenderDeck/RenderSlidePage consume parsed slides directly, so no
// Markdown parsing is needed.
func numberedSlideModel(path string) *slide.Slide {
	return &slide.Slide{File: path, Template: "numbered"}
}

// numberedDeck is a three-file deck — two horizontal slides with one vertical
// (letter) slide beneath the first — so the deck-wide total is 3 and the
// vertical slide's position label carries its letter ("1a").
func numberedDeck() (*deck.Deck, []*slide.Slide) {
	d := &deck.Deck{Stacks: []deck.Stack{
		{
			Slide: deck.Slide{Path: "slides/1-first.md", Number: 1, Label: "first"},
			Vertical: []deck.Slide{
				{Path: "slides/1a-alpha.md", Number: 1, Letter: "a", Label: "alpha"},
			},
		},
		{Slide: deck.Slide{Path: "slides/2-second.md", Number: 2, Label: "second"}},
	}}
	parsed := []*slide.Slide{
		numberedSlideModel("slides/1-first.md"),
		numberedSlideModel("slides/1a-alpha.md"),
		numberedSlideModel("slides/2-second.md"),
	}
	return d, parsed
}

// TestRenderSlidePage pins the standalone single-slide page against the
// fragment and the whole-deck page: for a horizontal slide, the vertical
// (letter) slide and the closing horizontal slide of a three-file deck, the
// one-slide page embeds the slide's `--fragment` markup byte for byte and holds
// only that slide's <section>, the whole-deck page embeds the same fragment,
// and every render prints the same whole-deck numbering — never a one-slide
// page's total of 1. The nil-input failures are *RenderError, as RenderDeck
// reports them.
func TestRenderSlidePage(t *testing.T) {
	reg, funcMap := mustNumberedRegistry(t)
	themeReg := fixtureThemes(t)
	cfg := &deck.Config{Title: "Numbered Deck", Theme: "plain", Navigation: deck.NavigationDefault}

	d, parsed := numberedDeck()
	byFile := make(map[string]*slide.Slide, len(parsed))
	for _, p := range parsed {
		byFile[p.File] = p
	}
	total := d.Total()
	if total != 3 {
		t.Fatalf("deck .Total() = %d, want 3 (two horizontals plus one letter slide)", total)
	}

	// The whole-deck page, rendered once: every selected slide's fragment must
	// appear inside it too, with the same whole-deck numbering.
	deckPage := renderDeckHTML(t, cfg, d, parsed, reg, themeReg, funcMap)

	selected := []struct {
		name       string
		meta       deck.Slide
		wantNumber string
	}{
		{"horizontal slide", deck.Slide{Path: "slides/1-first.md", Number: 1, Label: "first"}, "1"},
		{"vertical letter slide", deck.Slide{Path: "slides/1a-alpha.md", Number: 1, Letter: "a", Label: "alpha"}, "1a"},
		{"closing horizontal slide", deck.Slide{Path: "slides/2-second.md", Number: 2, Label: "second"}, "2"},
	}

	for _, tc := range selected {
		t.Run(tc.name, func(t *testing.T) {
			p := byFile[tc.meta.Path]
			if p == nil {
				t.Fatalf("no parsed model for %s", tc.meta.Path)
			}

			// The `--fragment` markup: the same RenderSlide call RenderSlidePage
			// makes, with the whole deck's total.
			fragment := renderSlideString(t, p, tc.meta.Label, cfg, tc.meta, total, reg, funcMap)
			wantNumber := `<span class="position">` + tc.wantNumber + `</span>`
			wantTotal := `<span class="total">3</span>`
			if !strings.Contains(fragment, wantNumber) {
				t.Fatalf("fragment = %q, want whole-deck position label %q", fragment, wantNumber)
			}
			if !strings.Contains(fragment, wantTotal) {
				t.Fatalf("fragment = %q, want whole-deck total %q", fragment, wantTotal)
			}

			// The one-slide page: the shared page shell, holding exactly this
			// slide's <section> and nothing else.
			page, err := RenderSlidePage(cfg, d, tc.meta, p, reg, themeReg, funcMap)
			if err != nil {
				t.Fatalf("RenderSlidePage(%s): %v", tc.meta.Label, err)
			}
			pageHTML := string(page)
			if !strings.Contains(pageHTML, "<!DOCTYPE") {
				t.Errorf("RenderSlidePage(%s) is not a complete page:\n%s", tc.meta.Label, pageHTML)
			}
			if !strings.Contains(pageHTML, fragment) {
				t.Errorf("RenderSlidePage(%s) does not embed its fragment byte for byte:\n  page:     %s\n  fragment: %s", tc.meta.Label, pageHTML, fragment)
			}
			if n := strings.Count(pageHTML, "<section"); n != 1 {
				t.Errorf("RenderSlidePage(%s) holds %d <section> elements, want only the selected slide's 1:\n%s", tc.meta.Label, n, pageHTML)
			}
			if n := strings.Count(pageHTML, "</section>"); n != 1 {
				t.Errorf("RenderSlidePage(%s) holds %d </section> closers, want 1:\n%s", tc.meta.Label, n, pageHTML)
			}
			for _, other := range []deck.Slide{
				{Path: "slides/1-first.md", Label: "first"},
				{Path: "slides/1a-alpha.md", Label: "alpha"},
				{Path: "slides/2-second.md", Label: "second"},
			} {
				if other.Label == tc.meta.Label {
					continue
				}
				if strings.Contains(pageHTML, `id="`+other.Label+`"`) {
					t.Errorf("RenderSlidePage(%s) also holds slide %q:\n%s", tc.meta.Label, other.Label, pageHTML)
				}
			}

			// The whole-deck page embeds the same fragment, so the numbering
			// agrees across the fragment, the one-slide page and the deck page.
			if !strings.Contains(deckPage, fragment) {
				t.Errorf("RenderDeck page does not embed slide %s's fragment byte for byte:\n  fragment: %s", tc.meta.Label, fragment)
			}
		})
	}

	t.Run("nil inputs are RenderErrors", func(t *testing.T) {
		p := byFile["slides/1-first.md"]
		meta := deck.Slide{Path: "slides/1-first.md", Number: 1, Label: "first"}

		cases := []struct {
			name string
			call func() (htmltmpl.HTML, error)
		}{
			{"nil config", func() (htmltmpl.HTML, error) {
				return RenderSlidePage(nil, d, meta, p, reg, themeReg, funcMap)
			}},
			{"nil deck", func() (htmltmpl.HTML, error) {
				return RenderSlidePage(cfg, nil, meta, p, reg, themeReg, funcMap)
			}},
			{"nil registry", func() (htmltmpl.HTML, error) {
				return RenderSlidePage(cfg, d, meta, p, nil, themeReg, funcMap)
			}},
			{"nil theme registry", func() (htmltmpl.HTML, error) {
				return RenderSlidePage(cfg, d, meta, p, reg, nil, funcMap)
			}},
			{"nil slide", func() (htmltmpl.HTML, error) {
				return RenderSlidePage(cfg, d, meta, nil, reg, themeReg, funcMap)
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := tc.call()
				if err == nil {
					t.Fatalf("RenderSlidePage with %s: want an error", tc.name)
				}
				if _, ok := err.(*RenderError); !ok {
					t.Errorf("RenderSlidePage with %s: got %T, want *RenderError", tc.name, err)
				}
			})
		}
	})
}
