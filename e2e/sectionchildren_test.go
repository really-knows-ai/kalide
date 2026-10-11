package e2e

// This file is the plan.phase-04.task-1 end-to-end test for the section helper's
// child sections (requirements.requirement.child-constructor,
// requirements.requirement.children-forwarding,
// requirements.requirement.section-helper,
// requirements.requirement.section-helper-load-checks,
// requirements.requirement.template-composition,
// requirements.requirement.nested-section-validation,
// requirements.requirement.library-layer-references,
// requirements.requirement.section-data-context,
// requirements.requirement.item-context,
// requirements.requirement.template-context). It drives the real kalide binary
// the harness builds (harness.go) through a served deck, rather than any
// internal package, so it exercises the whole path an author takes.
//
// TestSectionChildrenDeckEndToEnd serves a one-layer deck whose slide layout
// composes children two ways, and pins on the served page:
//
//  1. a `{{ section "literalwrapper" … (child "card" …) (child "card" …) }}`
//     call whose two literal `child` arguments are the two accepted templates
//     of the wrapper's declared `card` section — the `template` selector on
//     each child (cardperson / cardcompany) chooses which is instantiated —
//     with the first carrying a nested `child "tag"` grandchild. The rendered
//     child instances carry their own `.item` index/number/count/section/
//     template/parent and their own `.raw` source;
//  2. a `{{ section "forwardwrapper" … (children "cards" .data.cards) }}`
//     forwarding of the slide's OWN authored `cards` entries, so each forwarded
//     child re-instantiates keeping its authored template (from
//     `.item.template`), its `.raw` field values and raw body, and its nested
//     `.data`;
//  3. a bad literal child introduced by a live edit (`(child "bogus")`, a
//     section the target does not declare) that fails the library load and is
//     served as the fail-fast full-page error carrying the path-qualified
//     templates error naming the calling template's template.yaml.
//
// TestSectionChildrenLayeredExternalEndToEnd serves a two-layer deck (deck-root
// local templates/ plus an external `templates:` layer) and pins that a literal
// `child` naming a section of a LOCAL target instantiates a section template
// that lives in the EXTERNAL layer, and that the child's own `{{ media … }}`
// reference resolves in its owning (external) layer and is served under
// server.ExternalMediaPath.
//
// Both tests build and drive the real binary, so they are skipped under -short.
// They reuse the harness and the SSE/reload helpers (openSSE, waitLine,
// drainFor, waitFor) declared alongside the live-reload test in
// livereload_test.go, and layeredLayerManifestPath declared alongside the
// external-library test in external_library_test.go.

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/really-knows-ai/kalide/internal/server"
)

// ---------------------------------------------------------------------------
// TestSectionChildrenDeckEndToEnd: the one-layer literal-child, forwarding and
// load-check fixture.
// ---------------------------------------------------------------------------

// writeSectionChildrenLibrary lays down the one-layer template library the
// first test serves under templates/:
//
//   - `main` is the slide: it declares the `cards` child section its layout
//     forwards, and its layout makes the two section-helper calls under test;
//   - `literalwrapper` is the target of the literal-child call: it declares the
//     `card` section accepting the TWO templates cardperson/cardcompany, so a
//     literal child must name its `template` selector;
//   - `forwardwrapper` is the target of the `children` forwarding: it declares
//     the same `cards` section its call fills;
//   - `cardperson` is a card template that declares its own `tag` child section
//     (the grandchild), `cardcompany` a card template with no children, and
//     `tag` the grandchild template.
func writeSectionChildrenLibrary(t *testing.T, h *Harness) {
	t.Helper()

	files := map[string]string{
		"templates/library.yaml": "name: sectionchildren\n" +
			"description: e2e fixture library for section child/children composition\n" +
			"format: 1\n",

		"templates/themes/plain/theme.css": "body { margin: 0; }\n",

		// main: the slide. Its layout composes the two calls under test. It
		// reports its own authored `.data` keys and its rendered `cards` count
		// (proving the helper calls contribute to their targets only and never
		// add a `.data` key or a rendered element to the caller), then makes the
		// literal-child call and the `children` forwarding.
		"templates/slides/main/template.yaml": `description: slide composing literal and forwarded section children
fields:
  - name: heading
    type: text
    required: true
body:
  mode: optional
sections:
  - name: cards
    accepted: [cardperson, cardcompany]
    max: 4
`,
		"templates/slides/main/layout.html.tmpl": `<section class="section-children-slide">` +
			`<h1 class="slide-heading">{{.heading}}</h1>` +
			`<span class="slide-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
			`<span class="slide-cards-count">{{if .cards}}{{len .cards}}{{else}}0{{end}}</span>` +
			`{{ section "literalwrapper" (dict "heading" "Literal Wrapper") nil ` +
			`(child "card" (dict "template" "cardperson" "name" "Ada") "Ada body" (child "tag" (dict "label" "Friend"))) ` +
			`(child "card" (dict "template" "cardcompany" "name" "Acme")) }}` +
			`{{ section "forwardwrapper" (dict "heading" "Forward Wrapper") nil (children "cards" .data.cards) }}` +
			`</section>`,
		"templates/slides/main/example.md": "---\ntemplate: main\nheading: Example\n---\n",

		// literalwrapper: the target of the literal-child call. It splices its
		// rendered `card` list and, in parallel, reports each passed child's
		// `.data.card` entry (its own `.item` descriptor and its `.raw` source
		// fields/body). Its example is empty: a template declaring sections is
		// parsed at load and an empty example declares no instances, which the
		// min-0 declaration accepts.
		"templates/sections/literalwrapper/template.yaml": `description: a wrapper receiving literal child arguments
fields:
  - name: heading
    type: text
sections:
  - name: card
    accepted: [cardperson, cardcompany]
    max: 4
body:
  mode: optional
`,
		"templates/sections/literalwrapper/layout.html.tmpl": `<div class="literal-wrapper" data-heading="{{.heading}}" data-card-data="{{if .data.card}}{{len .data.card}}{{else}}0{{end}}">` +
			`<span class="literal-wrapper-heading">{{.heading}}</span>` +
			`<span class="literal-wrapper-card-count">{{if .card}}{{len .card}}{{else}}0{{end}}</span>` +
			`{{range .data.card}}` +
			`<span class="literal-wrapper-data-card" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-section="{{.item.section}}" data-template="{{.item.template}}">` +
			`<span class="literal-wrapper-data-raw-name">{{if .raw.name}}{{.raw.name}}{{else}}absent{{end}}</span>` +
			`<span class="literal-wrapper-data-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
			`</span>` +
			`{{end}}` +
			`{{range .card}}{{.}}{{end}}` +
			`</div>`,
		"templates/sections/literalwrapper/example.md": "",

		// forwardwrapper: the target of the `children` forwarding. It declares
		// the `cards` section the forwarded entries fill, reports each forwarded
		// entry's `.data.cards` (its own `.item.template`, `.raw` fields/body
		// and nested `.data`), then splices its rendered `cards` list so the
		// forwarded children are proven to instantiate through their own
		// templates.
		"templates/sections/forwardwrapper/template.yaml": `description: a wrapper receiving forwarded authored children
fields:
  - name: heading
    type: text
sections:
  - name: cards
    accepted: [cardperson, cardcompany]
    max: 4
body:
  mode: optional
`,
		"templates/sections/forwardwrapper/layout.html.tmpl": `<div class="forward-wrapper" data-heading="{{.heading}}" data-cards-data="{{if .data.cards}}{{len .data.cards}}{{else}}0{{end}}">` +
			`<span class="forward-wrapper-heading">{{.heading}}</span>` +
			`{{range .data.cards}}` +
			`<span class="forward-wrapper-card" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-section="{{.item.section}}" data-template="{{.item.template}}">` +
			`<span class="forward-wrapper-raw-name">{{if .raw.name}}{{.raw.name}}{{else}}absent{{end}}</span>` +
			`<span class="forward-wrapper-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
			`<span class="forward-wrapper-data-keys">{{range $k, $v := .data}}{{$k}};{{end}}</span>` +
			`{{range .data.tag}}<span class="forward-wrapper-tag-raw-label">{{.raw.label}}</span>{{end}}` +
			`</span>` +
			`{{end}}` +
			`{{range .cards}}{{.}}{{end}}` +
			`</div>`,
		"templates/sections/forwardwrapper/example.md": "",

		// cardperson: one of the two accepted `card` templates. It declares its
		// own `tag` child section (the grandchild the literal child passes) and
		// prints its own `.item`, `.raw` and its rendered `tag` list.
		"templates/sections/cardperson/template.yaml": `description: a person card child with a nested tag section
fields:
  - name: name
    type: text
    required: true
sections:
  - name: tag
    accepted: [tag]
    max: 4
body:
  mode: optional
`,
		"templates/sections/cardperson/layout.html.tmpl": `<span class="cardperson" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}">` +
			`<span class="cardperson-name">{{.name}}</span>` +
			`<span class="cardperson-raw-name">{{if .raw.name}}{{.raw.name}}{{else}}absent{{end}}</span>` +
			`<span class="cardperson-raw-body">{{if .raw.body}}{{.raw.body}}{{else}}absent{{end}}</span>` +
			`{{range .tag}}{{.}}{{end}}` +
			`</span>`,
		"templates/sections/cardperson/example.md": "```\nname: Example\n```\n",

		// cardcompany: the other accepted `card` template, with no child
		// sections, so the `template` selector's two-way choice is observable.
		"templates/sections/cardcompany/template.yaml": `description: a company card child
fields:
  - name: name
    type: text
    required: true
body:
  mode: optional
`,
		"templates/sections/cardcompany/layout.html.tmpl": `<span class="cardcompany" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-first="{{.item.first}}" data-last="{{.item.last}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}">` +
			`<span class="cardcompany-name">{{.name}}</span>` +
			`<span class="cardcompany-raw-name">{{if .raw.name}}{{.raw.name}}{{else}}absent{{end}}</span>` +
			`</span>`,
		"templates/sections/cardcompany/example.md": "```\nname: Example\n```\n",

		// tag: the grandchild template, printing its own `.item` (including
		// `.item.parent`, the enclosing card instance's field values).
		"templates/sections/tag/template.yaml": `description: a tag child nested under a card
fields:
  - name: label
    type: text
    required: true
body:
  mode: disallowed
`,
		"templates/sections/tag/layout.html.tmpl": `<span class="tag" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}" data-parent-name="{{if .item.parent}}{{.item.parent.name}}{{end}}">` +
			`<span class="tag-label">{{.label}}</span>` +
			`<span class="tag-raw-label">{{.raw.label}}</span>` +
			`</span>`,
		"templates/sections/tag/example.md": "```\nlabel: Example\n```\n",
	}
	for rel, data := range files {
		h.WriteFile(rel, []byte(data))
	}
}

// sectionChildrenSlide is the deck slide using the `main` template: it authors
// two `cards` entries (one per accepted template), the first carrying a nested
// `tag`, so the layout's `children "cards" .data.cards` forwarding has a real
// authored tree to re-instantiate.
func sectionChildrenSlide(heading string) string {
	return "---\n" +
		"template: main\n" +
		"heading: " + heading + "\n" +
		"---\n\n" +
		"The slide body.\n" +
		"\n# cards\n" +
		"```\ntemplate: cardperson\nname: Ada Authored\n```\n" +
		"Authored Ada body.\n" +
		"## tag\n" +
		"```\ntemplate: tag\nlabel: Nested\n```\n" +
		"\n# cards\n" +
		"```\ntemplate: cardcompany\nname: Acme Authored\n```\n"
}

// TestSectionChildrenDeckEndToEnd is the end-to-end test described at the top of
// this file: it serves the one-layer deck, asserts the rendered literal child
// instances (with the selector-chosen template, their `.item` siblings and the
// nested grandchild) and the forwarded authored children (keeping their own
// templates, `.raw` fields/body and nested `.data`), then introduces a bad
// literal child by live edit and asserts the fail-fast, path-qualified
// templates error page.
func TestSectionChildrenDeckEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

	h.WriteFile("kalide.yaml", []byte(
		"title: Section Children Demo\n"+
			"theme: plain\n",
	))
	writeSectionChildrenLibrary(t, h)
	h.WriteFile("slides/1-children.md", []byte(sectionChildrenSlide("Children Deck")))

	h.Start()

	page, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if strings.Contains(page, "Deck error") {
		t.Fatalf("served page is the error page, want the children deck:\n%s", page)
	}

	// (1) The literal-child call: the wrapper reports two passed children and a
	// `.data.card` view of two; each rendered child carries its OWN `.item`
	// descriptor — index/number/count over the passed `card` set, the declared
	// section `card`, the selector-chosen template, and `.item.parent` present
	// (the wrapper's field values) — and its own `.raw` source.
	for _, want := range []string{
		// The slide's own authored `.data` still holds only `cards`, and its
		// rendered `cards` count is 2: neither helper call leaked into the
		// caller's `.data` or its rendered section list.
		`<span class="slide-data-keys">cards;</span>`,
		`<span class="slide-cards-count">2</span>`,
		// The target's own views: two passed `card` children, two `.data.card`
		// entries.
		`<div class="literal-wrapper" data-heading="Literal Wrapper" data-card-data="2">`,
		`<span class="literal-wrapper-card-count">2</span>`,
		// The first child: the `cardperson` template chosen by the `template`
		// selector, index 0 of 2, first not last, its `.raw` name and body.
		`<span class="cardperson" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="card" data-template="cardperson" data-parent="yes">` +
			`<span class="cardperson-name">Ada</span>` +
			`<span class="cardperson-raw-name">Ada</span>` +
			`<span class="cardperson-raw-body">Ada body</span>`,
		// The second child: the `cardcompany` template chosen by the selector,
		// index 1 of 2, last not first.
		`<span class="cardcompany" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="card" data-template="cardcompany" data-parent="yes">` +
			`<span class="cardcompany-name">Acme</span>` +
			`<span class="cardcompany-raw-name">Acme</span>`,
		// The nested grandchild under the first child: its own descriptor
		// (section `tag`, template `tag`, one of one) with `.item.parent` the
		// enclosing cardperson instance's field values.
		`<span class="tag" data-index="0" data-number="1" data-count="1" data-section="tag" data-template="tag" data-parent="yes" data-parent-name="Ada">` +
			`<span class="tag-label">Friend</span>` +
			`<span class="tag-raw-label">Friend</span>`,
		// The target's `.data.card` view of the same two children: each entry's
		// `.item.template` is the selector-chosen template and its `.raw` source
		// is the supplied field/body.
		`<span class="literal-wrapper-data-card" data-index="0" data-number="1" data-count="2" data-section="card" data-template="cardperson">` +
			`<span class="literal-wrapper-data-raw-name">Ada</span>` +
			`<span class="literal-wrapper-data-raw-body">Ada body</span>`,
		`<span class="literal-wrapper-data-card" data-index="1" data-number="2" data-count="2" data-section="card" data-template="cardcompany">` +
			`<span class="literal-wrapper-data-raw-name">Acme</span>` +
			`<span class="literal-wrapper-data-raw-body">absent</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("served deck page does not contain the literal-child value %q:\n%s", want, page)
		}
	}

	// (2) The `children` forwarding: the wrapper sees two forwarded cards and
	// each forwarded entry keeps its OWN authored template (from
	// `.item.template`), its `.raw` field values and raw body, and its nested
	// `.data`. The wrapper's rendered `cards` list splices the forwarded
	// children through their own templates (section `cards`, not `card`).
	for _, want := range []string{
		`<div class="forward-wrapper" data-heading="Forward Wrapper" data-cards-data="2">`,
		// First forwarded entry: the authored cardperson, its raw name and body,
		// and its nested `tag` raw value. The raw body is asserted by prefix
		// only: it is the author's original source, which may retain trailing
		// whitespace, and the assertion is about its content, not its framing.
		`<span class="forward-wrapper-card" data-index="0" data-number="1" data-count="2" data-section="cards" data-template="cardperson">` +
			`<span class="forward-wrapper-raw-name">Ada Authored</span>` +
			`<span class="forward-wrapper-raw-body">Authored Ada body.`,
		`<span class="forward-wrapper-data-keys">tag;</span>`,
		`<span class="forward-wrapper-tag-raw-label">Nested</span>`,
		// Second forwarded entry: the authored cardcompany, with no body and no
		// nested data.
		`<span class="forward-wrapper-card" data-index="1" data-number="2" data-count="2" data-section="cards" data-template="cardcompany">` +
			`<span class="forward-wrapper-raw-name">Acme Authored</span>` +
			`<span class="forward-wrapper-raw-body">absent</span>` +
			`<span class="forward-wrapper-data-keys"></span>`,
		// The forwarded children render through their own templates: the
		// wrapper's rendered `cards` list carries the same two instances, now
		// described by section `cards` and the authored raw values. The nested
		// tag under the first forwarded card renders through the `tag` template
		// with `.item.parent` the forwarded cardperson instance's values.
		`<span class="cardperson" data-index="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="cards" data-template="cardperson" data-parent="yes">` +
			`<span class="cardperson-name">Ada Authored</span>` +
			`<span class="cardperson-raw-name">Ada Authored</span>`,
		`<span class="cardperson-raw-body">Authored Ada body.`,
		`<span class="tag" data-index="0" data-number="1" data-count="1" data-section="tag" data-template="tag" data-parent="yes" data-parent-name="Ada Authored">` +
			`<span class="tag-label">Nested</span>` +
			`<span class="tag-raw-label">Nested</span>`,
		`<span class="cardcompany" data-index="1" data-number="2" data-count="2" data-first="false" data-last="true" data-section="cards" data-template="cardcompany" data-parent="yes">` +
			`<span class="cardcompany-name">Acme Authored</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("served deck page does not contain the forwarded-child value %q:\n%s", want, page)
		}
	}

	// (3) A bad literal child fails the library load. The slide template's
	// layout is edited to pass `(child "bogus")` to `literalwrapper`, a section
	// that target does not declare: the loader's step-4 child check rejects it,
	// path-qualified to the calling template's template.yaml, and the running
	// server publishes the full-page error rather than a broken deck
	// (never-serve-broken-deck, section-helper-load-checks).
	mainManifestPath := layeredLayerManifestPath(t, h, "templates", "slides", "main")
	h.WriteFile("templates/slides/main/layout.html.tmpl",
		[]byte(`<section>{{ section "literalwrapper" nil nil (child "bogus") }}</section>`+"\n"))

	const wantChildError = `child names section "bogus", which section template "literalwrapper" does not declare`
	waitFor(t, 10*time.Second, "the bad-literal-child error page", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(html.UnescapeString(b), wantChildError)
	})

	errBody, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET / (bad-child error page): %v", err)
	}
	if !strings.Contains(errBody, "Deck error") || !strings.Contains(errBody, "error-page__message") {
		t.Errorf("served page is not the full-page error shell after the bad literal child:\n%s", errBody)
	}
	unescaped := html.UnescapeString(errBody)
	for _, want := range []string{
		// The templates error is path-qualified with the calling template's
		// manifest, the same qualification every step-4 helper failure uses.
		mainManifestPath,
		wantChildError,
	} {
		if !strings.Contains(unescaped, want) {
			t.Errorf("bad-literal-child error does not name %q:\n%s", want, unescaped)
		}
	}

	// A graceful stop exits 0, releases the port and leaves no child behind; the
	// harness asserts those post-conditions in Stop.
	port := h.Port()
	if port == 0 {
		t.Fatal("start did not bind a port")
	}
	h.Stop()
	if _, err := h.Get("/"); err == nil {
		t.Errorf("GET / succeeded after Stop; port %d was not released", port)
	}
}

// ---------------------------------------------------------------------------
// TestSectionChildrenLayeredExternalEndToEnd: the cross-layer child fixture.
// ---------------------------------------------------------------------------

// writeLayeredChildrenFixture lays down the two-layer deck the second test
// serves: the deck-root templates/ local layer carrying the calling slide, the
// local target `localwrapper` that declares the `column` child section, and the
// deck theme; plus the external `../shared-lib` layer carrying the child
// template `extchild` and its OWN media file `ext-child.svg`. No name is
// carried by both layers, so the merged library passes library-layer-uniqueness.
func writeLayeredChildrenFixture(h *Harness) {
	h.WriteFile("kalide.yaml", []byte(
		"title: Layered Children Deck\n"+
			"theme: plain\n"+
			"templates: ../shared-lib\n",
	))
	h.WriteFile("slides/1-caller.md", []byte(
		"---\ntemplate: caller\nheading: Layered Children\n---\n"))

	// The deck-root templates/ local layer: the calling slide and the local
	// target declaring the external child section.
	h.WriteFile("templates/library.yaml",
		[]byte("name: local-children-lib\ndescription: deck-root local layer\nformat: 1\n"))
	h.WriteFile("templates/themes/plain/theme.css", []byte("body { margin: 0; }\n"))
	h.WriteFile("templates/slides/caller/template.yaml", []byte(
		"description: a local slide invoking a local target's external child\n"+
			"fields:\n"+
			"  - name: heading\n"+
			"    type: text\n"+
			"    required: true\n"+
			"body:\n"+
			"  mode: optional\n"))
	h.WriteFile("templates/slides/caller/layout.html.tmpl", []byte(
		`<section class="caller"><h1 class="caller-heading">{{.heading}}</h1>{{ section "localwrapper" (dict "heading" "Local Wrapper") nil (child "column" (dict "title" "External Child")) }}</section>`))
	h.WriteFile("templates/slides/caller/example.md",
		[]byte("---\ntemplate: caller\nheading: Example\n---\n"))

	h.WriteFile("templates/sections/localwrapper/template.yaml", []byte(
		"description: a local wrapper declaring an external child section\n"+
			"fields:\n"+
			"  - name: heading\n"+
			"    type: text\n"+
			"sections:\n"+
			"  - name: column\n"+
			"    accepted: [extchild]\n"+
			"    max: 4\n"+
			"body:\n"+
			"  mode: optional\n"))
	h.WriteFile("templates/sections/localwrapper/layout.html.tmpl", []byte(
		`<div class="local-wrapper" data-heading="{{.heading}}" data-column-data="{{if .data.column}}{{len .data.column}}{{else}}0{{end}}"><span class="local-wrapper-heading">{{.heading}}</span>{{range .column}}{{.}}{{end}}</div>`))
	h.WriteFile("templates/sections/localwrapper/example.md", []byte(""))

	// The external `templates: ../shared-lib` layer: the child template and its
	// own media. The external layer is validated standalone, so it carries only
	// self-contained content.
	h.WriteFile("../shared-lib/library.yaml",
		[]byte("name: shared-children-lib\ndescription: external deck layer\nformat: 1\n"))
	h.WriteFile("../shared-lib/sections/extchild/template.yaml", []byte(
		"description: an external child template serving its own layer's media\n"+
			"fields:\n"+
			"  - name: title\n"+
			"    type: text\n"+
			"    required: true\n"+
			"body:\n"+
			"  mode: disallowed\n"))
	h.WriteFile("../shared-lib/sections/extchild/layout.html.tmpl", []byte(
		`<span class="ext-child" data-index="{{.item.index}}" data-number="{{.item.number}}" data-count="{{.item.count}}" data-section="{{.item.section}}" data-template="{{.item.template}}" data-parent="{{if .item.parent}}yes{{else}}no{{end}}"><span class="ext-child-title">{{.title}}</span><span class="ext-child-raw-title">{{.raw.title}}</span><img class="ext-child-media" src="{{ media "ext-child.svg" }}"></span>`))
	h.WriteFile("../shared-lib/sections/extchild/example.md", []byte("```\ntitle: E\n```\n"))
	h.WriteFile("../shared-lib/media/ext-child.svg", []byte("<svg class=\"ext-child\"/>\n"))
}

// TestSectionChildrenLayeredExternalEndToEnd is the end-to-end test described at
// the top of this file: a local slide layout's literal `child` instantiates a
// section template owned by the EXTERNAL `templates:` layer (library-layer-
// references), and that child's own `{{ media … }}` reference resolves in its
// owning layer, so it is served under server.ExternalMediaPath.
func TestSectionChildrenLayeredExternalEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	writeLayeredChildrenFixture(h)

	h.Start()

	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if strings.Contains(body, "Deck error") {
		t.Fatalf("served page is the error page, want the two-layer children deck:\n%s", body)
	}

	for _, want := range []string{
		"Layered Children Deck",
		`<h1 class="caller-heading">Layered Children</h1>`,
		// The local target sees exactly one passed child in its `.data.column`
		// view.
		`<div class="local-wrapper" data-heading="Local Wrapper" data-column-data="1">`,
		// The child instantiates the external-layer `extchild` template, with
		// its own `.item` descriptor (section `column`, template `extchild`) and
		// its `.raw` source.
		`<span class="ext-child" data-index="0" data-number="1" data-count="1" data-section="column" data-template="extchild" data-parent="yes">` +
			`<span class="ext-child-title">External Child</span>` +
			`<span class="ext-child-raw-title">External Child</span>`,
		// The child's own `media` reference resolves in ITS owning (external)
		// layer and is emitted at the external media prefix.
		`<img class="ext-child-media" src="` + server.ExternalMediaPath + `ext-child.svg">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("served layered children page is missing %q:\n%s", want, body)
		}
	}

	// The external layer's media file is actually served at that prefix: the URL
	// rendered above is fetchable exactly where render emitted it.
	if got, err := h.GetString(server.ExternalMediaPath + "ext-child.svg"); err != nil {
		t.Errorf("GET %sext-child.svg: %v", server.ExternalMediaPath, err)
	} else if !strings.Contains(got, `class="ext-child"`) {
		t.Errorf("external media route does not serve the external layer's child media: %q", got)
	}

	// A graceful stop exits 0, releases the port and leaves no child behind; the
	// harness asserts those post-conditions in Stop.
	port := h.Port()
	if port == 0 {
		t.Fatal("start did not bind a port")
	}
	h.Stop()
	if _, err := h.Get("/"); err == nil {
		t.Errorf("GET / succeeded after Stop; port %d was not released", port)
	}
}
