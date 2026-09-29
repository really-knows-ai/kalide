package template

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/really-knows-ai/kalide/internal/suggest"
)

// This file implements Registry, the compiled-in template registry, and the
// build-time checks that keep a mis-declared template out of the binary.
//
// The registry is the read-only catalogue the slide parser consults. It exposes
// exactly the slide.Catalogue method set — LookupSlideTemplate, TemplateNames,
// SectionNames and SectionDecl — with identical names and signatures, so
// *Registry satisfies that contract structurally: internal/template never
// imports internal/slide, and phase 5 adds the compile-time assertion.
//
// Registration is generic: Registry knows nothing about any concrete template.
// The built-ins are declared in Builtins (phase-4 task-10), which fills each
// Template's schema and structured example data and points the registry at the
// internal/assets Templates sub-tree for the layout and example source.
//
// Every failure here is a build-time programming failure — a compiled-in
// definition contradicting the schema, a composition cycle, or an example whose
// structured data does not satisfy its template. They are plain errors surfaced
// at startup or in tests, never positioned deck-author errors: an author can
// neither see nor fix a compiled-in template.
//
// The checks are deliberately structural. The example slide is validated as
// STRUCTURED data only (frontmatter/field values and declared section instances
// with their YAML data, through CheckValues and the section repeat limits); no
// Markdown or slide source is parsed here. Full end-to-end example validation
// through the parser pipeline is phase 5 (validate.ValidateBuiltinExamples).

// Registry is a set of compiled-in templates addressable by name. The zero
// value is ready to use; NewRegistry additionally attaches a content filesystem
// so Register can load layouts and example Markdown.
//
// A Registry is safe for concurrent lookups once populated. Registered
// templates are treated as immutable.
type Registry struct {
	mu        sync.RWMutex
	content   fs.FS
	templates map[string]*Template
}

// NewRegistry returns an empty registry. content is the fs.FS holding each
// template's compiled-in content at "<name>/layout.html.tmpl" (its Go
// html/template layout) and "<name>/example.md" (its validating example slide
// source); the built-ins pass assets.Templates(). A nil content makes Register
// skip content loading, so a template must carry its Layout.Text and
// Example.Markdown already — useful for tests and fully in-code templates.
//
// Templates registered here are subject to Register's reserved field/section
// name checks (checkDeclaredName): "deck", "slide" and "item" are reserved
// alongside "body", "notes", and "_format".
//
// single-binary (requirements.requirement.single-binary): the registry is
// packaged in the binary and works offline — content is read only through
// the given fs.FS (the go:embed'ed assets for the built-ins), with slash-
// separated io/fs paths built by path.Join, never path/filepath, the OS or
// the network. It is in-binary, self-contained and OS-neutral across all six
// supported targets (darwin/arm64, darwin/amd64, windows/amd64, windows/arm64,
// linux/amd64, linux/arm64), so lookup behaves identically on each.
func NewRegistry(content fs.FS) *Registry {
	return &Registry{content: content, templates: make(map[string]*Template)}
}

// Register adds t to the registry. It validates t's local definition (its name,
// usage and reserved field/section names), loads t's layout and example
// Markdown from the registry's content filesystem when one is attached, and
// stores it. A nil or empty content filesystem is skipped.
//
// The cross-template checks — reference cycles, a section accepting a slide
// template, a required-body template used as a field type — and the structured
// example checks run in Validate, because they need the whole set registered
// and must not depend on registration order. The built-ins loader registers
// every template and then calls Validate once.
func (r *Registry) Register(t *Template) error {
	if err := checkDefinition(t); err != nil {
		return err
	}
	if r.content != nil {
		if err := LoadTemplate(r.content, t); err != nil {
			return err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.templates == nil {
		r.templates = make(map[string]*Template)
	}
	if existing, exists := r.templates[t.Name]; exists {
		// Cross-kind name uniqueness: a slide template and a section may not
		// share a name (template-build-checks, templates-dir-layout).
		if existing.Usage != "" && t.Usage != "" && existing.Usage != t.Usage {
			return fmt.Errorf("template %q is declared as both a %s template and a %s template; a slide template and a section may not share a name",
				t.Name, existing.Usage, t.Usage)
		}
		return fmt.Errorf("template %q is already registered", t.Name)
	}
	r.templates[t.Name] = t
	return nil
}

// LoadTemplate reads t's layout source from "<name>/layout.html.tmpl" and its
// example slide source from "<name>/example.md" in content (the embedded
// internal/assets Templates sub-tree for the built-ins), filling Layout.Text
// and Example.Markdown. The structured example fields (Example.Frontmatter and
// Example.Sections) are authored alongside the Go schema, not read from the
// tree. Layout.Name defaults to the template name when unset, so a composed
// layout can invoke a section template with {{ template "<name>" . }}.
//
// The filesystem paths are fixed by the tree's convention, not configurable
// here: one directory per template, named by the template name.
func LoadTemplate(content fs.FS, t *Template) error {
	if t == nil {
		return errors.New("template: cannot load content for a nil template")
	}
	if t.Name == "" {
		return errors.New("template: cannot load content for a template with no name")
	}
	if content == nil {
		return fmt.Errorf("template %q: no content filesystem", t.Name)
	}

	layoutPath := path.Join(t.Name, "layout.html.tmpl")
	layout, err := fs.ReadFile(content, layoutPath)
	if err != nil {
		return fmt.Errorf("template %q: read layout %s: %w", t.Name, layoutPath, err)
	}
	if t.Layout.Name == "" {
		t.Layout.Name = t.Name
	}
	t.Layout.Text = string(layout)

	examplePath := path.Join(t.Name, "example.md")
	example, err := fs.ReadFile(content, examplePath)
	if err != nil {
		return fmt.Errorf("template %q: read example %s: %w", t.Name, examplePath, err)
	}
	t.Example.Markdown = string(example)

	return nil
}

// Validate runs every build-time check over the whole registry and returns the
// first failure, or nil when the registry is sound. It is the entry the
// built-ins loader calls once every template is registered, and the tests use
// it to prove each rejection.
//
// The checks run in a fixed, deterministic order over templates sorted by name:
//
//  1. each template's local definition (name, usage, reserved names);
//  2. no section accepts a slide-usage template;
//  3. no field (or list item) uses a required-body template as its type;
//  4. the composition tree has no reference cycle, no undefined names, and
//     no chain deeper than the six heading levels (the depth bound is
//     requirements.constraint.section-depth-limit); a walk failure names the
//     template it was walking so the templates loader path-qualifies it to
//     that template.yaml;
//  5. each example's STRUCTURED data satisfies its schema and the section
//     repeat limits (no Markdown or slide parsing).
func (r *Registry) Validate() error {
	for _, t := range r.Templates() {
		if err := checkDefinition(t); err != nil {
			return err
		}
	}
	for _, t := range r.Templates() {
		if err := r.checkSections(t); err != nil {
			return err
		}
		if err := r.checkFieldTypes(t); err != nil {
			return err
		}
	}
	for _, t := range r.Templates() {
		if _, err := NewSection(r.Lookup).Walk(t); err != nil {
			// Name the template the composition walk started from so the
			// templates loader (checkLibraryBuild, step 4) can path-qualify
			// the failure to that template's template.yaml:
			// manifestTemplateErrorName reads the `template "name": …`
			// prefix. The wrapped error still satisfies errors.As for
			// *SectionCycleError and *SectionDepthError.
			return fmt.Errorf("template %q: %w", t.Name, err)
		}
	}
	for _, t := range r.Templates() {
		if err := r.checkExample(t); err != nil {
			return err
		}
	}
	return nil
}

// Lookup returns the template registered under name. ok is false when no
// template has that name. Its signature matches CheckValues' and Section's
// resolver, so the registry is the resolver for both.
func (r *Registry) Lookup(name string) (*Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.templates[name]
	return t, ok
}

// Templates returns every registered template, sorted by name. The slice is a
// fresh allocation; the *Template values are the registered (immutable) ones.
func (r *Registry) Templates() []*Template {
	names := r.TemplateNames()
	out := make([]*Template, 0, len(names))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range names {
		out = append(out, r.templates[name])
	}
	return out
}

// LookupSlideTemplate reports how the named template may be used and whether it
// exists at all: usage is "slide" or "section", and ok is false when no
// template has that name. It is part of the slide.Catalogue contract.
func (r *Registry) LookupSlideTemplate(name string) (usage string, ok bool) {
	t, ok := r.Lookup(name)
	if !ok || t == nil {
		return "", false
	}
	return string(t.Usage), true
}

// TemplateNames returns every registered template name, sorted, so callers can
// pass them to a closest-match "did you mean …?" helper. It is part of the
// slide.Catalogue contract.
func (r *Registry) TemplateNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.templates))
	for name := range r.templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SectionNames returns the section names declared by the template tmpl, in
// declaration order. It never includes the reserved name "notes": notes are
// reserved for the parser's speaker-notes section and are not declarable as a
// section template. It returns nil for an unknown template; for a known
// template that declares no sections it returns an empty, non-nil slice. It is
// part of the slide.Catalogue contract.
func (r *Registry) SectionNames(tmpl string) []string {
	t, ok := r.Lookup(tmpl)
	if !ok || t == nil {
		return nil
	}
	names := make([]string, 0, len(t.Sections))
	for i := range t.Sections {
		if t.Sections[i].Name == "notes" {
			continue
		}
		names = append(names, t.Sections[i].Name)
	}
	return names
}

// SectionDecl returns the declaration for section section of the template tmpl:
// the section-usage templates it accepts and the min and max number of times it
// may repeat. ok is false when tmpl is unknown or declares no such section. The
// accepted slice is a fresh copy. It is part of the slide.Catalogue contract.
//
// A `template:` key is required on a section instance iff len(accepted) > 1:
// with exactly one accepted template the instance resolves to it, and with
// several the author must choose one.
func (r *Registry) SectionDecl(tmpl, section string) (accepted []string, min, max int, ok bool) {
	t, found := r.Lookup(tmpl)
	if !found || t == nil {
		return nil, 0, 0, false
	}
	d, found := declaredSection(t, section)
	if !found {
		return nil, 0, 0, false
	}
	out := make([]string, len(d.Accepted))
	copy(out, d.Accepted)
	return out, d.Min, d.Max, true
}

// checkDefinition validates a template's local definition: it must be non-nil
// with a name, its usage must be slide or section (an empty usage is tolerated
// so a partially built fixture can exercise the other checks), and its declared
// field and section names must be non-empty, unique, and free of the reserved
// `body`, `notes`, `deck`, `slide` and `item` names.
func checkDefinition(t *Template) error {
	if t == nil {
		return errors.New("template: definition must not be nil")
	}
	if t.Name == "" {
		return errors.New("template: name must not be empty")
	}
	if t.Usage != "" && t.Usage != UsageSlide && t.Usage != UsageSection {
		return fmt.Errorf("template %q: invalid usage %q (want %q or %q)", t.Name, string(t.Usage), UsageSlide, UsageSection)
	}

	fields := make(map[string]struct{}, len(t.Fields))
	for i := range t.Fields {
		name := t.Fields[i].Name
		if err := checkDeclaredName("field", t.Name, name); err != nil {
			return err
		}
		if _, dup := fields[name]; dup {
			return fmt.Errorf("template %q: field %q is declared more than once", t.Name, name)
		}
		fields[name] = struct{}{}
	}

	sections := make(map[string]struct{}, len(t.Sections))
	for i := range t.Sections {
		name := t.Sections[i].Name
		if err := checkDeclaredName("section", t.Name, name); err != nil {
			return err
		}
		if _, dup := sections[name]; dup {
			return fmt.Errorf("template %q: section %q is declared more than once", t.Name, name)
		}
		sections[name] = struct{}{}
	}

	for i := range t.Fields {
		if err := checkFieldVariants(t.Name, &t.Fields[i]); err != nil {
			return err
		}
	}
	return nil
}

// checkFieldVariants rejects an enum field with no declared variants, and a
// Default that is not one of them (template-variants), recursing into a
// list's single item type.
func checkFieldVariants(tmpl string, f *Field) error {
	if f.Type == FieldEnum {
		if len(f.Variants) == 0 {
			return fmt.Errorf("template %q: field %q is an enum field with no variants", tmpl, f.Name)
		}
		if def, ok := f.Default.(string); ok && def != "" && !containsString(f.Variants, def) {
			return fmt.Errorf("template %q: field %q has default %q, which is not one of its variants (%s)",
				tmpl, f.Name, def, strings.Join(f.Variants, ", "))
		}
	}
	if f.Type == FieldList && f.Item != nil {
		return checkFieldVariants(tmpl, f.Item)
	}
	return nil
}

// reservedDeclaredNames are the exact field/section names checkDeclaredName
// rejects: `body` the implied body field, `notes` the parser's speaker-notes
// section, `deck`, `slide` and `item` the reserved template-context names
// (requirements.constraint.reserved-template-names).
var reservedDeclaredNames = []string{"body", "notes", "deck", "slide", "item"}

// reservedDeclaredNameSuffix is the reserved suffix of a field's
// format-selection sibling: a declared field or section name ending in it
// (such as `foo_format`) is rejected.
const reservedDeclaredNameSuffix = "_format"

// ReservedNames returns the sorted reserved declared-name tokens enforced by
// checkDeclaredName: the exact names `body`, `notes`, `deck`, `slide` and
// `item`, plus the reserved `_format` suffix token. It is the single source of
// truth for the reserved-name vocabulary, deriving from the same declarations
// checkDeclaredName enforces (the `_format` entry is a suffix, not an exact
// name). The returned slice is a fresh allocation.
// (requirements.constraint.reserved-template-names)
func ReservedNames() []string {
	out := make([]string, 0, len(reservedDeclaredNames)+1)
	out = append(out, reservedDeclaredNames...)
	out = append(out, reservedDeclaredNameSuffix)
	sort.Strings(out)
	return out
}

// checkDeclaredName rejects a field or section name that is empty or reserved.
// `body` is the implied body field, `notes` the parser's speaker-notes section,
// `deck`, `slide` and `item` the reserved top-level template-context names, and
// a trailing `_format` the reserved format-selection sibling of a field. None
// of them is declarable.
//
// single-binary (requirements.requirement.single-binary): the check is pure
// string logic, OS-neutral and offline — an exact, case-sensitive byte
// comparison on the declared name, with no case folding, path-separator or
// filesystem handling — and identical across all six supported targets
// (darwin/arm64, darwin/amd64, windows/amd64, windows/arm64, linux/amd64,
// linux/arm64), so it accepts and rejects exactly the same names on each.
func checkDeclaredName(kind, tmpl, name string) error {
	if name == "" {
		return fmt.Errorf("template %q: %s name must not be empty", tmpl, kind)
	}
	if containsString(reservedDeclaredNames, name) {
		return fmt.Errorf("template %q: %s name %q is reserved and cannot be declared", tmpl, kind, name)
	}
	if strings.HasSuffix(name, reservedDeclaredNameSuffix) {
		return fmt.Errorf("template %q: %s name %q uses the reserved %s suffix", tmpl, kind, name, reservedDeclaredNameSuffix)
	}
	return nil
}

// checkSections rejects a section declaration that accepts no template, a
// template that is not defined, or a slide-usage template (sections accept only
// section-usage templates), and rejects impossible repeat bounds.
func (r *Registry) checkSections(t *Template) error {
	for i := range t.Sections {
		d := &t.Sections[i]
		if len(d.Accepted) == 0 {
			return fmt.Errorf("template %q: section %q accepts no section template", t.Name, d.Name)
		}
		if d.Min < 0 {
			return fmt.Errorf("template %q: section %q has a negative minimum %d", t.Name, d.Name, d.Min)
		}
		if d.Max > 0 && d.Max < d.Min {
			return fmt.Errorf("template %q: section %q has max %d below min %d", t.Name, d.Name, d.Max, d.Min)
		}
		for _, name := range d.Accepted {
			st, ok := r.Lookup(name)
			if !ok || st == nil {
				msg := fmt.Sprintf("template %q: section %q accepts %q, which is not a defined template", t.Name, d.Name, name)
				if closest := suggest.Closest(name, r.TemplateNames()); closest != "" {
					msg = fmt.Sprintf("%s: did you mean %q?", msg, closest)
				}
				return errors.New(msg)
			}
			if st.Usage == UsageSlide {
				return fmt.Errorf("template %q: section %q accepts %q, which is a slide-usage template; sections accept only section-usage templates", t.Name, d.Name, name)
			}
		}
	}
	return nil
}

// checkFieldTypes rejects a section-template field (or list item) that names no
// template, a slide-usage template, or a template whose body is required: a
// section template used as a field type takes YAML data only, so it can never
// carry the body a required-body template demands.
func (r *Registry) checkFieldTypes(t *Template) error {
	for i := range t.Fields {
		if err := r.checkFieldType(t.Name, &t.Fields[i]); err != nil {
			return err
		}
	}
	return nil
}

// checkFieldType checks one field, recursing into a list's single item type.
func (r *Registry) checkFieldType(tmpl string, f *Field) error {
	switch f.Type {
	case FieldSectionTemplate:
		if f.SectionTemplate == "" {
			return fmt.Errorf("template %q: field %q is a section-template field with no section template", tmpl, f.Name)
		}
		st, ok := r.Lookup(f.SectionTemplate)
		if !ok || st == nil {
			msg := fmt.Sprintf("template %q: field %q names section template %q, which is not defined", tmpl, f.Name, f.SectionTemplate)
			if closest := suggest.Closest(f.SectionTemplate, r.TemplateNames()); closest != "" {
				msg = fmt.Sprintf("%s: did you mean %q?", msg, closest)
			}
			return errors.New(msg)
		}
		if st.Body.Mode == BodyRequired {
			return fmt.Errorf("template %q: field %q uses section template %q as a field type, but that template requires a body", tmpl, f.Name, f.SectionTemplate)
		}
		if st.Usage == UsageSlide {
			return fmt.Errorf("template %q: field %q names %q, which is a slide-usage template; a section-template field requires a section-usage template", tmpl, f.Name, f.SectionTemplate)
		}
	case FieldList:
		if f.Item != nil {
			item := *f.Item
			return r.checkFieldType(tmpl, &item)
		}
	}
	return nil
}

// checkExample validates one template's STRUCTURED example data — no Markdown
// or slide parsing: the example frontmatter against the template's fields, each
// declared section instance against the section template it resolves to, and
// the section count against the declared min/max repeat limits. It also
// enforces that an instance names `template:` exactly when the section accepts
// more than one template, and that the named template is one of the accepted
// ones.
func (r *Registry) checkExample(t *Template) error {
	// A template whose example is validated end to end elsewhere
	// (checkLibraryExamples, template-manifest) has nothing for this
	// schema-only check to do.
	if t.Example.Deferred {
		return nil
	}
	if res := CheckValues(t.Example.Frontmatter, t, r.Lookup); len(res.Errors) > 0 {
		return fmt.Errorf("template %q: example frontmatter: %s", t.Name, res.Errors[0].Error())
	}

	names := make([]string, 0, len(t.Example.Sections))
	for _, sec := range t.Example.Sections {
		names = append(names, sec.Name)

		d, ok := declaredSection(t, sec.Name)
		if !ok {
			return fmt.Errorf("template %q: example declares section %q, which the template does not declare", t.Name, sec.Name)
		}
		chosen := sec.Template
		if chosen == "" {
			if len(d.Accepted) != 1 {
				return fmt.Errorf("template %q: example section %q must name a template: (accepts %s)", t.Name, sec.Name, strings.Join(d.Accepted, ", "))
			}
			chosen = d.Accepted[0]
		}
		if !containsString(d.Accepted, chosen) {
			return fmt.Errorf("template %q: example section %q uses template %q, which is not one of the accepted templates (%s)", t.Name, sec.Name, chosen, strings.Join(d.Accepted, ", "))
		}
		st, ok := r.Lookup(chosen)
		if !ok || st == nil {
			return fmt.Errorf("template %q: example section %q uses template %q, which is not defined", t.Name, sec.Name, chosen)
		}
		if res := CheckValues(sec.Frontmatter, st, r.Lookup); len(res.Errors) > 0 {
			return fmt.Errorf("template %q: example section %q (%s): %s", t.Name, sec.Name, chosen, res.Errors[0].Error())
		}
	}

	if errs := NewSection(r.Lookup).CheckRepeats(t, names); len(errs) > 0 {
		return fmt.Errorf("template %q: example: %s", t.Name, errs[0].Error())
	}
	return nil
}

// declaredSection returns the declaration for name in tmpl, if any.
func declaredSection(t *Template, name string) (SectionDecl, bool) {
	if t == nil {
		return SectionDecl{}, false
	}
	for i := range t.Sections {
		if t.Sections[i].Name == name {
			return t.Sections[i], true
		}
	}
	return SectionDecl{}, false
}
