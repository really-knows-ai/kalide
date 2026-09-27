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
// content is always nil: a library's layout and example are already loaded
// into LibraryTemplate.Layout/Layout.Text and LibraryTemplate.ExampleBytes by
// LoadLibrary, so there is no filesystem for Register to load them from —
// they are copied onto the parsed Template directly instead.
//
// Each parsed definition still passes through Register's reserved field/
// section name checks (checkDeclaredName): "deck" and "slide" are reserved
// alongside "body", "notes", and "_format".
func NewRegistryFromLibrary(lib *Library) (*Registry, error) {
	r := NewRegistry(nil)
	if lib == nil {
		return r, nil
	}

	for _, name := range sortedLibraryTemplateNames(lib.Sections) {
		if err := registerLibraryTemplate(r, lib.Sections[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range sortedLibraryTemplateNames(lib.Slides) {
		if err := registerLibraryTemplate(r, lib.Slides[name]); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// registerLibraryTemplate parses one LibraryTemplate's manifest and registers
// the resulting definition, carrying its layout and example Markdown across.
func registerLibraryTemplate(r *Registry, lt *LibraryTemplate) error {
	def, err := parseManifest(lt)
	if err != nil {
		return err
	}
	def.Layout = Layout{Name: lt.Name, Text: lt.LayoutText}
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

// wrapLibraryTemplateError adapts a Registry.Register/Validate error (a plain
// error naming the template) to a *LibraryError positioned at lt's manifest.
func wrapLibraryTemplateError(lt *LibraryTemplate, err error) error {
	if err == nil {
		return nil
	}
	return libraryErrorf(lt.ManifestPath, 0, "%s", err.Error())
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
