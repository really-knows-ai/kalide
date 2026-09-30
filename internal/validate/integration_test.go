package validate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// TestValidateIntegration runs real on-disk decks through the whole validator
// end to end: a temp deck directory (os.DirFS) read by the deck loaders, the
// slide parser, internal/mdcheck, internal/template.CheckValues/CheckBody and
// the link pass, all against mustBuiltins' fixture library-loaded registry
// (title/content/column). It reads and writes the real filesystem, so it is
// skipped under -short.
//
// It pins the exact formatted first error (validate.Format) for one
// representative deck per failure class — config, filename, frontmatter field,
// body rule and inter-slide link — in the fixed fail-fast order, plus the zero
// ValidationError for a valid deck that mirrors the fixture registry's
// example content.
func TestValidateIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads and writes real deck directories on disk")
	}

	reg := mustBuiltins(t)

	t.Run("valid deck through the built-ins", func(t *testing.T) {
		dir := writeDeck(t, map[string]string{
			"kalide.yaml":         "title: Integration Deck\n",
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

	// Heading-depth nesting and recursive example validation against a real
	// on-disk library: the fixture library is written to the temp dir's
	// templates/ as a container section template (group) holding block
	// children, so template.LoadLibrary's step-7 check validates the nested
	// example.md recursively (template-build-checks, nested-section-
	// validation) and the parser nests a deck's sections by heading depth
	// (slide-sections).
	t.Run("nested container library loads with recursive example validation", func(t *testing.T) {
		dir := writeDeck(t, nestedLibraryFiles())
		// loadFixtureLibrary fails the test when LoadLibrary reports a
		// nested example violation, so reaching here proves the container
		// example validated recursively.
		loadFixtureLibrary(t, dir)
	})

	t.Run("heading-depth nested deck validates end to end", func(t *testing.T) {
		files := nestedLibraryFiles()
		files["kalide.yaml"] = "title: Nested Deck\ntheme: plain\n"
		files["slides/1-nested.md"] = nestedDeckSlide
		dir := writeDeck(t, files)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if invalid {
			t.Fatalf("Validate() invalid = true with %q, want a valid nested deck", Format(verr))
		}
	})

	t.Run("demo library parses one heading level and validates", func(t *testing.T) {
		demoDir := filepath.Join("..", "..", "examples", "demo")
		if _, err := os.Stat(demoDir); err != nil {
			t.Skipf("examples/demo not found: %v", err)
		}
		fsys := os.DirFS(demoDir)
		// Loading the demo library recursively validates its examples.
		lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
		if err != nil {
			t.Fatalf("template.LoadLibrary(examples/demo): %v", err)
		}
		reg, err := template.NewRegistryFromLibrary(lib)
		if err != nil {
			t.Fatalf("template.NewRegistryFromLibrary: %v", err)
		}
		themes, err := theme.LoadDir(fsys, filepath.Join(template.TemplatesDir, template.ThemesDir))
		if err != nil {
			t.Fatalf("theme.LoadDir: %v", err)
		}

		// The demo's team slide is one heading level deep: its content
		// template declares `columns` at level 1, with no deeper instances.
		srcBytes, err := os.ReadFile(filepath.Join(demoDir, "slides", "3-team.md"))
		if err != nil {
			t.Fatalf("read demo slide: %v", err)
		}
		ps, err := slide.Parse("slides/3-team.md", srcBytes, reg)
		if err != nil {
			t.Fatalf("slide.Parse(demo team slide): %v", err)
		}
		if len(ps.Sections) != 2 {
			t.Fatalf("len(demo team sections) = %d, want 2", len(ps.Sections))
		}
		for i := range ps.Sections {
			sec := ps.Sections[i]
			if sec.Name != "columns" || sec.Level != 1 || sec.Index != i {
				t.Errorf("Sections[%d] = %q level %d index %d, want %q level 1 index %d", i, sec.Name, sec.Level, sec.Index, "columns", i)
			}
			if len(sec.Children) != 0 {
				t.Errorf("Sections[%d].Children = %+v, want none (the demo is one level)", i, sec.Children)
			}
		}

		verr, invalid := Validate(fsys, reg, themes)
		if invalid {
			t.Fatalf("Validate(examples/demo) invalid = true with %q", Format(verr))
		}
	})

	t.Run("broken nested example fails at load with the containment path", func(t *testing.T) {
		files := nestedLibraryFiles()
		// The nested `block` child of the container's own example names an
		// unknown field, so the recursive example validation must fail at
		// load with the child instance's containment path (`blocks[0]`),
		// positioned at the container's example.md.
		files["templates/sections/group/example.md"] = "# blocks\n```\ntemplate: block\nlable: first\n```\n"
		dir := writeDeck(t, files)

		_, err := template.LoadLibrary(os.DirFS(dir), template.TemplatesDir)
		if err == nil {
			t.Fatal("LoadLibrary() error = nil, want the nested example violation")
		}
		var libErr *template.LibraryError
		if !errors.As(err, &libErr) {
			t.Fatalf("LoadLibrary() error = %T (%v), want *template.LibraryError", err, err)
		}
		if want := template.TemplatesDir + "/sections/group/example.md"; libErr.Path != want {
			t.Errorf("LibraryError.Path = %q, want %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, "blocks[0]") {
			t.Errorf("LibraryError.Message = %q, want the child containment path %q", libErr.Message, "blocks[0]")
		}
		if !strings.Contains(libErr.Message, `unknown field "lable"`) {
			t.Errorf("LibraryError.Message = %q, want the nested unknown-field violation", libErr.Message)
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
				"kalide.yaml":       "navigation: diagonal\n",
				"slides/1-title.md": builtinTitleExample,
			},
			want: `kalide.yaml:1: key "navigation": unknown navigation mode "diagonal" (valid values: default, linear, grid)`,
		},
		{
			name: "filename error",
			files: map[string]string{
				"kalide.yaml":         "title: T\n",
				"slides/notaslide.md": "not a slide\n",
			},
			want: "slides/notaslide.md: filename must be <number>[letter]-<label>.md",
		},
		{
			name: "frontmatter field error",
			files: map[string]string{
				"kalide.yaml":   "title: T\n",
				"slides/1-a.md": "---\ntemplate: content\n---\n\nbody.\n\n# columns\n\nA\n\n# columns\n\nB\n",
			},
			want: `slides/1-a.md › heading: required: field "heading" is required but missing — add a heading: value`,
		},
		{
			name: "body rule error",
			files: map[string]string{
				"kalide.yaml":   "title: T\n",
				"slides/1-a.md": "---\ntemplate: content\nheading: H\n---\n\none.\n\ntwo.\n\nthree.\n\n# columns\n\nA\n\n# columns\n\nB\n",
			},
			want: "slides/1-a.md:5 › body: max_paragraphs: the body has 3 paragraphs, maximum is 2 — shorten the body to at most 2 paragraphs",
		},
		{
			name: "inter-slide link error",
			files: map[string]string{
				"kalide.yaml":          "title: T\n",
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

// builtinTitleExample mirrors mustBuiltins' fixture title example.md: a
// fixture title slide that must validate end to end.
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

// builtinContentExample mirrors mustBuiltins' fixture content example.md: a
// fixture content slide with two composed columns sections.
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

// nestedDeckSlide is a deck slide whose sections nest by heading depth: a
// top-level `# columns` instance resolves to the container template `group`,
// and its `## blocks` children resolve to `block` (slide-sections).
const nestedDeckSlide = `---
template: content
heading: Nested heading
---

# columns
` + "```" + `
template: group
title: Group one
` + "```" + `

## blocks
` + "```" + `
template: block
label: first
` + "```" + `

## blocks
` + "```" + `
template: block
label: second
` + "```" + `
`

// nestedLibraryFiles returns a complete, otherwise-valid on-disk templates/
// library with a container section template (`group`) that declares `blocks`
// children accepting `block`, plus a `content` slide template that declares
// `columns` accepting `group`. Its group/example.md nests a block child, so
// template.LoadLibrary's recursive example validation parses the example by
// heading depth (template-build-checks, nested-section-validation).
func nestedLibraryFiles() map[string]string {
	return map[string]string{
		"templates/library.yaml":           "name: nested-fixture\nformat: 1\n",
		"templates/themes/plain/theme.css": "body { margin: 0; }\n",

		"templates/slides/content/template.yaml": "description: content with a nested container section\n" +
			"fields:\n  - name: heading\n    type: text\n    required: true\n" +
			"sections:\n  - name: columns\n    accepted: [group]\n" +
			"body:\n  mode: optional\n",
		"templates/slides/content/layout.html.tmpl": "<section>{{.heading}}{{range .columns}}<div class=\"col\">{{.}}</div>{{end}}</section>",
		"templates/slides/content/example.md": "---\ntemplate: content\nheading: Nested\n---\n\n" +
			"# columns\n```\ntemplate: group\ntitle: Group one\n```\n\n" +
			"## blocks\n```\ntemplate: block\nlabel: first\n```\n",

		"templates/sections/group/template.yaml": "description: a container of block children\n" +
			"fields:\n  - name: title\n    type: text\n" +
			"sections:\n  - name: blocks\n    accepted: [block]\n    min: 1\n    max: 2\n" +
			"body:\n  mode: optional\n",
		"templates/sections/group/layout.html.tmpl": "<div class=\"group\">{{.title}}{{range .blocks}}<div class=\"block\">{{.}}</div>{{end}}</div>",
		"templates/sections/group/example.md":       "# blocks\n```\ntemplate: block\nlabel: first\n```\n",

		"templates/sections/block/template.yaml":    "description: a leaf block\nfields:\n  - name: label\n    type: text\nbody:\n  mode: optional\n",
		"templates/sections/block/layout.html.tmpl": "<span class=\"block\">{{.label}}</span>",
		"templates/sections/block/example.md":       "```\nlabel: first\n```\n",
	}
}

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
			"kalide.yaml":       "title: Fixture Deck\ntheme: plain\n",
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
			"kalide.yaml":       "title: Fixture Deck\ntheme: plain\n",
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
			"kalide.yaml":       "title: Fixture Deck\ntheme: not-a-theme\n",
			"slides/1-hello.md": "---\ntemplate: hello\ntitle: Hi there\n---\n",
		})
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the unknown theme to fail")
		}
		want := `kalide.yaml:2: unknown theme "not-a-theme" (available themes: plain)`
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})
}

// writeFixtureDeck writes files (a deck's kalide.yaml and slides/) to a fresh
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
