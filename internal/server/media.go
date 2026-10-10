package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
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

	// ExternalMediaPath is the URL prefix a TWO-layer deck's external
	// `templates:` layer's media/** is served under
	// (layered-template-library). It is the per-layer counterpart of
	// MediaPath: the deck-root local templates/ layer of a two-layer deck
	// keeps MediaPath, while the external `templates:` layer is addressed
	// under this distinct prefix, so each layer's media is served from its
	// own root rather than a merged namespace. The layer is identified by the
	// `external` path segment; there is no index and no query tag. It must
	// match internal/render's externalMediaURLPrefix exactly (render/render.go
	// emits a `media` URL under this prefix for a layout owned by the
	// external layer; the two packages cannot share the constant directly
	// since server imports render).
	ExternalMediaPath = "/assets/templates/external/media/"

	// ExternalThemesPath is the URL prefix a TWO-layer deck's external
	// `templates:` layer's themes/<name>/** is served under
	// (layered-template-library). It is the per-layer counterpart of
	// ThemesPath: under it a theme name addresses the external layer's own
	// themes/ registry independently of the deck-root local layer's, never a
	// merged registry.
	//
	// Both external prefixes are more specific than the embedded /assets/
	// tree (assets.go's AssetsPath) and distinct from MediaPath, ThemesPath,
	// the /templates gallery and the /events SSE endpoint, so a per-layer
	// route never collides with a route Listen already registers.
	ExternalThemesPath = "/assets/templates/external/themes/"
)

// externalThemeCSSBases are a two-layer deck's external `templates:` layer's
// serving bases: ExternalMediaPath and ExternalThemesPath, the URLs a .css
// served from that layer's media/ or themes/ tree has its reserved-prefix
// references rewritten onto (rewriteThemeCSS, phase-04 tasks 21-22). The
// primary layer's bases stay primaryThemeCSSBases (MediaPath/ThemesPath).
var externalThemeCSSBases = themeCSSBases{mediaBase: ExternalMediaPath, themesBase: ExternalThemesPath}

// mediaHandler serves a project library's media and theme files, one serving
// layer at a time, over the per-layer routes render.RenderSlide addresses
// (layered-template-library, the phase-02 task-13 URL contract):
//
//   - the single/PRIMARY layer — the sole layer of a single-root library, or
//     the deck-root local templates/ layer of a two-layer deck — is served at
//     today's exact unprefixed URLs: templates/media/** under MediaPath
//     (/assets/templates/media/) and templates/themes/<name>/** under
//     ThemesPath (/assets/templates/themes/), the latter from the primary
//     layer's own theme registry.
//   - a two-layer deck's EXTERNAL `templates:` layer is additionally mounted
//     at the distinct per-layer prefixes ExternalMediaPath
//     (/assets/templates/external/media/) and ExternalThemesPath
//     (/assets/templates/external/themes/), resolving in that layer's own
//     media filesystem and its own theme registry.
//
// media and themes are resolved per layer through lib.Layers, never merged:
// the primary media route reads the primary layer's media (Library.Media, the
// primary layer's convenience field) and the external media route reads the
// external layer's own Layers[i].Media (libraryMediaFS). The primary theme
// route serves the primary layer's themes with a per-name fallback to the
// external layer's themes (themeOwnerHandler): a deck theme that only the
// external `templates:` layer carries is served at the unprefixed URL from
// that layer's own Assets, rewritten against the external layer's own bases,
// while a theme the primary layer owns is served locally. The external theme
// route serves ONLY the external layer's themes
// (libraryLayerThemeRegistry); when lib has no external-role layer — a
// single-root library — the passed themes registry is used for the primary
// route unchanged, so a single-layer library's behaviour is byte-identical.
//
// The external routes exist only when lib actually has an external-role
// layer, so a single-root library mounts exactly the two routes it did
// before. A request under none of the mounted prefixes, one for a missing
// file, or one that resolves outside its layer's root 404s; there is no
// directory listing and no allow-list beyond "the file exists under its
// declared root" (mediaFileHandler, themeFileHandler). The routes are
// registered here on the returned mux; Listen mounts the same handler at the
// four prefixes (phase-04 task-11), matching the URLs the renderer emits
// (MediaPath/ThemesPath for the primary layer,
// ExternalMediaPath/ExternalThemesPath for the external layer).
func mediaHandler(lib *template.Library, themes *theme.Registry) http.Handler {
	mux := http.NewServeMux()
	// The primary layer keeps today's exact routes and bases. Its theme
	// registry is the primary layer's own in a two-layer deck, or the
	// caller's (merged == the only) registry for a single-root library; the
	// external layer's own registry is the per-name fallback the unprefixed
	// ThemesPath route serves an external-only deck theme from. For a
	// single-root library there is no external layer, so themeOwnerHandler
	// returns the primary themeFileHandler unchanged and the route stays
	// byte-identical.
	external := libraryLayerByRole(lib, template.LayerRoleExternal)
	externalThemes := libraryLayerThemeRegistry(external)
	mux.Handle(MediaPath, mediaFileHandler(primaryLibraryMediaFS(lib), MediaPath, primaryThemeCSSBases))
	mux.Handle(ThemesPath, themeOwnerHandler(ThemesPath, primaryThemeRegistry(lib, themes), externalThemes))

	// A two-layer deck's external `templates:` layer is mounted under its own
	// prefixes and served from its own root, so it never shares the primary
	// layer's namespace. Its theme route is mounted only when the layer has a
	// themes/ registry; the media route is always mounted for the layer
	// (mediaFileHandler treats a nil media filesystem as "nothing is ever
	// found here").
	if external != nil {
		mux.Handle(ExternalMediaPath, mediaFileHandler(libraryMediaFS(lib, libraryExternalMedia), ExternalMediaPath, externalThemeCSSBases))
		if externalThemes != nil {
			mux.Handle(ExternalThemesPath, themeFileHandler(externalThemes, ExternalThemesPath, externalThemeCSSBases))
		}
	}
	return mux
}

// themeOwnerHandler serves the unprefixed ThemesPath route
// (/assets/templates/themes/<name>/<file>) for a deck's primary layer, falling
// back per requested theme NAME to a two-layer deck's external `templates:`
// layer. It is a small composing handler: for each request it reads the name
// from the path (`<name>/<file>`), picks the owning layer's registry and bases,
// and delegates to that layer's themeFileHandler (whose signature is
// unchanged).
//
//   - The primary (local) layer's registry wins whenever it carries the name,
//     so a locally-owned theme is served locally from the primary layer's own
//     Assets (primaryThemeCSSBases).
//   - Otherwise a two-layer deck's external layer serves the theme from ITS
//     own Assets with its reserved-prefix references rewritten against the
//     external layer's own bases (externalThemeCSSBases:
//     ExternalMediaPath/ExternalThemesPath), so an external-only deck theme —
//     the normal case after `kalide init <external>`, whose default theme is
//     external — is served at the unprefixed URL and its media:/theme:<name>/
//     references still resolve in its own layer.
//   - When neither registry carries the name the primary handler runs and
//     404s, exactly as before.
//
// Theme names are unique across layers (checkLayerUniqueness), so the name
// alone selects the owner and there is never a merged registry. prefix names
// the mounted route (ThemesPath) and both delegated handlers strip it
// themselves, so the request is passed through unstripped. When externalReg is
// nil — a single-root library, or a two-layer deck whose external layer has no
// themes/ — there is no fallback and the primary themeFileHandler is returned
// directly, so single-layer behaviour stays byte-identical. The external-prefix
// route (ExternalThemesPath) is unaffected: it keeps addressing the external
// layer's registry directly.
func themeOwnerHandler(prefix string, primaryReg, externalReg *theme.Registry) http.Handler {
	primary := themeFileHandler(primaryReg, prefix, primaryThemeCSSBases)
	if externalReg == nil {
		return primary
	}
	external := themeFileHandler(externalReg, prefix, externalThemeCSSBases)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The full request path is still present here (the delegated handler
		// strips prefix itself), so trim it to read <name>/<file>.
		name, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, prefix), "/")
		if !themeRegistryHas(primaryReg, name) && themeRegistryHas(externalReg, name) {
			external.ServeHTTP(w, r)
			return
		}
		primary.ServeHTTP(w, r)
	})
}

// themeRegistryHas reports whether reg carries a theme named name. A nil
// registry — a layer with no themes/ directory — and an empty name carry none,
// so neither is ever treated as an owner.
func themeRegistryHas(reg *theme.Registry, name string) bool {
	if reg == nil || name == "" {
		return false
	}
	_, err := reg.Lookup(name)
	return err == nil
}

// primaryThemeRegistry returns the theme registry the primary layer's
// ThemesPath route serves. When the Library has a separate external
// `templates:` layer it is the primary layer's OWN themes, so a two-layer
// deck's unprefixed theme route never exposes the external layer's themes;
// otherwise (a single-root library, where merged is exactly the sole layer's
// registry) it is the passed merged registry unchanged, so single-layer
// behaviour stays byte-identical. A nil primary layer — a nil Library, or one
// with no contributing layers — falls back to the passed registry.
func primaryThemeRegistry(lib *template.Library, merged *theme.Registry) *theme.Registry {
	if libraryLayerByRole(lib, template.LayerRoleExternal) == nil {
		return merged
	}
	return libraryLayerThemeRegistry(primaryLibraryLayer(lib))
}

// primaryLibraryLayer returns the merged Library's PRIMARY layer — the layer
// served at the unprefixed MediaPath/ThemesPath URLs: the deck-root local
// templates/ layer when one is present, otherwise the last contributing layer
// (the sole layer of a single-root library). It mirrors mergeLibraryLayers'
// primary selection. A nil Library, or one with no contributing layers,
// returns nil.
func primaryLibraryLayer(lib *template.Library) *template.LibraryLayer {
	if lib == nil {
		return nil
	}
	var primary *template.LibraryLayer
	for _, layer := range lib.Layers {
		if layer == nil {
			continue
		}
		if layer.Role == template.LayerRoleLocal {
			return layer
		}
		primary = layer
	}
	return primary
}

// libraryLayerByRole returns the contributing layer carrying role, or nil
// when the Library has none (a nil Library, or a single-root Library with no
// external layer). It never merges layers: it returns exactly one layer, or
// none.
func libraryLayerByRole(lib *template.Library, role template.LibraryLayerRole) *template.LibraryLayer {
	if lib == nil {
		return nil
	}
	for _, layer := range lib.Layers {
		if layer != nil && layer.Role == role {
			return layer
		}
	}
	return nil
}

// libraryLayerThemeRegistry builds the theme registry for exactly one
// contributing layer from that layer's own loaded themes: each theme's Assets
// is the layer's themes/<name> sub-filesystem, so a theme file is served from
// the layer that owns it, never a merged registry. It returns nil when the
// layer has no themes/ directory (a layer with no themes contributes none) or
// when its root cannot address a theme directory, which themeFileHandler
// treats as "no theme is ever found here" rather than a panic.
func libraryLayerThemeRegistry(layer *template.LibraryLayer) *theme.Registry {
	if layer == nil || layer.Root == nil || len(layer.Themes) == 0 {
		return nil
	}
	reg := theme.NewRegistry()
	for name, th := range layer.Themes {
		if th == nil {
			continue
		}
		assets, err := fs.Sub(layer.Root, th.Dir)
		if err != nil {
			return nil
		}
		if err := reg.Register(theme.Theme{
			Name:       name,
			Stylesheet: theme.Stylesheet,
			Assets:     assets,
		}); err != nil {
			return nil
		}
	}
	if len(reg.Names()) == 0 {
		return nil
	}
	return reg
}

// libraryMediaLayer selects which contributing layer of a merged Library a
// caller wants a media filesystem for. A layer is addressed by this selector,
// never merged into one namespace, so a media path resolves in exactly the
// requested layer (layered-template-library).
type libraryMediaLayer int

const (
	// libraryPrimaryMedia selects the merged Library's PRIMARY layer: the
	// sole layer of a single-root library, or the deck-root local templates/
	// layer of a two-layer deck. It is the layer served under the unprefixed
	// /assets/templates/media/ URL (MediaPath), today's behaviour.
	libraryPrimaryMedia libraryMediaLayer = iota

	// libraryExternalMedia selects a two-layer deck's external `templates:`
	// layer — the layer served under the /assets/templates/external/media/
	// prefix (phase-04 task-7, phase-02 task-13 URL contract). A single-root
	// library, or one with no external layer, has no such layer, so it
	// resolves to nil.
	libraryExternalMedia
)

// libraryMediaFS returns the media filesystem of the layer selected by sel,
// or nil when there is no such layer (a nil Library, a Library with no
// external layer, or a layer whose media/ directory is absent, HasMedia
// false) — mediaFileHandler treats a nil filesystem as "nothing is ever
// found here", not a panic.
//
// It never merges layers: every call addresses exactly one layer's own media
// namespace. libraryPrimaryMedia returns the primary layer's media — exactly
// the Library.Media convenience field, so today's behaviour is unchanged —
// while libraryExternalMedia returns a two-layer deck's external `templates:`
// layer's own media (its Layers entry with Role LayerRoleExternal). The two
// selectors mirror the two served URL prefixes: the primary layer is served
// at MediaPath and the external layer at the /assets/templates/external/media/
// prefix (phase-04 task-7, phase-02 task-13 URL contract).
func libraryMediaFS(lib *template.Library, sel libraryMediaLayer) fs.FS {
	if lib == nil {
		return nil
	}
	if sel == libraryExternalMedia {
		return libraryLayerMedia(lib, template.LayerRoleExternal)
	}
	return lib.Media
}

// primaryLibraryMediaFS is the convenience path for the merged Library's
// primary layer media — libraryMediaFS(lib, libraryPrimaryMedia), the
// pre-layered behaviour. It exists so a caller that wants the primary layer
// (the sole layer of a single-root library, or the deck-root local templates/
// layer of a two-layer deck) does not need to name the selector. A nil
// Library returns nil.
func primaryLibraryMediaFS(lib *template.Library) fs.FS {
	return libraryMediaFS(lib, libraryPrimaryMedia)
}

// libraryLayerMedia returns the media filesystem of the contributing layer
// carrying role, or nil when no contributing layer has that role. A layer
// with no media/ directory (HasMedia false) has a nil Media, so it resolves
// to nil here too.
func libraryLayerMedia(lib *template.Library, role template.LibraryLayerRole) fs.FS {
	layer := libraryLayerByRole(lib, role)
	if layer == nil {
		return nil
	}
	return layer.Media
}

// mediaFileHandler serves a single file directly under mediaFS, for the media
// route a caller mounts at prefix: it strips prefix itself (http.StripPrefix),
// so a caller registers it with a plain mux.Handle(prefix, …) and passes the
// same prefix here. The stripped request path is cleaned and checked exactly as
// assetHandler checks an embedded-asset request (assetName, isRegularFile), so
// an absolute path or a ".." segment 404s instead of escaping mediaFS's root.
//
// The prefix names the serving layer's route and bases carries that same
// layer's media/themes base URLs — the URLs a rewritten .css reference is
// joined onto. Mounting the single/primary layer passes MediaPath and
// primaryThemeCSSBases, today's exact behaviour; the external `templates:`
// layer's media route passes its own prefix
// (/assets/templates/external/media/) and bases (mediaBase
// /assets/templates/external/media/, themesBase
// /assets/templates/external/themes/), so a stylesheet served from that layer's
// media/ folder has its reserved-prefix references rewritten to that layer's
// own URLs rather than a fixed or merged base.
//
// A .css file is served through rewriteThemeCSS (with bases) and explicitly as
// text/css, so a stylesheet reached from the /media/ route — including one
// another stylesheet @imports through the reserved media: prefix — has its own
// media:/theme:<name>/ references rewritten before the bytes reach the client.
// Every other media file is served raw, exactly as before.
func mediaFileHandler(mediaFS fs.FS, prefix string, bases themeCSSBases) http.Handler {
	return http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if !strings.HasSuffix(name, ".css") {
			serveFileFS(w, r, mediaFS, name)
			return
		}

		// A stylesheet served from the media tree may itself carry
		// reserved-prefix references; rewrite them to the served URLs before
		// writing the bytes, and set the MIME type explicitly (as the theme
		// route does) rather than relying on ServeContent sniffing the
		// rewritten bytes.
		info, err := fs.Stat(mediaFS, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		css, err := fs.ReadFile(mediaFS, name)
		if err != nil {
			// The file stat'd as a regular file but could not be read; this
			// is a server-side failure, not a missing resource.
			http.Error(w, "read media stylesheet", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		http.ServeContent(w, r, path.Base(name), info.ModTime(), bytes.NewReader(rewriteThemeCSS(css, bases)))
	}))
}

// themeFileHandler serves templates/themes/<name>/<file> requests for one
// serving layer: it strips prefix itself (http.StripPrefix), so a caller
// registers it with a plain mux.Handle(prefix, …) and passes the same prefix
// here. The first path segment after prefix names the theme, looked up in
// themes (that layer's own theme.Registry, never a merged one); the rest is
// resolved against that theme's own Assets filesystem (theme.Theme.Assets, as
// built by theme.LoadDir) with the same path-cleaning assetHandler uses. An
// unknown theme name, a missing file or a path escaping the theme's root all
// 404.
//
// The prefix names the serving layer's route and bases carries that same
// layer's media/themes base URLs — the URLs a rewritten .css reference is
// joined onto. Mounting the single/primary layer passes ThemesPath and
// primaryThemeCSSBases, today's exact behaviour; a two-layer deck's external
// `templates:` layer passes its own prefix
// (/assets/templates/external/themes/) and bases (mediaBase
// /assets/templates/external/media/, themesBase
// /assets/templates/external/themes/), so the same theme name addresses each
// layer independently under its own route.
//
// A .css theme asset is served through rewriteThemeCSS (theme-shared-media,
// with bases): url() references carrying the reserved media: or theme:<name>/
// prefix — and any such reference inside the url() or bare quoted-string form
// of an @import target — are rewritten to that serving layer's media/themes
// URLs before the bytes reach the client. Every other theme file is served
// raw, exactly as before. This applies to every .css file the theme tree owns,
// including theme.css itself and any stylesheet reached by an @import from it,
// and including another theme's stylesheet reached through a reserved
// theme:<name>/ reference: that stylesheet is then requested at the themes
// route and served by this same handler, so its own references are rewritten
// in turn.
func themeFileHandler(themes *theme.Registry, prefix string, bases themeCSSBases) http.Handler {
	return http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if themes == nil {
			http.NotFound(w, r)
			return
		}
		// The prefix is already stripped (http.StripPrefix), so the request
		// path is exactly <name>/<file> within this layer's themes tree.
		rest := r.URL.Path
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
		if !strings.HasSuffix(name, ".css") {
			serveFileFS(w, r, t.Assets, name)
			return
		}

		// A stylesheet may reach the layer's shared media tree through the
		// reserved media: prefix, or a sibling theme through the reserved
		// theme:<name>/ prefix, whether directly in a url() or as an @import
		// target; rewrite those references (in every .css file this handler
		// serves) to the serving layer's media/themes URLs (bases) before
		// writing the bytes. The MIME type is derived from the .css extension
		// exactly as serveFileFS derives it (via ServeContent's name), but set
		// explicitly so the rewritten bytes are served as CSS rather than
		// content-sniffed.
		info, err := fs.Stat(t.Assets, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		css, err := fs.ReadFile(t.Assets, name)
		if err != nil {
			// The file stat'd as a regular file but could not be read; this
			// is a server-side failure, not a missing resource.
			http.Error(w, "read theme stylesheet", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		http.ServeContent(w, r, path.Base(name), info.ModTime(), bytes.NewReader(rewriteThemeCSS(css, bases)))
	}))
}
