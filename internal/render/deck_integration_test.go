package render

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/assets"
	"github.com/really-knows-ai/ey-present/internal/deck"
	"github.com/really-knows-ai/ey-present/internal/slide"
	"github.com/really-knows-ai/ey-present/internal/template"
	"github.com/really-knows-ai/ey-present/internal/validate"
)

// TestRenderDeckPipeline is the whole-deck integration test: a real deck
// directory on disk is loaded by internal/deck (LoadConfig, LoadSlides), checked
// by internal/validate.Validate against template.Builtins(), and rendered by
// RenderDeck into the complete offline page. It reads the real filesystem, so it
// is skipped under -short.
//
// It asserts the full page HTML the phase-6 pipeline produces: the .reveal >
// .slides shell, the title slide's title/notes, the vertical slide nested as a
// stack inside its numbered slide, anchor ids taken from the slide labels,
// the navigationMode from eypres.yaml, the registry-resolved theme link, and
// that every /assets/ path the page references — and every url(…) the default
// theme stylesheet references — actually resolves in the embedded
// internal/assets FS.
func TestRenderDeckPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	reg := mustBuiltinRegistry(t)
	dir := writePipelineDeck(t)
	fsys := os.DirFS(dir)

	// Deck loader: eypres.yaml then slides/, in the validator's fail-fast order.
	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile)
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
	if len(d.Stacks) != 2 {
		t.Fatalf("deck has %d horizontal stacks, want 2", len(d.Stacks))
	}
	if got := d.Stacks[0].Slide.Label; got != "title" {
		t.Errorf("first stack label = %q, want %q", got, "title")
	}
	if len(d.Stacks[0].Vertical) != 1 || d.Stacks[0].Vertical[0].Label != "agenda" {
		t.Fatalf("first stack vertical = %+v, want the single agenda slide", d.Stacks[0].Vertical)
	}

	// Validator: the same real directory, through the built-in registry.
	if verr, invalid := validate.Validate(fsys, reg, nil); invalid {
		t.Fatalf("validate.Validate rejected the deck: %s", validate.Format(verr))
	}

	// Parse each slide in model order, keyed by its deck path, exactly as
	// RenderDeck expects (the convention internal/validate uses).
	parsed := parsePipelineSlides(t, fsys, d, reg)

	pageHTML, err := RenderDeck(cfg, d, parsed, reg, nil)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	page := string(pageHTML)

	// Document shell and title.
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

	// Anchors come from the slide labels.
	for _, label := range []string{"title", "agenda", "content"} {
		if !strings.Contains(page, `id="`+label+`"`) {
			t.Errorf("page is missing anchor id %q:\n%s", label, page)
		}
	}

	// The letter slide nests as a vertical stack inside its numbered slide: the
	// two horizontals are at depth 1, the agenda at depth 2, and the page has
	// exactly three <section> elements.
	titleAt := deckHTMLIndex(t, page, `id="title"`)
	agendaAt := deckHTMLIndex(t, page, `id="agenda"`)
	contentAt := deckHTMLIndex(t, page, `id="content"`)
	if !(titleAt < agendaAt && agendaAt < contentAt) {
		t.Errorf("slides out of model order: title=%d agenda=%d content=%d", titleAt, agendaAt, contentAt)
	}
	wantDepths := map[string]int{"title": 1, "agenda": 2, "content": 1}
	gotDepths := map[string]int{
		"title":   sectionDepth(page, titleAt),
		"agenda":  sectionDepth(page, agendaAt),
		"content": sectionDepth(page, contentAt),
	}
	for label, want := range wantDepths {
		if got := gotDepths[label]; got != want {
			t.Errorf("slide %q has section depth %d, want %d", label, got, want)
		}
	}
	if got := strings.Count(page, "<section"); got != 3 {
		t.Errorf("page has %d <section> elements, want 3 (2 horizontal + 1 vertical)", got)
	}

	// Rendered slide content from the built-in layouts.
	if !strings.Contains(page, `<h1 class="ey-title">Integration Deck</h1>`) {
		t.Errorf("page is missing the rendered title heading:\n%s", page)
	}
	if !strings.Contains(page, `<h2 class="ey-heading">Where the growth is coming from</h2>`) {
		t.Errorf("page is missing the rendered content heading:\n%s", page)
	}

	// Speaker notes: a # notes section on the title and content slides becomes a
	// reveal.js <aside class="notes"> inside the slide.
	if got := strings.Count(page, `<aside class="notes">`); got != 2 {
		t.Errorf("page has %d notes asides, want 2:\n%s", got, page)
	}
	for _, want := range []string{
		"Greet the audience, then hand over to the presenters.",
		"Pause on the metric so the number lands.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing rendered notes %q:\n%s", want, page)
		}
	}

	// navigationMode is passed straight through from eypres.yaml.
	if !strings.Contains(page, `navigationMode: "grid"`) {
		t.Errorf("page does not carry navigation grid:\n%s", page)
	}

	// The default theme resolves through the theme registry to the embedded
	// stylesheet URL.
	if !strings.Contains(page, `<link rel="stylesheet" href="/assets/theme.css">`) {
		t.Errorf("page does not link the resolved theme stylesheet:\n%s", page)
	}

	// Every /assets/ URL the page references resolves in the embedded FS, and the
	// offline asset set the page is expected to reference is present.
	assertPageAssetRefsResolve(t, page)
	assertThemeStylesheetRefsResolve(t)
}

// pipelineDeckFiles is the real deck written to a t.TempDir(): a title slide with
// speaker notes, a letter (vertical) slide beneath it, and a content slide with
// two composed columns and notes. It uses only the built-in templates.
var pipelineDeckFiles = map[string]string{
	"eypres.yaml": "title: Integration Deck\n" +
		"author: Ada Lovelace\n" +
		"date: 2026-09-25\n" +
		"theme: default\n" +
		"navigation: grid\n",
	"slides/1-title.md": `---
template: title
title: Integration Deck
subtitle: End to end
date: 2026-09-25
date_format: long
---
# notes
Greet the audience, then hand over to the presenters.
`,
	"slides/1a-agenda.md": `---
template: title
title: Agenda
subtitle: What we will cover
---
`,
	"slides/2-content.md": contentSlideSource,
}

// contentSlideSource is the content slide with two composed columns sections and
// a speaker-notes section. The ``` fences are plain fences opening each section
// instance's frontmatter (section-frontmatter), so they are built by
// concatenation rather than embedded in a raw string.
const contentSlideSource = `---
template: content
heading: Where the growth is coming from
layout: columns
metric: 1250000
metric_format: compact
show_metric: true
as_of: 2026-09-25
as_of_format: long
---

Revenue is up across every region, led by services.

# columns
` + "```" + `
template: column
title: Revenue
` + "```" + `

Recurring revenue grew 18% year over year.

# columns
` + "```" + `
template: column
title: New customers
` + "```" + `

We added 1,250 new logos in the quarter.

# notes
Pause on the metric so the number lands.
`

// writePipelineDeck writes pipelineDeckFiles to a fresh t.TempDir() and returns
// the directory, creating parent directories as needed.
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
	return dir
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
// under the /assets/ mount.
var assetRefRE = regexp.MustCompile(`(?:href|src)="/assets/([^"]+)"`)

// pageAssetFS resolves an /assets/-relative path against the embedded sub-tree
// that serves it (assets.Reveal/Fonts/Logo, or the root for theme.css), returning
// the accessor and the path within it. The phase-7 server mounts assets.FS at
// /assets/, so these are exactly the files the page will fetch.
func pageAssetFS(name string) (fsys fs.FS, rel string) {
	switch {
	case strings.HasPrefix(name, "reveal/"):
		return assets.Reveal(), strings.TrimPrefix(name, "reveal/")
	case strings.HasPrefix(name, "fonts/"):
		return assets.Fonts(), strings.TrimPrefix(name, "fonts/")
	case strings.HasPrefix(name, "logo/"):
		return assets.Logo(), strings.TrimPrefix(name, "logo/")
	default:
		return assets.FS, name
	}
}

// assertPageAssetRefsResolve checks that every /assets/ path referenced by the
// page exists in the embedded assets FS (the tree served under /assets/), and
// that the offline assets the deck page is expected to load are all referenced.
func assertPageAssetRefsResolve(t *testing.T, page string) {
	t.Helper()

	// The deck page template itself must resolve through its accessor.
	if _, err := fs.Stat(assets.DeckPage(), "deck.html.tmpl"); err != nil {
		t.Errorf("embedded deck page does not resolve: %v", err)
	}

	matches := assetRefRE.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		t.Fatalf("page references no /assets/ paths:\n%s", page)
	}

	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		fsys, rel := pageAssetFS(name)
		if _, err := fs.Stat(fsys, rel); err != nil {
			t.Errorf("page references %q, which does not resolve in the embedded assets FS: %v", name, err)
		}
	}

	for _, name := range []string{
		"reveal/dist/reset.css",
		"reveal/dist/reveal.css",
		"reveal/dist/reveal.js",
		"reveal/dist/plugin/notes.js",
		"theme.css",
	} {
		if !seen[name] {
			t.Errorf("page does not reference expected embedded asset %q", name)
		}
	}
}

// themeURLRE captures a single-quoted url(…) reference in a stylesheet.
var themeURLRE = regexp.MustCompile(`url\('([^']+)'\)`)

// assertThemeStylesheetRefsResolve checks that every locally referenced url(…)
// in the embedded default theme stylesheet resolves in assets.FS. The stylesheet
// lives at the root of the embedded tree, so its references are relative to it.
func assertThemeStylesheetRefsResolve(t *testing.T) {
	t.Helper()

	css, err := fs.ReadFile(assets.FS, "theme.css")
	if err != nil {
		t.Fatalf("read embedded theme.css: %v", err)
	}

	matches := themeURLRE.FindAllStringSubmatch(string(css), -1)
	if len(matches) == 0 {
		t.Fatal("embedded theme.css references no url(…) assets")
	}
	for _, m := range matches {
		ref := m[1]
		if strings.HasPrefix(ref, "http") || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "#") {
			continue
		}
		if _, err := fs.Stat(assets.FS, ref); err != nil {
			t.Errorf("theme.css references %q, which does not resolve in the embedded assets FS: %v", ref, err)
		}
	}
}
