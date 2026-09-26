package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// This file implements mediaHandler (template-media): serving a project's
// templates/media/** and templates/themes/<name>/** files directly from disk,
// with an extension-derived Content-Type and no allow-list. It is mounted by
// Listen alongside the embedded reveal.js assets (assets.go), and — like
// every route Listen registers — is only ever reached over the loopback bind
// (Host, 127.0.0.1); mediaHandler itself performs no binding.
//
// Both routes reuse the same path-cleaning and file-serving helpers
// assetHandler already implements (assetName, isRegularFile, serveFileFS):
// an absolute path or one escaping its root with a ".." segment resolves to
// nothing and 404s, and a directory is never listed.

const (
	// MediaPath is the URL prefix templates/media/** is served under. It
	// must match internal/render's mediaURLPrefix exactly (render/render.go
	// builds `media` URLs a layout receives with this prefix baked in; the
	// two packages cannot share the constant directly since server imports
	// render).
	MediaPath = "/assets/templates/media/"

	// ThemesPath is the URL prefix templates/themes/<name>/** is served
	// under. It must match internal/render's themesURLPrefix exactly
	// (render/deck.go bakes a theme stylesheet URL with this prefix into the
	// rendered deck page).
	ThemesPath = "/assets/templates/themes/"
)

// mediaHandler serves templates/media/** from mediaFS (typically a project
// Library's Media filesystem) under MediaPath, and templates/themes/<name>/**
// from themes (typically the project theme.Registry built by theme.LoadDir)
// under ThemesPath. A request under neither prefix, one for a missing file,
// or one that resolves outside its root 404s; there is no directory listing
// and no allow-list beyond "the file exists under its declared root".
func mediaHandler(lib *template.Library, themes *theme.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(MediaPath, http.StripPrefix(MediaPath, mediaFileHandler(libraryMediaFS(lib))))
	mux.Handle(ThemesPath, themeFileHandler(themes))
	return mux
}

// libraryMediaFS returns lib's Media filesystem, or nil when lib is nil or
// has none (Library.HasMedia false) — mediaFileHandler treats a nil
// filesystem as "nothing is ever found here", not a panic.
func libraryMediaFS(lib *template.Library) fs.FS {
	if lib == nil {
		return nil
	}
	return lib.Media
}

// mediaFileHandler serves a single file directly under mediaFS: the request
// path (already stripped of MediaPath by the caller) is cleaned and checked
// exactly as assetHandler checks an embedded-asset request (assetName,
// isRegularFile), so an absolute path or a ".." segment 404s instead of
// escaping mediaFS's root.
func mediaFileHandler(mediaFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name, ok := assetName(r.URL.Path)
		if !ok || !isRegularFile(mediaFS, name) {
			http.NotFound(w, r)
			return
		}
		serveFileFS(w, r, mediaFS, name)
	})
}

// themeFileHandler serves templates/themes/<name>/<file> requests under
// ThemesPath: the first path segment after ThemesPath names the theme, looked
// up in themes (theme.Registry); the rest is resolved against that theme's
// own Assets filesystem (theme.Theme.Assets, as built by theme.LoadDir) with
// the same path-cleaning assetHandler uses. An unknown theme name, a missing
// file or a path escaping the theme's root all 404.
func themeFileHandler(themes *theme.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if themes == nil {
			http.NotFound(w, r)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, ThemesPath)
		themeName, file, found := strings.Cut(rest, "/")
		if !found || themeName == "" {
			http.NotFound(w, r)
			return
		}
		t, err := themes.Lookup(themeName)
		if err != nil || t.Assets == nil {
			http.NotFound(w, r)
			return
		}
		name, ok := assetName("/" + file)
		if !ok || !isRegularFile(t.Assets, name) {
			http.NotFound(w, r)
			return
		}
		serveFileFS(w, r, t.Assets, name)
	})
}
