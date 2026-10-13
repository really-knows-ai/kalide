package template

import (
	"fmt"
	htmltemplate "html/template"
	"sort"
	"strings"
	"text/template/parse"
)

// This file implements Section, the composition, variant and controlled-effects
// engine: it resolves a template's declared sections, enforces their min/max
// repeat limits over the section instances an author declared, walks the
// section-template composition reference graph bounded at six heading levels
// (terminating safely on a reference cycle and reporting an over-deep chain),
// and resolves the layout variant each enum field selects.
//
// Composition (template-composition): a slide template declares named sections,
// each accepting one or more section-usage templates with min/max repeat limits;
// a section template declares sections of its own, so composition nests. Nesting
// depth is bounded by the content syntax — heading depth is nesting depth — so no
// composition chain of declared sections may exceed the six Markdown heading
// levels `#`…`######` (requirements.constraint.section-depth-limit). A layout's
// `section` helper calls are composition references too, so they join the same
// graph for reference-cycle rejection, but they are not headings and do not
// extend that heading-level chain. Sections never accept slide templates — the
// registry's build-time checks reject that, as they reject reference cycles;
// Walk here is what lets those checks terminate on a cyclic resolver instead of
// looping forever.
//
// Variants (template-variants): layout variants are selected only by enum
// fields (the field's value changes the layout). Variants reports those
// selections; there is deliberately no other variant mechanism and no
// author-facing variant key.
//
// Controlled effects (template-controlled-effects): fragments, transitions and
// backgrounds are set only by template layouts. No function or type here
// accepts them as author input, and CheckValues rejects a `transition:`,
// `background:` or `fragment:` key as an unknown field.
type Section struct {
	// resolve looks up a template by name. It is the same resolver CheckValues
	// takes, so a nil resolver yields no composition rather than a panic.
	resolve func(name string) (*Template, bool)
}

// NewSection returns a Section that resolves section templates through resolve.
// A nil resolve is allowed: composition walks then stop at the template in
// hand.
func NewSection(resolve func(name string) (*Template, bool)) *Section {
	return &Section{resolve: resolve}
}

// HelperCall is one `{{ section "name" [fields] [body] }}` invocation found in
// a template's layout: the target section-template name, the literal keys and
// body the call supplies, and markers recording which arguments are present and
// whether each is a literal value or an opaque expression. Section.HelperCalls
// extracts these records from a parsed layout; the registry's build-time checks
// consume them (requirements.requirement.section-helper-load-checks).
//
// Only a literal argument can be inspected at load time. A literal `dict`
// fields argument exposes the field names the call supplies, and a string-
// literal body exposes the body Markdown to parse. A non-literal expression — a
// variable, a `.raw`/`.item.parent` pass-through or a range value — is a
// permitted pass-through: it loads here and its keys and required fields are
// checked against the target's field and body rules when the target renders
// (field-rules).
type HelperCall struct {
	// Name is the target section-template name written as the first
	// argument, the `"name"` in `{{ section "name" … }}`.
	Name string

	// HasFields reports whether the call supplies a fields argument. It is
	// what distinguishes an omitted fields argument from a literal one that
	// supplies no keys.
	HasFields bool

	// FieldsLiteral reports whether the fields argument is a literal `dict`
	// call. When it is true FieldKeys holds that call's literal string keys;
	// when it is false the argument is either absent or a non-literal
	// expression whose keys cannot be known until render.
	FieldsLiteral bool

	// FieldKeys are the literal string keys of a literal `dict` fields
	// argument, in source order. It is nil when FieldsLiteral is false.
	FieldKeys []string

	// HasBody reports whether the call supplies a body argument. It is what
	// distinguishes an omitted body argument from a literal empty one.
	HasBody bool

	// BodyLiteral reports whether the body argument is a string literal.
	// When it is true Body holds that literal Markdown; when it is false the
	// argument is either absent or a non-literal expression that is checked
	// against the target's body rule when the target renders.
	BodyLiteral bool

	// Body is the literal body argument's Markdown, exactly as written in
	// the layout. It is empty when BodyLiteral is false.
	Body string

	// Children are the child-section arguments the call passes to its target
	// after fields and body, in source order: the values of its
	// `child`/`children` constructors as the extractor could surface them. It
	// is nil when the call passes no child arguments, or when the extractor
	// could not represent them. The registry's build-time checks inspect the
	// literal children at load and defer the rest, like a non-literal fields
	// or body expression, to render (requirements.requirement.
	// section-helper-load-checks).
	Children []HelperChild

	// Template is the name of the template whose layout declares this call.
	// It is carried so a build-time helper failure can be path-qualified to
	// that template's template.yaml.
	Template string

	// Line is the 1-based line of the call within the declaring layout's
	// source, or 0 when no position was resolved.
	Line int
}

// HelperChild is one child-section argument of a `section` helper call as it
// appears statically in the declaring layout: a `child "<section>" [fields]
// [body] [child…]` constructor or a `children "<section>" .data.<section>`
// forwarding, captured well enough for the load-time checks
// (requirements.requirement.section-helper-load-checks,
// requirements.requirement.child-constructor,
// requirements.requirement.children-forwarding).
//
// Only a literal child argument can be inspected at load. A literal string
// section name exposes the declared section the child fills, a literal `dict`
// fields argument exposes the entries the child supplies, and a string-literal
// body exposes the body Markdown to parse. A non-literal piece — a variable, a
// `.raw`/`.item.parent` pass-through or a range value — is a permitted
// pass-through: it loads here and its fields and body are checked against the
// target's declared sections when the target renders (field-rules, body-rules).
// A `children` forwarding of a runtime `.data` list is never inspected at load
// either.
//
// The struct mirrors HelperCall's representation of a call's fields and body:
// the literal markers (FieldsLiteral, BodyLiteral) record whether each argument
// was a literal the extractor could capture, while the presence markers
// (HasFields, HasBody) distinguish an omitted argument from a literal empty one.
// For a well-formed extracted child exactly one of Literal, NonLiteral and
// Forward is true.
type HelperChild struct {
	// Section is the declared section name the child fills — the literal
	// first argument, the `"<section>"` in `child "<section>" …`, naming a
	// slot in the helper target's declared `sections:`. It is empty when the
	// section argument is not a string literal, which NonLiteral records.
	Section string

	// HasFields reports whether the child supplies a fields argument. It
	// distinguishes an omitted fields argument from a literal one that
	// supplies no keys.
	HasFields bool

	// FieldsLiteral reports whether the fields argument is a literal `dict`
	// call all of whose entries the extractor could capture. When it is true
	// Fields holds that call's literal keys and values; when it is false the
	// argument is either absent or a non-literal expression whose entries
	// cannot be known until render.
	FieldsLiteral bool

	// Fields holds the literal keys and values of a literal `dict` fields
	// argument, exactly as the extractor sees them. It may carry the reserved
	// `template` selector key, which chooses the instantiated template; the
	// load-time check removes that key before it checks the rest, so it is
	// never counted as a field, exactly as for a resolved ChildRequest
	// (field-rules). It is nil when FieldsLiteral is false.
	Fields map[string]any

	// HasBody reports whether the child supplies a body argument. It
	// distinguishes an omitted body argument from a literal empty one, which
	// the target's body rule may treat differently.
	HasBody bool

	// BodyLiteral reports whether the body argument is a string literal. When
	// it is true Body holds that literal Markdown; when it is false the
	// argument is either absent or a non-literal expression that is checked
	// against the target's body rule when the target renders.
	BodyLiteral bool

	// Body is the literal body argument's Markdown, exactly as written in the
	// layout. It is empty when BodyLiteral is false.
	Body string

	// Children are the child's own nested `child` arguments, recursively, in
	// source order. It is nil when the child declares none. Each nested child
	// is a child argument in its own right and carries its own literal
	// markers, so a nested child that could not be captured statically does
	// not itself clear this child's Literal: the checks recurse.
	Children []HelperChild

	// Literal reports whether every piece THIS child argument carries is a
	// literal the load-time checks can inspect: a literal string section
	// name, and a literal (or omitted) fields `dict` and body. A literal
	// child is the shape the registry's build-time checks consume statically.
	// Nested children are not part of this flag — they are separate child
	// arguments, each with its own Literal (see Children) — so it is false
	// only for a `children` forwarding and for a child carrying a non-literal
	// section, fields or body piece.
	Literal bool

	// NonLiteral reports whether this is a `child` argument with at least one
	// non-literal piece, so it cannot be fully inspected at load and is a
	// permitted pass-through checked when the target renders. It is false for
	// a literal child and for a `children` forwarding, which Forward marks
	// instead.
	NonLiteral bool

	// Forward reports whether this child is a `children "<section>"
	// .data.<section>` forwarding of a runtime `.data` list rather than a
	// literal `child`. Such a child is never inspected at load: its entries
	// carry their own templates, fields, bodies and nested data and are
	// re-instantiated when the target renders (children-forwarding).
	Forward bool
}

// Declarations returns tmpl's declared sections keyed by section name, each
// entry a SectionDecl carrying the accepted section templates and the min/max
// repeat limits. It is the name → SectionDecl resolution the composition engine
// and the registry consume. The map is a fresh allocation; the declarations
// themselves are immutable value copies.
func (s *Section) Declarations(tmpl *Template) map[string]SectionDecl {
	if tmpl == nil {
		return nil
	}
	out := make(map[string]SectionDecl, len(tmpl.Sections))
	for i := range tmpl.Sections {
		out[tmpl.Sections[i].Name] = tmpl.Sections[i]
	}
	return out
}

// Declared returns the declaration for one named section of tmpl. ok is false
// when tmpl is nil or declares no such section.
func (s *Section) Declared(tmpl *Template, name string) (SectionDecl, bool) {
	if tmpl == nil {
		return SectionDecl{}, false
	}
	for i := range tmpl.Sections {
		if tmpl.Sections[i].Name == name {
			return tmpl.Sections[i], true
		}
	}
	return SectionDecl{}, false
}

// CheckRepeats enforces each of tmpl's declared section min/max repeat limits
// over the declared section instances whose names are listed in source order.
//
// A count is the number of instances of a declared section. Min 0 means no
// minimum; Max <= 0 means unbounded. Instances whose name tmpl does not declare
// are ignored here: an undeclared section is the slide parser's error, not a
// repeat-limit one. One ValueError per excess instance is produced, and a
// single one when a minimum is unmet, in section declaration order — the same
// positioned, structured shape CheckValues reports, so the phase-5 formatter
// carries them unchanged. Option values (WithLineSource) supply the line of an
// offending section instance.
func (s *Section) CheckRepeats(tmpl *Template, names []string, opts ...Option) []ValueError {
	if tmpl == nil {
		return nil
	}
	var cfg checkConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	line := func(path []PathSegment) int {
		if cfg.lineFor == nil {
			return 0
		}
		return cfg.lineFor(path)
	}

	declared := make(map[string]struct{}, len(tmpl.Sections))
	for i := range tmpl.Sections {
		declared[tmpl.Sections[i].Name] = struct{}{}
	}
	counts := make(map[string]int, len(tmpl.Sections))
	for _, name := range names {
		if _, ok := declared[name]; ok {
			counts[name]++
		}
	}

	var errs []ValueError
	for i := range tmpl.Sections {
		d := &tmpl.Sections[i]
		n := counts[d.Name]
		if d.Max > 0 && n > d.Max {
			for extra := d.Max; extra < n; extra++ {
				p := appendSeg(nil, PathSegment{Name: d.Name, Index: extra, HasIndex: true})
				errs = append(errs, ValueError{
					Path:  p,
					Line:  line(p),
					Rule:  "max_sections",
					Value: n,
					What:  fmt.Sprintf("max_sections: section %q has %d instance(s), maximum is %d", d.Name, n, d.Max),
					Fix:   fmt.Sprintf("use at most %d %q section(s)", d.Max, d.Name),
				})
			}
		}
		if d.Min > 0 && n < d.Min {
			p := appendSeg(nil, PathSegment{Name: d.Name})
			errs = append(errs, ValueError{
				Path:  p,
				Line:  line(p),
				Rule:  "min_sections",
				Value: n,
				What:  fmt.Sprintf("min_sections: section %q has %d instance(s), minimum is %d", d.Name, n, d.Min),
				Fix:   fmt.Sprintf("add at least %d %q section(s)", d.Min-n, d.Name),
			})
		}
	}
	return errs
}

// SectionCycleError reports that section templates reference one another in a
// cycle. Path is the composition chain from the root template to the repeated
// name, ending with that name, as plain template names, for example
// `content → columns → content`. Error renders it section-qualified, so the
// cycle reads `sections/content → sections/columns → sections/content`
// (requirements.constraint.section-cycle-error-chain).
//
// A cyclic resolver can never be walked to completion; Section.Walk reports
// this instead of looping forever. The registry rejects such a cycle at build
// time (template-build-checks).
type SectionCycleError struct {
	// Path is the chain of template names that closes the cycle, root first
	// and the repeated name last. Its entries are plain template names; the
	// `sections/` prefix is added only by Error.
	Path []string
}

// Error renders the cycle as a section-qualified chain: each plain template
// name in Path is written with the `sections/` directory prefix section
// templates live in, so a path of plain names `a → b → a` reads
// `sections/a → sections/b → sections/a`
// (requirements.constraint.section-cycle-error-chain).
func (e *SectionCycleError) Error() string {
	parts := make([]string, len(e.Path))
	for i, name := range e.Path {
		parts[i] = SectionsDir + "/" + name
	}
	return "section template reference cycle: " + strings.Join(parts, " → ")
}

// sectionDepthLimit is the greatest permitted composition depth, in heading
// levels: `#` opens a slide, `##` its first section, and so on through the last
// Markdown heading level `######`. It is the syntax bound, not a setting
// (requirements.constraint.section-depth-limit).
const sectionDepthLimit = 6

// SectionDepthError reports that a section-template composition chain is deeper
// than the six heading levels the content syntax allows
// (requirements.constraint.section-depth-limit): heading depth is nesting
// depth, so no composition chain of templates may exceed six levels.
//
// Path is the over-deep chain of template names from the root template to the
// template that crosses the bound. Limit is that bound, in heading levels. The
// chain may come from a declaration chain or from a bounded self-nesting walk
// (walkBoundedSelfNesting) whose max_depth allowed more levels than the syntax;
// MaxDepth carries the bound that produced the latter.
type SectionDepthError struct {
	// Path is the composition chain that exceeded the bound, the root
	// template first and the template that crosses the limit last.
	Path []string

	// Limit is the maximum permitted composition depth, in heading levels.
	Limit int

	// MaxDepth is the `max_depth` bound of the declaration that stopped the
	// bounded self-nesting walk at this chain, or 0 when the over-deep chain
	// was not bounded by a max_depth declaration (an ordinary declared-section
	// chain). It is the bound the depth-limit error names
	// (requirements.constraint.section-depth-limit); rendering it is
	// plan.phase-07.task-5's scope — this field is the data that task consumes.
	MaxDepth int
}

// Error renders the over-deep composition chain and the heading-level bound as
// a plain-language templates error: how deep the chain is, the six-heading-
// level limit it breaks, the chain itself, and a hint to nest fewer section
// templates or flatten the composition.
func (e *SectionDepthError) Error() string {
	limit := e.Limit
	if limit <= 0 {
		limit = sectionDepthLimit
	}
	return fmt.Sprintf(
		"section template composition is %d levels deep, exceeding the %d-heading-level limit: %s — nest fewer section templates or flatten the composition",
		len(e.Path), limit, strings.Join(e.Path, " → "))
}

// Walk returns every distinct section template reachable from tmpl through the
// composition reference graph, in depth-first declaration order: the targets
// of tmpl's declared sections first, then the targets of its `section` helper
// calls (HelperRefs). It descends every path — a template reachable at two
// depths is examined at both, so a shallow reach never masks a deeper chain —
// and stops a declared-section chain at the six-heading-level bound
// (requirements.constraint.section-depth-limit).
//
// The two reference kinds are deliberately split, not conflated:
//
//   - A declared-section acceptance is a heading: descending it nests the
//     child template one heading level deeper, exactly as before, so these are
//     the edges the six-heading-level bound counts.
//   - A `section` helper call is a composition reference but NOT a heading
//     (requirements.constraint.section-depth-limit). It joins the graph so a
//     helper-closed reference cycle is rejected with the full chain in
//     *SectionCycleError (requirements.constraint.section-cycle-error-chain),
//     but descending it does not extend the depth count — the target sits at
//     the caller's heading level, so only its own declared sections add a
//     level.
//
// A name already on the current descent path is a reference cycle. A
// declared-section self-nesting or cyclic acceptance whose own declarations
// carry max_depth is a bounded self-nesting
// (requirements.requirement.bounded-self-nesting), so it is accepted and
// followed to the longest chain its bounds allow (walkBoundedSelfNesting)
// instead of being reported; that full bounded chain is then measured against
// the six-heading-level bound
// (requirements.constraint.section-depth-limit), and an over-deep one is
// reported as *SectionDepthError carrying the chain and the max_depth that
// produced it. A cycle with no max_depth on it — including a helper- or
// child-closed cycle, which carries no heading bound — is reported as
// *SectionCycleError with the full chain (never an infinite loop)
// (requirements.constraint.section-cycle-error-chain). A name already returned
// through another branch is included in the result once. A chain of
// declared-section edges deeper than six heading levels is reported as
// *SectionDepthError with the over-deep chain. A referenced name the resolver
// does not define is an error. Whether a section accepts a slide-usage
// template is checked by the registry's build-time checks, not here.
//
// A layout whose helper calls cannot be read statically — a non-literal
// target name, or source that does not parse with the canonical layout func
// map — makes HelperRefs fail; Walk surfaces that error unchanged. It already
// names the declaring template and layout line, and Registry.Validate passes
// it through unchanged rather than adding a second `template "name":` prefix,
// so errors.As for *SectionCycleError and *SectionDepthError (and for the
// HelperCalls error itself) stays intact.
func (s *Section) Walk(tmpl *Template) ([]*Template, error) {
	if tmpl == nil {
		return nil, nil
	}
	if s == nil || s.resolve == nil {
		return nil, nil
	}

	var out []*Template
	emitted := make(map[string]bool)
	onPath := make(map[string]bool)

	var walk func(t *Template, depth int, path []string) error

	// descend follows one reference edge: resolve the target, record it once,
	// then walk it — unless the name is already on the path (a reference
	// cycle) and the edge is a declared-section acceptance on a bounded
	// self-nesting, which is accepted to the longest chain its max_depth
	// bounds allow instead of reported. depth is the target's heading level,
	// so a declared-section edge passes depth+1 (it is a heading) while a
	// helper edge passes depth unchanged (it is not). closing is the
	// declaration whose accepted section this edge descends, or nil for a
	// helper/child edge, which is not a heading and carries no max_depth.
	descend := func(name string, depth int, path []string, closing *SectionDecl) error {
		if onPath[name] {
			if closing != nil {
				// The acceptance recurs to a template already on the path: a
				// declared-section reference cycle. When the declarations on
				// the cycle carry max_depth it is a bounded self-nesting, so
				// follow it to the longest chain its bounds allow rather than
				// report the cycle; the returned chain is the full composition
				// chain from the root through the bound, which is then measured
				// against the six-heading-level bound.
				//
				// ancestors is the composition prefix above the repeated name,
				// root first, excluding the repeated name itself — the shape
				// walkBoundedSelfNesting expects.
				idx := -1
				for i := len(path) - 1; i >= 0; i-- {
					if path[i] == name {
						idx = i
						break
					}
				}
				var ancestors []string
				if idx > 0 {
					ancestors = path[:idx]
				}
				start, ok := s.resolve(name)
				if !ok || start == nil {
					return fmt.Errorf("section template %q is not defined", name)
				}
				chain, bounded, maxDepth, err := walkBoundedSelfNesting(s.resolve, start, ancestors, closing)
				if err != nil {
					return err
				}
				if bounded {
					if len(chain) > sectionDepthLimit {
						return &SectionDepthError{Path: chain, Limit: sectionDepthLimit, MaxDepth: maxDepth}
					}
					// Within the bound: the bounded walk already accounted for
					// the depth, so do not recurse the cycle again. The target
					// is reported once, like any other reachable template.
					if !emitted[name] {
						emitted[name] = true
						out = append(out, start)
					}
					return nil
				}
			}
			cycle := make([]string, len(path), len(path)+1)
			copy(cycle, path)
			return &SectionCycleError{Path: append(cycle, name)}
		}
		nested, ok := s.resolve(name)
		if !ok || nested == nil {
			return fmt.Errorf("section template %q is not defined", name)
		}
		if !emitted[name] {
			emitted[name] = true
			out = append(out, nested)
		}
		// Recurse even into an already-emitted template: a deeper path
		// through it must still be counted against the bound.
		return walk(nested, depth, path)
	}

	walk = func(t *Template, depth int, path []string) error {
		if t == nil {
			return nil
		}
		onPath[t.Name] = true
		path = append(path, t.Name)
		defer delete(onPath, t.Name)

		if depth > sectionDepthLimit {
			chain := make([]string, len(path))
			copy(chain, path)
			return &SectionDepthError{Path: chain, Limit: sectionDepthLimit}
		}

		// Declared-section acceptances are headings: one level deeper.
		for i := range t.Sections {
			d := &t.Sections[i]
			for _, name := range d.Accepted {
				if err := descend(name, depth+1, path, d); err != nil {
					return err
				}
			}
		}

		// Section-helper calls are references, not headings: same level.
		refs, err := s.HelperRefs(t)
		if err != nil {
			return err
		}
		for _, name := range refs {
			if err := descend(name, depth, path, nil); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(tmpl, 1, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// walkBoundedSelfNesting walks a declared-section self-nesting or cyclic
// acceptance to the longest composition chain its max_depth declarations allow,
// so the walk stops at a bound instead of reporting a reference cycle
// (requirements.requirement.bounded-self-nesting,
// requirements.constraint.section-cycle-error-chain).
//
// Section.Walk calls it when a declared-section acceptance names a template
// already on the composition path — the reference cycle it would otherwise
// report as *SectionCycleError. The cycle is bounded when a declaration on it
// carries max_depth: N, and the walk then follows the cycle to the longest chain
// its bounds allow, a composition prefix plus one heading per bounded level.
//
// resolve is the same name → *Template lookup Section.Walk holds. start is the
// template the acceptance recurs to (the resolved template of the section name
// already on the path), and ancestors is the composition chain above it, root
// first, as resolved template names — start's own name is NOT included (the
// caller passes the path up to but excluding the first occurrence of the
// repeated name). The returned chain is the full chain the walk produces:
// ancestors, start, then one heading per bounded level. closing is the
// declaration whose accepted section closed the cycle, the edge from the last
// ancestor to start; it is part of the cycle, so its max_depth counts toward
// boundedness. A nil closing leaves that edge unbounded.
//
// Only declared-section acceptances are walked: a `section` helper call is a
// composition reference but not a heading and carries no max_depth, so a
// helper-closed cycle is not bounded here and stays *SectionCycleError in Walk.
// Each declaration with max_depth N bounds the instances it produces — an
// instance resolving to the accepted template has at most N enclosing instances
// of that same template, so its .item.depth is at most N — and the walk stops
// adding that template once it already appears N+1 times on the chain. When
// several declarations on one cycle carry max_depth, each enforces its own bound
// independently and the walk stops at the first bound the chain reaches.
//
// bounded reports whether a max_depth declaration stopped the walk. When it is
// true, chain is the longest bounded chain and maxDepth is the bound that
// stopped it, for the depth-limit error's max_depth clause
// (requirements.constraint.section-depth-limit). When it is false the cycle
// carries no max_depth and is an unbounded reference cycle: chain is nil and
// Section.Walk must report it as *SectionCycleError. A referenced name the
// resolver does not define is an error, exactly as Section.Walk reports one; a
// nil start or resolver yields no chain and no error.
func walkBoundedSelfNesting(resolve func(name string) (*Template, bool), start *Template, ancestors []string, closing *SectionDecl) (chain []string, bounded bool, maxDepth int, err error) {
	if start == nil || resolve == nil {
		return nil, false, 0, nil
	}

	// counts[name] is how many times a resolved template name already appears on
	// the chain: the enclosing-instance count (.item.depth) a declaration's
	// max_depth bounds.
	counts := make(map[string]int, len(ancestors)+1)
	for _, name := range ancestors {
		counts[name]++
	}
	chain = make([]string, 0, len(ancestors)+1)
	chain = append(chain, ancestors...)
	chain = append(chain, start.Name)
	counts[start.Name]++

	// decls[i] is the declaration whose accepted section led to chain[i] (nil for
	// the composition prefix), so a revisited name can inspect the declarations
	// on the cycle it closes and decide whether a max_depth bounds it.
	decls := make([]*SectionDecl, len(chain))
	decls[len(ancestors)] = closing

	longest := append([]string(nil), chain...)
	longestMaxDepth := 0
	bounded = false
	unbounded := false
	var walkErr error

	onChain := func(name string) int {
		for i, n := range chain {
			if n == name {
				return i
			}
		}
		return -1
	}

	var walk func(t *Template)
	walk = func(t *Template) {
		if unbounded || walkErr != nil || t == nil {
			return
		}
		for i := range t.Sections {
			d := &t.Sections[i]
			for _, accepted := range d.Accepted {
				nested, ok := resolve(accepted)
				if !ok || nested == nil {
					walkErr = fmt.Errorf("section template %q is not defined", accepted)
					return
				}
				// The declaration's bound: an instance it produces resolves to
				// the accepted template and has counts[nested.Name] enclosing
				// instances of that same template, so its .item.depth must not
				// exceed max_depth.
				if d.MaxDepth > 0 && counts[nested.Name] > d.MaxDepth {
					bounded = true
					if len(chain) > len(longest) {
						longest = append([]string(nil), chain...)
						longestMaxDepth = d.MaxDepth
					}
					continue
				}
				if idx := onChain(nested.Name); idx >= 0 {
					// The edge closes a cycle. It is bounded only when a
					// declaration on the cycle carries max_depth: the closing
					// edge d plus every edge after the first occurrence at idx.
					// Otherwise the walk could never terminate, so the cycle is
					// an unbounded reference cycle.
					cycleBounded := d.MaxDepth > 0
					for j := idx + 1; j < len(decls) && !cycleBounded; j++ {
						if decls[j] != nil && decls[j].MaxDepth > 0 {
							cycleBounded = true
						}
					}
					if !cycleBounded {
						unbounded = true
						return
					}
				}
				counts[nested.Name]++
				chain = append(chain, nested.Name)
				decls = append(decls, d)
				walk(nested)
				decls = decls[:len(decls)-1]
				chain = chain[:len(chain)-1]
				counts[nested.Name]--
				if unbounded || walkErr != nil {
					return
				}
			}
		}
		if len(chain) > len(longest) {
			longest = append([]string(nil), chain...)
		}
	}

	walk(start)
	if walkErr != nil {
		return nil, false, 0, walkErr
	}
	if unbounded {
		return nil, false, 0, nil
	}
	return longest, bounded, longestMaxDepth, nil
}

// Variant is one enum field's layout-variant selection: the field that selects
// the variant and the effective variant value.
type Variant struct {
	// Field is the enum field's name.
	Field string

	// Value is the effective variant: the author's value when the field is
	// present, otherwise the field's Default when it is a string, otherwise "".
	Value string
}

// Variants returns the layout variant each enum field of tmpl selects for the
// author's values, in field declaration order. Enum fields are the only variant
// mechanism, so every other field type is ignored, and no variant is accepted
// from anywhere else.
//
// It only resolves the selection; whether a value is one of Field.Variants is
// CheckValues' enum rule, so a non-string or unknown value is passed through
// here rather than re-validated.
func (s *Section) Variants(tmpl *Template, values map[string]any) []Variant {
	if tmpl == nil {
		return nil
	}
	var out []Variant
	for i := range tmpl.Fields {
		f := &tmpl.Fields[i]
		if f.Type != FieldEnum {
			continue
		}
		value := ""
		if raw, ok := values[f.Name]; ok {
			if str, ok := raw.(string); ok {
				value = str
			}
		} else if def, ok := f.Default.(string); ok {
			value = def
		}
		out = append(out, Variant{Field: f.Name, Value: value})
	}
	return out
}

// HelperCalls statically extracts every `{{ section "name" [fields] [body] }}`
// invocation from tmpl's layout, in source order.
//
// Registry templates carry only their layout source text (Template.Layout.Text)
// and no stored parse tree, so HelperCalls re-parses that text with the
// canonical layout func map (LayoutFuncMap) — the same map the library loader's
// step-3 layout parse and the renderer's parseLayouts use — so a layout that
// calls `media`, `section`, `dict` or `list` parses here too. It walks the
// text/template/parse trees of the layout and of every `{{ define }}` /
// `{{ block }}` body it contains (parsed.Templates), descending through
// `if`/`range`/`with` branches, so a call anywhere in the layout is found.
//
// The arguments after the target name are positional: the second argument is
// the optional `fields` expression and the third the optional `body`
// expression, matching the `[fields] [body]` grammar. `fields` is a literal
// dict call when it is a parenthesized `(dict "key" value …)` pipeline all of
// whose key arguments are string literals; FieldsLiteral is then true and
// FieldKeys holds those keys in source order. Otherwise — a variable, a
// `.raw`/`.item.parent` pass-through or a range value, or a dict with a
// non-literal key — it is a non-literal pass-through: FieldsLiteral is false
// and FieldKeys is nil, to be checked against the target's field rules at
// render. `body` is a string literal only when the third argument is a quoted
// string; BodyLiteral is then true and Body holds its text. A non-literal
// fields or body expression is NOT an error here: pass-through is intended and
// is validated when the target renders (section-helper-load-checks).
//
// Every argument after the body (position four onward) is a child argument: the
// value of a `child`/`children` constructor, or any other expression the call
// passes as a child. HelperCall.Children records each one statically, in source
// order, as a HelperChild: a `child` all of whose pieces are literal is Literal,
// a `child` with a non-literal piece is NonLiteral with its literal pieces still
// captured, a `children` forwarding is Forward, and any other expression is
// NonLiteral. Like the fields and body marks, a non-literal child is a permitted
// pass-through checked when the target renders (child-constructor,
// children-forwarding, section-helper-load-checks). A child argument never sits
// in the fields or body slot, so it is never read as one.
//
// The target name, by contrast, must be a string literal: a load check resolves
// the target template statically by name, so a call whose name is not a literal
// (for example `{{ section .name }}`) cannot be checked and is rejected with an
// error naming the layout and line. A layout that does not parse with the
// canonical func map is likewise an error. HelperCall.Template is tmpl.Name and
// HelperCall.Line is the call's 1-based line in the layout source (0 only when
// no position could be resolved).
func (s *Section) HelperCalls(tmpl *Template) ([]HelperCall, error) {
	if tmpl == nil || strings.TrimSpace(tmpl.Layout.Text) == "" {
		return nil, nil
	}
	text := tmpl.Layout.Text
	layoutName := tmpl.Layout.Name
	if layoutName == "" {
		layoutName = tmpl.Name
	}
	// The layout is parsed alone here, so its name is the only namespace key a
	// Go error prefix can carry; map it to the layout's own on-disk path.
	resolve := namespaceLayoutResolver(layoutName, tmpl.Layout.Path)
	parsed, err := htmltemplate.New(layoutName).Funcs(LayoutFuncMap(nil, "")).Parse(text)
	if err != nil {
		return nil, layoutTemplateError(tmpl.Layout, resolve, fmt.Sprintf("template %q: parse layout", tmpl.Name), err)
	}

	var found []positionedCall
	seen := make(map[*parse.Tree]bool)
	for _, t := range parsed.Templates() {
		if t == nil || t.Tree == nil || seen[t.Tree] {
			continue
		}
		seen[t.Tree] = true
		if err := s.walkHelperList(t.Tree.Root, tmpl.Name, text, &found); err != nil {
			return nil, err
		}
	}

	sort.SliceStable(found, func(i, j int) bool { return found[i].pos < found[j].pos })
	calls := make([]HelperCall, len(found))
	for i := range found {
		calls[i] = found[i].call
	}
	return calls, nil
}

// HelperRefs returns the section-template names tmpl's layout references
// through the `section` helper, in source order, as the helper-call edges of
// the template composition reference graph. Section.Walk joins these edges to
// the declared-section edges so a helper-closed reference cycle is rejected
// with the chain in *SectionCycleError (template-composition,
// requirements.constraint.section-cycle-error-chain).
//
// It is built on HelperCalls: every extracted call contributes its target
// Name, and — because a `child` constructor's instantiated template is itself a
// reference (requirements.requirement.template-composition) — every call also
// contributes the template its literal `child` arguments instantiate and, for a
// `children` forwarding, the accepted templates of the forwarded declared
// section, nested child arguments included. A child whose section or template
// cannot be resolved statically (a non-literal child, or an unknown target)
// contributes no child edge; the load-time check reports that separately. A
// call anywhere in the layout — the root, a `define`/`block` body or a
// control-action branch — is a reference. A non-literal target name is the
// error HelperCalls reports, and a layout that does not parse with the
// canonical layout func map is likewise an error.
//
// Repeated names are de-duplicated, keeping the first occurrence's position, so
// the result is the edge set of the reference graph rather than its call list:
// two calls to the same target — or a call target and a child that both
// instantiate the same template — are one edge for cycle rejection. That is
// deliberate — the per-call detail (literal dict keys, literal body, child
// arguments and the literal/non-literal markers) stays available through
// HelperCalls, which the registry's load-time checks consume; HelperRefs exists
// only to feed the reference-graph walk.
func (s *Section) HelperRefs(tmpl *Template) ([]string, error) {
	calls, err := s.HelperCalls(tmpl)
	if err != nil {
		return nil, err
	}
	if len(calls) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(calls))
	seen := make(map[string]bool, len(calls))
	for i := range calls {
		call := &calls[i]
		appendRef(seen, &out, call.Name)
		s.childRefs(call.Name, call.Children, seen, &out)
	}
	return out, nil
}

// childRefs appends the section-template names the child arguments of a call on
// targetName reference — the template each literal `child` instantiates, the
// accepted templates of a forwarded `children` declared section, and the same
// for their nested children — to out, de-duplicating through seen. It resolves
// targetName through s.resolve, so a nil resolver (a Section built without one)
// or an unknown target contributes no child edge. expanded guards the recursion
// so a child chain that closes a cycle terminates here: Walk, not HelperRefs,
// is what reports the cycle.
func (s *Section) childRefs(targetName string, children []HelperChild, seen map[string]bool, out *[]string) {
	if len(children) == 0 || s == nil || s.resolve == nil {
		return
	}
	target, ok := s.resolve(targetName)
	if !ok || target == nil {
		return
	}
	expanded := map[string]bool{targetName: true}
	s.childRefsFor(target, children, seen, expanded, out)
}

// childRefsFor walks one level of child arguments against parent and appends
// every template name they reference, recursing into a literal child's nested
// children with that child's instantiated template as the new parent. expanded
// records templates already walked so a cycle among child references terminates
// here (Walk reports it); a template reached twice contributes its edges once.
func (s *Section) childRefsFor(parent *Template, children []HelperChild, seen, expanded map[string]bool, out *[]string) {
	for i := range children {
		c := &children[i]
		if c.Forward {
			if c.Section == "" {
				continue
			}
			if decl, ok := declaredSection(parent, c.Section); ok {
				for _, name := range decl.Accepted {
					appendRef(seen, out, name)
				}
			}
			continue
		}
		if !c.Literal {
			continue
		}
		decl, ok := declaredSection(parent, c.Section)
		if !ok {
			continue
		}
		name := instantiatedChildRefName(decl, c)
		if name == "" {
			for _, accepted := range decl.Accepted {
				appendRef(seen, out, accepted)
			}
			continue
		}
		appendRef(seen, out, name)
		if expanded[name] {
			continue
		}
		expanded[name] = true
		if childTmpl, ok := s.resolve(name); ok && childTmpl != nil {
			s.childRefsFor(childTmpl, c.Children, seen, expanded, out)
		}
	}
}

// instantiatedChildRefName returns the template a literal child argument
// instantiates, or "" when the declaration accepts more than one template and
// the child carries no usable `template` selector (or accepts none). The
// selector is the reserved `template` key of the child's literal fields; when
// it is absent the single accepted template is instantiated.
func instantiatedChildRefName(decl SectionDecl, c *HelperChild) string {
	if c.Fields != nil {
		if raw, present := c.Fields["template"]; present {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	if len(decl.Accepted) == 1 {
		return decl.Accepted[0]
	}
	return ""
}

// appendRef adds name to *out unless it is empty or already in seen, keeping
// first-occurrence order.
func appendRef(seen map[string]bool, out *[]string, name string) {
	if name == "" || seen[name] {
		return
	}
	seen[name] = true
	*out = append(*out, name)
}

// SectionCall is the resolved runtime execution input of one `section` helper
// invocation: the target template a `{{ section "<name>" [fields] [body] }}`
// call resolves to, the effective field values the target executes with, the
// validated body, the resolved child section arguments the call passes to the
// target, and the one-item `.item` descriptor it is rendered under. It is what
// ResolveSectionCall builds.
//
// SectionCall is the runtime counterpart of HelperCall: HelperCall is the
// static, load-time record of a call extracted from a layout's text (its target
// name, literal keys/body and literal/non-literal markers), while SectionCall is
// the resolved, ready-to-execute invocation. Both the load-time example
// execution (exampleSectionHelper) and the phase-03 renderer reuse
// ResolveSectionCall so resolution, defaults and validation have one
// implementation rather than one per caller.
type SectionCall struct {
	// Template is the resolved target, always a section-usage template.
	Template *Template

	// Layer is the LibraryLayer that owns Template, so a caller that executes
	// the target resolves the target's own `media` and `theme:<name>/`
	// references in that layer's root rather than the merged library's
	// (library-layer-references). It is populated by ResolveSectionCall from
	// the resolver's provenance.
	//
	// It is nil when the registry/library records no provenance for the
	// resolved target — a single-root library, or a resolver built without
	// layer information. Consumers must treat nil as "use the
	// primary/merged root": the primary layer is the one served at the
	// unprefixed /assets/templates/... URLs — the sole layer of a
	// single-root library, or the deck-root local templates/ layer
	// (LayerRoleLocal) of a two-layer deck; the external `templates:` layer
	// of a two-layer deck is never the primary (mergeLibraryLayers).
	Layer *LibraryLayer

	// Values is the effective field data the target executes with: the call's
	// supplied fields with the target's field defaults applied for every
	// declared field the call left out. It is the map CheckValues validated.
	Values map[string]any

	// Source is the call's authored source field values BEFORE the target's
	// declared defaults were applied — the caller-supplied fields, with the
	// reserved `template` selector removed for a child argument (ResolveChildCall)
	// — so a default materialised into Values is never mistaken for an authored
	// source value. It is the map the reserved `.raw` source view is built from
	// (RawContext), and is nil or empty when the call supplied no fields
	// (raw-source-context). ResolveSectionCallLayered sets it to the call's
	// supplied fields and ResolveChildCall to the stripped child fields;
	// ResolvedChild.Source carries it out to a consumer building a child's
	// `.raw`.
	Source map[string]any

	// Body is the validated body the call supplies, exactly as written; it is
	// "" when the call supplies none.
	Body string

	// Children are the resolved child section instances the call passes to the
	// target, in passed order. Each entry is the child's own resolved instance
	// together with that child's own recursively resolved children, the tree
	// shape ResolvedChild carries. It is nil when the call passes no child
	// sections.
	Children []*ResolvedChild

	// Item is the one-item `.item` descriptor the target executes under
	// (item-context): index 0, number and count 1, first and last true,
	// section and template the target name, depth the target's self-nesting
	// depth (InstanceDepth of the enclosing ancestors' resolved template names
	// against the target name), and parent the caller's fields (nil from a
	// slide layout). The helper call is the sole instance of its own one-item
	// group: the children the call passes are the target's own child instances
	// (Children) and each carries its own sibling descriptor, so they never
	// enter this one.
	Item map[string]any
}

// ResolveSectionCall resolves a `{{ section "<name>" [fields] [body] }}` call's
// target against resolve and builds the runtime execution input its layout is
// executed with, or reports why the call cannot be executed.
//
// resolve is the same name → *Template lookup CheckValues and NewSection take,
// so *Registry.Lookup and the library loader's resolver both satisfy it; a nil
// resolver is an error, not a panic.
//
// The target must resolve to a defined section-usage template: an unknown name
// or a slide-usage template is rejected, because sections never accept slide
// templates.
//
// Field handling materialises the target's declared defaults FIRST and then
// validates the effective map with CheckValues:
//
//   - applyFieldDefaults copies the call's supplied fields and fills in every
//     declared field the call leaves out with its Field.Default when it has one,
//     so the target executes with the values the renderer would give it;
//   - CheckValues reports a key the target does not declare or a supplied value
//     that breaks a field rule.
//
// The defaulted values are carried for execution, not to satisfy the required
// check: CheckValues independently agrees that an omitted required field
// carrying a default is satisfied (it is schema-only and never needs the
// default), so only an undefaulted required field is still reported missing.
// This matches the registry's build-time helper check (section-helper-load-checks),
// which likewise requires each required field to be supplied or carry a default.
//
// The body is checked against the target's body rule with CheckBody, and a body
// whose heading names a section the target declares is rejected even when the
// body rule would allow a subheading: a helper body carries no child sections,
// so children pass only through child arguments, never through the body.
//
// The trailing children are the call's child arguments — the values of its
// `child`/`children` constructors, in passed order. When non-empty (and also
// when empty, so a target declaring a mandatory child section the call leaves
// unfilled is still rejected) they are resolved with ResolveChildren against the
// target's declared sections and the effective values, with the target as the
// parent template: each child is checked against the accepted template, its
// fields/body/defaults are resolved exactly like an authored instance, and the
// declaration's min/max is enforced over the full passed set. The resolved tree
// is carried on SectionCall.Children.
//
// The returned *SectionCall carries the resolved *Template, the effective
// values, the body, the resolved children and the one-item .item descriptor. A
// violation is returned as the first ValueError CheckValues or CheckBody reports
// or the first ResolveChildren error, so a caller adapts it through the same
// positioned path as any other field or body error.
//
// ResolveSectionCall records no layer provenance: it delegates to
// ResolveSectionCallLayered with a nil layerOf, so the returned
// SectionCall.Layer is always nil. A caller that holds a layer source — the
// render and load-time example section helpers, whose merged registry answers
// *Registry.LayerOf — calls ResolveSectionCallLayered directly so the resolved
// target's owning layer is carried out on SectionCall.Layer
// (library-layer-references).
//
// ancestors is the enclosing instances' chain of resolved template names above
// the calling instance, forwarded unchanged to ResolveSectionCallLayered: the
// one-item descriptor's `depth` is InstanceDepth(ancestors, name), so a call
// from a slide layout, or any call whose ancestors carry no template equal to
// the target name, has depth 0, while a call from a section instance whose
// enclosing chain repeats the target name counts it (item-context,
// bounded-self-nesting). It is the same ancestor convention
// ResolveSectionCallLayered and DataContext take; a caller without a chain
// passes nil.
func ResolveSectionCall(resolve func(name string) (*Template, bool), name string, fields map[string]any, body string, ancestors []string, parent map[string]any, children ...*ChildRequest) (*SectionCall, error) {
	return ResolveSectionCallLayered(resolve, nil, name, fields, body, ancestors, parent, children...)
}

// ResolveSectionCallLayered is ResolveSectionCall plus layer provenance: it
// applies exactly the same resolution, defaults-first field validation, body
// checks, child resolution and error contract (see ResolveSectionCall) and
// additionally records the owning layer of the resolved target on
// SectionCall.Layer.
//
// ancestors is the enclosing instances' chain of resolved template names above
// the target — empty at the top level, and including the calling instance when
// the call is made from a section instance — carried so the one-item
// descriptor's depth counts the target's enclosing same-template ancestors
// through the shared InstanceDepth helper: a call from a slide layout, or any
// call whose ancestors carry no template equal to the target name, has depth 0
// (item-context). It is the same ancestor convention DataContext takes. The
// target's own children are resolved with the target instance's chain — this
// chain with the target name appended — so each child's `.item.depth` is
// computed against its full enclosing chain (item-context,
// bounded-self-nesting).
//
// The trailing children are resolved with the same resolve/layerOf pair, so a
// child naming a template that lives in the other library layer carries that
// layer on its own SectionCall.Layer (ResolveChildCall records layerOf of the
// instantiated name), independently of the target's layer
// (library-layer-references).
//
// resolve is consulted first; only once it has returned a section-usage target
// is layerOf consulted, so a rejected name or non-section target reports the
// same error and never touches layerOf. layerOf reports the *LibraryLayer that
// contributed the template registered under the resolved name, or nil when the
// caller has no provenance for it — *Registry.LayerOf satisfies it for a
// registry built from a merged Library (NewRegistryFromLibrary, via setLayer),
// which is how a call resolves through the merged section namespace and still
// learns which layer owns the target. The name passed to layerOf is the
// resolved target's name, so a caller identifies the target's own layer rather
// than the calling template's.
//
// A nil layerOf records no layer (SectionCall.Layer stays nil), which is
// ResolveSectionCall's behaviour; layerOf may likewise report nil for a name
// whose owning layer is unknown (a single-root library, or a registry built in
// code). Consumers treat a nil Layer as "resolve against the primary/merged
// root" (SectionCall.Layer).
func ResolveSectionCallLayered(resolve func(name string) (*Template, bool), layerOf func(name string) *LibraryLayer, name string, fields map[string]any, body string, ancestors []string, parent map[string]any, children ...*ChildRequest) (*SectionCall, error) {
	if resolve == nil {
		return nil, fmt.Errorf("section helper: no template resolver for %q", name)
	}
	target, ok := resolve(name)
	if !ok || target == nil {
		return nil, fmt.Errorf("section helper: section template %q is not defined", name)
	}
	if target.Usage != UsageSection {
		return nil, fmt.Errorf("section helper: template %q is not a section-usage template", name)
	}

	effective := applyFieldDefaults(target, fields)
	if res := CheckValues(effective, target, resolve); len(res.Errors) > 0 {
		return nil, res.Errors[0]
	}

	if ve, invalid := CheckBody(target.Body, body); invalid {
		return nil, ve
	}
	// A helper body carries no child sections: children pass only through child
	// arguments, so a heading naming a declared child section is rejected even
	// when the call also passes that child explicitly.
	if child, named := bodyNamesChildSection(target, body); named {
		return nil, ValueError{
			Path:  []PathSegment{{Name: bodyField}},
			Rule:  "subheadings",
			Value: body,
			What:  fmt.Sprintf("subheadings: the body heading names child section %q, but a helper body carries no child sections", child),
			Fix:   "remove the heading; pass the child with a child argument instead",
		}
	}

	// Resolve the call's child arguments against the target's declared
	// sections, with the target's effective values as each child's
	// `.item.parent`. The children's ancestor chain is the target instance's
	// own chain — the enclosing chain with the target's resolved name appended
	// — so each child's `.item.depth` counts its enclosing same-template
	// ancestors along the full chain (item-context, bounded-self-nesting).
	// ResolveChildren is called even for an empty set so a target declaring a
	// mandatory child section the call leaves unfilled is rejected here, by the
	// same per-declaration min/max check that enforces the count over the full
	// passed set (nested-section-validation).
	childAncestors := append(append([]string{}, ancestors...), name)
	resolved, err := ResolveChildren(resolve, layerOf, target, children, childAncestors, effective, true)
	if err != nil {
		return nil, err
	}

	call := &SectionCall{
		Template: target,
		Values:   effective,
		Source:   fields,
		Body:     body,
		Children: resolved,
		Item: map[string]any{
			"index":    0,
			"number":   1,
			"count":    1,
			"first":    true,
			"last":     true,
			"section":  name,
			"template": name,
			"depth":    InstanceDepth(ancestors, name),
			"parent":   parent,
		},
	}
	if layerOf != nil {
		call.Layer = layerOf(name)
	}
	return call, nil
}

// applyFieldDefaults returns a fresh copy of fields with every declared field
// that is absent and carries a Default set to that default. The caller's map is
// never mutated, and a caller-supplied value always wins over the default.
func applyFieldDefaults(target *Template, fields map[string]any) map[string]any {
	effective := make(map[string]any, len(fields)+len(target.Fields))
	for k, v := range fields {
		effective[k] = v
	}
	for i := range target.Fields {
		f := &target.Fields[i]
		if _, present := effective[f.Name]; present {
			continue
		}
		if f.Default != nil {
			effective[f.Name] = f.Default
		}
	}
	return effective
}

// bodyNamesChildSection reports the first heading in body that names a section
// target declares, so a helper body never smuggles in a child section. Fenced
// code is skipped, matching the registry's build-time helper scan; an empty body
// or a target with no declared sections can never match.
func bodyNamesChildSection(target *Template, body string) (string, bool) {
	if target == nil || len(target.Sections) == 0 || strings.TrimSpace(body) == "" {
		return "", false
	}
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if _, name, ok := exampleHeadingLevel(line); ok {
			if _, declared := declaredSection(target, name); declared {
				return name, true
			}
		}
	}
	return "", false
}

// positionedCall pairs an extracted HelperCall with the byte position of its
// `section` command, so calls gathered from several parse trees — the layout
// root plus its `define`/`block` bodies — can be returned in source order.
type positionedCall struct {
	call HelperCall
	pos  int
}

// walkHelperList visits every node of a template body in source order.
func (s *Section) walkHelperList(list *parse.ListNode, tmplName, text string, out *[]positionedCall) error {
	if list == nil {
		return nil
	}
	for _, n := range list.Nodes {
		if err := s.walkHelperNode(n, tmplName, text, out); err != nil {
			return err
		}
	}
	return nil
}

// walkHelperNode descends one parse node, entering the bodies of control
// actions (`if`/`range`/`with`, including their else branches), parenthesized
// pipelines, chain nodes and `template` argument pipelines.
func (s *Section) walkHelperNode(n parse.Node, tmplName, text string, out *[]positionedCall) error {
	switch x := n.(type) {
	case *parse.ListNode:
		return s.walkHelperList(x, tmplName, text, out)
	case *parse.ActionNode:
		return s.walkHelperPipe(x.Pipe, tmplName, text, out)
	case *parse.IfNode:
		return s.walkHelperBranch(&x.BranchNode, tmplName, text, out)
	case *parse.RangeNode:
		return s.walkHelperBranch(&x.BranchNode, tmplName, text, out)
	case *parse.WithNode:
		return s.walkHelperBranch(&x.BranchNode, tmplName, text, out)
	case *parse.TemplateNode:
		return s.walkHelperPipe(x.Pipe, tmplName, text, out)
	case *parse.PipeNode:
		return s.walkHelperPipe(x, tmplName, text, out)
	case *parse.CommandNode:
		return s.walkHelperCommand(x, tmplName, text, out)
	case *parse.ChainNode:
		return s.walkHelperNode(x.Node, tmplName, text, out)
	default:
		return nil
	}
}

// walkHelperBranch visits a control action's condition pipeline and both its
// body and else-body lists.
func (s *Section) walkHelperBranch(b *parse.BranchNode, tmplName, text string, out *[]positionedCall) error {
	if b == nil {
		return nil
	}
	if err := s.walkHelperPipe(b.Pipe, tmplName, text, out); err != nil {
		return err
	}
	if err := s.walkHelperList(b.List, tmplName, text, out); err != nil {
		return err
	}
	return s.walkHelperList(b.ElseList, tmplName, text, out)
}

// walkHelperPipe visits each command of a pipeline, so a section call as a
// chained or parenthesized command is found.
func (s *Section) walkHelperPipe(p *parse.PipeNode, tmplName, text string, out *[]positionedCall) error {
	if p == nil {
		return nil
	}
	for _, cmd := range p.Cmds {
		if err := s.walkHelperCommand(cmd, tmplName, text, out); err != nil {
			return err
		}
	}
	return nil
}

// walkHelperCommand records a section call when the command invokes the
// `section` helper and then recurses into every argument, so nested pipelines
// (a `dict` fields argument or a parenthesized expression) are visited too.
func (s *Section) walkHelperCommand(cmd *parse.CommandNode, tmplName, text string, out *[]positionedCall) error {
	if cmd == nil {
		return nil
	}
	call, found, err := extractSectionHelperCall(cmd, tmplName, text)
	if err != nil {
		return err
	}
	if found {
		*out = append(*out, positionedCall{call: call, pos: int(cmd.Position())})
	}
	for _, arg := range cmd.Args {
		if err := s.walkHelperNode(arg, tmplName, text, out); err != nil {
			return err
		}
	}
	return nil
}

// extractSectionHelperCall recognises a `section` command and builds its
// HelperCall, or reports found=false for any other command. A missing or
// non-literal target name is an error: the load-time checks need a static name.
//
// The arguments after the target name are positional: the second is the
// optional fields expression and the third the optional body, matching the
// `[fields] [body]` grammar. A literal `nil` in either slot is treated as an
// OMITTED argument — HasFields/HasBody stay false — exactly as the render-time
// helper reads a nil argument, so a
// `{{ section "columns" nil nil (child …) }}` call supplies no fields and no
// body and the load checks apply the target's defaults and required-field rules
// against a known-empty supplied set. (The `nil` placeholder is how a call
// reaches its trailing child arguments.) Every further argument (position four
// onward) is a child argument — the value of a `child`/`children` constructor,
// or any other expression the call passes as a child — and is recorded on
// HelperCall.Children, in source order, by extractHelperChild. A child call
// therefore never occupies the fields or body slot, so the existing field/body
// reading is unchanged.
func extractSectionHelperCall(cmd *parse.CommandNode, tmplName, text string) (HelperCall, bool, error) {
	if cmd == nil || len(cmd.Args) == 0 {
		return HelperCall{}, false, nil
	}
	ident, ok := cmd.Args[0].(*parse.IdentifierNode)
	if !ok || ident.Ident != "section" {
		return HelperCall{}, false, nil
	}

	line := layoutLine(text, cmd)
	call := HelperCall{Template: tmplName, Line: line}
	args := cmd.Args[1:]
	if len(args) == 0 {
		return HelperCall{}, false, fmt.Errorf(
			"template %q: layout line %d: section helper call is missing its target name", tmplName, line)
	}
	name, ok := args[0].(*parse.StringNode)
	if !ok {
		return HelperCall{}, false, fmt.Errorf(
			"template %q: layout line %d: section helper target name must be a string literal, got %s",
			tmplName, line, args[0].String())
	}
	call.Name = name.Text

	if len(args) >= 2 {
		if _, omitted := args[1].(*parse.NilNode); !omitted {
			call.HasFields = true
			if keys, ok := literalDictKeys(args[1]); ok {
				call.FieldsLiteral = true
				call.FieldKeys = keys
			}
		}
	}
	if len(args) >= 3 {
		if _, omitted := args[2].(*parse.NilNode); !omitted {
			call.HasBody = true
			if body, ok := args[2].(*parse.StringNode); ok {
				call.BodyLiteral = true
				call.Body = body.Text
			}
		}
	}
	if len(args) > 3 {
		call.Children = make([]HelperChild, 0, len(args)-3)
		for _, arg := range args[3:] {
			call.Children = append(call.Children, extractHelperChild(arg))
		}
	}
	return call, true, nil
}

// extractHelperChild builds the static HelperChild record for one trailing child
// argument of a `section` call: a `child "<section>" [fields] [body] [child…]`
// constructor, a `children "<section>" …` forwarding, or any other expression.
// A `child` all of whose pieces — section, literal `dict` fields, string-literal
// body — are literal is Literal; a `child` with any non-literal piece is
// NonLiteral, with each literal piece still captured (so a non-literal fields or
// body leaves Section populated when the section name itself was literal). A
// `nil` placeholder in a child's fields or body slot is an omitted argument, not
// a non-literal one — it is how the grammar skips to the nested child arguments
// — so it leaves HasFields/HasBody false and keeps the child Literal, exactly as
// ChildFunc treats a nil argument. A `children` call is Forward, whatever its
// pieces: its `.data` list is a runtime value the load-time checks never inspect.
// Any other expression — a variable, a field access such as `.x`, a plain
// `dict`/`list` or any non-child call — is NonLiteral, with no pieces captured.
//
// Nested child arguments are captured recursively on HelperChild.Children and
// deliberately do not affect this child's Literal flag: each nested child is a
// child argument in its own right carrying its own markers (HelperChild).
func extractHelperChild(n parse.Node) HelperChild {
	cmd, kind, ok := childHelperCommand(n)
	if !ok {
		return HelperChild{NonLiteral: true}
	}
	args := cmd.Args[1:]

	child := HelperChild{}
	literal := true
	if len(args) >= 1 {
		if section, ok := args[0].(*parse.StringNode); ok {
			child.Section = section.Text
		} else {
			literal = false
		}
	} else {
		literal = false
	}

	if kind == "children" {
		// A `children` forwarding of a runtime `.data` list is never inspected
		// at load: only its literal section name (when the first argument is a
		// string literal) is recorded, and Forward marks the value.
		child.Forward = true
		return child
	}

	if len(args) >= 2 {
		if _, omitted := args[1].(*parse.NilNode); !omitted {
			child.HasFields = true
			if fields, ok := literalDictMap(args[1]); ok {
				child.FieldsLiteral = true
				child.Fields = fields
			} else {
				literal = false
			}
		}
	}
	if len(args) >= 3 {
		if _, omitted := args[2].(*parse.NilNode); !omitted {
			child.HasBody = true
			if body, ok := args[2].(*parse.StringNode); ok {
				child.BodyLiteral = true
				child.Body = body.Text
			} else {
				literal = false
			}
		}
	}
	if len(args) > 3 {
		child.Children = make([]HelperChild, 0, len(args)-3)
		for _, arg := range args[3:] {
			child.Children = append(child.Children, extractHelperChild(arg))
		}
	}

	child.Literal = literal
	child.NonLiteral = !literal
	return child
}

// childHelperCommand reports the command of a parenthesized `child` or
// `children` pipeline argument and whether the node is such a call at all. It
// returns ok=false for any other node, so a plain variable, a field access or a
// non-child call is never mistaken for a child constructor.
func childHelperCommand(n parse.Node) (*parse.CommandNode, string, bool) {
	pipe, ok := n.(*parse.PipeNode)
	if !ok || pipe == nil || len(pipe.Cmds) != 1 || pipe.Cmds[0] == nil {
		return nil, "", false
	}
	cmd := pipe.Cmds[0]
	if len(cmd.Args) == 0 {
		return nil, "", false
	}
	ident, ok := cmd.Args[0].(*parse.IdentifierNode)
	if !ok {
		return nil, "", false
	}
	if ident.Ident != "child" && ident.Ident != "children" {
		return nil, "", false
	}
	return cmd, ident.Ident, true
}

// literalDictKeys reports whether n is a parenthesized `(dict …)` pipeline all
// of whose key arguments (the even-indexed arguments after `dict`) are string
// literals, and returns those keys. A non-dict pipeline, or a dict with a
// non-literal key, cannot be checked fully, so it is not a literal dict.
func literalDictKeys(n parse.Node) ([]string, bool) {
	pipe, ok := n.(*parse.PipeNode)
	if !ok || pipe == nil || len(pipe.Cmds) != 1 || pipe.Cmds[0] == nil {
		return nil, false
	}
	cmd := pipe.Cmds[0]
	if len(cmd.Args) == 0 {
		return nil, false
	}
	ident, ok := cmd.Args[0].(*parse.IdentifierNode)
	if !ok || ident.Ident != "dict" {
		return nil, false
	}
	keys := make([]string, 0, (len(cmd.Args)-1+1)/2)
	for i := 1; i < len(cmd.Args); i += 2 {
		key, ok := cmd.Args[i].(*parse.StringNode)
		if !ok {
			return nil, false
		}
		keys = append(keys, key.Text)
	}
	return keys, true
}

// literalDictMap reports whether n is a parenthesized `(dict …)` pipeline whose
// keys are string literals and all of whose values are literal values the
// extractor can capture, and returns those keys and values. Unlike
// literalDictKeys — which reads only the keys of the `section` call's own
// fields, because those values are passed through opaque — a child's fields
// argument is captured whole or not at all, so a dict carrying any non-literal
// piece, a non-string key or an odd argument count is not a literal dict.
func literalDictMap(n parse.Node) (map[string]any, bool) {
	pipe, ok := n.(*parse.PipeNode)
	if !ok || pipe == nil || len(pipe.Cmds) != 1 || pipe.Cmds[0] == nil {
		return nil, false
	}
	cmd := pipe.Cmds[0]
	if len(cmd.Args) == 0 {
		return nil, false
	}
	ident, ok := cmd.Args[0].(*parse.IdentifierNode)
	if !ok || ident.Ident != "dict" {
		return nil, false
	}
	out := make(map[string]any, (len(cmd.Args)-1)/2)
	for i := 1; i < len(cmd.Args); i += 2 {
		key, ok := cmd.Args[i].(*parse.StringNode)
		if !ok || i+1 >= len(cmd.Args) {
			return nil, false
		}
		value, ok := literalNodeValue(cmd.Args[i+1])
		if !ok {
			return nil, false
		}
		out[key.Text] = value
	}
	return out, true
}

// literalNodeValue returns the Go value a parse node literally denotes — a
// quoted string, a number, a boolean, null, or a nested literal `dict`/`list` —
// and reports false for a dynamic expression (a variable, a field access, a
// function call) the extractor cannot evaluate at load. It is what makes a
// literal `dict`'s entries capturable; a single non-literal value makes the
// whole dict non-literal.
func literalNodeValue(n parse.Node) (any, bool) {
	switch x := n.(type) {
	case *parse.StringNode:
		return x.Text, true
	case *parse.BoolNode:
		return x.True, true
	case *parse.NilNode:
		return nil, true
	case *parse.NumberNode:
		switch {
		case x.IsInt:
			return x.Int64, true
		case x.IsUint:
			return x.Uint64, true
		case x.IsFloat:
			return x.Float64, true
		}
		return nil, false
	case *parse.PipeNode:
		if m, ok := literalDictMap(x); ok {
			return m, true
		}
		return literalListValue(x)
	}
	return nil, false
}

// literalListValue reports whether n is a parenthesized `(list …)` pipeline all
// of whose items are literal values, and returns them. A list carrying a
// non-literal item is not a literal list.
func literalListValue(n parse.Node) ([]any, bool) {
	pipe, ok := n.(*parse.PipeNode)
	if !ok || pipe == nil || len(pipe.Cmds) != 1 || pipe.Cmds[0] == nil {
		return nil, false
	}
	cmd := pipe.Cmds[0]
	if len(cmd.Args) == 0 {
		return nil, false
	}
	ident, ok := cmd.Args[0].(*parse.IdentifierNode)
	if !ok || ident.Ident != "list" {
		return nil, false
	}
	out := make([]any, 0, len(cmd.Args)-1)
	for _, item := range cmd.Args[1:] {
		value, ok := literalNodeValue(item)
		if !ok {
			return nil, false
		}
		out = append(out, value)
	}
	return out, true
}

// layoutLine returns the 1-based line of n within text. parse node positions
// are byte offsets into the parsed source, so the line is the number of
// newlines before that offset plus one.
func layoutLine(text string, n parse.Node) int {
	if n == nil {
		return 0
	}
	pos := int(n.Position())
	if pos < 0 {
		pos = 0
	}
	if pos > len(text) {
		pos = len(text)
	}
	return 1 + strings.Count(text[:pos], "\n")
}
