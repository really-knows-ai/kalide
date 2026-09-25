package mdcheck

import (
	"fmt"
	"sort"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// maxListNesting is the deepest list nesting the subset accepts, counted in
// levels of nesting: a top-level list has nesting level 0, a list inside one of
// its items has nesting level 1 (allowed), and a list inside that has nesting
// level 2 (rejected). So bulleted and numbered lists may be nested one level
// deep, and no deeper.
const maxListNesting = 1

// allowedConstructs is the complete allow-list of goldmark node kinds the
// Markdown subset accepts. It is consulted by default: a node kind absent from
// this table is rejected (default-deny), so an unknown or future node kind
// fails closed rather than slipping through.
//
// Hard line breaks have no node kind of their own: the CommonMark parser
// records them as a flag on the surrounding ast.Text (Text.HardLineBreak), so
// they are accepted wherever ast.KindText is.
//
// The allow-list is deliberately a plain table so the phase-3 disallowed
// construct work and tests can read it directly, and so adding a construct is
// a one-line, reviewable change.
var allowedConstructs = map[ast.NodeKind]bool{
	ast.KindDocument:  true, // root wrapper, never reported
	ast.KindParagraph: true,
	ast.KindTextBlock: true, // tight-list content wrapper
	ast.KindHeading:   true, // ## and ### only, enforced by checkDisallowed
	ast.KindList:      true, // one nesting level only, enforced by checkDisallowed
	ast.KindListItem:  true,
	ast.KindText:      true,
	ast.KindString:    true,
	ast.KindCodeSpan:  true,
	ast.KindEmphasis:  true, // bold (level 2) and italic (level 1)
	ast.KindLink:      true, // inline [text](url) only
}

// blockConstructs are the node kinds that count as a block construct. In
// InlineMode they are rejected because an inline text field may only contain
// inline constructs. Paragraph and Document are structural wrappers the parser
// always produces and are not authoring constructs, so they are excluded.
var blockConstructs = map[ast.NodeKind]bool{
	ast.KindHeading:                 true,
	ast.KindBlockquote:              true,
	ast.KindList:                    true,
	ast.KindListItem:                true,
	ast.KindCodeBlock:               true,
	ast.KindFencedCodeBlock:         true,
	ast.KindHTMLBlock:               true,
	ast.KindThematicBreak:           true,
	ast.KindTextBlock:               true,
	ast.KindLinkReferenceDefinition: true,
}

// constructNames maps node kinds to author-facing names for issue messages.
// Kinds without an entry fall back to the kind's own string.
var constructNames = map[ast.NodeKind]string{
	ast.KindDocument:                "the document",
	ast.KindParagraph:               "a paragraph",
	ast.KindTextBlock:               "a text block",
	ast.KindHeading:                 "a heading",
	ast.KindBlockquote:              "a block quote",
	ast.KindList:                    "a list",
	ast.KindListItem:                "a list item",
	ast.KindCodeBlock:               "an indented code block",
	ast.KindFencedCodeBlock:         "a fenced code block",
	ast.KindHTMLBlock:               "raw HTML",
	ast.KindThematicBreak:           "a thematic break",
	ast.KindText:                    "text",
	ast.KindString:                  "text",
	ast.KindCodeSpan:                "inline code",
	ast.KindEmphasis:                "emphasis",
	ast.KindLink:                    "a link",
	ast.KindImage:                   "an image",
	ast.KindAutoLink:                "an autolink",
	ast.KindRawHTML:                 "inline raw HTML",
	ast.KindLinkReferenceDefinition: "a link reference definition",
}

// walkContext carries the state threaded through one walk.
type walkContext struct {
	// file is the content identity used in issue positions.
	file string

	// source is the exact byte slice handed to goldmark; offsets are relative
	// to it.
	source []byte

	// lines maps byte offsets back to 1-based fragment line and column.
	lines *lineIndex

	// mode is body or inline-field validation.
	mode Mode

	// startLine is the 1-based file line of source[0]. Fragment lines are
	// shifted by startLine-1 to become file lines.
	startLine int

	// listDepth is the nesting level of the list currently being visited: 0
	// for a top-level list, 1 inside its items, and so on.
	listDepth int
}

// walk walks the parsed document's children and returns every issue found, in
// document order. The Document node itself is a wrapper and is never reported.
func walk(ctx *walkContext, doc ast.Node) []Issue {
	var issues []Issue
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		issues = append(issues, visit(ctx, child)...)
	}
	return issues
}

// visit checks one node and, when it is accepted, its children.
//
// Order of decisions:
//
//  1. In inline-field mode, any block construct is an error. It is reported
//     once and not descended into, so one block yields one issue.
//  2. checkDisallowed gets first refusal: construct-specific rejections with
//     tailored guidance (this is the phase-3 seam). A non-empty result is
//     final for the node.
//  3. The allow-list is applied: an unknown kind is rejected (default-deny).
//  4. Accepted nodes are descended into, tracking list nesting depth.
func visit(ctx *walkContext, node ast.Node) []Issue {
	if ctx.mode == InlineMode && blockConstructs[node.Kind()] {
		return []Issue{ctx.newIssue(node, KindBlockConstruct,
			fmt.Sprintf("%s is not allowed in an inline text field", constructName(node)),
			"use inline formatting only: bold, italic, inline code or a link")}
	}

	if issues := checkDisallowed(ctx, node); len(issues) > 0 {
		return issues
	}

	if !allowedConstructs[node.Kind()] {
		return []Issue{ctx.newIssue(node, KindUnsupportedConstruct,
			fmt.Sprintf("%s is not allowed here", constructName(node)),
			"use only the supported Markdown subset: paragraphs, ## or ### headings, "+
				"one level of bullet or numbered lists, bold, italic, inline code and [](…) links")}
	}

	var issues []Issue
	if _, ok := node.(*ast.List); ok {
		ctx.listDepth++
		defer func() { ctx.listDepth-- }()
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		issues = append(issues, visit(ctx, child)...)
	}
	return issues
}

// checkDisallowed reports construct-specific rejections: known Markdown
// constructs outside the allow-list, accepted kinds used outside their
// permitted shape, and the extension-syntax heuristics that the CommonMark core
// parser leaves as literal text.
//
// This is the seam the phase-3 disallowed-construct task fills in. A non-empty
// result is final for the node: visit records the issues and does not descend,
// so a disallowed construct produces exactly one error. Returning nil falls
// through to the default-deny allow-list check in visit, so an unrecognised
// kind still fails closed.
//
// Implemented here (the shape rules that define the accepted subset):
//
//   - headings: the CommonMark parser recognises both ATX (`## x`) and setext
//     (`x` underlined with `---`) headings, and goldmark's AST does not record
//     which form was used (*ast.Heading carries only Level). This check
//     therefore enforces the level rule — 2 and 3 are accepted, `#` is reserved
//     for slide sections and levels 4+ are not part of the slide typography.
//     Rejecting setext headings specifically (only ATX `##`/`###` are allowed)
//     requires inspecting the heading's source line and is the phase-3
//     disallowed-construct task's job; see the seam note below.
//   - lists: at most one nesting level (maxListDepth).
//   - emphasis: only level 1 (italic) and level 2 (bold).
//
// Deliberately NOT implemented here yet (the phase-3 disallowed-construct task
// adds the tailored messages for these): images, tables, block quotes, fenced
// and indented code, thematic breaks, raw HTML (block and inline),
// angle-bracket autolinks, reference-style links and link reference
// definitions, setext heading detection, `#`/`####+` heading guidance, and the
// extension-syntax heuristics (~~strike~~, ==highlight==, ^sup^, ~sub~,
// :emoji:). Until then most of them are still rejected by the default-deny
// allow-list, just with the generic message.
//
// Seam for the phase-3 disallowed-construct task:
//
//   - add the construct-specific `case` clauses to this switch (block and
//     inline raw HTML, image, autolink, thematic break, fenced/indented code,
//     block quote, link reference definition, reference-style link, setext
//     heading, tables, …);
//   - to scan a text run, add a `case *ast.Text:` and regex-match
//     n.Segment.Value(ctx.source), recording ctx.position(node);
//   - text runs inside inline code must not be scanned. A CodeSpan's content is
//     reachable as child Text nodes, so guard with an ancestor check (e.g. walk
//     node.Parent() looking for *ast.CodeSpan) before matching;
//   - table syntax is not part of CommonMark core and produces no AST node (a
//     `| a | b |` block parses as an ordinary paragraph/Text), so tables must be
//     rejected by a text-level heuristic, not by node kind;
//   - ATX and setext headings are indistinguishable in the AST, so rejecting
//     setext headings requires inspecting the heading's source line (the seam
//     is the `case *ast.Heading:` clause above);
//   - InlineMode currently accepts a Paragraph because the parser always wraps
//     an inline field in one. If a second paragraph must be rejected as a block
//     construct, count Paragraph children in InlineMode.
func checkDisallowed(ctx *walkContext, node ast.Node) []Issue {
	switch n := node.(type) {
	case *ast.Heading:
		if n.Level != 2 && n.Level != 3 {
			return []Issue{ctx.newIssue(node, KindUnsupportedConstruct,
				fmt.Sprintf("heading level %d is not allowed", n.Level),
				"use ## or ###; # is reserved for slide sections and ####+ is not supported")}
		}
	case *ast.List:
		if ctx.listDepth > maxListNesting {
			return []Issue{ctx.newIssue(node, KindUnsupportedConstruct,
				"lists may be nested at most one level deep",
				"flatten the nested list into the parent list or a new slide")}
		}
	case *ast.Emphasis:
		if n.Level != 1 && n.Level != 2 {
			return []Issue{ctx.newIssue(node, KindUnsupportedConstruct,
				fmt.Sprintf("emphasis level %d is not allowed", n.Level),
				"use *italic* or **bold**")}
		}
	}
	return nil
}

// newIssue builds a positioned Issue for node.
func (ctx *walkContext) newIssue(node ast.Node, kind Kind, message, guidance string) Issue {
	line, col := ctx.position(node)
	return Issue{
		File:     ctx.file,
		Line:     line,
		Col:      col,
		Kind:     kind,
		Message:  message,
		Guidance: guidance,
	}
}

// position returns the 1-based file line and byte column of node's first
// source byte, or (0, 0) when no position can be derived.
//
// Some inline nodes do not carry a source segment of their own (notably
// ast.AutoLink, whose URL is stored decoded). For those, the position falls
// back to the containing block's first line, which is the most precise
// position the CommonMark AST makes available.
func (ctx *walkContext) position(node ast.Node) (line, col int) {
	offset, ok := nodeOffset(node)
	if !ok {
		for p := node.Parent(); p != nil && !ok; p = p.Parent() {
			offset, ok = nodeOffset(p)
		}
	}
	if !ok {
		return 0, 0
	}
	fragLine, fragCol := ctx.lines.at(offset)
	if fragLine < 1 {
		return 0, 0
	}
	return ctx.startLine + fragLine - 1, fragCol
}

// lineBlock is implemented by every goldmark block node (they embed
// ast.BaseBlock): it exposes the node's source segments.
type lineBlock interface {
	Lines() *text.Segments
}

// nodeOffset returns the byte offset of node's first source byte. Block nodes
// report their first line segment; inline raw HTML reports its own segment
// list; other inline nodes are searched for their first descendant text segment
// (which also anchors wrappers such as links and emphasis, whose own position
// is their label's start).
func nodeOffset(node ast.Node) (int, bool) {
	if r, ok := node.(*ast.RawHTML); ok {
		if r.Segments != nil && r.Segments.Len() > 0 {
			return r.Segments.At(0).Start, true
		}
	}
	if b, ok := node.(lineBlock); ok {
		if segs := b.Lines(); segs != nil && segs.Len() > 0 {
			seg := segs.At(0)
			return seg.Start, true
		}
	}
	best := -1
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if off, ok := nodeOffset(child); ok && (best < 0 || off < best) {
			best = off
		}
	}
	if best >= 0 {
		return best, true
	}
	if t, ok := node.(*ast.Text); ok {
		return t.Segment.Start, true
	}
	return 0, false
}

// constructName returns an author-facing name for node's kind.
func constructName(node ast.Node) string {
	if name, ok := constructNames[node.Kind()]; ok {
		return name
	}
	if s := node.Kind().String(); s != "" {
		return s
	}
	return "this construct"
}

// lineIndex maps a byte offset to a 1-based line and column within one source
// fragment.
type lineIndex struct {
	starts []int
}

// newLineIndex indexes the start offset of every line in src. Line 1 starts at
// offset 0.
func newLineIndex(src []byte) *lineIndex {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' && i+1 < len(src) {
			starts = append(starts, i+1)
		}
	}
	return &lineIndex{starts: starts}
}

// at returns the 1-based line and byte column of offset. A negative offset or
// one past the end returns (0, 0).
func (li *lineIndex) at(offset int) (line, col int) {
	if offset < 0 {
		return 0, 0
	}
	// Greatest line start <= offset.
	i := sort.Search(len(li.starts), func(i int) bool {
		return li.starts[i] > offset
	}) - 1
	if i < 0 {
		return 0, 0
	}
	return i + 1, offset - li.starts[i] + 1
}
