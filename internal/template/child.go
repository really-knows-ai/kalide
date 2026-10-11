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
