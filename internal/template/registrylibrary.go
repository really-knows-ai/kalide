package template

import (
	"sort"
)

// This file implements NewRegistryFromLibrary: building a *Registry from an
// already-validated Library (its manifests parsed into definitions and its
// layouts/examples already checked by LoadLibrary's earlier steps). It is the
// library-driven replacement for the Go-builtins Registry construction
// (Builtins), used by checkLibraryBuild (templates-dir-validation step 4) and
// by callers that want a Registry over a project's templates/ library.
//
// NewRegistryFromLibrary performs no validation ordering of its own: it
// registers every section then every slide template (sections first, so a
// slide's field-type and section references resolve against an
// already-registered set) and lets Registry.Validate run the cross-template
// build-time checks exactly as the Go-builtins loader does. LoadLibrary owns
// the templates-dir-validation step order (checkLibraryBuild, task 11, is the
// only caller during library loading).

// NewRegistryFromLibrary builds a *Registry over lib's slide and section
// templates: each manifest is parsed (parseManifest) into a *Template
// definition, its layout and example Markdown are copied from the already-
// loaded LibraryTemplate, and it is registered. Sections are registered
// before slides. It returns the first error parseManifest or Register
// reports; it does not call Validate — the caller (checkLibraryBuild) does.
//
// After a template is registered and its owning layer recorded, the layer-aware
// local-definition check runs over it (expandLibraryTemplateVariants), so an
// enum field's `variants_from:` glob is expanded against that layer's root into
// the field's effective Variants and VariantsFrom is cleared — on the very
// registry this function returns, not only on the throwaway registry
// checkLibraryBuild builds and validates. A runtime caller that builds a
// registry and never calls Validate (kalide templates/start/check) therefore
// sees the effective variants the schema engine and `kalide templates <name>`
// read. A template whose owning layer is unknown (no Layers, or an
// unidentifiable entry) leaves the glob unexpanded, exactly as Validate does.
//
// content is always nil: a library's layout and example are already loaded
// into LibraryTemplate.Layout/Layout.Text and LibraryTemplate.ExampleBytes by
// LoadLibrary, so there is no filesystem for Register to load them from —
// they are copied onto the parsed Template directly instead.
//
// Each parsed definition still passes through Register's reserved field/
// section name checks (checkDeclaredName): "deck" and "slide" are reserved
// alongside "body", "notes", and "_format".
//
// It also records each registered template's owning-layer provenance
// (library-layer-references): after a section is registered its owning layer is
// looked up through resolveMergedSectionTarget, and after a slide is registered
// through resolveMergedSlideLayer; the layer is stored via Registry.setLayer so
// Registry.LayerOf can return it. A Library that carries no Layers — the empty
// zero-layer merge, or a Library built without them — records nothing, exactly
// as a name whose owning layer cannot be identified does: both report nil
// through LayerOf. Registration and validation behaviour are otherwise
// unchanged.
//
// Each template's owning-layer named definitions are threaded through
// registerLibraryTemplate into parseManifest, so a template's `type:`,
// `include:` and `@group` references resolve in its OWN layer's definitions
// (named-library-definitions). The definitions come from the owning layer
// (LibraryLayer.Definitions) when the layer is identified; a Library with no
// Layers — the single-layer *Library the loader body builds for step 4 —
// carries them on its unexported definitions carrier instead, so that is the
// fallback. A nil defs holds no definitions.
//
// single-binary (requirements.requirement.single-binary): the library is
// resolved locally and offline — it works purely on the in-memory Library
// LoadLibrary already built (which reads only the project's templates/
// directory), performing no filesystem, OS or network access. It is in-binary,
// self-contained and OS-neutral across all six supported targets
// (darwin/arm64, darwin/amd64, windows/amd64, windows/arm64, linux/amd64,
// linux/arm64), so it behaves identically on each.
func NewRegistryFromLibrary(lib *Library) (*Registry, error) {
	r := NewRegistry(nil)
	if lib == nil {
		return r, nil
	}

	for _, name := range sortedLibraryTemplateNames(lib.Sections) {
		lt := lib.Sections[name]
		_, layer, _ := resolveMergedSectionTarget(lib, name)
		defs := lib.definitions
		if layer != nil {
			defs = layer.Definitions
		}
		if err := registerLibraryTemplate(r, lt, defs); err != nil {
			return nil, err
		}
		r.setLayer(name, layer)
		if err := expandLibraryTemplateVariants(r, lt, layer); err != nil {
			return nil, err
		}
	}
	for _, name := range sortedLibraryTemplateNames(lib.Slides) {
		lt := lib.Slides[name]
		layer := resolveMergedSlideLayer(lib, name)
		defs := lib.definitions
		if layer != nil {
			defs = layer.Definitions
		}
		if err := registerLibraryTemplate(r, lt, defs); err != nil {
			return nil, err
		}
		r.setLayer(name, layer)
		if err := expandLibraryTemplateVariants(r, lt, layer); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// registerLibraryTemplate parses one LibraryTemplate's manifest and registers
// the resulting definition, carrying its layout and example Markdown across.
// defs is the named definitions of the layer that owns lt (LibraryLayer.
// Definitions, or the loading *Library's carrier on the single-layer path),
// threaded through to parseManifest so lt's `type:`/`include:`/`@group`
// references resolve in its own layer (named-library-definitions).
func registerLibraryTemplate(r *Registry, lt *LibraryTemplate, defs *LibraryDefinitions) error {
	def, err := parseManifest(lt, defs)
	if err != nil {
		return err
	}
	// Layout.Name stays the layer-relative layout name (the parsed layout's
	// own name, or the template name as a fallback); Layout.Path is the
	// layer's root-joined on-disk layout path, carried for error display
	// only, never as a namespace or lookup key.
	def.Layout = Layout{Name: lt.Name, Path: lt.LayoutPath, Text: lt.LayoutText}
	if lt.Layout != nil {
		def.Layout.Name = lt.Layout.Name()
	}
	def.Example.Markdown = string(lt.ExampleBytes)
	def.Example.Deferred = true

	if err := r.Register(def); err != nil {
		return wrapLibraryTemplateError(lt, err)
	}
	return nil
}

// expandLibraryTemplateVariants runs the layer-aware local-definition check
// (checkDefinition) over the template registered under lt.Name, expanding any
// enum field's `variants_from:` glob against layer.Root into the field's
// effective Variants and clearing VariantsFrom (enum-variants-from).
//
// Register has already run the root-less check while the template was added —
// its owning layer is only known afterwards, and Register's check passes a nil
// root — so an enum field with a `variants_from:` glob would otherwise keep
// VariantsFrom set and Variants empty on the registry NewRegistryFromLibrary
// returns. The runtime callers (kalide templates/start/check) build a registry
// and never call Validate, so this second, root-supplied pass is what makes the
// effective variants available to the schema engine's membership check
// (checkEnum) and to `kalide templates <name>`. Idempotent with Validate: once
// expanded, VariantsFrom is cleared, so a later Validate over the same registry
// does not see both `variants:` and `variants_from:` and falsely reject.
//
// A nil lt, a nil layer, or a layer whose Root is nil records no usable
// provenance, so there is no root to expand against and the glob stays
// unexpanded — a no-op, exactly as Registry.Validate treats a template with no
// owning layer. An expansion failure is wrapped as a *LibraryError positioned
// at lt's manifest, matching registerLibraryTemplate's error shape.
func expandLibraryTemplateVariants(r *Registry, lt *LibraryTemplate, layer *LibraryLayer) error {
	if r == nil || lt == nil || layer == nil || layer.Root == nil {
		return nil
	}
	def, ok := r.Lookup(lt.Name)
	if !ok || def == nil {
		return nil
	}
	if err := checkDefinition(def, layer.Root); err != nil {
		return wrapLibraryTemplateError(lt, err)
	}
	return nil
}

// wrapLibraryTemplateError adapts a Registry.Register/Validate error (a plain
// error naming the template) to a *LibraryError positioned at lt's manifest.
func wrapLibraryTemplateError(lt *LibraryTemplate, err error) error {
	if err == nil {
		return nil
	}
	return libraryErrorf(lt.ManifestPath, 0, "%s", err.Error())
}

// resolveMergedSlideLayer returns the *LibraryLayer whose Slides map holds, by
// identity, the slide template registered under name in lib's merged Slides
// map: the slide counterpart of resolveMergedSectionTarget (library.go). The
// merged Slides map is the union of the layers' slides, so it alone cannot say
// which layer contributed a slide; this lookup recovers that provenance for
// NewRegistryFromLibrary.
//
// A nil lib, an unknown slide name, or a Library whose Layers do not carry that
// slide — no Layers at all, or none holding the same *LibraryTemplate pointer —
// reports nil, so no provenance is recorded and LayerOf reports nil.
func resolveMergedSlideLayer(lib *Library, name string) *LibraryLayer {
	if lib == nil {
		return nil
	}
	target := lib.Slides[name]
	if target == nil {
		return nil
	}
	for _, layer := range lib.Layers {
		if layer == nil {
			continue
		}
		if layer.Slides[name] == target {
			return layer
		}
	}
	return nil
}

// sortedLibraryTemplateNames returns m's keys sorted, so registration order
// (and therefore first-error order) is deterministic.
func sortedLibraryTemplateNames(m map[string]*LibraryTemplate) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
