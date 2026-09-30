package validate

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/mdcheck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// This file implements Validate, the fail-fast whole-deck validator. It drives
// the phase-2/3/4 loaders and checkers in the fixed deterministic order the
// specification requires and turns their first error into the single
// author-facing ValidationError defined in validate.go.
//
// Validate owns no rules of its own: kalide.yaml is checked by internal/deck
// (which resolves the theme through internal/theme), slide filenames and
// ordering by internal/deck, slide structure by internal/slide.Parse, Markdown
// bodies and inline text by internal/mdcheck, and field values by
// internal/template.CheckValues. Validate's whole job is the order, the
// recursion boundaries and the error adaptation.
//
// The compile-time assertion below pins the structural conformance between the
// phase-4 template registry and the phase-2 slide catalogue: internal/template
// never imports internal/slide, so *template.Registry must keep satisfying
// slide.Catalogue by its method set alone, and any drift in a method name or
// signature fails go build (whole-deck-validation).
var _ slide.Catalogue = (*template.Registry)(nil)

// Validate validates the whole deck rooted at fsys — ConfigFile (kalide.yaml)
// and the SlidesDir (slides/) directly beneath it — and returns exactly the
// first failure in the fixed deterministic order defined by
// whole-deck-validation:
//
//  1. kalide.yaml, through deck.LoadConfig, with the config's theme resolved
//     through themeReg (internal/deck + internal/theme);
//  2. slide filenames — numbering, letters and label uniqueness — through
//     deck.LoadSlides;
//  3. each slide in number/letter order (a horizontal slide before the vertical
//     slides beneath it), and within a slide top-to-bottom by source line: the
//     slide frontmatter's fields (template.CheckValues), then the slide body
//     (mdcheck.Check), then each section in source order with its frontmatter
//     fields and body, then the reserved notes body;
//  4. inter-slide #label links last, once every label in the deck is known,
//     through checkLinks (links.go).
//
// The rules are uniform and recursive at every nesting level: CheckValues
// follows section-template-as-type fields and list items to any depth against
// reg's lookup, and every field error carries its full path (for example
// `column[1] › people[0] › name`). Field errors carry no line because the
// phase-2 parser does not retain per-value YAML positions; the ValidationError
// formatter omits `:line` then (see ValidationError.Line). Structural,
// filename, body and Markdown errors do carry their line.
//
// reg must be a populated template registry — the caller passes the registry
// built from the project's loaded template.Library
// (template.NewRegistryFromLibrary). themeReg must be the project's theme
// registry — typically the one theme.LoadDir built from the project's
// templates/themes directory; it is required, never defaulted to any
// compiled-in registry. The second result reports whether an error was found:
// when it is false the deck is valid and the returned ValidationError is the
// zero value, so a caller reports the first error like:
//
//	if verr, invalid := validate.Validate(fsys, reg, themeReg); invalid {
//		fmt.Fprintln(os.Stderr, validate.Format(verr))
//	}
//
// Validate is deterministic: the same deck always yields the same single
// error.
//
// single-binary (requirements.requirement.single-binary): validation is
// packaged in the binary and runs offline — every read goes through fsys
// with slash-separated io/fs paths (deck.ConfigFile, deck.SlidesDir, slide
// paths, image fs.Stat), never path/filepath, the OS or the network. It stays
// in-process, self-contained and OS-neutral across all six supported targets
// (darwin/arm64, darwin/amd64, windows/amd64, windows/arm64, linux/amd64,
// linux/arm64), so the same deck yields the same result on each.
func Validate(fsys fs.FS, reg *template.Registry, themeReg *theme.Registry) (ValidationError, bool) {
	if fsys == nil {
		return New("", 0, nil, "no deck filesystem given", "pass the deck directory's fs.FS"), true
	}
	if reg == nil {
		return New(deck.ConfigFile, 0, nil, "no template registry given", "pass a populated template registry"), true
	}
	if themeReg == nil {
		return New(deck.ConfigFile, 0, nil, "no theme registry given", "pass the project's theme registry"), true
	}

	// Step 1: kalide.yaml, including the deck-wide theme, through internal/deck
	// and internal/theme.
	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themeReg)
	if err != nil {
		return adaptDeckError(err), true
	}
	// deck.LoadConfig has already resolved cfg.Theme against themeReg, so this
	// is a defensive re-check rather than the primary enforcement point.
	if !themeResolves(themeReg, cfg.Theme) {
		return New(deck.ConfigFile, 0, nil,
			fmt.Sprintf("unknown theme %q", cfg.Theme),
			"use one of the registered themes"), true
	}

	// Step 2: slide filenames, numbering, letters and label uniqueness.
	d, err := deck.LoadSlides(fsys, deck.SlidesDir)
	if err != nil {
		return adaptDeckError(err), true
	}

	// Step 3: each slide in number/letter order, a horizontal slide before the
	// vertical slides beneath it.
	for _, stack := range d.Stacks {
		ordered := make([]deck.Slide, 0, 1+len(stack.Vertical))
		ordered = append(ordered, stack.Slide)
		ordered = append(ordered, stack.Vertical...)
		for _, s := range ordered {
			if verr, invalid := validateSlide(fsys, s, reg); invalid {
				return verr, true
			}
		}
	}

	// Step 4: inter-slide #label links, last, now that every slide in the deck
	// has passed steps 1–3 and every slide label is therefore known.
	if verr, invalid := checkLinks(fsys, d, reg); invalid {
		return verr, true
	}
	return ValidationError{}, false
}

// validateSlide validates one slide file in top-to-bottom order: parse the
// frontmatter and section structure, then the slide frontmatter's fields, then
// the slide body, then each section's frontmatter fields followed by its body,
// then the notes body. It returns the first error and whether one was found.
func validateSlide(fsys fs.FS, s deck.Slide, reg *template.Registry) (ValidationError, bool) {
	src, err := fs.ReadFile(fsys, s.Path)
	if err != nil {
		return New(s.Path, 0, nil,
			fmt.Sprintf("cannot read slide: %v", err),
			"check that the slide file exists and is readable"), true
	}

	// The parser validates the frontmatter structure, the slide template, the
	// section names/fences and the section repeat limits; it is the
	// prerequisite for every later check.
	ps, err := slide.Parse(s.Path, src, reg)
	if err != nil {
		return adaptParseError(err), true
	}
	tmpl, ok := reg.Lookup(ps.Template)
	if !ok || tmpl == nil {
		// slide.Parse already reported an unknown slide template, so this is
		// unreachable; kept as a guard rather than a nil dereference.
		return New(s.Path, ps.TemplateLine, nil,
			fmt.Sprintf("unknown slide template %q", ps.Template),
			"use one of the built-in templates"), true
	}

	// Slide frontmatter fields, top of the file. The reserved `template:`
	// selector is not a field value and is removed before schema checking.
	if verr, invalid := checkFields(fsys, s.Path, ps.Frontmatter, tmpl, nil, reg); invalid {
		return verr, true
	}

	// Slide body Markdown, then the slide template's implied body rule. The
	// Markdown-subset check runs first because it is the body's well-formedness
	// check; template.CheckBody then enforces the rule the template declares
	// for the same body (body-rules). The subheading gate carries whether the
	// slide template declares child sections, so a heading at any depth is a
	// section marker there and deeper headings stay subheadings otherwise
	// (markdown-allowed-subset).
	if verr, invalid := checkBody(s.Path, ps.Body, ps.BodyLine, declaresChildSections(tmpl)); invalid {
		return verr, true
	}
	if verr, invalid := checkBodyRule(s.Path, tmpl.Body, ps.Body, ps.BodyLine); invalid {
		return verr, true
	}

	// Sections in source order, each one's frontmatter fields then its body,
	// recursing into nested section instances (slide-sections). A nested
	// instance's template declares its own body rule and its own
	// child-section state, so each body is checked against its own enclosing
	// template.
	if verr, invalid := checkSections(fsys, s.Path, ps.Sections, reg); invalid {
		return verr, true
	}

	// The reserved notes section is a body too and, being last, is checked
	// last. `# notes` is top-level only, and a deeper heading never starts
	// notes, so the notes body carries the slide body's subheading gate.
	if ps.Notes != nil {
		if verr, invalid := checkBody(s.Path, ps.Notes.Body, ps.Notes.BodyLine, declaresChildSections(tmpl)); invalid {
			return verr, true
		}
	}

	return ValidationError{}, false
}

// checkSections validates a slice of section instances in source order: each
// instance's frontmatter fields, then its body against its own template's
// implied body rule, then its children recursively (slide-sections). file is
// the slide path every error is positioned in.
func checkSections(fsys fs.FS, file string, sections []slide.Section, reg *template.Registry) (ValidationError, bool) {
	for i := range sections {
		sec := &sections[i]
		if sec.Template == "" {
			// No resolvable template: the parser already reported it, and no
			// nested instances can be checked without one. Children with a
			// template are still walked by their own parent below.
			if verr, invalid := checkSections(fsys, file, sec.Children, reg); invalid {
				return verr, true
			}
			continue
		}
		secTmpl, ok := reg.Lookup(sec.Template)
		if !ok || secTmpl == nil {
			if verr, invalid := checkSections(fsys, file, sec.Children, reg); invalid {
				return verr, true
			}
			continue
		}
		if verr, invalid := checkFields(fsys, file, sec.Frontmatter, secTmpl, nil, reg); invalid {
			return verr, true
		}
		if verr, invalid := checkBody(file, sec.Body, sec.BodyLine, declaresChildSections(secTmpl)); invalid {
			return verr, true
		}
		if verr, invalid := checkBodyRule(file, secTmpl.Body, sec.Body, sec.BodyLine); invalid {
			return verr, true
		}
		if verr, invalid := checkSections(fsys, file, sec.Children, reg); invalid {
			return verr, true
		}
	}
	return ValidationError{}, false
}

// declaresChildSections reports whether tmpl declares child sections, the
// state mdcheck.Check's subheading gate carries (markdown-allowed-subset,
// template-composition).
func declaresChildSections(tmpl *template.Template) bool {
	return tmpl != nil && len(tmpl.Sections) > 0
}

// checkFields checks one frontmatter/section data map against tmpl's field
// schema through template.CheckValues, which recurses through
// section-template-as-type values and list items at every depth with full path
// tracking. It returns the first error and whether one was found.
//
// prefix is the enclosing section instance's containment chain (for example
// []string{"columns[2]", "blocks[1]"}); when non-empty it is prepended to the
// rendered value path, so a nested instance's field error carries its full
// instance chain, e.g. `columns[2] › blocks[1] › lable`
// (nested-section-validation). The slide frontmatter call passes nil.
//
// Image fields are checked against fsys: template.CheckValues enforces the
// assets/ prefix itself and the WithImageExists callback verifies the file
// exists in the deck.
func checkFields(fsys fs.FS, file string, data map[string]any, tmpl *template.Template, prefix []string, reg *template.Registry) (ValidationError, bool) {
	res := template.CheckValues(withoutSelector(data), tmpl, reg.Lookup,
		template.WithImageExists(func(p string) bool {
			_, err := fs.Stat(fsys, p)
			return err == nil
		}))
	if len(res.Errors) == 0 {
		return ValidationError{}, false
	}
	return adaptValueError(file, res.Errors[0], prefix), true
}

// checkBody checks one Markdown body (a slide body, a section body or the notes
// body) against the accepted Markdown subset through internal/mdcheck. startLine
// is the body's 1-based line in the file, so issues are positioned absolutely.
// declaresChildSections is the enclosing template's child-section state, which
// mdcheck.Check's subheading gate carries (markdown-allowed-subset). It returns
// the first issue and whether one was found.
func checkBody(file, body string, startLine int, declaresChildSections bool) (ValidationError, bool) {
	if body == "" {
		return ValidationError{}, false
	}
	issues := mdcheck.Check(file, []byte(body), mdcheck.Options{
		Mode:                          mdcheck.BodyMode,
		StartLine:                     startLine,
		TemplateDeclaresChildSections: declaresChildSections,
	})
	if len(issues) == 0 {
		return ValidationError{}, false
	}
	return adaptIssue(issues[0]), true
}

// checkBodyRule checks one Markdown body against its template's implied `body`
// rule through template.CheckBody (body-rules): Mode required/optional/
// disallowed, the max_words/max_paragraphs/max_list_items limits and the
// subheadings allowance. startLine is the body's 1-based line in the file, passed
// through as WithBodyLine so a violation carries an absolute position (and a
// forbidden subheading is placed on its own line). It returns the first
// violation and whether one was found.
//
// CheckBody reports the same positioned template.ValueError shape CheckValues
// does, with the implied `body` path, so the violation adapts through
// adaptValueError unchanged.
func checkBodyRule(file string, rule template.BodyRule, body string, startLine int) (ValidationError, bool) {
	ve, invalid := template.CheckBody(rule, body, template.WithBodyLine(startLine))
	if !invalid {
		return ValidationError{}, false
	}
	return adaptValueError(file, ve, nil), true
}

// themeResolves reports whether name resolves in the caller's theme registry.
// deck.LoadConfig has already resolved the name against themeReg by the time
// this is called, so this is a defensive re-check rather than the primary
// enforcement point.
func themeResolves(reg *theme.Registry, name string) bool {
	if reg == nil {
		return false
	}
	_, err := reg.Lookup(name)
	return err == nil
}

// selectorKey is the reserved slide/section key that selects a template. It is
// not a field value and is stripped before schema checking, matching how
// template.Example carries its structured data.
const selectorKey = "template"

// withoutSelector returns a copy of data with the reserved `template:` selector
// removed. It never mutates the caller's map, and returns data unchanged when
// it is empty or carries no selector.
func withoutSelector(data map[string]any) map[string]any {
	if len(data) == 0 {
		return data
	}
	if _, ok := data[selectorKey]; !ok {
		return data
	}
	out := make(map[string]any, len(data)-1)
	for k, v := range data {
		if k == selectorKey {
			continue
		}
		out[k] = v
	}
	return out
}

// positionedErrorRE matches an error string of the form `file:line: message`,
// the convention internal/deck and internal/theme position their errors with.
// The file part never contains ':' for a deck-relative path.
var positionedErrorRE = regexp.MustCompile(`^([^:\n]+):([0-9]+): ([\s\S]*)$`)

// fileErrorRE matches an error string of the form `file: message` — a
// filename-only error with no line.
var fileErrorRE = regexp.MustCompile(`^([^:\n]+): ([\s\S]*)$`)

// adaptDeckError adapts a plain positioned error from internal/deck or
// internal/theme into a ValidationError. Those packages report `file:line:
// message` (or `file: message`) rather than a structured type, so the position
// is recovered from the message. An unrecognised error keeps its whole message
// as What, with no file.
func adaptDeckError(err error) ValidationError {
	if err == nil {
		return ValidationError{}
	}
	msg := err.Error()
	if m := positionedErrorRE.FindStringSubmatch(msg); m != nil {
		line, _ := strconv.Atoi(m[2])
		return New(m[1], line, nil, m[3], "")
	}
	if m := fileErrorRE.FindStringSubmatch(msg); m != nil {
		return New(m[1], 0, nil, m[2], "")
	}
	return New("", 0, nil, msg, "")
}

// adaptParseError adapts a *slide.ParseError, carrying its file, line,
// containment path and message across. The path is the rendered containment
// chain (`columns[2] › blocks[1]`) the parser builds for a nested section
// instance, or empty at the slide's top level; it becomes the ValidationError
// path so the canonical ` › ` notation is uniform at any depth
// (nested-section-validation, error-reporting). Any other error falls back to
// adaptDeckError.
func adaptParseError(err error) ValidationError {
	var pe *slide.ParseError
	if errors.As(err, &pe) {
		var path []string
		if pe.Path != "" {
			path = strings.Split(pe.Path, segmentSeparator)
		}
		return New(pe.File, pe.Line, path, pe.Msg, "")
	}
	return adaptDeckError(err)
}

// adaptIssue adapts a positioned mdcheck.Issue: its message is the "what" and
// its guidance the "fix". mdcheck has no path concept, so the path is empty.
func adaptIssue(iss mdcheck.Issue) ValidationError {
	return New(iss.File, iss.Line, nil, iss.Message, iss.Guidance)
}

// adaptValueError adapts a template.ValueError into a ValidationError: the
// caller supplies the file (ValueError carries only a path within it), the path
// segments are rendered, and What and Fix carry across. prefix, when non-empty,
// is the enclosing section instance's containment chain and is prepended to the
// rendered value path, so a nested instance's field error reads
// `columns[2] › blocks[1] › lable: …` (nested-section-validation).
func adaptValueError(file string, ve template.ValueError, prefix []string) ValidationError {
	path := valuePath(ve.Path)
	if len(prefix) > 0 {
		path = append(append([]string(nil), prefix...), path...)
	}
	return New(file, ve.Line, path, ve.What, ve.Fix)
}

// valuePath renders template.PathSegment values into the display strings
// ValidationError.Path expects, one per segment, exactly as
// template.PathString joins them: an indexed segment becomes `name[i]`. An
// index segment with no name (a list element) renders as `[i]`.
func valuePath(p []template.PathSegment) []string {
	if len(p) == 0 {
		return nil
	}
	out := make([]string, len(p))
	for i, seg := range p {
		if seg.HasIndex {
			out[i] = seg.Name + "[" + strconv.Itoa(seg.Index) + "]"
			continue
		}
		out[i] = seg.Name
	}
	return out
}
