package render

import (
	htmltmpl "html/template"
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
