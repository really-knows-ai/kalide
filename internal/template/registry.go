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

	// helperTargets memoises HelperTargets, keyed by template name: a present
	// key holds a template's computed section-helper reference names —
	// including nil for a layout with no helper calls, which is distinct from
	// an absent key that has not been computed yet. It is lazily filled on
	// first use and guarded by mu. It never needs invalidation: registered
	// templates are treated as immutable and Register refuses to re-register a
	// name, so a template's layout text cannot change after it is cached.
	helperTargets map[string][]string

	// layers records each registered template's owning-layer provenance,
	// keyed by template name: the *LibraryLayer that contributed the template
	// when the registry was built from a merged Library
	// (NewRegistryFromLibrary, via setLayer). A name is absent when no
	// provenance is known — a registry built in code (NewRegistry/Register),
	// or a Library whose layer for that template could not be identified —
	// and LayerOf then reports nil. It is lazily filled on first setLayer and
	// guarded by mu. Like helperTargets it never needs invalidation:
	// registered templates are immutable and a name is never re-registered.
	layers map[string]*LibraryLayer
}

// NewRegistry returns an empty registry. content is the fs.FS holding each
// template's compiled-in content at "<name>/layout.html.tmpl" (its Go
// html/template layout) and "<name>/example.md" (its validating example slide
// source); the built-ins pass assets.Templates(). A nil content makes Register
// skip content loading, so a template must carry its Layout.Text and
// Example.Markdown already — useful for tests and fully in-code templates.
//
// Templates registered here are subject to Register's reserved field/section
// name checks (checkDeclaredName): "deck", "slide", "item", "raw" and "data"
// are reserved alongside "body", "notes", and "_format".
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
	// No layer root is known at registration: a template's owning layer is
	// recorded only after registration (NewRegistryFromLibrary's setLayer),
	// so an enum `variants_from:` glob is not expanded here. The owning
	// layer's root is supplied by the caller that knows it.
	if err := checkDefinition(t, nil); err != nil {
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
//  2. each template's declared sections and the static analysis of every
//     `section` helper call in its layout (checkSections,
//     section-helper-load-checks): no section accepts a slide-usage template,
//     and each call's target must be a defined section-usage template (an
//     unknown name gets a closest-match suggestion), each literal `dict` key a
//     field the target declares, every required target field present or
//     defaulted, no target declaring a min>0 child section, and no literal body
//     heading naming a declared child section of the target;
//  3. no field (or list item) uses a required-body template as its type;
//  4. the composition tree has no reference cycle, no undefined names, and
//     no chain deeper than the six heading levels (the depth bound is
//     requirements.constraint.section-depth-limit); the walk (NewSection.Walk)
//     follows the `section` helper-call edges (HelperRefs) as well as the
//     declared-section edges, so a helper-closed cycle is rejected with the
//     chain in the error, but a helper edge is not a heading and adds no depth
//     level; a walk failure names the template it was walking so the templates
//     loader path-qualifies it to that template.yaml;
//  5. each example's STRUCTURED data satisfies its schema and the section
//     repeat limits (no Markdown or slide parsing).
//
// Every failure carries exactly one `template "<name>":` prefix — the helper
// checks of step 2 carry their own, and step 4 adds it to the walk errors that
// lack one while leaving a HelperRefs error's prefix untouched — so
// checkLibraryBuild (templates-dir-validation step 4) path-qualifies it to the
// offending template.yaml.
func (r *Registry) Validate() error {
	for _, t := range r.Templates() {
		// A nil root: the registry carries no per-template owning-layer root
		// yet, so an enum `variants_from:` glob stays unexpanded here (a
		// no-op, not a rejection).
		if err := checkDefinition(t, nil); err != nil {
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
			// Section.Walk surfaces a HelperRefs failure unchanged, and every
			// HelperCalls error already carries the `template "<name>":`
			// prefix (the same single prefix checkSections returns at step 2),
			// so wrapping it again would double that prefix. If the error is
			// already in the `template "name": …` shape
			// manifestTemplateErrorName recognises, return it as-is.
			//
			// A cycle, depth or undefined-name walk error carries no prefix,
			// so name the template the composition walk started from: the
			// templates loader (checkLibraryBuild, step 4) path-qualifies the
			// failure to that template's template.yaml via the same
			// `template "name": …` prefix. The wrapped error still satisfies
			// errors.As for *SectionCycleError and *SectionDepthError.
			if _, named := manifestTemplateErrorName(err); named {
				return err
			}
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

// LayerOf returns the *LibraryLayer that contributed the template registered
// under name, or nil when the registry carries no provenance for it. It is how
// a caller holding only a template name — the render/example paths binding the
// `media` helper for a slide or a called section, for example — finds that
// template's owning layer, so every reference inside it resolves in the layer
// that owns it rather than the merged Library's primary root or media
// (library-layer-references).
//
// Provenance is recorded only when a registry is built from a merged Library
// (NewRegistryFromLibrary, via setLayer). A registry built in code
// (NewRegistry/Register), and any name for which no layer was identified,
// report nil — "no owning layer known" — which a caller treats as the
// pre-provenance behaviour: resolve against the primary/merged root. A nil
// receiver also returns nil, so the accessor is safe on an unset registry.
//
// The returned *LibraryLayer is the registered (immutable) one; the accessor is
// safe for concurrent use.
func (r *Registry) LayerOf(name string) *LibraryLayer {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.layers[name]
}

// setLayer records that the template registered under name was contributed by
// layer, so LayerOf(name) can return it. NewRegistryFromLibrary calls it while
// building a registry over a merged Library, identifying each template's
// owning layer by identity from the Library's Layers. A nil layer (or an empty
// name) records nothing: an unknown owning layer is indistinguishable from no
// provenance, and both report nil through LayerOf. Registration is
// package-internal; a registry a caller builds with Register records none.
func (r *Registry) setLayer(name string, layer *LibraryLayer) {
	if r == nil || name == "" || layer == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.layers == nil {
		r.layers = make(map[string]*LibraryLayer)
	}
	r.layers[name] = layer
}

// HelperTargets returns the section-template names the named template's layout
// references through the `section` helper, in source order and de-duplicated —
// the helper-call edges the render path follows when it decides which layouts
// belong in a slide's shared namespace. A call's `child`/`children` arguments
// are references too, so the target's declared section's instantiated template
// (a literal `child`) or accepted templates (a `children` forwarding) are
// included, making the set the full reference set a renderer must carry.
//
// It is the registry-level, lazily memoised accessor over Section.HelperRefs:
// the first call for a name re-parses that template's Layout.Text with the
// canonical layout func map (media/section/dict/list/child/children) and caches
// the result, so
// a renderer walking a whole slide need not re-parse every reachable layout per
// RenderSlide. Repeated calls return the identical cached slice.
//
// It is computed on first use rather than during Validate because the render
// path may use a registry that was never Validated: `kalide start` builds the
// served registry with NewRegistryFromLibrary and validates deck content, not
// the template registry. Computing lazily therefore covers every construction
// path — NewRegistry/Register/Validate, NewRegistryFromLibrary and LoadTemplate
// — whether or not Validate ever ran.
//
// A HelperRefs error is swallowed deliberately, exactly as the renderer
// swallows it: load has already validated every layout, the only way HelperRefs
// can fail is a layout that does not re-parse with the canonical func map, and
// the renderer's own Parse reports that same text. The result is cached even
// when empty, so a layout with no helper calls is re-parsed at most once. An
// unknown name yields nil and is not cached (there is no layout to parse).
//
// The returned slice is the cached one, not a copy: the caller must not mutate
// it. The accessor is safe for concurrent use — the cache is guarded by the
// registry mutex — and needs no invalidation because registered templates are
// immutable and a name can never be re-registered (Register rejects a
// duplicate).
func (r *Registry) HelperTargets(name string) []string {
	r.mu.RLock()
	if r.helperTargets != nil {
		if refs, ok := r.helperTargets[name]; ok {
			r.mu.RUnlock()
			return refs
		}
	}
	r.mu.RUnlock()

	t, ok := r.Lookup(name)
	if !ok || t == nil {
		return nil
	}
	refs, err := NewSection(r.Lookup).HelperRefs(t)
	if err != nil {
		refs = nil
	}

	r.mu.Lock()
	if r.helperTargets == nil {
		r.helperTargets = make(map[string][]string)
	}
	if existing, cached := r.helperTargets[name]; cached {
		// Another goroutine computed the same entry while we were parsing;
		// return its slice so every caller shares one backing array.
		refs = existing
	} else {
		r.helperTargets[name] = refs
	}
	r.mu.Unlock()
	return refs
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

// checkInternalTemplate is the single internal-template rule (internal-templates):
// it reports whether t is hidden from the default listing by its manifest's
// `internal: true` (Template.Internal). It is the one answer the registry's
// Catalogue surface and the slide parser's Markdown-template-target checks
// consult, so the rule cannot drift between them. The read is nil-safe: a nil
// *Template is not internal, matching an unknown name.
func checkInternalTemplate(t *Template) bool {
	return t != nil && t.Internal
}

// LookupSlideTemplate reports how the named template may be used, whether it is
// internal, and whether it exists at all: usage is "slide" or "section",
// internal is the named template's Internal flag (false when no template has
// that name), and ok is false when no template has that name. An internal
// template is still returned here with ok true — the caller distinguishes it
// from an unknown name — and the Catalogue surface refuses it as a Markdown
// target. It is part of the slide.Catalogue contract.
func (r *Registry) LookupSlideTemplate(name string) (usage string, internal bool, ok bool) {
	t, ok := r.Lookup(name)
	if !ok || t == nil {
		return "", false, false
	}
	return string(t.Usage), checkInternalTemplate(t), true
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
// the section-usage templates it accepts, the min and max number of times it may
// repeat, and the declaration's max_depth bound. ok is false when tmpl is
// unknown or declares no such section. The accepted slice is a fresh copy.
// maxDepth is the declaration's positive-integer `max_depth` bound (0 when
// unbounded). It is part of the slide.Catalogue contract.
//
// A `template:` key is required on a section instance iff len(accepted) > 1:
// with exactly one accepted template the instance resolves to it, and with
// several the author must choose one.
func (r *Registry) SectionDecl(tmpl, section string) (accepted []string, min, max, maxDepth int, ok bool) {
	t, found := r.Lookup(tmpl)
	if !found || t == nil {
		return nil, 0, 0, 0, false
	}
	d, found := declaredSection(t, section)
	if !found {
		return nil, 0, 0, 0, false
	}
	out := make([]string, len(d.Accepted))
	copy(out, d.Accepted)
	return out, d.Min, d.Max, d.MaxDepth, true
}

// checkDefinition validates a template's local definition: it must be non-nil
// with a name, its usage must be slide or section (an empty usage is tolerated
// so a partially built fixture can exercise the other checks), and its declared
// field and section names must be non-empty, unique, and free of the reserved
// `body`, `notes`, `deck`, `slide`, `item`, `raw` and `data` names.
//
// It also validates every list field's cross-field bound
// (cross-field-list-constraints): a max_items_from/min_items_from must name a
// declared list field — a leading `/name` a top-level field of the template, a
// bare name a peer in the field's own scope — recursing into an inline record's
// item fields so a bound declared inside a record resolves against that
// record's siblings. A reference that names no field, or a field that is not a
// list, is rejected here before any content is validated.
//
// root is the owning library layer's root filesystem (media as a direct
// entry), threaded to checkFieldVariants so an enum field's `variants_from:`
// glob expands against the layer that owns the template (enum-variants-from).
// A nil root leaves such a glob unexpanded (and unchecked): the caller that
// knows the owning layer supplies it, and a registry built with no layer
// provenance passes nil.
func checkDefinition(t *Template, root fs.FS) error {
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
		if err := checkFieldVariants(t.Name, &t.Fields[i], root); err != nil {
			return err
		}
	}

	// cross-field-list-constraints: each list field's max_items_from and
	// min_items_from must name a declared list field — a leading `/name` a
	// top-level field of the template, a bare name a peer in the field's own
	// scope (a sibling within the enclosing inline record, or a top-level field
	// for a top-level field). The reference is resolved here, at load, so a
	// broken bound is rejected before any content is validated; an unresolvable
	// name is a templates error carrying a closest-match suggestion. Record item
	// fields are walked recursively, each record's own fields forming the scope
	// its bare names resolve against, while a `/name` always names a top-level
	// field.
	var checkListRefs func(scope []Field) error
	checkListRefs = func(scope []Field) error {
		for i := range scope {
			f := &scope[i]
			for _, ref := range []struct{ key, val string }{
				{"max_items_from", f.MaxItemsFrom},
				{"min_items_from", f.MinItemsFrom},
			} {
				if ref.val == "" {
					continue
				}
				name := ref.val
				lookup := scope
				if strings.HasPrefix(name, "/") {
					name = strings.TrimPrefix(name, "/")
					lookup = t.Fields
				}
				var found *Field
				candidates := make([]string, 0, len(lookup))
				for j := range lookup {
					candidates = append(candidates, lookup[j].Name)
					if lookup[j].Name == name {
						found = &lookup[j]
					}
				}
				if found == nil {
					msg := fmt.Sprintf("template %q: field %q: key %q names %q, which is not a declared field", t.Name, f.Name, ref.key, ref.val)
					if closest := suggest.Closest(name, candidates); closest != "" {
						msg = fmt.Sprintf("%s: did you mean %q?", msg, closest)
					}
					return errors.New(msg)
				}
				if found.Type != FieldList {
					return fmt.Errorf("template %q: field %q: key %q names %q, which is not a list field", t.Name, f.Name, ref.key, ref.val)
				}
			}
			if f.Item != nil && f.Item.Type == FieldRecord {
				if err := checkListRefs(f.Item.Fields); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := checkListRefs(t.Fields); err != nil {
		return err
	}
	return nil
}

// checkFieldVariants rejects an enum field with no declared variants, and a
// Default that is not one of them (template-variants), recursing into a
// list's single item type and into an inline record item's fields. An enum
// field's variants are the only variant mechanism at every nesting depth, so
// the same two errors fire for an enum field declared inside a record item
// (record-item-type), however deeply the record is nested.
//
// A field that declares its variants with a `variants_from:` glob instead of
// an inline `variants:` set has the glob expanded through expandVariantsFrom
// into its effective Variants before those two checks run
// (enum-variants-from): root is the owning library layer's root filesystem
// (media as a direct entry), so the glob resolves in the layer that owns the
// template. The expanded, sorted set is written into f.Variants — the
// effective variants validateEnum and `kalide templates <name>` read — and
// f.VariantsFrom is cleared so a second pass over the same field (Register
// then Validate both call checkDefinition) is a no-op rather than a false
// "declares both" error. Declaring both `variants:` and `variants_from:` on
// one field is rejected here without consulting root.
//
// A nil root (a registry built without layer provenance) cannot expand a
// glob: the field's effective variants are then unknown, so the glob is left
// unexpanded and neither the no-variants nor the default-membership check
// runs — it is a sensible no-op, not a rejection. Wiring the owning layer's
// root through checkDefinition is what makes the glob checkable.
func checkFieldVariants(tmpl string, f *Field, root fs.FS) error {
	if f.Type == FieldEnum {
		if f.VariantsFrom != "" {
			if len(f.Variants) > 0 {
				return fmt.Errorf("template %q: field %q declares both %q and %q", tmpl, f.Name, "variants", "variants_from")
			}
			if root == nil {
				// No root: the glob cannot be expanded, so the effective
				// variants are not knowable here. Leave them unexpanded and
				// run no membership check. An enum field declares no child
				// descriptors, so there is nothing to recurse into.
				return nil
			}
			variants, err := expandVariantsFrom(f, root)
			if err != nil {
				return fmt.Errorf("template %q: field %q: %w", tmpl, f.Name, err)
			}
			f.Variants = variants
			f.VariantsFrom = ""
		}
		if len(f.Variants) == 0 {
			return fmt.Errorf("template %q: field %q is an enum field with no variants", tmpl, f.Name)
		}
		if def, ok := f.Default.(string); ok && def != "" && !containsString(f.Variants, def) {
			return fmt.Errorf("template %q: field %q has default %q, which is not one of its variants (%s)",
				tmpl, f.Name, def, strings.Join(f.Variants, ", "))
		}
	}
	switch f.Type {
	case FieldList:
		if f.Item != nil {
			return checkFieldVariants(tmpl, f.Item, root)
		}
	case FieldRecord:
		for i := range f.Fields {
			if err := checkFieldVariants(tmpl, &f.Fields[i], root); err != nil {
				return err
			}
		}
	}
	return nil
}

// reservedDeclaredNames are the exact field/section names checkDeclaredName
// rejects: `body` the implied body field, `notes` the parser's speaker-notes
// section, `deck`, `slide`, `item`, `raw` and `data` the reserved
// template-context names (requirements.constraint.reserved-template-names).
var reservedDeclaredNames = []string{"body", "notes", "deck", "slide", "item", "raw", "data"}

// reservedDeclaredNameSuffix is the reserved suffix of a field's
// format-selection sibling: a declared field or section name ending in it
// (such as `foo_format`) is rejected.
const reservedDeclaredNameSuffix = "_format"

// ReservedNames returns the sorted reserved declared-name tokens enforced by
// checkDeclaredName: the exact names `body`, `notes`, `deck`, `slide`, `item`,
// `raw` and `data`, plus the reserved `_format` suffix token. It is the single source of
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
// `deck`, `slide`, `item`, `raw` and `data` the reserved top-level
// template-context names, and a trailing `_format` the reserved
// format-selection sibling of a field. None
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
//
// Every name is resolved against the registry's WHOLE template namespace, not
// just the checked template's own layer: r.Lookup — and r.TemplateNames, which
// feeds the closest-match suggestion — sees every template registered in r. For
// a registry built from a merged Library (NewRegistryFromLibrary), that
// namespace is the union of every contributing layer's slides and sections
// (layered-template-library), so a call in a local-layer template resolves an
// external-layer section and vice versa (library-layer-references). This check
// therefore only decides whether each call resolves somewhere in the union.
//
// It deliberately does NOT re-check an external-layer template's call that
// names a local-only section: that reference is already rejected when the
// external layer is loaded standalone (loadLibrary in libraryLayerFull mode,
// the external/standalone root), which validates the layer as a self-contained
// library against its own namespace (external-template-library). Per-layer
// isolation is a per-layer load concern; this merged pass runs over the union
// and lets a cross-layer call resolve.
//
// It then applies the load-time checks for every `section` helper call in the
// template's layout (requirements.requirement.section-helper-load-checks). A
// helper call is a static reference, so the same check pass that validates the
// declared sections validates it:
//
//   - the target must resolve to a defined section-usage template — an unknown
//     name or a slide template is an error with a closest-match suggestion;
//   - each literal `dict` key must be a field the target declares;
//   - the target's required fields must be present among those keys or carry a
//     default (when `fields` is omitted every required field must be
//     defaulted);
//   - each literal `child` argument names a section the target declares, its
//     instantiated template (selected by the reserved `template` key among its
//     fields, required iff the declaration accepts more than one) is accepted
//     by that declaration, its literal `dict` keys are fields the instantiated
//     template declares (the `template` key removed first), its required fields
//     are supplied or defaulted, its literal body names no declared child
//     section of its own instantiated template, and its nested `child`
//     arguments are checked the same way, recursively
//     (requirements.requirement.child-constructor);
//   - when the call's whole child set is statically known — every child
//     argument a literal `child`, nested likewise — each of the target's
//     declared sections has its min/max enforced over the literal set; a
//     target declaring a child section with min > 0 that the call leaves
//     entirely unfilled is rejected with the no-children message, and a mixed
//     call (any `children` forwarding or other non-literal child argument)
//     validates each literal child individually and defers the target's min/max
//     to render (requirements.requirement.children-forwarding,
//     requirements.requirement.nested-section-validation);
//   - a literal body whose heading names a declared child section of the target
//     is rejected, because children pass only through child arguments, never
//     through the helper body.
//
// A non-literal `fields`, `body` or child argument is a permitted pass-through
// and is not checked here: its keys, body and child sections are validated
// against the target's field, body and section rules when the target renders
// (field-rules, child-constructor). Every error
// carries the `template "<name>":` prefix naming the CALLING template (t.Name)
// — not the resolved target — even when the failure is about that target (an
// undefined name, a missing required field, a child section). The templates
// loader's path-qualification (checkLibraryBuild, templates-dir-validation
// step 4) therefore maps every failure back to the calling template's own
// template.yaml, across layers as well as within one.
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

	calls, err := NewSection(r.Lookup).HelperCalls(t)
	if err != nil {
		return err
	}
	for i := range calls {
		call := &calls[i]

		target, ok := r.Lookup(call.Name)
		if !ok || target == nil {
			msg := fmt.Sprintf("template %q: section helper call on layout line %d names %q, which is not a defined template", t.Name, call.Line, call.Name)
			if closest := suggest.Closest(call.Name, r.TemplateNames()); closest != "" {
				msg = fmt.Sprintf("%s: did you mean %q?", msg, closest)
			}
			return errors.New(msg)
		}
		if target.Usage == UsageSlide {
			return fmt.Errorf("template %q: section helper call on layout line %d names %q, which is a slide-usage template; the section helper requires a section-usage template", t.Name, call.Line, call.Name)
		}

		// A literal `dict` fields argument is checkable at load; an omitted
		// one supplies nothing; a non-literal expression is a pass-through.
		if call.FieldsLiteral || !call.HasFields {
			fields := make(map[string]struct{}, len(target.Fields))
			for fi := range target.Fields {
				fields[target.Fields[fi].Name] = struct{}{}
			}
			supplied := make(map[string]struct{}, len(call.FieldKeys))
			if call.FieldsLiteral {
				for _, key := range call.FieldKeys {
					if _, declared := fields[key]; !declared {
						return fmt.Errorf("template %q: section helper call on layout line %d supplies field %q, which section template %q does not declare", t.Name, call.Line, key, call.Name)
					}
					supplied[key] = struct{}{}
				}
			}
			for fi := range target.Fields {
				f := &target.Fields[fi]
				if !f.Required || f.Default != nil {
					continue
				}
				if _, present := supplied[f.Name]; !present {
					return fmt.Errorf("template %q: section helper call on layout line %d omits required field %q of section template %q, which has no default", t.Name, call.Line, f.Name, call.Name)
				}
			}
		}

		// Child-section arguments: each literal `child` is inspected here; a
		// `children` forwarding or any other non-literal child argument is a
		// permitted pass-through, deferred to render exactly like a non-literal
		// fields/body expression (child-constructor, children-forwarding,
		// section-helper-load-checks).
		if err := r.checkChildArgs(t, call, target); err != nil {
			return err
		}

		// A literal body is parsed at load: a heading naming a declared child
		// section of the target is the same rejection the body rule produces
		// when the body is validated, since children pass only through child
		// arguments, never through the helper body.
		if call.BodyLiteral {
			if name, named := bodyNamesChildSection(target, call.Body); named {
				return fmt.Errorf("template %q: section helper call on layout line %d supplies a body whose heading names child section %q of section template %q; the section helper passes no child sections", t.Name, call.Line, name, call.Name)
			}
		}
	}
	return nil
}

// checkChildArgs applies the static child-section checks of one `section` helper
// call to its target (requirements.requirement.section-helper-load-checks,
// requirement.child-constructor, requirement.children-forwarding,
// requirement.nested-section-validation).
//
// The target's child set is statically known when every child argument is a
// literal `child` call, nested `child` arguments likewise: then each of the
// target's declared sections has its min/max enforced over the literal set. A
// call that mixes literal `child` arguments with a `children` forwarding or any
// other non-literal child argument validates each literal child individually
// and skips the min/max counts, because the full set is known only at render.
func (r *Registry) checkChildArgs(t *Template, call *HelperCall, target *Template) error {
	staticallyKnown := allChildrenLiteral(call.Children)
	return r.checkChildSet(t, call, target, call.Children, staticallyKnown, true)
}

// allChildrenLiteral reports whether every child argument in the set — and
// every nested child argument, recursively — is a literal `child` call the
// load-time checks can inspect. A `children` forwarding or a `child` carrying a
// non-literal section, fields or body piece makes the set not statically known,
// so the enclosing call's min/max child counts are deferred to render.
func allChildrenLiteral(children []HelperChild) bool {
	for i := range children {
		c := &children[i]
		if !c.Literal {
			return false
		}
		if !allChildrenLiteral(c.Children) {
			return false
		}
	}
	return true
}

// checkChildSet validates one level of child arguments against parent's declared
// sections and, when checkCounts is true, enforces each declaration's min/max
// over the literal set. A non-literal child is skipped (a permitted
// pass-through); a literal child is fully checked and its own nested children
// recurse with the child's instantiated template as the new parent. topLevel
// distinguishes the call's own child set, whose empty-and-min>0 case keeps the
// historical "the section helper passes no child sections" message, from a
// nested child's set. caller is the template whose layout made the call and is
// what every error is path-qualified to.
func (r *Registry) checkChildSet(caller *Template, call *HelperCall, parent *Template, children []HelperChild, checkCounts, topLevel bool) error {
	counts := make(map[string]int, len(parent.Sections))
	for i := range children {
		c := &children[i]
		if !c.Literal {
			// A `children` forwarding of a runtime `.data` list, or any other
			// non-literal child argument, is not inspected at load.
			continue
		}

		decl, declared := declaredSection(parent, c.Section)
		if !declared {
			msg := fmt.Sprintf("template %q: section helper call on layout line %d: child names section %q, which section template %q does not declare", caller.Name, call.Line, c.Section, parent.Name)
			if closest := suggest.Closest(c.Section, declaredSectionNames(parent)); closest != "" {
				msg = fmt.Sprintf("%s: did you mean %q?", msg, closest)
			}
			return errors.New(msg)
		}

		tmplName, err := r.instantiatedChildTemplate(caller, call, parent, decl, c)
		if err != nil {
			return err
		}
		childTmpl, ok := r.Lookup(tmplName)
		if !ok || childTmpl == nil {
			msg := fmt.Sprintf("template %q: section helper call on layout line %d: child section %q of section template %q instantiates %q, which is not a defined template", caller.Name, call.Line, c.Section, parent.Name, tmplName)
			if closest := suggest.Closest(tmplName, r.TemplateNames()); closest != "" {
				msg = fmt.Sprintf("%s: did you mean %q?", msg, closest)
			}
			return errors.New(msg)
		}
		if childTmpl.Usage == UsageSlide {
			return fmt.Errorf("template %q: section helper call on layout line %d: child section %q of section template %q instantiates %q, which is a slide-usage template; sections accept only section-usage templates", caller.Name, call.Line, c.Section, parent.Name, tmplName)
		}

		if err := r.checkChildFields(caller, call, c, childTmpl); err != nil {
			return err
		}

		// A child body carries no child sections either: its nested children
		// pass through nested child arguments, so a heading naming one of the
		// instantiated template's declared child sections is rejected rather
		// than silently dropped, exactly as for the helper's own body
		// (child-constructor, body-rules).
		if c.BodyLiteral {
			if name, named := bodyNamesChildSection(childTmpl, c.Body); named {
				return fmt.Errorf("template %q: section helper call on layout line %d: child section %q supplies a body whose heading names child section %q of section template %q; a child body carries no child sections", caller.Name, call.Line, c.Section, name, childTmpl.Name)
			}
		}

		if err := r.checkChildSet(caller, call, childTmpl, c.Children, checkCounts, false); err != nil {
			return err
		}
		counts[c.Section]++
	}

	if !checkCounts {
		return nil
	}
	for si := range parent.Sections {
		d := &parent.Sections[si]
		n := counts[d.Name]
		if d.Max > 0 && n > d.Max {
			return fmt.Errorf("template %q: section helper call on layout line %d: section template %q declares child section %q with a maximum of %d, but the call passes %d", caller.Name, call.Line, parent.Name, d.Name, d.Max, n)
		}
		if d.Min > 0 && n < d.Min {
			if topLevel && len(children) == 0 {
				return fmt.Errorf("template %q: section helper call on layout line %d names section template %q, which declares child section %q with a minimum of %d; the section helper passes no child sections", caller.Name, call.Line, parent.Name, d.Name, d.Min)
			}
			return fmt.Errorf("template %q: section helper call on layout line %d: section template %q declares child section %q with a minimum of %d, but the call passes %d", caller.Name, call.Line, parent.Name, d.Name, d.Min, n)
		}
	}
	return nil
}

// instantiatedChildTemplate resolves the template a literal `child` argument
// instantiates from the declaration it fills: the reserved `template` key of
// the child's literal fields selects among decl.Accepted and is required iff
// the declaration accepts more than one; otherwise the declaration must accept
// exactly one and that one is instantiated. The selector is read from the
// child's fields but never counted as a declared field (checkChildFields
// removes it before the undeclared-key check). An unknown or non-string
// selector, and a declaration accepting no template, are errors naming the
// child section and the parent.
func (r *Registry) instantiatedChildTemplate(caller *Template, call *HelperCall, parent *Template, decl SectionDecl, c *HelperChild) (string, error) {
	if len(decl.Accepted) == 0 {
		return "", fmt.Errorf("template %q: section helper call on layout line %d: child section %q of section template %q accepts no section template", caller.Name, call.Line, c.Section, parent.Name)
	}

	selector, hasSelector := "", false
	if c.Fields != nil {
		if raw, present := c.Fields["template"]; present {
			hasSelector = true
			s, ok := raw.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return "", fmt.Errorf("template %q: section helper call on layout line %d: child section %q of section template %q: key %q must name a section template", caller.Name, call.Line, c.Section, parent.Name, "template")
			}
			selector = s
		}
	}
	if !hasSelector {
		if len(decl.Accepted) > 1 {
			return "", fmt.Errorf("template %q: section helper call on layout line %d: child section %q of section template %q accepts more than one template; a %q selector is required", caller.Name, call.Line, c.Section, parent.Name, "template")
		}
		return decl.Accepted[0], nil
	}
	if !containsString(decl.Accepted, selector) {
		return "", fmt.Errorf("template %q: section helper call on layout line %d: child section %q of section template %q does not accept template %q (accepts %s)", caller.Name, call.Line, c.Section, parent.Name, selector, strings.Join(decl.Accepted, ", "))
	}
	return selector, nil
}

// checkChildFields applies the literal-fields half of the static child checks:
// every literal key the child supplies (the reserved `template` selector
// removed) must be a field the instantiated template declares, and every
// required field of the instantiated template must be supplied or carry a
// default. An omitted fields argument and a literal empty `dict` are both
// checked (the target's defaults apply in the latter case exactly as they do
// for the `section` helper's own fields).
func (r *Registry) checkChildFields(caller *Template, call *HelperCall, c *HelperChild, childTmpl *Template) error {
	declared := make(map[string]struct{}, len(childTmpl.Fields))
	for fi := range childTmpl.Fields {
		declared[childTmpl.Fields[fi].Name] = struct{}{}
	}
	supplied := make(map[string]struct{}, len(c.Fields))
	for key := range c.Fields {
		if key == "template" {
			// The selector is consumed, never a declared field a child sets.
			continue
		}
		if _, ok := declared[key]; !ok {
			return fmt.Errorf("template %q: section helper call on layout line %d: child section %q supplies field %q, which section template %q does not declare", caller.Name, call.Line, c.Section, key, childTmpl.Name)
		}
		supplied[key] = struct{}{}
	}
	for fi := range childTmpl.Fields {
		f := &childTmpl.Fields[fi]
		if !f.Required || f.Default != nil {
			continue
		}
		if _, present := supplied[f.Name]; !present {
			return fmt.Errorf("template %q: section helper call on layout line %d: child section %q omits required field %q of section template %q, which has no default", caller.Name, call.Line, c.Section, f.Name, childTmpl.Name)
		}
	}
	return nil
}

// declaredSectionNames returns the names of the sections t declares, in
// declaration order, for a closest-match "did you mean …?" suggestion when a
// child names an undeclared section. It is nil for a nil template.
func declaredSectionNames(t *Template) []string {
	if t == nil {
		return nil
	}
	names := make([]string, 0, len(t.Sections))
	for i := range t.Sections {
		names = append(names, t.Sections[i].Name)
	}
	return names
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
