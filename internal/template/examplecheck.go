package template

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file implements checkLibraryExamples, templates-dir-validation step 7:
// each template's example.md is validated against its parsed Definition
// (checkLibraryBuild, step 4, must have already filled that in) and its
// layout is executed. A schema violation or a layout execute error is
// reported as a *LibraryError positioned at the example or layout's file:line
// (template-manifest, template-language).
//
// A template that declares no fields and no sections has nothing for the
// schema-only checks to validate; its example.md is still required to exist
// (step 3) but its content is not shape-checked here, and its layout is still
// executed against a safe, empty render context. This keeps a minimal
// template (no schema at all) valid without requiring a particular
// example.md shape.

// checkLibraryExamples is templates-dir-validation step 7: validating each
// template's example.md against its parsed definition and executing its
// layout. Phase 1 leaves it a no-op hook point wired into LoadLibrary as the
// last step; phase-2 task-12 fills in the body.
func checkLibraryExamples(lib *Library) error {
	if lib == nil {
		return nil
	}
	for _, name := range sortedLibraryTemplateNames(lib.Slides) {
		if err := checkLibraryExample(lib, lib.Slides[name]); err != nil {
			return err
		}
	}
	for _, name := range sortedLibraryTemplateNames(lib.Sections) {
		if err := checkLibraryExample(lib, lib.Sections[name]); err != nil {
			return err
		}
	}
	return nil
}

// checkLibraryExample validates lt's example.md against lt.Definition and
// executes lt.Layout against the render context that example produces.
func checkLibraryExample(lib *Library, lt *LibraryTemplate) error {
	if lt == nil || lt.Definition == nil {
		return nil
	}
	def := lt.Definition
	resolve := func(name string) (*Template, bool) {
		other, _, ok := lib.TemplateByName(name)
		if !ok || other == nil || other.Definition == nil {
			return nil, false
		}
		return other.Definition, true
	}

	ctx := map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}

	if len(def.Fields) > 0 || len(def.Sections) > 0 {
		block, err := parseExampleBlock(string(lt.ExampleBytes), lt.Kind, 1)
		if err != nil {
			return withPath(err, lt.ExamplePath)
		}
		if err := validateExampleBlock(lt.ExamplePath, def, block, resolve, ctx); err != nil {
			return err
		}
	}

	var buf strings.Builder
	if err := lt.Layout.Execute(&buf, ctx); err != nil {
		return libraryErrorf(lt.LayoutPath, 0, "execute: %v", err)
	}
	return nil
}

// exampleBlock is one parsed `---`/fence-delimited frontmatter plus body plus
// nested named section instances, either a whole example.md (slide or
// section) or one `# name` section instance within one.
type exampleBlock struct {
	// Frontmatter is the parsed YAML mapping, or nil when the block carries
	// none.
	Frontmatter map[string]any

	// FrontmatterLine is the 1-based file line the frontmatter starts on, 0
	// when Frontmatter is nil.
	FrontmatterLine int

	// Body is the Markdown after the frontmatter, up to the first top-level
	// `# name` heading (or the whole remainder when there is none).
	Body string

	// BodyLine is Body's 1-based starting file line.
	BodyLine int

	// Sections holds each top-level `# name` instance in source order,
	// excluding a reserved `# notes` section.
	Sections []exampleSection
}

// exampleSection is one `# name` instance within an exampleBlock.
type exampleSection struct {
	Name string
	Line int
	*exampleBlock
}

// parseExampleBlock parses one frontmatter-delimited block starting at file
// line startLine: a slide-kind block expects a leading `---` YAML fence: a
// section-kind block (and every nested `# name` instance, regardless of the
// enclosing kind) expects a leading backtick fence. A block with neither
// leading form carries no frontmatter at all — its whole content is Body —
// which lets a schema-free template's example.md be any Markdown.
func parseExampleBlock(src string, kind TemplateKind, startLine int) (*exampleBlock, error) {
	lines := strings.Split(src, "\n")
	i := 0
	// Skip leading blank lines.
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}

	block := &exampleBlock{}
	restStart := i
	restLine := startLine + i

	if kind == KindSlide {
		if i < len(lines) && strings.TrimSpace(lines[i]) == "---" {
			fmLines, end, ok := scanFence(lines, i+1, "---")
			if ok {
				fm, err := decodeFrontmatter(strings.Join(fmLines, "\n"), startLine+i+1)
				if err != nil {
					return nil, err
				}
				block.Frontmatter = fm
				block.FrontmatterLine = startLine + i + 1
				restStart = end + 1
				restLine = startLine + end + 1
			}
		}
	} else {
		if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			fmLines, end, ok := scanFence(lines, i+1, "```")
			if ok {
				fm, err := decodeFrontmatter(strings.Join(fmLines, "\n"), startLine+i+1)
				if err != nil {
					return nil, err
				}
				block.Frontmatter = fm
				block.FrontmatterLine = startLine + i + 1
				restStart = end + 1
				restLine = startLine + end + 1
			}
		}
	}

	rest := lines[restStart:]
	body, bodyLine, sections := splitExampleSections(rest, restLine)
	block.Body = body
	block.BodyLine = restLine
	if body == "" {
		block.BodyLine = 0
	}
	_ = bodyLine
	block.Sections = sections
	return block, nil
}

// scanFence returns the lines strictly between lines[start-1] (the opening
// fence, already matched by the caller) and the next line equal to close,
// and the index of that closing line. ok is false when no closing line is
// found before the input ends.
func scanFence(lines []string, start int, close string) ([]string, int, bool) {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == close {
			return lines[start:i], i, true
		}
	}
	return nil, 0, false
}

// sectionHeadingPattern matches a top-level `# name` section heading line —
// exactly one `#`, a space, then the name — never a `##`/`###` subheading.
func isSectionHeading(line string) (string, bool) {
	trimmed := strings.TrimRight(line, " \t\r")
	if !strings.HasPrefix(trimmed, "# ") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "##") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
	if name == "" {
		return "", false
	}
	return name, true
}

// splitExampleSections splits lines (starting at file line startLine) into
// the body text before the first top-level `# name` heading and the section
// instances that follow, tracking fenced code blocks so a `#` inside one is
// never mistaken for a heading.
func splitExampleSections(lines []string, startLine int) (body string, bodyLine int, sections []exampleSection) {
	inFence := false
	bodyEnd := len(lines)
	var headings []struct {
		name string
		line int
		idx  int
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if name, ok := isSectionHeading(line); ok {
			if bodyEnd == len(lines) {
				bodyEnd = i
			}
			headings = append(headings, struct {
				name string
				line int
				idx  int
			}{name, startLine + i, i})
		}
	}

	body = strings.TrimRight(strings.Join(lines[:bodyEnd], "\n"), "\n")
	bodyLine = startLine

	for h := range headings {
		start := headings[h].idx + 1
		end := len(lines)
		if h+1 < len(headings) {
			end = headings[h+1].idx
		}
		if headings[h].name == "notes" {
			continue
		}
		content := lines[start:end]
		nested, _ := parseExampleBlock(strings.Join(content, "\n"), KindSection, startLine+start)
		sections = append(sections, exampleSection{
			Name:         headings[h].name,
			Line:         headings[h].line,
			exampleBlock: nested,
		})
	}
	return body, bodyLine, sections
}

// decodeFrontmatter parses raw YAML into a map, extracting and removing a
// `template:` selector key so the remainder is exactly the field data
// CheckValues expects. startLine is the frontmatter's first file line, used
// to offset yaml.v3's line numbers to file-absolute.
func decodeFrontmatter(raw string, startLine int) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// exampleTemplateName returns the `template:` selector a section instance's
// frontmatter names, and whether it named one at all.
func exampleTemplateName(fm map[string]any) (string, bool) {
	if fm == nil {
		return "", false
	}
	v, ok := fm["template"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// validateExampleBlock validates one block (a whole example.md, or one
// nested `# name` section instance) against def, populates ctx with the
// rendered field/body/section values a layout expects, and recurses into
// each declared section instance.
func validateExampleBlock(path string, def *Template, block *exampleBlock, resolve func(string) (*Template, bool), ctx map[string]any) error {
	fm := block.Frontmatter
	if fm == nil {
		fm = map[string]any{}
	}
	fields := make(map[string]any, len(fm))
	for k, v := range fm {
		if k == "template" {
			continue
		}
		fields[k] = v
	}

	if res := CheckValues(fields, def, resolve); len(res.Errors) > 0 {
		e := res.Errors[0]
		return libraryErrorf(path, block.FrontmatterLine, "%s: %s", e.PathString(), e.What)
	}
	for k, v := range fields {
		ctx[k] = v
	}

	if ve, invalid := CheckBody(def.Body, block.Body, WithBodyLine(block.BodyLine)); invalid {
		return libraryErrorf(path, ve.Line, "%s", ve.What)
	}
	if strings.TrimSpace(block.Body) != "" {
		ctx["body"] = block.Body
	}

	names := make([]string, 0, len(block.Sections))
	for _, sec := range block.Sections {
		names = append(names, sec.Name)
	}
	sect := NewSection(resolve)
	if errs := sect.CheckRepeats(def, names); len(errs) > 0 {
		return libraryErrorf(path, errs[0].Line, "%s", errs[0].What)
	}

	sectionValues := map[string][]map[string]any{}
	for _, sec := range block.Sections {
		d, ok := sect.Declared(def, sec.Name)
		if !ok {
			return libraryErrorf(path, sec.Line, "example declares section %q, which the template does not declare", sec.Name)
		}
		chosen, named := exampleTemplateName(sec.Frontmatter)
		if !named {
			if len(d.Accepted) != 1 {
				return libraryErrorf(path, sec.Line, "example section %q must name a template: (accepts %s)", sec.Name, strings.Join(d.Accepted, ", "))
			}
			chosen = d.Accepted[0]
		}
		if !containsString(d.Accepted, chosen) {
			return libraryErrorf(path, sec.Line, "example section %q uses template %q, which is not one of the accepted templates (%s)", sec.Name, chosen, strings.Join(d.Accepted, ", "))
		}
		nested, ok := resolve(chosen)
		if !ok || nested == nil {
			return libraryErrorf(path, sec.Line, "example section %q uses template %q, which is not defined", sec.Name, chosen)
		}
		nestedCtx := map[string]any{}
		if err := validateExampleBlock(path, nested, sec.exampleBlock, resolve, nestedCtx); err != nil {
			return err
		}
		sectionValues[sec.Name] = append(sectionValues[sec.Name], nestedCtx)
	}
	for name, instances := range sectionValues {
		ctx[name] = instances
	}
	return nil
}

// emptyExampleDeckContext returns a well-formed but empty stand-in for the
// reserved `.deck` render context, matching the shape render.deckContext
// produces, so a library example layout that reads `.deck.*` (including
// `.deck.properties`) executes cleanly at load-time even though no real deck
// data exists yet. internal/template cannot import internal/render (would
// create an import cycle), so the shape is duplicated here intentionally.
func emptyExampleDeckContext() map[string]any {
	return map[string]any{
		"title":      "",
		"author":     "",
		"date":       "",
		"properties": map[string]any{},
	}
}

// emptyExampleSlideContext returns a well-formed but empty stand-in for the
// reserved `.slide` render context, matching the shape render.slideContext
// produces. See emptyExampleDeckContext for why this is duplicated here.
func emptyExampleSlideContext() map[string]any {
	return map[string]any{
		"number": "",
		"total":  0,
	}
}

// ContextKeys returns the sorted reserved render-context keys a library example
// layout may read, as dotted paths: the `.deck` keys from
// emptyExampleDeckContext as deck.title/deck.author/deck.date/deck.properties,
// and the `.slide` keys from emptyExampleSlideContext as
// slide.number/slide.total (template-context, deck-data-in-templates). It
// derives them from those two functions, which stay the single source of truth
// for the reserved render context, so callers that must enumerate the context
// vocabulary (notably the agent-guide drift self-test) do not duplicate the
// list. The returned slice is a fresh allocation.
func ContextKeys() []string {
	out := make([]string, 0, len(emptyExampleDeckContext())+len(emptyExampleSlideContext()))
	for key := range emptyExampleDeckContext() {
		out = append(out, "deck."+key)
	}
	for key := range emptyExampleSlideContext() {
		out = append(out, "slide."+key)
	}
	sort.Strings(out)
	return out
}

// withPath re-positions err (from decodeFrontmatter, which carries no path)
// at path with line 0, when it is not already a *LibraryError.
func withPath(err error, path string) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*LibraryError); ok {
		return err
	}
	return libraryErrorf(path, 0, "%v", err)
}
