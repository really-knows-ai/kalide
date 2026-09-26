package render

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/assets"
	"github.com/really-knows-ai/ey-present/internal/deck"
	"github.com/really-knows-ai/ey-present/internal/slide"
	"github.com/really-knows-ai/ey-present/internal/template"
	"github.com/really-knows-ai/ey-present/internal/theme"
)

// TestRenderDeck covers RenderDeck end to end on the built-in templates: the
// .reveal > .slides structure with letter slides nested as vertical stacks, the
// navigationMode passthrough, the registry-resolved theme stylesheet, the
// embedded offline asset URLs, print-to-PDF wiring, and determinism.
func TestRenderDeck(t *testing.T) {
	reg := mustBuiltinRegistry(t)

	t.Run("vertical stack nesting and order", func(t *testing.T) {
		d := &deck.Deck{Stacks: []deck.Stack{
			{
				Slide: deck.Slide{Path: "slides/1-first.md", Number: 1, Label: "first"},
				Vertical: []deck.Slide{
					{Path: "slides/1a-alpha.md", Number: 1, Letter: "a", Label: "alpha"},
					{Path: "slides/1b-beta.md", Number: 1, Letter: "b", Label: "beta"},
				},
			},
			{Slide: deck.Slide{Path: "slides/2-second.md", Number: 2, Label: "second"}},
		}}
		parsed := []*slide.Slide{
			titleSlideModel("slides/1-first.md", "First"),
			titleSlideModel("slides/1a-alpha.md", "Alpha"),
			titleSlideModel("slides/1b-beta.md", "Beta"),
			titleSlideModel("slides/2-second.md", "Second"),
		}
		cfg := &deck.Config{Title: "Deck", Theme: theme.DefaultName, Navigation: deck.NavigationDefault}

		page := renderDeckHTML(t, cfg, d, parsed, reg, nil)

		if !strings.Contains(page, `<div class="reveal">`) || !strings.Contains(page, `<div class="slides">`) {
			t.Fatalf("page is missing the .reveal > .slides container:\n%s", page)
		}

		first := deckHTMLIndex(t, page, `id="first"`)
		alpha := deckHTMLIndex(t, page, `id="alpha"`)
		beta := deckHTMLIndex(t, page, `id="beta"`)
		second := deckHTMLIndex(t, page, `id="second"`)

		if !(first < alpha && alpha < beta && beta < second) {
			t.Fatalf("slides are out of model order: first=%d alpha=%d beta=%d second=%d", first, alpha, beta, second)
		}

		// A slide's nesting depth is the number of still-open <section>
		// elements before its anchor id. The two horizontals are top-level
		// (depth 1); the letter slides are nested inside the first (depth 2).
		depths := map[string]int{
			"first":  sectionDepth(page, first),
			"alpha":  sectionDepth(page, alpha),
			"beta":   sectionDepth(page, beta),
			"second": sectionDepth(page, second),
		}
		want := map[string]int{"first": 1, "alpha": 2, "beta": 2, "second": 1}
		for name, got := range depths {
			if got != want[name] {
				t.Errorf("slide %q has section depth %d, want %d", name, got, want[name])
			}
		}

		if got := strings.Count(page, "<section"); got != 4 {
			t.Errorf("page has %d <section> elements, want 4 (2 horizontal + 2 vertical)", got)
		}
	})

	t.Run("navigation mode passthrough", func(t *testing.T) {
		cases := []struct {
			name string
			nav  string
			want string
		}{
			{"default", deck.NavigationDefault, `navigationMode: "default"`},
			{"linear", deck.NavigationLinear, `navigationMode: "linear"`},
			{"grid", deck.NavigationGrid, `navigationMode: "grid"`},
			{"omitted", "", `navigationMode: "default"`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cfg := &deck.Config{Title: "Deck", Theme: theme.DefaultName, Navigation: tc.nav}
				page := renderDeckHTML(t, cfg, simpleDeck(), simpleDeckSlides(), reg, nil)
				if !strings.Contains(page, tc.want) {
					t.Fatalf("navigation %q: page does not contain %q:\n%s", tc.nav, tc.want, page)
				}
				if !strings.Contains(page, "hash: true") {
					t.Errorf("navigation %q: Reveal.initialize does not enable hash anchors", tc.nav)
				}
			})
		}
	})

	t.Run("theme css link from registry", func(t *testing.T) {
		// The built-in default resolves through theme.Builtin() when no registry
		// is passed, and through an explicit built-in registry when one is.
		cfg := &deck.Config{Title: "Deck", Theme: theme.DefaultName, Navigation: deck.NavigationDefault}
		for _, themeReg := range []*theme.Registry{nil, theme.Builtin()} {
			page := renderDeckHTML(t, cfg, simpleDeck(), simpleDeckSlides(), reg, themeReg)
			want := `<link rel="stylesheet" href="/assets/theme.css">`
			if !strings.Contains(page, want) {
				t.Fatalf("default theme: page does not contain %q:\n%s", want, page)
			}
		}

		// An empty theme name resolves to the default theme's stylesheet.
		cfg.Theme = ""
		page := renderDeckHTML(t, cfg, simpleDeck(), simpleDeckSlides(), reg, nil)
		if !strings.Contains(page, `<link rel="stylesheet" href="/assets/theme.css">`) {
			t.Fatalf("empty theme: page does not resolve the default stylesheet:\n%s", page)
		}

		// A registry-registered theme drives the link: the URL is the registry's
		// Stylesheet under /assets/, not a hard-coded path.
		custom := theme.NewRegistry()
		if err := custom.Register(theme.Theme{Name: "sunset", Stylesheet: "themes/sunset.css"}); err != nil {
			t.Fatalf("register custom theme: %v", err)
		}
		cfg.Theme = "sunset"
		page = renderDeckHTML(t, cfg, simpleDeck(), simpleDeckSlides(), reg, custom)
		want := `<link rel="stylesheet" href="/assets/themes/sunset.css">`
		if !strings.Contains(page, want) {
			t.Fatalf("custom theme: page does not contain %q:\n%s", want, page)
		}
		if strings.Contains(page, `href="/assets/theme.css"`) {
			t.Errorf("custom theme: page still links the default stylesheet:\n%s", page)
		}
	})

	t.Run("embedded asset urls", func(t *testing.T) {
		cfg := &deck.Config{Title: "Deck", Theme: theme.DefaultName, Navigation: deck.NavigationDefault}
		page := renderDeckHTML(t, cfg, simpleDeck(), simpleDeckSlides(), reg, nil)

		want := []string{
			`href="/assets/reveal/dist/reset.css"`,
			`href="/assets/reveal/dist/reveal.css"`,
			`href="/assets/theme.css"`,
			`src="/assets/reveal/dist/reveal.js"`,
			`src="/assets/reveal/dist/plugin/notes.js"`,
		}
		for _, ref := range want {
			if !strings.Contains(page, ref) {
				t.Errorf("page does not reference embedded asset %q:\n%s", ref, page)
			}
		}
	})

	t.Run("print pdf support bundled", func(t *testing.T) {
		cfg := &deck.Config{Title: "Deck", Theme: theme.DefaultName, Navigation: deck.NavigationDefault}
		page := renderDeckHTML(t, cfg, simpleDeck(), simpleDeckSlides(), reg, nil)

		// Print-to-PDF is reveal.js core (?print-pdf) using the print styles
		// bundled in dist/reveal.css: the page must not link a separate print
		// plugin or print stylesheet.
		lower := strings.ToLower(page)
		for _, forbidden := range []string{"print-pdf", "print.css", "print.js", "print/"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("deck page references separate print asset %q; print support must be core+bundled:\n%s", forbidden, page)
			}
		}

		// The wiring is the embedded reveal.css (print styles) and reveal.js
		// (core ?print-pdf handling).
		css, err := fs.ReadFile(assets.Reveal(), "dist/reveal.css")
		if err != nil {
			t.Fatalf("read embedded dist/reveal.css: %v", err)
		}
		if !strings.Contains(string(css), "print-pdf") {
			t.Errorf("embedded dist/reveal.css does not carry print-pdf styles")
		}
		if !strings.Contains(string(css), "@media print") {
			t.Errorf("embedded dist/reveal.css does not carry @media print rules")
		}

		js, err := fs.ReadFile(assets.Reveal(), "dist/reveal.js")
		if err != nil {
			t.Fatalf("read embedded dist/reveal.js: %v", err)
		}
		if !strings.Contains(string(js), "print-pdf") {
			t.Errorf("embedded dist/reveal.js does not handle ?print-pdf in core")
		}
	})

	t.Run("deterministic output", func(t *testing.T) {
		cfg := &deck.Config{Title: "Deck", Theme: theme.DefaultName, Navigation: deck.NavigationLinear}
		first := renderDeckHTML(t, cfg, simpleDeck(), simpleDeckSlides(), reg, nil)
		second := renderDeckHTML(t, cfg, simpleDeck(), simpleDeckSlides(), reg, nil)
		if first != second {
			t.Fatalf("RenderDeck is not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
		}
	})
}

// mustBuiltinRegistry returns the compiled-in template registry or fails the
// test. RenderDeck needs a populated registry to resolve each slide's layout.
func mustBuiltinRegistry(t *testing.T) *template.Registry {
	t.Helper()
	reg, err := template.Builtins()
	if err != nil {
		t.Fatalf("template.Builtins: %v", err)
	}
	return reg
}

// titleSlideModel builds the parsed model of a title-template slide carrying
// the given text. RenderDeck consumes parsed slides directly, so no Markdown
// parsing is needed here.
func titleSlideModel(path, title string) *slide.Slide {
	return &slide.Slide{
		File:        path,
		Template:    "title",
		Frontmatter: map[string]any{"title": title},
	}
}

// simpleDeck is a one-horizontal-slide deck used where the stack shape is not
// under test.
func simpleDeck() *deck.Deck {
	return &deck.Deck{Stacks: []deck.Stack{
		{Slide: deck.Slide{Path: "slides/1-only.md", Number: 1, Label: "only"}},
	}}
}

// simpleDeckSlides is the parsed model matching simpleDeck.
func simpleDeckSlides() []*slide.Slide {
	return []*slide.Slide{titleSlideModel("slides/1-only.md", "Only")}
}

// renderDeckHTML renders the deck and returns the page HTML as a string.
func renderDeckHTML(t *testing.T, cfg *deck.Config, d *deck.Deck, parsed []*slide.Slide, reg *template.Registry, themeReg *theme.Registry) string {
	t.Helper()
	out, err := RenderDeck(cfg, d, parsed, reg, themeReg)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	return string(out)
}

// deckHTMLIndex returns the position of sub in page or fails the test.
func deckHTMLIndex(t *testing.T, page, sub string) int {
	t.Helper()
	i := strings.Index(page, sub)
	if i < 0 {
		t.Fatalf("page does not contain %q:\n%s", sub, page)
	}
	return i
}

// sectionDepth returns how many <section> elements are still open at pos, a
// cheap structural check of vertical-stack nesting without an HTML parser.
func sectionDepth(page string, pos int) int {
	depth := 0
	for i := 0; i < pos; {
		open := strings.Index(page[i:pos], "<section")
		close := strings.Index(page[i:pos], "</section>")
		if open < 0 && close < 0 {
			break
		}
		if open >= 0 && (close < 0 || open < close) {
			depth++
			i += open + len("<section")
			continue
		}
		depth--
		i += close + len("</section>")
	}
	return depth
}
