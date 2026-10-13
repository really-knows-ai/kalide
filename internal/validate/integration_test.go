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

	// A whole-deck image-field existence failure: the `bg` slide template's
	// `background` field declares a `media` root (dir bgs, jpg extension), so
	// validate.checkFields resolves the authored path against the media/ tree
	// of the library layer that owns the field's template. A path naming no
	// file under that tree fails in the error-reporting format carrying the
	// offending path and the closest accepted candidate, while an existing
	// path validates (image-field-roots).
	t.Run("missing background image fails with the closest media candidate", func(t *testing.T) {
		files := imageLibraryFiles()
		files["kalide.yaml"] = "title: Image Deck\ntheme: plain\n"
		files["slides/1-bg.md"] = "---\ntemplate: bg\nbackground: bgs/right-88.jpg\n---\n\nBackground.\n"
		dir := writeDeck(t, files)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the missing background image to fail")
		}
		got := Format(verr)
		if !strings.Contains(got, "bgs/right-88.jpg") {
			t.Errorf("Validate() error = %q, want it to carry the offending path %q", got, "bgs/right-88.jpg")
		}
		if !strings.Contains(got, `did you mean "bgs/right-8.jpg"?`) {
			t.Errorf("Validate() error = %q, want the closest-match suggestion %q", got, `did you mean "bgs/right-8.jpg"?`)
		}
		want := displayFile("slides/1-bg.md") + ` › background: image: "bgs/right-88.jpg" does not exist — did you mean "bgs/right-8.jpg"?`
		if got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})

	t.Run("existing background image validates", func(t *testing.T) {
		files := imageLibraryFiles()
		files["kalide.yaml"] = "title: Image Deck\ntheme: plain\n"
		files["slides/1-bg.md"] = "---\ntemplate: bg\nbackground: bgs/right-8.jpg\n---\n\nBackground.\n"
		dir := writeDeck(t, files)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if invalid {
			t.Fatalf("Validate() invalid = true with %q, want a valid deck", Format(verr))
		}
		if !reflect.DeepEqual(verr, ValidationError{}) {
			t.Fatalf("Validate() error = %#v, want the zero ValidationError", verr)
		}
	})

	// A whole-deck enum field whose variants come from a `variants_from:` glob:
	// the registry Validate is handed already expanded the glob against the
	// owning layer (NewRegistryFromLibrary, enum-variants-from, feedback-53
	// regression), so a slide authoring an expanded media-relative variant
	// validates while a non-member fails with the closest suggestion.
	t.Run("expanded enum variant validates whole-deck", func(t *testing.T) {
		files := variantsLibraryFiles()
		files["kalide.yaml"] = "title: Variants Deck\ntheme: plain\n"
		files["slides/1-bg.md"] = "---\ntemplate: bg\nbackground: bgs/right-8.jpg\neffect: bgs/right-9.jpg\n---\n\nBackground.\n"
		dir := writeDeck(t, files)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if invalid {
			t.Fatalf("Validate() invalid = true with %q, want the expanded variant to validate", Format(verr))
		}
		if !reflect.DeepEqual(verr, ValidationError{}) {
			t.Fatalf("Validate() error = %#v, want the zero ValidationError", verr)
		}
	})

	t.Run("enum non-member fails whole-deck with the closest suggestion", func(t *testing.T) {
		files := variantsLibraryFiles()
		files["kalide.yaml"] = "title: Variants Deck\ntheme: plain\n"
		files["slides/1-bg.md"] = "---\ntemplate: bg\nbackground: bgs/right-8.jpg\neffect: bgs/right-88.jpg\n---\n\nBackground.\n"
		dir := writeDeck(t, files)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the non-member enum to fail")
		}
		want := displayFile("slides/1-bg.md") + ` › effect: enum: "bgs/right-88.jpg" is not one of the allowed variants (bgs/right-8.jpg, bgs/right-9.jpg) — did you mean "bgs/right-8.jpg"? use one of: bgs/right-8.jpg, bgs/right-9.jpg`
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
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
		// LibraryError.Path is a root-joined display path: it is shown in the
		// host OS's native spelling (native-display-paths).
		if want := displayFile(template.TemplatesDir + "/sections/group/example.md"); libErr.Path != want {
			t.Errorf("LibraryError.Path = %q, want %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, "blocks[0]") {
			t.Errorf("LibraryError.Message = %q, want the child containment path %q", libErr.Message, "blocks[0]")
		}
		if !strings.Contains(libErr.Message, `unknown field "lable"`) {
			t.Errorf("LibraryError.Message = %q, want the nested unknown-field violation", libErr.Message)
		}
	})

	// Deck-level nested errors through Validate carry the full containment
	// chain in the canonical ` › ` form (nested-section-validation). The
	// earlier slides are valid, so the fail-fast walk reaches the third
	// slide (slides/3-columns.md).

	t.Run("nested field error carries the full instance chain", func(t *testing.T) {
		dir := writeNestedErrorDeck(t, nestedFieldErrorSlide)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the nested unknown-field error")
		}
		// A field error retains its field segment and carries the enclosing
		// instance chain. Field errors carry no source line (the parser does
		// not retain per-value YAML positions), so there is no :line suffix
		// (whole-deck-validation).
		want := displayFile("slides/3-columns.md") + ` › columns[2] › blocks[1] › lable: unknown field "lable" — did you mean "label"?`
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})

	t.Run("nested max violation carries the aggregate parent path", func(t *testing.T) {
		dir := writeNestedErrorDeck(t, nestedMaxErrorSlide)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the nested max error")
		}
		// The max is counted per parent instance: the error is positioned on
		// the offending child heading and its path is the parent instance's
		// chain extended by the bare child name, without a child index.
		want := displayFile("slides/3-columns.md") + `:48 › columns[2] › blocks: section "blocks": at most 2 allowed, found 3`
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})

	t.Run("nested body-rule violation carries the full containment path", func(t *testing.T) {
		dir := writeNestedErrorDeck(t, nestedBodyRuleErrorSlide)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the nested body-rule error")
		}
		// The block child's body exceeds the block template's implied
		// max_paragraphs, so the body rule is reported as the implied `body`
		// field with the enclosing instance chain prepended: the nested
		// instance's `blocks[0]` under `columns[0]` (nested-section-validation).
		want := displayFile("slides/3-columns.md") + `:17 › columns[0] › blocks[0] › body: max_paragraphs: the body has 2 paragraphs, maximum is 1 — shorten the body to at most 1 paragraphs`
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
		}
	})

	t.Run("nested Markdown issue carries the full containment path", func(t *testing.T) {
		dir := writeNestedErrorDeck(t, nestedMarkdownErrorSlide)
		reg, themes := loadFixtureLibrary(t, dir)
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("Validate() invalid = false, want the nested Markdown-subset error")
		}
		// A disallowed Markdown construct in the block child's body is
		// positioned in the body and carries the enclosing instance chain as
		// its path, with no `body` segment (the issue is the section's body
		// itself): `columns[0] › blocks[0]` (nested-section-validation).
		want := displayFile("slides/3-columns.md") + `:17 › columns[0] › blocks[0]: block quotes are not allowed — remove the > quote markers and keep the text in the body`
		if got := Format(verr); got != want {
			t.Fatalf("Validate() error =\n  %q\nwant\n  %q", got, want)
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
			want: displayFile("kalide.yaml") + `:1: key "navigation": unknown navigation mode "diagonal" (valid values: default, linear, grid)`,
		},
		{
			name: "filename error",
			files: map[string]string{
				"kalide.yaml":         "title: T\n",
				"slides/notaslide.md": "not a slide\n",
			},
			want: displayFile("slides/notaslide.md") + ": filename must be <number>[letter]-<label>.md",
		},
		{
			name: "frontmatter field error",
			files: map[string]string{
				"kalide.yaml":   "title: T\n",
				"slides/1-a.md": "---\ntemplate: content\n---\n\nbody.\n\n# columns\n\nA\n\n# columns\n\nB\n",
			},
			want: displayFile("slides/1-a.md") + ` › heading: required: field "heading" is required but missing — add a heading: value`,
		},
		{
			name: "body rule error",
			files: map[string]string{
				"kalide.yaml":   "title: T\n",
				"slides/1-a.md": "---\ntemplate: content\nheading: H\n---\n\none.\n\ntwo.\n\nthree.\n\n# columns\n\nA\n\n# columns\n\nB\n",
			},
			want: displayFile("slides/1-a.md") + ":5 › body: max_paragraphs: the body has 3 paragraphs, maximum is 2 — shorten the body to at most 2 paragraphs",
		},
		{
			name: "inter-slide link error",
			files: map[string]string{
				"kalide.yaml":          "title: T\n",
				"slides/1-overview.md": "---\ntemplate: content\nheading: H\n---\n\nSee [x](#zzz).\n\n# columns\n\nA\n\n# columns\n\nB\n",
			},
			want: displayFile("slides/1-overview.md") + `:6: unknown link label "zzz" — use the label of one of the deck's slides`,
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

	// Collect-all end to end over a real on-disk multi-error deck. The fixed
	// order is config -> slide filenames -> slides top-to-bottom -> inter-slide
	// links. A deck cannot carry both a filename fault and slide faults —
	// deck.LoadSlides yields no deck on a malformed filename, so the slide and
	// link passes have nothing to walk (the unit testOrdering covers that pair) —
	// so this deck pairs a config fault with slide faults. Within the slide step
	// the errors read top-to-bottom (slide 1's three frontmatter-field errors
	// before slide 2's body rule), and the `#label` link error is last, after
	// every slide.
	//
	// Every error is asserted twice: its exact struct fields (File deck-relative
	// forward-slash identity, Line, Path, What, Fix) and its validate.Format text
	// (native display path). The suggestion lives only in Fix, the sole
	// "did you mean" carrier — there is no structured suggestion field.
	t.Run("collect-all over a multi-error deck", func(t *testing.T) {
		files := multiErrorLibraryFiles()
		files["kalide.yaml"] = "title: Multi Error Deck\ntheme: plain\nnavigation: diagonal\n"
		files["slides/1-bg.md"] = "---\ntemplate: bg\nbackground: bgs/right-88.jpg\neffect: bgs/right-88.jpg\n---\n\nA simple body.\n"
		files["slides/2-body.md"] = "---\ntemplate: bg\nheading: Two\nbackground: bgs/right-8.jpg\neffect: bgs/right-9.jpg\n---\n\nFirst paragraph with [x](#zzz).\n\nSecond paragraph.\n"
		dir := writeDeck(t, files)
		reg, themes := loadFixtureLibrary(t, dir)

		want := []ValidationError{
			New("kalide.yaml", 3, nil,
				`key "navigation": unknown navigation mode "diagonal" (valid values: default, linear, grid)`, ""),
			New("slides/1-bg.md", 0, []string{"heading"},
				`required: field "heading" is required but missing`, "add a heading: value"),
			New("slides/1-bg.md", 0, []string{"background"},
				`image: "bgs/right-88.jpg" does not exist`, `did you mean "bgs/right-8.jpg"?`),
			New("slides/1-bg.md", 0, []string{"effect"},
				`enum: "bgs/right-88.jpg" is not one of the allowed variants (bgs/right-8.jpg, bgs/right-9.jpg)`,
				`did you mean "bgs/right-8.jpg"? use one of: bgs/right-8.jpg, bgs/right-9.jpg`),
			New("slides/2-body.md", 7, []string{"body"},
				`max_paragraphs: the body has 2 paragraphs, maximum is 1`, `shorten the body to at most 1 paragraphs`),
			New("slides/2-body.md", 8, nil,
				`unknown link label "zzz"`, `use the label of one of the deck's slides`),
		}

		all := ValidateAll(os.DirFS(dir), reg, themes)
		if len(all) != len(want) {
			t.Fatalf("ValidateAll() returned %d error(s), want %d:\n  %q", len(all), len(want), formatAll(all))
		}
		for i, e := range all {
			if !reflect.DeepEqual(e, want[i]) {
				t.Errorf("ValidateAll()[%d] =\n  %#v\nwant\n  %#v", i, e, want[i])
			}
			if got, w := Format(e), Format(want[i]); got != w {
				t.Errorf("ValidateAll()[%d] Format =\n  %q\nwant\n  %q", i, got, w)
			}
			// The stored File is the deck-relative forward-slash identity, never
			// the host OS's native spelling (native-display-paths).
			if filepath.IsAbs(e.File) {
				t.Errorf("ValidateAll()[%d].File = %q, want a deck-relative path", i, e.File)
			}
		}

		// The fail-fast entry reports exactly the first error of that order.
		verr, invalid := Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatalf("Validate() invalid = false, want the earliest error %q", Format(want[0]))
		}
		if !reflect.DeepEqual(verr, all[0]) {
			t.Fatalf("Validate() error =\n  %#v\nwant ValidateAll()[0]\n  %#v", verr, all[0])
		}
	})

	// The collect-all entry returns no errors for a sound on-disk deck carrying
	// the same multi-error library, including a valid inter-slide `#label` link.
	t.Run("collect-all valid deck returns no errors", func(t *testing.T) {
		files := multiErrorLibraryFiles()
		files["kalide.yaml"] = "title: Valid Deck\ntheme: plain\n"
		files["slides/1-bg.md"] = "---\ntemplate: bg\nheading: One\nbackground: bgs/right-8.jpg\neffect: bgs/right-8.jpg\n---\n\nA simple body.\n"
		files["slides/2-more.md"] = "---\ntemplate: bg\nheading: Two\nbackground: bgs/right-9.jpg\neffect: bgs/right-9.jpg\n---\n\nBack to [one](#bg).\n"
		dir := writeDeck(t, files)
		reg, themes := loadFixtureLibrary(t, dir)

		if all := ValidateAll(os.DirFS(dir), reg, themes); len(all) != 0 {
			t.Fatalf("ValidateAll() = %q, want no errors for a valid deck", formatAll(all))
		}
		if verr, invalid := Validate(os.DirFS(dir), reg, themes); invalid {
			t.Fatalf("Validate() invalid = true with %q, want a valid deck", Format(verr))
		}
	})
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

// nestedValidSlide is a valid content slide with one columns instance holding
// one block child; it stands in for the earlier slides of the nested-error
// decks below, so Validate fails fast on the third slide.
const nestedValidSlide = `---
template: content
heading: One
---

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `
`

// nestedFieldErrorSlide is a valid deck's third slide except that its third
// columns instance (columns[2]) has a second block (blocks[1]) carrying an
// unknown field `lable`. It pins the full instance chain a nested field error
// carries.
const nestedFieldErrorSlide = `---
template: content
heading: Field
---

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `

## blocks
` + "```" + `
template: block
lable: bad
` + "```" + `
`

// nestedMaxErrorSlide is a valid deck's third slide except that its third
// columns instance (columns[2]) holds three blocks, one over the container's
// per-parent maximum of two. The third block's heading is on slide line 48.
const nestedMaxErrorSlide = `---
template: content
heading: Max
---

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `

## blocks
` + "```" + `
template: block
label: b
` + "```" + `

## blocks
` + "```" + `
template: block
label: c
` + "```" + `
`

// nestedBodyRuleErrorSlide is a valid deck's third slide except that its
// nested block child (blocks[0]) has a two-paragraph body, one over the block
// template's implied max_paragraphs of one. It pins the full containment path a
// nested body-rule violation carries: `columns[0] › blocks[0] › body`.
const nestedBodyRuleErrorSlide = `---
template: content
heading: Body rule
---

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `

first paragraph.

second paragraph.
`

// nestedMarkdownErrorSlide is a valid deck's third slide except that its
// nested block child (blocks[0]) has a block quote in its body, a construct
// outside the accepted Markdown subset. It pins the containment path a nested
// Markdown-subset issue carries: `columns[0] › blocks[0]`.
const nestedMarkdownErrorSlide = `---
template: content
heading: Markdown
---

# columns
` + "```" + `
template: group
title: G
` + "```" + `

## blocks
` + "```" + `
template: block
label: a
` + "```" + `
> quoted text
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

		"templates/sections/block/template.yaml":    "description: a leaf block\nfields:\n  - name: label\n    type: text\nbody:\n  mode: optional\n  max_paragraphs: 1\n",
		"templates/sections/block/layout.html.tmpl": "<span class=\"block\">{{.label}}</span>",
		"templates/sections/block/example.md":       "```\nlabel: first\n```\n",
	}
}

// imageLibraryFiles returns an on-disk templates/ library whose `bg` slide
// template declares an image field `background` with a `media` root, a `bgs/`
// dir filter and a `jpg` extension filter, plus the media/ tree those options
// resolve in (image-field-roots). Its example names an existing media-relative
// file, so the library loads; a deck slide can then author either an existing
// or a missing background path through the whole-deck validator.
func imageLibraryFiles() map[string]string {
	return map[string]string{
		"templates/library.yaml":           "name: image-fixture\nformat: 1\n",
		"templates/themes/plain/theme.css": "body { margin: 0; }\n",

		"templates/slides/bg/template.yaml": "description: a background image slide\n" +
			"fields:\n  - name: background\n    type: image\n    roots:\n      - media\n    dir: bgs\n    extensions:\n      - jpg\n" +
			"body:\n  mode: optional\n",
		"templates/slides/bg/layout.html.tmpl": `<section style="background-image: url({{ .background }})"></section>`,
		"templates/slides/bg/example.md":       "---\ntemplate: bg\nbackground: bgs/right-8.jpg\n---\n\nA slide.\n",

		"templates/media/bgs/right-8.jpg": "8",
		"templates/media/bgs/right-9.jpg": "9",
	}
}

// variantsLibraryFiles extends imageLibraryFiles' `bg` slide with an enum field
// `effect` whose variants come from a `variants_from:` glob over the same media
// tree, so a whole-deck validation can author an expanded media-relative variant
// and prove the registry the validator is handed resolved the glob against the
// owning layer (enum-variants-from, feedback-53 regression). The example names
// an expanded variant, so the library's own example validation still passes.
func variantsLibraryFiles() map[string]string {
	files := imageLibraryFiles()
	files["templates/slides/bg/template.yaml"] = "description: a background image slide\n" +
		"fields:\n  - name: background\n    type: image\n    roots:\n      - media\n    dir: bgs\n    extensions:\n      - jpg\n" +
		"  - name: effect\n    type: enum\n    variants_from: media/bgs/*.jpg\n" +
		"body:\n  mode: optional\n"
	files["templates/slides/bg/example.md"] = "---\ntemplate: bg\nbackground: bgs/right-8.jpg\neffect: bgs/right-8.jpg\n---\n\nA slide.\n"
	return files
}

// multiErrorLibraryFiles extends variantsLibraryFiles' `bg` slide template with a
// required `heading` text field and a `max_paragraphs: 1` body rule, so one deck
// can exercise the whole collect-all error set at once: a required field missing,
// a missing media-root image with a closest-match fix, an expanded
// `variants_from:` enum non-member with a closest-match fix, and a body-rule
// violation. Its example names an existing media file, an expanded variant and a
// single-paragraph body, so the library itself still loads.
func multiErrorLibraryFiles() map[string]string {
	files := variantsLibraryFiles()
	files["templates/slides/bg/template.yaml"] = "description: a slide with a required heading, an image and an enum\n" +
		"fields:\n" +
		"  - name: heading\n    type: text\n    required: true\n" +
		"  - name: background\n    type: image\n    roots:\n      - media\n    dir: bgs\n    extensions:\n      - jpg\n" +
		"  - name: effect\n    type: enum\n    variants_from: media/bgs/*.jpg\n" +
		"body:\n  mode: optional\n  max_paragraphs: 1\n"
	files["templates/slides/bg/example.md"] = "---\ntemplate: bg\nheading: Example\nbackground: bgs/right-8.jpg\neffect: bgs/right-8.jpg\n---\n\nAn example slide.\n"
	return files
}

// writeNestedErrorDeck writes a deck whose templates/ is the nested fixture
// library and whose third slide is thirdSlide (slides/3-columns.md). The
// first two slides are valid, so Validate's fail-fast, slide-ordered walk
// reaches the third slide's nested error.
func writeNestedErrorDeck(t *testing.T, thirdSlide string) string {
	t.Helper()
	files := nestedLibraryFiles()
	files["kalide.yaml"] = "title: Nested Error Deck\ntheme: plain\n"
	files["slides/1-a.md"] = nestedValidSlide
	files["slides/2-b.md"] = nestedValidSlide
	files["slides/3-columns.md"] = thirdSlide
	return writeDeck(t, files)
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
		want := displayFile("slides/1-hello.md") + ` › title: required: field "title" is required but missing — add a title: value`
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
