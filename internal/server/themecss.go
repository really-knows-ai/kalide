package server

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/really-knows-ai/kalide/internal/template"
)

// This file implements rewriteThemeCSS (theme-shared-media): the serve-time
// rewrite of a theme stylesheet's reserved-prefix library references. A theme
// author writes url('media:fonts/x.woff2') in theme CSS to reach the library's
// shared templates/media tree, and url('theme:default/fonts/x.woff2') to reach
// a sibling theme's tree. Neither byte sequence is a servable URL, so the
// serve-time route rewrites each reserved-prefix reference to the URL the
// corresponding route exposes (MediaPath + the cleaned path, or ThemesPath +
// the theme name + the cleaned path) before writing the stylesheet to the
// client. The reserved prefixes themselves are owned by internal/template
// (MediaURLPrefix and ThemeURLPrefix) and are never hardcoded here.
//
// Both reserved prefixes are valid for a plain url() reference and for an
// @import target, in either of the two @import forms: the url() form and the
// bare quoted-string form. An @import target carrying a reserved prefix is
// rewritten exactly like a plain reference, and both @import forms are
// canonicalised to the url() output shape.
//
// rewriteThemeCSS is pure: it takes the stylesheet bytes and returns the bytes
// to serve, with no filesystem access. Whether a referenced file actually
// exists is phase-01 load-time validation's concern, not this helper's; a
// reserved-prefix argument reaching here has already been validated.

// themeCSSURLPattern matches a CSS url(...) reference, capturing its argument.
// It has the same shape as the template package's step-6 scanner pattern
// (internal/template's cssURLPattern), so the set of references this helper
// rewrites is exactly the set phase-01 validates. Like that scanner it
// recognises the lowercase url( form only.
var themeCSSURLPattern = regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`)

// themeCSSImportStringPattern matches an @import rule in its bare quoted-string
// form (@import "x.css" / @import 'x.css'). That form contains no url() token,
// so themeCSSURLPattern above would never see its target; this pattern captures
// it (group 1 single-quoted, group 2 double-quoted) so a reserved-prefix
// target there is rewritten too. It does not match the url() form: after the
// whitespace an @import url(...) target begins with "url", not a quote.
var themeCSSImportStringPattern = regexp.MustCompile(`@import\s+(?:'([^']*)'|"([^"]*)")`)

// rewriteThemeCSS rewrites every reserved-prefix reference in css to the URL
// its route exposes and returns the result.
//
// A reference carrying the canonical media prefix (template.MediaURLPrefix) is
// rewritten to server.MediaPath + the cleaned path, matching the layout `media`
// helper's served shape, so url('media:fonts/x.woff2') becomes
// url("/assets/templates/media/fonts/x.woff2"). A reference carrying the
// canonical theme prefix (template.ThemeURLPrefix) has the shape
// theme:<name>/<path>: the remainder after the token is cut on its first "/"
// into the sibling theme name and the path within that theme, and the output is
// server.ThemesPath + name + "/" + the cleaned path, so
// url('theme:default/fonts/x.woff2') becomes
// url("/assets/templates/themes/default/fonts/x.woff2"). The name-then-path cut
// matches themeFileHandler's own Cut(rest, "/"), which serves the emitted URL.
//
// The reserved prefixes are honoured both for a plain url() reference and
// inside an @import target, in the url() form and the bare quoted-string form.
// Both @import forms are canonicalised to the url() output shape, so
// @import url('media:x.css') and @import "media:x.css" both become
// @import url("/assets/templates/media/x.css").
//
// Every reference that does not carry a reserved prefix — a theme-owned
// relative reference, an absolute path, a `..` reference, http:/https:/data:/
// `//` and the like — and every other byte of css are left byte-for-byte
// unchanged. A reserved-prefix argument with no usable path (an empty name or
// path, a name containing "/", a path cleaning to "." or escaping with "..",
// or an absolute path) is left as-is rather than rewritten to a degenerate URL;
// phase-01 validation rejects such a reference before it can be served.
//
// An empty css, and css with nothing to rewrite, is returned unchanged.
func rewriteThemeCSS(css []byte) []byte {
	if len(css) == 0 {
		return css
	}
	mediaPrefix := template.MediaURLPrefix()
	themePrefix := template.ThemeURLPrefix()
	// Mirror the routes' join exactly: the route prefix with exactly one
	// trailing "/" (MediaFunc uses the same shape for media).
	mediaBase := strings.TrimSuffix(MediaPath, "/") + "/"
	themeBase := strings.TrimSuffix(ThemesPath, "/") + "/"

	var edits []cssEdit

	// A plain url() reference, and the url() form of an @import target: the
	// url() token inside @import url(...) is matched and rewritten like any
	// other url() reference.
	for _, m := range themeCSSURLPattern.FindAllSubmatchIndex(css, -1) {
		arg := string(css[m[2]:m[3]])
		resolved, ok := resolveThemeCSSRef(arg, mediaPrefix, mediaBase, themePrefix, themeBase)
		if !ok {
			continue
		}
		edits = append(edits, cssEdit{m[0], m[1], `url("` + resolved + `")`})
	}

	// The bare quoted-string form of an @import target, which carries no url()
	// token for the pattern above to see. It is canonicalised to the url()
	// output shape when (and only when) its target carries a reserved prefix.
	for _, m := range themeCSSImportStringPattern.FindAllSubmatchIndex(css, -1) {
		var arg string
		switch {
		case m[2] >= 0:
			arg = string(css[m[2]:m[3]])
		case m[4] >= 0:
			arg = string(css[m[4]:m[5]])
		default:
			continue
		}
		resolved, ok := resolveThemeCSSRef(arg, mediaPrefix, mediaBase, themePrefix, themeBase)
		if !ok {
			continue
		}
		edits = append(edits, cssEdit{m[0], m[1], `@import url("` + resolved + `")`})
	}

	if len(edits) == 0 {
		return css
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })

	out := make([]byte, 0, len(css)+32)
	last := 0
	for _, e := range edits {
		if e.start < last {
			continue // an overlapping earlier edit already covered this span
		}
		out = append(out, css[last:e.start]...)
		out = append(out, e.text...)
		last = e.end
	}
	return append(out, css[last:]...)
}

// cssEdit is one byte-for-byte replacement in a stylesheet: the half-open span
// [start, end) of the original bytes and the text that replaces it.
type cssEdit struct {
	start, end int
	text       string
}

// resolveThemeCSSRef resolves one CSS reference argument that carries a
// reserved prefix to the served URL its route exposes, or reports false when
// the argument carries no reserved prefix or carries one but has no usable
// path. mediaPrefix/mediaBase and themePrefix/themeBase are the canonical
// tokens and the route bases (each ending in exactly one "/") for the two
// reserved prefixes.
func resolveThemeCSSRef(arg, mediaPrefix, mediaBase, themePrefix, themeBase string) (string, bool) {
	if rest := strings.TrimPrefix(arg, mediaPrefix); rest != arg {
		clean := path.Clean(rest)
		if rest == "" || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return "", false
		}
		return mediaBase + clean, true
	}

	if rest := strings.TrimPrefix(arg, themePrefix); rest != arg {
		name, p, found := strings.Cut(rest, "/")
		// The token is theme:<name>/<path>: both must be present, the name
		// must be a single segment (Cut makes a "/" in it impossible, kept as
		// the declared guard), and the path must be non-empty and stay inside
		// the named theme.
		if !found || name == "" || strings.Contains(name, "/") || p == "" {
			return "", false
		}
		clean := path.Clean(p)
		if clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return "", false
		}
		return themeBase + name + "/" + clean, true
	}

	return "", false
}
