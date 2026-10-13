package mdcheck

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

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

	// templateDeclaresChildSections says whether the template enclosing this
	// body declares child sections. When true, every heading at any depth 1-6
	// is a section marker and none is accepted as a subheading; when false,
	// `##`/`###` subheadings are accepted and depths 4-6 are refused in
	// subheading position (markdown-allowed-subset).
	templateDeclaresChildSections bool

	// listDepth is the nesting level of the list currently being visited: 0
	// for a top-level list, 1 inside its items, and so on.
	listDepth int

	// themeCursor is the byte offset at which the next thematic-break scan
	// resumes. goldmark's *ast.ThematicBreak carries no source segment of its
	// own, so its position is recovered by scanning source lines in document
	// order (visit is depth-first in document order).
	themeCursor int

	// tables is the effective GFM-table limits the enclosing template
	// declares for this body, or nil when the template does not opt in
	// (table-opt-in, table-limits). A non-nil tables turns on the whole-body
	// table pass (checkTables): the body's GFM tables are parsed with
	// ParseTables and every count, limit and cell-attribute violation is one
	// positioned KindTable Issue. A nil tables leaves a body's table to the
	// opt-out refusal in checkDisallowed (markdown-disallowed-errors).
	tables *TableOptions

	// cellAttributes is the enclosing template's declared cell attributes
	// (table-cell-attributes). The table pass consults it — only when tables
	// is non-nil — to resolve each cell's `{...}` group and report an
	// unknown attribute or a value of the wrong type.
	cellAttributes []CellAttributeSpec
}

// walk walks the parsed document's children and returns every issue found, in
// document order. The Document node itself is a wrapper and is never reported.
//
// When the enclosing template opts in to GFM tables, the whole-body table pass
// (checkTables) runs alongside the AST walk and its issues are merged into the
// result in source order.
func walk(ctx *walkContext, doc ast.Node) []Issue {
	var issues []Issue
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		issues = append(issues, visit(ctx, child)...)
	}
	return mergeTableIssues(issues, ctx.checkTables())
}

// checkTables is the whole-body GFM-table pass (gfm-tables, table-opt-in,
// table-syntax, table-limits, table-cell-attributes). It runs only for an
// opted-in body: when ctx.tables is nil the template does not accept tables, so
// the pass reports nothing and a table in the body is left to the opt-out
// refusal in checkDisallowed (markdown-disallowed-errors). It also reports
// nothing outside BodyMode, where there is no body to scan.
//
// When the template opts in, the body's whole source is scanned with
// ParseTables, which yields each parsed table and one Issue per strict-column-
// count or limit breach; each such Issue is tagged with the body's file. Every
// parsed table's cells — header cells and body cells alike — are then decoded
// against the template's declared cell attributes and each attribute violation
// is one further KindTable Issue positioned at the offending cell. Every issue
// is KindTable, positioned file-absolutely, and returned in ParseTables' order
// (the strict-count Issues in source order, then the limit Issues, then the
// cell-attribute Issues appended table by table).
func (ctx *walkContext) checkTables() []Issue {
	if ctx.mode != BodyMode || ctx.tables == nil {
		return nil
	}
	tables, issues := ParseTables(string(ctx.source), ctx.startLine, *ctx.tables)
	for i := range issues {
		issues[i].File = ctx.file
	}
	for _, table := range tables {
		issues = append(issues, ctx.checkTableCells(table)...)
	}
	return issues
}

// checkTableCells decodes every cell of one parsed table against the enclosing
// template's declared cell attributes and returns one KindTable Issue for each
// attribute violation, positioned at the offending cell's 1-based line and
// column (table-cell-attributes). A header cell is decoded exactly as a body
// cell, as decodeCellAttributes does. A cell that decodes cleanly yields no
// Issue.
func (ctx *walkContext) checkTableCells(table Table) []Issue {
	var issues []Issue
	for _, cell := range table.Headers {
		if issue, bad := ctx.checkTableCell(cell); bad {
			issues = append(issues, issue)
		}
	}
	for _, row := range table.Rows {
		for _, cell := range row {
			if issue, bad := ctx.checkTableCell(cell); bad {
				issues = append(issues, issue)
			}
		}
	}
	return issues
}

// checkTableCell decodes one parsed table cell against the template's declared
// cell attributes and, when the cell's `{...}` group is invalid, returns a
// KindTable Issue positioned at the cell. The decoder's error — an unknown
// attribute (carrying its closest-match suggestion) or a value that is not of
// the attribute's declared type — is the issue's message. The reserved `{br}`
// line break is never an attribute and never an error.
func (ctx *walkContext) checkTableCell(cell TableCell) (Issue, bool) {
	if _, err := decodeCellAttributes(cell, ctx.cellAttributes); err != nil {
		return Issue{
			File:     ctx.file,
			Line:     cell.Line,
			Col:      cell.Column,
			Kind:     KindTable,
			Message:  err.Error(),
			Guidance: "use one of the template's declared cell attributes, or remove the {…} group",
		}, true
	}
	return Issue{}, false
}

// mergeTableIssues merges the AST walk's document-ordered issues with the table
// pass's issues into one list in source order. The merge is a stable sort on the
// issue position (line then column), so issues at the same position keep their
// relative order — the AST walk's before the table pass's — and the AST walk's
// own order is returned untouched when the table pass found nothing.
func mergeTableIssues(astIssues, tableIssues []Issue) []Issue {
	if len(tableIssues) == 0 {
		return astIssues
	}
	merged := make([]Issue, 0, len(astIssues)+len(tableIssues))
	merged = append(merged, astIssues...)
	merged = append(merged, tableIssues...)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Line != merged[j].Line {
			return merged[i].Line < merged[j].Line
		}
		return merged[i].Col < merged[j].Col
	})
	return merged
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
	if ctx.mode == InlineMode {
		// Paragraph is the parser's wrapper for an inline field, so the first
		// top-level paragraph is not an authoring block. Every later paragraph
		// is a second block construct and is rejected, along with every other
		// block kind.
		isWrapperParagraph := node.Kind() == ast.KindParagraph && node.PreviousSibling() == nil
		if blockConstructs[node.Kind()] || (node.Kind() == ast.KindParagraph && !isWrapperParagraph) {
			return []Issue{ctx.newIssue(node, KindBlockConstruct,
				fmt.Sprintf("%s is not allowed in an inline text field", constructName(node)),
				"use inline formatting only: bold, italic, inline code or a link")}
		}
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
// A non-empty result is final for the node: visit records the issues and does
// not descend, so a disallowed construct produces exactly one error. Returning
// nil falls through to the default-deny allow-list check in visit, so an
// unrecognised kind still fails closed.
//
// Implemented here:
//
//   - headings: ATX only. The accepting depth depends on the enclosing
//     template (markdown-allowed-subset): when it declares child sections,
//     every depth 1-6 is a section marker and no heading is accepted here;
//     otherwise `##` and `###` are accepted subheadings and `#` and
//     `####`+ are rejected by level. Setext headings are rejected by
//     inspecting the heading's source line, because goldmark's AST records
//     only the level (*ast.Heading.Level) and not which form was used.
//   - lists: at most one nesting level (maxListNesting).
//   - emphasis: only level 1 (italic) and level 2 (bold).
//   - images, block quotes, fenced and indented code, thematic breaks, block
//     and inline raw HTML, angle-bracket autolinks, reference-style links and
//     link reference definitions.
//   - tables: the CommonMark core has no table syntax, so a GFM table parses as
//     an ordinary paragraph; it is caught by looking for a delimiter row among
//     the paragraph's own source lines (hasTableDelimiterRow), with the same
//     recognition the whole-body table pass uses, rather than by node kind. This
//     is the opt-out refusal (markdown-disallowed-errors): when the enclosing
//     template opts in (ctx.tables is non-nil) the table is accepted here and
//     validated by the whole-body table pass (checkTables), so the construct
//     yields exactly one error on either path. The refusal is positioned at the
//     delimiter row — the line that makes the paragraph a table — so the gate
//     and the table pass point at the same place.
//   - the extension-syntax heuristics (~~strike~~, ==highlight==, ^sup^, ~sub~,
//     :emoji:) that the core parser leaves as literal text. They scan ast.Text
//     segments only — never inline code content, never link destinations — and
//     backslash-escaped delimiters are masked out before matching.
func checkDisallowed(ctx *walkContext, node ast.Node) []Issue {
	switch n := node.(type) {
	case *ast.Heading:
		if isSetextHeading(ctx.source, n) {
			return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
				"setext headings are not allowed",
				"use ATX headings instead: ## or ### at the start of the line")}
		}
		if ctx.templateDeclaresChildSections {
			// Every heading at any depth is a section marker in this
			// template, consumed by the parser as a nested section instance,
			// never a Markdown subheading.
			return []Issue{ctx.newIssue(node, KindUnsupportedConstruct,
				fmt.Sprintf("heading level %d is not allowed", n.Level),
				"headings at any depth are section markers inside a template that declares child sections; move the text into the section's own body or a field")}
		}
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
	case *ast.Image:
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"images are not allowed in Markdown",
			"use the slide's image field instead of a Markdown image")}
	case *ast.Paragraph:
		if off, ok := hasTableDelimiterRow(ctx.source, n); ok {
			// The template opts in (ctx.tables != nil): the table pass
			// (checkTables) owns this table's validation, so the paragraph is
			// accepted here and the table is not refused.
			if ctx.tables != nil {
				return nil
			}
			// The template does not accept tables: this is the single opt-out
			// refusal (markdown-disallowed-errors), positioned at the delimiter
			// row — the line that makes the paragraph a table — so the gate and
			// the table pass agree on where the table is.
			return []Issue{ctx.newIssueAt(off, KindDisallowedConstruct,
				"tables are not allowed",
				"opt in to tables with the `tables` option in the template's body rule, or use a list instead")}
		}
	case *ast.Blockquote:
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"block quotes are not allowed",
			"remove the > quote markers and keep the text in the body")}
	case *ast.FencedCodeBlock:
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"fenced code blocks are not allowed",
			"remove the ``` code fence, or use inline code with backticks")}
	case *ast.CodeBlock:
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"indented code blocks are not allowed",
			"remove the leading indentation, or use inline code with backticks")}
	case *ast.ThematicBreak:
		if off, ok := ctx.thematicBreakOffset(node); ok {
			return []Issue{ctx.newIssueAt(off, KindDisallowedConstruct,
				"thematic breaks (---) are not allowed",
				"remove the --- line")}
		}
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"thematic breaks (---) are not allowed",
			"remove the --- line")}
	case *ast.HTMLBlock:
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"raw HTML is not allowed",
			"write Markdown instead; HTML is not rendered")}
	case *ast.RawHTML:
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"inline raw HTML is not allowed",
			"remove the HTML tag and use Markdown formatting")}
	case *ast.AutoLink:
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"angle-bracket autolinks are not allowed",
			"use an inline link: [text](https://example.com)")}
	case *ast.Link:
		if n.Reference != nil {
			return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
				"reference-style links are not allowed",
				"use an inline link: [text](url)")}
		}
	case *ast.LinkReferenceDefinition:
		return []Issue{ctx.newIssue(node, KindDisallowedConstruct,
			"link reference definitions are not allowed",
			"use an inline link: [text](url)")}
	case *ast.Text:
		return ctx.checkTextHeuristics(n)
	}
	return nil
}

// Extension-syntax heuristics. The CommonMark core parser has no extensions
// registered, so these constructs are left as literal ast.Text and must be
// rejected textually. The patterns are deliberately anchored on non-word
// neighbours to avoid flagging ordinary prose such as "a == b", "~5 to ~10",
// "10:30:45" or "x ^ y".
var (
	reStrike    = regexp.MustCompile(`~~\S(.*?\S)?~~`)
	reHighlight = regexp.MustCompile(`==\S(.*?\S)?==`)
	reSup       = regexp.MustCompile(`\^\S+?\^`)
	reSub       = regexp.MustCompile(`(^|[^~\w])~[^\s~]+~([^~\w]|$)`)
	reEmoji     = regexp.MustCompile(`(^|\s):[a-z0-9_+-]+:(\s|$)`)
)

// checkTextHeuristics scans one ast.Text run for extension syntax. Text inside
// inline code is skipped (a CodeSpan's content is exposed as child Text nodes),
// as are link destinations, which are node attributes rather than Text.
func (ctx *walkContext) checkTextHeuristics(node *ast.Text) []Issue {
	if insideCodeSpan(node) {
		return nil
	}
	text := maskBackslashEscapes(string(node.Segment.Value(ctx.source)))

	var issues []Issue
	if reStrike.MatchString(text) {
		issues = append(issues, ctx.newIssue(node, KindDisallowedConstruct,
			"strikethrough (~~text~~) is not supported",
			"remove the ~~ markers or use plain text"))
	}
	if reHighlight.MatchString(text) {
		issues = append(issues, ctx.newIssue(node, KindDisallowedConstruct,
			"highlight (==text==) is not supported",
			"remove the == markers"))
	}
	if reSup.MatchString(text) {
		issues = append(issues, ctx.newIssue(node, KindDisallowedConstruct,
			"superscript (^text^) is not supported",
			"remove the ^ markers or write the text normally"))
	}
	if reSub.MatchString(text) {
		issues = append(issues, ctx.newIssue(node, KindDisallowedConstruct,
			"subscript (~text~) is not supported",
			"remove the ~ markers or write the text normally"))
	}
	if reEmoji.MatchString(text) {
		issues = append(issues, ctx.newIssue(node, KindDisallowedConstruct,
			"emoji shortcodes (:name:) are not supported",
			"remove the :name: shortcode or paste the emoji character instead"))
	}
	return issues
}

// insideCodeSpan reports whether node is content of an inline code span.
func insideCodeSpan(node ast.Node) bool {
	for p := node.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.CodeSpan); ok {
			return true
		}
	}
	return false
}

// maskBackslashEscapes blanks out every CommonMark backslash escape (a
// backslash followed by ASCII punctuation) so an escaped delimiter such as
// `\~`, `\=`, `\^`, `\:` or `\[` is treated as literal text and cannot be
// matched by the extension heuristics. Escapes keep their byte length, so the
// masked string lines up with the source.
func maskBackslashEscapes(s string) string {
	escaped := false
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '\\' && isASCIIPunctuation(s[i+1]) {
			escaped = true
			break
		}
	}
	if !escaped {
		return s
	}
	b := []byte(s)
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '\\' && isASCIIPunctuation(b[i+1]) {
			b[i], b[i+1] = ' ', ' '
			i++
		}
	}
	return string(b)
}

// isASCIIPunctuation reports whether c is an ASCII punctuation byte, the set
// of characters a backslash may escape in CommonMark.
func isASCIIPunctuation(c byte) bool {
	return (c >= '!' && c <= '/') ||
		(c >= ':' && c <= '@') ||
		(c >= '[' && c <= '`') ||
		(c >= '{' && c <= '~')
}

// isSetextHeading reports whether the heading used a setext underline rather
// than ATX `#` markers. goldmark does not record the form, so the source line
// that starts the heading's first line is inspected: an ATX heading is preceded
// only by spaces/tabs followed by one or more `#` markers.
func isSetextHeading(src []byte, h *ast.Heading) bool {
	segs := h.Lines()
	if segs == nil || segs.Len() == 0 {
		return false
	}
	start := segs.At(0).Start
	if start > len(src) {
		return false
	}
	i := start - 1
	for i >= 0 && (src[i] == ' ' || src[i] == '\t') {
		i--
	}
	return i < 0 || src[i] != '#'
}

// hasTableDelimiterRow reports whether a paragraph contains a GFM table
// delimiter row (for example `| --- | :--: |`) and, when it does, the byte
// offset in src at which that row begins. CommonMark core has no tables, so a
// GFM table parses as an ordinary paragraph and this is the only signal that
// the author wrote one.
//
// The per-line test is isTableDelimiterRow, the parser's own delimiter-row
// recognition (table.go), which splits a row with splitTableRow — so a
// backslash-escaped `\|` is a literal pipe rather than a separator, exactly as
// parseTable reads a table's rows. Both the opt-in gate in checkDisallowed and
// the whole-body table pass (checkTables) reach their verdict through it, so
// detection and validation agree on what a table is. The offset is returned so
// a caller can position a table-level issue at the delimiter row.
func hasTableDelimiterRow(src []byte, b lineBlock) (offset int, ok bool) {
	segs := b.Lines()
	if segs == nil {
		return 0, false
	}
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		if seg.Start < 0 || seg.Stop > len(src) || seg.Start > seg.Stop {
			continue
		}
		if isTableDelimiterRow(string(src[seg.Start:seg.Stop])) {
			return seg.Start, true
		}
	}
	return 0, false
}

// isTableDelimiterRow reports whether a single source line is a GFM table
// delimiter row. It is the parser's own delimiter-row recognition (table.go):
// the line's cells are split with splitTableRow, the same splitter parseTable
// reads a table's rows with, so a backslash-escaped `\|` is a literal pipe and
// never a separator and the opt-in gate and the table pass agree on what a
// delimiter row is. A delimiter row has at least one pipe and at least one
// non-empty cell, and every non-empty cell consists only of `-` and `:` with at
// least one `-` — the `:?-+:?` shape, its leading and trailing colon optional.
func isTableDelimiterRow(line string) bool {
	if s := strings.TrimSpace(line); !strings.Contains(s, "|") || !strings.Contains(s, "-") {
		return false
	}
	sawCell := false
	for _, cell := range splitTableRow(line) {
		text := strings.TrimSpace(cell.Text)
		if text == "" {
			continue
		}
		if !strings.Contains(text, "-") {
			return false
		}
		sawCell = true
		for i := 0; i < len(text); i++ {
			if text[i] != '-' && text[i] != ':' {
				return false
			}
		}
	}
	return sawCell
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

// newIssueAt builds an Issue positioned at a known source byte offset. It is
// used for nodes that carry no source segment of their own (thematic breaks).
func (ctx *walkContext) newIssueAt(offset int, kind Kind, message, guidance string) Issue {
	line, col := ctx.positionAt(offset)
	return Issue{
		File:     ctx.file,
		Line:     line,
		Col:      col,
		Kind:     kind,
		Message:  message,
		Guidance: guidance,
	}
}

// thematicBreakOffset locates the source line of a *ast.ThematicBreak node.
// goldmark gives thematic breaks no source segment, so the scan resumes at the
// end of the node's preceding sibling (or the running cursor, whichever is
// later) and returns the first thematic-break line at or after that point.
func (ctx *walkContext) thematicBreakOffset(node ast.Node) (int, bool) {
	start := ctx.themeCursor
	if prev := node.PreviousSibling(); prev != nil {
		if end := subtreeEnd(prev); end > start {
			start = end
		}
	}
	off, ok := findThematicBreakLine(ctx.source, start)
	if ok {
		// Advance past the found line so a run of consecutive thematic breaks
		// is reported line by line rather than re-finding the first one.
		ctx.themeCursor = lineStartAfter(ctx.source, off)
	}
	return off, ok
}

// lineStartAfter returns the offset of the line following the line that begins
// at offset, or len(src) when that was the last line.
func lineStartAfter(src []byte, offset int) int {
	for i := offset; i < len(src); i++ {
		if src[i] == '\n' {
			return i + 1
		}
	}
	return len(src)
}

// findThematicBreakLine returns the offset of the first CommonMark thematic
// break line at or after start, or false when there is none.
func findThematicBreakLine(src []byte, start int) (int, bool) {
	if start < 0 {
		start = 0
	}
	for i := start; i < len(src); {
		lineEnd := i
		for lineEnd < len(src) && src[lineEnd] != '\n' {
			lineEnd++
		}
		line := src[i:lineEnd]
		if marker, ok := thematicBreakMarker(line); ok {
			return i + marker, true
		}
		if lineEnd >= len(src) {
			break
		}
		i = lineEnd + 1
	}
	return 0, false
}

// thematicBreakMarker reports whether line is a CommonMark thematic break and,
// if so, the byte offset within line of its first marker character: three or
// more of the same character from `*`, `-` or `_`, optionally separated by
// spaces or tabs and indented by at most three spaces.
func thematicBreakMarker(line []byte) (int, bool) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	first := -1
	var marker byte
	count := 0
	for ; i < len(line); i++ {
		c := line[i]
		switch c {
		case ' ', '\t', '\r':
			continue
		case '*', '-', '_':
			if marker == 0 {
				marker = c
			} else if c != marker {
				return 0, false
			}
			if first < 0 {
				first = i
			}
			count++
		default:
			return 0, false
		}
	}
	if marker == 0 || count < 3 {
		return 0, false
	}
	return first, true
}

// subtreeEnd returns the greatest source byte offset contained in node's
// subtree, or 0 when the subtree has no source extent.
func subtreeEnd(node ast.Node) int {
	end := 0
	if isBlockKind(node.Kind()) {
		if b, ok := node.(lineBlock); ok {
			if segs := b.Lines(); segs != nil && segs.Len() > 0 {
				end = segs.At(segs.Len() - 1).Stop
			}
		}
	}
	if r, ok := node.(*ast.RawHTML); ok && r.Segments != nil && r.Segments.Len() > 0 {
		if e := r.Segments.At(r.Segments.Len() - 1).Stop; e > end {
			end = e
		}
	}
	if t, ok := node.(*ast.Text); ok && t.Segment.Stop > end {
		end = t.Segment.Stop
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if e := subtreeEnd(child); e > end {
			end = e
		}
	}
	return end
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
	return ctx.positionAt(offset)
}

// positionAt returns the 1-based file line and byte column of a source byte
// offset, or (0, 0) when the offset cannot be mapped.
func (ctx *walkContext) positionAt(offset int) (line, col int) {
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
	// Only block nodes may consult Lines(): ast.BaseInline.Lines() panics
	// ("can not call with inline nodes"), and inline nodes embed it.
	if isBlockKind(node.Kind()) {
		if b, ok := node.(lineBlock); ok {
			if segs := b.Lines(); segs != nil && segs.Len() > 0 {
				seg := segs.At(0)
				return seg.Start, true
			}
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

// isBlockKind reports whether k is a goldmark block node kind, for which the
// Lines() source accessor is meaningful.
func isBlockKind(k ast.NodeKind) bool {
	switch k {
	case ast.KindDocument, ast.KindParagraph, ast.KindTextBlock, ast.KindHeading,
		ast.KindBlockquote, ast.KindList, ast.KindListItem, ast.KindCodeBlock,
		ast.KindFencedCodeBlock, ast.KindHTMLBlock, ast.KindThematicBreak,
		ast.KindLinkReferenceDefinition:
		return true
	}
	return false
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
