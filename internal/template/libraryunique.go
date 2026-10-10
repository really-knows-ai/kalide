package template

import (
	"io/fs"
	"path"
	"sort"
)

// This file implements checkLayerUniqueness, the cross-layer half of
// library-layer-uniqueness. The single-layer loader (loadlibrary.go) enforces
// uniqueness WITHIN one layer — checkCrossKindNames rejects a name carried by
// both slides/ and sections/ of the same layer — but a deck's layers merge with
// equal standing and no precedence (mergeLibraryLayers), so a name carried by
// two layers is a validation error rather than an override. checkLayerUniqueness
// is that check: given the deck's ordered loaded layers, it rejects the first
// name two layers carry, before any deck is served.
//
// There are three separate namespaces (library-layer-uniqueness), checked in
// this fixed order:
//
//   - the template namespace, shared by slides and sections: a slide or
//     section name may be carried by only one layer. A local slide named like
//     an external section (or vice versa) collides just as two same-kind
//     templates do, because the merged registry keys slides and sections in
//     one namespace.
//   - the theme namespace: a theme name may be carried by only one layer.
//   - the media namespace: a media path (a file's path relative to its layer's
//     media/ directory) may be carried by only one layer.
//
// A violation is reported as a *LibraryError naming the duplicated name and the
// path that carries it in BOTH layers — a template's manifest path, a theme's
// directory path, or a media file's path — each path-qualified with its own
// layer root (LibraryLayer.RootPath). The later layer is never allowed to
// override the earlier one, and neither is silently dropped: the error stops
// the load, and the merge is only reached once this check passes
// (LoadDeckLibrary).

// checkLayerUniqueness rejects a name carried by two layers of the ordered
// layers slice, in the fixed order templates, then themes, then media, and
// within each namespace by the name's sorted order — so a layer set with
// several collisions always reports the same first violation. It returns the
// first violation as a *LibraryError naming the name and both carrying paths
// (path-qualified with each layer's own root), or nil when every slide, section,
// theme and media path is carried by at most one layer.
//
// Within one layer the same single-namespace uniqueness holds across slides/
// and sections/ (enforced by the per-layer loader's checkCrossKindNames); the
// per-layer maps are therefore already name-disjoint, and this check only has
// to compare across layers. layers is the ordered layer set
// ResolveLibraryLayers returns and loadLibrary fills: the external
// `templates:` layer first when the deck configures one, then the deck-root
// local templates/ layer. A nil entry is ignored.
func checkLayerUniqueness(layers []*LibraryLayer) error {
	if err := checkLayerTemplateNameUniqueness(layers); err != nil {
		return err
	}
	if err := checkLayerThemeNameUniqueness(layers); err != nil {
		return err
	}
	return checkLayerMediaPathUniqueness(layers)
}

// checkLayerTemplateNameUniqueness rejects a slide or section name carried by
// two layers (library-layer-uniqueness). Slides and sections share one
// namespace, so a slide in one layer and a section in another collide just as
// two same-kind templates do. names are considered in sorted order, and the
// error names the name and each layer's manifest path.
func checkLayerTemplateNameUniqueness(layers []*LibraryLayer) error {
	// carriers maps a template name to the manifest path that carries it in
	// each layer, in layer order. Building a per-layer name map first ensures a
	// single layer contributes at most one carrier per name, so only a genuine
	// cross-layer duplicate is reported (a within-layer slide/section clash is
	// the per-layer loader's checkCrossKindNames).
	carriers := map[string][]string{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		perLayer := make(map[string]*LibraryTemplate, len(layer.Slides)+len(layer.Sections))
		for name, lt := range layer.Slides {
			perLayer[name] = lt
		}
		for name, lt := range layer.Sections {
			if _, ok := perLayer[name]; !ok {
				perLayer[name] = lt
			}
		}
		for name, lt := range perLayer {
			carriers[name] = append(carriers[name], layerTemplateCarrierPath(layer, lt))
		}
	}
	for _, name := range sortedStringKeys(carriers) {
		paths := carriers[name]
		if len(paths) < 2 {
			continue
		}
		return libraryErrorf(paths[0], 0,
			"template %q is declared in both %s and %s", name, paths[0], paths[1])
	}
	return nil
}

// checkLayerThemeNameUniqueness rejects a theme name carried by two layers
// (library-layer-uniqueness). Theme names form their own namespace, separate
// from template names. names are considered in sorted order, and the error
// names the name and each layer's theme directory path.
func checkLayerThemeNameUniqueness(layers []*LibraryLayer) error {
	carriers := map[string][]string{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		for name, th := range layer.Themes {
			carriers[name] = append(carriers[name], layerThemeCarrierPath(layer, th))
		}
	}
	for _, name := range sortedStringKeys(carriers) {
		paths := carriers[name]
		if len(paths) < 2 {
			continue
		}
		return libraryErrorf(paths[0], 0,
			"theme %q is declared in both %s and %s", name, paths[0], paths[1])
	}
	return nil
}

// checkLayerMediaPathUniqueness rejects a media file path carried by two layers
// (library-layer-uniqueness). A media path is a file's path relative to its
// layer's media/ directory, so the same relative path under two layers is a
// duplicate. A layer with no media directory contributes nothing. Relative
// paths are considered in sorted order, and the error names the path and its
// full location under both layers' roots.
func checkLayerMediaPathUniqueness(layers []*LibraryLayer) error {
	// carriers maps a media-relative path to the file's full path under each
	// layer root, in layer order.
	carriers := map[string][]string{}
	for _, layer := range layers {
		if layer == nil || !layer.HasMedia || layer.Media == nil {
			continue
		}
		root := path.Join(layer.RootPath, MediaDir)
		err := fs.WalkDir(layer.Media, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			carriers[p] = append(carriers[p], path.Join(root, p))
			return nil
		})
		if err != nil {
			return libraryErrorf(root, 0, "read media directory: %v", err)
		}
	}
	for _, rel := range sortedStringKeys(carriers) {
		paths := carriers[rel]
		if len(paths) < 2 {
			continue
		}
		return libraryErrorf(paths[0], 0,
			"media path %q is declared in both %s and %s", rel, paths[0], paths[1])
	}
	return nil
}

// layerTemplateCarrierPath returns the path that carries lt in layer for a
// uniqueness error: the template's manifest path, already path-qualified with
// the layer root. A hand-built layer whose manifest path is unset falls back to
// the template directory, so the error always names a path.
func layerTemplateCarrierPath(layer *LibraryLayer, lt *LibraryTemplate) string {
	if lt == nil {
		return layer.RootPath
	}
	if lt.ManifestPath != "" {
		return lt.ManifestPath
	}
	if lt.Dir != "" {
		return path.Join(layer.RootPath, lt.Dir, ManifestFile)
	}
	return layer.RootPath
}

// layerThemeCarrierPath returns the path that carries th in layer for a
// uniqueness error: the theme's directory, path-qualified with the layer root.
// A hand-built layer whose theme has no Dir falls back to the conventional
// themes/<name> directory, so the error always names a path.
func layerThemeCarrierPath(layer *LibraryLayer, th *LibraryTheme) string {
	if th == nil {
		return layer.RootPath
	}
	if th.Dir != "" {
		return path.Join(layer.RootPath, th.Dir)
	}
	return path.Join(layer.RootPath, ThemesDir, th.Name)
}

// sortedStringKeys returns m's keys sorted, so a check that iterates a map
// reports its first violation deterministically.
func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
