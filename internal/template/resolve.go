package template

import (
	"os"
	"path/filepath"
)

// This file is the template loader's library-root resolution point
// (external-template-library): ResolveLibraryRoot turns a deck's raw
// `templates:` value plus its deck root into the deck's PRIMARY library root
// (the external `templates:` root when configured, else the deck-root
// templates/), ResolveLibraryLayers returns every layer root the deck draws
// from, and LoadDeckLibrary loads, validates and merges every one of those
// layers through the shared loader body in loadlibrary.go. ResolveLibraryRoot
// is not authoritative over
// the local layer: a configured `templates:` root is the primary root, but the
// deck-root templates/ still participates as the local layer through
// ResolveLibraryLayers, so a configured path never suppresses a present local
// templates/. The error when neither resolves names the offending or expected
// path.
//
// The roots are resolved on the real filesystem, against the directory holding
// kalide.yaml, rather than through the deck's fs.FS: an external library may
// sit above the deck root (e.g. `../shared-lib`), which os.DirFS and
// fs.ValidPath reject.

// ResolveLibraryRoot resolves the primary template-library root a deck uses.
//
// deckRoot is the directory holding the deck's kalide.yaml, as an
// operating-system path. configured is the deck's raw top-level `templates:`
// value as written (deck.TemplatesPath): "" when the key is absent or empty.
//
// When configured is non-empty it is the primary root: a relative path is
// resolved against deckRoot and an absolute path is used as given. When
// configured is empty the deck-root templates/ directory under deckRoot is the
// primary root. ResolveLibraryRoot is not authoritative over the local layer:
// the local templates/ still participates as the local layer through
// ResolveLibraryLayers, so a configured path does not ignore it. Callers that
// need every layer the deck draws from must use ResolveLibraryLayers.
//
// The resolved root must exist and be a directory. When it does not — the
// configured path is missing or not a directory, or (with no key) the local
// templates/ is absent or not a directory — ResolveLibraryRoot returns a
// *LibraryError naming that offending or expected path, with the `kalide init`
// hint only for the absent-local case. There is no built-in fallback, and a
// missing configured path errors rather than silently falling back to the
// local templates/ (templates-dir-required).
func ResolveLibraryRoot(deckRoot, configured string) (string, error) {
	if configured != "" {
		root := configured
		if !filepath.IsAbs(root) {
			root = filepath.Join(deckRoot, root)
		}
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil {
			return "", libraryErrorf(root, 0, "%s directory not found", root).
				withHint("check the `templates:` path in kalide.yaml")
		}
		if !info.IsDir() {
			return "", libraryErrorf(root, 0, "%s exists but is not a directory", root).
				withHint("check the `templates:` path in kalide.yaml")
		}
		return root, nil
	}

	root := filepath.Join(deckRoot, TemplatesDir)
	info, err := os.Stat(root)
	if err != nil {
		return "", missingTemplatesDirError(root, root+" directory not found")
	}
	if !info.IsDir() {
		return "", missingTemplatesDirError(root, root+" exists but is not a directory")
	}
	return root, nil
}

// ResolveLibraryLayers resolves the ordered set of template-library roots a
// deck draws from (external-template-library, templates-dir-required).
//
// deckRoot is the directory holding the deck's kalide.yaml, as an
// operating-system path. configured is the deck's raw top-level `templates:`
// value as written (deck.TemplatesPath): "" when the key is absent or empty.
//
// The returned slice is ordered from the external library to the local one:
//
//   - When configured is non-empty its root comes first and is authoritative
//     for that layer: a relative path is resolved against deckRoot, an
//     absolute path is used as given, and the path must exist and be a
//     directory. When it does not, ResolveLibraryLayers returns a
//     *LibraryError naming that path with the `templates:` hint and no layers.
//   - The deck-root templates/ directory (TemplatesDir) is appended next
//     whenever that directory exists. A local templates/ that exists but is
//     not a directory is an error naming it, never silently skipped.
//
// Both layers may resolve (external first, local second) or just one; a
// configured path never suppresses a present local templates/ layer, and the
// local layer never silently falls back in place of a configured one. When
// neither layer resolves — configured is empty and the local templates/ is
// absent — ResolveLibraryLayers returns the same missingTemplatesDirError for
// the expected local path that ResolveLibraryRoot uses, carrying the `kalide
// init` hint.
func ResolveLibraryLayers(deckRoot, configured string) ([]string, error) {
	var layers []string

	if configured != "" {
		root := configured
		if !filepath.IsAbs(root) {
			root = filepath.Join(deckRoot, root)
		}
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil {
			return nil, libraryErrorf(root, 0, "%s directory not found", root).
				withHint("check the `templates:` path in kalide.yaml")
		}
		if !info.IsDir() {
			return nil, libraryErrorf(root, 0, "%s exists but is not a directory", root).
				withHint("check the `templates:` path in kalide.yaml")
		}
		layers = append(layers, root)
	}

	local := filepath.Join(deckRoot, TemplatesDir)
	info, err := os.Stat(local)
	switch {
	case err != nil:
		if len(layers) == 0 {
			return nil, missingTemplatesDirError(local, local+" directory not found")
		}
	case !info.IsDir():
		return nil, missingTemplatesDirError(local, local+" exists but is not a directory")
	default:
		layers = append(layers, local)
	}

	return layers, nil
}

// LoadDeckLibrary loads, validates and merges the template library a deck
// uses. It resolves the deck's ordered layer roots through
// ResolveLibraryLayers — the external `templates:` root first when configured,
// else the deck-root templates/ as the sole layer — loads each root through
// loadLibrary at its own root (so every error stays path-qualified with that
// layer's own root), rejects a name carried by two layers
// (checkLayerUniqueness), and merges the layers into one *Library with equal
// standing (mergeLibraryLayers). The merged Library carries the layers it was
// merged from (Library.Layers) and takes its metadata and default root from
// the primary layer, the first in resolution order (the external `templates:`
// root when configured, else the local one).
//
// The first layer — the external `templates:` layer when configured, and any
// sole layer — is loaded fully (libraryLayerFull), so it validates as a
// self-contained standalone library: a reference it makes to a template, theme
// or media name only another layer carries is rejected at that layer's own
// load. When two layers load, the local layer is loaded deferred
// (libraryLayerDeferred), because its templates may call templates contributed
// by the external layer; the cross-template section-call build checks
// (checkLibraryBuild) and the example runs (checkLibraryExamples) are then run
// once over the merged library, so a local template's reference to an external
// template resolves while an unresolved reference still fails, with the error
// path-qualified to the failing template's own file.
//
// A configured `templates:` path that names the deck's own templates/
// directory is one layer, not two: duplicate roots are collapsed by their
// cleaned absolute path and, where it resolves, their symlink target.
//
// deckRoot is the directory holding the deck's kalide.yaml; configured is the
// deck's raw `templates:` value as written (deck.TemplatesPath). A resolved
// root may sit outside the deck, so LoadDeckLibrary opens it directly on the
// operating system rather than through the deck's fs.FS. Its errors are
// path-qualified with the resolved root, e.g.
// "<root>/slides/content/template.yaml:12".
func LoadDeckLibrary(deckRoot, configured string) (*Library, error) {
	roots, err := ResolveLibraryLayers(deckRoot, configured)
	if err != nil {
		return nil, err
	}
	roots = dedupeLibraryRoots(roots)

	layers := make([]*LibraryLayer, 0, len(roots))
	for i, root := range roots {
		// A sole layer, and the first (external) layer of a layered deck, is
		// validated fully. Only the local/second layer defers the
		// cross-template checks, which the merged library runs once below.
		mode := libraryLayerFull
		if len(roots) > 1 && i > 0 {
			mode = libraryLayerDeferred
		}
		layer, err := loadLibrary(os.DirFS(root), ".", root, mode)
		if err != nil {
			return nil, err
		}
		// Record the layer's role from the resolution, before the uniqueness
		// check and the merge, so everything downstream — the merged Library
		// and every render path reading a layer through Library.Layers — sees
		// it. A single surviving layer, whether the external `templates:` root
		// alone or the deck-root local templates/ alone, is the deck's whole
		// library and is sole. When two layers survive, the first (the
		// configured external `templates:` layer) is external and the second
		// (the deck-root templates/) is local; render derives each layer's
		// served URL prefixes from its role (layered-template-library).
		switch {
		case len(roots) > 1 && i == 0:
			layer.Role = LayerRoleExternal
		case len(roots) > 1:
			layer.Role = LayerRoleLocal
		default:
			layer.Role = LayerRoleSole
		}
		layers = append(layers, layer)
	}

	// A name carried by two layers is a validation error, detected before the
	// merge so it is never silently resolved (library-layer-uniqueness).
	if err := checkLayerUniqueness(layers); err != nil {
		return nil, err
	}

	merged := mergeLibraryLayers(layers)

	// When two layers loaded, the local layer's per-layer load deferred these
	// cross-template checks; run them once over the whole merged set.
	if len(layers) > 1 {
		if err := checkLibraryBuild(merged); err != nil {
			return nil, err
		}
		if err := checkLibraryExamples(merged); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// dedupeLibraryRoots collapses roots that name the same directory, keeping the
// first occurrence (the external `templates:` root when configured, else the
// local one). Two roots are the same when their cleaned absolute paths match
// or, where it resolves, their symlink target matches — so a configured
// `templates:` path that points at the deck's own templates/ directory,
// directly, through a relative spelling or through a symlink, is one layer
// rather than two.
func dedupeLibraryRoots(roots []string) []string {
	if len(roots) < 2 {
		return roots
	}
	out := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		key := canonicalLibraryRoot(root)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, root)
	}
	return out
}

// canonicalLibraryRoot returns a root's identity for dedupe: its cleaned
// absolute path, replaced by its resolved symlink target when that succeeds.
// A root that does not resolve is identified by its cleaned absolute path.
func canonicalLibraryRoot(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	root = filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = filepath.Clean(resolved)
	}
	return root
}
