package template

import (
	"errors"
	"io/fs"
	"os"

	"github.com/really-knows-ai/kalide/internal/theme"
)

// This file implements LoadThemeRegistry: building a *theme.Registry from an
// already-resolved *Library, one layer at a time. It is the shared home of the
// per-layer theme load that `kalide start`/`check`/`render`, the server reload
// pipeline and `kalide upgrade`'s post-refresh validation all perform
// (layered-template-library, theme-selection). The library is resolved by the
// caller (LoadDeckLibrary, honouring a configured `templates:` path), so the
// deck config read stays in the caller.

// LoadThemeRegistry builds the deck's theme registry from EVERY layer of the
// merged library (layered-template-library, theme-selection): each layer's
// themes/ directory is loaded from that layer's OWN RootPath, so a theme a
// layer contributes keeps that layer's own theme files (theme.Theme.Assets is
// rooted inside the contributing layer), and the merged registry lets the
// deck's `theme:` key — default `default` — resolve a theme contributed by
// either layer. Theme names are unique across layers, enforced by
// LoadDeckLibrary before it returns, so merging the layers never collides and
// no theme silently overrides another.
//
// A layer that carries no themes/ directory contributes no themes: themes/ is
// optional per layer (as it is for a single-root library), so a two-layer deck
// whose theme lives only in one layer still resolves it. A single-layer
// library keeps theme.LoadDir's exact behaviour, including its error when the
// layer's themes/ directory is missing.
func LoadThemeRegistry(library *Library) (*theme.Registry, error) {
	// The whole library is one layer: load its themes exactly as before.
	if len(library.Layers) == 1 {
		return theme.LoadDir(os.DirFS(library.RootPath), ThemesDir)
	}

	merged := theme.NewRegistry()
	for _, layer := range library.Layers {
		fsys := os.DirFS(layer.RootPath)
		if _, err := fs.Stat(fsys, ThemesDir); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		layerReg, err := theme.LoadDir(fsys, ThemesDir)
		if err != nil {
			return nil, err
		}
		// theme.Registry exposes registration but no multi-root loader, so the
		// layers merge by transferring each layer's registered themes into the
		// one merged registry (via Names/Lookup/Register). Uniqueness was
		// already enforced over the merged library, so Register cannot collide.
		for _, name := range layerReg.Names() {
			th, err := layerReg.Lookup(name)
			if err != nil {
				return nil, err
			}
			if err := merged.Register(th); err != nil {
				return nil, err
			}
		}
	}
	return merged, nil
}
