package render

import (
	htmltmpl "html/template"
	"os"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
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

	// TestRenderSlide exercises the layout mechanics, not the reserved
	// deck/slide values, so it threads a well-formed but minimal context
	// through every RenderSlide call.
	cfg := &deck.Config{Title: "Test Deck"}
	meta := deck.Slide{Number: 1, Label: "hello"}
	total := 2

	t.Run("media func renders the served templates/media URL", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/1-hello.md",
			Template:    "hello",
			Frontmatter: map[string]any{"title": "Hello, world"},
		}
		got := renderSlideString(t, parsed, "hello", cfg, meta, total, reg, funcMap)

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
		got := renderSlideString(t, parsed, "hello", cfg, meta, total, reg, funcMap)
		if !strings.Contains(got, `<section id="hello">`) {
			t.Errorf("slide label is not the root anchor id:\n%s", got)
		}

		escaped := renderSlideString(t, parsed, `a"b<c>&d`, cfg, meta, total, reg, funcMap)
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
		got := renderSlideString(t, withNotes, "hello", cfg, meta, total, reg, funcMap)

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
		plain := renderSlideString(t, none, "hello", cfg, meta, total, reg, funcMap)
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
		got := renderSlideString(t, parsed, "hello", cfg, meta, total, reg, funcMap)
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
		first := renderSlideString(t, parsed, "hello", cfg, meta, total, reg, funcMap)
		second := renderSlideString(t, parsed, "hello", cfg, meta, total, reg, funcMap)
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

		if _, err := RenderSlide(nil, "x", cfg, meta, total, reg, funcMap); err == nil {
			t.Error("nil slide: want an error")
		} else if _, ok := err.(*RenderError); !ok {
			t.Errorf("nil slide: got %T, want *RenderError", err)
		}

		if _, err := RenderSlide(parsed, "x", cfg, meta, total, nil, funcMap); err == nil {
			t.Error("nil registry: want an error")
		} else if re, ok := err.(*RenderError); !ok {
			t.Errorf("nil registry: got %T, want *RenderError", err)
		} else if re.File != parsed.File {
			t.Errorf("nil registry: RenderError.File = %q, want %q", re.File, parsed.File)
		}

		unknown := *parsed
		unknown.Template = "does-not-exist"
		if _, err := RenderSlide(&unknown, "x", cfg, meta, total, reg, funcMap); err == nil {
			t.Error("unknown template: want an error")
		} else if !strings.Contains(err.Error(), "unknown slide template") {
			t.Errorf("unknown template: error %q does not name the unknown template", err)
		}

		sectionAsSlide := *parsed
		sectionAsSlide.Template = "item"
		if _, err := RenderSlide(&sectionAsSlide, "x", cfg, meta, total, reg, funcMap); err == nil {
			t.Error("section template used as a slide: want an error")
		} else if !strings.Contains(err.Error(), "not a slide template") {
			t.Errorf("section-as-slide: error %q does not reject the usage", err)
		}
	})
}

// renderSlideString renders one slide with the library's layout func map
// (MediaFunc plus the format functions) and returns the fragment as a string,
// failing the test on a render error. cfg, meta and total are the reserved
// `deck`/`slide` context RenderSlide threads into the layout.
func renderSlideString(t *testing.T, parsed *slide.Slide, label string, cfg *deck.Config, meta deck.Slide, total int, reg *template.Registry, funcMap htmltmpl.FuncMap) string {
	t.Helper()
	out, err := RenderSlide(parsed, label, cfg, meta, total, reg, funcMap)
	if err != nil {
		t.Fatalf("RenderSlide(%q): %v", label, err)
	}
	return string(out)
}

// The section-composition probe library is an in-code, two-level template set
// (no filesystem): a slide template declaring one section (`blocks`, accepting
// `probeblock`) whose section template declares its own child section
// (`leaves`, accepting `probeleaf`). Each layout is a single compact line so
// the rendered fragment can be asserted exactly, and every section layout
// prints its reserved `.item` descriptor — index, number, count, first, last,
// section, template and parent — as data attributes, so the unit test pins the
// bottom-up execution, the child-HTML-list shape and the descriptor values
// (item-context, template-language).
const (
	// probeSlideLayout ranges the top-level `blocks` list of trusted HTML
	// without inspecting it as data, and reports whether the reserved `item`
	// entry is present: a slide has no sibling place, so `.item` is absent.
	probeSlideLayout = `<section class="probe-slide"><h1>{{.title}}</h1><span class="slide-item">{{if .item}}present{{else}}absent{{end}}</span>{{range .blocks}}{{.}}{{end}}</section>`

	// probeBlockLayout is a section layout: it prints its own `.item`
	// descriptor, then splices each rendered child (`leaves`) with
	// `{{range .leaves}}{{.}}{{end}}` — the child list arrives as trusted HTML
	// (template-language). `.item.parent` is absent at the top level, so the
	// `data-parent` attribute says whether the descriptor carries one.
	probeBlockLayout = `<div class="block" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}"><span class="block-title">{{.title}}</span>{{range .leaves}}{{.}}{{end}}</div>`

	// probeLeafLayout is the deepest section layout: alongside its own
	// `.item` descriptor it reads `.item.parent.title`, the enclosing section
	// instance's field values (item-context).
	probeLeafLayout = `<span class="leaf" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent-title="{{.item.parent.title}}">{{.name}}</span>`
)

// mustSectionRegistry builds the in-code section-composition probe library and
// returns the registry plus its layout func map. It registers templates
// directly (no content filesystem), so it needs no disk and runs under -short.
func mustSectionRegistry(t *testing.T) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	reg := template.NewRegistry(nil)
	for _, tmpl := range []*template.Template{
		{
			Name:     "probeslide",
			Usage:    template.UsageSlide,
			Fields:   []template.Field{{Name: "title", Type: template.FieldText, Required: true}},
			Sections: []template.SectionDecl{{Name: "blocks", Accepted: []string{"probeblock"}, Max: 4}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: probeSlideLayout},
		},
		{
			Name:     "probeblock",
			Usage:    template.UsageSection,
			Fields:   []template.Field{{Name: "title", Type: template.FieldText}},
			Sections: []template.SectionDecl{{Name: "leaves", Accepted: []string{"probeleaf"}, Max: 4}},
			Body:     template.BodyRule{Mode: template.BodyDisallowed},
			Layout:   template.Layout{Text: probeBlockLayout},
		},
		{
			Name:   "probeleaf",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "name", Type: template.FieldText, Required: true}},
			Body:   template.BodyRule{Mode: template.BodyDisallowed},
			Layout: template.Layout{Text: probeLeafLayout},
		},
	} {
		if err := reg.Register(tmpl); err != nil {
			t.Fatalf("register template %q: %v", tmpl.Name, err)
		}
	}
	return reg, template.LayoutFuncMap(nil, "")
}

// TestRenderSlideNestedSections pins bottom-up section execution on the in-code
// two-level probe library: a parent section instance is rendered AFTER its
// children, with each child arriving as trusted HTML inside the parent's
// `{{range .leaves}}{{.}}{{end}}` (so the child markup is nested inside the
// parent markup, deepest-first); every section layout reads its reserved
// `.item` descriptor (index/number/count/first/last/section/template/parent);
// and the slide layout's `.item` is absent, since a slide has no siblings
// (template-language, item-context, template-context).
func TestRenderSlideNestedSections(t *testing.T) {
	reg, funcMap := mustSectionRegistry(t)

	parsed := &slide.Slide{
		File:        "slides/1-probe.md",
		Template:    "probeslide",
		Frontmatter: map[string]any{"title": "Probe"},
		Sections: []slide.Section{
			{
				Name:        "blocks",
				Level:       1,
				Index:       0,
				Template:    "probeblock",
				Frontmatter: map[string]any{"title": "First"},
				Children: []slide.Section{
					{Name: "leaves", Level: 2, Index: 0, Template: "probeleaf", Frontmatter: map[string]any{"name": "A"}},
					{Name: "leaves", Level: 2, Index: 1, Template: "probeleaf", Frontmatter: map[string]any{"name": "B"}},
				},
			},
			{Name: "blocks", Level: 1, Index: 1, Template: "probeblock", Frontmatter: map[string]any{"title": "Second"}},
		},
	}

	cfg := &deck.Config{Title: "Unit Deck"}
	meta := deck.Slide{Number: 1, Label: "probe"}
	got := renderSlideString(t, parsed, "probe", cfg, meta, 1, reg, funcMap)

	// The deepest section instance renders first, through its own layout, and
	// its markup is handed to the parent as one trusted-HTML list element
	// (each leaf reads its own `.item` AND `.item.parent.title`).
	leafA := `<span class="leaf" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="leaves" data-template="probeleaf" data-parent-title="First">A</span>`
	leafB := `<span class="leaf" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="leaves" data-template="probeleaf" data-parent-title="First">B</span>`

	// The first block carries its own `.item` (one of two same-name siblings;
	// its parent is the slide, so `.item.parent` is nil) and splices both
	// children as rendered markup, in source order, inside its own wrapper.
	firstBlock := `<div class="block" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="blocks" data-template="probeblock" data-parent="no"><span class="block-title">First</span>` + leafA + leafB + `</div>`

	// The second block is the last of two siblings and has no children.
	secondBlock := `<div class="block" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="blocks" data-template="probeblock" data-parent="no"><span class="block-title">Second</span></div>`

	// The slide layout receives the top-level section instances as trusted
	// HTML, and its own `.item` is absent.
	want := `<section id="probe" class="probe-slide"><h1>Probe</h1><span class="slide-item">absent</span>` + firstBlock + secondBlock + `</section>`

	if got != want {
		t.Errorf("RenderSlide nested sections:\ngot:  %s\nwant: %s", got, want)
	}

	// Explicit containment: each child's markup sits inside its parent's
	// wrapper, deepest-first, rather than alongside it.
	if a, b := strings.Index(firstBlock, leafA), strings.Index(firstBlock, leafB); a < 0 || b < 0 || a > b {
		t.Errorf("child leaf markup is not nested inside the block wrapper in source order (a=%d b=%d):\n%s", a, b, firstBlock)
	}
}
