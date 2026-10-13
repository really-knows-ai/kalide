package template

import (
	"fmt"
	htmltemplate "html/template"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/mdcheck"
)

// This file implements checkLibraryExamples, templates-dir-validation step 7:
// each template's example.md is validated against its parsed Definition
// (checkLibraryBuild, step 4, must have already filled that in) and its
// layout is executed. A schema violation or a layout execute error is
// reported as a *LibraryError positioned at the example or layout's file:line
// (template-manifest, template-language).
//
// A template that declares no fields and no sections has nothing for the
// schema-only checks to validate; its example.md is still required to exist
// (step 3) but its content is not shape-checked here, and its layout is still
// executed against a safe, empty render context. This keeps a minimal
// template (no schema at all) valid without requiring a particular
// example.md shape.

// checkLibraryExamples is templates-dir-validation step 7: validating each
// template's example.md against its parsed definition and executing its
// layout. It runs once over a merged Library after every layer has loaded.
//
// Each example executes against the merged per-layer layout namespace
// exampleLayoutNamespace builds from every slide and section of the merged
// Library — the full layout set render's namespaceForLayer parses from the
// registry, bound with the executing template's own layer's media and the
// calling instance's fields — rather than the single target layout text alone.
// Parsing the whole set into one shared namespace makes a layout's standard
// `{{ template "<layout name>" . }}` composition call resolve to any other
// library layout, whichever layer contributed it (example-layout-composition).
//
// For each template it resolves that template's own owning *LibraryLayer by
// identity over lib.Layers — resolveMergedSlideLayer for a slide and
// resolveMergedSectionTarget for a section, the same lookups
// NewRegistryFromLibrary uses to record LayerOf provenance — and passes the
// layer to checkLibraryExample, so the executing layout's `media` helper
// resolves in the layer that owns the template rather than the merged
// Library's primary media (library-layer-references). A single-layer Library,
// or a template whose owning layer cannot be identified, passes a nil layer
// and falls back to lib.Media — the pre-layer behaviour, unchanged.
func checkLibraryExamples(lib *Library) error {
	if lib == nil {
		return nil
	}
	for _, name := range sortedLibraryTemplateNames(lib.Slides) {
		if err := checkLibraryExample(lib, lib.Slides[name], resolveMergedSlideLayer(lib, name)); err != nil {
			return err
		}
	}
	for _, name := range sortedLibraryTemplateNames(lib.Sections) {
		_, layer, _ := resolveMergedSectionTarget(lib, name)
		if err := checkLibraryExample(lib, lib.Sections[name], layer); err != nil {
			return err
		}
	}
	return nil
}

// checkLibraryExample validates lt's example.md against lt.Definition and
// executes lt.Layout against the render context that example produces.
//
// A container template's example.md may declare nested instances (template-
// build-checks, nested-section-validation): validateExampleBlock parses the
// example by heading depth and validates every instance recursively against
// its own resolved template, reporting a violation with the full containment
// path (`columns[2] › blocks[1]`). Each block's body is checked against its own
// enclosing template — including whether that template declares child sections
// and its body.subheadings rule — so a container's headings are consumed as
// nested instances, never treated as subheadings, and a no-children template's
// `##`/`###` are governed by its body.subheadings. This keeps example
// validation in agreement with deck validation.
//
// After validation the reserved source contexts are published into the example
// execution context alongside deck/slide (raw-source-context,
// section-data-context, template-context, item-context): `.raw` is the
// example's original source field values and body (RawContext) and
// `.data` is its authored child sections as data (exampleContextNodes adapted
// to the neutral tree and walked by DataContext), both mirroring the
// render-time rawContext/dataContext, so an unguarded
// `{{ section "…" … .raw.body }}` or `{{ .data.<section> }}` access executes
// at step 7 instead of failing LoadLibrary. A section template's own layout is
// itself a section instance, so it additionally gets the one-item `.item`
// descriptor mirroring a section-helper invocation; a slide template's item
// stays absent.
//
// layer is lt's own owning *LibraryLayer when the merged Library records one
// (checkLibraryExamples resolves it by identity over lib.Layers), and nil for a
// single-layer Library or a template whose owning layer cannot be identified.
// The layer supplies the media sub-filesystem the layout's `media` helper
// resolves against, so a template contributed by a non-primary layer resolves
// its own `{{ media … }}` reference in that layer's media rather than the
// merged Library's primary media (library-layer-references); a nil layer falls
// back to lib.Media, the pre-layer behaviour.
func checkLibraryExample(lib *Library, lt *LibraryTemplate, layer *LibraryLayer) error {
	if lt == nil || lt.Definition == nil {
		return nil
	}
	def := lt.Definition
	resolve := func(name string) (*Template, bool) {
		other, _, ok := lib.TemplateByName(name)
		if !ok || other == nil || other.Definition == nil {
			return nil, false
		}
		return other.Definition, true
	}

	ctx := map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}

	// A section template's layout is itself a section instance, so a
	// `{{ section … }}` call it makes reads this example's field values as its
	// `.item.parent` (item-context). A slide template's parent is the slide, not
	// a section, so its calls carry nil. The `template` key is the section
	// instance's template selector, not a field, so it is dropped here exactly
	// as validateExampleBlock drops it from the executed context.
	var callerFields map[string]any
	// callerChain is the example instance's own chain of resolved template
	// names — the resolved templates enclosing the instance whose layout
	// executes, with its own resolved template name appended. A section
	// template's example is the OUTERMOST instance of its own template —
	// example.md belongs to lt, and nothing encloses it — so its chain is just
	// lt's name, and the layout's `{{ section … }}` calls pass it as the target's
	// enclosing chain (a target resolving to the same template reads
	// `.item.depth` 1), exactly as the render side computes it through the shared
	// InstanceDepth helper (item-context, bounded-self-nesting). The example's
	// authored children are enclosed by the same instance, so `.data` walks them
	// with the same chain. A slide template's example is the slide itself, not an
	// instance of any template, so its top-level instances keep an empty
	// enclosing chain and a call from its layout carries no caller.
	var callerChain []string
	if len(def.Fields) > 0 || len(def.Sections) > 0 {
		block, err := parseExampleBlock(string(lt.ExampleBytes), lt.Kind, 1)
		if err != nil {
			return withPath(err, lt.ExamplePath)
		}
		if err := validateExampleBlock(lt.ExamplePath, def, block, resolve, ctx); err != nil {
			return err
		}

		// The reserved source contexts, mirroring the render-time
		// rawContext/dataContext. source is the example frontmatter minus the
		// reserved `template` selector, exactly as validateExampleBlock drops
		// it from the executed context.
		source := make(map[string]any, len(block.Frontmatter))
		for k, v := range block.Frontmatter {
			if k == "template" {
				continue
			}
			source[k] = v
		}
		ctx["raw"] = RawContext(source, def, block.Body)
		if lt.Kind == KindSection {
			callerChain = []string{lt.Name}
		}
		ctx["data"] = DataContext(exampleContextNodes(block.Sections, def, resolve), callerChain, nil)

		if lt.Kind == KindSection {
			// A section template's own layout is a section instance, so its
			// `.item` descriptor mirrors a one-item section-helper invocation:
			// section and template are the template's own name, depth 0 (it is
			// the outermost instance of its template, the shared InstanceDepth
			// of an empty enclosing chain), parent nil (item-context). A slide
			// template's item stays absent.
			callerFields = source
			ctx["item"] = map[string]any{
				"index":    0,
				"number":   1,
				"count":    1,
				"first":    true,
				"last":     true,
				"section":  lt.Name,
				"template": lt.Name,
				"depth":    InstanceDepth(nil, lt.Name),
				// A typed nil map, matching the shape itemContext,
				// ResolveSectionCall and DataContext publish for an
				// absent parent: `{{if .parent}}` is false and a chained
				// `.parent.<field>` evaluates to no value rather than failing.
				"parent": map[string]any(nil),
			}
		}
	}

	// Execute lt.Layout against the merged library layout namespace
	// (exampleLayoutNamespace): every library layout text parsed together,
	// keyed by layout name, with step 3's `section` stub overridden by the
	// load-time helper so a layout that calls `{{ section … }}` renders the
	// target instead of failing LoadLibrary's step-7 execution, and with the
	// `media` helper bound to lt's own owning layer when one is known so an
	// external-layer template's own `{{ media … }}` reference resolves in the
	// external layer's media rather than the merged Library's primary media
	// (templates-dir-validation step 7, library-layer-references), and with the
	// `section` helper bound to the example instance's own chain so a
	// `{{ section … }}` call the layout makes computes its target's
	// `.item.depth` through the shared InstanceDepth helper (item-context,
	// bounded-self-nesting). Parsing the whole set — rather than lt.LayoutText
	// alone — keeps the shared namespace,
	// so a layout's `{{ template "<layout name>" . }}` composition call
	// resolves too (example-layout-composition).
	// The shared namespace holds every library layout, so an error prefix may
	// name any of them; resolve each namespace name to its own on-disk path
	// rather than assuming the invoking template's layout.
	layoutPaths := libraryLayoutPathResolver(lib)
	namespace, err := exampleLayoutNamespace(lib, layer, callerFields, callerChain...)
	if err != nil {
		return layoutTemplateError(lt.Definition.Layout, layoutPaths, "parse", err)
	}

	layoutName := lt.Layout.Name()
	if layoutName == "" {
		layoutName = lt.Name
	}
	var buf strings.Builder
	if err := namespace.ExecuteTemplate(&buf, layoutName, ctx); err != nil {
		return layoutTemplateError(lt.Definition.Layout, layoutPaths, "execute", err)
	}
	return nil
}

// exampleLayoutNamespace builds the html/template namespace a library layout
// executes against at the load-time example check: the load-time mirror of the
// render-time renderer.namespaceForLayer (example-layout-composition,
// library-layer-references).
//
// Every library layout is parsed into one shared namespace, keyed by its layout
// name, so a layout's standard `{{ template "<layout name>" . }}` composition
// call resolves at step 7 instead of failing with "no such template" — the bug
// this helper fixes, where executing the single target layout text in a fresh
// namespace left no other template to invoke. The namespace holds every slide
// and every section of the merged Library, whichever layer contributed it, so
// the set is exactly the layout set render's namespaceForLayer parses from the
// registry (Registry.Templates) — the union of the layers.
//
// Every library layout is keyed by the loader-built layer-relative layout name
// (lt.Layout.Name()), the layout's forward-slash path within its own layer
// (`slides|sections/<name>/layout.html.tmpl`, loadlibrary.go) — identical on
// every OS and wherever the layer sits on disk — falling back to lt.Name when
// the loader did not set one, the same lt.Layout.Name()-else-lt.Name fallback
// layoutName uses in render.go. Because each layer's layouts keep their own
// layer-relative name, a two-layer deck resolves a cross-layer include by the
// target's own layer-relative name (library-layer-references). A failure's
// position is display-only and comes from the offending layout's on-disk
// LayoutPath, never from its layer-relative key.
//
// layer is the example-checked template's own owning *LibraryLayer, when the
// merged Library records one; it supplies the media sub-filesystem every
// layout's `media` helper resolves against, so each `{{ media … }}` reference
// binds to that layer's media rather than the merged Library's primary media
// (library-layer-references). A nil layer — a single-layer Library, or no
// recorded owner — falls back to lib.Media, the pre-layer behaviour.
// callerFields is the calling instance's field values, bound as the `section`
// helper's parent exactly as checkLibraryExample binds it (item-context); a
// slide template passes nil. callerChain is the calling instance's own chain of
// resolved template names — the resolved templates enclosing the instance whose
// layout executes, with the instance's own resolved template name appended (the
// example instance is the outermost instance of its own template, so a section
// template's chain is just its own name and a slide template, the slide itself,
// has none) — forwarded to the `section` helper so a `{{ section … }}` call the
// layout makes reads its target's `.item.depth` through the shared InstanceDepth
// helper exactly as the render side does (item-context, bounded-self-nesting); a
// caller with no instance to name passes none. Parsing re-parses the layout
// sources rather than cloning an already-parsed namespace because html/template
// resolves a func map at parse time and cannot rebind `media`/`section` on a
// parsed set.
//
// The returned namespace's root template is "layouts"; a caller executes a
// layout with ExecuteTemplate under that layout's name. A library template with
// no layout text is an error, mirroring render.namespaceForLayer.
func exampleLayoutNamespace(lib *Library, layer *LibraryLayer, callerFields map[string]any, callerChain ...string) (*htmltemplate.Template, error) {
	if lib == nil {
		return nil, fmt.Errorf("example layout namespace: no library")
	}
	mediaFS := lib.Media
	if layer != nil {
		mediaFS = layer.Media
	}
	funcMap := LayoutFuncMap(mediaFS, "/"+MediaDir)
	funcMap["section"] = exampleSectionHelper(lib, callerFields, callerChain...)

	namespace := htmltemplate.New("layouts").Funcs(funcMap)
	add := func(lt *LibraryTemplate) error {
		if lt == nil {
			return nil
		}
		name := lt.Name
		if lt.Layout != nil && lt.Layout.Name() != "" {
			name = lt.Layout.Name()
		}
		if lt.LayoutText == "" {
			return fmt.Errorf("template %q has no layout text", name)
		}
		if _, err := namespace.New(name).Parse(lt.LayoutText); err != nil {
			return fmt.Errorf("template %q: parse layout: %w", name, err)
		}
		return nil
	}

	for _, name := range sortedLibraryTemplateNames(lib.Slides) {
		if err := add(lib.Slides[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range sortedLibraryTemplateNames(lib.Sections) {
		if err := add(lib.Sections[name]); err != nil {
			return nil, err
		}
	}
	return namespace, nil
}

// exampleContextNodes adapts the load-time example tree into the neutral
// []ContextNode the shared DataContext walks (section-data-context,
// raw-source-context, item-context, template-context). It replaces the old
// load-time `.data` builder: it no longer builds the `.data` map itself, only
// the neutral tree, which DataContext then walks — the same walk the render
// side performs over its own slide.Section tree (mapped through a parallel
// adapter) — so the load-time and render-time `.data` views cannot drift. A
// node's `raw` view is built by RawContext from its Source, Body and Template
// when DataContext walks the tree.
//
// sections is one parent's authored child sections in source order, exactly as
// parseExampleBlock produces them (exampleBlock.Sections); def is the parent's
// resolved template, whose declarations name the accepted templates an instance
// without an explicit `template:` selector resolves to; resolve looks up a
// template by name. A nil resolve yields unresolved nodes rather than a panic.
//
// Each node's Template is the instance's resolved template — an explicit
// `template:` selector, otherwise the single accepted template when the section
// accepts exactly one, the same resolution validateExampleBlock enforces. When
// that names a template but resolve cannot return it (a nil resolver, or a name
// outside the library), the node still carries a name-only template, so
// DataContext's `item.template` label stays the declared name instead of
// dropping to "" — matching this builder's previous item.template. A name-only
// template declares no fields, so its source view copies none, exactly as an
// unresolved template did before.
//
// Only authored instances become nodes: a section invoked by the `section`
// helper is a direct call, never part of the parsed tree, so it is absent from
// `.data` exactly as it is absent from the rendered-HTML list.
func exampleContextNodes(sections []exampleSection, def *Template, resolve func(string) (*Template, bool)) []ContextNode {
	if len(sections) == 0 {
		return nil
	}
	sect := NewSection(resolve)
	nodes := make([]ContextNode, len(sections))
	for i := range sections {
		sec := &sections[i]
		// The instance's resolved template: an explicit `template:` selector,
		// otherwise the single accepted template when the section accepts
		// exactly one — the same resolution validateExampleBlock enforces.
		name, named := exampleTemplateName(sec.Frontmatter)
		if !named {
			if d, ok := sect.Declared(def, sec.Name); ok && len(d.Accepted) == 1 {
				name = d.Accepted[0]
			}
		}
		var tmpl *Template
		if resolve != nil {
			tmpl, _ = resolve(name)
		}
		// A name the resolver could not return still labels the item, as the
		// previous builder did; a name-only template declares no fields, so
		// the source view copies none, exactly as before.
		if tmpl == nil && name != "" {
			tmpl = &Template{Name: name}
		}
		nodes[i] = ContextNode{
			Name:     sec.Name,
			Index:    sec.Index,
			Template: tmpl,
			Source:   sec.Frontmatter,
			Body:     sec.Body,
			Children: exampleContextNodes(sec.Sections, tmpl, resolve),
		}
	}
	return nodes
}

// exampleSectionHelper is the template-local `section` func the load-time
// example execution binds (templates-dir-validation step 7). It is a factory
// returning the func bound into a layout's func map, closing over lib and the
// calling instance's field values — callerFields is nil when the call is made
// from a slide layout, whose parent is the slide, not a section
// (item-context). It lives here rather than in internal/render because
// internal/template cannot import internal/render; the render-time renderer
// binds its own equivalent.
//
// callerChain is the calling instance's own chain of resolved template names —
// the resolved templates enclosing the instance whose layout executes, with
// that instance's own resolved template name appended (its ancestor chain plus
// itself). It is the ancestor identity the call passes to
// ResolveSectionCallLayered, so the resolved target's one-item `.item.depth`
// counts its enclosing same-template ancestors through the shared InstanceDepth
// helper exactly as the render-time helper computes it (item-context,
// bounded-self-nesting): a call from a slide layout, which is not an instance
// of any template, passes none and has depth 0, while a call whose caller (or
// its ancestors) resolves to the target name counts each. A caller with no
// instance to name passes none.
//
// The returned func parses the call's positional arguments — the optional
// fields map, the optional body and the optional child arguments (each the
// *ChildRequest a `child`/`children` constructor built, exampleSectionCallArgs)
// — and resolves the call with ResolveSectionCallLayered: the same
// resolution/validation the render-time helper reuses, so the target must be a
// section-usage template, the call's fields are defaults-first validated with
// CheckValues, the body is validated with CheckBody, a body heading naming one
// of the target's child sections is rejected, and the passed children are
// resolved against the target's declared sections — accepted-template, defaults,
// field/body rules and per-declaration min/max, recursively
// (child-constructor, children-forwarding, nested-section-validation). layerOf
// is the library-based provenance lookup (resolveMergedSectionTarget over lib),
// so the returned SectionCall carries the target's owning layer.
//
// The target's execution context is built by exampleSectionRunner.render, which
// mirrors checkLibraryExample's slide/section context so the target sees a
// consistent shape: the effective field values as top-level entries, the
// reserved `deck`/`slide` entries (the empty example stand-ins), the resolved
// `.item` descriptor (index 0, number/count 1, first/last true, section/template
// the target name, depth the shared InstanceDepth of the caller's own chain
// against the target name, parent callerFields), the reserved `.raw` source view
// of what the call supplied plus the supplied body (RawContext, the shared
// source-view builder both halves use), and the reserved `.data` entry built by
// the shared DataContext over the call's resolved children, so an unguarded
// `{{ .data.<child> }}` and a nested `children "<section>" .data.<section>`
// forwarding execute at load exactly as at render time (section-data-context,
// children-forwarding). Each resolved child is rendered through that same runner
// and its HTML appended to the target's rendered child-section list keyed by the
// child's declared section, the list a target layout ranges over to splice child
// markup (template-language). A child-less call passes no children, so its
// `.data` is the shared builders' empty map and its rendered child lists are
// empty — the previous behaviour. The body is set only when the call supplies
// one.
//
// The target's layout is re-parsed under LayoutFuncMap with the `section` entry
// overridden by this same factory bound to the target's own effective values and
// own chain (the caller's chain with the target's resolved name appended), so a
// target layout that itself calls `{{ section … }}` both parses and executes —
// the nested call's `.item.parent` is this target's values and its `.item.depth`
// counts the target (and any same-template ancestor) through the shared
// InstanceDepth helper — and with the `media` helper bound to the target's OWN
// owning layer (call.Layer.Media), so
// an external-layer section's `{{ media … }}` reference resolves in the external
// layer's media rather than the merged Library's primary media
// (library-layer-references). When the target's owning layer is unknown
// (call.Layer nil — a single-layer Library, or no recorded provenance) the
// helper falls back to lib.Media, the pre-layer behaviour. The returned value
// is the target's rendered HTML as html/template.HTML, trusted exactly as the
// renderer returns it, so the caller's layout splices it unescaped.
func exampleSectionHelper(lib *Library, callerFields map[string]any, callerChain ...string) func(name string, args ...any) (htmltemplate.HTML, error) {
	resolve := func(name string) (*Template, bool) {
		other, _, ok := lib.TemplateByName(name)
		if !ok || other == nil || other.Definition == nil {
			return nil, false
		}
		return other.Definition, true
	}
	// layerOf is the library-based owning-layer provenance lookup, the same one
	// NewRegistryFromLibrary uses to record LayerOf: a resolved section target's
	// own *LibraryLayer over the merged lib.Layers, or nil when no layer owns it
	// (a single-layer Library, or a Library carrying no Layers). It lets
	// ResolveSectionCallLayered carry the target's layer out on SectionCall.
	layerOf := func(name string) *LibraryLayer {
		_, layer, _ := resolveMergedSectionTarget(lib, name)
		return layer
	}

	// callerChain is the calling instance's own chain of resolved template
	// names (its ancestors plus its own resolved template name), handed to
	// ResolveSectionCallLayered as the target's enclosing chain so the target's
	// `.item.depth` counts its enclosing same-template ancestors through the
	// shared InstanceDepth helper (item-context, bounded-self-nesting), and
	// carried on the runner so a nested call it renders derives the target's own
	// chain from it.
	runner := &exampleSectionRunner{lib: lib, resolve: resolve, layerOf: layerOf, callerChain: append([]string(nil), callerChain...)}

	return func(name string, args ...any) (htmltemplate.HTML, error) {
		fields, body, children, err := exampleSectionCallArgs(name, args)
		if err != nil {
			return "", err
		}

		call, err := ResolveSectionCallLayered(resolve, layerOf, name, fields, body, runner.callerChain, callerFields, children...)
		if err != nil {
			return "", fmt.Errorf("section helper %q: %w", name, err)
		}

		rendered, err := runner.render(call)
		if err != nil {
			return "", fmt.Errorf("section helper %q: %w", name, err)
		}
		return rendered, nil
	}
}

// exampleSectionCallArgs parses a `{{ section "<name>" [fields] [body] [child…] }}`
// call's execution arguments, the same positional grammar the render-time
// sectionHelper accepts and the loader's static extractor reads (section-helper,
// child-constructor, children-forwarding). args is the call's argument list
// after the target name:
//
//   - args[0] is the optional fields map (a map[string]any, or nil when the
//     placeholder is omitted);
//   - args[1] is the optional body string (a string, or nil);
//   - args[2:] are the optional child arguments, each the *ChildRequest a
//     `child`/`children` constructor built;
//
// a fields argument of the wrong type or a body argument of the wrong type is a
// programming error naming the call, matching the previous behaviour, and so is
// a child argument that is neither nil nor a *ChildRequest. A nil child slot is
// skipped so an all-placeholder call still parses; the returned children slice
// is nil when the call passes none, so a child-less call is byte-identical to
// before.
func exampleSectionCallArgs(name string, args []any) (map[string]any, string, []*ChildRequest, error) {
	var fields map[string]any
	if len(args) >= 1 && args[0] != nil {
		m, ok := args[0].(map[string]any)
		if !ok {
			return nil, "", nil, fmt.Errorf("section helper %q: fields must be a map, got %T", name, args[0])
		}
		fields = m
	}
	var body string
	if len(args) >= 2 && args[1] != nil {
		s, ok := args[1].(string)
		if !ok {
			return nil, "", nil, fmt.Errorf("section helper %q: body must be a string, got %T", name, args[1])
		}
		body = s
	}
	var children []*ChildRequest
	for i := 2; i < len(args); i++ {
		if args[i] == nil {
			continue
		}
		c, ok := args[i].(*ChildRequest)
		if !ok {
			return nil, "", nil, fmt.Errorf("section helper %q: child argument %d must be a child value, got %T", name, i-1, args[i])
		}
		children = append(children, c)
	}
	return fields, body, children, nil
}

// exampleSectionRunner executes a resolved *SectionCall's target layout and its
// child instances at load time, mirroring the render-time renderer's
// renderSection/values context (section-helper, section-data-context,
// item-context, raw-source-context, template-context, library-layer-references).
// Its resolve/layerOf pair is exampleSectionHelper's library lookup, reused so a
// child's own template and owning layer are already resolved on each
// *ResolvedChild.
type exampleSectionRunner struct {
	lib     *Library
	resolve func(name string) (*Template, bool)
	layerOf func(name string) *LibraryLayer

	// callerChain is the calling instance's own chain of resolved template
	// names — the resolved templates enclosing the instance whose layout the
	// runner renders, with that instance's own resolved template name appended
	// (its ancestor chain plus itself). It is the enclosing chain a `section`
	// call made from that layout passes as its target's ancestors, so the
	// target's `.item.depth` is computed through the shared InstanceDepth helper
	// exactly as at render time (item-context, bounded-self-nesting). A slide
	// layout, which is not an instance of any template, carries none.
	callerChain []string
}

// render executes call's target layout with the context the render-time helper
// gives the same resolved call and returns its trusted HTML.
//
// The context mirrors checkLibraryExample's slide/section context so the target
// sees a consistent shape: its effective field values as top-level entries —
// every declared field present, an absent optional field with no default as a
// nil entry exactly as the render-time helper's values conversion presents it,
// so a layout cannot tell an absent optional from an explicit null
// (nullable-fields) — the reserved `deck`/`slide` entries (the empty example
// stand-ins), the resolved `.item` descriptor (parent callerFields, or the
// parent target's values for a child), the reserved `.raw` source view built by
// the shared RawContext from the call's AUTHORED Source — the supplied fields
// before the target's defaults, so a default never leaks into `.raw` as an
// authored value (raw-source-context) — and the reserved `.data` entry built by
// the shared DataContext over the call's resolved children with the target's own
// resolved-template chain as their ancestors, so each `.data` item's depth
// counts its enclosing same-template ancestors through the shared InstanceDepth
// helper over exactly the chain the children were resolved with (item-context,
// section-data-context) and an unguarded
// `{{ .data.<child> }}` (and a nested `children "<section>" .data.<section>`
// forwarding its entries) executes at load exactly as at render time
// (section-data-context, children-forwarding). Each resolved child is rendered
// through this same method and its HTML appended to the target's rendered
// child-section list keyed by its declared section name, the list a target
// layout ranges over to splice child markup (template-language). The body is
// set only when the call supplies one.
//
// The target's layout is re-parsed under LayoutFuncMap with the `section` entry
// overridden by exampleSectionHelper bound to the target's own effective values
// and own chain — the runner's caller chain with the target's resolved name
// appended — so a target layout that itself calls `{{ section … }}` both parses
// and executes, the nested call's `.item.parent` being this target's values and
// its `.item.depth` counting the target (and any same-template ancestor) through
// the shared InstanceDepth helper, and with the `media` helper bound to the
// target's OWN owning layer
// (call.Layer.Media) when one is known (library-layer-references); a nil layer
// falls back to lib.Media, the pre-layer behaviour.
func (ex *exampleSectionRunner) render(call *SectionCall) (htmltemplate.HTML, error) {
	ctx := make(map[string]any, len(call.Values)+len(call.Template.Fields)+6)
	for k, v := range call.Values {
		ctx[k] = v
	}
	// Every declared field is present in the target's context, exactly as the
	// render-time helper's values conversion presents it: an absent optional
	// field with no default has no entry in call.Values (defaults-first
	// resolution leaves it out), yet values exposes it as a nil entry, so a
	// layout cannot tell an absent optional from an explicit null
	// (nullable-fields). Mirror that here so the load-time and render-time
	// target contexts agree.
	for i := range call.Template.Fields {
		if _, present := ctx[call.Template.Fields[i].Name]; !present {
			ctx[call.Template.Fields[i].Name] = nil
		}
	}
	ctx["deck"] = emptyExampleDeckContext()
	ctx["slide"] = emptyExampleSlideContext()
	ctx["item"] = call.Item
	// The target is both the parent of the call's resolved children and the
	// calling instance for any `{{ section … }}` call its own layout makes: its
	// own chain of resolved template names is the runner's caller chain with the
	// target's resolved name appended (its ancestor chain plus itself). Passing
	// that chain as the children's ancestors makes each `.data` item's depth
	// count its enclosing same-template ancestors through the shared
	// InstanceDepth helper over exactly the chain ResolveSectionCallLayered
	// resolved the children with, so a `.data` item and the child's own `.item`
	// descriptor agree (item-context, section-data-context, bounded-self-nesting);
	// giving it to the nested `section` helper lets a nested call compute its
	// target's `.item.depth` identically.
	targetChain := make([]string, 0, len(ex.callerChain)+1)
	targetChain = append(targetChain, ex.callerChain...)
	targetChain = append(targetChain, call.Template.Name)

	// `.raw` is the instance's authored source view — RawContext over the
	// call's Source (supplied fields, before defaults) — and, exactly as
	// renderSection passes secCtx.raw, it is also the parent each child's
	// `.data` entry carries as `.item.parent`, so the load-time and render-time
	// data views agree (raw-source-context, section-data-context).
	raw := RawContext(call.Source, call.Template, call.Body)
	ctx["raw"] = raw
	ctx["data"] = DataContext(exampleChildNodes(call.Children), targetChain, raw)
	if strings.TrimSpace(call.Body) != "" {
		ctx["body"] = call.Body
	}
	if err := ex.renderChildLists(ctx, call.Children); err != nil {
		return "", err
	}

	mediaFS := ex.lib.Media
	if call.Layer != nil {
		mediaFS = call.Layer.Media
	}
	funcMap := LayoutFuncMap(mediaFS, "/"+MediaDir)
	funcMap["section"] = exampleSectionHelper(ex.lib, call.Values, targetChain...)

	layoutName := call.Template.Layout.Name
	if layoutName == "" {
		layoutName = call.Template.Name
	}
	// The target layout is re-parsed alone, so its name is the namespace key a
	// Go error prefix carries; map it to the target's own on-disk path.
	layoutPaths := namespaceLayoutResolver(layoutName, call.Template.Layout.Path)
	parsed, err := htmltemplate.New(layoutName).Funcs(funcMap).Parse(call.Template.Layout.Text)
	if err != nil {
		return "", layoutTemplateError(call.Template.Layout, layoutPaths, "parse target layout", err)
	}
	var buf strings.Builder
	if err := parsed.Execute(&buf, ctx); err != nil {
		return "", layoutTemplateError(call.Template.Layout, layoutPaths, "execute target layout", err)
	}
	return htmltemplate.HTML(buf.String()), nil
}

// renderChildLists renders every resolved child of call, deepest first, and
// appends each child's trusted HTML to ctx under the child's declared section
// name — the rendered child-section list a target layout splices with
// `{{ range .<section> }}{{ . }}{{ end }}`, mirroring the render-side
// renderChildren list (template-language, section-data-context). A child whose
// layout fails to execute is reported with the child's section name so the error
// stays path-qualified.
func (ex *exampleSectionRunner) renderChildLists(ctx map[string]any, children []*ResolvedChild) error {
	for _, child := range children {
		if child == nil || child.Call == nil {
			continue
		}
		name := exampleChildName(child.Call)
		rendered, err := ex.render(child.Call)
		if err != nil {
			return fmt.Errorf("child %q: %w", name, err)
		}
		list, _ := ctx[name].([]htmltemplate.HTML)
		ctx[name] = append(list, rendered)
	}
	return nil
}

// exampleChildNodes maps the resolved child tree of a section-helper call onto
// the neutral []ContextNode the shared DataContext walks, so the call's target
// exposes its passed children as data exactly as an authored parent exposes its
// child instances (section-data-context, children-forwarding). Each node's
// Template is the child's resolved template, its Name/Index the child's declared
// section and sibling position from its resolved `.item`, its Source the child's
// AUTHORED supplied fields (ResolvedChild.Source, the fields before the
// instantiated template's defaults, so a default never appears in the child's
// `.raw`) and its Body the child's body; nested resolved children recurse. The
// order is the resolved passed order, so sibling index/count match the `.item`
// descriptors ResolveChildren assigned.
func exampleChildNodes(children []*ResolvedChild) []ContextNode {
	if len(children) == 0 {
		return nil
	}
	nodes := make([]ContextNode, 0, len(children))
	for _, child := range children {
		if child == nil || child.Call == nil {
			continue
		}
		call := child.Call
		nodes = append(nodes, ContextNode{
			Name:     exampleChildName(call),
			Index:    exampleChildIndex(call),
			Template: call.Template,
			Source:   child.Source,
			Body:     call.Body,
			Children: exampleChildNodes(child.Children),
		})
	}
	return nodes
}

// exampleChildName returns the declared section name a resolved child fills:
// its `.item.section`, falling back to the instantiated template's name when the
// descriptor is absent, so the child's `.data` key and rendered-list key match
// its declared section.
func exampleChildName(call *SectionCall) string {
	if call == nil {
		return ""
	}
	if call.Item != nil {
		if s, ok := call.Item["section"].(string); ok && s != "" {
			return s
		}
	}
	if call.Template != nil {
		return call.Template.Name
	}
	return ""
}

// exampleChildIndex returns the zero-based sibling index a resolved child fills
// from its `.item.index`, or 0 when the descriptor is absent.
func exampleChildIndex(call *SectionCall) int {
	if call == nil || call.Item == nil {
		return 0
	}
	if i, ok := call.Item["index"].(int); ok {
		return i
	}
	return 0
}

// exampleBlock is one parsed `---`/fence-delimited frontmatter plus body plus
// nested named section instances, either a whole example.md (slide or
// section) or one heading-delimited section instance within one.
type exampleBlock struct {
	// Frontmatter is the parsed YAML mapping, or nil when the block carries
	// none.
	Frontmatter map[string]any

	// FrontmatterLine is the 1-based file line the frontmatter starts on, 0
	// when Frontmatter is nil.
	FrontmatterLine int

	// Body is the Markdown after the frontmatter, up to the first child
	// heading (or the whole remainder when there is none).
	Body string

	// BodyLine is Body's 1-based starting file line.
	BodyLine int

	// Sections holds each section instance in source order, excluding a
	// reserved `# notes` section.
	Sections []exampleSection
}

// exampleSection is one heading-delimited instance within an exampleBlock: the
// section name, the heading depth that opened it, its zero-based instance index
// among siblings of the same name, and its own block (frontmatter, body and
// nested children) under exampleBlock.Sections.
type exampleSection struct {
	Name  string
	Line  int
	Level int
	Index int
	*exampleBlock
}

// parseExampleBlock parses one frontmatter-delimited block starting at file
// line startLine: a slide-kind block expects a leading `---` YAML fence: a
// section-kind block (and every nested `# name` instance, regardless of the
// enclosing kind) expects a leading backtick fence. A block with neither
// leading form carries no frontmatter at all — its whole content is Body —
// which lets a schema-free template's example.md be any Markdown.
func parseExampleBlock(src string, kind TemplateKind, startLine int) (*exampleBlock, error) {
	lines := strings.Split(src, "\n")
	i := 0
	// Skip leading blank lines.
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}

	block := &exampleBlock{}
	restStart := i
	restLine := startLine + i

	if kind == KindSlide {
		if i < len(lines) && strings.TrimSpace(lines[i]) == "---" {
			fmLines, end, ok := scanFence(lines, i+1, "---")
			if ok {
				fm, err := decodeFrontmatter(strings.Join(fmLines, "\n"), startLine+i+1)
				if err != nil {
					return nil, err
				}
				block.Frontmatter = fm
				block.FrontmatterLine = startLine + i + 1
				restStart = end + 1
				restLine = startLine + end + 1
			}
		}
	} else {
		if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			fmLines, end, ok := scanFence(lines, i+1, "```")
			if ok {
				fm, err := decodeFrontmatter(strings.Join(fmLines, "\n"), startLine+i+1)
				if err != nil {
					return nil, err
				}
				block.Frontmatter = fm
				block.FrontmatterLine = startLine + i + 1
				restStart = end + 1
				restLine = startLine + end + 1
			}
		}
	}

	rest := lines[restStart:]
	body, bodyLine, sections := splitExampleSections(rest, restLine)
	block.Body = body
	block.BodyLine = restLine
	if body == "" {
		block.BodyLine = 0
	}
	_ = bodyLine
	block.Sections = sections
	return block, nil
}

// scanFence returns the lines strictly between lines[start-1] (the opening
// fence, already matched by the caller) and the next line equal to close,
// and the index of that closing line. ok is false when no closing line is
// found before the input ends.
func scanFence(lines []string, start int, close string) ([]string, int, bool) {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == close {
			return lines[start:i], i, true
		}
	}
	return nil, 0, false
}

// sectionHeadingPattern matches a top-level `# name` section heading line —
// exactly one `#`, a space, then the name — never a `##`/`###` subheading.
func isSectionHeading(line string) (string, bool) {
	level, name, ok := exampleHeadingLevel(line)
	if !ok || level != 1 {
		return "", false
	}
	return name, true
}

// exampleHeadingLevel parses an ATX heading line's depth (1-6) and its name.
// Heading depth is nesting depth, so a deeper heading is a child instance of
// the most recent shallower one (slide-sections). Seven or more `#` is not a
// heading. The name has any ATX closing sequence stripped.
func exampleHeadingLevel(line string) (level int, name string, ok bool) {
	trimmed := strings.TrimRight(line, " \t\r")
	trimmed = strings.TrimLeft(trimmed, " \t")
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes > 6 {
		return 0, "", false
	}
	rest := trimmed[hashes:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	name = strings.TrimSpace(rest)
	if strings.HasSuffix(name, "#") {
		body := strings.TrimRight(name, "#")
		if body != "" && (strings.HasSuffix(body, " ") || strings.HasSuffix(body, "\t")) {
			name = strings.TrimSpace(body)
		}
	}
	if name == "" {
		return 0, "", false
	}
	return hashes, name, true
}

// splitExampleSections splits lines (starting at file line startLine) into the
// body text before the first heading and the section instances that follow,
// nesting each heading one level deeper beneath the most recent shallower one
// (heading depth is nesting depth, slide-sections). A heading at the same or a
// shallower depth closes the open instances. Fenced code blocks are tracked so
// a `#` inside one is never mistaken for a heading.
func splitExampleSections(lines []string, startLine int) (body string, bodyLine int, sections []exampleSection) {
	body, bodyLine, sections = splitExampleLevel(lines, startLine)
	return body, bodyLine, sections
}

// splitExampleLevel parses one level of an example body: the body before its
// first heading and the instances at that level, recursing into deeper ones.
func splitExampleLevel(lines []string, startLine int) (body string, bodyLine int, sections []exampleSection) {
	inFence := false
	bodyEnd := len(lines)
	type heading struct {
		level int
		name  string
		line  int
		idx   int
	}
	var headings []heading
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		level, name, ok := exampleHeadingLevel(line)
		if !ok {
			continue
		}
		if bodyEnd == len(lines) {
			bodyEnd = i
		}
		headings = append(headings, heading{level, name, startLine + i, i})
	}

	body = strings.TrimRight(strings.Join(lines[:bodyEnd], "\n"), "\n")
	bodyLine = startLine
	if len(headings) == 0 {
		return body, bodyLine, nil
	}

	base := headings[0].level
	// Only headings at the first heading's depth open siblings here; a deeper
	// heading belongs to the instance that precedes it.
	counts := map[string]int{}
	i := 0
	for i < len(headings) {
		h := headings[i]
		if h.level < base {
			break
		}
		if h.level > base {
			i++
			continue
		}
		// Extent runs to the next heading at base depth or shallower.
		j := i + 1
		for j < len(headings) && headings[j].level > base {
			j++
		}
		start := h.idx + 1
		end := len(lines)
		if j < len(headings) {
			end = headings[j].idx
		}
		if h.name == "notes" {
			i = j
			continue
		}
		content := lines[start:end]
		index := counts[h.name]
		counts[h.name]++
		// Each instance is a full block in its own right: parseExampleBlock
		// strips its optional leading ``` frontmatter and, via
		// splitExampleSections, nests its own deeper instances.
		nested, err := parseExampleBlock(strings.Join(content, "\n"), KindSection, startLine+start)
		if err != nil {
			nested = &exampleBlock{Body: strings.Join(content, "\n"), BodyLine: startLine + start}
		}
		sections = append(sections, exampleSection{
			Name:         h.name,
			Line:         h.line,
			Level:        h.level,
			Index:        index,
			exampleBlock: nested,
		})
		i = j
	}
	return body, bodyLine, sections
}

// decodeFrontmatter parses raw YAML into a map, extracting and removing a
// `template:` selector key so the remainder is exactly the field data
// CheckValues expects. startLine is the frontmatter's first file line, used
// to offset yaml.v3's line numbers to file-absolute.
func decodeFrontmatter(raw string, startLine int) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// exampleTemplateName returns the `template:` selector a section instance's
// frontmatter names, and whether it named one at all.
func exampleTemplateName(fm map[string]any) (string, bool) {
	if fm == nil {
		return "", false
	}
	v, ok := fm["template"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// validateExampleBlock validates one block (a whole example.md, or one nested
// section instance) against def, populates ctx with the rendered
// field/body/section values a layout expects, and recurses into each declared
// child instance. container is the instance's own containment path
// (`columns[2]`) or "" at the root; child errors extend it and carry the full
// path (nested-section-validation). imageExists is the load-time image-field
// existence resolver, forwarded unchanged to validateExampleBlockAt
// (image-field-roots).
func validateExampleBlock(path string, def *Template, block *exampleBlock, resolve func(string) (*Template, bool), imageExists func(f *Field, path string) (exists bool, candidates []string), ctx map[string]any) error {
	return validateExampleBlockAt(path, def, block, resolve, imageExists, ctx, "")
}

// validateExampleBlockAt is validateExampleBlock with the owning instance's
// containment path threaded through for nested errors.
//
// The author-supplied fields are validated with the schema-only CheckValues
// (task-5), which treats an omitted required field that declares a Default as
// satisfied; after it passes, applyFieldDefaults materialises each declared
// field's Default and every declared field is converted into ctx through the
// same nullable/record/coerce conversion renderer.values/fieldValue applies at
// render time (nullable-fields, record-item-type, text-coercion), so the
// executed example context carries the same values the renderer would. This
// happens at every composition depth, because the recursion below runs each
// nested instance through this same function. The `.raw` source view is built
// from the authored frontmatter, not here, so a default never leaks into it (a
// default is not an authored source value).
//
// imageExists is the load-time image-field existence resolver threaded into
// CheckValues through WithImageExists (image-field-roots): checkLibraryExample
// closes it over the owning layer's media/ tree with a nil assets filesystem,
// because no deck exists at load, so only a media-root image field is
// existence-checked and an assets/no-roots field stays unchecked. It is passed
// unchanged into every recursive nested-instance call, so an image field at any
// composition depth is resolved identically. A nil resolver leaves image
// existence unchecked.
func validateExampleBlockAt(path string, def *Template, block *exampleBlock, resolve func(string) (*Template, bool), imageExists func(f *Field, path string) (exists bool, candidates []string), ctx map[string]any, container string) error {
	fm := block.Frontmatter
	if fm == nil {
		fm = map[string]any{}
	}
	fields := make(map[string]any, len(fm))
	for k, v := range fm {
		if k == "template" {
			continue
		}
		fields[k] = v
	}

	if res := CheckValues(fields, def, resolve, WithImageExists(imageExists)); len(res.Errors) > 0 {
		e := res.Errors[0]
		return exampleError(path, block.FrontmatterLine, container, "%s: %s", e.PathString(), e.What)
	}
	// applyFieldDefaults is the same default-filling idiom ResolveSectionCall
	// uses for a section-helper call and renderer.values mirrors at render time:
	// a supplied value wins, an omitted declared field with a Default gets it,
	// and the caller's map is copied. CheckValues stays schema-only.
	//
	// The defaulted values are then converted into the execution context through
	// the same per-type conversion renderer.values/fieldValue applies at render
	// time for the typed schema core (nullable-fields, record-item-type,
	// text-coercion), so the load-time example context and the render-time
	// context agree on those values. Every declared field is present in ctx: an
	// explicit null and an absent optional field (with no Default) both reach the
	// context as a nil entry; a nullable field's null converts to nil with no
	// further per-type conversion; a coerce text field's YAML number or boolean
	// becomes its string form; a list recurses into its item descriptor; and an
	// inline record becomes one map per item with each declared field converted
	// recursively by this same per-type conversion. The reserved `template:`
	// selector and any `<field>_format` sibling are not declared fields, so
	// neither reaches the context, matching renderer.values.
	//
	// A text field stays its authored string at load time: the renderer's
	// inline-Markdown pre-rendering is render-side only, because a converted
	// text field there is html/template.HTML, which is not the string a helper
	// call's text-field validation accepts (a layout may pass a converted field
	// straight into a `{{ section }}` call). The conversion is a local closure
	// rather than a package symbol because no task owns a template-side
	// conversion helper; it mirrors the renderer's FieldList/FieldRecord
	// recursion and coerceText string form inline, as the render conversion
	// repeats template.coerceText.
	var convertExampleField func(f *Field, raw any) any
	convertExampleField = func(f *Field, raw any) any {
		if raw == nil && f.Nullable {
			return nil
		}
		switch f.Type {
		case FieldText:
			if s, ok := raw.(string); ok {
				return s
			}
			if f.Coerce {
				if coerced, cok := coerceText(raw); cok {
					return coerced
				}
			}
			// A non-string value (already rejected by validation) is left as
			// text for html/template to escape.
			return raw
		case FieldList:
			items, ok := asSlice(raw)
			if !ok || f.Item == nil {
				return raw
			}
			out := make([]any, len(items))
			for i, elem := range items {
				out[i] = convertExampleField(f.Item, elem)
			}
			return out
		case FieldRecord:
			m, ok := asMap(raw)
			if !ok {
				return raw
			}
			out := make(map[string]any, len(f.Fields))
			for i := range f.Fields {
				sub := &f.Fields[i]
				val, present := m[sub.Name]
				if !present {
					if sub.Default == nil {
						out[sub.Name] = nil
						continue
					}
					val = sub.Default
				}
				out[sub.Name] = convertExampleField(sub, val)
			}
			return out
		default:
			// boolean, enum, number, date, image, link and section-template
			// values keep their typed value.
			return raw
		}
	}
	defaulted := applyFieldDefaults(def, fields)
	for i := range def.Fields {
		f := &def.Fields[i]
		raw, present := defaulted[f.Name]
		if !present {
			// An absent optional field with no Default still reaches the
			// context, as a nil entry, exactly as renderer.values does
			// (nullable-fields).
			ctx[f.Name] = nil
			continue
		}
		ctx[f.Name] = convertExampleField(f, raw)
	}

	// The subheading gate carries whether this block's own enclosing template
	// declares child sections, so a heading at any depth is a section marker
	// there and deeper headings stay subheadings otherwise
	// (markdown-allowed-subset); CheckBody then enforces the template's own
	// body.subheadings rule.
	if ve, invalid := CheckBody(def.Body, block.Body, WithBodyLine(block.BodyLine)); invalid {
		return exampleError(path, ve.Line, container, "%s", ve.What)
	}
	if declares := len(def.Sections) > 0; declares || !def.Body.Subheadings {
		if err := checkExampleBodySubheadings(path, def, block, container); err != nil {
			return err
		}
	}
	if strings.TrimSpace(block.Body) != "" {
		ctx["body"] = block.Body
	}

	names := make([]string, 0, len(block.Sections))
	for _, sec := range block.Sections {
		names = append(names, sec.Name)
	}
	sect := NewSection(resolve)
	if errs := sect.CheckRepeats(def, names); len(errs) > 0 {
		return exampleError(path, errs[0].Line, container, "%s", errs[0].What)
	}

	sectionValues := map[string][]map[string]any{}
	counts := map[string]int{}
	for _, sec := range block.Sections {
		childPath := joinContainment(container, sec.Name, counts[sec.Name])
		counts[sec.Name]++
		d, ok := sect.Declared(def, sec.Name)
		if !ok {
			return exampleError(path, sec.Line, container, "example declares section %q, which the template does not declare", sec.Name)
		}
		chosen, named := exampleTemplateName(sec.Frontmatter)
		if !named {
			if len(d.Accepted) != 1 {
				return exampleError(path, sec.Line, childPath, "example section %q must name a template: (accepts %s)", sec.Name, strings.Join(d.Accepted, ", "))
			}
			chosen = d.Accepted[0]
		}
		if !containsString(d.Accepted, chosen) {
			return exampleError(path, sec.Line, childPath, "example section %q uses template %q, which is not one of the accepted templates (%s)", sec.Name, chosen, strings.Join(d.Accepted, ", "))
		}
		nested, ok := resolve(chosen)
		if !ok || nested == nil {
			return exampleError(path, sec.Line, childPath, "example section %q uses template %q, which is not defined", sec.Name, chosen)
		}
		nestedCtx := map[string]any{}
		if err := validateExampleBlockAt(path, nested, sec.exampleBlock, resolve, imageExists, nestedCtx, childPath); err != nil {
			return err
		}
		sectionValues[sec.Name] = append(sectionValues[sec.Name], nestedCtx)
	}
	for name, instances := range sectionValues {
		ctx[name] = instances
	}
	return nil
}

// joinContainment extends a containment path by one indexed segment
// `name[index]`, starting the path when parent is empty.
func joinContainment(parent, name string, index int) string {
	seg := name + "[" + strconv.Itoa(index) + "]"
	if parent == "" {
		return seg
	}
	return parent + " › " + seg
}

// exampleError builds a *LibraryError at path:line whose message is prefixed
// with the instance's containment path when there is one, so the canonical
// ` › ` notation is uniform at any depth (nested-section-validation,
// error-reporting).
func exampleError(path string, line int, container, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if container != "" {
		msg = container + ": " + msg
	}
	return libraryErrorf(path, line, "%s", msg)
}

// checkExampleBodySubheadings refuses a `##`/`###` subheading in an example
// body when the enclosing template declares child sections (every heading at
// any depth is then a section marker) or when the template forbids
// subheadings; depths 4-6 are refused in subheading position
// (markdown-allowed-subset). It reports the first offending heading with the
// instance's containment path.
func checkExampleBodySubheadings(path string, def *Template, block *exampleBlock, container string) error {
	if strings.TrimSpace(block.Body) == "" {
		return nil
	}
	issues := mdcheck.Check(path, []byte(block.Body), mdcheck.Options{
		Mode:                          mdcheck.BodyMode,
		StartLine:                     block.BodyLine,
		TemplateDeclaresChildSections: len(def.Sections) > 0,
	})
	for _, iss := range issues {
		if iss.Kind == mdcheck.KindUnsupportedConstruct && strings.Contains(iss.Message, "heading level") {
			return exampleError(path, iss.Line, container, "%s", iss.Message)
		}
	}
	return nil
}

// emptyExampleDeckContext returns a well-formed but empty stand-in for the
// reserved `.deck` render context, matching the shape render.deckContext
// produces, so a library example layout that reads `.deck.*` (including
// `.deck.properties`) executes cleanly at load-time even though no real deck
// data exists yet. internal/template cannot import internal/render (would
// create an import cycle), so the shape is duplicated here intentionally.
func emptyExampleDeckContext() map[string]any {
	return map[string]any{
		"title":      "",
		"author":     "",
		"date":       "",
		"properties": map[string]any{},
	}
}

// emptyExampleSlideContext returns a well-formed but empty stand-in for the
// reserved `.slide` render context, matching the shape render.slideContext
// produces. See emptyExampleDeckContext for why this is duplicated here.
func emptyExampleSlideContext() map[string]any {
	return map[string]any{
		"number": "",
		"total":  0,
	}
}

// ContextKeys returns the sorted reserved render-context keys a library example
// layout may read: the fixed `.deck`/`.slide` leaf keys from
// emptyExampleDeckContext as deck.title/deck.author/deck.date/deck.properties
// and emptyExampleSlideContext as slide.number/slide.total (template-context,
// deck-data-in-templates), plus the reserved source-context namespaces `.raw`
// (the author's original source values) and `.data` (the authored section tree
// as data), whose members are template-specific and so cannot be enumerated as
// leaf keys (raw-source-context, section-data-context). The `.deck`/`.slide`
// keys derive from those two functions, which stay the single source of truth
// for the fixed namespace shapes; `raw`/`data` are the dynamic source-values and
// authored-section-tree views. Callers that must enumerate the context
// vocabulary (notably the agent-guide drift self-test) do not duplicate the
// list. The returned slice is a fresh allocation.
func ContextKeys() []string {
	deck := emptyExampleDeckContext()
	slide := emptyExampleSlideContext()
	out := make([]string, 0, len(deck)+len(slide)+2)
	for key := range deck {
		out = append(out, "deck."+key)
	}
	for key := range slide {
		out = append(out, "slide."+key)
	}
	out = append(out, ".raw", ".data")
	sort.Strings(out)
	return out
}

// withPath re-positions err (from decodeFrontmatter, which carries no path)
// at path with line 0, when it is not already a *LibraryError.
func withPath(err error, path string) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*LibraryError); ok {
		return err
	}
	return libraryErrorf(path, 0, "%v", err)
}
