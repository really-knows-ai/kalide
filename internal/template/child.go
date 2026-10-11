package template

import "fmt"

// This file is the home of the `child` and `children` layout constructors: the
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
