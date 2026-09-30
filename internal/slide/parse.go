package slide

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
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

// atxHeadingRe matches a CommonMark ATX heading of depth 1-6: up to three
// leading spaces, one to six `#`, then whitespace and the heading text (which
// may be empty). Seven or more `#`, or `#` with no following whitespace, is not
// a heading. Heading depth is nesting depth (slide-sections). Group 1 is the
// `#` run and group 2 the raw heading text.
var atxHeadingRe = regexp.MustCompile(`^[ \t]{0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)

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

// Section is one `# name` (or deeper) section of a slide. A section's
// frontmatter is the optional plain ``` fence immediately after its heading
// (section-frontmatter). Sections nest: a Section's Children are the child
// sections a deeper heading opens beneath it, so the tree is as deep as the
// headings are (slide-sections).
type Section struct {
	// Name is the section name: the heading text after its leading `#`s.
	Name string

	// Level is the heading depth that opened the section: 1 for `#` through 6
	// for `######`. Heading depth is nesting depth (slide-sections).
	Level int

	// Index is the section's zero-based instance index among its siblings of
	// the same Name, used to render the containment path segment `name[i]`
	// (nested-section-validation).
	Index int

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
	// present), up to the first child heading or the next heading at the same
	// or a shallower depth.
	Body string

	// BodyLine is the 1-based line where Body starts.
	BodyLine int

	// Children are the section's child sections in source order. A section
	// whose template declares no child sections has none; a child is exactly
	// one heading level deeper than its parent (slide-sections).
	Children []Section
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
// known (0 otherwise), the containment path when the error belongs to a nested
// section instance, and the message. Error renders as `file:line: message`, or
// `file:line › path: message` when a containment path is present, matching the
// deck package's canonical ` › ` (U+203A) notation
// (nested-section-validation, error-reporting).
type ParseError struct {
	// File is the file the error belongs to.
	File string

	// Line is the 1-based line, or 0 when there is no meaningful line.
	Line int

	// Path is the rendered containment path of the section instance the error
	// belongs to, for example `columns[2] › blocks[1]`, or "" at the slide's
	// top level. Segments are already in their final `name[i]` display form.
	Path string

	// Msg is the human-readable message.
	Msg string
}

// Error renders the positioned message.
func (e *ParseError) Error() string {
	pos := e.File
	if e.Line > 0 {
		pos = fmt.Sprintf("%s:%d", pos, e.Line)
	}
	if e.Path != "" {
		return fmt.Sprintf("%s › %s: %s", pos, e.Path, e.Msg)
	}
	return fmt.Sprintf("%s: %s", pos, e.Msg)
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
// Rules (deck-structure, slide-sections):
//   - the file opens with YAML frontmatter delimited by --- … --- at line 1;
//     missing, unterminated or not-at-top frontmatter is an error, and the
//     frontmatter must name a slide-usage `template:`;
//   - the content between the frontmatter and the first heading is the slide's
//     body;
//   - a `# name` heading starts a top-level section; a heading one level deeper
//     opens a child of the most recent shallower heading, down to `######`
//     (heading depth is nesting depth). A section's name must be declared by
//     its parent's template — the slide template for a top-level section, the
//     resolved parent template for a child — with a closest-match suggestion
//     otherwise, and its repeat count per parent must lie within the declared
//     min/max;
//   - a plain ``` fence immediately after a section heading is that section's
//     frontmatter (at any depth); a language tag on it, or a fence anywhere
//     else, is an error; `template:` is required on the section instance iff
//     the section accepts more than one template;
//   - `# notes` is the reserved speaker-notes section: top-level only, at most
//     one, last, and excluded from repeat limits; `notes` cannot be a declared
//     section name.
//
// Errors in a nested section instance carry its full containment path in the
// canonical ` › ` form (nested-section-validation).
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

	fm, err := parseBlock(file, lines[1:closeIdx], 2, "")
	if err != nil {
		return nil, err
	}
	tmpl, tmplLine, hasTmpl, err := templateInBlock(file, fm, "")
	if err != nil {
		return nil, err
	}
	if !hasTmpl || strings.TrimSpace(tmpl) == "" {
		return nil, parseError(file, 1, "missing required key %q in slide frontmatter", "template")
	}
	usage, ok := cat.LookupSlideTemplate(tmpl)
	if !ok {
		return nil, unknownName(file, tmplLine, "", "template", tmpl, cat.TemplateNames())
	}
	if usage != "slide" {
		return nil, parseError(file, tmplLine,
			"template %q is a %s template, not a slide template", tmpl, usage)
	}

	// Body before the first section marker. A heading is a marker only where
	// the enclosing template declares child sections; a depth-2-6 heading in a
	// slide template that declares no child sections stays in the slide body as
	// an ordinary Markdown subheading (slide-sections, markdown-allowed-subset).
	slideDeclares := len(cat.SectionNames(tmpl)) > 0
	firstHeading := -1
	for i := closeIdx + 1; i < len(lines); i++ {
		depth, _, isHeading := headingLevel(lines[i])
		if !isHeading {
			continue
		}
		if _, isMarker := sectionMarkerParent(nil, depth, slideDeclares); isMarker {
			firstHeading = i
			break
		}
	}
	bodyStart := closeIdx + 1
	bodyEnd := len(lines)
	if firstHeading != -1 {
		bodyEnd = firstHeading
	}
	if err := checkNoFence(file, lines, bodyStart, bodyEnd, ""); err != nil {
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

	sections, notes, err := parseSections(file, lines, firstHeading, cat, tmpl, tmplLine)
	if err != nil {
		return nil, err
	}
	slide.Sections = sections
	slide.Notes = notes
	return slide, nil
}

// parseSections builds the slide's section tree from the heading sequence
// starting at firstHeading (the index of the first section marker, or -1).
// A heading one level deeper than the most recent shallower heading is that
// heading's child; a heading at the same or a shallower depth closes the open
// sections until it finds its parent. Each section carries its own optional
// plain-fence frontmatter and its resolved `template:` (resolved against its
// parent's declarations), and each parent's children are counted per Name to
// enforce the parent's declared min/max. It returns the top-level sections and
// the reserved `# notes` section, if any.
//
// A heading is a section marker only where its enclosing template declares
// child sections; anywhere else a depth-1-6 heading stays in the enclosing body
// as ordinary Markdown content, and `# notes` stays top-level only
// (slide-sections, markdown-allowed-subset).
//
// Section names, child names and template resolution are all validated here, so
// the errors carry the offending instance's containment path
// (nested-section-validation).
func parseSections(file string, lines []string, firstHeading int, cat Catalogue, slideTemplate string, tmplLine int) ([]Section, *Notes, error) {
	if firstHeading < 0 {
		return nil, nil, nil
	}

	// slideDeclares is whether the slide template declares top-level sections;
	// when it does not, a depth-2-6 heading in the slide body stays ordinary
	// Markdown (slide-sections, markdown-allowed-subset).
	slideDeclares := len(cat.SectionNames(slideTemplate)) > 0

	// childCounts[depth][parentInstanceKey][childName] counts the child
	// instances declared directly under one parent instance, so a parent's
	// min/max is enforced over its own children, not the whole slide
	// (nested-section-validation). parentInstanceKey is the parent's path.
	childCounts := map[int]map[string]map[string]int{}

	// topCounts counts the slide's top-level instances by declared name.
	topCounts := map[string]int{}

	var stack []sectionFrame
	var roots []Section

	var notes *Notes
	notesSeen := false

	i := firstHeading
	for i >= 0 && i < len(lines) {
		depth, name, isHeading := headingLevel(lines[i])
		if !isHeading {
			i++
			continue
		}
		headingLine := i + 1

		if notesSeen {
			if name == NotesSection && depth == 1 {
				return nil, nil, parseError(file, headingLine, "duplicate %q section", NotesSection)
			}
			return nil, nil, parseError(file, headingLine,
				"%q section must be the last section on the slide", NotesSection)
		}
		if depth == 1 && name == NotesSection {
			// The reserved notes name is never a declared section: a slide
			// template that declares it is an author error (speaker-notes).
			if containsStr(cat.SectionNames(slideTemplate), NotesSection) {
				return nil, nil, parseError(file, headingLine,
					"%q is reserved and cannot be declared as a section name", NotesSection)
			}
			// A notes section may only be followed by end of file; any later
			// heading is caught at the top of the next iteration.
			notesStart := i + 1
			notesEnd := len(lines)
			if next := nextMarker(lines, notesStart, nil, slideDeclares); next != -1 {
				notesSeen = true
				notesEnd = next
			}
			if err := checkNoFence(file, lines, notesStart, notesEnd, ""); err != nil {
				return nil, nil, err
			}
			notes = &Notes{
				HeadingLine: headingLine,
				BodyLine:    notesStart + 1,
				Body:        strings.Join(lines[notesStart:notesEnd], "\n"),
			}
			i = nextMarker(lines, notesStart, nil, slideDeclares)
			continue
		}

		// A heading is a section marker only where its enclosing template
		// declares child sections; anywhere else it stays in the enclosing body
		// as ordinary Markdown content (slide-sections,
		// markdown-allowed-subset).
		parentIdx, isMarker := sectionMarkerParent(stack, depth, slideDeclares)
		if !isMarker {
			i++
			continue
		}
		if name == "" {
			return nil, nil, parseError(file, headingLine, "section heading needs a name")
		}
		// Close the frames this marker supersedes: it is a child of the nearest
		// open instance shallower than it, or a top-level section when there is
		// none.
		stack = stack[:parentIdx+1]
		parentPath := ""
		if parentIdx >= 0 {
			parentPath = stack[parentIdx].path
		}
		topLevel := parentIdx < 0

		var d sectionDecl
		if topLevel {
			// Top-level: the slide template declares this section.
			if !containsStr(cat.SectionNames(slideTemplate), name) {
				return nil, nil, unknownName(file, headingLine, "", "section", name, sortedCopy(cat.SectionNames(slideTemplate)))
			}
			topCounts[name]++
			accepted, minRep, maxRep, _ := cat.SectionDecl(slideTemplate, name)
			d = sectionDecl{accepted: accepted, min: minRep, max: maxRep, ok: true}
			if d.max > 0 && topCounts[name] > d.max {
				return nil, nil, parseError(file, headingLine,
					"section %q: at most %d allowed, found %d", name, d.max, topCounts[name])
			}
		} else {
			// Nested: the enclosing instance's own resolved template declares
			// this child (slide-sections).
			parent := stack[parentIdx]
			dd, err := resolveChildTemplate(file, cat, parent.template, name, headingLine, containmentPath(instanceNames(parent.path), instanceIndexes(parent.path)))
			if err != nil {
				return nil, nil, err
			}
			d = dd
			if childCounts[depth] == nil {
				childCounts[depth] = map[string]map[string]int{}
			}
			if childCounts[depth][parent.path] == nil {
				childCounts[depth][parent.path] = map[string]int{}
			}
			childCounts[depth][parent.path][name]++
			if d.max > 0 && childCounts[depth][parent.path][name] > d.max {
				return nil, nil, parseError(file, headingLine,
					"section %q: at most %d allowed, found %d", name, d.max, childCounts[depth][parent.path][name])
			}
		}
		siblingIndex := topCounts[name] - 1
		if !topLevel {
			siblingIndex = childCounts[depth][parentPath][name] - 1
		}
		path := chainPath(parentPath, name, siblingIndex)

		section := Section{
			Name:        name,
			Level:       depth,
			Index:       siblingIndex,
			HeadingLine: headingLine,
		}
		contentStart := i + 1

		// A plain ``` fence immediately after the heading is the section's
		// frontmatter (section-frontmatter), at any depth. Its close consumes
		// the block, so YAML comment lines inside it are never mistaken for
		// headings.
		if contentStart < len(lines) {
			if isFence, lang := matchFence(lines[contentStart]); isFence {
				if lang != "" {
					return nil, nil, &ParseError{File: file, Line: contentStart + 1, Path: path,
						Msg: fmt.Sprintf("section frontmatter fence must not have a language tag (%q)", lang)}
				}
				close := -1
				for j := contentStart + 1; j < len(lines); j++ {
					if f, l := matchFence(lines[j]); f {
						if l != "" {
							return nil, nil, &ParseError{File: file, Line: j + 1, Path: path,
								Msg: fmt.Sprintf("section frontmatter fence must not have a language tag (%q)", l)}
						}
						close = j
						break
					}
				}
				if close == -1 {
					return nil, nil, &ParseError{File: file, Line: contentStart + 1, Path: path,
						Msg: fmt.Sprintf("unterminated section frontmatter: missing closing %q", SectionFence)}
				}
				blk, err := parseBlock(file, lines[contentStart+1:close], contentStart+2, path)
				if err != nil {
					return nil, nil, err
				}
				section.FenceLine = contentStart + 1
				section.Frontmatter = blk.values
				resolvePath := containmentPath(instanceNames(path), pathSegments(path))
				if err := resolveSectionTemplate(file, cat, name,
					d, blk, section.FenceLine, headingLine, &section, resolvePath); err != nil {
					return nil, nil, err
				}
				contentStart = close + 1
			}
		}

		// A template: is required on a section instance iff len(accepted) > 1.
		// Exactly one accepted template resolves automatically. When the
		// section has no fence at all, the requirement is reported at the
		// heading, since there is no fence line.
		if section.FenceLine == 0 {
			switch {
			case len(d.accepted) > 1:
				return nil, nil, &ParseError{File: file, Line: headingLine, Path: path,
					Msg: fmt.Sprintf("section %q: a template: is required when the section accepts more than one template", name)}
			case len(d.accepted) == 1:
				section.Template = d.accepted[0]
			}
		}
		if section.Template == NotesSection {
			return nil, nil, &ParseError{File: file, Line: positionOr(section.TemplateLine, headingLine), Path: path,
				Msg: fmt.Sprintf("%q is reserved and cannot be used as a section template", NotesSection)}
		}

		// Attach the instance to the tree and open its frame, so its body can
		// be bounded at the next section marker in its own resolved template's
		// context (slide-sections).
		if topLevel {
			roots = append(roots, section)
			stack = append(stack, newSectionFrame(&roots[len(roots)-1], cat, path))
		} else {
			parent := &stack[parentIdx]
			parent.section.Children = append(parent.section.Children, section)
			stack = append(stack, newSectionFrame(&parent.section.Children[len(parent.section.Children)-1], cat, path))
		}

		// The section body runs to the next section marker for this frame. A
		// deeper heading inside a template that declares no child sections is
		// body content, not a marker; a fence found in the body is misplaced
		// and is reported at the fence line.
		bodyEnd := len(lines)
		if next := nextMarker(lines, contentStart, stack, slideDeclares); next != -1 {
			bodyEnd = next
		}
		if err := checkNoFence(file, lines, contentStart, bodyEnd, path); err != nil {
			return nil, nil, err
		}

		frame := &stack[len(stack)-1]
		frame.section.BodyLine = contentStart + 1
		frame.section.Body = strings.Join(lines[contentStart:bodyEnd], "\n")

		i = bodyEnd
	}

	// Per-parent min repeats are checked last. A child's own template declares
	// its required children; a missing child has no offending line, so the
	// parent instance's heading line is the most useful position and the error
	// carries the parent's containment path (nested-section-validation).
	if err := checkMinChildren(file, lines, roots, cat); err != nil {
		return nil, nil, err
	}
	// The slide template's own top-level minimums.
	if err := checkTopMin(file, cat.SectionNames(slideTemplate), cat, slideTemplate, topCounts, tmplLine); err != nil {
		return nil, nil, err
	}
	return roots, notes, nil
}

// checkTopMin enforces the slide template's declared min repeat counts over the
// slide's top-level instances, iterating the declared names in sorted order for
// determinism. A missing section has no offending line, so the slide's template
// line is used.
func checkTopMin(file string, names []string, cat Catalogue, slideTemplate string, counts map[string]int, tmplLine int) error {
	sorted := sortedCopy(names)
	for _, name := range sorted {
		if name == NotesSection {
			continue
		}
		accepted, minRep, _, ok := cat.SectionDecl(slideTemplate, name)
		_ = accepted
		if !ok || minRep <= 0 {
			continue
		}
		if counts[name] < minRep {
			return parseError(file, positionOr(tmplLine, 1),
				"section %q: requires at least %d, found %d", name, minRep, counts[name])
		}
	}
	return nil
}

// checkMinChildren walks the built section tree and enforces each section
// template's declared minimums over the child instances its instances actually
// contain, reporting the parent instance's containment path.
func checkMinChildren(file string, lines []string, roots []Section, cat Catalogue) error {
	_ = lines
	var err error
	var walk func(s *Section, path string)
	walk = func(s *Section, path string) {
		if err != nil {
			return
		}
		childDecls := map[string]sectionDecl{}
		if s.Template != "" {
			childDecls = declsFor(cat, s.Template)
		}
		seen := map[string]int{}
		for k := range s.Children {
			seen[s.Children[k].Name]++
		}
		for _, name := range sortedCopy(keysOf(childDecls)) {
			d := childDecls[name]
			if !d.ok || d.min <= 0 {
				continue
			}
			if seen[name] < d.min {
				err = &ParseError{File: file, Line: s.HeadingLine, Path: path,
					Msg: fmt.Sprintf("section %q: requires at least %d, found %d", name, d.min, seen[name])}
				return
			}
		}
		for k := range s.Children {
			child := &s.Children[k]
			walk(child, chainPath(path, child.Name, child.Index))
		}
	}
	for r := range roots {
		walk(&roots[r], containmentPath([]string{roots[r].Name}, []int{roots[r].Index}))
	}
	return err
}

// declsFor builds the name → sectionDecl map for tmpl's declared sections.
func declsFor(cat Catalogue, tmpl string) map[string]sectionDecl {
	names := cat.SectionNames(tmpl)
	out := make(map[string]sectionDecl, len(names))
	for _, n := range names {
		a, mn, mx, ok := cat.SectionDecl(tmpl, n)
		out[n] = sectionDecl{accepted: a, min: mn, max: mx, ok: ok}
	}
	return out
}

// sortedCopy returns a sorted copy of ss.
func sortedCopy(ss []string) []string {
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}

// containsStr reports whether ss contains s.
func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// keysOf returns the keys of m.
func keysOf(m map[string]sectionDecl) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// pathChain is the parsed form of a containment path: the section names from
// the root to the instance and each one's zero-based instance index.
type pathChain struct {
	names   []string
	indexes []int
}

// chainPath extends parent by one instance segment `name[index]`.
func chainPath(parent, name string, index int) string {
	var c pathChain
	if parent != "" {
		c = parseChain(parent)
	}
	c.names = append(c.names, name)
	c.indexes = append(c.indexes, index)
	return containmentPath(c.names, c.indexes)
}

// parseChain parses a rendered containment path (`a[0] › b[2]`) back to its
// names and indexes.
func parseChain(path string) pathChain {
	if path == "" {
		return pathChain{}
	}
	var c pathChain
	for _, seg := range strings.Split(path, " › ") {
		name := seg
		idx := -1
		if open := strings.LastIndexByte(seg, '['); open >= 0 && strings.HasSuffix(seg, "]") {
			if n, err := strconv.Atoi(seg[open+1 : len(seg)-1]); err == nil {
				name = seg[:open]
				idx = n
			}
		}
		c.names = append(c.names, name)
		c.indexes = append(c.indexes, idx)
	}
	return c
}

// instanceNames returns the section names of a rendered containment path.
func instanceNames(path string) []string { return parseChain(path).names }

// instanceIndexes returns the instance indexes of a rendered containment path.
func instanceIndexes(path string) []int { return parseChain(path).indexes }

// pathSegments returns the indexes of a rendered containment path (the same as
// instanceIndexes), named for the containmentPath call sites.
func pathSegments(path string) []int { return parseChain(path).indexes }

// sectionDecl mirrors a Catalogue SectionDecl result.
type sectionDecl struct {
	accepted []string
	min      int
	max      int
	ok       bool
}

// resolveChildTemplate scopes one child heading to its parent: given the parent
// template that owns the declarations (the slide template for a top-level
// section, the parent section's resolved template for a child), it returns the
// declaration for name among that parent's declared child sections
// (Catalogue.SectionNames/SectionDecl). An undeclared name is rejected with a
// closest-match suggestion over the parent's declared names, carrying the
// child's containment path. It owns child-scope derivation and child-name
// validation only; it never resolves the instance's `template:` value.
func resolveChildTemplate(file string, cat Catalogue, parentTemplate, name string, line int, path string) (sectionDecl, error) {
	names := cat.SectionNames(parentTemplate)
	accepted, minRep, maxRep, ok := cat.SectionDecl(parentTemplate, name)
	if !ok {
		return sectionDecl{}, unknownName(file, line, path, "section", name, names)
	}
	return sectionDecl{accepted: accepted, min: minRep, max: maxRep, ok: true}, nil
}

// resolveSectionTemplate is the parent-level `template:` resolution entry
// point. Given one section instance's frontmatter block (nil when the section
// has none) and the parent-scoped sectionDecl d it must resolve against
// (from the slide template for a top-level section, from resolveChildTemplate
// for a child), it resolves the instance's `template:` into out: required when
// the parent declares more than one accepted template, resolved automatically
// when exactly one is accepted, refused for the reserved `notes` name, checked
// to be a section-usage template, and checked for membership in the accepted
// set. It never derives declarations.
//
// fenceLine is the line of the opening ``` fence (0 when there is no
// frontmatter), headingLine the section heading line, and path the instance's
// containment path, so a required-but-missing template: is reported at the
// fence when there is one and at the heading otherwise, and every error carries
// the containment path (nested-section-validation).
func resolveSectionTemplate(file string, cat Catalogue, section string,
	d sectionDecl, blk *catalogueBlock, fenceLine, headingLine int, out *Section, path string) error {

	perr := func(line int, format string, args ...any) error {
		return &ParseError{File: file, Line: line, Path: path, Msg: fmt.Sprintf(format, args...)}
	}

	tmplName, tmplLine, hasTmpl, err := templateInBlock(file, blk, path)
	if err != nil {
		return err
	}
	if len(d.accepted) == 0 {
		if hasTmpl {
			return perr(positionOr(tmplLine, fenceLine),
				"section %q does not accept a template:", section)
		}
		return nil
	}
	if !hasTmpl {
		if len(d.accepted) > 1 {
			return perr(positionOr(fenceLine, headingLine),
				"section %q: a template: is required when the section accepts more than one template", section)
		}
		out.Template = d.accepted[0]
		return nil
	}

	line := positionOr(tmplLine, positionOr(fenceLine, headingLine))
	if strings.TrimSpace(tmplName) == "" {
		return perr(line,
			"section %q: key %q must name a template", section, "template")
	}
	if tmplName == NotesSection {
		return perr(line,
			"%q is reserved and cannot be used as a section template", NotesSection)
	}
	usage, ok := cat.LookupSlideTemplate(tmplName)
	if !ok {
		return unknownName(file, line, path, "template", tmplName, cat.TemplateNames())
	}
	if usage != "section" {
		return perr(line,
			"template %q is a %s template, not a section template", tmplName, usage)
	}
	if !contains(d.accepted, tmplName) {
		return unknownName(file, line, path,
			"template for section "+section, tmplName, d.accepted)
	}
	out.Template = tmplName
	out.TemplateLine = tmplLine
	return nil
}

// unknownName reports an unknown template or section name, adding a
// closest-match suggestion from candidates when one exists. path is the
// offender's containment path, or "" at the slide's top level.
func unknownName(file string, line int, path, what, name string, candidates []string) error {
	if s := suggest.Closest(name, candidates); s != "" {
		return &ParseError{File: file, Line: line, Path: path,
			Msg: fmt.Sprintf("unknown %s %q: did you mean %q?", what, name, s)}
	}
	return &ParseError{File: file, Line: line, Path: path,
		Msg: fmt.Sprintf("unknown %s %q", what, name)}
}

// templateInBlock finds the `template:` key in a frontmatter block. The block's
// YAML node lines are absolute (parseBlock pads the source), so they can be
// reported directly. has is false when the key is absent. path is the
// containment path of the section the block belongs to, or "" for the slide
// frontmatter.
func templateInBlock(file string, b *catalogueBlock, path string) (name string, line int, has bool, err error) {
	if b == nil || b.mapping == nil {
		return "", 0, false, nil
	}
	m := b.mapping
	if m.Kind != yaml.MappingNode {
		return "", 0, false, &ParseError{File: file, Line: nodeLine(m), Path: path,
			Msg: fmt.Sprintf("expected a mapping of frontmatter keys, got %s", kindWord(m))}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i], m.Content[i+1]
		if key.Value != "template" {
			continue
		}
		keyLine := nodeLine(key)
		if !isString(val) {
			return "", keyLine, true, &ParseError{File: file, Line: keyLine, Path: path,
				Msg: fmt.Sprintf("key %q: expected a string, got %s", "template", kindWord(val))}
		}
		return val.Value, keyLine, true, nil
	}
	return "", 0, false, nil
}

// parseBlock decodes a YAML frontmatter block. startLine is the 1-based line
// of the block's first YAML line; the text is padded with newlines so every
// node line and YAML error position is absolute within the file. An empty block
// decodes to an empty block with no mapping. path is the containment path of
// the section the block belongs to, or "" for the slide frontmatter.
func parseBlock(file string, lines []string, startLine int, path string) (*catalogueBlock, error) {
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
		return nil, &ParseError{File: file, Line: startLine, Path: path,
			Msg: fmt.Sprintf("invalid YAML: %v", err)}
	}
	m := documentMapping(&doc)
	if m == nil {
		return b, nil
	}
	if m.Kind != yaml.MappingNode {
		return nil, &ParseError{File: file, Line: nodeLine(m), Path: path,
			Msg: fmt.Sprintf("expected a mapping of frontmatter keys, got %s", kindWord(m))}
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
// reported as such, at the fence line. path is the enclosing instance's
// containment path, or "" at the slide's top level.
func checkNoFence(file string, lines []string, from, to int, path string) error {
	for i := from; i < to && i < len(lines); i++ {
		if isFence, lang := matchFence(lines[i]); isFence {
			if lang != "" {
				return &ParseError{File: file, Line: i + 1, Path: path,
					Msg: fmt.Sprintf("section frontmatter fence must not have a language tag (%q)", lang)}
			}
			return &ParseError{File: file, Line: i + 1, Path: path,
				Msg: "frontmatter fence is only allowed immediately after a section heading"}
		}
	}
	return nil
}

// sectionFrame is one open section instance while parseSections walks the
// heading sequence: a pointer into the built tree, the template the instance
// resolved to, whether that template declares child sections, and the
// instance's containment path.
type sectionFrame struct {
	section          *Section
	template         string
	declaresChildren bool
	path             string
}

// newSectionFrame opens the frame for one section instance attached to the
// tree, deriving its child-section state from the template the instance
// resolved to (slide-sections).
func newSectionFrame(section *Section, cat Catalogue, path string) sectionFrame {
	return sectionFrame{
		section:          section,
		template:         section.Template,
		declaresChildren: len(cat.SectionNames(section.Template)) > 0,
		path:             path,
	}
}

// sectionMarkerParent reports whether a heading of the given depth is a section
// marker in the current context, and the index into stack of its enclosing
// section frame (-1 at the slide's top level). stack is the open-section stack,
// shallowest first; slideDeclares says whether the slide template declares
// top-level sections. Frames at the same or a deeper level are superseded by
// the heading.
//
// A depth-1 heading is always a marker: `#` is reserved for top-level sections
// and the reserved `# notes` section (markdown-allowed-subset, speaker-notes).
// A depth-2-6 heading at the slide's top level is a marker only when the slide
// template declares child sections; otherwise it stays in the slide body. A
// deeper heading inside an instance is a marker only when that instance's
// resolved template declares child sections; otherwise it stays in the
// instance's body as an ordinary Markdown subheading (slide-sections,
// markdown-allowed-subset).
func sectionMarkerParent(stack []sectionFrame, depth int, slideDeclares bool) (parent int, ok bool) {
	n := len(stack)
	for n > 0 && stack[n-1].section.Level >= depth {
		n--
	}
	if n == 0 {
		if depth == 1 {
			return -1, true
		}
		return -1, slideDeclares
	}
	return n - 1, stack[n-1].declaresChildren
}

// nextMarker returns the index of the next section marker at or after from in
// the context of stack, or -1 when the rest of the lines are body content. A
// heading that is not a marker (a deeper heading inside a template that
// declares no child sections) is skipped, so it stays in the enclosing body
// (slide-sections, markdown-allowed-subset).
func nextMarker(lines []string, from int, stack []sectionFrame, slideDeclares bool) int {
	for i := from; i < len(lines); i++ {
		depth, _, isHeading := headingLevel(lines[i])
		if !isHeading {
			continue
		}
		if _, ok := sectionMarkerParent(stack, depth, slideDeclares); ok {
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

// headingName returns the section name of an ATX heading line at any depth 1-6,
// or "" when the line is not a heading or the heading has no name. A CommonMark
// ATX closing sequence (`# foo #`, `## foo ##`) is stripped at any depth.
func headingName(line string) string {
	m := atxHeadingRe.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(m[2])
	if strings.HasSuffix(name, "#") {
		trimmed := strings.TrimRight(name, "#")
		if trimmed != "" && strings.HasSuffix(trimmed, " ") {
			name = strings.TrimSpace(trimmed)
		}
	}
	return name
}

// headingLevel parses an ATX heading line's depth (1-6) and its name. ok is
// false when line is not a depth-1-6 ATX heading. The name has any CommonMark
// ATX closing sequence stripped (headingName). Heading depth is nesting depth: a
// `#` heading is a top-level section and each extra `#` is one level deeper
// (slide-sections).
func headingLevel(line string) (level int, name string, ok bool) {
	m := atxHeadingRe.FindStringSubmatch(line)
	if m == nil {
		return 0, "", false
	}
	return len(m[1]), headingName(line), true
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

// containmentPath renders a nested-section containment path from, per depth, the
// section name and the zero-based instance index of the section at that depth.
// A negative index renders the segment as the bare name: it marks a section
// that is not one instance (the leaf of a min/max or unknown-name error, or an
// aggregate over a parent's child instances). Segments are joined with the
// canonical ` › ` (U+203A) separator (nested-section-validation,
// error-reporting), so per-depth counters [2, 1] over [columns, blocks] render
// `columns[2] › blocks[1]`. It is shared by the parser's nested validation
// errors.
func containmentPath(names []string, indexes []int) string {
	parts := make([]string, len(names))
	for i, name := range names {
		if i < len(indexes) && indexes[i] >= 0 {
			parts[i] = name + "[" + strconv.Itoa(indexes[i]) + "]"
			continue
		}
		parts[i] = name
	}
	return strings.Join(parts, " › ")
}

// parseError builds a positioned *ParseError.
func parseError(file string, line int, format string, args ...any) error {
	return &ParseError{File: file, Line: line, Msg: fmt.Sprintf(format, args...)}
}
