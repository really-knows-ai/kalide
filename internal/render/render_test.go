package render

import (
	htmltmpl "html/template"
	"os"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
)

// fixtureLibraryDir is the phase-3 fixture library's project root, relative to
// this package, holding templates/slides/hello, templates/sections/item,
// templates/themes/plain and templates/media/logo.svg.
const fixtureLibraryDir = "../template/testdata/library"

// mustFixtureRegistry loads the phase-3 fixture library
// (internal/template/testdata/library/templates) and returns the registry it
// builds plus the library's layout func map (`media` bound to the served
// templates/media URL base, plus the format functions) — the same func map a
// real project's library layouts execute with.
func mustFixtureRegistry(t *testing.T) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	fsys := os.DirFS(fixtureLibraryDir)
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	return reg, template.LayoutFuncMap(lib.Media, "/media")
}

// TestRenderSlide covers RenderSlide on the fixture library's "hello" slide
// template: the `media` helper resolving to the served templates/media URL,
// the slide label as the root anchor id, the speaker-notes aside, and
// html/template's escaping of text-field values. It also checks the number/
// date format functions installed on the library func map produce the same
// output as before the func map was threaded through RenderSlide.
func TestRenderSlide(t *testing.T) {
	reg, funcMap := mustFixtureRegistry(t)

	t.Run("media func renders the served templates/media URL", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/1-hello.md",
			Template:    "hello",
			Frontmatter: map[string]any{"title": "Hello, world"},
		}
		got := renderSlideString(t, parsed, "hello", reg, funcMap)

		if !strings.Contains(got, `<img src="/media/logo.svg" alt="logo">`) {
			t.Errorf("rendered slide does not contain the served media URL:\n%s", got)
		}
		if !strings.Contains(got, "<h1>Hello, world</h1>") {
			t.Errorf("rendered slide is missing the title heading:\n%s", got)
		}
	})

	t.Run("label is the root section anchor id", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/1-hello.md",
			Template:    "hello",
			Frontmatter: map[string]any{"title": "Hello"},
		}
		got := renderSlideString(t, parsed, "hello", reg, funcMap)
		if !strings.Contains(got, `<section id="hello">`) {
			t.Errorf("slide label is not the root anchor id:\n%s", got)
		}

		escaped := renderSlideString(t, parsed, `a"b<c>&d`, reg, funcMap)
		if !strings.Contains(escaped, `<section id="a&#34;b&lt;c&gt;&amp;d">`) {
			t.Errorf("slide label is not attribute-escaped:\n%s", escaped)
		}
	})

	t.Run("notes aside only when a notes section exists", func(t *testing.T) {
		withNotes := &slide.Slide{
			File:        "slides/1-hello.md",
			Template:    "hello",
			Frontmatter: map[string]any{"title": "Hello"},
			Notes:       &slide.Notes{Body: "Pause on the metric so the number lands.\n"},
		}
		got := renderSlideString(t, withNotes, "hello", reg, funcMap)

		wantAside := `<aside class="notes"><p>Pause on the metric so the number lands.</p>` + "\n" + `</aside>`
		if !strings.Contains(got, wantAside) {
			t.Errorf("notes slide is missing the rendered aside %q:\n%s", wantAside, got)
		}
		aside := strings.Index(got, wantAside)
		close := strings.LastIndex(got, "</section>")
		if aside < 0 || close < 0 || aside > close {
			t.Errorf("notes aside is not inside the slide element (aside=%d close=%d):\n%s", aside, close, got)
		}

		none := &slide.Slide{
			File:        "slides/1-hello.md",
			Template:    "hello",
			Frontmatter: map[string]any{"title": "Hello"},
		}
		plain := renderSlideString(t, none, "hello", reg, funcMap)
		if strings.Contains(plain, "aside") {
			t.Errorf("slide without notes emits an aside:\n%s", plain)
		}
	})

	t.Run("text fields are escaped", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/1-hello.md",
			Template:    "hello",
			Frontmatter: map[string]any{"title": "R&D and Tom & Jerry"},
		}
		got := renderSlideString(t, parsed, "hello", reg, funcMap)
		if !strings.Contains(got, "R&amp;D and Tom &amp; Jerry") {
			t.Errorf("text field is not escaped:\n%s", got)
		}
	})

	t.Run("number and date format funcs produce the same output as before", func(t *testing.T) {
		fn, ok := funcMap["compact"].(template.FormatFunc)
		if !ok {
			t.Fatalf("funcMap[compact] has an unexpected type %T", funcMap["compact"])
		}
		got, err := fn(1250000)
		if err != nil || got != "1.25M" {
			t.Errorf("compact(1250000) = (%q, %v), want (\"1.25M\", nil)", got, err)
		}

		exact, ok := funcMap["exact"].(template.FormatFunc)
		if !ok {
			t.Fatalf("funcMap[exact] has an unexpected type %T", funcMap["exact"])
		}
		if got, err := exact(1250000); err != nil || got != "1,250,000" {
			t.Errorf("exact(1250000) = (%q, %v), want (\"1,250,000\", nil)", got, err)
		}

		long, ok := funcMap["long"].(template.FormatFunc)
		if !ok {
			t.Fatalf("funcMap[long] has an unexpected type %T", funcMap["long"])
		}
		if got, err := long("2026-09-25"); err != nil || got != "25 September 2026" {
			t.Errorf("long(2026-09-25) = (%q, %v), want (\"25 September 2026\", nil)", got, err)
		}

		short, ok := funcMap["short"].(template.FormatFunc)
		if !ok {
			t.Fatalf("funcMap[short] has an unexpected type %T", funcMap["short"])
		}
		if got, err := short("2026-09-25"); err != nil || got != "25 Sep 2026" {
			t.Errorf("short(2026-09-25) = (%q, %v), want (\"25 Sep 2026\", nil)", got, err)
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/1-hello.md",
			Template:    "hello",
			Frontmatter: map[string]any{"title": "Hello"},
		}
		first := renderSlideString(t, parsed, "hello", reg, funcMap)
		second := renderSlideString(t, parsed, "hello", reg, funcMap)
		if first != second {
			t.Errorf("RenderSlide is not deterministic:\nfirst:  %q\nsecond: %q", first, second)
		}
	})

	t.Run("definition failures are RenderErrors", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/1-hello.md",
			Template:    "hello",
			Frontmatter: map[string]any{"title": "Hello"},
		}

		if _, err := RenderSlide(nil, "x", reg, funcMap); err == nil {
			t.Error("nil slide: want an error")
		} else if _, ok := err.(*RenderError); !ok {
			t.Errorf("nil slide: got %T, want *RenderError", err)
		}

		if _, err := RenderSlide(parsed, "x", nil, funcMap); err == nil {
			t.Error("nil registry: want an error")
		} else if re, ok := err.(*RenderError); !ok {
			t.Errorf("nil registry: got %T, want *RenderError", err)
		} else if re.File != parsed.File {
			t.Errorf("nil registry: RenderError.File = %q, want %q", re.File, parsed.File)
		}

		unknown := *parsed
		unknown.Template = "does-not-exist"
		if _, err := RenderSlide(&unknown, "x", reg, funcMap); err == nil {
			t.Error("unknown template: want an error")
		} else if !strings.Contains(err.Error(), "unknown slide template") {
			t.Errorf("unknown template: error %q does not name the unknown template", err)
		}

		sectionAsSlide := *parsed
		sectionAsSlide.Template = "item"
		if _, err := RenderSlide(&sectionAsSlide, "x", reg, funcMap); err == nil {
			t.Error("section template used as a slide: want an error")
		} else if !strings.Contains(err.Error(), "not a slide template") {
			t.Errorf("section-as-slide: error %q does not reject the usage", err)
		}
	})
}

// renderSlideString renders one slide with the library's layout func map
// (MediaFunc plus the format functions) and returns the fragment as a
// string, failing the test on a render error.
func renderSlideString(t *testing.T, parsed *slide.Slide, label string, reg *template.Registry, funcMap htmltmpl.FuncMap) string {
	t.Helper()
	out, err := RenderSlide(parsed, label, reg, funcMap)
	if err != nil {
		t.Fatalf("RenderSlide(%q): %v", label, err)
	}
	return string(out)
}
