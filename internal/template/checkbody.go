package template

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/really-knows-ai/kalide/internal/mdcheck"
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

	// withoutTableCheck suppresses CheckBody's table pass. A caller that owns
	// its body's table errors elsewhere sets it so CheckBody does not also
	// emit them (each table error is emitted exactly once per construct).
	withoutTableCheck bool
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

// WithoutTableCheck suppresses CheckBody's table pass, so CheckBody reports no
// table error for the body. A caller that owns its body's table errors
// elsewhere passes it, keeping every table error emitted exactly once per
// construct: the deck path (validate.checkBodyRule, which owns slide, section
// and notes bodies through validate.checkTables) and the example path
// (validateExampleBlockAt, whose checkExampleBodySubheadings owns the example
// block body's table errors) both pass it. CheckBody's table pass then runs
// only for the callers that rely on it — a section-helper call's literal body
// (ResolveSectionCallLayered) and a child constructor's literal body
// (ResolveChildCall) — which pass no option.
func WithoutTableCheck() BodyOption {
	return func(c *bodyConfig) { c.withoutTableCheck = true }
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
//     "subheadings";
//  7. A table violation — the body's GFM tables checked against the rule's
//     Tables option: a non-opted-in body's table is refused (the mdcheck
//     opt-out refusal) and an opted-in body's table is checked for its strict
//     column counts, declared limits and cell attributes, reporting the issue's
//     Rule (its mdcheck kind).
//
// The rule's Tables option is enforced by the table pass, which runs last so a
// body that also breaks one of the fixed-order rules reports that rule first.
// The pass is skipped when the caller passes WithoutTableCheck, so a caller
// that owns its body's table errors elsewhere does not see them twice (each
// table error is emitted exactly once per construct).
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

	if !cfg.withoutTableCheck {
		if ve, invalid := checkBodyTables(rule, body, path, cfg.line); invalid {
			return ve, true
		}
	}

	return ValueError{}, false
}

// tableOptOutMessage is the author-facing message mdcheck's opt-out refusal
// carries (checkDisallowed) for a table in a body whose template has not opted
// in through `body.tables`. CheckBody recognises the refusal by it because the
// refusal is a KindDisallowedConstruct issue, not a KindTable issue, so it
// cannot be told from another disallowed construct by kind alone. It is the
// single opt-out refusal site (mdcheck.checkDisallowed); CheckBody adds none.
const tableOptOutMessage = "tables are not allowed"

// isTableIssue reports whether iss is a table-kind issue CheckBody's table pass
// surfaces: a KindTable issue from mdcheck's table pass (a strict column count,
// a breached limit or an unknown/ill-typed cell attribute), or the opt-out
// refusal mdcheck emits for a non-opted-in body's table. Every other mdcheck
// issue is not a table issue and is dropped by the pass.
func isTableIssue(iss mdcheck.Issue) bool {
	if iss.Kind == mdcheck.KindTable {
		return true
	}
	return iss.Kind == mdcheck.KindDisallowedConstruct && iss.Message == tableOptOutMessage
}

// checkBodyTables runs mdcheck's GFM-table pass for a body against the
// template's `body.tables` option and returns the first table violation as a
// positioned ValueError (table-opt-in, table-errors). It is CheckBody's table
// pass; CheckBody calls it after the fixed-order checks, and skips it entirely
// when WithoutTableCheck is set.
//
// The rule's TableRule is converted to mdcheck's own plain options
// (mdcheckTableOptions / mdcheckCellAttributeSpecs) and mdcheck.Check is run in
// BodyMode. Only table-kind issues are surfaced (isTableIssue): a non-opted-in
// body's table is the single opt-out refusal mdcheck emits through
// checkDisallowed, and an opted-in body's table is checked for its strict
// column counts, declared limits and cell attributes. Every other mdcheck issue
// is dropped: the body rules CheckBody already checked (mode, size,
// subheadings) are its own, and the remaining Markdown-subset issues belong to
// internal/validate's ordinary body check, not here.
//
// The violation carries the implied `body` path and the issue's mdcheck kind as
// its Rule, its message as What and its guidance as Fix, positioned at the
// issue's line into the table. When the caller passed no position source the
// line is 0, matching CheckBody's other violations.
func checkBodyTables(rule BodyRule, body string, path []PathSegment, startLine int) (ValueError, bool) {
	opts := mdcheck.Options{
		Mode:      mdcheck.BodyMode,
		StartLine: startLine,
	}
	if rule.Tables != nil {
		tables := mdcheckTableOptions(rule.Tables)
		opts.Tables = &tables
		opts.CellAttributes = mdcheckCellAttributeSpecs(rule.Tables.CellAttributes)
	}

	for _, iss := range mdcheck.Check("", []byte(body), opts) {
		if !isTableIssue(iss) {
			continue
		}
		line := iss.Line
		if startLine < 1 {
			// CheckBody carries line 0 when the caller passed no position
			// source, matching its other violations.
			line = 0
		}
		return ValueError{
			Path:  path,
			Line:  line,
			Rule:  string(iss.Kind),
			Value: body,
			What:  iss.Message,
			Fix:   iss.Guidance,
		}, true
	}
	return ValueError{}, false
}

// mdcheckTableOptions converts a template's decoded TableRule into mdcheck's
// own plain TableOptions, the effective limits mdcheck's table parser and table
// pass consume (mdcheck.Options.Tables). A nil rule — a template that does not
// opt in to tables — yields the zero TableOptions; the caller must not pass it
// as a non-nil Tables, which is what opts a body in (table-opt-in). Every limit
// maps one to one.
func mdcheckTableOptions(rule *TableRule) mdcheck.TableOptions {
	if rule == nil {
		return mdcheck.TableOptions{}
	}
	return mdcheck.TableOptions{
		MinTables:  rule.MinTables,
		MaxTables:  rule.MaxTables,
		MinColumns: rule.MinColumns,
		MaxColumns: rule.MaxColumns,
		MinRows:    rule.MinRows,
		MaxRows:    rule.MaxRows,
	}
}

// CellAttributeSpecs converts a template's decoded CellAttribute declarations
// into mdcheck's own plain CellAttributeSpec descriptors, the closed set
// mdcheck's cell-extension decoder resolves a cell's `{...}` group against
// (mdcheck.Options.CellAttributes). It is the single shared converter every
// caller uses — the implied-body check, the validator and the renderer — so the
// conversion lives in exactly one place rather than being duplicated.
//
// The descriptor's Kind is mapped explicitly to mdcheck's plain boolean/enum
// constants (FieldBoolean and FieldEnum); the variants are copied with
// append([]string(nil), a.Variants...), so the returned slice never aliases the
// template's own (read-only) declaration slice. A template that declares none
// yields nil.
func CellAttributeSpecs(attrs []CellAttribute) []mdcheck.CellAttributeSpec {
	if len(attrs) == 0 {
		return nil
	}
	specs := make([]mdcheck.CellAttributeSpec, len(attrs))
	for i, a := range attrs {
		kind := string(a.Kind)
		switch a.Kind {
		case FieldBoolean:
			kind = mdcheck.CellAttributeBoolean
		case FieldEnum:
			kind = mdcheck.CellAttributeEnum
		}
		specs[i] = mdcheck.CellAttributeSpec{
			Name:     a.Name,
			Kind:     kind,
			Variants: append([]string(nil), a.Variants...),
		}
	}
	return specs
}

// mdcheckCellAttributeSpecs converts a template's decoded CellAttribute
// declarations into mdcheck's own plain CellAttributeSpec descriptors, the
// closed set mdcheck's cell-extension decoder resolves a cell's `{...}` group
// against (mdcheck.Options.CellAttributes). It is a thin wrapper over the
// shared CellAttributeSpecs, keeping the historical helper name for its
// existing callers while the conversion body lives in exactly one place.
func mdcheckCellAttributeSpecs(attrs []CellAttribute) []mdcheck.CellAttributeSpec {
	return CellAttributeSpecs(attrs)
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
