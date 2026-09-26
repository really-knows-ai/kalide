package validate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/template"
	"github.com/really-knows-ai/ey-present/internal/theme"
)

// TestValidateIntegration runs real on-disk decks through the whole validator
// end to end: a temp deck directory (os.DirFS) read by the deck loaders, the
// slide parser, internal/mdcheck, internal/template.CheckValues/CheckBody and
// the link pass, all against template.Builtins()' compiled-in registry. It
// reads and writes the real filesystem, so it is skipped under -short.
//
// It pins the exact formatted first error (validate.Format) for one
// representative deck per failure class — config, filename, frontmatter field,
// body rule and inter-slide link — in the fixed fail-fast order, plus the zero
// ValidationError for a valid deck that mirrors a built-in template's example.
func TestValidateIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads and writes real deck directories on disk")
	}

	reg := mustBuiltins(t)

	t.Run("valid deck through the built-ins", func(t *testing.T) {
		dir := writeDeck(t, map[string]string{
			"eypres.yaml":         "title: Integration Deck\n",
			"slides/1-title.md":   builtinTitleExample,
			"slides/2-content.md": builtinContentExample,
		})
		verr, invalid := Validate(os.DirFS(dir), reg, exampleThemeRegistry())
		if invalid {
			t.Fatalf("Validate() invalid = true with %q, want a valid deck", Format(verr))
		}
		if !reflect.DeepEqual(verr, ValidationError{}) {
			t.Fatalf("Validate() error = %#v, want the zero ValidationError", verr)
		}
	})

	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "config error",
			files: map[string]string{
				"eypres.yaml":       "navigation: diagonal\n",
				"slides/1-title.md": builtinTitleExample,
			},
			want: `eypres.yaml:1: key "navigation": unknown navigation mode "diagonal" (valid values: default, linear, grid)`,
		},
		{
			name: "filename error",
			files: map[string]string{
				"eypres.yaml":         "title: T\n",
				"slides/notaslide.md": "not a slide\n",
			},
			want: "slides/notaslide.md: filename must be <number>[letter]-<label>.md",
		},
		{
			name: "frontmatter field error",
			files: map[string]string{
				"eypres.yaml":   "title: T\n",
				"slides/1-a.md": "---\ntemplate: content\n---\n\nbody.\n\n# columns\n\nA\n\n# columns\n\nB\n",
			},
			want: `slides/1-a.md › heading: required: field "heading" is required but missing — add a heading: value`,
		},
		{
			name: "body rule error",
			files: map[string]string{
				"eypres.yaml":   "title: T\n",
				"slides/1-a.md": "---\ntemplate: content\nheading: H\n---\n\none.\n\ntwo.\n\nthree.\n\n# columns\n\nA\n\n# columns\n\nB\n",
			},
			want: "slides/1-a.md:5 › body: max_paragraphs: the body has 3 paragraphs, maximum is 2 — shorten the body to at most 2 paragraphs",
		},
		{
			name: "inter-slide link error",
			files: map[string]string{
				"eypres.yaml":          "title: T\n",
				"slides/1-overview.md": "---\ntemplate: content\nheading: H\n---\n\nSee [x](#zzz).\n\n# columns\n\nA\n\n# columns\n\nB\n",
			},
			want: `slides/1-overview.md:6: unknown link label "zzz" — use the label of one of the deck's slides`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeDeck(t, tc.files)
			verr, invalid := Validate(os.DirFS(dir), reg, exampleThemeRegistry())
			if !invalid {
				t.Fatalf("Validate() invalid = false, want error %q", tc.want)
			}
			if got := Format(verr); got != tc.want {
				t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

// builtinTitleExample mirrors internal/assets/templates/title/example.md: a
// built-in title slide that must validate end to end.
const builtinTitleExample = `---
template: title
title: Quarterly Business Review
subtitle: Performance, outlook and priorities
date: 2026-09-25
date_format: long
---
# notes
Greet the audience, then hand over to the presenters.
`

// builtinContentExample mirrors internal/assets/templates/content/example.md: a
// built-in content slide with two composed columns sections.
const builtinContentExample = `---
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

// writeDeck writes one deck file tree to a fresh t.TempDir() and returns the
// directory. Keys are deck-relative slash paths (for example
// "slides/1-a.md"); parent directories are created as needed.
func writeDeck(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
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

// TestValidateIntegrationFixtureLibrary validates real on-disk decks against
// the phase-3 fixture project templates/ library (a copy of
// internal/template/testdata/library/templates), through
// template.LoadLibrary + template.NewRegistryFromLibrary and theme.LoadDir —
// not template.Builtins() — proving Validate's file-ordering and
// error-reporting positions work identically over a library-built registry.
// It reads and writes the real filesystem, so it is skipped under -short.
func TestValidateIntegrationFixtureLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads and writes real deck directories on disk")
	}

	t.Run("valid deck through the fixture library", func(t *testing.T) {
		dir := writeFixtureDeck(t, map[string]string{
			"eypres.yaml":       "title: Fixture Deck\ntheme: plain\n",
			"slides/1-hello.md": "---\ntemplate: hello\ntitle: Hi there\n---\n",
		})
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if invalid {
			t.Fatalf("Validate() invalid = true with %q, want a valid deck", Format(verr))
		}
		if !reflect.DeepEqual(verr, ValidationError{}) {
			t.Fatalf("Validate() error = %#v, want the zero ValidationError", verr)
		}
	})

	t.Run("missing required field is positioned on disk", func(t *testing.T) {
		dir := writeFixtureDeck(t, map[string]string{
			"eypres.yaml":       "title: Fixture Deck\ntheme: plain\n",
			"slides/1-hello.md": "---\ntemplate: hello\n---\n",
		})
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the missing title field to fail")
		}
		want := `slides/1-hello.md › title: required: field "title" is required but missing — add a title: value`
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})

	t.Run("unknown theme in deck config", func(t *testing.T) {
		dir := writeFixtureDeck(t, map[string]string{
			"eypres.yaml":       "title: Fixture Deck\ntheme: not-a-theme\n",
			"slides/1-hello.md": "---\ntemplate: hello\ntitle: Hi there\n---\n",
		})
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the unknown theme to fail")
		}
		want := `eypres.yaml:2: unknown theme "not-a-theme" (available themes: plain)`
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})
}

// writeFixtureDeck writes files (a deck's eypres.yaml and slides/) to a fresh
// t.TempDir(), then copies the phase-3 fixture templates/ library alongside
// them, so the deck has its own templates/ tree on disk.
func writeFixtureDeck(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := writeDeck(t, files)
	copyDir(t, filepath.Join("..", "template", "testdata", "library", "templates"), filepath.Join(dir, "templates"))
	return dir
}

// copyDir recursively copies the real directory src to dst.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s -> %s: %v", src, dst, err)
	}
}

// loadFixtureLibrary loads dir's own templates/ library into a registry and
// theme registry, through template.LoadLibrary + template.NewRegistryFromLibrary
// and theme.LoadDir.
func loadFixtureLibrary(t *testing.T, dir string) (*template.Registry, *theme.Registry) {
	t.Helper()
	fsys := os.DirFS(dir)
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	themes, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}
	return reg, themes
}
