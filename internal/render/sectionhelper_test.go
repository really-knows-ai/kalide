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

// The layered template-composition probes below drive executeLayeredLayout's
// per-layer namespace (feedback-28): a two-layer deck's layout may invoke
// another layout with `{{ template "<layout name>" . }}` — the composition
// parseLayouts' shared namespace supports — and that call must resolve while
// each `{{ media … }}` reference still resolves in the EXECUTING layout's own
// layer.
//
// A loader subtlety shapes how these fixtures are built. LoadLibrary's step 7
// (checkLibraryExamples) executes each template's layout in isolation, parsed
// alone, so a DISK layout carrying `{{ template "<other>" . }}` fails to load
// with `no such template` before any render happens. These probes therefore
// load ordinary, self-contained libraries through the public LoadDeckLibrary
// first and then install the composition layout text on the loaded
// *template.LibraryTemplate (exactly the text an authored composition layout
// would carry) before building the registry; the render path under test reads
// only that in-memory LayoutText. This mirrors internal/template's own tests,
// which set a LibraryTemplate's LayoutText directly, and keeps the probe on the
// public two-layer loader (real layer roles and per-layer media filesystems)
// rather than a hand-built library.
//
// The layout name a `{{ template … }}` call must name is layoutName(t) —
// t.Layout.Name, which for a library-loaded template is its layout file path
// (loadlibrary.go: path.Join(layerRoot, dir, layout.html.tmpl)). The probes
// read that name back off the loaded template, so the literal and the
// namespace key cannot drift.

// layeredCompositionFixture writes a two-layer deck whose DISK layouts are all
// self-contained (no `{{ template }}` call), so LoadDeckLibrary accepts them at
// its step-7 example runs; a probe installs the composition text afterwards.
// The external layer carries three sections — extcaller/extcallee (the
// template-composition pair) and extinvoked (invoked from the local layer) —
// and the local layer two slides, localslide (which calls the external section
// through the `section` helper) and localinvokee (invoked as a layout). Every
// media file lives in one layer only — ext-*.svg externally, local-*.svg
// locally — so a media URL reveals which layer's filesystem resolved it and a
// mis-bound layer fails rather than silently matching a same-named file.
func layeredCompositionFixture(t *testing.T) (deckRoot, extRoot string) {
	t.Helper()

	extRoot = t.TempDir()
	writeLayerManifest(t, extRoot, "ext-comp-lib")
	writeLayerTemplate(t, extRoot, template.SectionsDir, "extcaller",
		`<div class="ext-caller"><img class="caller" src="{{ media "ext-one.svg" }}"></div>`)
	writeLayerTemplate(t, extRoot, template.SectionsDir, "extcallee",
		`<div class="ext-callee"><img class="callee" src="{{ media "ext-two.svg" }}"></div>`)
	writeLayerTemplate(t, extRoot, template.SectionsDir, "extinvoked",
		`<div class="ext-invoked"><img class="invoked" src="{{ media "ext-logo.svg" }}"></div>`)
	writeLayerMedia(t, extRoot, "ext-one.svg", "<svg/>")
	writeLayerMedia(t, extRoot, "ext-two.svg", "<svg/>")
	writeLayerMedia(t, extRoot, "ext-logo.svg", "<svg/>")

	deckRoot = t.TempDir()
	localRoot := filepath.Join(deckRoot, template.TemplatesDir)
	writeLayerManifest(t, localRoot, "local-comp-lib")
	writeLayerTemplate(t, localRoot, template.SlidesDir, "localslide",
		`<section class="local-slide"><img class="own" src="{{ media "local-logo.svg" }}">{{ section "extcaller" }}</section>`)
	writeLayerTemplate(t, localRoot, template.SlidesDir, "localinvokee",
		`<section class="local-invokee"><img class="invokee" src="{{ media "local-logo2.svg" }}"></section>`)
	writeLayerMedia(t, localRoot, "local-logo.svg", "<svg/>")
	writeLayerMedia(t, localRoot, "local-logo2.svg", "<svg/>")
	writeLayerMedia(t, localRoot, "local-invoked.svg", "<svg/>")
	return deckRoot, extRoot
}

// loadedLayoutName returns the layout name a `{{ template … }}` call must use
// to invoke the named loaded slide or section template: layoutName(t), the name
// executeLayeredLayout/namespaceForLayer keys the per-layer namespace by. For a
// library-loaded template it is the layout file path (loadlibrary.go), so the
// probe builds the call literal from the loaded template rather than guessing.
func loadedLayoutName(t *testing.T, lib *template.Library, name string) string {
	t.Helper()
	lt := lib.Slides[name]
	if lt == nil {
		lt = lib.Sections[name]
	}
	if lt == nil || lt.Layout == nil {
		t.Fatalf("loaded template %q has no parsed layout", name)
	}
	return lt.Layout.Name()
}

// installLayoutText replaces the named loaded template's layout text with
// layout before NewRegistryFromLibrary reads it into the registry definition.
// The loader's step-7 example run cannot accept a disk layout with a
// cross-template `{{ template … }}` call (see the probe comment above), so a
// composition layout is installed here instead.
func installLayoutText(t *testing.T, lib *template.Library, name, layout string) {
	t.Helper()
	lt := lib.Slides[name]
	if lt == nil {
		lt = lib.Sections[name]
	}
	if lt == nil {
		t.Fatalf("loaded template %q not found", name)
	}
	lt.LayoutText = layout
}

// mustLayeredRegistryInstalled loads the two-layer fixture through the public
// layered loader, lets install replace any layout text a probe needs, then
// builds the registry and layout func map exactly as mustLayeredRegistry does.
func mustLayeredRegistryInstalled(t *testing.T, deckRoot, extRoot string, install func(*template.Library)) (*template.Library, *template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	lib, err := template.LoadDeckLibrary(deckRoot, extRoot)
	if err != nil {
		t.Fatalf("template.LoadDeckLibrary: %v", err)
	}
	if install != nil {
		install(lib)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	return lib, reg, template.LayoutFuncMap(lib.Media, mediaURLPrefix)
}

// TestRenderSlideLayeredTemplateCompositionLocal pins feedback-28 for the
// local-to-local case: a local slide layout invokes ANOTHER local layout with
// `{{ template "<name>" . }}` and both layouts resolve their own `{{ media … }}`
// under the single/primary /assets/templates/media/ base. Before
// executeLayeredLayout parsed every registered layout into a per-layer
// namespace, the call failed with "no such template".
func TestRenderSlideLayeredTemplateCompositionLocal(t *testing.T) {
	deckRoot, extRoot := layeredCompositionFixture(t)

	_, reg, funcMap := mustLayeredRegistryInstalled(t, deckRoot, extRoot, func(lib *template.Library) {
		invokee := loadedLayoutName(t, lib, "localinvokee")
		installLayoutText(t, lib, "localslide",
			`<section class="local-slide"><img class="own" src="{{ media "local-logo.svg" }}">{{ template "`+invokee+`" . }}</section>`)
	})

	parsed := &slide.Slide{File: "slides/1-local.md", Template: "localslide"}
	got := renderSlideString(t, parsed, "local",
		&deck.Config{Title: "Layered"}, deck.Slide{Number: 1, Label: "local"}, 1, reg, funcMap)

	for _, want := range []string{
		`<img class="own" src="/assets/templates/media/local-logo.svg">`,
		`<img class="invokee" src="/assets/templates/media/local-logo2.svg">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered slide does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/assets/templates/external/") {
		t.Errorf("local composition emitted an external prefix:\n%s", got)
	}
}

// TestRenderSlideLayeredTemplateCompositionExternal pins feedback-28 for the
// external-to-external case: a local slide calls an external section through
// the `section` helper, and that external section invokes ANOTHER external
// layout with `{{ template "<name>" . }}`; both resolve their media under the
// external /assets/templates/external/media/ base. The external layout's
// composition runs in the external layer's namespace, so the invoked layout's
// media must not fall back to the primary (local) prefix.
func TestRenderSlideLayeredTemplateCompositionExternal(t *testing.T) {
	deckRoot, extRoot := layeredCompositionFixture(t)

	_, reg, funcMap := mustLayeredRegistryInstalled(t, deckRoot, extRoot, func(lib *template.Library) {
		callee := loadedLayoutName(t, lib, "extcallee")
		installLayoutText(t, lib, "extcaller",
			`<div class="ext-caller"><img class="caller" src="{{ media "ext-one.svg" }}">{{ template "`+callee+`" . }}</div>`)
	})

	parsed := &slide.Slide{File: "slides/2-caller.md", Template: "localslide"}
	got := renderSlideString(t, parsed, "caller",
		&deck.Config{Title: "Layered"}, deck.Slide{Number: 2, Label: "caller"}, 1, reg, funcMap)

	for _, want := range []string{
		`<img class="caller" src="/assets/templates/external/media/ext-one.svg">`,
		`<img class="callee" src="/assets/templates/external/media/ext-two.svg">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered slide does not contain %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{
		`/assets/templates/media/ext-one.svg`,
		`/assets/templates/media/ext-two.svg`,
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("external composition media resolved with the primary prefix: found %q:\n%s", forbidden, got)
		}
	}
}

// TestRenderSlideLayeredTemplateCompositionCrossLayer pins feedback-28 for the
// cross-layer case: a LOCAL layout invokes an EXTERNAL layout with
// `{{ template "<name>" . }}`, and the invoked external layout's `{{ media … }}`
// resolves in the EXECUTING (local) layer — the local /assets/templates/media/
// base and the local media filesystem — not the invoked layout's own external
// layer. The invoked layout names a LOCAL-only media file, so a render bound to
// the external layer would fail to resolve it rather than silently pick a
// same-path file.
func TestRenderSlideLayeredTemplateCompositionCrossLayer(t *testing.T) {
	deckRoot, extRoot := layeredCompositionFixture(t)

	lib, reg, funcMap := mustLayeredRegistryInstalled(t, deckRoot, extRoot, func(lib *template.Library) {
		invoked := loadedLayoutName(t, lib, "extinvoked")
		installLayoutText(t, lib, "extinvoked",
			`<div class="ext-invoked"><img class="invoked" src="{{ media "local-invoked.svg" }}"></div>`)
		installLayoutText(t, lib, "localslide",
			`<section class="local-slide"><img class="own" src="{{ media "local-logo.svg" }}">{{ template "`+invoked+`" . }}</section>`)
	})

	// The invoked layout's media file lives only in the local layer, so the
	// filesystem half of the executing-layer binding is observable.
	if _, err := fs.Stat(lib.Layers[0].Media, "local-invoked.svg"); err == nil {
		t.Error("external layer media holds local-invoked.svg, want it local-only so the fixture proves the executing-layer binding")
	}
	if _, err := fs.Stat(lib.Layers[1].Media, "local-invoked.svg"); err != nil {
		t.Errorf("local layer media does not hold local-invoked.svg: %v", err)
	}

	parsed := &slide.Slide{File: "slides/3-cross.md", Template: "localslide"}
	got := renderSlideString(t, parsed, "cross",
		&deck.Config{Title: "Layered"}, deck.Slide{Number: 3, Label: "cross"}, 1, reg, funcMap)

	for _, want := range []string{
		`<img class="own" src="/assets/templates/media/local-logo.svg">`,
		`<img class="invoked" src="/assets/templates/media/local-invoked.svg">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered slide does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/assets/templates/external/media/local-invoked.svg") {
		t.Errorf("cross-layer invoked layout media used the external prefix, want the executing local layer's base:\n%s", got)
	}
}

// The child-argument probes below are the unit-test deliverable for
// plan.phase-02.task-4: the render-time children path
// (renderer.sectionHelper → template.ResolveSectionCallLayered →
// renderer.renderChildren) is driven end-to-end through RenderSlide with an
// in-code registry — no filesystem, so they run under -short — whose wrapper
// section declares the child sections its call fills. Each test pins one clause
// of renderChildren's contract:
//
//   - a literal `child` and a forwarded `children` list each render as its own
//     sibling instance with its own `.item` (0-based index, 1-based number,
//     count, first/last over the FULL passed set, the named section and the
//     instantiated template, and `parent` the target's field values), its own
//     `.raw` source view and its own `.data` view of its own nested children;
//   - the target declaration's min/max is enforced at render, below/above the
//     bound failing as a render error naming the declaration and the parent
//     template;
//   - a re-entrant/over-deep child chain stops at renderChildren's
//     maxSectionNesting bound with the pinned depth error.
//
// The registry is built in code the way mustSectionHelperRegistry is: it never
// runs Registry.Validate, because the render-time helper's field/body and
// min/max checks (ResolveSectionCallLayered) are exactly what these tests pin,
// so the invalid calls must stay reachable at render.
const (
	// childrenSlideLayout is the caller: it makes one `section` call whose child
	// arguments are a literal `child "column"` (with a body and a nested
	// `child "tag"`) followed by a `children "column" .data.column` forwarding
	// of the slide's own two authored `column` entries. It also reports its own
	// `.data` keys, so the test can pin that the call adds nothing to the
	// CALLER's data.
	childrenSlideLayout = `<section class="children-slide"><h1>{{.title}}</h1>` +
		`<span class="slide-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`{{section "childrenwrapper" (dict "heading" "Wrapper H") nil (child "column" (dict "label" "Literal") "Literal body" (child "tag" (dict "name" "LT"))) (children "column" .data.column)}}` +
		`</section>`

	// childrenWrapperLayout is the `section` call's target: it splices its
	// rendered `column` list (the passed children, in passed order) and reports
	// the length of its own `.data.column` view plus each entry's template, so
	// the test pins the target-side data view the call builds.
	childrenWrapperLayout = `<div class="wrapper" data-count="{{len .data.column}}" data-child-count="{{len .column}}">` +
		`{{range .column}}{{.}}{{end}}` +
		`<span class="wrapper-data-templates">{{range .data.column}}{{.item.template}};{{end}}</span>` +
		`</div>`

	// childrenItemLayout is the instantiated child template: it prints its own
	// `.item` descriptor (including `.item.parent.heading`, the target's field
	// values), its own `.raw` source view (field and body) and its own `.data`
	// keys, then splices its own rendered `tag` children.
	childrenItemLayout = `<span class="child-item" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" data-parent-heading="{{if .item.parent}}{{.item.parent.heading}}{{end}}">` +
		`<span class="child-label">{{.label}}</span>` +
		`<span class="child-raw-label">{{if .raw.label}}{{.raw.label}}{{else}}absent{{end}}</span>` +
		`<span class="child-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
		`<span class="child-body">{{if .body}}{{.body}}{{else}}absent{{end}}</span>` +
		`<span class="child-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
		`{{range .tag}}{{.}}{{end}}` +
		`</span>`

	childTagLayout = `<b class="child-tag">{{.name}}</b>`

	// childrenRangeLayout is the target of the min/max probes; it is never
	// executed because resolution rejects the set first.
	childrenRangeLayout = `<div class="range-wrapper">{{range .column}}{{.}}{{end}}</div>`

	childrenUnderLayout = `<section>{{section "childrenrangewrapper" nil nil (child "column" (dict "label" "Only"))}}</section>`
	childrenOverLayout  = `<section>{{section "childrenrangewrapper" nil nil (child "column" (dict "label" "A")) (child "column" (dict "label" "B")) (child "column" (dict "label" "C"))}}</section>`
)

// mustSectionChildrenRegistry builds the in-code render-time child-argument
// probe library: a slide whose layout passes a literal `child` and a forwarded
// `children` to a wrapper section, the wrapper declaring `column` children, the
// `childrenitem` child template declaring its own `tag` child, a `childtag`
// leaf, and a min/max wrapper with its under/over slides. It registers templates
// directly (no content filesystem), so it needs no disk and runs under -short.
func mustSectionChildrenRegistry(t *testing.T) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	reg := template.NewRegistry(nil)
	for _, tmpl := range []*template.Template{
		{
			Name:     "childrenslide",
			Usage:    template.UsageSlide,
			Fields:   []template.Field{{Name: "title", Type: template.FieldText}},
			Sections: []template.SectionDecl{{Name: "column", Accepted: []string{"childrenitem"}, Max: 5}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: childrenSlideLayout},
		},
		{
			Name:     "childrenwrapper",
			Usage:    template.UsageSection,
			Fields:   []template.Field{{Name: "heading", Type: template.FieldText}},
			Sections: []template.SectionDecl{{Name: "column", Accepted: []string{"childrenitem"}, Max: 5}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: childrenWrapperLayout},
		},
		{
			Name:     "childrenitem",
			Usage:    template.UsageSection,
			Fields:   []template.Field{{Name: "label", Type: template.FieldText}},
			Sections: []template.SectionDecl{{Name: "tag", Accepted: []string{"childtag"}}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: childrenItemLayout},
		},
		{
			Name:   "childtag",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "name", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyDisallowed},
			Layout: template.Layout{Text: childTagLayout},
		},
		{
			Name:   "childrenunderslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: childrenUnderLayout},
		},
		{
			Name:   "childrenoverslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: childrenOverLayout},
		},
		{
			Name:     "childrenrangewrapper",
			Usage:    template.UsageSection,
			Sections: []template.SectionDecl{{Name: "column", Accepted: []string{"childrenitem"}, Min: 2, Max: 2}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: childrenRangeLayout},
		},
	} {
		if err := reg.Register(tmpl); err != nil {
			t.Fatalf("register template %q: %v", tmpl.Name, err)
		}
	}
	return reg, template.LayoutFuncMap(nil, "")
}

// TestRenderSlideSectionHelperChildren pins the render-time instantiation of a
// `section` call's child arguments through renderChildren: a literal `child`
// and the entries of a `children` forwarding render as one sibling group, each
// with its own `.item` descriptor over the full passed set (index/number/count/
// first/last/section/template and a `parent` that is the target's field
// values), its own `.raw` source view and its own `.data` view (recursively for
// the literal child's nested `tag`), while the call adds nothing to the
// caller's `.data`
// (item-context, raw-source-context, section-data-context, section-template-context).
func TestRenderSlideSectionHelperChildren(t *testing.T) {
	reg, funcMap := mustSectionChildrenRegistry(t)
	cfg := &deck.Config{Title: "Children Deck"}

	parsed := &slide.Slide{
		File:        "slides/1-children.md",
		Template:    "childrenslide",
		Frontmatter: map[string]any{"title": "Children"},
		Sections: []slide.Section{
			// Two authored `column` entries are forwarded through `children`;
			// the first carries a nested `tag`, so the forwarded child's OWN
			// `.data`/rendered list is exercised, not only its `.raw`.
			{
				Name: "column", Level: 1, Index: 0, Template: "childrenitem",
				Frontmatter: map[string]any{"label": "Fwd A"}, Body: "Fwd A body",
				Children: []slide.Section{
					{Name: "tag", Level: 2, Index: 0, Template: "childtag", Frontmatter: map[string]any{"name": "FA"}},
				},
			},
			{
				Name: "column", Level: 1, Index: 1, Template: "childrenitem",
				Frontmatter: map[string]any{"label": "Fwd B"},
			},
		},
	}
	got := renderSlideString(t, parsed, "children", cfg, deck.Slide{Number: 1, Label: "children"}, 1, reg, funcMap)

	for _, want := range []string{
		// The target's rendered child list holds all three passed children (the
		// literal `child` first, then the two forwarded entries), and its own
		// `.data.column` view holds the same three, each with its instantiated
		// template.
		`<div class="wrapper" data-count="3" data-child-count="3">`,
		`<span class="wrapper-data-templates">childrenitem;childrenitem;childrenitem;</span>`,

		// The literal child: its own sibling descriptor (index 0, number/count
		// 1/3, first not last, section "column", template "childrenitem"), its
		// `.item.parent` the target's field values (heading "Wrapper H"), its
		// own `.raw` (the authored field and body), its own `.data` (the nested
		// tag) and its own rendered nested tag.
		`<span class="child-item" data-index="0" data-number="1" data-count="3" data-first="true" data-last="false" data-section="column" data-template="childrenitem" data-parent="yes" data-parent-heading="Wrapper H">` +
			`<span class="child-label">Literal</span>` +
			`<span class="child-raw-label">Literal</span>` +
			`<span class="child-raw-body">Literal body</span>` +
			"<span class=\"child-body\"><p>Literal body</p>\n</span>" +
			`<span class="child-data-keys">tag;</span>` +
			`<b class="child-tag">LT</b>`,

		// The first forwarded entry keeps its own raw fields/body and its own
		// nested tag, and is the second of the same sibling group (index 1,
		// count 3).
		`<span class="child-item" data-index="1" data-number="2" data-count="3" data-first="false" data-last="false" data-section="column" data-template="childrenitem" data-parent="yes" data-parent-heading="Wrapper H">` +
			`<span class="child-label">Fwd A</span>` +
			`<span class="child-raw-label">Fwd A</span>` +
			`<span class="child-raw-body">Fwd A body</span>` +
			"<span class=\"child-body\"><p>Fwd A body</p>\n</span>" +
			`<span class="child-data-keys">tag;</span>` +
			`<b class="child-tag">FA</b>`,

		// The second forwarded entry is last of the group (index 2, count 3),
		// with no body and no nested children: its own `.raw` body and `.data`
		// are empty but present.
		`<span class="child-item" data-index="2" data-number="3" data-count="3" data-first="false" data-last="true" data-section="column" data-template="childrenitem" data-parent="yes" data-parent-heading="Wrapper H">` +
			`<span class="child-label">Fwd B</span>` +
			`<span class="child-raw-label">Fwd B</span>` +
			`<span class="child-raw-body">absent</span>` +
			`<span class="child-body">absent</span>` +
			`<span class="child-data-keys"></span>`,

		// The call contributes to the TARGET only: the caller's `.data` still
		// carries only its authored `column` section, never the helper target.
		`<span class="slide-data-keys">column;</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered slide does not contain %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "childrenwrapper;") {
		t.Errorf("the helper call leaked its target into the caller's .data:\n%s", got)
	}
}

// TestRenderSlideSectionHelperChildrenRange pins the render-time min/max
// enforcement over a helper call's passed child set: a set below the target
// declaration's minimum or above its maximum fails as a render error naming the
// declaration and the parent template. The declaration is on the call's TARGET
// (`childrenrangewrapper`), so the counts are checked by ResolveChildren during
// ResolveSectionCallLayered, before any layout executes
// (nested-section-validation, section-helper, item-context).
func TestRenderSlideSectionHelperChildrenRange(t *testing.T) {
	reg, funcMap := mustSectionChildrenRegistry(t)
	cfg := &deck.Config{}
	meta := deck.Slide{Number: 1, Label: "range"}

	cases := []struct {
		name         string
		templateName string
		want         []string
	}{
		{
			name:         "a set below the declared minimum",
			templateName: "childrenunderslide",
			want: []string{
				`section "column"`,
				`parent template "childrenrangewrapper"`,
				"has 1 child(ren)",
				"minimum is 2",
			},
		},
		{
			name:         "a set above the declared maximum",
			templateName: "childrenoverslide",
			want: []string{
				`section "column"`,
				`parent template "childrenrangewrapper"`,
				"has 3 child(ren)",
				"maximum is 2",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed := &slide.Slide{File: "slides/1-range.md", Template: tc.templateName}
			_, err := RenderSlide(parsed, "range", cfg, meta, 1, reg, funcMap)
			if err == nil {
				t.Fatalf("RenderSlide(%s) succeeded; want a render-time child-count error", tc.templateName)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// buildDeepChildArg builds the nested `child "nest"` argument chain of the given
// depth: a chain of depth nodes, each a `child "nest" nil nil <next>` with the
// innermost a leaf `(child "nest" nil nil)`. The rendered recursion follows it
// one level per node, so a depth of maxSectionNesting+1 trips the guard.
func buildDeepChildArg(depth int) string {
	arg := `(child "nest" nil nil)`
	for i := 1; i < depth; i++ {
		arg = `(child "nest" nil nil ` + arg + `)`
	}
	return arg
}

// mustSectionChildrenDepthRegistry builds an in-code registry for the depth
// probe: a slide whose layout makes one `section "deepchildrenwrapper"` call
// with a depth-deep nested `child "nest"` chain, and the wrapper section that
// declares `nest` (accepting itself) and splices each level's rendered child.
func mustSectionChildrenDepthRegistry(t *testing.T, depth int) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	reg := template.NewRegistry(nil)
	for _, tmpl := range []*template.Template{
		{
			Name:   "deepchildrenslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: `<section>{{section "deepchildrenwrapper" nil nil ` + buildDeepChildArg(depth) + `}}</section>`},
		},
		{
			Name:     "deepchildrenwrapper",
			Usage:    template.UsageSection,
			Sections: []template.SectionDecl{{Name: "nest", Accepted: []string{"deepchildrenwrapper"}}},
			Body:     template.BodyRule{Mode: template.BodyOptional},
			Layout:   template.Layout{Text: `<div class="deep-wrapper">{{range .nest}}{{.}}{{end}}</div>`},
		},
	} {
		if err := reg.Register(tmpl); err != nil {
			t.Fatalf("register template %q: %v", tmpl.Name, err)
		}
	}
	return reg, template.LayoutFuncMap(nil, "")
}

// TestRenderSlideSectionHelperChildrenDepthBound pins renderChildren's depth
// guard on the real render path: a child chain deeper than maxSectionNesting
// stops at the bound with the pinned error rather than recursing without bound.
// The chain is a layout-level fixture — a depth-(maxSectionNesting+1) nested
// `child` argument — so sectionHelper's resolution succeeds and the guard fires
// while renderChildren walks the resolved tree (child-constructor,
// children-forwarding).
func TestRenderSlideSectionHelperChildrenDepthBound(t *testing.T) {
	reg, funcMap := mustSectionChildrenDepthRegistry(t, maxSectionNesting+1)

	parsed := &slide.Slide{File: "slides/1-deep.md", Template: "deepchildrenslide"}
	_, err := RenderSlide(parsed, "deep", &deck.Config{}, deck.Slide{Number: 1, Label: "deep"}, 1, reg, funcMap)
	if err == nil {
		t.Fatal("RenderSlide(deepchildrenslide) succeeded; want the child-nesting depth bound to trip")
	}
	if want := "section children nested too deeply: depth exceeds 64"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain the depth-bound message %q", err, want)
	}
}

// TestRenderChildrenDepthGuard asserts renderChildren's bound directly: at the
// maxSectionNesting depth the guard fails before any child is rendered, with the
// pinned error. This covers the guard even if a resolved tree is ever handed to
// renderChildren without going through sectionHelper.
func TestRenderChildrenDepthGuard(t *testing.T) {
	r := &renderer{sectionDepth: maxSectionNesting}
	children := []*template.ResolvedChild{{Call: &template.SectionCall{Template: &template.Template{Name: "leaf", Usage: template.UsageSection}}}}

	_, _, err := r.renderChildren(children, nil)
	want := "section children nested too deeply: depth exceeds 64"
	if err == nil {
		t.Fatalf("renderChildren at depth %d error = nil, want %q", maxSectionNesting, want)
	}
	if err.Error() != want {
		t.Errorf("renderChildren at depth %d error = %q, want %q", maxSectionNesting, err.Error(), want)
	}
}
