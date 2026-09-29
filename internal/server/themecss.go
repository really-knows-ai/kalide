package server

import (
	"path"
	"regexp"
	"strings"

	"github.com/really-knows-ai/kalide/internal/template"
)

// This file implements rewriteThemeCSS (theme-shared-media): the serve-time
// rewrite of a theme stylesheet's reserved-prefix media references. A theme
// author writes url('media:fonts/x.woff2') in theme CSS to reach the library's
// shared templates/media tree; the bytes on disk are not a servable URL, so
// the theme route rewrites each such url() to the same served media URL the
// layout `media` helper returns (MediaPath + the cleaned path) before writing
// the stylesheet to the client. The reserved prefix itself is owned by
// internal/template.MediaURLPrefix and is never hardcoded here.
//
// rewriteThemeCSS is pure: it takes the stylesheet bytes and returns the bytes
// to serve, with no filesystem access. Whether a referenced media file
// actually exists is phase-01 load-time validation's concern, not this
// helper's; a media: argument reaching here has already been validated.

// themeCSSURLPattern matches a CSS url(...) reference, capturing its argument.
// It has the same shape as the template package's step-6 scanner pattern
// (internal/template's cssURLPattern), so the set of references this helper
// rewrites is exactly the set phase-01 validates. Like that scanner it
// recognises the lowercase url( form only.
var themeCSSURLPattern = regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`)

// themeCSSImportURLPattern matches an @import rule in its url() form
// (@import url("x.css")). Only this form is matched: the bare quoted-string
// form (@import "x.css") contains no url() token, so no url() reference can
// sit inside it and it needs no span. The matched span is used to recognise a
// url() that is an @import target and must never be rewritten — the reserved
// media: prefix is valid only for a non-@import url() reference.
var themeCSSImportURLPattern = regexp.MustCompile(`@import\s+url\(\s*(?:'[^']*'|"[^"]*"|[^'")\s]+)\s*\)`)

// rewriteThemeCSS rewrites every url() reference in css whose argument begins
// with the canonical reserved prefix (template.MediaURLPrefix) to the served
// media URL server.MediaPath + the path after the prefix, and returns the
// result. The rewritten URL matches the layout `media` helper's served shape
// (MediaFunc: strings.TrimSuffix(base, "/") + "/" + path.Clean(p)), so
// url('media:fonts/x.woff2') becomes url("/assets/templates/media/fonts/x.woff2").
//
// Every url() that does not carry the reserved prefix — a theme-owned relative
// reference, an absolute path, a `..` reference, http:/https:/data:/`//` and
// the like — and every other byte of css are left byte-for-byte unchanged. A
// url() that is the target of an @import is never rewritten, in either the
// quoted or unquoted url() form, independently of phase-01's load-time
// rejection of such an import: that rejection alone does not protect a
// stylesheet served from a tree that was not loaded through the validator.
//
// An empty css, and css with nothing to rewrite, is returned unchanged. A
// reserved-prefix argument with no usable path (empty, or cleaning to "." or
// escaping with "..") is left as-is rather than rewritten to a degenerate URL;
// phase-01 validation rejects such a reference before it can be served.
func rewriteThemeCSS(css []byte) []byte {
	if len(css) == 0 {
		return css
	}
	prefix := template.MediaURLPrefix()
	// Mirror MediaFunc's join exactly: TrimSuffix(MediaPath, "/") + "/".
	mediaBase := strings.TrimSuffix(MediaPath, "/") + "/"

	importSpans := themeCSSImportURLPattern.FindAllIndex(css, -1)

	var out []byte
	last := 0
	for _, m := range themeCSSURLPattern.FindAllSubmatchIndex(css, -1) {
		start, end := m[0], m[1]
		if offsetInsideAny(importSpans, start) {
			continue // an @import url() target: never rewritten
		}
		arg := string(css[m[2]:m[3]])
		rest := strings.TrimPrefix(arg, prefix)
		if rest == arg {
			continue // no reserved prefix: leave byte-for-byte unchanged
		}
		clean := path.Clean(rest)
		if rest == "" || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			continue // no usable path after the prefix: leave unchanged
		}
		if out == nil {
			out = make([]byte, 0, len(css)+32)
		}
		out = append(out, css[last:start]...)
		out = append(out, "url(\""...)
		out = append(out, mediaBase...)
		out = append(out, clean...)
		out = append(out, "\")"...)
		last = end
	}
	if out == nil {
		return css
	}
	return append(out, css[last:]...)
}

// offsetInsideAny reports whether offset falls inside any [start, end) span.
func offsetInsideAny(spans [][]int, offset int) bool {
	for _, s := range spans {
		if offset >= s[0] && offset < s[1] {
			return true
		}
	}
	return false
}
