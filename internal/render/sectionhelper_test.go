package render

import (
	htmltmpl "html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
)

// The render-time `section` helper unit probe is an in-code template set (no
// filesystem), so these tests run under -short. It drives RenderSlide's real
// pipeline — parseLayouts binding the renderer-backed helper onto the shared
// namespace, reachableTemplates pulling a helper-only target in, slideData and
// renderSection building the reserved context, and sectionHelper executing the
// target as a one-item group — and pins the render-time contract of
// `{{ section "<name>" [fields] [body] }}` (section-helper, item-context,
// raw-source-context, section-data-context).
//
// helperfooter is deliberately NOT a declared child of helperslide: it is
// reached only through the slide layout's `{{ section "helperfooter" … }}`
// call, so the test fails unless reachableTemplates follows the helper-call
// edge into the shared namespace. helperauthored is both a declared child and a
// helper target, so the same call exercises "a helper call adds no entry to the
// caller's .data and no element to its rendered section list".
const (
	// helperSlideLayout is the caller: it reports the authored section list and
	// data lengths (which a helper call must not change), then makes three
	// calls — a helper-only target from the slide (nil parent), a helper target
	// that is also a declared child (whose own layout makes a nested call), and
	// a literal-body call.
	helperSlideLayout = `<section class="helper-slide"><h1>{{.title}}</h1>` +
		`<span class="authored-count">{{len .authored}}</span>` +
		`<span class="authored-data-count">{{len .data.authored}}</span>` +
		`<span class="data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`{{range .authored}}{{.}}{{end}}` +
		`{{section "helperfooter" (dict "title" "Slide footer") "Footer body"}}` +
		`{{section "helperauthored" (dict "title" "Helper authored" "name" "Bee")}}` +
		`{{section "helperbodytarget" (dict "title" "Literal body target") "Body words"}}` +
		`</section>`

	// helperDefaultSlideLayout calls the helper with no fields at all, so the
	// target's declared defaults are the only source of its values.
	helperDefaultSlideLayout = `<section>{{section "helperfooter"}}</section>`

	helperUnknownFieldLayout   = `<section>{{section "helperfooter" (dict "title" "T" "nope" "x")}}</section>`
	helperRequiredSlideLayout  = `<section>{{section "helperrequired"}}</section>`
	helperLiteralBadBodyLayout = `<section>{{section "helperbodytarget" (dict "title" "T") "# items"}}</section>`
	helperDisallowedBodyLayout = `<section>{{section "helpernobody" (dict "title" "T") "some words"}}</section>`
	helperNonLiteralBodyLayout = `<section>{{section "helperbodytarget" (dict "title" "T") .raw.body}}</section>`

	// helperFooterLayout prints its one-item `.item` descriptor, its fields, the
	// source view `.raw` (absent keys read "absent", which distinguishes a
	// default from an authored value), the rendered `.body`, and the keys of its
	// (always empty) `.data`.
	helperFooterLayout = `<footer class="helper-footer" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" data-parent-title="{{if .item.parent}}{{.item.parent.title}}{{end}}">` +
		`<span class="footer-title">{{.title}}</span>` +
		`<span class="footer-kind">{{.kind}}</span>` +
		`<span class="footer-raw-title">{{if .raw.title}}{{.raw.title}}{{else}}absent{{end}}</span>` +
		`<span class="footer-raw-kind">{{if .raw.kind}}{{.raw.kind}}{{else}}absent{{end}}</span>` +
		`<span class="footer-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="footer-body">{{if .body}}{{.body}}{{else}}absent{{end}}</span>` +
		`<span class="footer-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`</footer>`

	// helperAuthoredLayout is both an authored child and a helper target: it
	// reads its own `.item`/`.raw` and makes a nested call, so the target's
	// `.item.parent` is this instance's field values (item-context).
	helperAuthoredLayout = `<div class="helper-authored" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" data-parent-title="{{if .item.parent}}{{.item.parent.title}}{{end}}">` +
		`<span class="authored-title">{{.title}}</span>` +
		`<span class="authored-raw-title">{{if .raw.title}}{{.raw.title}}{{else}}absent{{end}}</span>` +
		`{{section "helperfooter" (dict "title" "Nested footer")}}` +
		`</div>`

	helperBodyTargetLayout = `<div class="helper-body-target"><span class="body-target-title">{{.title}}</span><span class="body-target-body">{{if .body}}{{.body}}{{else}}absent{{end}}</span></div>`
	helperItemLayout       = `<span class="helper-item">{{.label}}</span>`
	helperNoBodyLayout     = `<div class="helper-no-body">{{.title}}</div>`
	helperRequiredLayout   = `<div class="helper-required">{{.must}}</div>`
)

// mustSectionHelperRegistry builds the in-code render-time section-helper probe
// library and returns the registry plus its layout func map. It registers
// templates directly (no content filesystem), so it needs no disk and runs
// under -short. It does NOT run Registry.Validate: the render-time helper's
// field/body checks (ResolveSectionCall) are what these tests pin, so the
// invalid calls are deliberately reachable at render.
func mustSectionHelperRegistry(t *testing.T) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	reg := template.NewRegistry(nil)
	for _, tmpl := range []*template.Template{
		{
			Name:     "helperslide",
			Usage:    template.UsageSlide,
			Fields:   []template.Field{{Name: "title", Type: template.FieldText, Required: true}},
			Sections: []template.SectionDecl{{Name: "authored", Accepted: []string{"helperauthored"}, Max: 4}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: helperSlideLayout},
		},
		{
			Name:   "helperdefaultslide",
			Usage:  template.UsageSlide,
			Fields: []template.Field{{Name: "title", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: helperDefaultSlideLayout},
		},
		{
			Name:   "helperunknownfieldslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: helperUnknownFieldLayout},
		},
		{
			Name:   "helperrequiredslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: helperRequiredSlideLayout},
		},
		{
			Name:   "helperliteralbadslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: helperLiteralBadBodyLayout},
		},
		{
			Name:   "helperdisallowedbodyslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: helperDisallowedBodyLayout},
		},
		{
			Name:   "helpernonliteralslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: helperNonLiteralBodyLayout},
		},
		{
			Name:  "helperfooter",
			Usage: template.UsageSection,
			Fields: []template.Field{
				// A required field WITH a default: defaults are applied before
				// CheckValues, so omitting it is not "required but missing".
				{Name: "title", Type: template.FieldText, Required: true, Default: "Untitled"},
				{Name: "kind", Type: template.FieldText, Default: "note"},
			},
			Body:   template.BodyRule{Mode: template.BodyOptional, Subheadings: true},
			Layout: template.Layout{Text: helperFooterLayout},
		},
		{
			Name:   "helperauthored",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "title", Type: template.FieldText, Required: true}, {Name: "name", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: helperAuthoredLayout},
		},
		{
			Name:     "helperbodytarget",
			Usage:    template.UsageSection,
			Fields:   []template.Field{{Name: "title", Type: template.FieldText}},
			Sections: []template.SectionDecl{{Name: "items", Accepted: []string{"helperitem"}, Max: 4}},
			// Subheadings are allowed, so a `# items` body passes CheckBody and
			// is caught only by the child-section rule the helper adds.
			Body:   template.BodyRule{Mode: template.BodyOptional, Subheadings: true},
			Layout: template.Layout{Text: helperBodyTargetLayout},
		},
		{
			Name:   "helperitem",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "label", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyDisallowed},
			Layout: template.Layout{Text: helperItemLayout},
		},
		{
			Name:   "helpernobody",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "title", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyDisallowed},
			Layout: template.Layout{Text: helperNoBodyLayout},
		},
		{
			Name:   "helperrequired",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "must", Type: template.FieldText, Required: true}},
			Body:   template.BodyRule{Mode: template.BodyDisallowed},
			Layout: template.Layout{Text: helperRequiredLayout},
		},
	} {
		if err := reg.Register(tmpl); err != nil {
			t.Fatalf("register template %q: %v", tmpl.Name, err)
		}
	}
	return reg, template.LayoutFuncMap(nil, "")
}

// TestRenderSlideSectionHelper pins the render-time `section` helper through
// RenderSlide's real pipeline: a helper-only target (not a declared child) is
// still rendered, reached via reachableTemplates; a helper target that is also
// a declared child renders without changing the caller's `.data` or rendered
// section list; `.item` is the one-item descriptor (index 0, number/count 1,
// first/last true, section/template the target name, parent the caller's fields
// or absent from a slide layout); `.raw` is the source view of the supplied
// values plus body; and the target's declared defaults are applied without
// appearing in `.raw` (section-helper, item-context, raw-source-context,
// section-data-context).
func TestRenderSlideSectionHelper(t *testing.T) {
	reg, funcMap := mustSectionHelperRegistry(t)
	cfg := &deck.Config{Title: "Helper Deck"}
	meta := deck.Slide{Number: 3, Label: "helper"}
	total := 5

	parsed := &slide.Slide{
		File:        "slides/1-helper.md",
		Template:    "helperslide",
		Frontmatter: map[string]any{"title": "Helper"},
		Sections: []slide.Section{
			{Name: "authored", Level: 1, Index: 0, Template: "helperauthored", Frontmatter: map[string]any{"title": "First authored", "name": "Ada"}},
		},
	}
	got := renderSlideString(t, parsed, "helper", cfg, meta, total, reg, funcMap)

	for _, want := range []string{
		// The helper-only target renders on the slide; it is the sole instance
		// of its own one-item group (index 0, number/count 1, first/last true),
		// .section/.template are the target name, and .item.parent is absent
		// because the call is made from a slide layout.
		`<footer class="helper-footer" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="helperfooter" data-template="helperfooter" data-parent="no" data-parent-title="">` +
			`<span class="footer-title">Slide footer</span>` +
			`<span class="footer-kind">note</span>` + // default applied
			`<span class="footer-raw-title">Slide footer</span>` + // supplied source
			`<span class="footer-raw-kind">absent</span>` + // default is not source
			`<span class="footer-raw-body">Footer body</span>` + // supplied body source
			`<span class="footer-body"><p>Footer body</p>`,

		// The helper target that is ALSO a declared child: its target layout
		// reads this instance's fields as the nested call's `.item.parent`.
		// As an authored instance `.item.section` is the declared section name
		// while `.item.template` is the resolved target.
		`<div class="helper-authored" data-index="0" data-number="1" data-count="1" data-section="authored" data-template="helperauthored" data-parent="no" data-parent-title="">` +
			`<span class="authored-title">First authored</span>` +
			`<span class="authored-raw-title">First authored</span>`,

		// Calling that target from the slide: .item.parent is still absent (the
		// parent is the slide), and its nested call reads its fields.
		`<div class="helper-authored" data-index="0" data-number="1" data-count="1" data-section="helperauthored" data-template="helperauthored" data-parent="no" data-parent-title="">` +
			`<span class="authored-title">Helper authored</span>` +
			`<span class="authored-raw-title">Helper authored</span>`,

		// A nested call made from a SECTION layout: .item.parent is the calling
		// instance's fields, so data-parent is yes and the title resolves.
		`<footer class="helper-footer" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="helperfooter" data-template="helperfooter" data-parent="yes" data-parent-title="First authored">` +
			`<span class="footer-title">Nested footer</span>`,
		`<footer class="helper-footer" data-index="0" data-number="1" data-count="1" data-first="true" data-last="true" data-section="helperfooter" data-template="helperfooter" data-parent="yes" data-parent-title="Helper authored">` +
			`<span class="footer-title">Nested footer</span>`,

		// A literal body is validated against the target and rendered as body
		// HTML.
		`<div class="helper-body-target"><span class="body-target-title">Literal body target</span><span class="body-target-body"><p>Body words</p>`,

		// The helper call appends nothing to the caller's .data (only the
		// authored `authored` section is a key) and nothing to its rendered
		// section list (still exactly one authored element), even though it
		// targets the same section template.
		`<span class="authored-count">1</span>`,
		`<span class="authored-data-count">1</span>`,
		`<span class="data-keys">authored;</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered slide does not contain %q:\n%s", want, got)
		}
	}

	for _, forbidden := range []string{"helperfooter;", "helperbodytarget;", "helperauthored;"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("helper call leaked into the caller's .data: found %q:\n%s", forbidden, got)
		}
	}
}

// TestRenderSlideSectionHelperReachable pins reachableTemplates directly: a
// target referenced only through a layout's `{{ section … }}` call (never a
// declared child, section-template field or accepted name) is still pulled into
// the shared namespace, which is what lets parseLayouts parse it and
// renderer.sectionHelper execute it (section-helper).
func TestRenderSlideSectionHelperReachable(t *testing.T) {
	reg, _ := mustSectionHelperRegistry(t)

	root, ok := reg.Lookup("helperslide")
	if !ok {
		t.Fatal("helperslide is not registered")
	}
	names := make(map[string]bool)
	for _, tmpl := range reachableTemplates(root, reg) {
		names[tmpl.Name] = true
	}
	if !names["helperfooter"] {
		t.Errorf("reachableTemplates(helperslide) = %v; want it to include the helper-only target helperfooter", names)
	}
	if !names["helperauthored"] {
		t.Errorf("reachableTemplates(helperslide) = %v; want it to include helperauthored (declared child and helper target)", names)
	}
}

// TestRenderSlideSectionHelperDefaults pins the defaults-first rule at render:
// a call that supplies no fields executes the target with its declared defaults
// (so a required field carrying a default is satisfied), while `.raw` — the
// source view of what the call supplied — stays empty, because a default is not
// an authored source value (section-helper, raw-source-context).
func TestRenderSlideSectionHelperDefaults(t *testing.T) {
	reg, funcMap := mustSectionHelperRegistry(t)
	cfg := &deck.Config{}
	meta := deck.Slide{Number: 1, Label: "default"}

	parsed := &slide.Slide{File: "slides/2-default.md", Template: "helperdefaultslide"}
	got := renderSlideString(t, parsed, "default", cfg, meta, 1, reg, funcMap)

	for _, want := range []string{
		`<span class="footer-title">Untitled</span>`,
		`<span class="footer-kind">note</span>`,
		`<span class="footer-raw-title">absent</span>`,
		`<span class="footer-raw-kind">absent</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered slide does not contain %q:\n%s", want, got)
		}
	}
}

// TestRenderSlideSectionHelperValidation pins the render-time validation the
// helper delegates to template.ResolveSectionCall: a call-supplied dict field
// the target does not declare, an undefaulted required field left out, a
// disallowed body, and — for both a literal and a non-literal body expression —
// a body whose heading names a child section the target declares are all
// rejected at render, with no partial output
// (section-helper, section-helper-load-checks).
func TestRenderSlideSectionHelperValidation(t *testing.T) {
	reg, funcMap := mustSectionHelperRegistry(t)
	cfg := &deck.Config{}
	meta := deck.Slide{Number: 1, Label: "bad"}

	cases := []struct {
		name         string
		templateName string
		frontmatter  map[string]any
		body         string
		want         []string
	}{
		{
			// A literal dict key the target does not declare.
			name:         "supplied dict field is not declared",
			templateName: "helperunknownfieldslide",
			want:         []string{`unknown field "nope"`},
		},
		{
			// An undefaulted required field is still reported missing.
			name:         "required field missing with no default",
			templateName: "helperrequiredslide",
			want:         []string{"required", "must"},
		},
		{
			// A literal body the target's body rule disallows.
			name:         "literal body is disallowed by the target",
			templateName: "helperdisallowedbodyslide",
			want:         []string{"disallowed"},
		},
		{
			// A literal body whose heading names a declared child section.
			name:         "literal body names a child section",
			templateName: "helperliteralbadslide",
			want:         []string{"items", "child section"},
		},
		{
			// The same violation reached through a NON-literal body expression:
			// the slide's own source body is passed through as .raw.body.
			name:         "non-literal body names a child section",
			templateName: "helpernonliteralslide",
			body:         "# items\n",
			want:         []string{"items", "child section"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed := &slide.Slide{
				File:        "slides/1-bad.md",
				Template:    tc.templateName,
				Frontmatter: tc.frontmatter,
				Body:        tc.body,
			}
			_, err := RenderSlide(parsed, "bad", cfg, meta, 1, reg, funcMap)
			if err == nil {
				t.Fatalf("RenderSlide(%s) succeeded; want a section-helper error", tc.templateName)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// The layered-media probes below drive the REAL layered loader over two small
// on-disk library directories — an external `templates:` library and the
// deck-root templates/ layer — because internal/render cannot construct a
// two-layer template.Library through template's unexported test helpers. The
// trees are tiny and entirely local, so these unit tests still run under
// -short (t.TempDir, no network or port); they pin
// render.RenderSlide/sectionHelper/renderSection's per-layer media binding
// (library-layer-references):
//
//   - a local template's own `media` reference keeps the primary served base
//     and media filesystem, while a section it invokes from the external layer
//     resolves in the EXTERNAL layer — both the served URL and the media
//     filesystem;
//   - a declared child section (renderSection) contributed by the external
//     layer binds that layer the same way;
//   - a sole-layer library renders byte-for-byte as the non-layered
//     (template.LoadLibrary) path does, so a single-layer deck is unchanged.
//
// theme:/media: CSS rewriting is server-side (phase 4) and is deliberately not
// exercised here.

// writeLayerFile writes data at path, creating its parent directories.
func writeLayerFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// writeLayerManifest writes a minimal valid library.yaml at root.
func writeLayerManifest(t *testing.T, root, name string) {
	t.Helper()
	writeLayerFile(t, filepath.Join(root, template.LibraryFile),
		"name: "+name+"\nformat: 1\n")
}

// writeLayerTemplate writes one schema-free template directory
// root/<kindDir>/<name> with the given layout and a placeholder example. A
// schema-free template's example is not shape-checked, so its layout may call
// `{{ section … }}` or `{{ media … }}` without an authored example body.
func writeLayerTemplate(t *testing.T, root, kindDir, name, layout string) {
	t.Helper()
	base := filepath.Join(root, kindDir, name)
	writeLayerFile(t, filepath.Join(base, template.ManifestFile), "name: "+name+"\n")
	writeLayerFile(t, filepath.Join(base, template.LayoutFile), layout)
	writeLayerFile(t, filepath.Join(base, template.ExampleFile), "# "+name+"\n")
}

// writeLayerMedia writes one file under root/media.
func writeLayerMedia(t *testing.T, root, rel, data string) {
	t.Helper()
	writeLayerFile(t, filepath.Join(root, template.MediaDir, filepath.FromSlash(rel)), data)
}

// layeredMediaFixture writes a two-layer deck under two fresh temp dirs and
// returns them. extRoot is the external `templates:` library: one section,
// extmedia, whose layout resolves its own media ext-logo.svg. deckRoot holds
// the local layer at deckRoot/templates: a localslide that resolves its own
// local-logo.svg and calls the external extmedia through the `section` helper,
// plus a localchild that splices a declared child section. ext-logo.svg exists
// ONLY in the external layer, so a render that bound the external section to
// the local layer's media would fail to resolve it rather than silently pick a
// same-named file — which is what makes the URL AND the filesystem assertions
// meaningful.
func layeredMediaFixture(t *testing.T) (deckRoot, extRoot string) {
	t.Helper()

	extRoot = t.TempDir()
	writeLayerManifest(t, extRoot, "ext-lib")
	writeLayerTemplate(t, extRoot, template.SectionsDir, "extmedia",
		`<div class="ext-section"><img src="{{ media "ext-logo.svg" }}"></div>`)
	writeLayerMedia(t, extRoot, "ext-logo.svg", "<svg/>")

	deckRoot = t.TempDir()
	localRoot := filepath.Join(deckRoot, template.TemplatesDir)
	writeLayerManifest(t, localRoot, "local-lib")
	writeLayerTemplate(t, localRoot, template.SlidesDir, "localslide",
		`<section class="local-slide">`+
			`<img class="own" src="{{ media "local-logo.svg" }}">`+
			`{{ section "extmedia" }}</section>`)
	writeLayerTemplate(t, localRoot, template.SlidesDir, "localchild",
		`<section class="local-child">`+
			`<img class="own" src="{{ media "local-logo.svg" }}">`+
			`{{ range .blocks }}{{ . }}{{ end }}</section>`)
	writeLayerMedia(t, localRoot, "local-logo.svg", "<svg/>")
	return deckRoot, extRoot
}

// mustLayeredRegistry loads the two-layer fixture through the public layered
// loader and returns the merged library, its registry and the layout func map a
// deck executes with (`media` bound to the primary served base). LoadDeckLibrary
// sets each layer's Role, so the test never assigns a LibraryLayer.Role by hand.
func mustLayeredRegistry(t *testing.T, deckRoot, extRoot string) (*template.Library, *template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	lib, err := template.LoadDeckLibrary(deckRoot, extRoot)
	if err != nil {
		t.Fatalf("template.LoadDeckLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	// The primary layer's base; the renderer overrides `media` per layer when
	// the deck is two-layered, so only a sole-layer deck actually uses it.
	return lib, reg, template.LayoutFuncMap(lib.Media, mediaURLPrefix)
}

// TestRenderSlideLayeredSectionMedia proves the render-time `section` helper
// binds the called section's OWN owning layer for its execution
// (library-layer-references): from a LOCAL slide layout, the local template's
// own `media` reference keeps the primary `/assets/templates/media/` base and
// the local media filesystem, while the external section's `media` reference
// resolves in the EXTERNAL layer — `/assets/templates/external/media/<file>`
// served from the external layer's media filesystem — not the local one. The
// renderer's layer hook is installed because LoadDeckLibrary set the slide
// template's owning layer to a two-layer role.
func TestRenderSlideLayeredSectionMedia(t *testing.T) {
	deckRoot, extRoot := layeredMediaFixture(t)
	lib, reg, funcMap := mustLayeredRegistry(t, deckRoot, extRoot)

	// LoadDeckLibrary assigns the roles: the configured external root first,
	// the deck-root templates/ second.
	if len(lib.Layers) != 2 {
		t.Fatalf("len(Layers) = %d, want 2 (external + local)", len(lib.Layers))
	}
	if lib.Layers[0].Role != template.LayerRoleExternal {
		t.Errorf("Layers[0].Role = %v, want LayerRoleExternal", lib.Layers[0].Role)
	}
	if lib.Layers[1].Role != template.LayerRoleLocal {
		t.Errorf("Layers[1].Role = %v, want LayerRoleLocal", lib.Layers[1].Role)
	}

	// ext-logo.svg lives only in the external layer's media filesystem, so the
	// fs half of the per-layer binding is observable.
	if _, err := fs.Stat(lib.Layers[0].Media, "ext-logo.svg"); err != nil {
		t.Errorf("external layer media does not hold ext-logo.svg: %v", err)
	}
	if _, err := fs.Stat(lib.Layers[1].Media, "ext-logo.svg"); err == nil {
		t.Error("local layer media holds ext-logo.svg, want it external-only so the fixture proves the layer binding")
	}

	parsed := &slide.Slide{File: "slides/1-local.md", Template: "localslide"}
	got := renderSlideString(t, parsed, "local",
		&deck.Config{Title: "Layered"}, deck.Slide{Number: 1, Label: "local"}, 1, reg, funcMap)

	// The local template's own media keeps the primary /assets/templates/media base.
	if !strings.Contains(got, `<img class="own" src="/assets/templates/media/local-logo.svg">`) {
		t.Errorf("local template's own media did not keep the primary base:\n%s", got)
	}
	// The external section's media resolves in the EXTERNAL layer: URL and fs.
	if !strings.Contains(got, `<div class="ext-section"><img src="/assets/templates/external/media/ext-logo.svg"></div>`) {
		t.Errorf("external section media did not resolve under the external prefix:\n%s", got)
	}
	// The prefixes never cross: the local file is not addressed as external,
	// and the external file is not addressed with the primary layer's base.
	if strings.Contains(got, "/assets/templates/external/media/local-logo.svg") {
		t.Errorf("local media was addressed with the external prefix:\n%s", got)
	}
	if strings.Contains(got, "/assets/templates/media/ext-logo.svg") {
		t.Errorf("external media was addressed with the primary prefix:\n%s", got)
	}
}

// TestRenderSlideLayeredDeclaredSectionMedia proves the DECLARED child-section
// path (renderSection) binds the child template's own owning layer, exactly as
// the helper path does: a declared child instance whose template the external
// layer contributed resolves its `media` reference under the external prefix
// and external media filesystem, while the local slide's own media stays the
// primary base (library-layer-references).
func TestRenderSlideLayeredDeclaredSectionMedia(t *testing.T) {
	deckRoot, extRoot := layeredMediaFixture(t)
	_, reg, funcMap := mustLayeredRegistry(t, deckRoot, extRoot)

	parsed := &slide.Slide{
		File:     "slides/2-child.md",
		Template: "localchild",
		Sections: []slide.Section{
			{Name: "blocks", Level: 1, Index: 0, Template: "extmedia"},
		},
	}
	got := renderSlideString(t, parsed, "child",
		&deck.Config{Title: "Layered"}, deck.Slide{Number: 2, Label: "child"}, 1, reg, funcMap)

	if !strings.Contains(got, `<img class="own" src="/assets/templates/media/local-logo.svg">`) {
		t.Errorf("local slide's own media did not keep the primary base:\n%s", got)
	}
	if !strings.Contains(got, `<div class="ext-section"><img src="/assets/templates/external/media/ext-logo.svg"></div>`) {
		t.Errorf("declared external child section did not resolve its media in the external layer:\n%s", got)
	}
}

// writeSoleLayerLibrary writes a single-layer deck under a fresh temp dir: a
// deck-root templates/ library with one schema-free slide resolving its own
// media. With no configured `templates:` key this directory is the deck's only
// layer.
func writeSoleLayerLibrary(t *testing.T) string {
	t.Helper()
	deckRoot := t.TempDir()
	root := filepath.Join(deckRoot, template.TemplatesDir)
	writeLayerManifest(t, root, "solo-lib")
	writeLayerTemplate(t, root, template.SlidesDir, "soloslide",
		`<section class="solo-slide"><img src="{{ media "solo.svg" }}"></section>`)
	writeLayerMedia(t, root, "solo.svg", "<svg/>")
	return deckRoot
}

// TestRenderSlideSoleLayerMediaUnchanged proves a sole-layer library renders
// byte-for-byte as the non-layered path: the same slide rendered through the
// layered loader (LoadDeckLibrary, one layer, LayerRoleSole) and through the
// non-layered single-root loader (LoadLibrary) is identical, keeps the primary
// `/assets/templates/media/` base, and never emits an external prefix. A sole
// layer installs no layer hook, so RenderSlide leaves the shared namespace —
// and the existing `media` helper — exactly as before.
func TestRenderSlideSoleLayerMediaUnchanged(t *testing.T) {
	deckRoot := writeSoleLayerLibrary(t)

	// The layered resolver path: one layer, role sole.
	layered, err := template.LoadDeckLibrary(deckRoot, "")
	if err != nil {
		t.Fatalf("template.LoadDeckLibrary: %v", err)
	}
	if len(layered.Layers) != 1 {
		t.Fatalf("len(Layers) = %d, want 1 (the deck-root templates/ alone)", len(layered.Layers))
	}
	if layered.Layers[0].Role != template.LayerRoleSole {
		t.Errorf("Layers[0].Role = %v, want LayerRoleSole", layered.Layers[0].Role)
	}
	layeredReg, err := template.NewRegistryFromLibrary(layered)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary(layered): %v", err)
	}

	// The non-layered single-root path.
	plain, err := template.LoadLibrary(os.DirFS(deckRoot), template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	plainReg, err := template.NewRegistryFromLibrary(plain)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary(plain): %v", err)
	}

	parsed := &slide.Slide{File: "slides/1-solo.md", Template: "soloslide"}
	cfg := &deck.Config{Title: "Solo"}
	meta := deck.Slide{Number: 1, Label: "solo"}

	gotLayered := renderSlideString(t, parsed, "solo", cfg, meta, 1,
		layeredReg, template.LayoutFuncMap(layered.Media, mediaURLPrefix))
	gotPlain := renderSlideString(t, parsed, "solo", cfg, meta, 1,
		plainReg, template.LayoutFuncMap(plain.Media, mediaURLPrefix))

	if gotLayered != gotPlain {
		t.Errorf("sole-layer render differs from the non-layered path:\nlayered: %q\nplain:   %q", gotLayered, gotPlain)
	}
	if !strings.Contains(gotLayered, `<img src="/assets/templates/media/solo.svg">`) {
		t.Errorf("sole-layer media did not keep the primary base:\n%s", gotLayered)
	}
	if strings.Contains(gotLayered, "/assets/templates/external/media/") {
		t.Errorf("sole-layer render emitted an external media prefix:\n%s", gotLayered)
	}
}
