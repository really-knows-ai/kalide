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
	"github.com/really-knows-ai/ey-present/internal/theme"
	"github.com/really-knows-ai/ey-present/internal/validate"
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
// navigationMode from eypres.yaml, the registry-resolved theme stylesheet
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

	// Deck loader: eypres.yaml then slides/, in the validator's fail-fast order.
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

	// navigationMode is passed straight through from eypres.yaml.
	if !strings.Contains(page, `navigationMode: "grid"`) {
		t.Errorf("page does not carry navigation grid:\n%s", page)
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

// pipelineDeckFiles is the real deck written to a t.TempDir(): eypres.yaml,
// one slide using the fixture library's hello template, and the fixture
// templates/ library itself copied alongside it.
var pipelineDeckFiles = map[string]string{
	"eypres.yaml": "title: Integration Deck\n" +
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
