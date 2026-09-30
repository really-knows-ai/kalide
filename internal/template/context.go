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
