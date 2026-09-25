package mdcheck

import "fmt"

// Kind classifies the Markdown construct or rule an Issue concerns. It is a
// plain string type so later layers can add construct kinds without changing
// this type, and so the phase-5 formatter can branch on it when it needs to
// (the formatter only needs Message and Guidance to render the author-facing
// text; Kind is metadata).
type Kind string

const (
	// KindUnsupportedConstruct is a node kind outside the allow-list (the
	// default-deny path), or an allowed kind used in a disallowed way (for
	// example a heading at a level other than 2 or 3, or over-deep list
	// nesting).
	KindUnsupportedConstruct Kind = "unsupported-construct"

	// KindBlockConstruct is a block construct found in an inline text field,
	// where only inline constructs are accepted.
	KindBlockConstruct Kind = "block-construct"

	// KindDisallowedConstruct is reserved for the phase-3 disallowed-construct
	// checks (task-2): known Markdown syntax with a tailored message, such as
	// images, tables, block quotes, fenced/indented code, thematic breaks, raw
	// HTML, autolinks, reference links and extension syntax. Those checks fill
	// the checkDisallowed seam.
	KindDisallowedConstruct Kind = "disallowed-construct"

	// KindLinkScheme is reserved for the phase-3 link-scheme check (task-3):
	// a link destination whose scheme is neither `#label` nor http(s).
	KindLinkScheme Kind = "link-scheme"
)

// Issue is one positioned Markdown-subset violation.
//
// It is the single error shape mdcheck exposes, shared by the disallowed
// construct checks and (later) link extraction. The phase-5 validator adapts
// it into its own ValidationError, mapping Message to its "what" and Guidance
// to its "fix"; the browser error page and terminal output render both through
// that formatter.
type Issue struct {
	// File is the identity the content was checked as, typically the slide's
	// path.
	File string

	// Line is the 1-based line within File. It is 0 only when no position
	// could be derived from the AST.
	Line int

	// Col is the 1-based byte column within Line, or 0 when unknown.
	Col int

	// Kind classifies the construct or rule.
	Kind Kind

	// Message is the author-facing "what is wrong" text.
	Message string

	// Guidance is the author-facing "how to fix it" text. It is empty when
	// there is nothing useful to add.
	Guidance string
}

// Error renders the issue as a positioned message. The final author-facing
// format belongs to the phase-5 formatter; this is a convenience for tests,
// logs and callers that want a single string.
func (i Issue) Error() string {
	pos := i.File
	if i.Line > 0 {
		pos = fmt.Sprintf("%s:%d", pos, i.Line)
		if i.Col > 0 {
			pos = fmt.Sprintf("%s:%d", pos, i.Col)
		}
	}
	if i.Guidance != "" {
		return fmt.Sprintf("%s: %s — %s", pos, i.Message, i.Guidance)
	}
	return fmt.Sprintf("%s: %s", pos, i.Message)
}
