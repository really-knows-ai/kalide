package template

import (
	htmltemplate "html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for
// section-helpers/plan.phase-07.task-2, the example-context half of the
// required+default rule (field-rules): an omitted required field that declares
// a Default is APPLIED at every composition depth, and the application is
// OBSERVABLE.
//
// LoadLibrary returns no output — it only reports success — and executing a
// missing map key is not itself an error, so a load-only assertion would pass
// even if validateExampleBlockAt (the load-time materialisation site,
// task-13) were never implemented. This test therefore repeats step 7's exact
// binding in-package, as sectionhelper_int_test.go does: it re-parses the
// host layout under LayoutFuncMap with its `section` entry overridden by the
// load-time exampleSectionHelper, and executes it against the SAME example
// context validateExampleBlock builds (the default-materialising step-7 path),
// so the rendered bytes are captured and asserted.
//
// The observable dependency is made explicit rather than implied by a value:
//
//   - TOP LEVEL: the host's omitted `heading` default is passed as the
//     required, NON-defaulted `target` field of a `{{ section "probe" … }}`
//     call. Without the default the argument is nil, so ResolveSectionCall's
//     CheckValues rejects a nil text field and the step-7 execution ERRORS. The
//     control subtest deletes the materialised default and asserts that error.
//   - NESTED: an authored `probe` instance omits its own required, defaulted
//     `label`; the host layout reads the nested instance's materialised value
//     from the example context. The control deletes the nested default and
//     asserts the rendered value changes.
//
// It also pins the source-view rule in the example context: `.raw` EXCLUDES
// the default (a declared default is not an authored source value), and that an
// undefaulted omitted required field still fails LoadLibrary's example.md check.
//
// The unit tier (fieldrules_required_default_test.go) pins CheckValues'
// schema-only half and ResolveSectionCall's materialisation; the render pipeline
// half lives in internal/render/fieldrules_default_integration_test.go. These
// tests touch the real filesystem, so they are skipped under `go test -short`.
//
// typed-records/plan.phase-02.task-11 extends TestFieldRulesDefaultAppliedExampleInt
// with the field-types/field-rules deliverable at the load-time example site:
// the site applies every field type and rule through the shared CheckValues
// engine — surfacing exactly the engine's first error, positioned at the
// example.md frontmatter line and qualified by the instance's containment path
// — and is defaults-first, publishing a declared default (or a supplied value,
// which wins) into the context it hands the layout, identically to the
// slide-frontmatter and section-instance sites (internal/validate.checkFields,
// plan.phase-02.task-10).

const (
	// defaultProbeHostManifest is a slide template whose required `heading`
	// carries a Default and whose one child section accepts `probe`.
	defaultProbeHostManifest = `name: host
description: default-probe host with an omitted defaulted required field
fields:
  - name: heading
    type: text
    required: true
    default: HEAD-DEF
sections:
  - name: items
    accepted:
      - probe
    max: 4
body:
  mode: optional
`

	// defaultProbeHostLayout exercises the defaulted `heading` three ways: a
	// direct read, a `.raw` source-view probe (which must stay `absent`), and as
	// the required non-defaulted `target` of a `{{ section }}` call — the
	// error-on-nil dependency. It then reads each nested instance's materialised
	// `label` default.
	defaultProbeHostLayout = `<section class="host">` +
		`<h1 class="host-heading">{{ .heading }}</h1>` +
		`<span class="host-raw-heading">{{ if index .raw "heading" }}present{{ else }}absent{{ end }}</span>` +
		`<span class="host-helper">{{ section "probe" (dict "target" .heading) }}</span>` +
		`{{ range .items }}<span class="host-nested">{{ .label }}</span>{{ end }}` +
		`</section>`

	// defaultProbeHostExample omits the defaulted `heading` entirely and omits
	// the nested `probe` instance's defaulted `label`, while supplying probe's
	// required non-defaulted `target`.
	defaultProbeHostExample = "---\ntemplate: host\n---\n" +
		"Host body source.\n" +
		"# items\n" +
		"```\ntemplate: probe\ntarget: T1\n```\n"

	// defaultProbeSectionManifest declares a required `label` with a Default and
	// a required `target` with none.
	defaultProbeSectionManifest = `name: probe
description: default-probe section
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

	defaultProbeSectionLayout = `<probe data-label="{{ .label }}" data-target="{{ .target }}"></probe>`

	// defaultProbeSectionExample omits the defaulted `label`, so the section
	// template's own example also exercises the load-time materialisation.
	defaultProbeSectionExample = "```\ntarget: T1\n```\n"
)

// exampleFieldSitePath and exampleFieldSiteLine are the example.md position the
// load-time example site reports a field-schema violation at: the example's own
// on-disk path and the 1-based line its frontmatter starts on. The site
// (validateExampleBlockAt) positions a field error at the block's frontmatter
// line, so the constructed blocks below pin both. The path is under
// templates/slides, the owning layer of a slide template's example.
const (
	exampleFieldSitePath = TemplatesDir + "/slides/site/example.md"
	exampleFieldSiteLine = 12
)

// executeStep7ExampleInt repeats checkLibraryExample's (step 7) binding for one
// loaded template and returns the bytes it executes, which LoadLibrary itself
// discards. It builds the example context through the SAME production path
// validateExampleBlock (and so validateExampleBlockAt, the default-
// materialising site) uses, publishes the reserved `.raw`/`.data` context with
// the shared RawContext/DataContext builders exactly as checkLibraryExample
// does, then parses the layout under LayoutFuncMap with its `section` entry
// overridden by the load-time exampleSectionHelper — the same override step 7
// installs. mutate, when non-nil, runs after the context is built and before
// execution, so a caller can remove a materialised default to prove the
// execution depends on it.
func executeStep7ExampleInt(t *testing.T, lib *Library, name string, mutate func(ctx map[string]any)) (string, error) {
	t.Helper()
	lt, _, ok := lib.TemplateByName(name)
	if !ok || lt == nil || lt.Definition == nil {
		t.Fatalf("TemplateByName(%q) not loaded with a Definition", name)
	}
	def := lt.Definition
	resolve := func(n string) (*Template, bool) {
		other, _, ok := lib.TemplateByName(n)
		if !ok || other == nil || other.Definition == nil {
			return nil, false
		}
		return other.Definition, true
	}

	ctx := map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}
	block, err := parseExampleBlock(string(lt.ExampleBytes), lt.Kind, 1)
	if err != nil {
		t.Fatalf("parseExampleBlock(%s): %v", name, err)
	}
	if err := validateExampleBlock(lt.ExamplePath, def, block, resolve, nil, ctx); err != nil {
		t.Fatalf("validateExampleBlock(%s): %v", name, err)
	}

	// The reserved source contexts, exactly as checkLibraryExample publishes
	// them: `.raw` from the authored frontmatter minus the reserved template
	// selector, `.data` from the authored section tree.
	source := make(map[string]any, len(block.Frontmatter))
	for k, v := range block.Frontmatter {
		if k == "template" {
			continue
		}
		source[k] = v
	}
	ctx["raw"] = RawContext(source, def, block.Body)
	ctx["data"] = DataContext(exampleContextNodes(block.Sections, def, resolve), nil, nil)

	if mutate != nil {
		mutate(ctx)
	}

	funcMap := LayoutFuncMap(lib.Media, "/"+MediaDir)
	funcMap["section"] = exampleSectionHelper(lib, nil) // a slide layout's parent is the slide
	layout, err := htmltemplate.New(name).Funcs(funcMap).Parse(lt.LayoutText)
	if err != nil {
		t.Fatalf("parse %s layout under the step-7 section binding: %v", name, err)
	}
	var buf strings.Builder
	execErr := layout.Execute(&buf, ctx)
	return buf.String(), execErr
}

// TestFieldRulesDefaultAppliedExampleInt proves that an omitted required field
// declaring a Default is materialised into the step-7 example context at every
// composition depth, with an observable dependency, and that the example
// context's `.raw` view excludes the default (field-rules,
// raw-source-context).
func TestFieldRulesDefaultAppliedExampleInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	files := mergeIntFiles(
		templateIntFiles(SlidesDir, "host", defaultProbeHostManifest, defaultProbeHostLayout, defaultProbeHostExample),
		templateIntFiles(SectionsDir, "probe", defaultProbeSectionManifest, defaultProbeSectionLayout, defaultProbeSectionExample),
	)
	projectDir := writeSectionHelperIntLibrary(t, files)
	lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: an omitted required field with a default must load", err)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}

	got, execErr := executeStep7ExampleInt(t, lib, "host", nil)
	if execErr != nil {
		t.Fatalf("step-7 example execution error = %v, want nil: the default must be materialised before execution", execErr)
	}

	// The direct read and the required-target field both show the materialised
	// top-level default; the nested instance shows its own materialised default;
	// and `.raw` does NOT carry the top-level default.
	for _, want := range []string{
		`<h1 class="host-heading">HEAD-DEF</h1>`,
		`<probe data-label="PROBE-DEF" data-target="HEAD-DEF"></probe>`,
		`<span class="host-raw-heading">absent</span>`,
		`<span class="host-nested">PROBE-DEF</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("step-7 rendered host layout does not contain %q:\n%s", want, got)
		}
	}

	// The top-level dependency is real: removing the materialised `heading`
	// passes nil to the helper target's required, non-defaulted `target` field,
	// which CheckValues rejects, so the step-7 execution errors.
	t.Run("top-level execution depends on the materialised default", func(t *testing.T) {
		_, err := executeStep7ExampleInt(t, lib, "host", func(ctx map[string]any) {
			delete(ctx, "heading")
		})
		if err == nil {
			t.Fatal("step-7 execution without the materialised top-level default succeeded; want the required-target type error")
		}
		if !strings.Contains(err.Error(), "got null — this field isn't nullable") {
			t.Errorf("error = %v, want the non-nullable null rejection", err)
		}
	})

	// The nested dependency is real: removing the nested instance's materialised
	// `label` changes what the host layout renders for it.
	t.Run("nested execution depends on the materialised default", func(t *testing.T) {
		var nested int
		got, err := executeStep7ExampleInt(t, lib, "host", func(ctx map[string]any) {
			items, ok := ctx["items"].([]map[string]any)
			if !ok || len(items) == 0 {
				t.Fatalf("example context items = %#v, want the materialised nested instance", ctx["items"])
			}
			nested = len(items)
			delete(items[0], "label")
		})
		if err != nil {
			t.Fatalf("step-7 execution error = %v, want nil", err)
		}
		if nested == 0 {
			t.Fatal("no nested instance was materialised into the example context")
		}
		if strings.Contains(got, `<span class="host-nested">PROBE-DEF</span>`) {
			t.Errorf("nested instance rendered its default label after that default was removed:\n%s", got)
		}
	})

	// --- every field type and rule through the shared engine ---------------
	//
	// The load-time example site (validateExampleBlockAt, which checkLibraryExample
	// runs for every template's example.md and which every nested instance recurses
	// through) is a thin adapter over the shared template.CheckValues engine — the
	// same engine the slide-frontmatter and section-instance sites
	// (internal/validate.checkFields) call — so its field-schema coverage is the
	// engine's whole schema: every field type and every value rule, reported at the
	// example.md position with the engine's path-qualified what. Each case below
	// drives validateExampleBlock (the site) directly; the accepted cases
	// additionally prove the site is defaults-first, publishing a declared default
	// — or a supplied value, which wins over it — into the context it hands the
	// layout.
	//
	// The image type is checked for its assets/ prefix but not for existence here:
	// checkLibraryExample calls CheckValues with no image-existence callback,
	// because a library loads before any deck exists, so the example site has no
	// assets/ tree to resolve against; the deck-side existence rule belongs to the
	// slide/section instance site (internal/validate.checkFields with its fsys).
	t.Run("every field type and rule is enforced through the shared engine", func(t *testing.T) {
		zero := 0.0
		col := newSection("col", Field{Name: "title", Type: FieldText, Required: true})
		colResolve := resolverFor(col)

		textTmpl := newSlide("site-text", Field{Name: "text", Type: FieldText})
		numTmpl := newSlide("site-num", Field{Name: "num", Type: FieldNumber})
		numMinTmpl := newSlide("site-num-min", Field{Name: "num", Type: FieldNumber, Min: &zero})
		numMaxTmpl := newSlide("site-num-max", Field{Name: "num", Type: FieldNumber, Max: float64Ptr(10)})
		dateTmpl := newSlide("site-date", Field{Name: "date", Type: FieldDate})
		dateMinTmpl := newSlide("site-date-min", Field{Name: "date", Type: FieldDate, MinDate: "2020-01-01"})
		dateMaxTmpl := newSlide("site-date-max", Field{Name: "date", Type: FieldDate, MaxDate: "2025-12-31"})
		boolTmpl := newSlide("site-bool", Field{Name: "flag", Type: FieldBoolean})
		enumTmpl := newSlide("site-enum", Field{Name: "mode", Type: FieldEnum, Variants: []string{"a", "b"}})
		imageTmpl := newSlide("site-image", Field{Name: "img", Type: FieldImage})
		linkTmpl := newSlide("site-link", Field{Name: "url", Type: FieldLink})
		listTmpl := newSlide("site-list", Field{
			Name: "items", Type: FieldList, Item: &Field{Type: FieldText},
			MinItems: 1, MaxItems: 2,
		})
		reqTmpl := newSlide("site-req", Field{Name: "req", Type: FieldText, Required: true})
		longTmpl := newSlide("site-long", Field{Name: "long", Type: FieldText, MaxLength: 2})
		suggestTmpl := newSlide("site-suggest", Field{Name: "title", Type: FieldText})
		nullableTmpl := newSlide("site-nullable", Field{Name: "title", Type: FieldText, Nullable: true})
		coerceTmpl := newSlide("site-coerce", Field{Name: "coerced", Type: FieldText, Coerce: true})
		defTmpl := newSlide("site-def", Field{Name: "title", Type: FieldText, Required: true, Default: "Untitled"})
		sectionTmpl := newSlide("site-section", Field{
			Name: "block", Type: FieldSectionTemplate, SectionTemplate: "col",
		})
		recordTmpl := newSlide("site-record", Field{
			Name: "items", Type: FieldList,
			Item: &Field{Type: FieldRecord, Fields: []Field{
				{Name: "title", Type: FieldText, Required: true},
			}},
		})
		recordScalarTmpl := newSlide("site-record-scalar", Field{
			Name: "items", Type: FieldList,
			Item: &Field{Type: FieldRecord, Fields: []Field{
				{Name: "n", Type: FieldNumber, Min: &zero},
			}},
		})
		nestedRecordTmpl := newSlide("site-nested-record", Field{
			Name: "items", Type: FieldList,
			Item: &Field{Type: FieldRecord, Fields: []Field{
				{Name: "values", Type: FieldList, Item: &Field{
					Type: FieldRecord, Fields: []Field{
						{Name: "n", Type: FieldNumber, Min: &zero},
					},
				}},
			}},
		})

		cases := []struct {
			name        string
			def         *Template
			resolve     func(string) (*Template, bool)
			frontmatter map[string]any
			valid       bool
			want        string
			wantCtxKey  string
			wantCtx     any
		}{
			// --- every field type: reject a value of the wrong type -------
			{
				name: "text rejects a number", def: textTmpl,
				frontmatter: map[string]any{"text": 42},
				want:        `text: type: expected a text field, got a number`,
			},
			{
				name: "number rejects a boolean", def: numTmpl,
				frontmatter: map[string]any{"num": true},
				want:        `num: type: expected a number, got a boolean`,
			},
			{
				name: "number rejects a quoted value", def: numTmpl,
				frontmatter: map[string]any{"num": "42"},
				want:        `num: type: expected a number, got the quoted value "42"`,
			},
			{
				name: "date rejects a number", def: dateTmpl,
				frontmatter: map[string]any{"date": 1},
				want:        `date: type: expected a YYYY-MM-DD date, got a number`,
			},
			{
				name: "boolean rejects a quoted string", def: boolTmpl,
				frontmatter: map[string]any{"flag": "yes"},
				want:        `flag: type: expected a boolean, got a quoted string`,
			},
			{
				name: "enum rejects a number", def: enumTmpl,
				frontmatter: map[string]any{"mode": 3},
				want:        `mode: type: expected one of the enum variants, got a number`,
			},
			{
				name: "image rejects a number", def: imageTmpl,
				frontmatter: map[string]any{"img": 5},
				want:        `img: type: expected an image path, got a number`,
			},
			{
				name: "link rejects a number", def: linkTmpl,
				frontmatter: map[string]any{"url": 5},
				want:        `url: type: expected a link, got a number`,
			},
			{
				name: "list rejects a quoted string", def: listTmpl,
				frontmatter: map[string]any{"items": "notalist"},
				want:        `items: type: expected a list, got a quoted string`,
			},
			{
				name: "section-template rejects a quoted string", def: sectionTmpl, resolve: colResolve,
				frontmatter: map[string]any{"block": "notmap"},
				want:        `block: type: expected a mapping for section template "col", got a quoted string`,
			},
			{
				name: "coerce text still rejects a mapping", def: coerceTmpl,
				frontmatter: map[string]any{"coerced": map[string]any{"a": 1}},
				want:        `coerced: type: expected a text field, got a mapping`,
			},
			{
				name: "record item rejects a non-mapping", def: recordScalarTmpl,
				frontmatter: map[string]any{"items": []any{7}},
				want:        `items › [0]: type: expected a mapping for a record, got a number`,
			},

			// --- every field rule: reject a violating value ---------------
			{
				name: "required absent is rejected", def: reqTmpl,
				frontmatter: map[string]any{},
				want:        `req: required: field "req" is required but missing`,
			},
			{
				name: "max_length is enforced", def: longTmpl,
				frontmatter: map[string]any{"long": "abc"},
				want:        `long: max_length: text is 3 characters, maximum is 2`,
			},
			{
				name: "number min is enforced", def: numMinTmpl,
				frontmatter: map[string]any{"num": -1},
				want:        `num: min: -1 is below the minimum 0`,
			},
			{
				name: "number max is enforced", def: numMaxTmpl,
				frontmatter: map[string]any{"num": 11},
				want:        `num: max: 11 exceeds the maximum 10`,
			},
			{
				name: "date min is enforced", def: dateMinTmpl,
				frontmatter: map[string]any{"date": "2019-01-01"},
				want:        `date: min: 2019-01-01 is below the minimum 2020-01-01`,
			},
			{
				name: "date max is enforced", def: dateMaxTmpl,
				frontmatter: map[string]any{"date": "2026-01-01"},
				want:        `date: max: 2026-01-01 exceeds the maximum 2025-12-31`,
			},
			{
				name: "malformed date is rejected", def: dateTmpl,
				frontmatter: map[string]any{"date": "25/09/2026"},
				want:        `date: date: "25/09/2026" is not a YYYY-MM-DD date`,
			},
			{
				name: "enum membership is enforced", def: enumTmpl,
				frontmatter: map[string]any{"mode": "c"},
				want:        `mode: enum: "c" is not one of the allowed variants (a, b)`,
			},
			{
				name: "image must be under assets", def: imageTmpl,
				frontmatter: map[string]any{"img": "pic.png"},
				want:        `img: image: "pic.png" is not under assets/`,
			},
			{
				name: "link must be a label or an http URL", def: linkTmpl,
				frontmatter: map[string]any{"url": "mailto:x@example.com"},
				want:        `url: link: "mailto:x@example.com" is neither a #label anchor nor an http(s) URL`,
			},
			{
				name: "link label must be non-empty", def: linkTmpl,
				frontmatter: map[string]any{"url": "#"},
				want:        `url: link: "#" has an empty label`,
			},
			{
				name: "list min_items is enforced", def: listTmpl,
				frontmatter: map[string]any{"items": []any{}},
				want:        `items: min_items: 0 item(s) is below the minimum 1`,
			},
			{
				name: "list max_items is enforced", def: listTmpl,
				frontmatter: map[string]any{"items": []any{"a", "b", "c"}},
				want:        `items: max_items: 3 item(s) exceeds the maximum 2`,
			},
			{
				name: "unknown field is rejected", def: suggestTmpl,
				frontmatter: map[string]any{"titel": "x"},
				want:        `titel: unknown field "titel"`,
			},
			{
				name: "non-nullable null is rejected", def: suggestTmpl,
				frontmatter: map[string]any{"title": nil},
				want:        `title: got null`,
			},

			// --- path qualification through nesting -----------------------
			{
				name: "record item required field carries the full path", def: recordTmpl,
				frontmatter: map[string]any{"items": []any{map[string]any{}}},
				want:        `items › [0] › title: required: field "title" is required but missing`,
			},
			{
				name: "record-list-record violation carries the full path", def: nestedRecordTmpl,
				frontmatter: map[string]any{"items": []any{map[string]any{"values": []any{map[string]any{"n": -1}}}}},
				want:        `items › [0] › values › [0] › n: min: -1 is below the minimum 0`,
			},
			{
				name: "section-template field violation carries the nested path", def: sectionTmpl, resolve: colResolve,
				frontmatter: map[string]any{"block": map[string]any{}},
				want:        `block › title: required: field "title" is required but missing`,
			},

			// --- every field type accepts a valid value -------------------
			{name: "text accepts a string", def: textTmpl, frontmatter: map[string]any{"text": "hello"}, valid: true},
			{name: "number accepts a number", def: numMinTmpl, frontmatter: map[string]any{"num": 3}, valid: true},
			{name: "date accepts a date", def: dateTmpl, frontmatter: map[string]any{"date": "2026-09-25"}, valid: true},
			{name: "boolean accepts a boolean", def: boolTmpl, frontmatter: map[string]any{"flag": true}, valid: true},
			{name: "enum accepts a variant", def: enumTmpl, frontmatter: map[string]any{"mode": "a"}, valid: true},
			{
				// No image-existence callback is supplied at the example site, so
				// an assets/ path is accepted without the file existing; the
				// existence rule is the deck-side site's (see the comment above).
				name: "image accepts an assets path", def: imageTmpl,
				frontmatter: map[string]any{"img": "assets/pic.png"}, valid: true,
			},
			{name: "link accepts a #label", def: linkTmpl, frontmatter: map[string]any{"url": "#summary"}, valid: true},
			{name: "list accepts its items", def: listTmpl, frontmatter: map[string]any{"items": []any{"a"}}, valid: true},
			{
				name: "section-template accepts a mapping", def: sectionTmpl, resolve: colResolve,
				frontmatter: map[string]any{"block": map[string]any{"title": "x"}}, valid: true,
			},
			{
				name: "nullable accepts null", def: nullableTmpl,
				frontmatter: map[string]any{"title": nil}, valid: true,
				wantCtxKey: "title", wantCtx: nil,
			},

			// --- defaults-first: the site materialises the default -------
			{
				name: "coerce text accepts a number and converts it", def: coerceTmpl,
				frontmatter: map[string]any{"coerced": 42}, valid: true,
				wantCtxKey: "coerced", wantCtx: "42",
			},
			{
				name: "required with a default accepts omission", def: defTmpl,
				frontmatter: map[string]any{}, valid: true,
				wantCtxKey: "title", wantCtx: "Untitled",
			},
			{
				name: "a supplied value wins over the default", def: defTmpl,
				frontmatter: map[string]any{"title": "explicit"}, valid: true,
				wantCtxKey: "title", wantCtx: "explicit",
			},
			{
				name: "the reserved template selector is stripped", def: suggestTmpl,
				frontmatter: map[string]any{"template": "site-suggest", "title": "x"}, valid: true,
			},
		}

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				ctx := map[string]any{}
				err := validateExampleBlock(exampleFieldSitePath, tt.def,
					&exampleBlock{Frontmatter: tt.frontmatter, FrontmatterLine: exampleFieldSiteLine},
					tt.resolve, nil, ctx)

				if tt.valid {
					if err != nil {
						t.Fatalf("validateExampleBlock() error = %v, want nil: the value is valid for its type and rules", err)
					}
					if tt.wantCtxKey != "" {
						got, present := ctx[tt.wantCtxKey]
						if !present {
							t.Fatalf("example context has no %q entry; the site must publish every declared field", tt.wantCtxKey)
						}
						if got != tt.wantCtx {
							t.Errorf("example context[%q] = %#v, want %#v", tt.wantCtxKey, got, tt.wantCtx)
						}
					}
					return
				}

				libErr := asLibraryError(t, err)
				if libErr.Path != exampleFieldSitePath {
					t.Errorf("error Path = %q, want the example.md site %q", libErr.Path, exampleFieldSitePath)
				}
				if libErr.Line != exampleFieldSiteLine {
					t.Errorf("error Line = %d, want the frontmatter line %d", libErr.Line, exampleFieldSiteLine)
				}
				if libErr.Message != tt.want {
					t.Errorf("error Message =\n  %q\nwant\n  %q", libErr.Message, tt.want)
				}
			})
		}
	})

	// The site surfaces exactly the shared engine's FIRST error, adapted to the
	// example.md position — the same wiring internal/validate.checkFields has for
	// the slide-frontmatter and section-instance sites. This is what makes the
	// example site identical to those sites rather than a parallel checker.
	t.Run("the site surfaces the shared engine's first error", func(t *testing.T) {
		tmpl := newSlide("site-wire",
			Field{Name: "bad_text", Type: FieldText},
			Field{Name: "bad_num", Type: FieldNumber},
		)
		data := map[string]any{"bad_text": 42, "bad_num": true}

		res := CheckValues(data, tmpl, nil)
		if len(res.Errors) == 0 {
			t.Fatal("CheckValues() reported no error for the offending data")
		}

		err := validateExampleBlock(exampleFieldSitePath, tmpl,
			&exampleBlock{Frontmatter: data, FrontmatterLine: exampleFieldSiteLine}, nil, nil, map[string]any{})
		libErr := asLibraryError(t, err)
		want := res.Errors[0].PathString() + ": " + res.Errors[0].What
		if libErr.Message != want {
			t.Errorf("error Message = %q, want the shared engine's first error %q", libErr.Message, want)
		}
		if libErr.Path != exampleFieldSitePath || libErr.Line != exampleFieldSiteLine {
			t.Errorf("error position = %s:%d, want %s:%d", libErr.Path, libErr.Line, exampleFieldSitePath, exampleFieldSiteLine)
		}
	})

	// A nested section instance is validated through the same site function,
	// with the instance's containment path prefixed to the engine's error — so
	// every field type and rule above applies identically at any composition
	// depth, and a violation names both the instance and the field.
	t.Run("a nested instance is validated through the same engine with its containment path", func(t *testing.T) {
		col := newSection("col", Field{Name: "title", Type: FieldText, Required: true})
		container := newSection("container")
		container.Sections = []SectionDecl{{Name: "items", Accepted: []string{"col"}}}
		resolve := resolverFor(col)

		block := &exampleBlock{
			Frontmatter:     map[string]any{},
			FrontmatterLine: exampleFieldSiteLine,
			Sections: []exampleSection{{
				Name:  "items",
				Line:  exampleFieldSiteLine + 5,
				Index: 0,
				exampleBlock: &exampleBlock{
					// The nested instance omits col's required `title`.
					Frontmatter:     map[string]any{},
					FrontmatterLine: exampleFieldSiteLine + 6,
				},
			}},
		}

		err := validateExampleBlock(exampleFieldSitePath, container, block, resolve, nil, map[string]any{})
		libErr := asLibraryError(t, err)
		if libErr.Path != exampleFieldSitePath {
			t.Errorf("error Path = %q, want the example.md site %q", libErr.Path, exampleFieldSitePath)
		}
		if libErr.Line != exampleFieldSiteLine+6 {
			t.Errorf("error Line = %d, want the nested frontmatter line %d", libErr.Line, exampleFieldSiteLine+6)
		}
		want := `items[0]: title: required: field "title" is required but missing`
		if libErr.Message != want {
			t.Errorf("error Message =\n  %q\nwant\n  %q", libErr.Message, want)
		}
	})
}

// TestFieldRulesUndefaultedExampleInt proves the other half of the rule at the
// same example.md site: an omitted required field with NO Default still fails
// LoadLibrary's example check, positioned at the template's example.md
// (field-rules). It complements the defaulted case above: the default is what
// makes the omission legal.
func TestFieldRulesUndefaultedExampleInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	files := mergeIntFiles(
		templateIntFiles(SlidesDir, "strict",
			"name: strict\nfields:\n  - name: must\n    type: text\n    required: true\n",
			"<section>{{ .must }}</section>",
			"---\ntemplate: strict\n---\n"),
	)
	projectDir := writeSectionHelperIntLibrary(t, files)
	lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
	if err == nil {
		t.Fatalf("LoadLibrary() = %+v, nil; want the undefaulted omitted required field to fail the example check", lib)
	}
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
	}
	libErr := asLibraryError(t, err)
	if want := filepath.Join(TemplatesDir, SlidesDir, "strict", ExampleFile); libErr.Path != want {
		t.Errorf("Path = %q, want the example.md site %q", libErr.Path, want)
	}
	if !strings.Contains(libErr.Message, "required") || !strings.Contains(libErr.Message, "must") {
		t.Errorf("Message = %q, want a required error naming the field", libErr.Message)
	}
}
