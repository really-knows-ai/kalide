package template

import (
	"fmt"
	htmltemplate "html/template"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/mdcheck"
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
//
// A container template's example.md may declare nested instances (template-
// build-checks, nested-section-validation): validateExampleBlock parses the
// example by heading depth and validates every instance recursively against
// its own resolved template, reporting a violation with the full containment
// path (`columns[2] › blocks[1]`). Each block's body is checked against its own
// enclosing template — including whether that template declares child sections
// and its body.subheadings rule — so a container's headings are consumed as
// nested instances, never treated as subheadings, and a no-children template's
// `##`/`###` are governed by its body.subheadings. This keeps example
// validation in agreement with deck validation.
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

	// A section template's layout is itself a section instance, so a
	// `{{ section … }}` call it makes reads this example's field values as its
	// `.item.parent` (item-context). A slide template's parent is the slide, not
	// a section, so its calls carry nil. The `template` key is the section
	// instance's template selector, not a field, so it is dropped here exactly
	// as validateExampleBlock drops it from the executed context.
	var callerFields map[string]any
	if len(def.Fields) > 0 || len(def.Sections) > 0 {
		block, err := parseExampleBlock(string(lt.ExampleBytes), lt.Kind, 1)
		if err != nil {
			return withPath(err, lt.ExamplePath)
		}
		if err := validateExampleBlock(lt.ExamplePath, def, block, resolve, ctx); err != nil {
			return err
		}
		if lt.Kind == KindSection {
			callerFields = make(map[string]any, len(block.Frontmatter))
			for k, v := range block.Frontmatter {
				if k == "template" {
					continue
				}
				callerFields[k] = v
			}
		}
	}

	// Step 3 parsed lt.Layout with LayoutFuncMap's parse-resolvable `section`
	// stub; re-parse its source with that entry overridden by the load-time
	// helper so a layout that calls `{{ section … }}` executes here by
	// rendering the target instead of failing LoadLibrary's step-7 execution
	// (templates-dir-validation step 7).
	funcMap := LayoutFuncMap(lib.Media, "/"+MediaDir)
	funcMap["section"] = exampleSectionHelper(lib, callerFields)

	layoutName := lt.Layout.Name()
	if layoutName == "" {
		layoutName = lt.Name
	}
	layout, err := htmltemplate.New(layoutName).Funcs(funcMap).Parse(lt.LayoutText)
	if err != nil {
		return libraryErrorf(lt.LayoutPath, 0, "parse: %v", err)
	}

	var buf strings.Builder
	if err := layout.Execute(&buf, ctx); err != nil {
		return libraryErrorf(lt.LayoutPath, 0, "execute: %v", err)
	}
	return nil
}

// exampleRawContext builds the reserved `.raw` execution-context entry for a
// load-time example execution (templates-dir-validation step 7), mirroring the
// renderer's rawContext (internal/render/render.go) so the load-time context
// matches the render-time one (raw-source-context, template-context). It holds
// the source value of every declared field the author supplied, keyed by the
// field's own name, and the example's original body source under `body` when
// the author supplied one.
//
// data is the example's pre-conversion source map — a slide's or section's
// decoded example frontmatter, minus the reserved `template` selector — and t
// is the resolved template whose field schema names the declared fields. Only
// declared fields are copied: an undeclared key (including the `template`
// selector and any `<field>_format` sibling) is not a field. A field the author
// did not supply is absent, and a field's declared default is not copied,
// because a default is not an authored source value; a body the author did not
// supply is likewise absent. A nil t or data yields a well-formed
// (possibly body-only) context, so a caller may pass them freely.
func exampleRawContext(data map[string]any, t *Template, body string) map[string]any {
	out := make(map[string]any, 1)
	if t != nil {
		for i := range t.Fields {
			name := t.Fields[i].Name
			if v, present := data[name]; present {
				out[name] = v
			}
		}
	}
	if body != "" {
		out["body"] = body
	}
	return out
}

// exampleSectionHelper is the template-local `section` func the load-time
// example execution binds (templates-dir-validation step 7). It is a factory
// returning the func bound into a layout's func map, closing over lib and the
// calling instance's field values — callerFields is nil when the call is made
// from a slide layout, whose parent is the slide, not a section
// (item-context). It lives here rather than in internal/render because
// internal/template cannot import internal/render; the render-time renderer
// binds its own equivalent.
//
// The returned func resolves the call with ResolveSectionCall — the same
// resolution/validation the render-time helper reuses, so the target must be a
// section-usage template, the call's fields are defaults-first validated with
// CheckValues, the body is validated with CheckBody, and a body heading naming
// one of the target's child sections is rejected — and executes the target's
// layout against that resolution's result.
//
// The target's execution context mirrors checkLibraryExample's slide/section
// context so the target sees a consistent shape: the effective field values as
// top-level entries, the reserved `deck`/`slide` entries (the empty example
// stand-ins), the one-item `.item` descriptor (index 0, number/count 1,
// first/last true, section/template the target name, parent callerFields), and
// the reserved `.raw` source view of what the call supplied plus the supplied
// body. The body is set only when the call supplies one. The helper passes no
// child sections, so no child section keys are published.
//
// The target's layout is re-parsed under LayoutFuncMap with the `section` entry
// overridden by this same factory bound to the target's own field values, so a
// target layout that itself calls `{{ section … }}` both parses and executes —
// the nested call's `.item.parent` is this target's fields. The returned value
// is the target's rendered HTML as html/template.HTML, trusted exactly as the
// renderer returns it, so the caller's layout splices it unescaped.
func exampleSectionHelper(lib *Library, callerFields map[string]any) func(name string, args ...any) (htmltemplate.HTML, error) {
	resolve := func(name string) (*Template, bool) {
		other, _, ok := lib.TemplateByName(name)
		if !ok || other == nil || other.Definition == nil {
			return nil, false
		}
		return other.Definition, true
	}

	return func(name string, args ...any) (htmltemplate.HTML, error) {
		var fields map[string]any
		if len(args) >= 1 && args[0] != nil {
			m, ok := args[0].(map[string]any)
			if !ok {
				return "", fmt.Errorf("section helper %q: fields must be a map, got %T", name, args[0])
			}
			fields = m
		}
		var body string
		if len(args) >= 2 && args[1] != nil {
			s, ok := args[1].(string)
			if !ok {
				return "", fmt.Errorf("section helper %q: body must be a string, got %T", name, args[1])
			}
			body = s
		}

		call, err := ResolveSectionCall(resolve, name, fields, body, callerFields)
		if err != nil {
			return "", fmt.Errorf("section helper %q: %w", name, err)
		}

		// The target executes with its effective field values addressable at the
		// top level, the reserved deck/slide context and its one-item .item
		// descriptor — the same shape checkLibraryExample gives a section
		// template's own layout. .raw is the source view of what the call
		// supplied (the fields plus the body when present).
		ctx := make(map[string]any, len(call.Values)+5)
		for k, v := range call.Values {
			ctx[k] = v
		}
		ctx["deck"] = emptyExampleDeckContext()
		ctx["slide"] = emptyExampleSlideContext()
		ctx["item"] = call.Item
		raw := make(map[string]any, len(fields)+1)
		for k, v := range fields {
			raw[k] = v
		}
		if body != "" {
			raw["body"] = body
		}
		ctx["raw"] = raw
		if strings.TrimSpace(body) != "" {
			ctx["body"] = body
		}

		// A target layout that itself calls {{ section … }} must both parse and
		// execute: bind this same factory, closing over the target's own field
		// values as the nested call's caller fields (.item.parent).
		funcMap := LayoutFuncMap(lib.Media, "/"+MediaDir)
		funcMap["section"] = exampleSectionHelper(lib, call.Values)

		layoutName := call.Template.Layout.Name
		if layoutName == "" {
			layoutName = call.Template.Name
		}
		parsed, err := htmltemplate.New(layoutName).Funcs(funcMap).Parse(call.Template.Layout.Text)
		if err != nil {
			return "", fmt.Errorf("section helper %q: parse target layout: %w", name, err)
		}
		var buf strings.Builder
		if err := parsed.Execute(&buf, ctx); err != nil {
			return "", fmt.Errorf("section helper %q: execute target layout: %w", name, err)
		}
		return htmltemplate.HTML(buf.String()), nil
	}
}

// exampleBlock is one parsed `---`/fence-delimited frontmatter plus body plus
// nested named section instances, either a whole example.md (slide or
// section) or one heading-delimited section instance within one.
type exampleBlock struct {
	// Frontmatter is the parsed YAML mapping, or nil when the block carries
	// none.
	Frontmatter map[string]any

	// FrontmatterLine is the 1-based file line the frontmatter starts on, 0
	// when Frontmatter is nil.
	FrontmatterLine int

	// Body is the Markdown after the frontmatter, up to the first child
	// heading (or the whole remainder when there is none).
	Body string

	// BodyLine is Body's 1-based starting file line.
	BodyLine int

	// Sections holds each section instance in source order, excluding a
	// reserved `# notes` section.
	Sections []exampleSection
}

// exampleSection is one heading-delimited instance within an exampleBlock: the
// section name, the heading depth that opened it, its zero-based instance index
// among siblings of the same name, and its own block (frontmatter, body and
// nested children) under exampleBlock.Sections.
type exampleSection struct {
	Name  string
	Line  int
	Level int
	Index int
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
	level, name, ok := exampleHeadingLevel(line)
	if !ok || level != 1 {
		return "", false
	}
	return name, true
}

// exampleHeadingLevel parses an ATX heading line's depth (1-6) and its name.
// Heading depth is nesting depth, so a deeper heading is a child instance of
// the most recent shallower one (slide-sections). Seven or more `#` is not a
// heading. The name has any ATX closing sequence stripped.
func exampleHeadingLevel(line string) (level int, name string, ok bool) {
	trimmed := strings.TrimRight(line, " \t\r")
	trimmed = strings.TrimLeft(trimmed, " \t")
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes > 6 {
		return 0, "", false
	}
	rest := trimmed[hashes:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	name = strings.TrimSpace(rest)
	if strings.HasSuffix(name, "#") {
		body := strings.TrimRight(name, "#")
		if body != "" && (strings.HasSuffix(body, " ") || strings.HasSuffix(body, "\t")) {
			name = strings.TrimSpace(body)
		}
	}
	if name == "" {
		return 0, "", false
	}
	return hashes, name, true
}

// splitExampleSections splits lines (starting at file line startLine) into the
// body text before the first heading and the section instances that follow,
// nesting each heading one level deeper beneath the most recent shallower one
// (heading depth is nesting depth, slide-sections). A heading at the same or a
// shallower depth closes the open instances. Fenced code blocks are tracked so
// a `#` inside one is never mistaken for a heading.
func splitExampleSections(lines []string, startLine int) (body string, bodyLine int, sections []exampleSection) {
	body, bodyLine, sections = splitExampleLevel(lines, startLine)
	return body, bodyLine, sections
}

// splitExampleLevel parses one level of an example body: the body before its
// first heading and the instances at that level, recursing into deeper ones.
func splitExampleLevel(lines []string, startLine int) (body string, bodyLine int, sections []exampleSection) {
	inFence := false
	bodyEnd := len(lines)
	type heading struct {
		level int
		name  string
		line  int
		idx   int
	}
	var headings []heading
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		level, name, ok := exampleHeadingLevel(line)
		if !ok {
			continue
		}
		if bodyEnd == len(lines) {
			bodyEnd = i
		}
		headings = append(headings, heading{level, name, startLine + i, i})
	}

	body = strings.TrimRight(strings.Join(lines[:bodyEnd], "\n"), "\n")
	bodyLine = startLine
	if len(headings) == 0 {
		return body, bodyLine, nil
	}

	base := headings[0].level
	// Only headings at the first heading's depth open siblings here; a deeper
	// heading belongs to the instance that precedes it.
	counts := map[string]int{}
	i := 0
	for i < len(headings) {
		h := headings[i]
		if h.level < base {
			break
		}
		if h.level > base {
			i++
			continue
		}
		// Extent runs to the next heading at base depth or shallower.
		j := i + 1
		for j < len(headings) && headings[j].level > base {
			j++
		}
		start := h.idx + 1
		end := len(lines)
		if j < len(headings) {
			end = headings[j].idx
		}
		if h.name == "notes" {
			i = j
			continue
		}
		content := lines[start:end]
		index := counts[h.name]
		counts[h.name]++
		// Each instance is a full block in its own right: parseExampleBlock
		// strips its optional leading ``` frontmatter and, via
		// splitExampleSections, nests its own deeper instances.
		nested, err := parseExampleBlock(strings.Join(content, "\n"), KindSection, startLine+start)
		if err != nil {
			nested = &exampleBlock{Body: strings.Join(content, "\n"), BodyLine: startLine + start}
		}
		sections = append(sections, exampleSection{
			Name:         h.name,
			Line:         h.line,
			Level:        h.level,
			Index:        index,
			exampleBlock: nested,
		})
		i = j
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

// validateExampleBlock validates one block (a whole example.md, or one nested
// section instance) against def, populates ctx with the rendered
// field/body/section values a layout expects, and recurses into each declared
// child instance. container is the instance's own containment path
// (`columns[2]`) or "" at the root; child errors extend it and carry the full
// path (nested-section-validation).
func validateExampleBlock(path string, def *Template, block *exampleBlock, resolve func(string) (*Template, bool), ctx map[string]any) error {
	return validateExampleBlockAt(path, def, block, resolve, ctx, "")
}

// validateExampleBlockAt is validateExampleBlock with the owning instance's
// containment path threaded through for nested errors.
func validateExampleBlockAt(path string, def *Template, block *exampleBlock, resolve func(string) (*Template, bool), ctx map[string]any, container string) error {
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
		return exampleError(path, block.FrontmatterLine, container, "%s: %s", e.PathString(), e.What)
	}
	for k, v := range fields {
		ctx[k] = v
	}

	// The subheading gate carries whether this block's own enclosing template
	// declares child sections, so a heading at any depth is a section marker
	// there and deeper headings stay subheadings otherwise
	// (markdown-allowed-subset); CheckBody then enforces the template's own
	// body.subheadings rule.
	if ve, invalid := CheckBody(def.Body, block.Body, WithBodyLine(block.BodyLine)); invalid {
		return exampleError(path, ve.Line, container, "%s", ve.What)
	}
	if declares := len(def.Sections) > 0; declares || !def.Body.Subheadings {
		if err := checkExampleBodySubheadings(path, def, block, container); err != nil {
			return err
		}
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
		return exampleError(path, errs[0].Line, container, "%s", errs[0].What)
	}

	sectionValues := map[string][]map[string]any{}
	counts := map[string]int{}
	for _, sec := range block.Sections {
		childPath := joinContainment(container, sec.Name, counts[sec.Name])
		counts[sec.Name]++
		d, ok := sect.Declared(def, sec.Name)
		if !ok {
			return exampleError(path, sec.Line, container, "example declares section %q, which the template does not declare", sec.Name)
		}
		chosen, named := exampleTemplateName(sec.Frontmatter)
		if !named {
			if len(d.Accepted) != 1 {
				return exampleError(path, sec.Line, childPath, "example section %q must name a template: (accepts %s)", sec.Name, strings.Join(d.Accepted, ", "))
			}
			chosen = d.Accepted[0]
		}
		if !containsString(d.Accepted, chosen) {
			return exampleError(path, sec.Line, childPath, "example section %q uses template %q, which is not one of the accepted templates (%s)", sec.Name, chosen, strings.Join(d.Accepted, ", "))
		}
		nested, ok := resolve(chosen)
		if !ok || nested == nil {
			return exampleError(path, sec.Line, childPath, "example section %q uses template %q, which is not defined", sec.Name, chosen)
		}
		nestedCtx := map[string]any{}
		if err := validateExampleBlockAt(path, nested, sec.exampleBlock, resolve, nestedCtx, childPath); err != nil {
			return err
		}
		sectionValues[sec.Name] = append(sectionValues[sec.Name], nestedCtx)
	}
	for name, instances := range sectionValues {
		ctx[name] = instances
	}
	return nil
}

// joinContainment extends a containment path by one indexed segment
// `name[index]`, starting the path when parent is empty.
func joinContainment(parent, name string, index int) string {
	seg := name + "[" + strconv.Itoa(index) + "]"
	if parent == "" {
		return seg
	}
	return parent + " › " + seg
}

// exampleError builds a *LibraryError at path:line whose message is prefixed
// with the instance's containment path when there is one, so the canonical
// ` › ` notation is uniform at any depth (nested-section-validation,
// error-reporting).
func exampleError(path string, line int, container, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if container != "" {
		msg = container + ": " + msg
	}
	return libraryErrorf(path, line, "%s", msg)
}

// checkExampleBodySubheadings refuses a `##`/`###` subheading in an example
// body when the enclosing template declares child sections (every heading at
// any depth is then a section marker) or when the template forbids
// subheadings; depths 4-6 are refused in subheading position
// (markdown-allowed-subset). It reports the first offending heading with the
// instance's containment path.
func checkExampleBodySubheadings(path string, def *Template, block *exampleBlock, container string) error {
	if strings.TrimSpace(block.Body) == "" {
		return nil
	}
	issues := mdcheck.Check(path, []byte(block.Body), mdcheck.Options{
		Mode:                          mdcheck.BodyMode,
		StartLine:                     block.BodyLine,
		TemplateDeclaresChildSections: len(def.Sections) > 0,
	})
	for _, iss := range issues {
		if iss.Kind == mdcheck.KindUnsupportedConstruct && strings.Contains(iss.Message, "heading level") {
			return exampleError(path, iss.Line, container, "%s", iss.Message)
		}
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
// layout may read: the fixed `.deck`/`.slide` leaf keys from
// emptyExampleDeckContext as deck.title/deck.author/deck.date/deck.properties
// and emptyExampleSlideContext as slide.number/slide.total (template-context,
// deck-data-in-templates), plus the reserved source-context namespaces `.raw`
// (the author's original source values) and `.data` (the authored section tree
// as data), whose members are template-specific and so cannot be enumerated as
// leaf keys (raw-source-context, section-data-context). The `.deck`/`.slide`
// keys derive from those two functions, which stay the single source of truth
// for the fixed namespace shapes; `raw`/`data` are the dynamic source-values and
// authored-section-tree views. Callers that must enumerate the context
// vocabulary (notably the agent-guide drift self-test) do not duplicate the
// list. The returned slice is a fresh allocation.
func ContextKeys() []string {
	deck := emptyExampleDeckContext()
	slide := emptyExampleSlideContext()
	out := make([]string, 0, len(deck)+len(slide)+2)
	for key := range deck {
		out = append(out, "deck."+key)
	}
	for key := range slide {
		out = append(out, "slide."+key)
	}
	out = append(out, ".raw", ".data")
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
