package template

import (
	"fmt"
	"strings"
)

// This file implements Section, the composition, variant and controlled-effects
// engine: it resolves a template's declared sections, enforces their min/max
// repeat limits over the section instances an author declared, walks the
// section-template composition tree with no depth limit (terminating safely on
// a reference cycle), and resolves the layout variant each enum field selects.
//
// Composition (template-composition): a slide template declares named sections,
// each accepting one or more section-usage templates with min/max repeat limits;
// a section template declares sections of its own, so composition nests with no
// depth limit. Sections never accept slide templates — the registry's build-time
// checks reject that, as they reject reference cycles; Walk here is what lets
// those checks terminate on a cyclic resolver instead of looping forever.
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
// name, ending with that name, for example `content → columns → content`.
//
// A cyclic resolver can never be walked to completion; Section.Walk reports
// this instead of looping forever. The registry rejects such a cycle at build
// time (template-build-checks).
type SectionCycleError struct {
	// Path is the chain of template names that closes the cycle, root first
	// and the repeated name last.
	Path []string
}

// Error renders the cycle as a readable chain.
func (e *SectionCycleError) Error() string {
	return "section template reference cycle: " + strings.Join(e.Path, " → ")
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
// descends as far as the resolver allows, with no depth limit.
//
// A name already on the current descent path is a reference cycle and is
// reported as *SectionCycleError (never an infinite loop); a name already
// returned through another branch is skipped, so each template appears once. A
// name the resolver does not define is an error. Whether a section accepts a
// slide-usage template is checked by the registry's build-time checks, not
// here.
func (s *Section) Walk(tmpl *Template) ([]*Template, error) {
	if tmpl == nil {
		return nil, nil
	}
	if s == nil || s.resolve == nil {
		return nil, nil
	}

	var out []*Template
	visited := make(map[string]bool)
	onPath := make(map[string]bool)

	var walk func(t *Template, path []string) error
	walk = func(t *Template, path []string) error {
		if t == nil {
			return nil
		}
		onPath[t.Name] = true
		path = append(path, t.Name)
		defer delete(onPath, t.Name)

		for i := range t.Sections {
			for _, name := range t.Sections[i].Accepted {
				if onPath[name] {
					cycle := make([]string, len(path), len(path)+1)
					copy(cycle, path)
					return &SectionCycleError{Path: append(cycle, name)}
				}
				if visited[name] {
					continue
				}
				nested, ok := s.resolve(name)
				if !ok || nested == nil {
					return fmt.Errorf("section template %q is not defined", name)
				}
				visited[name] = true
				out = append(out, nested)
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
