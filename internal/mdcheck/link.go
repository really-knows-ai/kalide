package mdcheck

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Link is one inline Markdown link (`[text](destination)`) found by
// ExtractLinks, together with its source position.
//
// ExtractLinks is the single collector for links in a Markdown fragment; it
// does not judge whether a link is otherwise well formed (that is Check's job)
// and it does not resolve labels (that is the phase-5 validator's job). It
// reports exactly the two things the link layer owns: the links themselves and
// the destinations whose scheme is not allowed.
type Link struct {
	// Text is the link's visible label text, flattened to plain text. It is
	// best-effort: it concatenates the label's text and inline-code content and
	// does not reconstruct emphasis markers.
	Text string

	// Destination is the raw link destination as written, for example
	// "#speaker-notes" or "https://example.com". Surrounding whitespace and any
	// angle-bracket wrapper have already been removed by the Markdown parser.
	Destination string

	// Label is the slide label a `#label` destination targets, without the
	// leading '#'. It is empty for http(s) links and for no other destination
	// (any non-#, non-http(s) destination is an Issue, never a Link target).
	//
	// ExtractLinks deliberately does not check that the label exists: it only
	// exposes the targets. The phase-5 validator collects these alongside the
	// labels declared by slides and reports an unknown label there, once every
	// label in the deck is known.
	Label string

	// Line is the 1-based line within the checked file of the link's opening
	// '['. It is 0 only when no position could be derived from the AST.
	Line int

	// Col is the 1-based byte column within Line of the link's opening '[', or
	// 0 when unknown.
	Col int
}

// ExtractLinks parses src as the Markdown subset and returns every inline
// `[text](destination)` link, in document order, plus a positioned Issue for
// every link destination whose scheme is not allowed.
//
// The allowed destinations are:
//
//   - `#label` — an inter-slide link; the label is exposed on Link.Label and is
//     not checked for existence here;
//   - an `http` or `https` URL (the scheme is matched case-insensitively).
//
// Every other destination is a positioned error of Kind KindLinkScheme,
// including `mailto:` and `ftp:` URLs, protocol-relative destinations
// (`//example.com`), bare relative paths (`other.md`, `./other.md`,
// `../other.md`) and absolute filesystem paths (`/assets/x.png`).
//
// file is the identity used in issue positions, typically the slide's path.
// opts carries the same StartLine offset as Check so a body or inline field
// sliced out of a slide gets file-absolute positions; opts.Mode does not affect
// link extraction (links are always inline constructs) but is accepted so
// callers can share one Options value with Check.
//
// Reference-style links (`[text][ref]`) and angle-bracket autolinks are not
// inline `[]()` links: they are rejected as disallowed constructs by Check and
// are never returned here.
//
// An empty src is valid and yields no links and no issues.
func ExtractLinks(file string, src []byte, opts Options) ([]Link, []Issue) {
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

	// Same parse as Check: goldmark's CommonMark core with no extensions. Only
	// the inline link nodes are of interest, so the returned document is walked
	// for them directly rather than through the validation walker, whose
	// allow-list and default-deny rules are a separate concern.
	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(src))

	var links []Link
	var issues []Issue
	collectLinks(ctx, doc, &links, &issues)
	return links, issues
}

// collectLinks appends every inline link in node's subtree to links, in
// document order, and every scheme Issue to issues. It descends into every child
// so links nested inside otherwise accepted inline constructs are found; a link
// has no source segment of its own, so its position comes from ctx.position
// (the start of its label, as for every other inline wrapper).
func collectLinks(ctx *walkContext, node ast.Node, links *[]Link, issues *[]Issue) {
	if l, ok := node.(*ast.Link); ok && l.Reference == nil {
		*links = append(*links, ctx.linkInfo(l))
		if issue, bad := ctx.checkLinkScheme(l); bad {
			*issues = append(*issues, issue)
		}
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		collectLinks(ctx, child, links, issues)
	}
}

// linkInfo builds the Link value for one inline link node.
func (ctx *walkContext) linkInfo(l *ast.Link) Link {
	dest := string(l.Destination)
	label := ""
	if kind, target := classifyDestination(dest); kind == destLabel {
		label = target
	}
	line, col := ctx.position(l)
	return Link{
		Text:        ctx.linkText(l),
		Destination: dest,
		Label:       label,
		Line:        line,
		Col:         col,
	}
}

// checkLinkScheme reports an Issue when l's destination is neither a `#label`
// reference nor an http(s) URL. Every rejection uses KindLinkScheme so the
// phase-5 formatter can treat link destinations as one class of error.
func (ctx *walkContext) checkLinkScheme(l *ast.Link) (Issue, bool) {
	dest := string(l.Destination)
	if kind, _ := classifyDestination(dest); kind != destOther {
		return Issue{}, false
	}

	guidance := "use a #label link or an http(s) URL, for example [text](#label) or [text](https://example.com)"
	d := strings.TrimSpace(dest)
	if d == "" {
		return ctx.newIssue(l, KindLinkScheme,
			"link destination is empty",
			guidance), true
	}
	if scheme, ok := uriScheme(d); ok {
		return ctx.newIssue(l, KindLinkScheme,
			fmt.Sprintf("link destination %q uses the unsupported %q scheme", dest, scheme),
			guidance), true
	}
	return ctx.newIssue(l, KindLinkScheme,
		fmt.Sprintf("relative link destination %q is not allowed", dest),
		guidance), true
}

// destinationKind classifies a link destination.
type destinationKind int

const (
	// destLabel is a `#label` reference.
	destLabel destinationKind = iota
	// destHTTP is an http or https URL.
	destHTTP
	// destOther is any other destination, which is an error.
	destOther
)

// classifyDestination classifies the raw destination dest and, for a `#label`
// reference, returns the label without its leading '#'.
//
// The destination is trimmed first because the parser may leave surrounding
// whitespace. A leading '#' is always the label form. Otherwise the destination
// is classified by URI scheme: the scheme is matched case-insensitively against
// http and https, and every other scheme, a destination with no scheme (a
// relative path or a protocol-relative `//host` destination) and an empty
// destination are destOther.
func classifyDestination(dest string) (destinationKind, string) {
	d := strings.TrimSpace(dest)
	if strings.HasPrefix(d, "#") {
		return destLabel, d[1:]
	}
	if scheme, ok := uriScheme(d); ok {
		switch strings.ToLower(scheme) {
		case "http", "https":
			return destHTTP, ""
		}
	}
	return destOther, ""
}

// uriScheme returns the URI scheme of a destination: the prefix before the
// first ':' when that prefix is a syntactically valid scheme (an ASCII letter
// followed by letters, digits, '+', '-' or '.'). It returns false when the
// destination has no scheme, which is how a relative or protocol-relative
// destination is recognised.
func uriScheme(d string) (string, bool) {
	for i := 0; i < len(d); i++ {
		if d[i] == ':' {
			return d[:i], i > 0
		}
		if !isSchemeByte(d[i], i == 0) {
			return "", false
		}
	}
	return "", false
}

// isSchemeByte reports whether c is allowed in a URI scheme at the given
// position: the first byte must be an ASCII letter, later bytes may also be
// digits, '+', '-' or '.'.
func isSchemeByte(c byte, first bool) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
		return true
	}
	if first {
		return false
	}
	if c >= '0' && c <= '9' {
		return true
	}
	return c == '+' || c == '-' || c == '.'
}

// linkText flattens the visible text of an inline link to a plain string. It
// walks the link's children (text, inline code, emphasis) and concatenates
// their content; soft and hard line breaks contribute nothing, matching how
// ast.Text segments exclude the newline.
func (ctx *walkContext) linkText(l *ast.Link) string {
	var b strings.Builder
	appendLinkText(ctx, l, &b)
	return b.String()
}

// appendLinkText writes the text content of n and its descendants to b.
func appendLinkText(ctx *walkContext, n ast.Node, b *strings.Builder) {
	switch t := n.(type) {
	case *ast.Text:
		b.Write(t.Segment.Value(ctx.source))
	case *ast.String:
		b.Write(t.Value)
	}
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		appendLinkText(ctx, child, b)
	}
}
