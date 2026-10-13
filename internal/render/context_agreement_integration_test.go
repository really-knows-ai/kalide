package render

import (
	"bytes"
	htmltmpl "html/template"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// This file is the integration-test deliverable for
// section-helpers/plan.phase-07.task-5: the reserved `.raw`/`.data` context
// that one shared template.RawContext/template.DataContext now feeds both the
// LOAD-TIME example execution (templates-dir-validation step 7,
// checkLibraryExample) and the RENDER-TIME execution (internal/render) agrees
// for the SAME authored content.
//
// Strategy and limitation. checkLibraryExample is unexported in
// internal/template and DISCARDS the bytes it executes (it builds a
// strings.Builder and returns only an error), and internal/template cannot
// import internal/render; there is therefore no way to capture the load-time
// executed HTML from a cross-package test without adding a source seam. So the
// test, which must live in internal/render (the only package that can reach
// both halves):
//
//   - drives the real LOAD-TIME half through template.LoadLibrary, whose step 7
//     executes each example layout — here UNGUARDED — against the reserved
//     `.raw`/`.data` context, so a successful load proves that half supplied the
//     context (and the same shape);
//   - drives the real RENDER-TIME half through the full library→parser→render
//     pipeline (deck.LoadConfig/LoadSlides, slide.Parse, RenderDeck);
//   - builds the load-time-EQUIVALENT context for the identical authored content
//     with the SAME shared builders checkLibraryExample uses
//     (template.RawContext and template.DataContext over a neutral
//     template.ContextNode tree adapted from the example bytes) — plus the
//     typed-schema fields validateExampleBlockAt converts value-only, supplied
//     as that conversion's documented output because it is an unexported local
//     closure — executes the same layout with it, and requires the result to
//     appear VERBATIM in the render-time page.
//
// The equality is on OBSERVABLE OUTPUT of `.raw`/`.data`/`.item` source values
// and of the value-only-converted typed-schema fields, not the discarded
// load-time bytes: if the render half had derived any of them from anything
// other than the shared builders / the same conversion rules, the fragment
// would differ. The limitation is exactly that checkLibraryExample's own
// executed bytes are not captured; the load-time half is proved to run the
// identical layout against the shared builders and the same conversion rules,
// and the render-time half is proved to reproduce their output byte for byte.
//
// Only `.raw` (the author's original source values) and `.data` (the authored
// section tree as data, each entry carrying its own `.raw`/`.item`/`.data`) are
// compared for TEXT: a converted text field legitimately differs — the render
// half runs it through inline Markdown while the load-time example context
// carries the raw string. The typed-schema fields the two halves convert
// value-only — a nullable null, a coerce text field's string form and a
// list-of-record's per-item maps — do NOT differ, so they are compared directly
// as converted values (nullable-fields, record-item-type, text-coercion).

const (
	// agreementSource is the authored slide: it is written BOTH as the deck
	// slide (slides/1-agree.md) and as the slide template's own example.md, so
	// the load-time example execution and the render-time slide execution run
	// the same layout against the same source values. The single-quoted YAML
	// scalars keep the `**`/`*` Markdown markers visible in `.raw`, distinct
	// from the converted values.
	agreementSource = "---\n" +
		"template: agreehost\n" +
		"title: '**Host** title'\n" +
		// The typed-schema probe fields: an explicit null on a nullable field, a
		// YAML number on a coerce text field, and a list whose item is an inline
		// record (its `id` coerced, its `note` omitted). Both halves convert
		// these value-only, so the converted values must agree byte for byte.
		"nickname: null\n" +
		"code: 7\n" +
		"people:\n" +
		"  - name: Ada\n" +
		"    id: 7\n" +
		"  - name: Grace\n" +
		"    id: 42\n" +
		"---\n" +
		"Host *body* source.\n" +
		"# blocks\n" +
		"```\n" +
		"template: agreeblock\n" +
		"text: '**First** block'\n" +
		"```\n" +
		"First block body source.\n" +
		"## leaves\n" +
		"```\n" +
		"template: agreeleaf\n" +
		"name: '**Ada**'\n" +
		"```\n" +
		"## leaves\n" +
		"```\n" +
		"template: agreeleaf\n" +
		"name: Grace\n" +
		"```\n" +
		"# blocks\n" +
		"```\n" +
		"template: agreeblock\n" +
		"text: Second block\n" +
		"```\n"

	agreementHostTemplateYAML = `description: agreement host slide reading the reserved raw/data context and the nullable/record/coerce field values unguarded
fields:
  - name: title
    type: text
    required: true
  - name: subtitle
    type: text
    default: Defaulted
  - name: summary
    type: text
  - name: nickname
    type: text
    nullable: true
  - name: code
    type: text
    coerce: true
  - name: people
    type: list
    item:
      type: record
      fields:
        - name: name
          type: text
        - name: id
          type: text
          coerce: true
        - name: note
          type: text
sections:
  - name: blocks
    accepted:
      - agreeblock
    max: 4
body:
  mode: optional
`

	// agreementHostLayout prints ONLY the reserved source views — its own `.raw`
	// (including its `.raw` key listing, so an explicit null stays a PRESENT
	// entry while an omitted optional field is ABSENT) and its authored children
	// as `.data` (each entry's `.item.depth`, the shared InstanceDepth value at
	// every level) — plus the typed-schema field values BOTH halves convert
	// identically (a nullable null, a coerced string and a per-item record map).
	// It never prints a converted text field value through any other name: the
	// render half pre-renders text through inline Markdown while the load half
	// keeps the authored string, so only `.raw` carries text.
	// The `len` reads make the coercion observable: a coerce text field that was
	// left a YAML number would fail `len` on both halves, and a record item that
	// was left an unconverted map would fail `len .id` likewise — so neither
	// half can silently skip the conversion and still execute.
	agreementHostLayout = `<div class="agree-host">` +
		`<span class="host-raw-title">{{ .raw.title }}</span>` +
		`<span class="host-raw-body">{{ .raw.body }}</span>` +
		`<span class="host-raw-keys">{{ range $k, $v := .raw }}{{ $k }};{{ end }}</span>` +
		`<span class="host-field-nickname">{{ if .nickname }}present{{ else }}nil{{ end }}</span>` +
		`<span class="host-field-summary">{{ if .summary }}present{{ else }}nil{{ end }}</span>` +
		`<span class="host-field-code">{{ .code }}</span>` +
		`<span class="host-field-code-len">{{ len .code }}</span>` +
		`<span class="host-field-people-count">{{ len .people }}</span>` +
		`{{ range .people }}` +
		`<span class="host-field-person" data-name="{{ .name }}" data-id="{{ .id }}" data-id-len="{{ len .id }}" data-note="{{ if .note }}present{{ else }}nil{{ end }}"></span>` +
		`{{ end }}` +
		`{{ range .data.blocks }}` +
		`<span class="host-block" data-index="{{ .item.index }}" data-depth="{{ .item.depth }}" data-number="{{ .item.number }}" data-count="{{ .item.count }}" data-first="{{ .item.first }}" data-last="{{ .item.last }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}" data-parent="{{ if .item.parent }}present{{ else }}absent{{ end }}">` +
		`<span class="host-block-raw-text">{{ .raw.text }}</span>` +
		`<span class="host-block-raw-body">{{ .raw.body }}</span>` +
		`<span class="host-block-raw-keys">{{ range $k, $v := .raw }}{{ $k }};{{ end }}</span>` +
		`{{ range .data.leaves }}` +
		`<span class="host-leaf" data-index="{{ .item.index }}" data-depth="{{ .item.depth }}" data-count="{{ .item.count }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}" data-parent-text="{{ .item.parent.text }}">` +
		`<span class="host-leaf-raw-name">{{ .raw.name }}</span>` +
		`<span class="host-leaf-raw-keys">{{ range $k, $v := .raw }}{{ $k }};{{ end }}</span>` +
		`</span>` +
		`{{ end }}` +
		`</span>` +
		`{{ end }}` +
		`</div>`

	agreementBlockTemplateYAML = `description: agreement block section carrying a nested leaves child section
fields:
  - name: text
    type: text
    required: true
sections:
  - name: leaves
    accepted:
      - agreeleaf
    max: 4
body:
  mode: optional
`

	// agreementBlockLayout prints ONLY the reserved `.item`/`.raw` views (never a
	// converted field value), so the block's load-time-equivalent execution and
	// its render-time execution produce identical bytes: the block's executed
	// `.item.depth` (0 — no enclosing same-template ancestor) and its `.raw` key
	// listing (`body;text;`, an authored body present, the `template` selector
	// excluded).
	agreementBlockLayout = `<div class="agree-block" data-depth="{{ .item.depth }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}">` +
		`<span class="agree-block-raw-text">{{ .raw.text }}</span>` +
		`<span class="agree-block-raw-keys">{{ range $k, $v := .raw }}{{ $k }};{{ end }}</span>` +
		`</div>`

	agreementBlockExample = "```\n" +
		"text: Sample block\n" +
		"```\n" +
		"Sample block body.\n" +
		"# leaves\n" +
		"```\n" +
		"template: agreeleaf\n" +
		"name: Sample leaf\n" +
		"```\n"

	agreementLeafTemplateYAML = `description: agreement leaf section reading the reserved raw context unguarded
fields:
  - name: name
    type: text
    required: true
body:
  mode: disallowed
`

	// agreementLeafLayout prints ONLY the reserved `.item`/`.raw` views, so the
	// leaf's load-time-equivalent execution and its render-time execution match
	// byte for byte: the leaf's executed `.item.depth` (0) and its `.raw` key
	// listing (`name;`, a body the example does not supply is absent).
	agreementLeafLayout = `<span class="agree-leaf" data-depth="{{ .item.depth }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}"><span class="leaf-raw-name">{{ .raw.name }}</span><span class="leaf-raw-keys">{{ range $k, $v := .raw }}{{ $k }};{{ end }}</span></span>`

	agreementLeafExample = "```\n" +
		"name: Sample leaf\n" +
		"```\n"

	// The self-nesting probe: a slide template declaring a `loop` section
	// accepting the `agreerepeatloop` section template, which declares the SAME
	// `loop` section accepting `agreerepeatloop` — a self-accepting (cyclic)
	// declaration. The chain is built directly in the test (Register, no
	// Validate) so the loader still rejects the cycle until phase 7 adds
	// max_depth, while the render pipeline (which never validates) genuinely
	// renders it. The slide layout reads the top-level `.data.loop` entry's
	// `item.depth`, and the loop layout reads its own executed `item.depth` and
	// its own `.data.loop` entry's `item.depth`, so the three self-nested levels
	// read 0/1/2 from BOTH the executed descriptor and the shared `.data` view
	// (item-context, bounded-self-nesting).
	agreeRepeatSlideLayout = `<section class="agree-repeat-slide">` +
		`{{ range .data.loop }}<span class="agree-repeat-data" data-depth="{{ .item.depth }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}"><span class="agree-repeat-data-name">{{ .raw.name }}</span></span>{{ end }}` +
		`{{ range .loop }}{{ . }}{{ end }}` +
		`</section>`

	agreeRepeatLoopLayout = `<div class="agree-repeat-loop" data-depth="{{ .item.depth }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}">` +
		`<span class="agree-repeat-name">{{ .raw.name }}</span>` +
		`{{ range .data.loop }}<span class="agree-repeat-data" data-depth="{{ .item.depth }}" data-section="{{ .item.section }}" data-template="{{ .item.template }}"><span class="agree-repeat-data-name">{{ .raw.name }}</span></span>{{ end }}` +
		`{{ range .loop }}{{ . }}{{ end }}` +
		`</div>`
)

// TestLoadAndRenderRawDataContextAgree asserts that load-time and render-time
// `.raw`/`.data` AGREE for the same authored content, because the shared
// template.RawContext/template.DataContext builders feed both halves, and that
// the CONVERTED typed-schema field values agree too — a nullable null, a
// list-of-record's per-item maps and a coerce text field's string form — since
// validateExampleBlockAt and renderer.values/fieldValue convert them
// value-only (nullable-fields, record-item-type, text-coercion). It is an
// integration test: a real on-disk library and deck are loaded by the real
// loader (LoadLibrary, which runs checkLibraryExample's unguarded example
// execution) and rendered by the real pipeline (RenderDeck). It reads the real
// filesystem, so it is skipped under -short.
//
// It pins (raw-source-context, section-data-context, item-context,
// template-context, nullable-fields, record-item-type, text-coercion): the
// slide's `.raw.title`/`.raw.body` are the author's source, not the converted
// text values; the slide's `.raw` key listing keeps an explicitly null declared
// field (`nickname:`) as a PRESENT entry while an omitted optional field
// (`summary`) is ABSENT (raw-source-context); `.data.blocks` carries one entry
// per authored instance, each with its own `.raw`/`.item` (whose `depth` is the
// shared InstanceDepth value at every level), and a nested instance's children
// are reached through its entry's `.data` with `.item.parent` the enclosing
// source values; the nullable `nickname` reads nil, the coerce `code` reads "7"
// and the `people` list reads as one map per item with its coerce `id` a string
// and its omitted optional `note` nil; and the render-time page reproduces the
// load-time-equivalent fragment byte for byte — the slide fragment, and each
// executed block/leaf fragment (its `.item.depth` and `.raw` key listing built
// by the shared builders) — so the two halves cannot drift.
//
// The loadable fixture composes four DISTINCT templates (agreehost →
// agreeblock → agreeleaf), so every level's `item.depth` is 0. A level whose
// resolved template REPEATS needs a self-nesting declaration, which is a
// composition cycle template.LoadLibrary/Registry.Validate reject until phase 7
// adds max_depth; the subtest below pins the repeated case — the renderer's
// executed `.item.depth` beside the shared `template.DataContext` `.data` view —
// on a chain built directly with Register (no Validate), compared against the
// same load-time builder (`template.DataContext` over the test's neutral-node
// adapter, the render-package counterpart of internal/template's unexported
// exampleContextNodes).
func TestLoadAndRenderRawDataContextAgree(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real deck directory from disk")
	}

	dir := writeAgreementDeck(t)
	fsys := os.DirFS(dir)

	// LOAD-TIME half: LoadLibrary runs checkLibraryExample (step 7), which
	// builds the reserved `.raw`/`.data` context with the shared builders and
	// executes each example layout. The agreehost example layout reads `.raw`
	// and `.data` UNGUARDED, so a successful load is itself the regression
	// assertion that the load-time half supplied the reserved context.
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v (the unguarded .raw/.data example layout must load)", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("template.Registry.Validate: %v", err)
	}

	// The deck slide and the template's own example.md are the SAME authored
	// content, so both halves run the same layout against the same source.
	slideBytes, err := fs.ReadFile(fsys, "slides/1-agree.md")
	if err != nil {
		t.Fatalf("read deck slide: %v", err)
	}
	host, _, ok := lib.TemplateByName("agreehost")
	if !ok || host == nil {
		t.Fatal(`TemplateByName("agreehost") not loaded`)
	}
	if !bytes.Equal(slideBytes, host.ExampleBytes) {
		t.Fatalf("deck slide and agreehost example.md must be the same authored content:\nslide:\n%s\nexample:\n%s", slideBytes, host.ExampleBytes)
	}

	// Rebuild the LOAD-TIME context for that content with the SAME shared
	// builders checkLibraryExample uses. The example bytes are slide-shaped, so
	// parse them and adapt the authored tree into the neutral
	// template.ContextNode tree the shared template.DataContext walks.
	exampleSlide, err := slide.Parse(host.ExamplePath, host.ExampleBytes, reg)
	if err != nil {
		t.Fatalf("slide.Parse(agreehost example): %v", err)
	}
	hostDef, _ := reg.Lookup("agreehost")
	loadRaw := template.RawContext(exampleSlide.Frontmatter, hostDef, exampleSlide.Body)
	loadData := template.DataContext(agreementNodes(reg, exampleSlide.Sections), nil, nil)

	// The CONVERTED field values the load-time example context carries, built
	// here as the load-time EQUIVALENT: validateExampleBlockAt is unexported and
	// its value-only nullable/record/coerce conversion (mirroring
	// renderer.values/fieldValue) produces, for the probe fields above, an
	// explicit null on a nullable field as a nil entry, a list-of-record as one
	// map per item with every declared field present (its coerce `id` as its
	// string form and its omitted optional `note` as nil), and the top-level
	// coerce `code` as its string form. Text fields are NOT reproduced here: the
	// render half pre-renders text through inline Markdown while the load-time
	// half keeps the authored string, so text is only ever compared through the
	// reserved `.raw` view.
	loadFields := map[string]any{
		"nickname": nil,
		// summary is declared but omitted: neither half supplies a source
		// value, so both present it as a nil entry — the ABSENT counterpart of
		// the explicit-null `nickname`, whose `.raw` presence below distinguishes
		// the two (raw-source-context).
		"summary": nil,
		"code":    "7",
		"people": []any{
			map[string]any{"name": "Ada", "id": "7", "note": nil},
			map[string]any{"name": "Grace", "id": "42", "note": nil},
		},
	}

	funcMap := template.LayoutFuncMap(lib.Media, "/assets/templates/media")
	loadCtx := map[string]any{
		"raw":  loadRaw,
		"data": loadData,
	}
	for k, v := range loadFields {
		loadCtx[k] = v
	}
	loadHostHTML := executeStandalone(t, "agreehost", host.LayoutText, funcMap, loadCtx)

	// The concrete load-time values the comparison pins: the source view of the
	// slide and of both authored `blocks` instances (with the first block's two
	// nested `leaves`). If LoadLibrary's step-7 context or the shared builders
	// changed, this fragment changes.
	for _, want := range []string{
		`<span class="host-raw-title">**Host** title</span>`,
		`<span class="host-raw-body">Host *body* source.`,
		// The `.raw` key listing: the explicitly null `nickname` is a PRESENT
		// entry while the omitted optional `summary` is ABSENT. The explicit null
		// and the absent optional are indistinguishable as converted values (both
		// nil) but distinct here (raw-source-context); `subtitle` (declared with a
		// default the author did not write) is likewise absent, as is the reserved
		// `template` selector and any undeclared key.
		`<span class="host-raw-keys">body;code;nickname;people;title;</span>`,
		// The converted typed-schema values, identical on both halves: the
		// nullable null reads as nil, the omitted optional `summary` nil, the
		// coerce number as its string form (whose `len` would fail if it were
		// still a YAML number), and the list-of-record as one per-item map (its
		// coerce `id` a string, its omitted optional `note` nil).
		`<span class="host-field-nickname">nil</span>`,
		`<span class="host-field-summary">nil</span>`,
		`<span class="host-field-code">7</span>`,
		`<span class="host-field-code-len">1</span>`,
		`<span class="host-field-people-count">2</span>`,
		`<span class="host-field-person" data-name="Ada" data-id="7" data-id-len="1" data-note="nil"></span>`,
		`<span class="host-field-person" data-name="Grace" data-id="42" data-id-len="2" data-note="nil"></span>`,
		// The `.data` view's per-level item depth: both blocks are the same
		// template at the top level (no enclosing same-template ancestor, depth
		// 0), and their nested leaves resolve the distinct agreeleaf (depth 0).
		`data-index="0" data-depth="0" data-number="1" data-count="2" data-first="true" data-last="false" data-section="blocks" data-template="agreeblock" data-parent="absent"`,
		`data-index="1" data-depth="0" data-number="2" data-count="2" data-first="false" data-last="true" data-section="blocks" data-template="agreeblock" data-parent="absent"`,
		`<span class="host-block-raw-text">**First** block</span>`,
		`<span class="host-block-raw-body">First block body source.`,
		`<span class="host-block-raw-text">Second block</span>`,
		`<span class="host-block-raw-keys">body;text;</span>`,
		`data-index="0" data-depth="0" data-count="2" data-section="leaves" data-template="agreeleaf" data-parent-text="**First** block"`,
		`data-index="1" data-depth="0" data-count="2" data-section="leaves" data-template="agreeleaf" data-parent-text="**First** block"`,
		`<span class="host-leaf-raw-name">**Ada**</span>`,
		`<span class="host-leaf-raw-name">Grace</span>`,
		`<span class="host-leaf-raw-keys">name;</span>`,
	} {
		if !strings.Contains(loadHostHTML, want) {
			t.Errorf("load-time-equivalent slide fragment does not contain %q:\n%s", want, loadHostHTML)
		}
	}

	// RENDER-TIME half: the full library→parser→render pipeline on the same
	// authored content.
	themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}
	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themeReg)
	if err != nil {
		t.Fatalf("deck.LoadConfig: %v", err)
	}
	d, err := deck.LoadSlides(fsys, deck.SlidesDir)
	if err != nil {
		t.Fatalf("deck.LoadSlides: %v", err)
	}
	parsed := parsePipelineSlides(t, fsys, d, reg)
	pageHTML, err := RenderDeck(cfg, d, parsed, reg, themeReg, funcMap)
	if err != nil {
		t.Fatalf("RenderDeck: %v", err)
	}
	page := string(pageHTML)

	// AGREE: the render-time page embeds the load-time-context execution
	// byte for byte. A divergent `.raw`/`.data`/`.item` on either side — a
	// different source view, a different `.item.depth`, a child tree keyed or
	// parented differently — would break this containment. This covers the
	// slide's own `.raw` key listing (explicit null vs absent) and each applied
	// `.data` entry's `.item.depth`.
	if !strings.Contains(page, loadHostHTML) {
		t.Errorf("render-time page does not embed the load-time-equivalent .raw/.data fragment:\nwant (load-time context execution):\n%s\npage:\n%s", loadHostHTML, page)
	}

	// The block and leaf layouts themselves are exercised UNGUARDED by
	// LoadLibrary's step-7 example execution above (the agreeblock/agreeleaf
	// examples run their own layouts against the section-instance `.item`/`.raw`
	// the loader builds), so a successful load already proves those layouts read
	// the reserved context at load time; the render-time sibling of each is
	// pinned by the self-nested subtest below (an executed section layout whose
	// own `.item.depth` and `.raw` key listing must match the shared builder).

	// A level whose resolved template REPEATS needs a self-nesting declaration,
	// which is a composition cycle that LoadLibrary/Registry.Validate reject
	// until phase 7 adds max_depth. The render pipeline never validates, so a
	// self-nesting chain built directly here (Register, no Validate) genuinely
	// renders and pins the +1-per-repeat rule at every level, compared against
	// the same load-time builder (`template.DataContext` over the test's
	// neutral-node adapter) the load-time example execution walks.
	t.Run("a self-nested chain agrees on per-level item.depth", func(t *testing.T) {
		repeatReg := template.NewRegistry(nil)
		for _, tmpl := range []*template.Template{
			{
				Name:     "agreerepeat",
				Usage:    template.UsageSlide,
				Sections: []template.SectionDecl{{Name: "loop", Accepted: []string{"agreerepeatloop"}, Max: 4}},
				Body:     template.BodyRule{Mode: template.BodyOptional},
				Layout:   template.Layout{Text: agreeRepeatSlideLayout},
			},
			{
				Name:     "agreerepeatloop",
				Usage:    template.UsageSection,
				Fields:   []template.Field{{Name: "name", Type: template.FieldText, Required: true}},
				Sections: []template.SectionDecl{{Name: "loop", Accepted: []string{"agreerepeatloop"}, Max: 4}},
				Body:     template.BodyRule{Mode: template.BodyDisallowed},
				Layout:   template.Layout{Text: agreeRepeatLoopLayout},
			},
		} {
			if err := repeatReg.Register(tmpl); err != nil {
				t.Fatalf("register template %q: %v", tmpl.Name, err)
			}
		}

		// One `loop` instance nested three levels deep, every level resolving
		// the SAME template (agreerepeatloop) through the self-accepting `loop`
		// declaration, so the enclosed chain each level's depth is measured
		// against repeats at each step.
		inner := slide.Section{Name: "loop", Level: 3, Index: 0, Template: "agreerepeatloop", Frontmatter: map[string]any{"name": "inner"}}
		mid := slide.Section{Name: "loop", Level: 2, Index: 0, Template: "agreerepeatloop", Frontmatter: map[string]any{"name": "mid"}, Children: []slide.Section{inner}}
		outer := slide.Section{Name: "loop", Level: 1, Index: 0, Template: "agreerepeatloop", Frontmatter: map[string]any{"name": "outer"}, Children: []slide.Section{mid}}
		parsed := &slide.Slide{
			File:        "slides/1-agree-repeat.md",
			Template:    "agreerepeat",
			Frontmatter: map[string]any{},
			Sections:    []slide.Section{outer},
		}

		rendered, err := RenderSlide(parsed, "agree-repeat", &deck.Config{Title: "Agree Repeat Deck"}, deck.Slide{Number: 1, Label: "agree-repeat"}, 1, repeatReg, template.LayoutFuncMap(nil, ""))
		if err != nil {
			t.Fatalf("RenderSlide: %v", err)
		}
		repeatPage := string(rendered)

		// The renderer's EXECUTED item descriptors: the slide's top-level
		// instance is depth 0, the inner `loop` nested in it depth 1 and the
		// innermost depth 2.
		for _, want := range []string{
			`<div class="agree-repeat-loop" data-depth="0" data-section="loop" data-template="agreerepeatloop">`,
			`<div class="agree-repeat-loop" data-depth="1" data-section="loop" data-template="agreerepeatloop">`,
			`<div class="agree-repeat-loop" data-depth="2" data-section="loop" data-template="agreerepeatloop">`,
		} {
			if !strings.Contains(repeatPage, want) {
				t.Errorf("self-nested render page does not contain the executed descriptor %q:\n%s", want, repeatPage)
			}
		}
		// The renderer's own `.data` view carries the same per-level depth (the
		// slide's `.data.loop` is 0, the outer loop's `.data.loop` is 1, the mid
		// loop's `.data.loop` is 2).
		for _, want := range []string{
			`<span class="agree-repeat-data" data-depth="0" data-section="loop" data-template="agreerepeatloop">`,
			`<span class="agree-repeat-data" data-depth="1" data-section="loop" data-template="agreerepeatloop">`,
			`<span class="agree-repeat-data" data-depth="2" data-section="loop" data-template="agreerepeatloop">`,
		} {
			if !strings.Contains(repeatPage, want) {
				t.Errorf("self-nested render page does not contain the data entry %q:\n%s", want, repeatPage)
			}
		}

		// The load-time builder walks the same authored tree with the same
		// ancestors and reads the same 0/1/2 per-level depth, so the two halves
		// cannot drift.
		loadLoop := template.DataContext(agreementNodes(repeatReg, parsed.Sections), nil, nil)
		if got, want := agreeLoopChainDepths(t, loadLoop), []int{0, 1, 2}; !reflect.DeepEqual(got, want) {
			t.Errorf("shared template.DataContext per-level item.depth = %v, want %v", got, want)
		}
	})
}

// agreeEntryList asserts v is the []map[string]any a `template.DataContext` key
// maps to and returns it; a nil or absent key yields nil, so an entry with no
// children is not an error.
func agreeEntryList(t *testing.T, v any, what string) []map[string]any {
	t.Helper()
	if v == nil {
		return nil
	}
	list, ok := v.([]map[string]any)
	if !ok {
		t.Fatalf("%s = %#v (%T), want []map[string]any", what, v, v)
	}
	return list
}

// agreeLoopChainDepths walks the `loop` chain of a `template.DataContext` view
// (each level's first entry, then that entry's own `.data`), returning the
// per-level `.item.depth` in order.
func agreeLoopChainDepths(t *testing.T, data map[string]any) []int {
	t.Helper()
	var depths []int
	for {
		entries := agreeEntryList(t, data["loop"], `data["loop"]`)
		if len(entries) == 0 {
			return depths
		}
		depth, ok := entries[0]["item"].(map[string]any)["depth"].(int)
		if !ok {
			t.Fatalf(`data["loop"][0].item.depth = %#v, want int`, entries[0]["item"])
		}
		depths = append(depths, depth)
		next, ok := entries[0]["data"].(map[string]any)
		if !ok {
			return depths
		}
		data = next
	}
}

// agreementNodes adapts a parsed slide/example section tree into the neutral
// template.ContextNode tree the shared template.DataContext walks, exactly as
// the load-time exampleContextNodes and render-time renderer.contextNodes
// adapters do: the declared name, the sibling index, the resolved template
// (looked up in the registry, or a name-only stand-in when unresolvable), the
// verbatim source frontmatter and body, and the recursed children. It is the
// test's own "equivalent input" on the load-time side, so it does not depend on
// either production adapter.
func agreementNodes(reg *template.Registry, sections []slide.Section) []template.ContextNode {
	if len(sections) == 0 {
		return nil
	}
	nodes := make([]template.ContextNode, len(sections))
	for i := range sections {
		sec := &sections[i]
		tmpl, _ := reg.Lookup(sec.Template)
		if tmpl == nil && sec.Template != "" {
			tmpl = &template.Template{Name: sec.Template}
		}
		nodes[i] = template.ContextNode{
			Name:     sec.Name,
			Index:    sec.Index,
			Template: tmpl,
			Source:   sec.Frontmatter,
			Body:     sec.Body,
			Children: agreementNodes(reg, sec.Children),
		}
	}
	return nodes
}

// executeStandalone parses and executes one layout source with funcMap and ctx,
// returning the HTML the layout emits. It executes the exact layout text the
// render-time pipeline parses, so the result is comparable byte for byte.
func executeStandalone(t *testing.T, name, layoutText string, funcMap htmltmpl.FuncMap, ctx map[string]any) string {
	t.Helper()
	layout, err := htmltmpl.New(name).Funcs(funcMap).Parse(layoutText)
	if err != nil {
		t.Fatalf("parse %s layout: %v", name, err)
	}
	var buf strings.Builder
	if err := layout.Execute(&buf, ctx); err != nil {
		t.Fatalf("execute %s layout: %v", name, err)
	}
	return buf.String()
}

// writeAgreementDeck writes a fresh temporary deck directory: kalide.yaml, the
// authored slide under slides/, a copy of the fixture library's templates/ tree
// for the plain theme and media, and the agreement slide/section templates.
// The agreehost example.md is written with the SAME bytes as the deck slide.
func writeAgreementDeck(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for name, data := range map[string]string{
		"kalide.yaml":       "title: Agreement Deck\ntheme: plain\n",
		"slides/1-agree.md": agreementSource,
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	copyTemplatesDir(t, filepath.Join(fixtureLibraryDir, "templates"), filepath.Join(dir, "templates"))

	writeProbeTemplate(t, dir, "slides", "agreehost", agreementHostTemplateYAML, agreementHostLayout, agreementSource)
	writeProbeTemplate(t, dir, "sections", "agreeblock", agreementBlockTemplateYAML, agreementBlockLayout, agreementBlockExample)
	writeProbeTemplate(t, dir, "sections", "agreeleaf", agreementLeafTemplateYAML, agreementLeafLayout, agreementLeafExample)

	return dir
}
