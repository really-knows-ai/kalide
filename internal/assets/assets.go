// Package assets embeds the static, offline core assets that ship inside the
// kalide binary.
//
// The embedded tree holds two sub-trees:
//
//	reveal/  vendored reveal.js 6.0.2 runtime — dist/reveal.js,
//	         dist/reveal.css (print styles bundled), dist/reset.css,
//	         dist/plugin/notes.js and LICENSE (MIT)
//	pages/   full-page html/template shells (the reveal.js deck page, the
//	         full-page error page and the /templates gallery page) — each
//	         an unbranded shell whose visual style comes entirely from the
//	         project's own theme stylesheet, never from an asset in this
//	         package
//
// Everything is compiled into the binary: the served deck never reaches the
// network or the filesystem for these files (global.constraint
// go-static-embedded-binary). Brand assets (webfonts, logo), built-in
// template content and the starter deck are not part of this package; a
// project supplies its own templates/, themes/ and starter content on disk.
package assets

import (
	"embed"
	"io/fs"
)

// FS is the root of the embedded asset tree.
//
//go:embed reveal pages
var FS embed.FS

// revealFS and pagesFS are resolved once: fs.Sub can only fail for a missing
// or malformed directory, and the embed directive above guarantees each
// sub-tree exists, so resolving lazily would only add error handling that can
// never trigger at runtime.
var (
	revealFS = mustSub("reveal")
	pagesFS  = mustSub("pages")
)

// Reveal returns the vendored reveal.js sub-tree, rooted so that paths such as
// "dist/reveal.js", "dist/reveal.css" and "dist/plugin/notes.js" are valid.
func Reveal() fs.FS { return revealFS }

// DeckPage returns the sub-tree holding the full-page html/template shells; the
// reveal.js deck page is "deck.html.tmpl" within it. internal/render parses and
// executes that file to produce the served presentation, so the page and every
// asset it references are compiled into the binary and served offline.
//
// DeckPage is a narrow view of the same sub-tree Pages exposes: callers that
// want one specific page keep using their accessor, while a caller that serves
// several pages (internal/server) walks Pages.
func DeckPage() fs.FS { return pagesFS }

// Pages returns the sub-tree holding every full-page html/template shell,
// rooted so that a page is addressable by its filename:
//
//	deck.html.tmpl     the reveal.js deck page (internal/render.RenderDeck)
//	error.html.tmpl    the full-page deck-error page (internal/server)
//	gallery.html.tmpl  the /templates gallery page (internal/server)
//
// internal/server parses each page in its own html/template namespace, for
// example:
//
//	template.Must(template.New("error").
//	    ParseFS(assets.Pages(), "error.html.tmpl"))
//
// Every page is standalone and offline: it references only the embedded
// reveal.js runtime and the project's own theme stylesheet, served under
// /assets/, never a CDN. Each documents the execution context it expects at
// the top of the file.
func Pages() fs.FS { return pagesFS }

// mustSub returns the sub-tree of FS rooted at dir, panicking when dir is not
// an embedded directory. The embed directive is the source of truth, so a
// failure here is a programming error, not a runtime condition.
func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(FS, dir)
	if err != nil {
		panic("assets: missing embedded sub-tree " + dir + ": " + err.Error())
	}
	return sub
}
