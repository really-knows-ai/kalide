package template

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
