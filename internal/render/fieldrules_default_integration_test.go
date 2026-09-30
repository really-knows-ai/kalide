package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// This file is the render-pipeline half of the integration-test deliverable for
// section-helpers/plan.phase-07.task-2 (field-rules): an omitted required field
// that declares a Default is APPLIED through the real
// library->parser->validate->render pipeline at the slide frontmatter and at a
// nested section instance; an author-supplied value WINS over the default; and
// an undefaulted omitted required field still fails the pipeline's
// internal/validate.checkFields step.
//
// It complements the load-time example-context half in
// internal/template/fieldrules_default_int_test.go and the existing render-time
// section-helper unit cases (internal/render/sectionhelper_test.go): those pin
// ResolveSectionCall's defaults-first rule for a helper CALL, while this test
// pins an authored slide's and an authored section INSTANCE's own omitted
// default as the real renderer materialises it (renderer.values at
// renderSection/slideData). It reads a real deck directory, so it is skipped
// under `go test -short`.

const (
	// defaultRenderHostManifest is a slide template whose required `heading`
	// carries a Default and whose `items` child section accepts `rprobe`.
	defaultRenderHostManifest = `name: rhost
description: render default-probe host
fields:
  - name: heading
    type: text
    required: true
    default: HEAD-DEF
sections:
  - name: items
    accepted:
      - rprobe
    max: 4
body:
  mode: optional
`

	// defaultRenderHostLayout renders the slide's own defaulted `heading` beside
	// the authored `items` instances as trusted HTML. (It deliberately does not
	// pass a converted field into a `{{ section }}` call: a converted text field
	// is html/template.HTML, not a string, so the helper's string-field
	// validation would reject it; the render-time helper's own defaults are
	// pinned by sectionhelper_test.go's TestRenderSlideSectionHelperDefaults.)
	defaultRenderHostLayout = `<section class="rhost">` +
		`<h1 class="rhost-heading">{{ .heading }}</h1>` +
		`{{ range .items }}{{ . }}{{ end }}` +
		`</section>`

	// defaultRenderHostExample omits the defaulted `heading` and the nested
	// instance's defaulted `label`, so LoadLibrary's step-7 example execution
	// depends on both defaults materialising.
	defaultRenderHostExample = "---\ntemplate: rhost\n---\n" +
		"# items\n" +
		"```\ntemplate: rprobe\ntarget: T1\n```\n"

	// defaultRenderSectionManifest declares a required `label` with a Default
	// and a required `target` with none.
	defaultRenderSectionManifest = `name: rprobe
description: render default-probe section
fields:
  - name: label
    type: text
    required: true
    default: PROBE-DEF
  - name: target
    type: text
    required: true
body:
  mode: disallowed
`

	defaultRenderSectionLayout = `<span class="rprobe" data-label="{{ .label }}" data-target="{{ .target }}"></span>`

	defaultRenderSectionExample = "```\ntarget: T1\n```\n"

	// defaultRenderStrictManifest declares a required field with NO Default: used
	// to prove an undefaulted omission still fails checkFields through the
	// pipeline.
	defaultRenderStrictManifest = `name: rstrict
description: render undefaulted-required probe
fields:
  - name: must
    type: text
    required: true
body:
  mode: disallowed
`

	defaultRenderStrictLayout = `<section class="rstrict">{{ .must }}</section>`

	defaultRenderStrictExample = "---\ntemplate: rstrict\nmust: Present\n---\n"
)

// defaultRenderOmitSlide is a real deck slide that omits the slide's defaulted
// `heading` and the nested `rprobe` instance's defaulted `label`.
const defaultRenderOmitSlide = "---\ntemplate: rhost\n---\n" +
	"# items\n" +
	"```\ntemplate: rprobe\ntarget: T1\n```\n"

// defaultRenderGivenSlide supplies both values, so the default must NOT win.
const defaultRenderGivenSlide = "---\ntemplate: rhost\nheading: GIVEN\n---\n" +
	"# items\n" +
	"```\ntemplate: rprobe\nlabel: LABEL2\ntarget: T2\n```\n"

// TestFieldRulesDefaultRenderPipelineInt drives an omitted required field that
// declares a Default through the real pipeline. It asserts the default is
// applied to the slide frontmatter and to a nested section instance, and that an
// author-supplied value wins over it at both depths (field-rules).
func TestFieldRulesDefaultRenderPipelineInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writeDefaultsRenderDeck(t, map[string]string{
		"1-omit.md":  defaultRenderOmitSlide,
		"2-given.md": defaultRenderGivenSlide,
	})

	page := renderDefaultsDeck(t, dir)

	// The slide that omitted both defaults: the slide layout's `heading` is the
	// default, and the authored nested instance's `label` is the default (its
	// supplied `target` is untouched).
	for _, want := range []string{
		`<h1 class="rhost-heading">HEAD-DEF</h1>`,
		`<span class="rprobe" data-label="PROBE-DEF" data-target="T1"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("omitted-default slide does not contain %q:\n%s", want, page)
		}
	}

	// The slide that supplied values: the author-supplied `heading` and the
	// nested instance's supplied `label` both WIN over the defaults.
	for _, want := range []string{
		`<h1 class="rhost-heading">GIVEN</h1>`,
		`<span class="rprobe" data-label="LABEL2" data-target="T2"></span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("author-supplied slide does not contain %q:\n%s", want, page)
		}
	}

	// Each value appears exactly once, so neither default bled into the
	// author-supplied slide (or vice versa): a supplied value replaces the
	// default rather than joining it.
	for _, want := range []struct {
		fragment string
		count    int
	}{
		{`class="rhost-heading">HEAD-DEF<`, 1},
		{`class="rhost-heading">GIVEN<`, 1},
		{`data-label="PROBE-DEF" data-target="T1"`, 1},
		{`data-label="LABEL2" data-target="T2"`, 1},
	} {
		if got := strings.Count(page, want.fragment); got != want.count {
			t.Errorf("page contains %q %d time(s), want %d:\n%s", want.fragment, got, want.count, page)
		}
	}
}

// TestFieldRulesUndefaultedRenderPipelineInt proves the other half of the rule
// at the pipeline's field-check step: an omitted required field with NO Default
// still fails internal/validate.checkFields, for a slide's own frontmatter and
// for a nested section instance. It reads a real deck directory, so it is
// skipped under `go test -short`.
func TestFieldRulesUndefaultedRenderPipelineInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	t.Run("a slide frontmatter omitting an undefaulted required field", func(t *testing.T) {
		dir := writeDefaultsRenderDeck(t, map[string]string{
			"1-missing.md": "---\ntemplate: rstrict\n---\n",
		})
		_, reg, themes := loadDefaultsRenderDeck(t, dir)
		verr, invalid := validate.Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("validate.Validate accepted a slide omitting an undefaulted required field")
		}
		got := validate.Format(verr)
		for _, want := range []string{"slides/1-missing.md", "must", "required"} {
			if !strings.Contains(got, want) {
				t.Errorf("validate error = %q, want it to contain %q", got, want)
			}
		}
	})

	t.Run("a nested section instance omitting an undefaulted required field", func(t *testing.T) {
		dir := writeDefaultsRenderDeck(t, map[string]string{
			"1-nested.md": "---\ntemplate: rhost\nheading: H\n---\n" +
				"# items\n" +
				"```\ntemplate: rprobe\n```\n",
		})
		_, reg, themes := loadDefaultsRenderDeck(t, dir)
		verr, invalid := validate.Validate(os.DirFS(dir), reg, themes)
		if !invalid {
			t.Fatal("validate.Validate accepted a section instance omitting an undefaulted required field")
		}
		got := validate.Format(verr)
		for _, want := range []string{"slides/1-nested.md", "items[0]", "target", "required"} {
			if !strings.Contains(got, want) {
				t.Errorf("validate error = %q, want it to contain %q", got, want)
			}
		}
	})
}

// writeDefaultsRenderDeck writes a fresh temporary deck directory: kalide.yaml,
// the given slides under slides/, a copy of the fixture templates/ library, and
// the default-probe slide/section templates.
func writeDefaultsRenderDeck(t *testing.T, slides map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"kalide.yaml": "title: Defaults Deck\ntheme: plain\n"}
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
	writeProbeTemplate(t, dir, "slides", "rhost", defaultRenderHostManifest, defaultRenderHostLayout, defaultRenderHostExample)
	writeProbeTemplate(t, dir, "sections", "rprobe", defaultRenderSectionManifest, defaultRenderSectionLayout, defaultRenderSectionExample)
	writeProbeTemplate(t, dir, "slides", "rstrict", defaultRenderStrictManifest, defaultRenderStrictLayout, defaultRenderStrictExample)
	return dir
}

// loadDefaultsRenderDeck loads one temporary deck directory through the real
// library loader and theme registry, returning the loaded library, the registry
// built from it and the theme registry. It fails the test on any load error, so
// reaching a caller proves LoadLibrary's step-7 example execution (which
// depends on the defaulted omission materialising) succeeded.
func loadDefaultsRenderDeck(t *testing.T, dir string) (*template.Library, *template.Registry, *theme.Registry) {
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
	if err := reg.Validate(); err != nil {
		t.Fatalf("template.Registry.Validate: %v", err)
	}
	themes, err := theme.LoadDir(fsys, filepath.Join(template.TemplatesDir, template.ThemesDir))
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}
	return lib, reg, themes
}

// renderDefaultsDeck runs the whole pipeline — load, validate, parse, render —
// over one temporary deck directory and returns the rendered page HTML.
func renderDefaultsDeck(t *testing.T, dir string) string {
	t.Helper()
	fsys := os.DirFS(dir)
	lib, reg, themes := loadDefaultsRenderDeck(t, dir)
	funcMap := template.LayoutFuncMap(lib.Media, "/assets/templates/media")

	if verr, invalid := validate.Validate(fsys, reg, themes); invalid {
		t.Fatalf("validate.Validate rejected the defaulted-omission deck: %s", validate.Format(verr))
	}

	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themes)
	if err != nil {
		t.Fatalf("deck.LoadConfig: %v", err)
	}
	d, err := deck.LoadSlides(fsys, deck.SlidesDir)
	if err != nil {
		t.Fatalf("deck.LoadSlides: %v", err)
	}
	parsed := parsePipelineSlides(t, fsys, d, reg)
	pageHTML, err := RenderDeck(cfg, d, parsed, reg, themes, funcMap)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	return string(pageHTML)
}
