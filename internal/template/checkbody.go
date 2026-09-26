package template

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// This file implements CheckBody, the implied-body rule checker (body-rules).
//
// Every template has an implied `body` field whose content comes only from the
// Markdown after the frontmatter (a slide's body) or after a section heading
// (a section's body). It can never be set from YAML: a `body:` key in
// frontmatter or section data is an author error with guidance, reported by
// CheckValues' unknown-key branch (see unknownKey in checkvalues.go), not here.
// CheckBody therefore receives the already-sliced body Markdown only.
//
// A section template used as a field type takes YAML data only, so its body
// rule is BodyDisallowed (Registry rejects a required-body section template
// used as a field type at build time).
//
// CheckBody is deliberately separate from CheckValues: CheckValues checks a
// map of YAML values against a template's field schema and never looks at
// Markdown bodies, while CheckBody checks the one thing a body has — its rule
// — using the same CommonMark core parse internal/mdcheck uses. The accepted
// Markdown subset itself (allowed constructs, link schemes) stays
// internal/mdcheck's and internal/validate's job; CheckBody enforces only the
// body's mode and its size limits.

// bodyField is the reserved name of every template's implied body field. It is
// the PathSegment CheckBody error paths carry, so a body violation adapts
// exactly like a field violation.
const bodyField = "body"

// The reserved `body:` YAML key rejection. CheckBody owns these strings so the
// guidance has one source of truth; CheckValues' unknown-key branch reports
// them (it is the place that sees YAML keys).
const (
	bodyKeyWhat = `unknown field "body"`
	bodyKeyFix  = "body content comes from the Markdown after the frontmatter, not a body: key; body rules are declared in the template"
)

// BodyOption configures a CheckBody call.
type BodyOption func(*bodyConfig)

// bodyConfig holds the optional position source an option supplies.
type bodyConfig struct {
	line int
}

// WithBodyLine supplies the 1-based file line at which the body Markdown
// begins (the line after the frontmatter fence, or after a section heading).
// CheckBody attaches that line to every violation, and uses it to place a
// forbidden-subheading error on the offending subheading's own line. When no
// source is passed, errors carry line 0, matching CheckValues with no
// WithLineSource.
func WithBodyLine(line int) BodyOption {
	return func(c *bodyConfig) { c.line = line }
}

// CheckBody checks one Markdown body against a template's implied `body` rule
// and returns the first violation, plus whether one was found. When the body
// satisfies the rule the returned ValueError is the zero value, so a caller
// reports the first error like:
//
//	if ve, invalid := template.CheckBody(tmpl.Body, body, template.WithBodyLine(line)); invalid {
//		fmt.Fprintln(os.Stderr, ve.Error())
//	}
//
// The body is the Markdown after the frontmatter (or after a section heading)
// only; the caller slices it and passes WithBodyLine so positions are
// file-absolute. A body that is empty or only whitespace counts as absent.
//
// The rules are checked in a fixed, deterministic order, so the same body
// always yields the same first error:
//
//  1. Mode required with an absent body — rule "required";
//  2. Mode disallowed with a present body — rule "disallowed";
//  3. MaxWords exceeded (words of the Markdown-stripped text) — "max_words";
//  4. MaxParagraphs exceeded — "max_paragraphs";
//  5. MaxListItems exceeded — "max_list_items";
//  6. Subheadings forbidden but the body has a `##`/`###` heading —
//     "subheadings".
//
// A zero or negative MaxWords, MaxParagraphs or MaxListItems means no limit,
// matching BodyRule's documentation. An unrecognised BodyMode is treated as
// optional (build-time checks own mode validity), so CheckBody never reports a
// mode error of its own.
//
// The return value is the same positioned ValueError shape CheckValues uses,
// with the implied `body` path, so internal/validate adapts it through
// adaptValueError without change. Each error states the rule and the actual
// value and carries a fix, consistent with checkvalues.go.
func CheckBody(rule BodyRule, body string, opts ...BodyOption) (ValueError, bool) {
	var cfg bodyConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	path := []PathSegment{{Name: bodyField}}
	present := strings.TrimSpace(body) != ""

	if !present {
		if rule.Mode == BodyRequired {
			return ValueError{
				Path: path,
				Line: cfg.line,
				Rule: "required",
				What: "required: the body is required but missing",
				Fix:  "add body text after the frontmatter",
			}, true
		}
		// Optional and disallowed templates accept an absent body.
		return ValueError{}, false
	}

	if rule.Mode == BodyDisallowed {
		words := len(strings.Fields(markdownText(body, true)))
		return ValueError{
			Path:  path,
			Line:  cfg.line,
			Rule:  "disallowed",
			Value: body,
			What:  fmt.Sprintf("disallowed: the body is not allowed, but the body has %d word(s)", words),
			Fix:   "remove the body text; this template takes data only",
		}, true
	}

	st := scanBody(body, cfg.line)

	if rule.MaxWords > 0 && st.words > rule.MaxWords {
		return ValueError{
			Path:  path,
			Line:  cfg.line,
			Rule:  "max_words",
			Value: body,
			What:  fmt.Sprintf("max_words: the body is %d words, maximum is %d", st.words, rule.MaxWords),
			Fix:   fmt.Sprintf("shorten the body to at most %d words", rule.MaxWords),
		}, true
	}
	if rule.MaxParagraphs > 0 && st.paragraphs > rule.MaxParagraphs {
		return ValueError{
			Path:  path,
			Line:  cfg.line,
			Rule:  "max_paragraphs",
			Value: body,
			What:  fmt.Sprintf("max_paragraphs: the body has %d paragraphs, maximum is %d", st.paragraphs, rule.MaxParagraphs),
			Fix:   fmt.Sprintf("shorten the body to at most %d paragraphs", rule.MaxParagraphs),
		}, true
	}
	if rule.MaxListItems > 0 && st.listItems > rule.MaxListItems {
		return ValueError{
			Path:  path,
			Line:  cfg.line,
			Rule:  "max_list_items",
			Value: body,
			What:  fmt.Sprintf("max_list_items: the body has %d list items, maximum is %d", st.listItems, rule.MaxListItems),
			Fix:   fmt.Sprintf("shorten the body to at most %d list items", rule.MaxListItems),
		}, true
	}
	if !rule.Subheadings && st.firstSubhead != nil {
		return ValueError{
			Path:  path,
			Line:  st.firstSubheadLine,
			Rule:  "subheadings",
			Value: st.firstSubheadSource,
			What:  fmt.Sprintf("subheadings: %s is a subheading, but subheadings are not allowed", strconv.Quote(st.firstSubheadSource)),
			Fix:   "remove the subheading, or express it as a field or a new slide",
		}, true
	}

	return ValueError{}, false
}

// bodyStats is the measured shape of one body: its Markdown-stripped word
// count and its paragraph, list-item and first-subheading counts.
type bodyStats struct {
	// words is the number of whitespace-separated words in the body's
	// Markdown-stripped text.
	words int

	// paragraphs is the number of Markdown paragraphs.
	paragraphs int

	// listItems is the number of list items across every list.
	listItems int

	// firstSubhead is the first `##`/`###` heading in document order, or nil
	// when the body has none.
	firstSubhead *ast.Heading

	// firstSubheadSource is the offending heading's source line, for example
	// "## Details", and firstSubheadLine its file line (0 when unknown).
	firstSubheadSource string
	firstSubheadLine   int
}

// scanBody parses body with goldmark's CommonMark core — the same parser
// internal/mdcheck uses — and measures it. startLine is the body's 1-based file
// line, used to place the first subheading on its own line; 0 means unknown.
func scanBody(body string, startLine int) bodyStats {
	src := []byte(body)
	st := bodyStats{words: len(strings.Fields(markdownText(body, true)))}

	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(src))
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.Paragraph:
			// Only top-level paragraphs are prose blocks; a loose list item's
			// content is wrapped in a Paragraph too, and counting those would
			// make max_paragraphs depend on list looseness.
			if n.Parent() == doc {
				st.paragraphs++
			}
		case *ast.ListItem:
			st.listItems++
		case *ast.Heading:
			if (t.Level == 2 || t.Level == 3) && st.firstSubhead == nil {
				st.firstSubhead = t
				st.firstSubheadSource = headingSourceLine(src, t)
				st.firstSubheadLine = headingLine(src, t, startLine)
			}
		}
		return ast.WalkContinue, nil
	})
	return st
}

// headingLine returns the 1-based file line of h, or 0 when startLine is
// unknown. The count of newlines before the heading's first source segment
// gives the line within the body, which the body's start line then shifts to
// file-absolute.
func headingLine(src []byte, h *ast.Heading, startLine int) int {
	if startLine < 1 {
		return 0
	}
	off := 0
	if lines := h.Lines(); lines != nil && lines.Len() > 0 {
		off = lines.At(0).Start
	}
	if off > len(src) {
		off = len(src)
	}
	line := 1
	for i := 0; i < off; i++ {
		if src[i] == '\n' {
			line++
		}
	}
	return startLine + line - 1
}

// headingSourceLine returns h's source line, for example "## Details", by
// expanding the heading's first segment back to the start of its line and
// forward to the end.
func headingSourceLine(src []byte, h *ast.Heading) string {
	off := 0
	if lines := h.Lines(); lines != nil && lines.Len() > 0 {
		off = lines.At(0).Start
	}
	if off > len(src) {
		off = len(src)
	}
	start := off
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	end := off
	for end < len(src) && src[end] != '\n' {
		end++
	}
	return strings.TrimSpace(string(src[start:end]))
}
