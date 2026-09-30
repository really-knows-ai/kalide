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
// section-template composition tree bounded at six heading levels (terminating
// safely on a reference cycle and reporting an over-deep chain), and resolves
// the layout variant each enum field selects.
//
// Composition (template-composition): a slide template declares named sections,
// each accepting one or more section-usage templates with min/max repeat limits;
// a section template declares sections of its own, so composition nests. Nesting
// depth is bounded by the content syntax — heading depth is nesting depth — so no
// composition chain may exceed the six Markdown heading levels `#`…`######`
// (requirements.constraint.section-depth-limit). Sections never accept slide
// templates — the registry's build-time checks reject that, as they reject
// reference cycles; Walk here is what lets those checks terminate on a cyclic
// resolver instead of looping forever.
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

	// Template is the name of the template whose layout declares this call.
	// It is carried so a build-time helper failure can be path-qualified to
	// that template's template.yaml.
	Template string

	// Line is the 1-based line of the call within the declaring layout's
	// source, or 0 when no position was resolved.
	Line int
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
// template that crosses the bound. Limit is that bound, in heading levels.
type SectionDepthError struct {
	// Path is the composition chain that exceeded the bound, the root
	// template first and the template that crosses the limit last.
	Path []string

	// Limit is the maximum permitted composition depth, in heading levels.
	Limit int
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
// composition of its declared sections, in depth-first declaration order. It
// descends the composition tree, counting every path — a template reachable at
// two depths is examined at both, so a shallow reach never masks a deeper
// chain — and stops at the six-heading-level bound
// (requirements.constraint.section-depth-limit).
//
// A name already on the current descent path is a reference cycle and is
// reported as *SectionCycleError (never an infinite loop); a name already
// returned through another branch is included in the result once. A chain
// deeper than six heading levels is reported as *SectionDepthError with the
// over-deep chain. A name the resolver does not define is an error. Whether a
// section accepts a slide-usage template is checked by the registry's
// build-time checks, not here.
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

	var walk func(t *Template, path []string) error
	walk = func(t *Template, path []string) error {
		if t == nil {
			return nil
		}
		onPath[t.Name] = true
		path = append(path, t.Name)
		defer delete(onPath, t.Name)

		if len(path) > sectionDepthLimit {
			chain := make([]string, len(path))
			copy(chain, path)
			return &SectionDepthError{Path: chain, Limit: sectionDepthLimit}
		}

		for i := range t.Sections {
			for _, name := range t.Sections[i].Accepted {
				if onPath[name] {
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
				// Recurse even into an already-emitted template: a deeper
				// path through it must still be counted against the bound.
				if err := walk(nested, path); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(tmpl, nil); err != nil {
		return nil, err
	}
	return out, nil
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
	parsed, err := htmltemplate.New(layoutName).Funcs(LayoutFuncMap(nil, "")).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("template %q: parse layout: %w", tmpl.Name, err)
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
// Name, so a call anywhere in the layout — the root, a `define`/`block` body or
// a control-action branch — is a reference. A non-literal target name is the
// error HelperCalls reports, and a layout that does not parse with the
// canonical layout func map is likewise an error.
//
// Repeated names are de-duplicated, keeping the first occurrence's position, so
// the result is the edge set of the reference graph rather than its call list:
// two calls to the same target are one edge for cycle rejection. That is
// deliberate — the per-call detail (literal dict keys, literal body and the
// literal/non-literal markers) stays available through HelperCalls, which the
// registry's load-time checks consume; HelperRefs exists only to feed the
// reference-graph walk.
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
		name := calls[i].Name
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
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
		call.HasFields = true
		if keys, ok := literalDictKeys(args[1]); ok {
			call.FieldsLiteral = true
			call.FieldKeys = keys
		}
	}
	if len(args) >= 3 {
		call.HasBody = true
		if body, ok := args[2].(*parse.StringNode); ok {
			call.BodyLiteral = true
			call.Body = body.Text
		}
	}
	return call, true, nil
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
