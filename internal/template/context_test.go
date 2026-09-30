package template

import (
	"reflect"
	"testing"
)

// This file is the unit-test deliverable for section-helpers/plan.phase-07.task-3:
// the shared phase-5 context builders template.RawContext and template.DataContext
// and the neutral template.ContextNode tree they walk (context.go).
//
// It deliberately COMPLEMENTS, rather than repeats, examplecontext_test.go: that
// file reaches the builders through the load-time LoadLibrary pipeline and drives
// DataContext via the exampleContextNodes adapter; this file pins the builders'
// own contract directly —
//
//   - TestRawContextSourceView: the source-view filter (declared fields only,
//     reserved keys excluded, a declared default is not a source value, a body
//     only when non-empty, a nil template copies no fields, the source is not
//     mutated);
//   - TestDataContextLiteralNodes: DataContext over a LITERALLY built
//     []ContextNode, independent of any adapter — the `.raw`/`.item`/`.data`
//     shape, per-name sibling counts and first/last, a nil-template node, the
//     typed-nil top-level `parent`, and the recursion threading the enclosing
//     instance's raw as its children's parent;
//   - TestExampleContextNodesAdapter: the in-package adapter round-trips an
//     authored []exampleSection tree into the neutral []ContextNode (single
//     accepted template auto-resolved, an explicit `template:` selector among
//     several accepted, body and source propagation, recursion, and a
//     name-only template for an unresolvable name);
//
// It runs under -short: everything is an in-memory literal, so no filesystem or
// network is touched. The shared assertion helpers contextMap/contextEntryList/
// contextKeys and the newSection/resolverFor constructors live in the sibling
// test files of this package.

// TestRawContextSourceView pins the shared `.raw` source-view filter directly: it
// copies only the resolved template's declared fields the author supplied (a
// declared default and any undeclared/reserved key are absent), adds `body` only
// when non-empty, treats an explicitly present nil as an authored value, copies
// no fields under a nil template, and never mutates its source map. The
// LoadLibrary-driven reading of the same contract is in examplecontext_test.go.
func TestRawContextSourceView(t *testing.T) {
	def := &Template{
		Name:  "sourceview",
		Usage: UsageSection,
		Fields: []Field{
			{Name: "title", Type: FieldText, Default: "Defaulted"},
			{Name: "count", Type: FieldNumber},
		},
	}

	t.Run("copies only declared author-supplied values plus a non-empty body", func(t *testing.T) {
		source := map[string]any{
			"title":        "Authored",
			"other":        "undeclared",
			"title_format": "plain",
			"template":     "sourceview",
			"notes":        "reserved",
		}
		got := RawContext(source, def, "A source body.")
		want := map[string]any{
			"title": "Authored",
			"body":  "A source body.",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("RawContext() = %v, want %v (only declared fields, no reserved keys)", got, want)
		}
	})

	t.Run("a declared default is absent when the author omits the field", func(t *testing.T) {
		got := RawContext(map[string]any{"count": 3.0}, def, "")
		want := map[string]any{"count": 3.0}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("RawContext() = %v, want the defaulted title absent: %v", got, want)
		}
	})

	t.Run("an explicitly present nil is an authored value and is copied", func(t *testing.T) {
		got := RawContext(map[string]any{"title": nil}, def, "")
		v, has := got["title"]
		if !has {
			t.Fatalf("RawContext() = %v, want the present nil title copied", got)
		}
		if v != nil {
			t.Errorf(`raw["title"] = %v, want nil`, v)
		}
	})

	t.Run("a nil template copies no source fields but still carries a body", func(t *testing.T) {
		got := RawContext(map[string]any{"title": "Authored"}, nil, "Body.")
		want := map[string]any{"body": "Body."}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("RawContext(nil template) = %v, want %v (no fields copied)", got, want)
		}
	})

	t.Run("the source map is not mutated", func(t *testing.T) {
		source := map[string]any{"title": "Authored", "other": "keep"}
		before := map[string]any{"title": "Authored", "other": "keep"}
		_ = RawContext(source, def, "body")
		if !reflect.DeepEqual(source, before) {
			t.Errorf("RawContext() mutated its source = %v, want %v", source, before)
		}
	})
}

// TestDataContextLiteralNodes pins DataContext's contract over a literally built
// []ContextNode, with no adapter in the loop: authored instances are grouped by
// declared name in source order, each entry carries its own `raw`/`item`/`data`,
// the sibling descriptor derives number/first/last from the per-name count, a
// nil-template node labels `template` "" and copies only its body, the top-level
// `parent` is a typed nil map, and the recursion threads the enclosing instance's
// raw to its children's `parent`.
func TestDataContextLiteralNodes(t *testing.T) {
	block := newSection("block", Field{Name: "text", Type: FieldText})

	nodes := []ContextNode{
		{
			Name:     "blocks",
			Index:    0,
			Template: block,
			Source:   map[string]any{"text": "A"},
			Body:     "First body",
			Children: []ContextNode{
				{Name: "blocks", Index: 0, Template: block, Source: map[string]any{"text": "A.1"}},
			},
		},
		{
			Name: "widgets",
			// A nil template: no declared fields, so only the body survives.
			Index:  0,
			Source: map[string]any{"name": "ignored"},
			Body:   "Widget body",
		},
		{Name: "blocks", Index: 1, Template: block, Source: map[string]any{"text": "B"}},
	}

	got := DataContext(nodes, nil)
	if len(got) != 2 {
		t.Fatalf("DataContext() keys = %v, want exactly blocks and widgets", contextKeys(got))
	}

	blocks := contextEntryList(t, got["blocks"], `data["blocks"]`)
	if len(blocks) != 2 {
		t.Fatalf(`len(data["blocks"]) = %d, want 2 same-name siblings`, len(blocks))
	}
	if want := map[string]any{"text": "A", "body": "First body"}; !reflect.DeepEqual(blocks[0]["raw"], want) {
		t.Errorf("data[blocks][0].raw = %v, want %v", blocks[0]["raw"], want)
	}
	if want := map[string]any{"text": "B"}; !reflect.DeepEqual(blocks[1]["raw"], want) {
		t.Errorf("data[blocks][1].raw = %v, want %v (no body)", blocks[1]["raw"], want)
	}

	wantBlock0 := map[string]any{
		"index": 0, "number": 1, "count": 2, "first": true, "last": false,
		"section": "blocks", "template": "block", "parent": map[string]any(nil),
	}
	if !reflect.DeepEqual(blocks[0]["item"], wantBlock0) {
		t.Errorf("data[blocks][0].item = %v, want %v", blocks[0]["item"], wantBlock0)
	}
	wantBlock1 := map[string]any{
		"index": 1, "number": 2, "count": 2, "first": false, "last": true,
		"section": "blocks", "template": "block", "parent": map[string]any(nil),
	}
	if !reflect.DeepEqual(blocks[1]["item"], wantBlock1) {
		t.Errorf("data[blocks][1].item = %v, want %v", blocks[1]["item"], wantBlock1)
	}

	// The recursion: the first block's child data is keyed and shaped the same
	// way, and its item.parent is the enclosing instance's raw.
	nested := contextEntryList(t, contextMap(t, blocks[0]["data"], "blocks[0].data")["blocks"], `data[blocks][0].data["blocks"]`)
	if len(nested) != 1 {
		t.Fatalf(`len(data[blocks][0].data["blocks"]) = %d, want 1`, len(nested))
	}
	if want := map[string]any{"text": "A.1"}; !reflect.DeepEqual(nested[0]["raw"], want) {
		t.Errorf("nested raw = %v, want %v", nested[0]["raw"], want)
	}
	nestedItem := contextMap(t, nested[0]["item"], "nested item")
	if want := blocks[0]["raw"]; !reflect.DeepEqual(nestedItem["parent"], want) {
		t.Errorf("nested item.parent = %v, want the enclosing instance's raw %v", nestedItem["parent"], want)
	}

	// A nil-template node labels its template "" and copies no declared fields,
	// but still carries the raw body.
	widgets := contextEntryList(t, got["widgets"], `data["widgets"]`)
	if len(widgets) != 1 {
		t.Fatalf(`len(data["widgets"]) = %d, want 1`, len(widgets))
	}
	if want := map[string]any{"body": "Widget body"}; !reflect.DeepEqual(widgets[0]["raw"], want) {
		t.Errorf("data[widgets][0].raw = %v, want %v (nil template copies only the body)", widgets[0]["raw"], want)
	}
	wantWidget := map[string]any{
		"index": 0, "number": 1, "count": 1, "first": true, "last": true,
		"section": "widgets", "template": "", "parent": map[string]any(nil),
	}
	if !reflect.DeepEqual(widgets[0]["item"], wantWidget) {
		t.Errorf("data[widgets][0].item = %v, want %v", widgets[0]["item"], wantWidget)
	}

	t.Run("nil and empty nodes yield a non-nil empty map", func(t *testing.T) {
		if got := DataContext(nil, nil); got == nil || len(got) != 0 {
			t.Errorf("DataContext(nil, nil) = %v, want a non-nil empty map", got)
		}
		if got := DataContext([]ContextNode{}, nil); got == nil || len(got) != 0 {
			t.Errorf("DataContext([], nil) = %v, want a non-nil empty map", got)
		}
	})
}

// TestExampleContextNodesAdapter round-trips an authored example/section tree
// into the neutral []ContextNode the shared DataContext walks. It pins the
// adapter's own contract — the resolved template pointer, the original source
// map and body carried verbatim, single-accepted auto-resolution, an explicit
// `template:` selector among several accepted templates, recursion with the
// child's template as its schema, a name-only template for an unresolvable name,
// and nil for no sections — then feeds the result back through DataContext to
// show the tree produces the `.raw`/`.item` shape.
func TestExampleContextNodesAdapter(t *testing.T) {
	item := newSection("item", Field{Name: "name", Type: FieldText})
	column := &Template{
		Name:     "column",
		Usage:    UsageSection,
		Fields:   []Field{{Name: "title", Type: FieldText}},
		Sections: []SectionDecl{{Name: "items", Accepted: []string{"item"}}},
	}
	block := newSection("block", Field{Name: "text", Type: FieldText})
	widget := newSection("widget", Field{Name: "name", Type: FieldText})
	host := &Template{
		Name:  "host",
		Usage: UsageSlide,
		Sections: []SectionDecl{
			{Name: "columns", Accepted: []string{"column"}},
			// Two accepted templates, so an instance must name its template.
			{Name: "widgets", Accepted: []string{"block", "widget"}},
		},
	}
	resolve := resolverFor(item, column, block, widget)

	t.Run("round-trips resolved templates, source, body and recursion", func(t *testing.T) {
		child := exampleSection{
			Name:         "items",
			Index:        0,
			exampleBlock: &exampleBlock{Frontmatter: map[string]any{"name": "Child"}, Body: "item body"},
		}
		col := exampleSection{
			Name:  "columns",
			Index: 0,
			exampleBlock: &exampleBlock{
				Frontmatter: map[string]any{"title": "Col"},
				Body:        "col body",
				Sections:    []exampleSection{child},
			},
		}
		wide := exampleSection{
			Name:         "widgets",
			Index:        0,
			exampleBlock: &exampleBlock{Frontmatter: map[string]any{"template": "widget", "name": "W"}},
		}

		nodes := exampleContextNodes([]exampleSection{col, wide}, host, resolve)
		if len(nodes) != 2 {
			t.Fatalf("exampleContextNodes() = %d nodes, want 2", len(nodes))
		}

		if nodes[0].Name != "columns" {
			t.Errorf("nodes[0].Name = %q, want %q", nodes[0].Name, "columns")
		}
		if nodes[0].Template != column {
			t.Errorf("nodes[0].Template = %v, want the single-accepted column template", nodes[0].Template)
		}
		if !reflect.DeepEqual(nodes[0].Source, col.Frontmatter) {
			t.Errorf("nodes[0].Source = %v, want the verbatim frontmatter %v", nodes[0].Source, col.Frontmatter)
		}
		if nodes[0].Body != "col body" {
			t.Errorf("nodes[0].Body = %q, want %q", nodes[0].Body, "col body")
		}
		if len(nodes[0].Children) != 1 {
			t.Fatalf("nodes[0].Children = %d, want 1 recursive child", len(nodes[0].Children))
		}
		nested := nodes[0].Children[0]
		if nested.Name != "items" || nested.Template != item {
			t.Errorf("nested node = %+v, want name items resolved to the item template", nested)
		}
		if !reflect.DeepEqual(nested.Source, child.Frontmatter) || nested.Body != "item body" {
			t.Errorf("nested node source/body = %v/%q, want %v/%q", nested.Source, nested.Body, child.Frontmatter, "item body")
		}

		// The explicit `template:` selector among two accepted templates wins.
		if nodes[1].Name != "widgets" || nodes[1].Template != widget {
			t.Errorf("nodes[1] = %+v, want widgets resolved to the selected widget template", nodes[1])
		}
		// DataContext then produces the `.raw` source view: the declared field
		// only, never the reserved `template:` selector.
		data := DataContext(nodes, nil)
		widgets := contextEntryList(t, data["widgets"], `data["widgets"]`)
		if want := map[string]any{"name": "W"}; !reflect.DeepEqual(widgets[0]["raw"], want) {
			t.Errorf("data[widgets][0].raw = %v, want %v (selector excluded)", widgets[0]["raw"], want)
		}
		if got := contextMap(t, widgets[0]["item"], "widgets item")["template"]; got != "widget" {
			t.Errorf("data[widgets][0].item.template = %v, want %q", got, "widget")
		}
	})

	t.Run("an unresolvable name yields a name-only template that copies no fields", func(t *testing.T) {
		ghost := exampleSection{
			Name:         "widgets",
			Index:        0,
			exampleBlock: &exampleBlock{Frontmatter: map[string]any{"template": "ghost", "name": "G"}},
		}
		nodes := exampleContextNodes([]exampleSection{ghost}, host, resolve)
		if len(nodes) != 1 || nodes[0].Template == nil {
			t.Fatalf("exampleContextNodes() = %+v, want one node with a name-only template", nodes)
		}
		if nodes[0].Template.Name != "ghost" || len(nodes[0].Template.Fields) != 0 {
			t.Errorf("node.Template = %+v, want a name-only %q template", nodes[0].Template, "ghost")
		}
		data := DataContext(nodes, nil)
		widgets := contextEntryList(t, data["widgets"], `data["widgets"]`)
		if raw := contextMap(t, widgets[0]["raw"], "widgets raw"); len(raw) != 0 {
			t.Errorf("data[widgets][0].raw = %v, want empty for a name-only template", raw)
		}
		if got := contextMap(t, widgets[0]["item"], "widgets item")["template"]; got != "ghost" {
			t.Errorf("data[widgets][0].item.template = %v, want the declared name %q", got, "ghost")
		}
	})

	t.Run("no sections yields no nodes", func(t *testing.T) {
		if got := exampleContextNodes(nil, host, resolve); got != nil {
			t.Errorf("exampleContextNodes(nil) = %v, want nil", got)
		}
	})
}
