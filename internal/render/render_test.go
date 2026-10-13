package render

import (
	htmltmpl "html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/displaypath"
	"github.com/really-knows-ai/kalide/internal/mdcheck"
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
		} else {
			// The stored File stays the forward-slash deck-relative identity;
			// RenderError.Error renders it in the host OS's native spelling
			// (native-display-paths).
			if re.File != parsed.File {
				t.Errorf("nil registry: RenderError.File = %q, want %q", re.File, parsed.File)
			}
			if want := displaypath.Display("", parsed.File) + ": nil template registry"; re.Error() != want {
				t.Errorf("nil registry: RenderError.Error() = %q, want the native %q", re.Error(), want)
			}
			if native := filepath.FromSlash(parsed.File); native != parsed.File && strings.Contains(re.Error(), parsed.File) {
				t.Errorf("nil registry: RenderError.Error() = %q, want the native %q, not the forward-slash identity", re.Error(), native)
			}
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

	// The subtests below pin render.go's field conversion directly, at the
	// fieldValue/values boundary the layouts execute on: a nullable null
	// reaching the context as nil (nullable-fields), a coerce text field's YAML
	// number/boolean rendered as its string form (text-coercion), an inline
	// record item converted to one map per item with every declared field
	// present (record-item-type), the nested list-of-record recursion, and
	// values exposing an absent optional field as a present nil entry.
	// A bare renderer carries only the format catalogue these text/record/list
	// paths need; no registry or layout is involved.
	t.Run("nullable null converts to a nil context value", func(t *testing.T) {
		r := &renderer{formats: template.BuiltinFormats}

		f := &template.Field{Name: "subtitle", Type: template.FieldText, Nullable: true}
		got, err := r.fieldValue(f, nil, nil, nil)
		if err != nil {
			t.Fatalf("fieldValue(nullable null): %v", err)
		}
		if got != nil {
			t.Errorf("nullable null = %#v, want nil", got)
		}

		// A record item's explicitly null nullable field reaches the per-item
		// map as nil too, through the same recursion.
		rec := &template.Field{
			Type: template.FieldRecord,
			Fields: []template.Field{
				{Name: "label", Type: template.FieldText},
				{Name: "note", Type: template.FieldText, Nullable: true},
			},
		}
		got, err = r.fieldValue(rec, map[string]any{"label": "L", "note": nil}, nil, nil)
		if err != nil {
			t.Fatalf("fieldValue(record with nullable null): %v", err)
		}
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("record value = %#v, want map[string]any", got)
		}
		if m["note"] != nil {
			t.Errorf("record nullable null field = %#v, want nil", m["note"])
		}
	})

	t.Run("coerce text renders a YAML number or boolean as its string form", func(t *testing.T) {
		r := &renderer{formats: template.BuiltinFormats}
		coerce := &template.Field{Name: "id", Type: template.FieldText, Coerce: true}

		for _, tc := range []struct {
			name string
			raw  any
			want htmltmpl.HTML
		}{
			{"integer", 42, htmltmpl.HTML("42")},
			{"boolean", true, htmltmpl.HTML("true")},
			{"float", 3.5, htmltmpl.HTML("3.5")},
			{"string is unchanged", "42", htmltmpl.HTML("42")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got, err := r.fieldValue(coerce, tc.raw, nil, nil)
				if err != nil {
					t.Fatalf("fieldValue(%#v): %v", tc.raw, err)
				}
				if got != tc.want {
					t.Errorf("fieldValue(%#v) = %#v, want %#v", tc.raw, got, tc.want)
				}
			})
		}

		// A coerce text field inside a record item is converted by the same
		// per-type rule, not only at the template's top level.
		rec := &template.Field{
			Type:   template.FieldRecord,
			Fields: []template.Field{{Name: "count", Type: template.FieldText, Coerce: true}},
		}
		got, err := r.fieldValue(rec, map[string]any{"count": 7}, nil, nil)
		if err != nil {
			t.Fatalf("fieldValue(coerce record field): %v", err)
		}
		if m, ok := got.(map[string]any); !ok || m["count"] != htmltmpl.HTML("7") {
			t.Errorf("coerce record field = %#v, want a record map with count \"7\"", got)
		}
	})

	t.Run("a record item converts to one map with every declared field present", func(t *testing.T) {
		r := &renderer{formats: template.BuiltinFormats}
		rec := &template.Field{
			Type: template.FieldRecord,
			Fields: []template.Field{
				{Name: "name", Type: template.FieldText, Required: true},
				{Name: "role", Type: template.FieldText, Default: "member"},
				// Omitted with no default: it must still be present, as nil.
				{Name: "note", Type: template.FieldText},
			},
		}
		got, err := r.fieldValue(rec, map[string]any{"name": "Ada"}, nil, nil)
		if err != nil {
			t.Fatalf("fieldValue(record item): %v", err)
		}
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("record value = %#v, want map[string]any", got)
		}
		if len(m) != 3 {
			t.Errorf("record map has %d fields, want 3 (every declared field present): %#v", len(m), m)
		}
		if m["name"] != htmltmpl.HTML("Ada") {
			t.Errorf("record name = %#v, want \"Ada\"", m["name"])
		}
		if m["role"] != htmltmpl.HTML("member") {
			t.Errorf("record omitted field with default = %#v, want \"member\"", m["role"])
		}
		if note, present := m["note"]; !present || note != nil {
			t.Errorf("record omitted optional field = (%#v, present=%v), want (nil, present=true)", note, present)
		}
	})

	t.Run("a nested list-of-record recurses through the same conversion", func(t *testing.T) {
		r := &renderer{formats: template.BuiltinFormats}
		rec := &template.Field{
			Type: template.FieldRecord,
			Fields: []template.Field{
				{Name: "title", Type: template.FieldText},
				{
					Name: "tags",
					Type: template.FieldList,
					Item: &template.Field{
						Type:   template.FieldRecord,
						Fields: []template.Field{{Name: "label", Type: template.FieldText, Coerce: true}},
					},
				},
			},
		}
		got, err := r.fieldValue(rec, map[string]any{
			"title": "T",
			"tags": []any{
				map[string]any{"label": "a"},
				map[string]any{"label": 2},
				map[string]any{},
			},
		}, nil, nil)
		if err != nil {
			t.Fatalf("fieldValue(nested list-of-record): %v", err)
		}
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("outer record = %#v, want map[string]any", got)
		}
		items, ok := m["tags"].([]any)
		if !ok || len(items) != 3 {
			t.Fatalf("nested tags = %#v, want a 3-element []any", m["tags"])
		}
		if first, ok := items[0].(map[string]any); !ok || first["label"] != htmltmpl.HTML("a") {
			t.Errorf("tags[0] = %#v, want a record map with label \"a\"", items[0])
		}
		if second, ok := items[1].(map[string]any); !ok || second["label"] != htmltmpl.HTML("2") {
			t.Errorf("tags[1] = %#v, want a record map with coerced label \"2\"", items[1])
		}
		third, ok := items[2].(map[string]any)
		if !ok {
			t.Fatalf("tags[2] = %#v, want a record map", items[2])
		}
		if label, present := third["label"]; !present || label != nil {
			t.Errorf("tags[2].label = (%#v, present=%v), want (nil, present=true)", label, present)
		}
	})

	t.Run("values exposes an absent optional field as a nil entry", func(t *testing.T) {
		r := &renderer{formats: template.BuiltinFormats}
		tmpl := &template.Template{
			Name: "probe",
			Fields: []template.Field{
				{Name: "title", Type: template.FieldText, Required: true},
				{Name: "opt", Type: template.FieldText},
				{Name: "withDefault", Type: template.FieldText, Default: "d"},
			},
		}
		out, err := r.values(map[string]any{"title": "Hello"}, tmpl, nil)
		if err != nil {
			t.Fatalf("values: %v", err)
		}
		if out["title"] != htmltmpl.HTML("Hello") {
			t.Errorf("values title = %#v, want \"Hello\"", out["title"])
		}
		if opt, present := out["opt"]; !present || opt != nil {
			t.Errorf("absent optional field = (%#v, present=%v), want (nil, present=true)", opt, present)
		}
		if out["withDefault"] != htmltmpl.HTML("d") {
			t.Errorf("omitted field with default = %#v, want \"d\"", out["withDefault"])
		}

		// An explicit null and an absent optional field reach the layout
		// identically: a present nil entry.
		null, err := r.values(map[string]any{"title": "Hello", "opt": nil}, tmpl, nil)
		if err != nil {
			t.Fatalf("values(explicit null): %v", err)
		}
		if opt, present := null["opt"]; !present || opt != nil {
			t.Errorf("explicit null field = (%#v, present=%v), want (nil, present=true)", opt, present)
		}
	})

	// An image field resolves its value to the URL its layout receives
	// (image-field-roots): a `media` root through the owning layer's `media`
	// helper — the same URL a `{{ media … }}` reference in that template's
	// layout produces — while an `assets` root, and a field with no roots,
	// keep the written deck-relative `assets/…` value the deck page serves.
	// A field declaring BOTH roots lets the media helper's own existence check
	// decide: a path present in the layer's media/ resolves to the media URL,
	// and any other path keeps the written assets/ value.
	t.Run("image field roots resolve to the layout media helper URL", func(t *testing.T) {
		reg, funcMap := mustImageRootsRegistry(t)

		// The layout func map's own `media` helper — the exact func a
		// `{{ media … }}` reference in this template's layout executes with —
		// supplies the expected URL, so the media root is pinned against the
		// helper itself rather than a hardcoded string.
		mediaFn, ok := funcMap["media"].(func(string) (string, error))
		if !ok {
			t.Fatalf("funcMap[media] has an unexpected type %T", funcMap["media"])
		}
		wantMedia, err := mediaFn("pic.png")
		if err != nil {
			t.Fatalf(`layout media helper("pic.png"): %v`, err)
		}
		if wantMedia != "/media/pic.png" {
			t.Errorf("layout media helper = %q, want the served /media/… URL \"/media/pic.png\"", wantMedia)
		}

		parsed := &slide.Slide{
			File:     "slides/1-img.md",
			Template: "imgslide",
			Frontmatter: map[string]any{
				"hero":        "pic.png",
				"assetimg":    "assets/photo.png",
				"plainimg":    "assets/photo.png",
				"eithermedia": "pic.png",
				"eitherasset": "assets/photo.png",
			},
		}
		got := renderSlideString(t, parsed, "img",
			&deck.Config{Title: "Images"}, deck.Slide{Number: 1, Label: "img"}, 1, reg, funcMap)

		// A media root resolves to exactly the layout media helper's URL.
		if want := `<img class="hero" src="` + wantMedia + `">`; !strings.Contains(got, want) {
			t.Errorf("media-root image field did not resolve to the layout media helper URL:\nwant %q\ngot  %s", want, got)
		}
		// An assets-only root and a field with no roots keep the written
		// deck-relative value, which the deck page serves under the assets URL.
		for _, class := range []string{"asset", "plain"} {
			if want := `<img class="` + class + `" src="assets/photo.png">`; !strings.Contains(got, want) {
				t.Errorf("assets/no-roots image field (%s) did not keep the written assets/ value:\nwant %q\ngot  %s", class, want, got)
			}
		}
		// Both roots: media wins when the path exists in the owning layer's
		// media/, and the written assets/ value is kept otherwise.
		if want := `<img class="eithermedia" src="` + wantMedia + `">`; !strings.Contains(got, want) {
			t.Errorf("both-roots image field with a media file did not resolve to the media URL:\nwant %q\ngot  %s", want, got)
		}
		if want := `<img class="eitherasset" src="assets/photo.png">`; !strings.Contains(got, want) {
			t.Errorf("both-roots image field with no media file did not keep the written assets/ value:\nwant %q\ngot  %s", want, got)
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

// mustImageRootsRegistry loads the image-roots fixture library and returns the
// registry plus the layout func map bound to the served "/media" base — the
// same func map a real project's layout executes with, whose `media` helper
// resolves the sole layer's templates/media tree (the "owning layer's media/"
// of image-field-roots).
func mustImageRootsRegistry(t *testing.T) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	lib, err := template.LoadLibrary(os.DirFS(writeImageRootsLibrary(t)), template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	return reg, template.LayoutFuncMap(lib.Media, "/media")
}

// writeImageRootsLibrary writes a fresh single-layer deck library whose
// `imgslide` slide template declares one image field per root declaration — a
// media root, an assets root, no roots, and both roots — alongside a single
// templates/media/pic.png (the sole layer's media tree). Its layout emits each
// converted field as an `<img src>` so the test reads the exact URL the layout
// receives. The example supplies each field a value satisfying the load-time
// image check (a media-root value must exist in the layer's media/, while an
// assets-only/no-roots field is unchecked because no deck exists at load), so
// the library loads; the render assertions use their own frontmatter. It
// returns the deck root.
func writeImageRootsLibrary(t *testing.T) string {
	t.Helper()
	deckRoot := t.TempDir()
	root := filepath.Join(deckRoot, template.TemplatesDir)
	writeLayerManifest(t, root, "img-lib")

	dir := filepath.Join(root, template.SlidesDir, "imgslide")
	writeLayerFile(t, filepath.Join(dir, template.ManifestFile), `name: imgslide
fields:
  - name: hero
    type: image
    roots: [media]
  - name: assetimg
    type: image
    roots: [assets]
  - name: plainimg
    type: image
  - name: eithermedia
    type: image
    roots: [assets, media]
  - name: eitherasset
    type: image
    roots: [assets, media]
body:
  mode: optional
`)
	writeLayerFile(t, filepath.Join(dir, template.LayoutFile),
		`<section class="img-slide">`+
			`<img class="hero" src="{{.hero}}">`+
			`<img class="asset" src="{{.assetimg}}">`+
			`<img class="plain" src="{{.plainimg}}">`+
			`<img class="eithermedia" src="{{.eithermedia}}">`+
			`<img class="eitherasset" src="{{.eitherasset}}">`+
			`</section>`)
	writeLayerFile(t, filepath.Join(dir, template.ExampleFile), `---
template: imgslide
hero: pic.png
assetimg: assets/photo.png
plainimg: assets/photo.png
eithermedia: pic.png
eitherasset: pic.png
---
An image slide.
`)
	writeLayerMedia(t, root, "pic.png", "<svg/>")
	return deckRoot
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

// The parity probe is the unit deliverable for plan.phase-02.task-5: it renders
// the SAME wrapper section and the SAME child templates twice — once with the
// children authored directly under a slide's `wrapper` instance (the parsed-tree
// path, renderSection/renderInstance) and once with the children passed as
// `section` helper `child` arguments (the helper path,
// sectionHelper/renderChildren) — and compares the rendered CHILD fragments byte
// for byte. Both paths hand each child to the single shared instance renderer
// (renderInstance), so a helper child renders exactly as the equivalent authored
// instance does: same rendered HTML, same `.item` (index/number/count/first/
// last/section/template and parent-derived parent), same `.raw` source view and
// same `.data` view (item-context, raw-source-context, section-data-context,
// template-context, section-template-context).
//
// Only the CHILD fragments are compared, because the helper call's TARGET
// wrapper legitimately differs in its own `.item.section`: an authored wrapper
// is the declared section `wrapper`, while a helper invocation is named by the
// template it executes (`paritywrapper`). That one field is on the wrapper, not
// the children, and is deliberately excluded. The child's `.item.parent` is
// parent-derived: an authored child's parent is the wrapper's CONVERTED field
// values (a map carrying the wrapper's field values plus its reserved entries),
// a helper child's is the wrapper's RESOLVED EFFECTIVE values (the fields the
// call supplied, before conversion), and both render the same parent field (the
// wrapper's `heading`), so the fragments agree.
const (
	// parityChildArgs is the child-argument list BOTH parity paths pass to the
	// parity wrapper: two `column` children, each with a body and a nested `tag`
	// grandchild, so the child `.item`/`.raw`/`.data` and the recursion are all
	// exercised.
	parityChildArgs = `(child "column" (dict "label" "Alpha") "Alpha body" (child "tag" (dict "name" "AT"))) ` +
		`(child "column" (dict "label" "Beta") "Beta body" (child "tag" (dict "name" "BT")))`

	// parityWrapperLayout prints its own `.item` descriptor — including whether it
	// has a `.item.parent` and, when it does, the parent's `heading` field — then
	// splices its rendered `column` children between two marker spans and reports
	// the templates of its `.data.column` view. The marker pair lets the test
	// extract exactly the child fragments from each render.
	parityWrapperLayout = `<div class="parity-wrapper" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" data-parent-heading="{{if .item.parent}}{{.item.parent.heading}}{{end}}">` +
		`<span class="parity-heading">{{.heading}}</span>` +
		`<span class="parity-children">{{range .column}}{{.}}{{end}}</span>` +
		`<span class="parity-data-templates">{{range .data.column}}{{.item.template}};{{end}}</span>` +
		`</div>`

	// parityAuthoredSlideLayout is the AUTHORED variant: the slide carries one
	// authored `wrapper` instance whose two `column` children are declared in the
	// parsed tree, and the layout splices the rendered wrapper.
	parityAuthoredSlideLayout = `<section class="parity-slide">{{range .wrapper}}{{.}}{{end}}</section>`

	// parityHelperSlideLayout is the HELPER variant: the layout makes one
	// `{{ section "paritywrapper" (dict "heading" "Wrapper H") nil (child …) … }}`
	// call carrying the same two children as child arguments, so the wrapper and
	// its children come from the helper path.
	parityHelperSlideLayout = `<section class="parity-slide">{{section "paritywrapper" (dict "heading" "Wrapper H") nil ` + parityChildArgs + `}}</section>`

	// parityCallerLayout is a SECTION layout that makes the same helper call, so
	// the called wrapper's `.item.parent` is this calling instance's fields.
	parityCallerLayout = `<div class="parity-caller" data-heading="{{.heading}}">{{section "paritywrapper" (dict "heading" "Wrapper H") nil ` + parityChildArgs + `}}</div>`

	// parityCallerSlideLayout splices its authored `blocks` caller instance.
	parityCallerSlideLayout = `<section class="parity-callerslide">{{range .blocks}}{{.}}{{end}}</section>`
)

// mustSectionChildParityRegistry builds the in-code parity probe: the child
// templates from mustSectionChildrenRegistry (childrenitem/childtag) plus the
// parity wrapper and the authored, helper and section-caller slides. It
// registers templates directly (no content filesystem), so it runs under -short.
func mustSectionChildParityRegistry(t *testing.T) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	reg, funcMap := mustSectionChildrenRegistry(t)
	for _, tmpl := range []*template.Template{
		{
			Name:     "paritywrapper",
			Usage:    template.UsageSection,
			Fields:   []template.Field{{Name: "heading", Type: template.FieldText}},
			Sections: []template.SectionDecl{{Name: "column", Accepted: []string{"childrenitem"}, Max: 5}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: parityWrapperLayout},
		},
		{
			Name:     "parityauthoredslide",
			Usage:    template.UsageSlide,
			Sections: []template.SectionDecl{{Name: "wrapper", Accepted: []string{"paritywrapper"}, Max: 4}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: parityAuthoredSlideLayout},
		},
		{
			Name:   "parityhelperslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: parityHelperSlideLayout},
		},
		{
			Name:   "paritycaller",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "heading", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: parityCallerLayout},
		},
		{
			Name:     "paritycallerslide",
			Usage:    template.UsageSlide,
			Sections: []template.SectionDecl{{Name: "blocks", Accepted: []string{"paritycaller"}, Max: 4}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: parityCallerSlideLayout},
		},
	} {
		if err := reg.Register(tmpl); err != nil {
			t.Fatalf("register template %q: %v", tmpl.Name, err)
		}
	}
	return reg, funcMap
}

// parityChildren extracts the contiguous rendered `column` child fragments a
// parity wrapper splices between its `<span class="parity-children">` marker and
// the `<span class="parity-data-templates">` marker that follows, so an authored
// wrapper's and a helper wrapper's CHILD fragments — their `.item`/`.raw`/`.data`
// and parent-derived `.item.parent`, but not the wrapper's own descriptor — can
// be compared byte for byte.
func parityChildren(t *testing.T, rendered string) string {
	t.Helper()
	const open = `<span class="parity-children">`
	const close = `<span class="parity-data-templates">`
	i := strings.Index(rendered, open)
	j := strings.Index(rendered, close)
	if i < 0 || j < 0 || j < i {
		t.Fatalf("parity wrapper markers not found (open=%d close=%d):\n%s", i, j, rendered)
	}
	return rendered[i+len(open) : j]
}

// TestRenderSlideSectionHelperChildParity pins that a section-helper child
// renders byte-identically to the equivalent authored instance: the same wrapper
// and child templates are rendered once with the children authored under a
// slide's wrapper instance and once with the same children passed as `section`
// helper `child` arguments, and the rendered child fragments are compared
// exactly. It also pins that a `section` helper invocation made from a SLIDE
// layout has a nil `.item.parent`, while one made from a SECTION layout carries
// the caller's fields as `.item.parent`
// (item-context, section-template-context, template-context, raw-source-context,
// section-data-context).
func TestRenderSlideSectionHelperChildParity(t *testing.T) {
	reg, funcMap := mustSectionChildParityRegistry(t)
	cfg := &deck.Config{Title: "Parity Deck"}
	meta := deck.Slide{Number: 1, Label: "parity"}

	// The authored variant: one authored `wrapper` instance carrying the SAME two
	// `column` children the helper call passes as `child` arguments.
	authored := &slide.Slide{
		File:     "slides/1-authored.md",
		Template: "parityauthoredslide",
		Sections: []slide.Section{
			{
				Name: "wrapper", Level: 1, Index: 0, Template: "paritywrapper",
				Frontmatter: map[string]any{"heading": "Wrapper H"},
				Children: []slide.Section{
					{
						Name: "column", Level: 2, Index: 0, Template: "childrenitem",
						Frontmatter: map[string]any{"label": "Alpha"}, Body: "Alpha body",
						Children: []slide.Section{
							{Name: "tag", Level: 3, Index: 0, Template: "childtag", Frontmatter: map[string]any{"name": "AT"}},
						},
					},
					{
						Name: "column", Level: 2, Index: 1, Template: "childrenitem",
						Frontmatter: map[string]any{"label": "Beta"}, Body: "Beta body",
						Children: []slide.Section{
							{Name: "tag", Level: 3, Index: 0, Template: "childtag", Frontmatter: map[string]any{"name": "BT"}},
						},
					},
				},
			},
		},
	}
	authoredHTML := renderSlideString(t, authored, "parity", cfg, meta, 1, reg, funcMap)

	// The helper variant: the same wrapper reached through one `section` call
	// made from the slide layout.
	helper := &slide.Slide{File: "slides/2-helper.md", Template: "parityhelperslide"}
	helperHTML := renderSlideString(t, helper, "parity", cfg, meta, 1, reg, funcMap)

	authoredChildren := parityChildren(t, authoredHTML)
	helperChildren := parityChildren(t, helperHTML)
	if authoredChildren == "" {
		t.Fatal("the parity probe rendered no child fragments; the comparison would be vacuous")
	}
	if authoredChildren != helperChildren {
		t.Errorf("authored and section-helper child fragments differ:\nauthored: %s\nhelper:   %s", authoredChildren, helperChildren)
	}

	// Pin the shared child contract the parity rests on: each child is one of the
	// two same-name `column` siblings, with its own sibling descriptor over the
	// full passed set (index/number/count/first/last, section `column`, template
	// `childrenitem`), its `.item.parent` the wrapper's heading, its own `.raw`
	// source values and its own nested `tag`.
	for _, want := range []string{
		`<span class="child-item" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="column" data-template="childrenitem" data-parent="yes" data-parent-heading="Wrapper H">` +
			`<span class="child-label">Alpha</span>` +
			`<span class="child-raw-label">Alpha</span>` +
			`<span class="child-raw-body">Alpha body</span>` +
			"<span class=\"child-body\"><p>Alpha body</p>\n</span>" +
			`<span class="child-data-keys">tag;</span>` +
			`<b class="child-tag">AT</b>`,
		`<span class="child-item" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="column" data-template="childrenitem" data-parent="yes" data-parent-heading="Wrapper H">` +
			`<span class="child-label">Beta</span>` +
			`<span class="child-raw-label">Beta</span>` +
			`<span class="child-raw-body">Beta body</span>` +
			"<span class=\"child-body\"><p>Beta body</p>\n</span>" +
			`<span class="child-data-keys">tag;</span>` +
			`<b class="child-tag">BT</b>`,
	} {
		if !strings.Contains(helperChildren, want) {
			t.Errorf("helper child fragment does not contain %q:\n%s", want, helperChildren)
		}
	}

	// A `section` invocation made from a SLIDE layout has a nil `.item.parent`:
	// the parent of a call made from a slide is the slide, not a section
	// instance, so the wrapper's own descriptor carries no parent and no parent
	// heading.
	if want := `<div class="parity-wrapper" data-index="0" data-number="1" data-count="1" data-section="paritywrapper" data-template="paritywrapper" data-parent="no" data-parent-heading="">`; !strings.Contains(helperHTML, want) {
		t.Errorf("section helper target invoked from a slide does not carry a nil .item.parent:\nwant %q\ngot  %s", want, helperHTML)
	}

	// The same invocation made from a SECTION layout has the caller's fields as
	// its `.item.parent`.
	caller := &slide.Slide{
		File:     "slides/3-caller.md",
		Template: "paritycallerslide",
		Sections: []slide.Section{
			{Name: "blocks", Level: 1, Index: 0, Template: "paritycaller", Frontmatter: map[string]any{"heading": "Caller H"}},
		},
	}
	callerHTML := renderSlideString(t, caller, "parity", cfg, meta, 1, reg, funcMap)
	if want := `<div class="parity-wrapper" data-index="0" data-number="1" data-count="1" data-section="paritywrapper" data-template="paritywrapper" data-parent="yes" data-parent-heading="Caller H">`; !strings.Contains(callerHTML, want) {
		t.Errorf("section helper target invoked from a section does not carry the caller's fields as .item.parent:\nwant %q\ngot  %s", want, callerHTML)
	}
}

// cellContextHTML extracts the rendered html of one tableContext /
// tableCellContext cell map ({html, attrs}), failing if the shape is wrong.
func cellContextHTML(t *testing.T, cell any) string {
	t.Helper()
	m, ok := cell.(map[string]any)
	if !ok {
		t.Fatalf("cell = %T, want map[string]any", cell)
	}
	html, ok := m["html"].(htmltmpl.HTML)
	if !ok {
		t.Fatalf("cell html = %T, want html/template.HTML", m["html"])
	}
	return string(html)
}

// cellContextAttrs extracts the attrs map of one tableContext /
// tableCellContext cell map ({html, attrs}), failing if the shape is wrong.
func cellContextAttrs(t *testing.T, cell any) map[string]any {
	t.Helper()
	m, ok := cell.(map[string]any)
	if !ok {
		t.Fatalf("cell = %T, want map[string]any", cell)
	}
	attrs, ok := m["attrs"].(map[string]any)
	if !ok {
		t.Fatalf("cell attrs = %T, want map[string]any", m["attrs"])
	}
	return attrs
}

// TestTableContextColumnsInOrder pins tableContext's table map shape
// (table-layout-context, gfm-tables): headers, align and every body row carry one
// entry per column, in column order, and the returned .table is the body's first
// table — the same map .tables carries at index 0.
func TestTableContextColumnsInOrder(t *testing.T) {
	src := "| H1 | H2 | H3 |\n| :--- | :---: | ---: |\n| a | b | c |\n| d | e | f |"
	table, tables := tableContext(src, nil)

	if len(tables) != 1 {
		t.Fatalf("tableContext tables = %d entries, want 1", len(tables))
	}
	tm, ok := table.(map[string]any)
	if !ok {
		t.Fatalf("tableContext table = %T, want map[string]any", table)
	}

	headers, ok := tm["headers"].([]any)
	if !ok {
		t.Fatalf("table headers = %T, want []any", tm["headers"])
	}
	wantHeaders := []string{"H1", "H2", "H3"}
	if len(headers) != len(wantHeaders) {
		t.Fatalf("table headers = %d entries, want %d", len(headers), len(wantHeaders))
	}
	for i, want := range wantHeaders {
		if got := cellContextHTML(t, headers[i]); got != want {
			t.Errorf("header[%d] html = %q, want %q", i, got, want)
		}
	}

	align, ok := tm["align"].([]string)
	if !ok {
		t.Fatalf("table align = %T, want []string", tm["align"])
	}
	wantAlign := []string{"left", "center", "right"}
	if len(align) != len(wantAlign) {
		t.Fatalf("table align = %v, want %v", align, wantAlign)
	}
	for i, want := range wantAlign {
		if align[i] != want {
			t.Errorf("align[%d] = %q, want %q", i, align[i], want)
		}
	}

	rows, ok := tm["rows"].([]any)
	if !ok {
		t.Fatalf("table rows = %T, want []any", tm["rows"])
	}
	wantRows := [][]string{{"a", "b", "c"}, {"d", "e", "f"}}
	if len(rows) != len(wantRows) {
		t.Fatalf("table rows = %d entries, want %d", len(rows), len(wantRows))
	}
	for r, wantRow := range wantRows {
		cells, ok := rows[r].([]any)
		if !ok {
			t.Fatalf("row[%d] = %T, want []any", r, rows[r])
		}
		if len(cells) != len(wantRow) {
			t.Fatalf("row[%d] = %d cells, want %d", r, len(cells), len(wantRow))
		}
		for c, want := range wantRow {
			if got := cellContextHTML(t, cells[c]); got != want {
				t.Errorf("row[%d] cell[%d] html = %q, want %q", r, c, got, want)
			}
		}
	}

	// .table is the body's first table, i.e. the same map .tables[0] holds.
	if first, ok := tables[0].(map[string]any); !ok {
		t.Fatalf("tables[0] = %T, want map[string]any", tables[0])
	} else if got := cellContextHTML(t, first["headers"].([]any)[0]); got != "H1" {
		t.Errorf("tables[0] first header = %q, want the same first table .table exposes", got)
	}
}

// TestTableContextNoTable pins the always-present no-table shape in the render
// package's unit surface (table-layout-context, gfm-tables): without a table,
// .table is a TYPED nil map (map[string]any(nil)) — falsy but safe to range — and
// .tables is a non-nil, empty list.
func TestTableContextNoTable(t *testing.T) {
	table, tables := tableContext("A paragraph, and no table at all.\n", nil)

	tm, ok := table.(map[string]any)
	if !ok {
		t.Fatalf("tableContext table = %T, want map[string]any", table)
	}
	if tm != nil {
		t.Errorf("tableContext table = %#v, want a typed nil map", tm)
	}
	if tables == nil {
		t.Error("tableContext tables = nil, want a non-nil empty list")
	} else if len(tables) != 0 {
		t.Errorf("tableContext tables = %v, want an empty list", tables)
	}
}

// TestTableCellContext pins tableCellContext (table-cell-attributes,
// table-layout-context): a declared but unset attribute is present with a nil
// value; a set attribute carries its typed value and the `{...}` group is
// stripped from the html; a `{br}` joins its rendered segments with a literal
// `<br>`; and an unknown attribute degrades to the cell's raw inline html with
// every declared attribute left nil and no entry for the undeclared name.
func TestTableCellContext(t *testing.T) {
	specs := []mdcheck.CellAttributeSpec{
		{Name: "bold", Kind: mdcheck.CellAttributeBoolean},
		{Name: "highlight", Kind: mdcheck.CellAttributeEnum, Variants: []string{"green", "red"}},
	}

	t.Run("declared attributes are present with nil values", func(t *testing.T) {
		m := tableCellContext(mdcheck.TableCell{Text: "plain"}, specs)
		attrs := cellContextAttrs(t, m)
		if len(attrs) != 2 {
			t.Fatalf("attrs = %v, want the two declared attributes", attrs)
		}
		for _, name := range []string{"bold", "highlight"} {
			v, ok := attrs[name]
			if !ok {
				t.Errorf("attrs[%q] is absent, want present with a nil value", name)
			} else if v != nil {
				t.Errorf("attrs[%q] = %v, want nil", name, v)
			}
		}
	})

	t.Run("set attributes carry their typed values and strip the group", func(t *testing.T) {
		m := tableCellContext(mdcheck.TableCell{Text: "x{bold} y{highlight:green}"}, specs)
		if got := string(m["html"].(htmltmpl.HTML)); got != "x y" {
			t.Errorf("html = %q, want %q (the groups stripped)", got, "x y")
		}
		attrs := cellContextAttrs(t, m)
		if attrs["bold"] != true {
			t.Errorf("attrs[bold] = %v, want true", attrs["bold"])
		}
		if attrs["highlight"] != "green" {
			t.Errorf("attrs[highlight] = %v, want %q", attrs["highlight"], "green")
		}
	})

	t.Run("{br} joins the rendered segments with a literal <br>", func(t *testing.T) {
		m := tableCellContext(mdcheck.TableCell{Text: "a{br}**b**"}, specs)
		if got := string(m["html"].(htmltmpl.HTML)); got != "a<br><strong>b</strong>" {
			t.Errorf("html = %q, want %q", got, "a<br><strong>b</strong>")
		}
	})

	t.Run("an unknown attribute degrades to raw html with nil attrs", func(t *testing.T) {
		m := tableCellContext(mdcheck.TableCell{Text: "plain {hilight}"}, specs)
		if got := string(m["html"].(htmltmpl.HTML)); got != "plain {hilight}" {
			t.Errorf("html = %q, want the raw %q on a decode error", got, "plain {hilight}")
		}
		attrs := cellContextAttrs(t, m)
		if len(attrs) != 2 {
			t.Fatalf("attrs = %v, want only the two declared attributes", attrs)
		}
		for _, name := range []string{"bold", "highlight"} {
			if v := attrs[name]; v != nil {
				t.Errorf("attrs[%q] = %v, want nil on a decode error", name, v)
			}
		}
		if _, ok := attrs["hilight"]; ok {
			t.Errorf("attrs carries the undeclared name %q, want no entry", "hilight")
		}
	})
}

// TestRenderCellLines pins renderCellLines (table-layout-context): each decoded
// segment is rendered through renderInline and the rendered segments are joined
// with a literal `<br>`, so a `{br}` break survives an inline renderer that would
// escape a raw `<br>`.
func TestRenderCellLines(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{"no lines", nil, ""},
		{"single segment", []string{"a"}, "a"},
		{"two segments joined", []string{"a", "b"}, "a<br>b"},
		{"each segment rendered inline", []string{"**a**", "`b`"}, "<strong>a</strong><br><code>b</code>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(renderCellLines(tc.lines)); got != tc.want {
				t.Errorf("renderCellLines(%q) = %q, want %q", tc.lines, got, tc.want)
			}
		})
	}
}

// TestRenderBodyOmitsTables pins renderBody's table omission
// (table-layout-context, gfm-tables): a body's GFM table reaches the output only
// through the reserved .table/.tables context, so renderBody renders the
// surrounding content and drops the table block entirely.
func TestRenderBodyOmitsTables(t *testing.T) {
	src := "before\n\n| A | B |\n| --- | --- |\n| a | b |\n\nafter"
	html, err := renderBody(src)
	if err != nil {
		t.Fatalf("renderBody() error = %v, want nil", err)
	}
	if got, want := string(html), "<p>before</p>\n<p>after</p>\n"; got != want {
		t.Errorf("renderBody() = %q, want %q (the table omitted, other content kept)", got, want)
	}
}

// TestRenderBodyWithoutTablesByteIdentical pins that renderBody leaves a body
// with no table untouched (table-layout-context): it renders exactly as goldmark
// renders the source, with none of the table-blanking applied.
func TestRenderBodyWithoutTablesByteIdentical(t *testing.T) {
	src := "# Heading\n\nA paragraph with **bold** and `code`.\n"
	html, err := renderBody(src)
	if err != nil {
		t.Fatalf("renderBody() error = %v, want nil", err)
	}
	want := "<h1>Heading</h1>\n<p>A paragraph with <strong>bold</strong> and <code>code</code>.</p>\n"
	if got := string(html); got != want {
		t.Errorf("renderBody() = %q, want %q", got, want)
	}
}
