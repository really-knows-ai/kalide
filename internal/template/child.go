package template

import (
	"fmt"
	"sort"
	"strings"
)

// This file is the home for the `child` and `children` layout constructors: the
// two constructors a `section` helper call uses to pass child sections to its
// target (requirements.requirement.child-constructor,
// requirements.requirement.children-forwarding). It holds their request value
// (ChildRequest), the func-map entries ChildFunc and ChildrenFunc, and the
// resolution that turns a `section` call's child arguments into the target's
// declared sections (ResolveChildCall, ResolveChildren).
//
// `child "<section>" [fields] [body] [child…]` names one child of a declared
// section of the `section` call's target; `children "<section>" .data.<section>`
// forwards the caller's authored `.data` entries for a declared section as
// children, each keeping its own template selection, its raw fields, its raw
// body and its nested data. Both read nothing outside their arguments — they
// add no data access of their own — and a `child`/`children` value is consumed
// only as a child argument of a `section` call. Literal `child` arguments are
// checked at load; a `children` forwarding of a runtime `.data` list is checked
// when the target renders (requirements.requirement.section-helper-load-checks).
//
// `dict` and `list` (media.go) remain the source-value constructors; this file
// never adds a data access or a utility helper to the layout language.

// ChildRequest is one child section a `section` helper call passes to its
// target, as produced by the `child` and `children` layout constructors and
// consumed only as that call's child argument
// (requirements.requirement.child-constructor,
// requirements.requirement.children-forwarding).
//
// It is a plain value: the constructors build one and resolution reads it. A
// child names one declared section of the `section` call's target and is
// resolved exactly like an authored instance of that declaration — the target's
// accepted-template check, the declaration's min/max over the set passed, the
// instantiated template's defaults, field validation and body rule, its own
// nested children and its own `.item` descriptor (child-constructor). Which
// fields are meaningful depends on the constructor:
//
//   - a `child "<section>" [fields] [body] [child…]` fills Section, Fields,
//     Body/HasBody and Children;
//   - a `children "<section>" .data.<section>` fills Section, Forward and
//     Entries, and leaves Fields, Body and Children zero.
type ChildRequest struct {
	// Section is the declared section name the child fills: the name of a
	// slot in the target's declared `sections:`, exactly as an authored
	// `# <section>` heading names one.
	Section string

	// Fields is the source field values the child supplies, typically built
	// with `dict`, validated against the instantiated template's field schema
	// as an authored instance's frontmatter values are (field-rules). It is
	// nil when the constructor supplies no fields argument.
	//
	// It may carry the `template` selector key, which chooses the
	// instantiated template when Section's declaration accepts more than one
	// and takes precedence over any field the template declares under the
	// same name; resolution removes the key before field validation and the
	// undeclared-key check, so it is never counted as a field.
	Fields map[string]any

	// Body is the child's body Markdown, exactly as written, validated
	// against the instantiated template's body rule (body-rules).
	Body string

	// HasBody reports whether a body argument was supplied. It distinguishes
	// an omitted body from a provided empty one, which the body rule may
	// treat differently.
	HasBody bool

	// Children are the child's own nested `child` arguments, checked
	// recursively: they name the instantiated template's own declared child
	// sections, so grandchildren pass the same way. It is nil when the child
	// declares none.
	Children []*ChildRequest

	// Forward reports that this child is a `children` forwarding of a runtime
	// `.data` list rather than a literal `child`. Such a child is not
	// inspected at load and is checked against the target's declared sections
	// when the target renders (section-helper-load-checks).
	Forward bool

	// Entries is the forwarded `.data` list for a `children` forwarding: the
	// caller's authored entries for Section, each re-instantiated as a child
	// of the target keeping its own template, its `.raw` fields and body and
	// its own `.data` (children-forwarding). It is nil for a literal `child`.
	Entries []any
}

// ChildFunc is the `child` layout constructor:
// `{{ child "<section>" [fields] [body] [child…] }}`. It names one declared
// section of a `section` helper call's target and builds the ChildRequest that
// call passes as a child argument; the value is consumed only there, and the
// constructor reads nothing outside its arguments and adds no data access of
// its own (child-constructor, template-language).
//
// Argument handling mirrors the `section` helper's and `dict`'s arg checks
// (render's sectionHelper, DictFunc): the first argument names the declared
// section and must be a string; the optional second is the fields map and must
// be a map[string]any — nil is accepted and leaves Fields nil; the optional
// third is the body and must be a string; any remaining arguments are the
// child's own nested `child` arguments and must each be a *ChildRequest. A
// wrong type is an error, so a malformed call fails the whole template
// execution rather than building a partial request, exactly as a malformed
// `dict` does.
//
// HasBody records whether the body slot was actually supplied: an omitted body
// (no argument, or the nil placeholder) is distinguished from a provided empty
// one. Section, Fields, Body and Children are filled for later resolution;
// Forward and Entries are left zero, since only the `children` forwarding
// constructor builds those.
func ChildFunc(args ...any) (*ChildRequest, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("child: section must be a string, got no arguments")
	}
	section, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("child: section must be a string, got %s", describeValue(args[0]))
	}

	var fields map[string]any
	if len(args) >= 2 && args[1] != nil {
		m, ok := args[1].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("child %q: fields must be a map, got %T", section, args[1])
		}
		fields = m
	}

	var body string
	hasBody := false
	if len(args) >= 3 && args[2] != nil {
		s, ok := args[2].(string)
		if !ok {
			return nil, fmt.Errorf("child %q: body must be a string, got %T", section, args[2])
		}
		body = s
		hasBody = true
	}

	var children []*ChildRequest
	if len(args) > 3 {
		children = make([]*ChildRequest, 0, len(args)-3)
		for i, v := range args[3:] {
			c, ok := v.(*ChildRequest)
			if !ok {
				return nil, fmt.Errorf(
					"child %q: child argument %d must be a child value, got %T",
					section, i+1, v)
			}
			if c == nil {
				return nil, fmt.Errorf("child %q: child argument %d must not be nil", section, i+1)
			}
			children = append(children, c)
		}
	}

	return &ChildRequest{
		Section:  section,
		Fields:   fields,
		Body:     body,
		HasBody:  hasBody,
		Children: children,
	}, nil
}

// ChildrenFunc is the `children` layout constructor:
// `{{ children "<section>" .data.<section> }}`. It names one declared section
// of a `section` helper call's target and forwards the caller's already
// authored `.data` entries for that section as its children, building the
// ChildRequest that call passes as a child argument; the value is consumed only
// there, and the constructor reads nothing outside its arguments and adds no
// data access of its own (children-forwarding, template-language).
//
// Argument handling mirrors ChildFunc's: the first argument names the declared
// section and must be a string; the second is the `.data.<section>` list to
// forward. That list is whatever the shared data builder produces for the
// section — a []map[string]any (template.DataContext, the single builder both
// the load-time example execution and the render-time execution use) — or a
// []any a layout built in the template, and it is nil when the caller authored
// no entries for the section (a missing `.data.<section>` map key evaluates to
// nil, which is an empty forwarded set, not a malformed call). Any other type
// is an error, and a call that supplies no entries argument or extra arguments
// is an error too, so a malformed call fails the whole template execution
// rather than building a partial request, exactly as a malformed `child` does.
//
// Section, Forward and Entries are filled for later resolution; Fields, Body,
// HasBody and Children are left zero, since only the `child` constructor builds
// those.
func ChildrenFunc(args ...any) (*ChildRequest, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("children: section must be a string, got no arguments")
	}
	section, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("children: section must be a string, got %s", describeValue(args[0]))
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("children %q: entries must be a list, got no argument", section)
	}
	if len(args) > 2 {
		return nil, fmt.Errorf(
			"children %q: expected a section and entries, got %d arguments", section, len(args))
	}

	var entries []any
	switch list := args[1].(type) {
	case nil:
		// The caller authored no entries for the section: a missing
		// `.data.<section>` key evaluates to nil, so the forwarded set is
		// empty rather than malformed and Entries stays nil.
	case []any:
		entries = list
	case []map[string]any:
		entries = make([]any, len(list))
		for i := range list {
			entries[i] = list[i]
		}
	default:
		return nil, fmt.Errorf(
			"children %q: entries must be a list, got %T", section, args[1])
	}

	return &ChildRequest{
		Section: section,
		Forward: true,
		Entries: entries,
	}, nil
}

// ResolvedChild is one resolved child section argument: the child's own resolved
// instance and its own resolved nested children, in passed order. ResolveChildren
// returns these rather than bare *SectionCall values because a resolved child is
// a tree — a child may itself carry child sections — and the tree is carried on
// ResolvedChild.Children, mirroring the children a SectionCall records for its
// own target on SectionCall.Children. This is the recursive shape a caller
// renders depth-first: each child's own children first, since section markup
// composes bottom-up (requirements.requirement.child-constructor,
// requirements.requirement.children-forwarding).
type ResolvedChild struct {
	// Call is the child's resolved instance, exactly the shape ResolveChildCall
	// returns for a literal child: its instantiated Template, effective Values,
	// validated Body, one-item Item descriptor and owning Layer.
	Call *SectionCall

	// Source is the child's authored source field values BEFORE the
	// instantiated template's defaults were applied: a copy of the child's
	// Fields with the reserved `template` selector removed, exactly the map
	// ResolveChildCall validated (field-rules, raw-source-context). It is empty
	// when the child supplied no fields. A consumer builds the child's reserved
	// `.raw` view from it — the child body is Call.Body — so a materialised
	// default never leaks into `.raw` as an authored value; it is
	// Call.Source carried out for the example/render consumers.
	Source map[string]any

	// Children are the resolved instances of the child's own child arguments,
	// recursively, each in turn a *ResolvedChild, in the order the child's own
	// children were passed. It is nil/empty when the child declares no child
	// sections. When checkCounts is false an empty nested set is not walked, so
	// a child with no children carries a nil Children in that case.
	//
	// The same slice is recorded on the child's own Call.Children, so a consumer
	// rendering the child's own SectionCall reads its grandchildren from there
	// and publishes them to the child template's own `.data` and rendered
	// child-section lists; ResolvedChild.Children is the wrapper-facing view of
	// the same tree (section-data-context).
	Children []*ResolvedChild
}

// ResolveChildCall resolves one literal `child` argument against the declared
// section it fills on its parent template, building the resolved instance the
// parent renders it as (requirements.requirement.child-constructor,
// requirements.requirement.section-helper,
// requirements.requirement.nested-section-validation).
//
// lookup and layerOf are the same resolver pair ResolveSectionCallLayered takes:
// lookup resolves a section-template name in the merged template namespace
// (either library layer), and layerOf reports the *LibraryLayer that contributed
// a resolved name, or nil when the caller has no provenance (a single-root
// library, or a resolver built in code). parent is the template instance the
// child is a child OF — the `section` helper call's target — whose declared
// section req.Section the child fills. req is the literal ChildRequest to
// resolve; a `children` forwarding (req.Forward) is resolved by ResolveChildren,
// not here, and is refused.
//
// index and count place the child among its same-name siblings: index is its
// zero-based position and count the number of instances in the group, so the
// `.item` descriptor carries index/number/count/first/last exactly as an authored
// instance's does (requirements.requirement.item-context). parentFields is the
// parent instance's field values, carried out as `.item.parent` (nil from a
// slide layout, where the parent is the slide, not a section instance).
//
// Resolution mirrors an authored section instance of the named declaration:
//
//   - req.Section must be a section parent declares; an undeclared name is an
//     error naming the section and the parent template;
//   - the reserved `template` key is stripped from a COPY of req.Fields and used
//     as the selector: it is required iff the declaration accepts more than one
//     template, omitted when exactly one is accepted (that one is instantiated),
//     and must be one of the accepted names. The stripped copy is what is
//     validated, so the selector is never counted as an undeclared field and a
//     template's own `template` field cannot be set through a child
//     (requirements.requirement.field-rules);
//   - the instantiated template's declared defaults are materialised FIRST and
//     the effective map is validated with CheckValues (defaults-first);
//   - req.Body is validated against the instantiated template's body rule with
//     CheckBody, and a body whose heading names one of the instantiated
//     template's declared child sections is rejected (requirements.requirement.
//     body-rules): a child body carries no child sections either, so its nested
//     children pass only through nested `child` arguments, never through the
//     body — the same rejection the `section` helper's own body produces.
//
// The resolved instance is returned as a *SectionCall — the same resolved-
// instance shape ResolveSectionCallLayered returns — because a child resolves
// exactly like the helper's own target and the two share Template, Values,
// Source, Body, Item and Layer. Source is the stripped authored fields (the
// supplied fields with the `template` selector removed, before defaults), which
// ResolveChildren carries out as ResolvedChild.Source so a consumer builds the
// child's `.raw` from its authored values rather than its defaulted effective
// ones. `.item.section` is the named declared section (req.Section)
// while `.item.template` is the instantiated template's name. Layer is
// layerOf(tmplName), the instantiated template's OWN owning layer, so the child's
// `media`/`theme:<name>/` references resolve in that layer even when the layout
// comes from the other library layer (requirements.requirement.library-layer-references);
// a nil Layer means the primary/merged root.
//
// Nested req.Children are deliberately NOT resolved here: ResolveChildCall
// resolves exactly one literal child's own fields, body and `.item` descriptor.
// The recursive resolution of a child's own children — and of a `children`
// forwarding's entries — belongs to ResolveChildren, which the `section` helper
// entry points call for the whole child-argument set. It adds no data access of
// its own.
func ResolveChildCall(lookup func(name string) (*Template, bool), layerOf func(name string) *LibraryLayer, parent *Template, req *ChildRequest, index, count int, parentFields map[string]any) (*SectionCall, error) {
	if req == nil {
		return nil, fmt.Errorf("child: request must not be nil")
	}
	if req.Forward {
		return nil, fmt.Errorf("child %q: a children forwarding is resolved by ResolveChildren, not ResolveChildCall", req.Section)
	}
	if parent == nil {
		return nil, fmt.Errorf("child %q: no parent template to resolve the declared section against", req.Section)
	}
	if lookup == nil {
		return nil, fmt.Errorf("child %q: no template resolver", req.Section)
	}

	decl, declared := declaredSection(parent, req.Section)
	if !declared {
		return nil, fmt.Errorf("child: section %q is not declared by parent template %q", req.Section, parent.Name)
	}
	if len(decl.Accepted) == 0 {
		return nil, fmt.Errorf("child: section %q on parent template %q accepts no section template", req.Section, parent.Name)
	}

	// Strip the `template` selector from a copy of the fields before any field
	// validation: it is the selector, not a declared field, and stripping it is
	// what lets it take precedence over a template-declared field of the same
	// name (which therefore cannot be set through a child).
	fields := make(map[string]any, len(req.Fields))
	for k, v := range req.Fields {
		fields[k] = v
	}
	selector, hasSelector := "", false
	if raw, present := fields["template"]; present {
		hasSelector = true
		delete(fields, "template")
		s, ok := raw.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("child %q: key %q must name a section template", req.Section, "template")
		}
		selector = s
	}

	var tmplName string
	if !hasSelector {
		if len(decl.Accepted) > 1 {
			return nil, fmt.Errorf("child: section %q on parent template %q accepts more than one template; a %q selector is required",
				req.Section, parent.Name, "template")
		}
		tmplName = decl.Accepted[0]
	} else {
		if !containsString(decl.Accepted, selector) {
			return nil, fmt.Errorf("child: section %q on parent template %q does not accept template %q (accepts %s)",
				req.Section, parent.Name, selector, strings.Join(decl.Accepted, ", "))
		}
		tmplName = selector
	}

	target, ok := lookup(tmplName)
	if !ok || target == nil {
		return nil, fmt.Errorf("child %q: section template %q is not defined", req.Section, tmplName)
	}
	if target.Usage != UsageSection {
		return nil, fmt.Errorf("child %q: template %q is not a section-usage template", req.Section, tmplName)
	}

	effective := applyFieldDefaults(target, fields)
	if res := CheckValues(effective, target, lookup); len(res.Errors) > 0 {
		return nil, res.Errors[0]
	}

	if ve, invalid := CheckBody(target.Body, req.Body); invalid {
		return nil, ve
	}
	// A child body carries no child sections either: its nested children pass
	// only through nested `child` arguments, so a heading naming one of the
	// instantiated template's declared child sections is rejected rather than
	// silently dropped, exactly as for the section helper's own body
	// (requirements.requirement.body-rules, requirements.requirement.child-constructor).
	if child, named := bodyNamesChildSection(target, req.Body); named {
		return nil, ValueError{
			Path:  []PathSegment{{Name: bodyField}},
			Rule:  "subheadings",
			Value: req.Body,
			What:  fmt.Sprintf("subheadings: the body heading names child section %q, but a child body carries no child sections", child),
			Fix:   "remove the heading; pass the nested child with a child argument instead",
		}
	}

	call := &SectionCall{
		Template: target,
		Values:   effective,
		Source:   fields,
		Body:     req.Body,
		Item: map[string]any{
			"index":    index,
			"number":   index + 1,
			"count":    count,
			"first":    index == 0,
			"last":     index == count-1,
			"section":  req.Section,
			"template": tmplName,
			"parent":   parentFields,
		},
	}
	if layerOf != nil {
		call.Layer = layerOf(tmplName)
	}
	return call, nil
}

// ResolveChildren resolves a whole set of child arguments — every `child` and
// `children` argument a `section` helper call passes to its target — against the
// parent template's declared sections, returning one *ResolvedChild per resolved
// instance in passed order, each carrying its own resolved nested children
// (requirements.requirement.child-constructor,
// requirements.requirement.children-forwarding).
//
// lookup, layerOf, parent and parentFields are exactly ResolveChildCall's:
// lookup resolves a section-template name in the merged namespace, layerOf
// reports the owning library layer of a resolved name (or nil when the caller
// has no provenance), parent is the template the children are children OF — the
// `section` helper call's target, whose declared sections the children fill —
// and parentFields is the parent instance's field values, carried out as each
// child's `.item.parent` (nil from a slide layout). layerOf and parentFields may
// be nil and lookup must be non-nil whenever there are children to resolve,
// exactly as ResolveChildCall allows. parent must be the template whose
// declared sections name the children; a nil parent with an empty set returns no
// children.
//
// reqs is the ordered child-argument set the call passed. Each element is one of
// two kinds:
//
//   - a literal `child` request (Forward false), resolved directly by
//     ResolveChildCall: its Section must be a declared section of parent, its
//     `template` selector picks among the declaration's accepted templates, its
//     fields and body are validated against the instantiated template, and it
//     gets its own `.item` descriptor (child-constructor);
//   - a `children` forwarding (Forward true): its Entries are the caller's
//     authored `.data.<section>` entries, and each entry is expanded into a
//     literal-equivalent request for Section and resolved the same way, so the
//     entries of one forwarded list may name different templates with no single
//     template imposed on the list (children-forwarding).
//
// A forwarded entry is the shape the shared .data builder (DataContext) produces
// for an authored instance: a map carrying `item` (the sibling descriptor),
// `raw` (the original source field values and body) and `data` (the instance's
// own children as data). Its template is injected from `item.template` as the
// `template` selector; its fields are `raw` without the `body` key; its body is
// `raw.body`; and its own children are rebuilt from `data`, so a layout passes on
// the authored section instances it received without re-listing them
// (raw-source-context, section-data-context).
//
// Sibling index/count are computed over the FULL passed set, grouped by declared
// section name: a literal child and every forwarded entry for the same section
// are counted together, so each resolved child's `.item`
// index/number/count/first/last describe its place among all the children of
// that section the call passed, exactly as an authored instance's descriptor
// does (item-context, child-constructor).
//
// Nested children — a literal request's Children and a forwarded entry's `.data`
// — are resolved recursively with the child's OWN instantiated template as the
// parent, so grandchildren name that template's declared sections; the recursion
// carries checkCounts through unchanged (nested-section-validation). Each
// resolved nested set is recorded BOTH on the wrapper ResolvedChild (its
// Children) and on the child's own SectionCall.Children, so a caller rendering
// the child's own resolved instance reads its grandchildren from that instance
// and publishes them to the child template's own `.data.<grandchild>` view and
// rendered child-section lists, not only to the parent's `.data` view
// (section-data-context, child-constructor, children-forwarding).
//
// When checkCounts is true, every section parent declares has its min/max
// enforced over the passed set — the count of literal children and expanded
// forwarded entries for that name — and a violation is an error naming the
// declaration and the parent template. checkCounts is false for a load-time
// partial set: a `children` forwarding of a runtime `.data` list is not
// statically known and a mixed literal/forwarded call defers the parent min/max
// to render (section-helper-load-checks). A render-time call resolving a
// complete set passes true; a load-time check of literal children that cannot
// see the whole set passes false.
func ResolveChildren(lookup func(name string) (*Template, bool), layerOf func(name string) *LibraryLayer, parent *Template, reqs []*ChildRequest, parentFields map[string]any, checkCounts bool) ([]*ResolvedChild, error) {
	if parent == nil {
		if len(reqs) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("children: no parent template to resolve declared sections against")
	}
	if len(reqs) > 0 && lookup == nil {
		return nil, fmt.Errorf("children: no template resolver")
	}

	// Expand every request into the literal-equivalent set resolution consumes,
	// keeping the passed order: a literal request is itself, a forwarding
	// contributes one request per entry. The expansion is what makes sibling
	// index/count and the min/max counts "over the full passed set" rather than
	// per argument.
	type expandedChild struct {
		section string
		req     *ChildRequest
	}
	expanded := make([]expandedChild, 0, len(reqs))
	for _, req := range reqs {
		if req == nil {
			return nil, fmt.Errorf("children: child request must not be nil")
		}
		if !req.Forward {
			expanded = append(expanded, expandedChild{section: req.Section, req: req})
			continue
		}
		for _, entry := range req.Entries {
			er, err := childEntryRequest(req.Section, entry)
			if err != nil {
				return nil, err
			}
			expanded = append(expanded, expandedChild{section: req.Section, req: er})
		}
	}

	counts := make(map[string]int, len(expanded))
	for i := range expanded {
		counts[expanded[i].section]++
	}

	out := make([]*ResolvedChild, 0, len(expanded))
	seen := make(map[string]int, len(counts))
	for i := range expanded {
		ec := &expanded[i]
		index := seen[ec.section]
		seen[ec.section] = index + 1

		call, err := ResolveChildCall(lookup, layerOf, parent, ec.req, index, counts[ec.section], parentFields)
		if err != nil {
			return nil, err
		}

		rc := &ResolvedChild{Call: call, Source: call.Source}
		// Recurse into the child's own children. An empty nested set is still
		// walked when min/max are being checked, so a literal child that omits
		// a mandatory child section of its own instantiated template is
		// reported rather than silently accepted; when counts are skipped an
		// empty set has nothing to resolve.
		if len(ec.req.Children) > 0 || checkCounts {
			nested, err := ResolveChildren(lookup, layerOf, call.Template, ec.req.Children, call.Values, checkCounts)
			if err != nil {
				return nil, err
			}
			rc.Children = nested
			// The child's OWN resolved instance also carries its nested
			// children on its SectionCall, not only on the wrapper ResolvedChild:
			// a consumer that renders the child's own SectionCall (the load-time
			// example runner and the render-time instance renderer) reads
			// call.Children to build the child's own `.data.<grandchild>` view
			// and its own rendered grandchild lists, so a nested helper child
			// publishes its grandchildren to its own template. Without this the
			// child's own SectionCall would carry no children and its `.data`
			// and rendered child lists would be empty at every depth below the
			// first (requirements.requirement.child-constructor,
			// requirements.requirement.children-forwarding,
			// requirements.requirement.section-data-context).
			call.Children = nested
		}
		out = append(out, rc)
	}

	if checkCounts {
		for i := range parent.Sections {
			d := &parent.Sections[i]
			if err := childSetRangeError(parent, *d, counts[d.Name]); err != nil {
				return nil, err
			}
		}
	}

	return out, nil
}

// childEntryRequest converts one forwarded `.data` entry into the
// literal-equivalent ChildRequest ResolveChildren resolves. section is the
// declared section name the entry's list was keyed under (the name the entry
// re-instantiates as a child of parent). The entry must be the map
// template.DataContext builds for an authored instance:
//
//   - `item.template` (when non-empty) is injected as the `template` selector,
//     so the entry keeps its own authored template and a forwarded list may mix
//     templates;
//   - `raw` supplies the source field values — copied without the `body` key —
//     and `raw.body` (when present) the body;
//   - `data` supplies the entry's own children, rebuilt as forwarding requests
//     per section name, so the nested tree recurses.
//
// A non-map entry, or a `raw`/`data` value of the wrong shape, is an error
// naming the section, so a malformed `.data` set fails resolution rather than
// silently dropping an instance (children-forwarding).
func childEntryRequest(section string, entry any) (*ChildRequest, error) {
	m, ok := entry.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("children %q: forwarded entry must be a .data entry map, got %s", section, describeValue(entry))
	}

	selector := ""
	if item, ok := m["item"].(map[string]any); ok {
		if s, ok := item["template"].(string); ok {
			selector = strings.TrimSpace(s)
		}
	}

	var fields map[string]any
	body := ""
	hasBody := false
	if raw, ok := m["raw"].(map[string]any); ok && len(raw) > 0 {
		fields = make(map[string]any, len(raw))
		for k, v := range raw {
			if k == bodyField {
				continue
			}
			fields[k] = v
		}
		if b, ok := raw[bodyField].(string); ok {
			body = b
			hasBody = true
		}
	}
	if selector != "" {
		if fields == nil {
			fields = make(map[string]any, 1)
		}
		fields["template"] = selector
	}

	children, err := childEntryChildren(section, m["data"])
	if err != nil {
		return nil, err
	}

	return &ChildRequest{
		Section:  section,
		Fields:   fields,
		Body:     body,
		HasBody:  hasBody,
		Children: children,
	}, nil
}

// childEntryChildren rebuilds a forwarded entry's `.data` map — its own child
// sections as data, keyed by declared section name — as the nested forwarding
// requests ResolveChildren recurses into. section names the ENTRY's declared
// section (the parent of these children) only for error messages. Each key
// becomes a Forward request for that section carrying the key's entry list, so
// the entry's own `.item`/`.raw`/`.data` are re-read one level down; a nil or
// empty map yields nil. Keys are visited in sorted order so resolution is
// deterministic.
func childEntryChildren(section string, data any) ([]*ChildRequest, error) {
	if data == nil {
		return nil, nil
	}
	m, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("children %q: forwarded entry .data must be a map, got %s", section, describeValue(data))
	}
	if len(m) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)

	children := make([]*ChildRequest, 0, len(names))
	for _, name := range names {
		entries, err := childEntryList(name, m[name])
		if err != nil {
			return nil, err
		}
		children = append(children, &ChildRequest{
			Section: name,
			Forward: true,
			Entries: entries,
		})
	}
	return children, nil
}

// childEntryList normalises one `.data.<section>` value to the forwarding
// Entries slice. template.DataContext produces a []map[string]any for an
// authored list and a layout may also build a []any; a nil list is an empty
// forwarded set, not a malformed one, matching ChildrenFunc. Any other type is
// an error naming the section.
func childEntryList(section string, list any) ([]any, error) {
	switch v := list.(type) {
	case nil:
		return nil, nil
	case []any:
		return v, nil
	case []map[string]any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("children %q: entries must be a list, got %s", section, describeValue(list))
	}
}

// childSetRangeError reports a parent declaration whose child count over the
// passed set breaks its repeat limit, naming the declaration and the parent
// template so the error is actionable at any nesting depth, or returns nil when
// the count is within range. max <= 0 means unbounded and min 0 means no
// minimum, matching SectionDecl
// (requirements.requirement.nested-section-validation).
func childSetRangeError(parent *Template, d SectionDecl, count int) error {
	if d.Max > 0 && count > d.Max {
		return fmt.Errorf("children: section %q on parent template %q has %d child(ren), maximum is %d",
			d.Name, parent.Name, count, d.Max)
	}
	if d.Min > 0 && count < d.Min {
		return fmt.Errorf("children: section %q on parent template %q has %d child(ren), minimum is %d",
			d.Name, parent.Name, count, d.Min)
	}
	return nil
}
