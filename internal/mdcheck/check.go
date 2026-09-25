// Package mdcheck validates the Markdown subset an eypres presentation may use
// (markdown-allowed-subset, markdown-disallowed-errors).
//
// Content is parsed with goldmark's CommonMark core and no extensions, so
// extension syntax (~~strike~~, ==highlight==, tables, task lists, …) is never
// silently interpreted by the parser: it is left as literal text for the
// disallowed-construct checks to report. The parsed AST is then walked against
// an allow-list of constructs (allowedConstructs). Anything not on the list is
// rejected by default, so unknown or future node kinds fail closed.
//
// The accepted subset is deliberately small:
//
//   - paragraphs;
//   - headings at levels 2 and 3 only (`##`, `###`); `#` is reserved for slide
//     sections and levels 4+ are not part of the slide typography. (The
//     CommonMark AST does not distinguish ATX from setext headings; rejecting
//     setext underlines is the disallowed-construct task's job.);
//   - bulleted and numbered lists, nested at most one level deep;
//   - bold and italic emphasis;
//   - inline code;
//   - inline `[text](url)` links only (reference links and autolinks are
//     rejected);
//   - hard line breaks.
//
// Check reports every violation as a positioned Issue. It is deliberately
// silent about link destinations and about extension-syntax heuristics beyond
// the allow-list: those are separate concerns layered on the same walker (see
// checkDisallowed).
package mdcheck

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/text"
)

// Mode selects what kind of content a call to Check is validating. The
// distinction matters because inline text fields (frontmatter values, section
// fields) may only contain inline constructs, while slide and section bodies
// may also contain the accepted block constructs.
type Mode int

const (
	// BodyMode validates slide or section body content: the accepted block
	// constructs plus the accepted inline constructs.
	BodyMode Mode = iota

	// InlineMode validates an inline text field: only inline constructs are
	// accepted, and any block construct is an error. A single paragraph is the
	// parser's wrapper for such a field and is not treated as an authoring
	// block construct.
	InlineMode
)

// String names the mode for diagnostics.
func (m Mode) String() string {
	if m == InlineMode {
		return "inline"
	}
	return "body"
}

// Options configures a Check call.
type Options struct {
	// Mode selects body or inline-field validation. The zero value is
	// BodyMode.
	Mode Mode

	// StartLine is the 1-based line in the containing file at which src
	// begins. Slide bodies start after the slide frontmatter and section
	// bodies after a section heading, so callers pass the known offset to get
	// file-absolute positions. It defaults to 1 when unset.
	StartLine int
}

// Check parses src as the Markdown subset and returns every Issue it finds, in
// document order (which is source order for the accepted constructs). file is
// the identity used in issue positions, typically the slide's path.
//
// src is an arbitrary fragment: Check does not require or interpret
// frontmatter. The caller is responsible for slicing bodies and inline fields
// and for passing the matching Options.StartLine.
//
// An empty src is valid and yields no issues. Check never returns a non-nil
// error: a construct the parser does not understand simply surfaces as a
// default-deny Issue.
func Check(file string, src []byte, opts Options) []Issue {
	if opts.StartLine < 1 {
		opts.StartLine = 1
	}
	ctx := &walkContext{
		file:      file,
		source:    src,
		lines:     newLineIndex(src),
		mode:      opts.Mode,
		startLine: opts.StartLine,
	}

	// goldmark.New() with no options is the CommonMark core specification: no
	// GFM/table/strikethrough/task-list/definition-list extensions are
	// registered, so extension syntax stays literal text.
	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(src))
	return walk(ctx, doc)
}
