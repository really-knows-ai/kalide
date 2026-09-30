package template

import (
	htmltemplate "html/template"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-03.task-18: the
// load-time example context (templates-dir-validation step 7,
// checkLibraryExample) published by exampleRawContext / exampleDataContext and
// the section template's one-item `.item`. It proves that an example layout
// reads the reserved `.raw`/`.data`/`.item` context UNGUARDED — no
// `{{ if .raw }}` / `{{ with .body }}` / `{{ if .item }}` wrapper — and still
// loads cleanly, which is exactly the CR's unguarded pass-through
// `{{ section "callout" (dict "label" "Proposition") .raw.body }}` plus
// `{{ .raw.title }}` and a `.data.<section>` read.
//
// It runs under -short: the libraries are in-memory fstest.MapFS fixtures, so
// no real filesystem is touched. It reaches the same checkLibraryExample step
// LoadLibrary runs, and repeats step 7's execution in-package (LoadLibrary
// discards the rendered bytes) to observe the values the reserved context
// carries, as the sectionhelpers integration test does for the helper binding.
//
// It covers (requirements.requirement raw-source-context,
// section-data-context, template-context, item-context):
//
//   - a slide whose layout passes `.raw.body` straight through to a helper call
//     with NO guard and reads `{{ .raw.title }}` and `.data.<section>` loads;
//   - a section template whose own example layout reads `.item` and the
//     reserved `.raw` unguarded loads, and its `.raw` excludes a field's
//     declared default (a default is not an authored source value);
//   - exampleRawContext directly: declared supplied fields are copied, a
//     declared default is excluded, an undeclared key is excluded, and `body`
//     is present only when the author supplied one;
//   - exampleDataContext directly: entries are keyed by declared section name
//     and source-ordered within a key, one entry per instance with per-name
//     sibling counts, each carrying its own `raw`/`item`/`data`, and the
//     recursion sets a child's `item.parent` to the enclosing instance's
//     source values.

// unguardedExampleLibraryFS is a minimal, otherwise-valid in-memory library
// whose layouts read the reserved load-time context with no guard:
//
//   - slides/main: `{{ .raw.title }}`, `.data.blocks` (count and range with
//     `.raw.text` / `.item.index`) and the CR's unguarded
//     `{{ section "callout" (dict "label" "Proposition") .raw.body }}`;
//   - sections/callout: `.raw.body` and the one-item `.item` fields, unguarded;
//   - sections/block: `.raw.text` and `.item.index`, unguarded.
//
// `subtitle` carries a default the main example omits, so `.raw` must exclude
// it; `callout`/`block` are reached from main and each is also validated on its
// own example.
func unguardedExampleLibraryFS() fstest.MapFS {
	return fstest.MapFS{
		"library.yaml": {Data: []byte("name: demo\nformat: 1\n")},

		"slides/main/template.yaml": {Data: []byte(
			"name: main\n" +
				"fields:\n" +
				"  - name: title\n    type: text\n    required: true\n" +
				"  - name: subtitle\n    type: text\n    default: Untitled\n" +
				"sections:\n" +
				"  - name: blocks\n    accepted: [block]\n" +
				"body:\n  mode: optional\n")},
		"slides/main/layout.html.tmpl": {Data: []byte(
			`<section class="main">` +
				`<span class="main-raw-title">{{ .raw.title }}</span>` +
				`<span class="main-data-count">{{ len .data.blocks }}</span>` +
				`{{ range .data.blocks }}<span class="main-block">{{ .raw.text }}#{{ .item.index }}</span>{{ end }}` +
				`<span class="main-raw-body">{{ section "callout" (dict "label" "Proposition") .raw.body }}</span>` +
				`</section>`)},
		"slides/main/example.md": {Data: []byte(
			"---\n" +
				"template: main\n" +
				"title: Main Title\n" +
				"---\n" +
				"Main source body.\n" +
				"\n" +
				"# blocks\n" +
				"```\n" +
				"text: First\n" +
				"```\n" +
				"\n" +
				"# blocks\n" +
				"```\n" +
				"text: Second\n" +
				"```\n")},

		"sections/callout/template.yaml": {Data: []byte(
			"name: callout\n" +
				"fields:\n" +
				"  - name: label\n    type: text\n    required: true\n" +
				"body:\n  mode: optional\n")},
		"sections/callout/layout.html.tmpl": {Data: []byte(
			`<span class="callout">` +
				`<span class="callout-label">{{ .label }}</span>` +
				`<span class="callout-raw-body">{{ .raw.body }}</span>` +
				`<span class="callout-item">{{ .item.index }}/{{ .item.number }}/{{ .item.count }}/{{ .item.first }}/{{ .item.last }}/{{ .item.section }}/{{ .item.template }}/{{ if .item.parent }}parent{{ else }}noparent{{ end }}</span>` +
				`</span>`)},
		"sections/callout/example.md": {Data: []byte(
			"```\n" +
				"label: Example label\n" +
				"```\n" +
				"Example callout body.\n")},

		"sections/block/template.yaml": {Data: []byte(
			"name: block\n" +
				"fields:\n" +
				"  - name: text\n    type: text\n    required: true\n" +
				"body:\n  mode: disallowed\n")},
		"sections/block/layout.html.tmpl": {Data: []byte(
			`<b class="block">{{ .raw.text }}{{ .item.index }}</b>`)},
		"sections/block/example.md": {Data: []byte(
			"```\n" +
				"text: Sample\n" +
				"```\n")},
	}
}

// TestLoadLibraryUnguardedExampleContext proves LoadLibrary's step-7 example
// execution supplies the reserved `.raw`/`.data`/`.item` context, so a library
// whose example layouts read it UNGUARDED loads cleanly. Before phase-03 tasks
// 14-17 the main layout's `{{ .raw.body }}` pass-through and `.data.blocks`
// read failed step 7 (the context carried no `raw`/`data`), so a successful
// load is the regression assertion.
func TestLoadLibraryUnguardedExampleContext(t *testing.T) {
	lib, err := LoadLibrary(rootedFS(unguardedExampleLibraryFS()), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: unguarded .raw/.data/.item example layouts must load", err)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}
	for _, name := range []string{"main", "callout", "block"} {
		lt, _, ok := lib.TemplateByName(name)
		if !ok || lt == nil || lt.Definition == nil {
			t.Fatalf("template %q not loaded with a Definition: ok=%v", name, ok)
		}
	}
}

// TestUnguardedExampleRawAndDataContextValues repeats step 7's execution for
// slides/main — LoadLibrary discards the rendered bytes — to assert the values
// the reserved context carries as authored: `.raw` holds the slide's supplied
// title and source body but NOT the defaulted, unauthored subtitle; `.data` is
// the authored child tree, one entry per `# blocks` instance in source order,
// each with its own `raw` and one-item `item`. The rendered main layout proves
// the unguarded calls execute: the `.raw.body` pass-through reaches the helper
// target, which renders the one-item `.item` descriptor.
func TestUnguardedExampleRawAndDataContextValues(t *testing.T) {
	lib, err := LoadLibrary(rootedFS(unguardedExampleLibraryFS()), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil", err)
	}
	main, _, ok := lib.TemplateByName("main")
	if !ok || main == nil || main.Definition == nil {
		t.Fatal(`TemplateByName("main") not loaded with a Definition`)
	}
	def := main.Definition
	resolve := exampleLibraryResolver(lib)

	block, err := parseExampleBlock(string(main.ExampleBytes), main.Kind, 1)
	if err != nil {
		t.Fatalf("parseExampleBlock(main) error = %v", err)
	}
	source := make(map[string]any, len(block.Frontmatter))
	for k, v := range block.Frontmatter {
		if k == "template" {
			continue
		}
		source[k] = v
	}

	raw := exampleRawContext(source, def, block.Body)
	if got := raw["title"]; got != "Main Title" {
		t.Errorf(`raw["title"] = %v, want the authored %q`, got, "Main Title")
	}
	if _, has := raw["subtitle"]; has {
		t.Errorf("raw = %v, want the defaulted, unauthored subtitle excluded", raw)
	}
	if got := raw["body"]; got != "Main source body." {
		t.Errorf(`raw["body"] = %q, want the example's source body %q`, got, "Main source body.")
	}

	data := exampleDataContext(block.Sections, def, resolve, nil)
	blocks := contextEntryList(t, data["blocks"], `data["blocks"]`)
	if len(blocks) != 2 {
		t.Fatalf(`len(data["blocks"]) = %d, want 2 authored instances`, len(blocks))
	}
	wantTexts := []string{"First", "Second"}
	for i, want := range wantTexts {
		entryRaw := contextMap(t, blocks[i]["raw"], "blocks entry raw")
		if got := entryRaw["text"]; got != want {
			t.Errorf("data[blocks][%d].raw.text = %v, want %q (source order)", i, got, want)
		}
		entryItem := contextMap(t, blocks[i]["item"], "blocks entry item")
		if got := entryItem["index"]; got != i {
			t.Errorf("data[blocks][%d].item.index = %v, want %d", i, got, i)
		}
		if got := entryItem["count"]; got != 2 {
			t.Errorf("data[blocks][%d].item.count = %v, want the 2 same-name siblings", i, got)
		}
	}

	// Execute slides/main under step 7's exact binding (LayoutFuncMap with its
	// `section` entry overridden by the load-time exampleSectionHelper, nil
	// caller fields from a slide layout) against the reserved context built
	// above, so the unguarded reads produce observable output.
	funcMap := LayoutFuncMap(lib.Media, "/"+MediaDir)
	funcMap["section"] = exampleSectionHelper(lib, nil)
	layout, err := htmltemplate.New("main").Funcs(funcMap).Parse(main.LayoutText)
	if err != nil {
		t.Fatalf("parse main layout under the step-7 section binding: %v", err)
	}
	var buf strings.Builder
	if err := layout.Execute(&buf, map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
		"raw":   raw,
		"data":  data,
	}); err != nil {
		t.Fatalf("execute main layout under the step-7 section binding: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		`<span class="main-raw-title">Main Title</span>`,
		`<span class="main-data-count">2</span>`,
		`<span class="main-block">First#0</span>`,
		`<span class="main-block">Second#1</span>`,
		// The unguarded `.raw.body` pass-through reached the helper target,
		// which saw the supplied label and the source body, and executed under
		// its one-item `.item` descriptor (parent nil from a slide layout).
		`<span class="callout-label">Proposition</span>`,
		`<span class="callout-raw-body">Main source body.</span>`,
		`<span class="callout-item">0/1/1/true/true/callout/callout/noparent</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("step-7 rendered main layout does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "no render-time implementation bound") {
		t.Errorf("step-7 rendered main layout contains the parse-only section stub's error:\n%s", got)
	}
}

// TestSectionExampleReservedContext proves a section template's own example
// (validated by checkLibraryExample's section loop) also gets the reserved
// context: a section layout that reads `.item` and `.raw` with no guard loads
// cleanly, and the `.raw` for its example excludes a field's declared default
// (the default is not an authored source value), while still carrying the
// example's source body.
func TestSectionExampleReservedContext(t *testing.T) {
	fsys := fstest.MapFS{
		"library.yaml": {Data: []byte("name: demo\nformat: 1\n")},

		// A schema-free slide so the library has a slide; it reads nothing.
		"slides/main/template.yaml":    {Data: []byte("name: main\n")},
		"slides/main/layout.html.tmpl": {Data: []byte("<section></section>")},
		"slides/main/example.md":       {Data: []byte("# main\n")},

		// `label` has a default the example omits, so it is not a source value.
		"sections/badge/template.yaml": {Data: []byte(
			"name: badge\n" +
				"fields:\n" +
				"  - name: label\n    type: text\n    default: Defaulted\n" +
				"body:\n  mode: optional\n")},
		"sections/badge/layout.html.tmpl": {Data: []byte(
			`<span class="badge">` +
				`<span class="badge-item">{{ .item.index }}/{{ .item.number }}/{{ .item.count }}/{{ .item.first }}/{{ .item.last }}/{{ .item.section }}/{{ .item.template }}/{{ if .item.parent }}parent{{ else }}noparent{{ end }}</span>` +
				`<span class="badge-raw-body">{{ .raw.body }}</span>` +
				`</span>`)},
		// An empty frontmatter fence: the example supplies no field, and a body.
		"sections/badge/example.md": {Data: []byte(
			"```\n" +
				"```\n" +
				"Badge example body.\n")},
	}

	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: a section example reading .item/.raw unguarded must load", err)
	}
	badge, _, ok := lib.TemplateByName("badge")
	if !ok || badge == nil || badge.Definition == nil {
		t.Fatal(`TemplateByName("badge") not loaded with a Definition`)
	}

	block, err := parseExampleBlock(string(badge.ExampleBytes), badge.Kind, 1)
	if err != nil {
		t.Fatalf("parseExampleBlock(badge) error = %v", err)
	}
	source := make(map[string]any, len(block.Frontmatter))
	for k, v := range block.Frontmatter {
		if k == "template" {
			continue
		}
		source[k] = v
	}
	raw := exampleRawContext(source, badge.Definition, block.Body)
	if _, has := raw["label"]; has {
		t.Errorf("raw = %v, want the defaulted, unauthored label excluded", raw)
	}
	if got := raw["body"]; got != "Badge example body." {
		t.Errorf(`raw["body"] = %q, want the example's source body %q`, got, "Badge example body.")
	}
}

// TestExampleRawContext tests the reserved `.raw` context builder directly: it
// copies only the declared fields the author supplied (a declared default and an
// undeclared key are both absent) and includes `body` only when the author
// supplied one.
func TestExampleRawContext(t *testing.T) {
	def := &Template{
		Name:  "probe",
		Usage: UsageSection,
		Fields: []Field{
			{Name: "title", Type: FieldText, Default: "Defaulted"},
			{Name: "subtitle", Type: FieldText},
		},
	}

	t.Run("copies supplied declared fields and excludes defaults and undeclared keys", func(t *testing.T) {
		got := exampleRawContext(map[string]any{
			"title":        "Authored",
			"other":        "undeclared",
			"title_format": "plain",
		}, def, "")
		want := map[string]any{"title": "Authored"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("exampleRawContext() = %v, want %v", got, want)
		}
	})

	t.Run("includes body only when the author supplied one", func(t *testing.T) {
		if got := exampleRawContext(nil, def, ""); len(got) != 0 {
			t.Errorf("exampleRawContext(nil, def, \"\") = %v, want empty (default excluded, no body)", got)
		}
		got := exampleRawContext(nil, def, "Some body.")
		want := map[string]any{"body": "Some body."}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("exampleRawContext(nil, def, body) = %v, want %v", got, want)
		}
	})

	t.Run("a nil template or data yields a well-formed context", func(t *testing.T) {
		if got := exampleRawContext(nil, nil, ""); got == nil || len(got) != 0 {
			t.Errorf("exampleRawContext(nil, nil, \"\") = %v, want a non-nil empty map", got)
		}
		got := exampleRawContext(nil, nil, "Only body.")
		want := map[string]any{"body": "Only body."}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("exampleRawContext(nil, nil, body) = %v, want %v", got, want)
		}
	})
}

// TestExampleDataContext tests the reserved `.data` context builder directly:
// authored instances are keyed by declared section name (one entry per
// instance, source-ordered, with per-name sibling counts), each entry carries
// its own `raw`/`item`/`data`, an explicit `template:` selector resolves the
// entry's template and is not copied into `raw`, and the recursion sets a
// child's `item.parent` to the enclosing instance's source values.
func TestExampleDataContext(t *testing.T) {
	itemTmpl := &Template{Name: "item", Usage: UsageSection, Fields: []Field{{Name: "name", Type: FieldText}}}
	columnTmpl := &Template{
		Name:     "column",
		Usage:    UsageSection,
		Fields:   []Field{{Name: "title", Type: FieldText}},
		Sections: []SectionDecl{{Name: "items", Accepted: []string{"item"}}},
	}
	blockTmpl := &Template{Name: "block", Usage: UsageSection, Fields: []Field{{Name: "text", Type: FieldText}}}
	widgetTmpl := &Template{Name: "widget", Usage: UsageSection, Fields: []Field{{Name: "name", Type: FieldText}}}
	hostTmpl := &Template{
		Name:  "host",
		Usage: UsageSlide,
		Sections: []SectionDecl{
			{Name: "columns", Accepted: []string{"column"}},
			{Name: "blocks", Accepted: []string{"block"}},
			// Two accepted templates, so an instance must name its template.
			{Name: "widgets", Accepted: []string{"block", "widget"}},
		},
	}
	resolve := resolverFor(itemTmpl, columnTmpl, blockTmpl, widgetTmpl)

	t.Run("keyed by name, source-ordered, with per-name sibling counts", func(t *testing.T) {
		sections := []exampleSection{
			{Name: "blocks", Index: 0, exampleBlock: &exampleBlock{Frontmatter: map[string]any{"text": "A"}}},
			{Name: "widgets", Index: 0, exampleBlock: &exampleBlock{Frontmatter: map[string]any{"template": "widget", "name": "W"}}},
			{Name: "blocks", Index: 1, exampleBlock: &exampleBlock{Frontmatter: map[string]any{"text": "B"}}},
		}
		got := exampleDataContext(sections, hostTmpl, resolve, nil)
		if len(got) != 2 {
			t.Fatalf("exampleDataContext() keys = %v, want exactly blocks and widgets", contextKeys(got))
		}

		blocks := contextEntryList(t, got["blocks"], `data["blocks"]`)
		if len(blocks) != 2 {
			t.Fatalf(`len(data["blocks"]) = %d, want 2`, len(blocks))
		}
		for i, want := range []string{"A", "B"} {
			entryRaw := contextMap(t, blocks[i]["raw"], "blocks entry raw")
			if got := entryRaw["text"]; got != want {
				t.Errorf(`data[blocks][%d].raw.text = %v, want %q`, i, got, want)
			}
		}
		wantBlock0Item := map[string]any{
			"index": 0, "number": 1, "count": 2, "first": true, "last": false,
			"section": "blocks", "template": "block", "parent": map[string]any(nil),
		}
		if !reflect.DeepEqual(blocks[0]["item"], wantBlock0Item) {
			t.Errorf("data[blocks][0].item = %v, want %v", blocks[0]["item"], wantBlock0Item)
		}
		wantBlock1Item := map[string]any{
			"index": 1, "number": 2, "count": 2, "first": false, "last": true,
			"section": "blocks", "template": "block", "parent": map[string]any(nil),
		}
		if !reflect.DeepEqual(blocks[1]["item"], wantBlock1Item) {
			t.Errorf("data[blocks][1].item = %v, want %v", blocks[1]["item"], wantBlock1Item)
		}

		widgets := contextEntryList(t, got["widgets"], `data["widgets"]`)
		if len(widgets) != 1 {
			t.Fatalf(`len(data["widgets"]) = %d, want 1`, len(widgets))
		}
		// The explicit `template:` selector resolves the entry's template and
		// the reserved selector key is not copied into raw.
		wantWidgetRaw := map[string]any{"name": "W"}
		if !reflect.DeepEqual(widgets[0]["raw"], wantWidgetRaw) {
			t.Errorf(`data[widgets][0].raw = %v, want %v`, widgets[0]["raw"], wantWidgetRaw)
		}
		wantWidgetItem := map[string]any{
			"index": 0, "number": 1, "count": 1, "first": true, "last": true,
			"section": "widgets", "template": "widget", "parent": map[string]any(nil),
		}
		if !reflect.DeepEqual(widgets[0]["item"], wantWidgetItem) {
			t.Errorf("data[widgets][0].item = %v, want %v", widgets[0]["item"], wantWidgetItem)
		}
	})

	t.Run("recursion sets a child's parent to the enclosing instance's source", func(t *testing.T) {
		child := exampleSection{
			Name:         "items",
			Index:        0,
			exampleBlock: &exampleBlock{Frontmatter: map[string]any{"name": "Child"}},
		}
		column := exampleSection{
			Name:  "columns",
			Index: 0,
			exampleBlock: &exampleBlock{
				Frontmatter: map[string]any{"title": "Col"},
				Body:        "Col body.",
				Sections:    []exampleSection{child},
			},
		}
		got := exampleDataContext([]exampleSection{column}, hostTmpl, resolve, nil)
		entries := contextEntryList(t, got["columns"], `data["columns"]`)
		if len(entries) != 1 {
			t.Fatalf(`len(data["columns"]) = %d, want 1`, len(entries))
		}
		entry := entries[0]
		wantColumnRaw := map[string]any{"title": "Col", "body": "Col body."}
		if !reflect.DeepEqual(entry["raw"], wantColumnRaw) {
			t.Errorf(`data[columns][0].raw = %v, want %v`, entry["raw"], wantColumnRaw)
		}

		nested := contextEntryList(t, contextMap(t, entry["data"], "column data")["items"], `data[columns][0].data["items"]`)
		if len(nested) != 1 {
			t.Fatalf(`len(data[columns][0].data["items"]) = %d, want 1`, len(nested))
		}
		nestedItem := contextMap(t, nested[0]["item"], "nested item")
		if got := nestedItem["section"]; got != "items" {
			t.Errorf("nested item.section = %v, want %q", got, "items")
		}
		if got := nestedItem["template"]; got != "item" {
			t.Errorf("nested item.template = %v, want %q", got, "item")
		}
		if want := entry["raw"]; !reflect.DeepEqual(nestedItem["parent"], want) {
			t.Errorf("nested item.parent = %v, want the enclosing instance's raw %v", nestedItem["parent"], want)
		}
		if want := map[string]any{"name": "Child"}; !reflect.DeepEqual(nested[0]["raw"], want) {
			t.Errorf("nested raw = %v, want %v", nested[0]["raw"], want)
		}
	})

	t.Run("a nil resolver yields unresolved entries rather than a panic", func(t *testing.T) {
		sections := []exampleSection{
			{Name: "blocks", Index: 0, exampleBlock: &exampleBlock{Frontmatter: map[string]any{"text": "A"}}},
		}
		got := exampleDataContext(sections, hostTmpl, nil, nil)
		entries := contextEntryList(t, got["blocks"], `data["blocks"]`)
		if len(entries) != 1 {
			t.Fatalf(`len(data["blocks"]) = %d, want 1`, len(entries))
		}
		// With no resolver the entry's template is nil, so raw has no declared
		// field; the declared name still resolves the entry's template label.
		if got := contextMap(t, entries[0]["item"], "item")["template"]; got != "block" {
			t.Errorf("item.template = %v, want the single accepted %q", got, "block")
		}
		if raw := contextMap(t, entries[0]["raw"], "raw"); len(raw) != 0 {
			t.Errorf("raw = %v, want empty without a resolved template", raw)
		}
	})
}

// exampleLibraryResolver returns a name → *Template resolver over a loaded
// library's definitions, matching the resolver checkLibraryExample builds from
// lib.TemplateByName.
func exampleLibraryResolver(lib *Library) func(string) (*Template, bool) {
	return func(name string) (*Template, bool) {
		other, _, ok := lib.TemplateByName(name)
		if !ok || other == nil || other.Definition == nil {
			return nil, false
		}
		return other.Definition, true
	}
}

// contextMap asserts v is a map[string]any and returns it, so a mistyped
// context value fails the test with a clear message rather than panicking.
func contextMap(t *testing.T, v any, what string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: got %T, want map[string]any", what, v)
	}
	return m
}

// contextEntryList asserts v is a []map[string]any and returns it.
func contextEntryList(t *testing.T, v any, what string) []map[string]any {
	t.Helper()
	list, ok := v.([]map[string]any)
	if !ok {
		t.Fatalf("%s: got %T, want []map[string]any", what, v)
	}
	return list
}

// contextKeys returns a map's keys, for a failure message that names the
// unexpected key set.
func contextKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
