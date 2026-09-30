package e2e

// This file is the phase-4 task-7 end-to-end test for deck properties and the
// reserved `.deck`/`.slide` template context (section-template-context,
// deck-data-in-templates, slide-metadata): it drives a served deck through the
// real kalide binary via the harness, with:
//
//   - a `properties:` block in kalide.yaml (deck-data-in-templates),
//   - a slide layout that reads `{{.deck.title}}` and
//     `{{index .deck.properties "audience"}}`,
//   - a footer section instance that renders
//     `{{.slide.number}} / {{.slide.total}}` (section-template-context),
//   - twelve slide files, including one vertical (letter) slide, so the
//     served label is the vertical slide's string position over the deck's
//     total slide count (slide-metadata), e.g. "2a / 12".
//
// It builds and drives the real binary, so it is skipped under -short.

import (
	"strings"
	"testing"
)

// deckPropertiesLibrary lays down a minimal, self-contained template library
// under templates/: one theme, one slide-usage template ("main") whose layout
// reads `.deck.title` and a deck property, and one section-usage template
// ("footer") whose layout renders `.slide.number` and `.slide.total`.
func writeDeckPropertiesLibrary(t *testing.T, h *Harness) {
	t.Helper()

	files := map[string]string{
		"templates/library.yaml": "name: deckprops\n" +
			"description: e2e fixture library for deck properties and template context\n" +
			"format: 1\n",

		"templates/themes/plain/theme.css": "body { margin: 0; }\n",

		"templates/sections/footer/template.yaml": "description: a footer rendering the slide's position and the deck total\n" +
			"fields: []\n" +
			"body:\n" +
			"  mode: disallowed\n",
		"templates/sections/footer/layout.html.tmpl": "<div class=\"e2e-footer\">{{.slide.number}} / {{.slide.total}}</div>\n",
		"templates/sections/footer/example.md": "```\n" +
			"template: footer\n" +
			"```\n",

		"templates/slides/main/template.yaml": "description: a slide with a heading and a footer section\n" +
			"fields:\n" +
			"  - name: heading\n" +
			"    type: text\n" +
			"    required: true\n" +
			"    max_length: 80\n" +
			"sections:\n" +
			"  - name: footer\n" +
			"    accepted:\n" +
			"      - footer\n" +
			"    max: 1\n" +
			"body:\n" +
			"  mode: optional\n",
		// The footer instance arrives pre-rendered as trusted HTML (one element
		// per instance), so the slide layout splices it rather than ranging
		// data maps and reading `.slide` again (template-language).
		"templates/slides/main/layout.html.tmpl": "<section class=\"e2e-slide\">\n" +
			"  <h1 class=\"e2e-heading\">{{.heading}}</h1>\n" +
			"  <span class=\"e2e-deck-title\">{{.deck.title}}</span>\n" +
			"  <span class=\"e2e-audience\">{{index .deck.properties \"audience\"}}</span>\n" +
			"  {{range .footer}}{{.}}{{end}}\n" +
			"</section>\n",
		"templates/slides/main/example.md": "---\n" +
			"template: main\n" +
			"heading: Example\n" +
			"---\n",
	}
	for rel, data := range files {
		h.WriteFile(rel, []byte(data))
	}
}

// deckPropertiesSlide is one slide file using the "main" template with a
// footer section instance.
func deckPropertiesSlide(heading string) string {
	return "---\n" +
		"template: main\n" +
		"heading: " + heading + "\n" +
		"---\n\n" +
		"# footer\n" +
		"```\n" +
		"template: footer\n" +
		"```\n"
}

// TestDeckPropertiesAndTemplateContext is the end-to-end test described at
// the top of this file.
func TestDeckPropertiesAndTemplateContext(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

	h.WriteFile("kalide.yaml", []byte(
		"title: Deck Properties Demo\n"+
			"author: E2E Author\n"+
			"date: 2026-09-27\n"+
			"theme: plain\n"+
			"properties:\n"+
			"  audience: Executives\n",
	))

	writeDeckPropertiesLibrary(t, h)

	// Twelve slides, including exactly one vertical (letter) slide 2a, so the
	// deck total is 12 and the vertical slide's label is "2a".
	for _, n := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"} {
		h.WriteFile("slides/"+n+"-slide"+n+".md", []byte(deckPropertiesSlide("Slide "+n)))
	}
	h.WriteFile("slides/2a-detail.md", []byte(deckPropertiesSlide("Slide 2 detail")))

	h.Start()

	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}

	if !strings.Contains(body, `<span class="e2e-deck-title">Deck Properties Demo</span>`) {
		t.Errorf("served deck page does not render .deck.title:\n%s", body)
	}
	if !strings.Contains(body, `<span class="e2e-audience">Executives</span>`) {
		t.Errorf("served deck page does not render the deck's %q property:\n%s", "audience", body)
	}
	if !strings.Contains(body, `<div class="e2e-footer">2a / 12</div>`) {
		t.Errorf("served deck page does not render the vertical slide's label over the deck total (want \"2a / 12\"):\n%s", body)
	}

	h.Stop()
}
