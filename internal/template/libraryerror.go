package template

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/really-knows-ai/kalide/internal/suggest"
)

// This file implements LibraryError, the error type LoadLibrary and its
// helpers (MediaFunc, media.go) return for every templates/ problem. It is
// labelled as a templates error so a caller can tell it apart from a deck
// content error with errors.As, path-qualifies the offending location
// (templates/…/file:line), and carries an optional hint and closest-match
// suggestion. Its Error() rendering follows the same file:line: message
// convention as internal/deck's positioned errors and internal/theme's
// UnknownThemeError, so a templates/ problem reads the same way a deck
// problem does.

// LibraryError reports one problem found while loading or using a project's
// templates/ library (templates-dir-validation, templates-dir-required).
//
// Path is always relative to the project root (so it reads "templates/…",
// not the bare templates/ sub-filesystem path), which is what makes the
// error templates-labelled and path-qualified: a reader sees at a glance that
// the problem is in the template library, not in the deck.
type LibraryError struct {
	// Path is the offending file or directory, relative to the project
	// root, e.g. "templates/slides/hello/template.yaml". It is never empty:
	// a library-wide problem (a missing templates/ directory) names the
	// expected path.
	Path string

	// Line is the 1-based line within Path, or 0 when the problem has no
	// interior line (a missing file, a missing directory, a whole-file
	// problem).
	Line int

	// Message is the problem, without any path or line prefix.
	Message string

	// Hint is an optional short remediation, rendered in parentheses, e.g.
	// "run `kalide init` to create one".
	Hint string

	// Suggestion is an optional closest-match candidate name, rendered as
	// "did you mean %q?". It is "" when there is no close candidate.
	Suggestion string
}

// Error renders the message as "<path>[:<line>]: <message>[: did you mean
// "<suggestion>"?][ (<hint>)]", the same file:line: message shape
// internal/deck's positioned errors and internal/theme's UnknownThemeError
// use.
func (e *LibraryError) Error() string {
	var b strings.Builder
	b.WriteString(e.Path)
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	b.WriteString(": ")
	b.WriteString(e.Message)
	if e.Suggestion != "" {
		fmt.Fprintf(&b, ": did you mean %q?", e.Suggestion)
	}
	if e.Hint != "" {
		fmt.Fprintf(&b, " (%s)", e.Hint)
	}
	return b.String()
}

// libraryErrorf builds a *LibraryError positioned at path:line (line 0 for
// none) with a formatted message and no hint or suggestion.
func libraryErrorf(path string, line int, format string, args ...any) *LibraryError {
	return &LibraryError{Path: path, Line: line, Message: fmt.Sprintf(format, args...)}
}

// withHint returns a copy of e with Hint set.
func (e *LibraryError) withHint(hint string) *LibraryError {
	c := *e
	c.Hint = hint
	return &c
}

// withSuggestion returns a copy of e with Suggestion set to the closest
// candidate to name among candidates, when one is close enough.
func (e *LibraryError) withSuggestion(name string, candidates []string) *LibraryError {
	c := *e
	c.Suggestion = suggest.Closest(name, candidates)
	return &c
}

// LayoutPathResolver maps a layout's namespace name — the key Go's
// html/template reports in an error's `template: <name>:<line>:` (text/template)
// or `html/template:<name>:<line>:<col>:` (html/template) location prefix — to
// that layout's on-disk display path. It returns "" when it does not know name,
// and layoutTemplateError then falls back to the invoking layout's own Path (or,
// when that is empty, its Name). A caller builds one resolver from the
// namespace it parsed, so an error raised inside a composed layout is positioned
// at THAT layout's file rather than at the layout that invoked it.
type LayoutPathResolver func(name string) string

// templateErrorLocation matches the location prefix Go's html/template gives
// its errors: "template: <name>:<line>: " for a text/template parse or execute
// error and "html/template:<name>:<line>:<col>: " for an html/template error
// (for example "no such template"). Group 1 is <name>, the template's namespace
// key — a layer-relative layout name for a library layout, never a file path —
// and group 2 is <line>. The name is mapped to the owning layout's on-disk
// display path rather than shown. The match is deliberately unanchored: a caller
// may wrap the Go error (for example `template %q: parse layout: %w`), and the
// location is still the only place a layer-relative name could be read as a
// file location.
var templateErrorLocation = regexp.MustCompile(`(?:html/)?template:\s*([^:\n]*):(\d+)(?::\d+)?: `)

// layoutTemplateError positions a Go html/template error at the on-disk display
// path of the layout the error is attributed to and the line the Go error
// reports, rewriting the error's `template: <name>:<line>:` location prefix.
//
// The prefix's <name> is a layout's namespace key — a layer-relative layout name
// for a library layout, never a file path — so it is mapped through resolve to
// that layout's on-disk Path rather than assumed to be the invoking layout's.
// resolve is the resolver the caller builds from the namespace it parsed (for
// example every library layout's Layout.Name() mapped to its LayoutPath or
// Layout.Path); it may be nil. When the prefix's name equals layout's own
// Name — the single-template case, where the invoking layout is the only
// candidate — or resolve returns "" for it, the position falls back to layout's
// Path and, when that is empty (a generated layout with no on-disk file, such
// as the gallery wrapper), to layout's Name. Only the leading location prefix
// is rewritten, so a layer-relative layout name is never rendered as a file
// location while any quoted `{{ template }}` target name inside err stays
// exactly as written. context, when non-empty, is kept as the message's leading
// qualifier (for example "execute"), preserving the caller's own description of
// what failed. An error that carries no location prefix keeps its text and
// reports no line (0).
func layoutTemplateError(layout Layout, resolve LayoutPathResolver, context string, err error) *LibraryError {
	path := layout.Path
	if path == "" {
		path = layout.Name
	}
	line, message := 0, err.Error()
	if m := templateErrorLocation.FindStringSubmatchIndex(message); m != nil {
		// Group 1 (m[2]:m[3]) is the prefix's template name; group 2
		// (m[4]:m[5]) is the line. Only a name other than the invoking
		// layout's — already represented by path — asks the resolver for the
		// owning layout's file. The whole match, including its trailing ": ",
		// is the location prefix to strip.
		if name := message[m[2]:m[3]]; name != "" && name != layout.Name && resolve != nil {
			if resolved := resolve(name); resolved != "" {
				path = resolved
			}
		}
		if n, convErr := strconv.Atoi(message[m[4]:m[5]]); convErr == nil {
			line = n
		}
		message = message[:m[0]] + message[m[1]:]
	}
	if context != "" {
		message = context + ": " + message
	}
	return &LibraryError{Path: path, Line: line, Message: message}
}

// LayoutTemplateError is the exported form of layoutTemplateError, so
// internal/render can share this one implementation: a render-time layout parse
// or execute error is attributed and positioned exactly as a load-time one. The
// parameters are layoutTemplateError's — layout is the invoking layout, resolve
// maps a namespace name found in err to the owning layout's on-disk Path (and
// may be nil), context is the message's leading qualifier, and err is the Go
// html/template error. It is a thin wrapper with layoutTemplateError's
// behaviour, documented in full there.
func LayoutTemplateError(layout Layout, resolve LayoutPathResolver, context string, err error) *LibraryError {
	return layoutTemplateError(layout, resolve, context, err)
}

// namespaceLayoutResolver returns a LayoutPathResolver for a namespace that
// parsed a single layout under name: it maps name to path and every other name
// to "". A caller that parses a whole shared namespace builds a resolver over
// every layout instead (libraryLayoutPathResolver). It returns nil when either
// part is empty, so the caller falls back to the invoking layout's Path.
func namespaceLayoutResolver(name, path string) LayoutPathResolver {
	if name == "" || path == "" {
		return nil
	}
	return func(n string) string {
		if n == name {
			return path
		}
		return ""
	}
}

// libraryLayoutPathResolver returns a LayoutPathResolver over every slide and
// section layout of lib: each layout's namespace key (its parsed Layout.Name(),
// falling back to the template name) mapped to its on-disk display path
// (LibraryTemplate.LayoutPath). It is the resolver the callers that parse the
// merged per-layer layout namespace (checkLibraryExample, and the section
// helper's runner) build, so an error raised inside any composed layout is
// positioned at that layout's own file rather than the invoking one. A nil or
// empty-footprint lib returns nil.
func libraryLayoutPathResolver(lib *Library) LayoutPathResolver {
	if lib == nil {
		return nil
	}
	paths := make(map[string]string, len(lib.Slides)+len(lib.Sections))
	add := func(lt *LibraryTemplate) {
		if lt == nil || lt.LayoutPath == "" {
			return
		}
		name := lt.Name
		if lt.Layout != nil && lt.Layout.Name() != "" {
			name = lt.Layout.Name()
		}
		if name != "" {
			paths[name] = lt.LayoutPath
		}
	}
	for _, lt := range lib.Slides {
		add(lt)
	}
	for _, lt := range lib.Sections {
		add(lt)
	}
	if len(paths) == 0 {
		return nil
	}
	return func(name string) string { return paths[name] }
}

// missingTemplatesDirError reports that the project has no usable
// templates/ directory: expectedPath is the path LoadLibrary looked for
// (typically TemplatesDir, "templates"), and reason names what was wrong
// (absent, or present but not a directory). It always carries the `kalide
// init` hint (templates-dir-required).
func missingTemplatesDirError(expectedPath, reason string) *LibraryError {
	return libraryErrorf(expectedPath, 0, "%s", reason).
		withHint("run `kalide init` to create one")
}
