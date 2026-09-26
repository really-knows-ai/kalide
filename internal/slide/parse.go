package slide

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/suggest"
)

// NotesSection is the reserved name of a slide's speaker-notes section. It is
// not a declarable section name (speaker-notes).
const NotesSection = "notes"

// SlideDelimiter is the fence that opens and closes a slide file's YAML
// frontmatter. Slide frontmatter uses --- … --- at the very top of the file;
// the plain ``` fence is reserved for section frontmatter
// (slide-template-required, section-frontmatter).
const SlideDelimiter = "---"

// SectionFence is the fence that opens and closes a section's YAML
// frontmatter. It is a plain fence with no language tag.
const SectionFence = "```"

// headingRe matches a top-level ATX heading: up to three leading spaces, a
// single `#`, whitespace and the section name. `##` and deeper are not sections
// (Markdown has only one level of sections); `#name` without a space is not a
// heading. The name may be empty.
var headingRe = regexp.MustCompile(`^[ \t]{0,3}#(?:[ \t]+(.*?))?[ \t]*$`)

// Slide is one parsed slide file. Every element carries the 1-based line where
// it begins, so callers can report positioned errors and render from source.
type Slide struct {
	// File is the slide's path or identifier, as passed to Parse. It is the
	// prefix of every error position.
	File string

	// Template is the slide-usage template named by the slide frontmatter.
	Template string

	// TemplateLine is the 1-based line of the frontmatter `template:` key.
	TemplateLine int

	// Frontmatter is the decoded slide frontmatter (including `template`).
	Frontmatter map[string]any

	// Body is the content between the slide frontmatter and the first section
	// heading, with line endings normalised to "\n". It does not include the
	// frontmatter or any section.
	Body string

	// BodyLine is the 1-based line where Body starts.
	BodyLine int

	// Sections are the slide's top-level sections in source order. Notes are
	// not included here; they are in Notes.
	Sections []Section

	// Notes is the slide's `# notes` section, or nil when the slide has none.
	Notes *Notes
}

// Section is one top-level `# name` section of a slide. A section's frontmatter
// is the optional plain ``` fence immediately after its heading
// (section-frontmatter).
type Section struct {
	// Name is the section name: the heading text after `# `.
	Name string

	// HeadingLine is the 1-based line of the `# name` heading.
	HeadingLine int

	// Template is the section template the instance resolves to: an explicit
	// `template:` when given, otherwise the one accepted template when the
	// section accepts exactly one. It is "" when the section declares no
	// template or when the template could not be resolved.
	Template string

	// TemplateLine is the 1-based line of the section frontmatter's
	// `template:` key, or 0 when the section has no explicit template key.
	TemplateLine int

	// Frontmatter is the decoded section frontmatter, or nil when there is
	// none.
	Frontmatter map[string]any

	// FenceLine is the 1-based line of the section's opening frontmatter
	// fence, or 0 when the section has no frontmatter.
	FenceLine int

	// Body is the section content after its heading (and frontmatter, when
	// present), up to the next section heading or end of file.
	Body string

	// BodyLine is the 1-based line where Body starts.
	BodyLine int
}

// Notes is a slide's reserved `# notes` speaker-notes section
// (speaker-notes). At most one is allowed and it must be the last section; it
// does not count toward any section's repeat limits.
type Notes struct {
	// HeadingLine is the 1-based line of the `# notes` heading.
	HeadingLine int

	// Body is the notes content after the heading, up to the end of file.
	Body string

	// BodyLine is the 1-based line where Body starts.
	BodyLine int
}

// ParseError is a positioned parse error: the file, the 1-based line when
// known (0 otherwise), and the message. Error renders as `file:line: message`,
// matching the deck package's convention.
type ParseError struct {
	// File is the file the error belongs to.
	File string

	// Line is the 1-based line, or 0 when there is no meaningful line.
	Line int

	// Msg is the human-readable message.
	Msg string
}

// Error renders the positioned message.
func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Msg)
}

// catalogueBlock is a decoded YAML frontmatter block: its mapping node (for
// key lines) and its decoded values.
type catalogueBlock struct {
	mapping *yaml.Node
	values  map[string]any
}

// Parse parses one slide file from src, resolving template and section names
// against cat. file is the identity used in errors (typically the slide's
// path).
//
// Rules (deck-structure):
//   - the file opens with YAML frontmatter delimited by --- … --- at line 1;
//     missing, unterminated or not-at-top frontmatter is an error, and the
//     frontmatter must name a slide-usage `template:`;
//   - the content between the frontmatter and the first `#` heading is the
//     slide's body;
//   - a top-level `# name` heading starts a section (one level only); its name
//     must be declared by the slide's template, with a closest-match
//     suggestion otherwise, and its repeat count must lie within the declared
//     min/max;
//   - a plain ``` fence immediately after a section heading is that section's
//     frontmatter; a language tag on it, or a fence anywhere else, is an
//     error; `template:` is required on the section instance iff the section
//     accepts more than one template;
//   - `# notes` is the reserved speaker-notes section: at most one, last, and
//     excluded from repeat limits; `notes` cannot be a declared section name.
//
// Nested section-in-section checks are not the parser's job: Markdown has one
// section level, and composed/nested templates are checked later (phase 4
// schema, phase 5 validator).
func Parse(file string, src []byte, cat Catalogue) (*Slide, error) {
	lines := splitLines(src)

	// Slide frontmatter must be the first thing in the file.
	if len(lines) == 0 || !isDelimiter(lines[0]) {
		for i, line := range lines {
			if isDelimiter(line) {
				return nil, parseError(file, i+1,
					"slide frontmatter must start on line 1")
			}
		}
		return nil, parseError(file, 1, "missing slide frontmatter")
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if isDelimiter(lines[i]) {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return nil, parseError(file, 1,
			"unterminated slide frontmatter: missing closing %q", SlideDelimiter)
	}

	fm, err := parseBlock(file, lines[1:closeIdx], 2)
	if err != nil {
		return nil, err
	}
	tmpl, tmplLine, hasTmpl, err := templateInBlock(file, fm)
	if err != nil {
		return nil, err
	}
	if !hasTmpl || strings.TrimSpace(tmpl) == "" {
		return nil, parseError(file, 1, "missing required key %q in slide frontmatter", "template")
	}
	usage, ok := cat.LookupSlideTemplate(tmpl)
	if !ok {
		return nil, unknownName(file, tmplLine, "template", tmpl, cat.TemplateNames())
	}
	if usage != "slide" {
		return nil, parseError(file, tmplLine,
			"template %q is a %s template, not a slide template", tmpl, usage)
	}

	// The section declarations of the slide template, resolved once. The names
	// are copied and sorted so min-repeat reporting and suggestions are
	// deterministic and the catalogue's slice is never mutated.
	names := append([]string(nil), cat.SectionNames(tmpl)...)
	sort.Strings(names)
	declared := make(map[string]bool, len(names))
	decls := make(map[string]sectionDecl, len(names))
	for _, name := range names {
		declared[name] = true
		accepted, minRep, maxRep, ok := cat.SectionDecl(tmpl, name)
		decls[name] = sectionDecl{accepted: accepted, min: minRep, max: maxRep, ok: ok}
	}

	// Body before the first section heading.
	firstHeading := -1
	for i := closeIdx + 1; i < len(lines); i++ {
		if isSectionHeading(lines[i]) {
			firstHeading = i
			break
		}
	}
	bodyStart := closeIdx + 1
	bodyEnd := len(lines)
	if firstHeading != -1 {
		bodyEnd = firstHeading
	}
	if err := checkNoFence(file, lines, bodyStart, bodyEnd); err != nil {
		return nil, err
	}

	slide := &Slide{
		File:         file,
		Template:     tmpl,
		TemplateLine: tmplLine,
		Frontmatter:  fm.values,
		Body:         strings.Join(lines[bodyStart:bodyEnd], "\n"),
		BodyLine:     bodyStart + 1,
	}

	counts := make(map[string]int, len(names))
	notesSeen := false

	for i := firstHeading; i >= 0 && i < len(lines); {
		name := headingName(lines[i])
		headingLine := i + 1

		if name == "" {
			return nil, parseError(file, headingLine, "section heading needs a name")
		}

		// Any heading after a notes section is an error: a second notes is a
		// duplicate, anything else means notes was not last.
		if notesSeen {
			if name == NotesSection {
				return nil, parseError(file, headingLine, "duplicate %q section", NotesSection)
			}
			return nil, parseError(file, headingLine,
				"%q section must be the last section on the slide", NotesSection)
		}

		if name == NotesSection {
			// A notes section may only be followed by end of file. Any later
			// heading is caught at the top of the next iteration.
			if declared[NotesSection] {
				return nil, parseError(file, headingLine,
					"%q is reserved and cannot be declared as a section name", NotesSection)
			}
			notesStart := i + 1
			next := nextHeading(lines, notesStart)
			notesEnd := len(lines)
			if next != -1 {
				notesSeen = true
				notesEnd = next
			}
			if err := checkNoFence(file, lines, notesStart, notesEnd); err != nil {
				return nil, err
			}
			slide.Notes = &Notes{
				HeadingLine: headingLine,
				BodyLine:    notesStart + 1,
				Body:        strings.Join(lines[notesStart:notesEnd], "\n"),
			}
			i = next
			continue
		}

		if !declared[name] {
			return nil, unknownName(file, headingLine, "section", name, names)
		}
		counts[name]++
		// max <= 0 means unbounded (the Go zero value is the natural "no
		// limit" sentinel); a positive max is a hard limit.
		if d, ok := decls[name]; ok && d.ok && d.max > 0 && counts[name] > d.max {
			return nil, parseError(file, headingLine,
				"section %q: at most %d allowed, found %d", name, d.max, counts[name])
		}

		section := Section{Name: name, HeadingLine: headingLine}
		contentStart := i + 1

		// A plain ``` fence immediately after the heading is the section's
		// frontmatter (section-frontmatter). Its close consumes the block, so
		// YAML comment lines inside it are never mistaken for headings.
		if contentStart < len(lines) {
			if isFence, lang := matchFence(lines[contentStart]); isFence {
				if lang != "" {
					return nil, parseError(file, contentStart+1,
						"section frontmatter fence must not have a language tag (%q)", lang)
				}
				close := -1
				for j := contentStart + 1; j < len(lines); j++ {
					if f, l := matchFence(lines[j]); f {
						if l != "" {
							return nil, parseError(file, j+1,
								"section frontmatter fence must not have a language tag (%q)", l)
						}
						close = j
						break
					}
				}
				if close == -1 {
					return nil, parseError(file, contentStart+1,
						"unterminated section frontmatter: missing closing %q", SectionFence)
				}
				blk, err := parseBlock(file, lines[contentStart+1:close], contentStart+2)
				if err != nil {
					return nil, err
				}
				section.FenceLine = contentStart + 1
				section.Frontmatter = blk.values
				if err := resolveSectionTemplate(file, cat, name,
					decls[name], blk, section.FenceLine, &section); err != nil {
					return nil, err
				}
				contentStart = close + 1
			}
		}

		// The section body runs to the next heading; a fence found in it is
		// misplaced and is reported at the fence line before any
		// template-resolution complaint.
		next := nextHeading(lines, contentStart)
		bodyEnd := len(lines)
		if next != -1 {
			bodyEnd = next
		}
		if err := checkNoFence(file, lines, contentStart, bodyEnd); err != nil {
			return nil, err
		}

		// A template: is required on a section instance iff len(accepted) > 1.
		// Exactly one accepted template resolves automatically. When the
		// section has no fence at all, the requirement is reported at the
		// heading, since there is no fence line.
		if section.FenceLine == 0 {
			if d, ok := decls[name]; ok && d.ok {
				switch {
				case len(d.accepted) > 1:
					return nil, parseError(file, headingLine,
						"section %q: a template: is required when the section accepts more than one template", name)
				case len(d.accepted) == 1:
					section.Template = d.accepted[0]
				}
			}
		}
		if section.Template == NotesSection {
			return nil, parseError(file, positionOr(section.TemplateLine, headingLine),
				"%q is reserved and cannot be used as a section template", NotesSection)
		}

		section.BodyLine = contentStart + 1
		section.Body = strings.Join(lines[contentStart:bodyEnd], "\n")
		slide.Sections = append(slide.Sections, section)

		i = next
	}

	// min repeats are checked last: a missing section has no offending line,
	// so the slide's template line is the most useful position. Names are
	// iterated in sorted order for determinism.
	for _, name := range names {
		if name == NotesSection {
			continue
		}
		d := decls[name]
		if !d.ok || d.min <= 0 {
			continue
		}
		if counts[name] < d.min {
			return nil, parseError(file, positionOr(tmplLine, 1),
				"section %q: requires at least %d, found %d", name, d.min, counts[name])
		}
	}

	return slide, nil
}

// sectionDecl mirrors a Catalogue SectionDecl result.
type sectionDecl struct {
	accepted []string
	min      int
	max      int
	ok       bool
}

// resolveSectionTemplate resolves a declared section's template from its
// frontmatter block and records it in out. fenceLine is the line of the
// opening ``` fence, where a required-but-missing template: is reported.
func resolveSectionTemplate(file string, cat Catalogue, section string,
	d sectionDecl, blk *catalogueBlock, fenceLine int, out *Section) error {

	tmplName, tmplLine, hasTmpl, err := templateInBlock(file, blk)
	if err != nil {
		return err
	}
	if len(d.accepted) == 0 {
		if hasTmpl {
			return parseError(file, positionOr(tmplLine, fenceLine),
				"section %q does not accept a template:", section)
		}
		return nil
	}
	if !hasTmpl {
		if len(d.accepted) > 1 {
			return parseError(file, fenceLine,
				"section %q: a template: is required when the section accepts more than one template", section)
		}
		out.Template = d.accepted[0]
		return nil
	}

	line := positionOr(tmplLine, fenceLine)
	if strings.TrimSpace(tmplName) == "" {
		return parseError(file, line,
			"section %q: key %q must name a template", section, "template")
	}
	if tmplName == NotesSection {
		return parseError(file, line,
			"%q is reserved and cannot be used as a section template", NotesSection)
	}
	usage, ok := cat.LookupSlideTemplate(tmplName)
	if !ok {
		return unknownName(file, line, "template", tmplName, cat.TemplateNames())
	}
	if usage != "section" {
		return parseError(file, line,
			"template %q is a %s template, not a section template", tmplName, usage)
	}
	if !contains(d.accepted, tmplName) {
		return unknownName(file, line,
			"template for section "+section, tmplName, d.accepted)
	}
	out.Template = tmplName
	out.TemplateLine = tmplLine
	return nil
}

// unknownName reports an unknown template or section name, adding a
// closest-match suggestion from candidates when one exists.
func unknownName(file string, line int, what, name string, candidates []string) error {
	if s := suggest.Closest(name, candidates); s != "" {
		return parseError(file, line, "unknown %s %q: did you mean %q?", what, name, s)
	}
	return parseError(file, line, "unknown %s %q", what, name)
}

// templateInBlock finds the `template:` key in a frontmatter block. The block's
// YAML node lines are absolute (parseBlock pads the source), so they can be
// reported directly. has is false when the key is absent.
func templateInBlock(file string, b *catalogueBlock) (name string, line int, has bool, err error) {
	if b == nil || b.mapping == nil {
		return "", 0, false, nil
	}
	m := b.mapping
	if m.Kind != yaml.MappingNode {
		return "", 0, false, parseError(file, nodeLine(m),
			"expected a mapping of frontmatter keys, got %s", kindWord(m))
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i], m.Content[i+1]
		if key.Value != "template" {
			continue
		}
		keyLine := nodeLine(key)
		if !isString(val) {
			return "", keyLine, true, parseError(file, keyLine,
				"key %q: expected a string, got %s", "template", kindWord(val))
		}
		return val.Value, keyLine, true, nil
	}
	return "", 0, false, nil
}

// parseBlock decodes a YAML frontmatter block. startLine is the 1-based line
// of the block's first YAML line; the text is padded with newlines so every
// node line and YAML error position is absolute within the file. An empty block
// decodes to an empty block with no mapping.
func parseBlock(file string, lines []string, startLine int) (*catalogueBlock, error) {
	b := &catalogueBlock{values: map[string]any{}}
	if len(lines) == 0 {
		return b, nil
	}
	// Pad so YAML's own 1-based lines map onto file lines.
	pad := ""
	if startLine > 1 {
		pad = strings.Repeat("\n", startLine-1)
	}
	text := pad + strings.Join(lines, "\n")

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, parseError(file, startLine, "invalid YAML: %v", err)
	}
	m := documentMapping(&doc)
	if m == nil {
		return b, nil
	}
	if m.Kind != yaml.MappingNode {
		return nil, parseError(file, nodeLine(m),
			"expected a mapping of frontmatter keys, got %s", kindWord(m))
	}
	b.mapping = m

	var values map[string]any
	if err := yaml.Unmarshal([]byte(text), &values); err == nil && values != nil {
		b.values = values
	}
	return b, nil
}

// checkNoFence rejects any fence in lines[from:to): the plain ``` fence is only
// allowed immediately after a section heading (and as the matching close of
// such a fence), so one found here is misplaced. A language-tagged fence is
// reported as such, at the fence line.
func checkNoFence(file string, lines []string, from, to int) error {
	for i := from; i < to && i < len(lines); i++ {
		if isFence, lang := matchFence(lines[i]); isFence {
			if lang != "" {
				return parseError(file, i+1,
					"section frontmatter fence must not have a language tag (%q)", lang)
			}
			return parseError(file, i+1,
				"frontmatter fence is only allowed immediately after a section heading")
		}
	}
	return nil
}

// nextHeading returns the index of the first top-level heading at or after
// from, or -1 when there is none.
func nextHeading(lines []string, from int) int {
	for i := from; i < len(lines); i++ {
		if isSectionHeading(lines[i]) {
			return i
		}
	}
	return -1
}

// splitLines normalises line endings and splits src into lines. The trailing
// newline of a final line does not create an extra empty line.
func splitLines(src []byte) []string {
	s := strings.ReplaceAll(string(src), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// isDelimiter reports whether line is a slide frontmatter delimiter,
// tolerating surrounding whitespace.
func isDelimiter(line string) bool {
	return strings.TrimSpace(line) == SlideDelimiter
}

// matchFence reports whether line is a ``` fence and returns its language tag
// (empty for a plain fence).
func matchFence(line string) (isFence bool, lang string) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, SectionFence) {
		return false, ""
	}
	return true, strings.TrimSpace(t[len(SectionFence):])
}

// isSectionHeading reports whether line is a top-level `# name` heading.
func isSectionHeading(line string) bool {
	return headingRe.MatchString(line)
}

// headingName returns the section name of a heading line, or "" when the
// heading has no name. A CommonMark closing sequence (`# foo #`) is stripped.
func headingName(line string) string {
	m := headingRe.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(m[1])
	if strings.HasSuffix(name, "#") {
		trimmed := strings.TrimRight(name, "#")
		if trimmed != "" && strings.HasSuffix(trimmed, " ") {
			name = strings.TrimSpace(trimmed)
		}
	}
	return name
}

// documentMapping returns the root mapping of a parsed YAML document, or nil
// when the document is empty.
func documentMapping(doc *yaml.Node) *yaml.Node {
	if doc == nil || doc.Kind == 0 {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	return doc
}

// isString reports whether n is a scalar carrying the string tag.
func isString(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str"
}

// kindWord names a node's type for a human-readable type error.
func kindWord(n *yaml.Node) string {
	if n == nil {
		return "nothing"
	}
	switch n.ShortTag() {
	case "!!str":
		return "a string"
	case "!!int", "!!float":
		return "a number"
	case "!!bool":
		return "a boolean"
	case "!!null":
		return "nothing (null)"
	case "!!timestamp":
		return "a date"
	case "!!seq":
		return "a list"
	case "!!map":
		return "a mapping"
	default:
		return n.ShortTag()
	}
}

// nodeLine returns n's 1-based line, or 0 when it has none.
func nodeLine(n *yaml.Node) int {
	if n != nil && n.Line > 0 {
		return n.Line
	}
	return 0
}

// positionOr returns line when it is positive, else fallback.
func positionOr(line, fallback int) int {
	if line > 0 {
		return line
	}
	return fallback
}

// contains reports whether ss contains s.
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// parseError builds a positioned *ParseError.
func parseError(file string, line int, format string, args ...any) error {
	return &ParseError{File: file, Line: line, Msg: fmt.Sprintf(format, args...)}
}
