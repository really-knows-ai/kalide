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
//	pages/      full-page html/template shells (the reveal.js deck page)
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
//go:embed reveal fonts logo templates pages theme.css
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
// Later phases add the error and gallery pages to this same sub-tree and
// expose them alongside (or through a Pages accessor) without changing
// DeckPage's meaning.
func DeckPage() fs.FS { return pagesFS }

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
