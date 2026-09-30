package template

import (
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"path"
	"strings"
)

// This file implements the html/template func map every library layout
// executes with (template-language): exactly one helper, `media`, plus the
// number/date format functions (Format.FuncMap). It never adds a
// Sprig-style utility library or a Markdown helper: a layout composes
// already-rendered HTML fragments (body/text/section, passed in by the
// renderer as template.HTML) and plain data values, which html/template
// auto-escapes on its own.

// MediaURLPrefix returns the reserved URL prefix a theme stylesheet uses to
// reference the library's shared media tree, e.g. "media:fonts/x.woff2". It
// is the exact literal "media:" — the scheme-like token including the colon
// that precedes the path. This is the ONE canonical source of truth for the
// reserved prefix: the theme-CSS reference scanner, the serve-time CSS
// rewriter and the agent-guide drift test all read it from here, so the token
// must never be hardcoded elsewhere. It is valid for BOTH a url() reference
// and an @import target in theme CSS, resolving to the resolved library's
// shared media/ tree.
func MediaURLPrefix() string {
	return "media:"
}

// ThemeURLPrefix returns the reserved prefix a theme stylesheet uses to
// reference another theme's files, e.g. "theme:default/fonts/x.woff2". It is
// the exact literal "theme:" — the scheme-like token including the colon that
// precedes the sibling theme name. This is the ONE canonical source of truth
// for the reserved prefix: the theme-CSS reference scanner, the serve-time CSS
// rewriter and the agent-guide drift test all read it from here, so the token
// must never be hardcoded elsewhere. The <name> after the prefix names a
// sibling directory under the resolved library's themes/.
func ThemeURLPrefix() string {
	return "theme:"
}

// MediaFunc returns the `media` layout func: it resolves p relative to the
// library's templates/media directory only, rejects an absolute path or a
// path containing a `..` segment, and requires the file to exist — a missing
// file is reported as a *LibraryError, not a silent broken link. On success
// it returns the URL the server exposes the file under.
//
// mediaFS is the templates/media sub-filesystem (Library.Media); a nil
// mediaFS makes every call fail as if the file did not exist, since there is
// nowhere to resolve it. base is the URL prefix the caller serves media
// under (e.g. "/media"); it is joined with the cleaned path using "/" and
// never with an OS path separator, since it is a URL. MediaFunc never
// resolves into templates/themes/<name>: a theme's own files are served
// under the theme, not through `media`.
func MediaFunc(mediaFS fs.FS, base string) func(string) (string, error) {
	return func(p string) (string, error) {
		clean, err := cleanMediaPath(p)
		if err != nil {
			return "", err
		}
		if mediaFS == nil {
			return "", libraryErrorf(path.Join(MediaDir, p), 0,
				"media file not found: no templates/media directory")
		}
		if _, statErr := fs.Stat(mediaFS, clean); statErr != nil {
			return "", libraryErrorf(path.Join(MediaDir, clean), 0,
				"media file not found: %v", statErr)
		}
		return strings.TrimSuffix(base, "/") + "/" + clean, nil
	}
}

// cleanMediaPath validates and cleans a `media` argument: it must not be
// absolute and must not escape the media root with a `..` segment.
func cleanMediaPath(p string) (string, error) {
	if p == "" {
		return "", libraryErrorf(MediaDir, 0, "media: path must not be empty")
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") {
		return "", libraryErrorf(path.Join(MediaDir, p), 0,
			"media: path %q must be relative to templates/media, not absolute", p)
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", libraryErrorf(path.Join(MediaDir, p), 0,
			"media: path %q must not escape templates/media with \"..\"", p)
	}
	if clean == "." {
		return "", libraryErrorf(MediaDir, 0, "media: path must not be empty")
	}
	return clean, nil
}

// DictFunc is the `dict` layout constructor: `{{ dict "k1" "v1" "k2" "v2" }}`
// builds a map[string]any from alternating key/value arguments, the syntax a
// layout uses to pass named fields to the `section` helper. Every key must be
// a string — html/template would render a non-string key as a literal number,
// so a key such as `1` is rejected rather than silently mis-typed — and the
// argument count must be even. A malformed call returns an error, not a
// partial map, so it fails the whole template execution the way any
// html/template function error does; an empty call returns an empty, non-nil
// map.
func DictFunc(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf(
			"dict: expected an even number of arguments (key/value pairs), got %d", len(kv))
	}
	out := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf(
				"dict: key %d must be a string, got %s", i/2+1, describeValue(kv[i]))
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("dict: duplicate key %q", key)
		}
		out[key] = kv[i+1]
	}
	return out, nil
}

// LayoutFuncMap returns the complete, documented html/template func map every
// library layout parses and executes with: exactly one helper `media`
// (MediaFunc, bound to mediaFS and base) plus the built-in number/date format
// functions (Format.FuncMap). This is the single source of truth for "the
// documented func set": ParseLayout (used by LoadLibrary's step-3 layout
// parse check) and the renderer both build their func map from here, so a
// layout that parses during validation also executes the same way at render
// time.
func LayoutFuncMap(mediaFS fs.FS, base string) htmltemplate.FuncMap {
	fm := htmltemplate.FuncMap{
		"media": MediaFunc(mediaFS, base),
	}
	for name, fn := range BuiltinFormats.FuncMap() {
		fm[name] = fn
	}
	return fm
}
