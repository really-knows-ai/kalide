// Package validate contains the ey-present deck validator: it walks a deck in
// a fixed deterministic order and reports exactly the first error found.
//
// This file defines the single author-facing error shape, ValidationError, and
// the one Format function that renders it. The rendered text is shared
// verbatim by terminal output and by the browser's full-page error page, so the
// format lives here once rather than being duplicated at either call site
// (requirements.requirement.error-reporting,
// requirements.requirement.whole-deck-validation).
package validate

import (
	"fmt"
	"strings"
)

// segmentSeparator joins the elements of a ValidationError's Path, and also
// separates the file position from the first path segment. It is the
// `›` (U+203A) used by the specification example.
const segmentSeparator = " › "

// fixSeparator introduces the "how to fix it" clause.
const fixSeparator = " — "

// ValidationError is one author-facing validation failure.
//
// It carries everything the single formatter needs to produce the text shared
// by terminal output and the browser error page: the file, the line when
// known, the full path to the offending value inside the slide, what is wrong,
// and how to fix it. The example from the specification is:
//
//	slides/3-team.md:12 › column[1] › people[0] › name: required — add a name: value
//
// Validation is fail-fast, so at most one of these is ever reported for a deck.
type ValidationError struct {
	// File is the deck-relative path the error belongs to, for example
	// "eypres.yaml" or "slides/3-team.md".
	File string

	// Line is the 1-based line within File. It is 0 when no meaningful line
	// is known (a filename-only error, a whole-file error, or a value with no
	// source position); the formatter omits the `:line` suffix then.
	Line int

	// Path is the rendered chain of path segments locating the offending
	// value inside the slide, outermost first, for example
	// []string{"column[1]", "people[0]", "name"}. Each element is already in
	// its final display form (indexes like people[0] included). It is empty
	// when the error applies to the file or slide as a whole; the formatter
	// omits the path part then.
	Path []string

	// What is the author-facing "what is wrong" text, for example "required".
	What string

	// Fix is the author-facing "how to fix it" text, for example
	// "add a name: value". It is empty when there is nothing useful to add;
	// the formatter omits the ` — fix` clause then.
	Fix string
}

// New builds a ValidationError, taking a copy of the path segments. It is a
// convenience for callers that construct errors inline; it deliberately has no
// dependency on any other internal package, so the phase-2/3/4 adapters can
// use it without an import cycle.
func New(file string, line int, path []string, what, fix string) ValidationError {
	return ValidationError{
		File: file,
		Line: line,
		Path: append([]string(nil), path...),
		What: what,
		Fix:  fix,
	}
}

// Error implements the error interface by delegating to Format, so a
// ValidationError can be returned and printed directly.
func (e ValidationError) Error() string {
	return Format(e)
}

// Format renders e as the single author-facing error string shared by terminal
// output and the browser error page (requirements.requirement.error-reporting):
//
//	file:line › path: what — fix
//
// The `:line` suffix is omitted entirely when e.Line is 0, and the ` › path`
// part is omitted when e.Path is empty. Path segments are joined with ` › `,
// the file/line and path are separated from What by `: `, and Fix is
// introduced by ` — ` and omitted when empty. Formatting the specification
// example:
//
//	Format(ValidationError{
//	    File: "slides/3-team.md", Line: 12,
//	    Path: []string{"column[1]", "people[0]", "name"},
//	    What: "required", Fix: "add a name: value",
//	})
//	// slides/3-team.md:12 › column[1] › people[0] › name: required — add a name: value
func Format(e ValidationError) string {
	var b strings.Builder
	b.WriteString(e.File)
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	if len(e.Path) > 0 {
		b.WriteString(segmentSeparator)
		b.WriteString(strings.Join(e.Path, segmentSeparator))
	}
	b.WriteString(": ")
	b.WriteString(e.What)
	if e.Fix != "" {
		b.WriteString(fixSeparator)
		b.WriteString(e.Fix)
	}
	return b.String()
}
