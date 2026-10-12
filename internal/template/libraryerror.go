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

// templateErrorLocation matches the location prefix Go's html/template gives
// its errors: "template: <name>:<line>: " for a text/template parse or execute
// error and "html/template:<name>:<line>:<col>: " for an html/template error
// (for example "no such template"). The name is the template's namespace key —
// a layer-relative layout name for a library layout, never a file path — so it
// must be replaced by the layout's on-disk display path rather than shown. The
// match is deliberately unanchored: a caller may wrap the Go error (for
// example `template %q: parse layout: %w`), and the location is still the only
// place a layer-relative name could be read as a file location.
var templateErrorLocation = regexp.MustCompile(`(?:html/)?template:\s*[^:\n]*:(\d+)(?::\d+)?: `)

// layoutTemplateError positions a Go html/template error at layout's on-disk
// display path, Layout.Path, and the line the Go error reports, rewriting the
// error's `template: <name>:<line>:` location prefix — whose name is the
// layout's namespace key — to `<Layout.Path>:<line>`. When Layout.Path is
// empty — a generated layout with no on-disk file, such as the gallery wrapper
// — the position falls back to Layout.Name. Only that leading location prefix
// is rewritten, so a layer-relative layout name is never rendered as a file
// location while any quoted `{{ template }}` target name inside err stays
// exactly as written. context, when non-empty, is kept as the message's
// leading qualifier (for example "execute"), preserving the caller's own
// description of what failed. An error that carries no location prefix keeps
// its text and reports no line (0).
func layoutTemplateError(layout Layout, context string, err error) *LibraryError {
	path := layout.Path
	if path == "" {
		path = layout.Name
	}
	line, message := 0, err.Error()
	if m := templateErrorLocation.FindStringSubmatchIndex(message); m != nil {
		// Group 1 (m[2]:m[3]) is the line; the whole match, including its
		// trailing ": ", is the location prefix to strip.
		if n, convErr := strconv.Atoi(message[m[2]:m[3]]); convErr == nil {
			line = n
		}
		message = message[:m[0]] + message[m[1]:]
	}
	if context != "" {
		message = context + ": " + message
	}
	return &LibraryError{Path: path, Line: line, Message: message}
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
