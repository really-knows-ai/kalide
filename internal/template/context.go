package template

// This file declares ContextNode, the neutral section-tree node shared by the
// two halves of the template context: the load-time example execution
// (templates-dir-validation step 7, examplecheck.go) and the render-time
// execution (internal/render). internal/template cannot import internal/render,
// so the node lives here: each half maps its own section representation —
// slide.Section at render, exampleSection at load — onto the same node, and the
// shared .raw/.data context builders walk that node, so the load-time and
// render-time views cannot drift (raw-source-context, section-data-context,
// template-context).

// ContextNode is one authored section instance in the neutral section tree the
// shared .raw/.data context builders walk. It carries exactly the inputs those
// builders read from an instance, with no dependency on either side's own
// parser:
//
//   - the declared section name (Name) — the heading text the instance was
//     authored under, used to group its `.data` entry and as the `.item`
//     descriptor's `section`;
//   - the sibling position (Index) — the instance's zero-based index among its
//     same-name siblings, from which the `.item` descriptor's `index`, `number`,
//     `first` and `last` are derived (the sibling group supplies the count);
//   - the resolved template (Template) — the section template the instance
//     resolves to, whose Name is the `.item` descriptor's `template` and whose
//     declared fields are the schema the `.raw` source view is filtered by;
//   - the source field values and body (Source, Body) — the author's ORIGINAL,
//     pre-conversion values from which the reserved `.raw` source view is built;
//   - the child nodes (Children) — the instance's own authored children in
//     source order, recursed into the reserved `.data` view.
//
// Only authored instances become nodes. A section invoked by the `section`
// helper is a direct render call, never part of the parsed tree, so it has no
// node and appears in neither `.data` nor the rendered-HTML section list
// (section-data-context).
//
// The type is a plain value the adapters build and the shared builders walk;
// neither side reaches back into a slide.Section or an exampleSection.
type ContextNode struct {
	// Name is the declared section name the instance was authored under: the
	// heading text, the key its `.data` entry is grouped under, and the
	// `.item` descriptor's `section`.
	Name string

	// Index is the instance's zero-based position among its same-name
	// siblings. The `.item` descriptor's `index`, `number`, `first` and
	// `last` are derived from it and the sibling group's count.
	Index int

	// Template is the section template the instance resolves to — an explicit
	// `template:` selector, otherwise the single accepted template — or nil
	// when it could not be resolved. Its Name is the `.item` descriptor's
	// `template`, and its declared Fields are the schema the `.raw` source
	// view copies author-supplied values by. A nil Template yields an empty
	// `template` name and a source view that copies no fields, leaving any
	// body.
	Template *Template

	// Source is the instance's pre-conversion source values: a slide's or
	// section's decoded frontmatter, or a section helper call's supplied
	// fields. It is the mapping the `.raw` source view is filtered from, so
	// only the resolved template's declared fields are copied.
	Source map[string]any

	// Body is the instance's original body source, before any conversion, or
	// "" when the author supplied none. The `.raw` source view carries it
	// under `body` when non-empty.
	Body string

	// Children are the instance's own authored child instances, in source
	// order, each a ContextNode in turn. The reserved `.data` view recurses
	// into them, keyed and shaped the same way.
	Children []ContextNode
}

// RawContext builds the reserved `.raw` execution-context entry for a slide or
// section instance (raw-source-context): the author's ORIGINAL source values,
// alongside the converted ones the same context carries under the field names.
// This is the single shared builder both halves use — the load-time example
// execution (templates-dir-validation step 7, examplecheck.go) and the
// render-time execution (internal/render) — so the two source views cannot
// drift (template-context).
//
// It holds the source value of every declared field the author supplied —
// keyed by the field's own name, for a text field the Markdown exactly as
// written, before inline-Markdown rendering — and the instance's original body
// source under `body` when the author supplied one.
//
// source is the instance's pre-conversion source map — a slide's or section's
// decoded frontmatter, or a section helper call's supplied fields — and t is
// the resolved template whose field schema names the declared fields. Only
// declared fields are copied: the reserved `template:` selector, a
// `<field>_format` sibling and any other undeclared key are not fields. A field
// the author did not supply is absent, and a field's declared default is not
// copied, because a default is not an authored source value; a body the author
// did not supply is likewise absent. A nil t or source yields a well-formed
// (possibly body-only) context, so a caller may pass them freely.
func RawContext(source map[string]any, t *Template, body string) map[string]any {
	out := make(map[string]any, 1)
	if t != nil {
		for i := range t.Fields {
			name := t.Fields[i].Name
			if v, present := source[name]; present {
				out[name] = v
			}
		}
	}
	if body != "" {
		out["body"] = body
	}
	return out
}

// DataContext builds the reserved `.data` execution-context entry for a parent
// instance's authored child sections (section-data-context): the same authored
// instances the rendered-HTML list groups under each declared section name,
// exposed in parallel as DATA. This is the single shared builder both halves
// use — the load-time example execution (templates-dir-validation step 7,
// examplecheck.go) and the render-time execution (internal/render) — so the
// two data views cannot drift (template-context).
//
// The result is keyed by declared section name; each key maps to a
// source-ordered list with one entry per authored instance of that section, and
// each entry is a map carrying:
//
//   - `raw`: the instance's original source field values and body source
//     (RawContext) — the source view of its fields, alongside the converted
//     values the rendered instance exposes under their own names;
//   - `item`: the instance's sibling descriptor, matching the render side's
//     itemContext shape — its zero-based `index` and one-based `number` among
//     the same-name siblings, their `count`, `first`/`last`, the declared
//     `section` name, the resolved `template` name, and `parent` — the
//     enclosing instance's source field values, or a typed nil map at the top
//     level, where the parent is the slide, not a section instance
//     (item-context);
//   - `data`: the instance's own child sections as data, keyed and shaped the
//     same way (the recursion), with the instance's `raw` as the children's
//     parent, so the whole authored tree is addressable as data.
//
// nodes is one parent's authored children in source order, each a neutral
// ContextNode carrying its declared name, its sibling index, its resolved
// template and its own children; parent is the enclosing instance's source
// field values (the same map that instance exposes as its `.raw`), or nil at
// the top level. A nil node template yields an empty `template` name and a
// source view that copies no fields, leaving any body. A nil or empty nodes
// slice yields a non-nil empty map.
//
// Only authored instances appear: a section invoked by the `section` helper is
// a direct call, never part of the parsed tree, so it is absent from `.data`
// exactly as it is absent from the rendered-HTML list.
func DataContext(nodes []ContextNode, parent map[string]any) map[string]any {
	out := make(map[string]any, len(nodes))
	counts := make(map[string]int, len(nodes))
	for i := range nodes {
		counts[nodes[i].Name]++
	}
	for i := range nodes {
		node := &nodes[i]
		raw := RawContext(node.Source, node.Template, node.Body)
		tmplName := ""
		if node.Template != nil {
			tmplName = node.Template.Name
		}
		entry := map[string]any{
			"raw": raw,
			"item": map[string]any{
				"index":    node.Index,
				"number":   node.Index + 1,
				"count":    counts[node.Name],
				"first":    node.Index == 0,
				"last":     node.Index == counts[node.Name]-1,
				"section":  node.Name,
				"template": tmplName,
				"parent":   parent,
			},
			"data": DataContext(node.Children, raw),
		}
		list, _ := out[node.Name].([]map[string]any)
		out[node.Name] = append(list, entry)
	}
	return out
}
