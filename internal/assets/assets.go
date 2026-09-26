// Package assets embeds the static, offline assets that ship inside the
// eypres binary.
//
// The embedded tree has four sub-trees plus the default theme stylesheet at
// its root:
//
//	reveal/     vendored reveal.js 6.0.2 runtime — dist/reveal.js,
//	            dist/reveal.css (print styles bundled), dist/reset.css,
//	            dist/plugin/notes.js and LICENSE (MIT)
//	fonts/      EY brand webfonts (EYInterstate and EYGothic families) as .woff2
//	logo/       EY logo SVGs (full/small, light/dark variants)
//	templates/  built-in template content: one directory per template holding
//	            its html/template layout and validating example slide source
//	pages/      full-page html/template shells (the reveal.js deck page, the
//	            full-page error page and the /templates gallery page)
//	starter/    the starter deck copied verbatim by `eypres init`: a valid
//	            eypres.yaml at the root and two valid slides under slides/
//	theme.css   default EY theme tokens and @font-face rules
//
// Everything is compiled into the binary: the served deck never reaches the
// network or the filesystem for these files (global.constraint
// go-static-embedded-binary).
//
// Later phases add their own accessors for the assets they own (deck/error/
// gallery pages, starter files); this package only exposes the trees it
// embeds.
package assets

import (
	"embed"
	"io/fs"
)

// FS is the root of the embedded asset tree. Its paths are the names used by
// the sub-tree accessors and by theme.css, so `fonts/…` and `logo/…` resolve
// relative to the stylesheet when it is served from the root of this tree.
//
// The go:embed pattern deliberately does not reach the repository's original
// fonts/ and logo/ directories: embed cannot include parent directories, so
// the brand assets were moved under internal/assets/ when they were vendored.
//
//go:embed reveal fonts logo templates pages starter theme.css
var FS embed.FS

// revealFS, fontsFS, logoFS and templatesFS are resolved once: fs.Sub can only
// fail for a missing or malformed directory, and the embed directive above
// guarantees each sub-tree exists, so resolving lazily would only add error
// handling that can never trigger at runtime.
var (
	revealFS    = mustSub("reveal")
	fontsFS     = mustSub("fonts")
	logoFS      = mustSub("logo")
	templatesFS = mustSub("templates")
	pagesFS     = mustSub("pages")
	starterFS   = mustSub("starter")
)

// Reveal returns the vendored reveal.js sub-tree, rooted so that paths such as
// "dist/reveal.js", "dist/reveal.css" and "dist/plugin/notes.js" are valid.
func Reveal() fs.FS { return revealFS }

// Fonts returns the EY webfont sub-tree, rooted so that paths such as
// "EYInterstate-Regular.woff2" are valid.
func Fonts() fs.FS { return fontsFS }

// Logo returns the EY logo sub-tree, rooted so that paths such as
// "logo_full-light.svg" and "logo_small-dark.svg" are valid.
func Logo() fs.FS { return logoFS }

// Templates returns the built-in template content sub-tree, rooted so that a
// template's directory is addressable by name: "<name>/layout.html.tmpl" is
// its Go html/template layout and "<name>/example.md" its validating example
// slide source. The tree holds content only; the Go schema definitions and
// registration live in internal/template.Builtins, which imports this package
// (never the reverse). The layout/example conventions and every field name are
// documented in the tree's README.md.
func Templates() fs.FS { return templatesFS }

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
// Every page is standalone and offline: it references only files within the
// embedded tree (theme.css, logo/, fonts/, reveal/), served under /assets/, and
// never a CDN. Each documents the execution context it expects at the top of
// the file.
func Pages() fs.FS { return pagesFS }

// Starter returns the starter-deck sub-tree copied verbatim by `eypres init`
// (internal/scaffold). It is rooted at the deck directory, so its paths map
// one-to-one onto the deck a scaffold writes:
//
//	eypres.yaml          the deck configuration (title required, theme: default)
//	slides/1-title.md    a valid title-usage starter slide
//	slides/2-content.md  a valid content-usage starter slide
//
// The two slides use the built-in `title` and `content` templates and the deck
// passes whole-deck validation (internal/validate) unmodified. internal/scaffold
// walks this tree and copies each file to the same relative path under the
// target directory, creating slides/ and an empty assets/ alongside it; the
// layout is documented for it here and in the accessor's contract.
func Starter() fs.FS { return starterFS }

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
