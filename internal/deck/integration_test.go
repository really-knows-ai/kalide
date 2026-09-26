package deck

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// integrationTemplate is one fake catalogue entry used by the integration test:
// a template's usage and, for slide templates, its declared sections with the
// section templates they accept and their repeat bounds.
type integrationTemplate struct {
	usage    string
	sections []string
	accepted map[string][]string
	min      map[string]int
	max      map[string]int
}

// integrationCatalog is the small fake slide.Catalogue the integration test
// resolves names against. internal/deck must not import internal/template, so
// the catalogue is declared here, matching the templates used by the
// testdata/valid slides. A max <= 0 means unbounded, matching how Parse reads
// SectionDecl.
type integrationCatalog map[string]integrationTemplate

func (c integrationCatalog) LookupSlideTemplate(name string) (string, bool) {
	t, ok := c[name]
	if !ok {
		return "", false
	}
	return t.usage, true
}

func (c integrationCatalog) TemplateNames() []string {
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c integrationCatalog) SectionNames(tmpl string) []string {
	names := append([]string(nil), c[tmpl].sections...)
	sort.Strings(names)
	return names
}

func (c integrationCatalog) SectionDecl(tmpl, section string) ([]string, int, int, bool) {
	t, ok := c[tmpl]
	if !ok || !slices.Contains(t.sections, section) {
		return nil, 0, 0, false
	}
	return t.accepted[section], t.min[section], t.max[section], true
}

// integrationCatalogFor returns the catalogue the testdata/valid deck needs: a
// title slide template with no sections and a content slide template declaring
// "columns" (exactly one accepted section template, so its instance resolves
// automatically) and "gallery" (two accepted section templates, so its instance
// must name one).
func integrationCatalogFor() integrationCatalog {
	return integrationCatalog{
		"title": {usage: "slide"},
		"content": {usage: "slide", sections: []string{"columns", "gallery"},
			accepted: map[string][]string{
				"columns": {"column"},
				"gallery": {"fig", "card"},
			}},
		"column": {usage: "section"},
		"fig":    {usage: "section"},
		"card":   {usage: "section"},
	}
}

// deckDirThemes returns the project theme registry for a real on-disk deck
// directory: theme.LoadDir(fsys, "templates/themes") when the deck has a
// templates/themes directory, otherwise an in-memory registry with just
// "default", since none of the testdata deck fixtures ship a templates/ tree.
func deckDirThemes(fsys fs.FS) (*theme.Registry, error) {
	if _, err := fs.Stat(fsys, "templates/themes"); err == nil {
		return theme.LoadDir(fsys, "templates/themes")
	}
	reg := theme.NewRegistry()
	if err := reg.Register(theme.Theme{Name: theme.DefaultName}); err != nil {
		return nil, err
	}
	return reg, nil
}

// loadDeckDir loads one real on-disk deck directory through the deck loaders and
// the slide parser, in the fail-fast order the deck validator uses: the config
// first, then slide filenames and ordering, then each slide's contents in
// (number, letter) order. It returns the config and deck model alongside the
// parsed slides so callers can assert both.
func loadDeckDir(dir string) (*Config, *Deck, []*slide.Slide, error) {
	fsys := os.DirFS(dir)

	themes, err := deckDirThemes(fsys)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg, err := LoadConfig(fsys, ConfigFile, themes)
	if err != nil {
		return nil, nil, nil, err
	}
	d, err := LoadSlides(fsys, SlidesDir)
	if err != nil {
		return cfg, nil, nil, err
	}

	var parsed []*slide.Slide
	for _, stack := range d.Stacks {
		ordered := append([]Slide{stack.Slide}, stack.Vertical...)
		for _, s := range ordered {
			src, err := fs.ReadFile(fsys, s.Path)
			if err != nil {
				return cfg, d, parsed, fmt.Errorf("%s: %w", s.Path, err)
			}
			ps, err := slide.Parse(s.Path, src, integrationCatalogFor())
			if err != nil {
				return cfg, d, parsed, err
			}
			parsed = append(parsed, ps)
		}
	}
	return cfg, d, parsed, nil
}

// TestLoadDeckIntegration loads real on-disk deck directories from testdata/
// through internal/deck (LoadConfig, LoadSlides) and internal/slide.Parse with a
// small fake Catalogue (internal/deck must not import internal/template). It
// reads the real filesystem, so it is an integration test and is skipped under
// -short.
func TestLoadDeckIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads real deck directories from disk")
	}
	t.Run("valid deck model and parsed slides", testValidDeck)
	t.Run("broken variants report only the first error", testBrokenDecks)
}

// testValidDeck asserts the ordered deck model and the parsed slide structure of
// testdata/valid: numeric ordering with 10 after 2 and a vertical slide nested
// under its number.
func testValidDeck(t *testing.T) {
	dir := filepath.Join("testdata", "valid")

	cfg, d, parsed, err := loadDeckDir(dir)
	if err != nil {
		t.Fatalf("loadDeckDir(%s): %v", dir, err)
	}

	wantCfg := Config{
		Title:      "Integration Deck",
		Author:     "Ada Lovelace",
		Date:       "2026-09-25",
		Theme:      theme.DefaultName,
		Navigation: NavigationGrid,
	}
	if *cfg != wantCfg {
		t.Errorf("Config = %+v, want %+v", *cfg, wantCfg)
	}

	// Ordered horizontal model: 10 sorts after 2, and the only vertical slide
	// nests under stack 2.
	if got := deckNumbers(d); !equalInts(got, []int{1, 2, 10}) {
		t.Fatalf("horizontal numbers = %v, want [1 2 10]", got)
	}
	if len(d.Stacks[0].Vertical) != 0 {
		t.Errorf("stack 1 verticals = %d, want 0", len(d.Stacks[0].Vertical))
	}
	if got := verticalLetters(d.Stacks[1]); !equalStrings(got, []string{"a"}) {
		t.Errorf("stack 2 verticals = %v, want [a]", got)
	}
	if v := d.Stacks[1].Vertical[0]; v.Path != "slides/2a-detail.md" || v.Letter != "a" || v.Label != "detail" {
		t.Errorf("stack 2 vertical = %+v, want slides/2a-detail.md letter a label detail", v)
	}
	if d.Stacks[2].Slide.Path != "slides/10-end.md" || d.Stacks[2].Slide.Label != "end" {
		t.Errorf("last slide = %+v, want slides/10-end.md label end", d.Stacks[2].Slide)
	}

	// Parsed slides in (number, letter) order, one Parse per file.
	var paths []string
	for _, s := range parsed {
		paths = append(paths, s.File)
	}
	wantPaths := []string{
		"slides/1-intro.md",
		"slides/2-content.md",
		"slides/2a-detail.md",
		"slides/10-end.md",
	}
	if !equalStrings(paths, wantPaths) {
		t.Fatalf("parsed slide order = %v, want %v", paths, wantPaths)
	}

	if parsed[0].Template != "title" || parsed[0].Notes == nil {
		t.Errorf("slide 1 = {template %q, notes %v}, want title template and parsed notes",
			parsed[0].Template, parsed[0].Notes)
	}

	content := parsed[1]
	if content.Template != "content" {
		t.Fatalf("slide 2 template = %q, want content", content.Template)
	}
	if len(content.Sections) != 2 {
		t.Fatalf("slide 2 sections = %d, want 2", len(content.Sections))
	}
	if s := content.Sections[0]; s.Name != "columns" || s.Template != "column" {
		t.Errorf("slide 2 section 1 = %+v, want columns resolving to column", s)
	}
	if s := content.Sections[1]; s.Name != "gallery" || s.Template != "fig" {
		t.Errorf("slide 2 section 2 = %+v, want gallery resolving to the chosen fig", s)
	}
	if content.Notes != nil {
		t.Errorf("slide 2 notes = %+v, want nil", content.Notes)
	}

	if s := parsed[2]; s.File != "slides/2a-detail.md" || s.Template != "content" {
		t.Errorf("vertical parsed slide = %+v, want slides/2a-detail.md template content", s)
	}
	if parsed[3].Notes == nil {
		t.Error("slide 10 notes = nil, want the speaker notes section parsed")
	}
}

// testBrokenDecks asserts the first error of each broken variant, in the deck's
// fail-fast order.
func testBrokenDecks(t *testing.T) {
	t.Run("config error precedes an invalid slide filename", func(t *testing.T) {
		dir := filepath.Join("testdata", "broken-config")

		// The deck holds a bad slide filename too; the config fault must win.
		if _, err := LoadSlides(os.DirFS(dir), SlidesDir); err == nil {
			t.Fatal("LoadSlides: got nil error, want the invalid filename to fail")
		}

		_, _, _, err := loadDeckDir(dir)
		wantErr(t, err, "eypres.yaml:2:", `unknown navigation mode "diagonal"`)
		if strings.Contains(err.Error(), "filename") {
			t.Errorf("error = %q, want the config error before the filename error", err.Error())
		}
	})

	t.Run("invalid slide filename is reported first", func(t *testing.T) {
		dir := filepath.Join("testdata", "broken-filename")
		_, _, _, err := loadDeckDir(dir)
		wantErr(t, err,
			"slides/notaslide.md",
			"filename must be <number>[letter]-<label>.md")
	})

	t.Run("orphan vertical slide is reported first", func(t *testing.T) {
		dir := filepath.Join("testdata", "broken-vertical")
		_, _, _, err := loadDeckDir(dir)
		wantErr(t, err,
			"slides/5a-orphan.md",
			"vertical slide 5a has no numbered slide 5")
	})
}
