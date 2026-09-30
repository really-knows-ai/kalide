package validate

import (
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strings"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/mdcheck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/suggest"
	"github.com/really-knows-ai/kalide/internal/template"
)

// This file implements the step-(4) inter-slide link pass (inter-slide-links),
// the last step of the fixed deterministic order defined by
// whole-deck-validation. It runs only once every slide has passed steps 1–3,
// so every slide label in the deck is known.
//
// Links come from three places, collected in slide/line order within each slide
// and in the deck's number/letter order across slides:
//
//   - slide, section and notes bodies, through mdcheck.ExtractLinks, which is
//     also the single source of the positioned link-scheme judgment for a body
//     (mdcheck.Check deliberately does not judge destinations);
//   - `link`-type field values, and links written inside inline `text`-type
//     fields, both through template.CheckValues' Result.Links — the same call
//     step 3 already uses, so link fields and inline text fields are not
//     re-parsed here.
//
// The rule is uniform for every source (inter-slide-links):
//
//   - a `#label` destination must name a slide label known in this deck; an
//     unknown label is an error carrying a closest-match "did you mean …?"
//     suggestion from internal/suggest.Closest;
//   - an http or https URL is allowed;
//   - any other destination is an error.
//
// checkLinks is fail-fast like the rest of the validator: it returns the first
// offending link in that order and whether one was found.

// checkLinks runs the inter-slide link pass over the loaded deck d. d must be
// the deck model deck.LoadSlides produced; reg must be the same populated
// registry Validate used for the structural steps. It returns the first link
// error and whether one was found.
//
// Every slide is re-parsed through internal/slide to recover the bodies and
// section structure that carry the links; this is cheap for a deck-sized input
// and keeps the pass self-contained rather than threading state out of the
// step-3 loop.
func checkLinks(fsys fs.FS, d *deck.Deck, reg *template.Registry) (ValidationError, bool) {
	if d == nil {
		return ValidationError{}, false
	}
	labels, candidates := knownLabels(d)

	for _, stack := range d.Stacks {
		ordered := make([]deck.Slide, 0, 1+len(stack.Vertical))
		ordered = append(ordered, stack.Slide)
		ordered = append(ordered, stack.Vertical...)
		for _, s := range ordered {
			src, err := fs.ReadFile(fsys, s.Path)
			if err != nil {
				return New(s.Path, 0, nil,
					fmt.Sprintf("cannot read slide: %v", err),
					"check that the slide file exists and is readable"), true
			}
			// Step 3 already parsed every slide successfully, so a parse error
			// here is unreachable; the guard keeps the pass fail-fast rather
			// than dereferencing a nil slide.
			ps, err := slide.Parse(s.Path, src, reg)
			if err != nil {
				return adaptParseError(err), true
			}
			if verr, invalid := checkSlideLinks(s.Path, ps, reg, labels, candidates); invalid {
				return verr, true
			}
		}
	}
	return ValidationError{}, false
}

// checkSlideLinks checks one parsed slide's links in the same top-to-bottom
// order the structural steps use: the slide frontmatter's fields (they sit at
// the top of the file), the slide body, then each section's frontmatter fields
// followed by its body, then the reserved notes body. It returns the first
// error and whether one was found.
func checkSlideLinks(file string, ps *slide.Slide, reg *template.Registry, labels map[string]struct{}, candidates []string) (ValidationError, bool) {
	tmpl, _ := reg.Lookup(ps.Template)

	if verr, invalid := checkFieldLinks(file, ps.Frontmatter, tmpl, reg, labels, candidates); invalid {
		return verr, true
	}
	if verr, invalid := checkBodyLinks(file, ps.Body, ps.BodyLine, labels, candidates); invalid {
		return verr, true
	}

	if verr, invalid := checkSectionLinks(file, ps.Sections, reg, labels, candidates); invalid {
		return verr, true
	}

	if ps.Notes != nil {
		if verr, invalid := checkBodyLinks(file, ps.Notes.Body, ps.Notes.BodyLine, labels, candidates); invalid {
			return verr, true
		}
	}
	return ValidationError{}, false
}

// checkSectionLinks checks the links of each section instance in source order —
// its frontmatter fields then its body — recursing into nested instances
// (slide-sections). It returns the first error and whether one was found.
func checkSectionLinks(file string, sections []slide.Section, reg *template.Registry, labels map[string]struct{}, candidates []string) (ValidationError, bool) {
	for i := range sections {
		sec := &sections[i]
		var secTmpl *template.Template
		if sec.Template != "" {
			secTmpl, _ = reg.Lookup(sec.Template)
		}
		if verr, invalid := checkFieldLinks(file, sec.Frontmatter, secTmpl, reg, labels, candidates); invalid {
			return verr, true
		}
		if verr, invalid := checkBodyLinks(file, sec.Body, sec.BodyLine, labels, candidates); invalid {
			return verr, true
		}
		if verr, invalid := checkSectionLinks(file, sec.Children, reg, labels, candidates); invalid {
			return verr, true
		}
	}
	return ValidationError{}, false
}

// checkFieldLinks resolves the `#label` targets CheckValues collected from one
// frontmatter/section data map — the targets of `link` fields and the links
// written inside inline text fields, at every nesting level with their paths.
//
// A field has no retained source line (the phase-2 parser keeps only the
// section/field structure, not per-value YAML positions), so a link error in a
// field carries the field's path but no line, exactly as the field errors of
// step 3 do. Non-http(s) field destinations never reach here: template's
// validateLink/validateText already reject them in step 3, and the deck must
// have passed step 3 for this pass to run. It returns the first error and
// whether one was found.
func checkFieldLinks(file string, data map[string]any, tmpl *template.Template, reg *template.Registry, labels map[string]struct{}, candidates []string) (ValidationError, bool) {
	if tmpl == nil || len(data) == 0 {
		return ValidationError{}, false
	}
	res := template.CheckValues(withoutSelector(data), tmpl, reg.Lookup)
	for _, lr := range res.Links {
		if verr, invalid := checkLabelRef(file, lr.Label, lr.Destination, valuePath(lr.Path), 0, labels, candidates); invalid {
			return verr, true
		}
	}
	return ValidationError{}, false
}

// checkBodyLinks collects the inline links of one Markdown body and checks them
// in source order. startLine is the body's 1-based line in the file, so a
// body link error is positioned absolutely.
//
// mdcheck.ExtractLinks reports two streams: the links themselves (a bad-scheme
// link is still a Link, with an empty Label) and a positioned Issue for every
// destination that is neither `#label` nor http(s). The two are merged by
// source position so the first offender is chosen deterministically; an Issue
// is the scheme error for its link and is checked first at the same position.
// It returns the first error and whether one was found.
func checkBodyLinks(file, body string, startLine int, labels map[string]struct{}, candidates []string) (ValidationError, bool) {
	if body == "" {
		return ValidationError{}, false
	}
	links, issues := mdcheck.ExtractLinks(file, []byte(body), mdcheck.Options{
		Mode:      mdcheck.BodyMode,
		StartLine: startLine,
	})
	for _, ev := range mergeLinkEvents(links, issues) {
		if ev.issue != nil {
			return adaptIssue(*ev.issue, nil), true
		}
		if verr, invalid := checkLabelRef(file, ev.link.Label, ev.link.Destination, nil, ev.link.Line, labels, candidates); invalid {
			return verr, true
		}
	}
	return ValidationError{}, false
}

// linkEvent is one link or link-scheme Issue from a body, placed by source
// position, so the two streams ExtractLinks returns can be walked in the
// document order the author wrote them.
type linkEvent struct {
	line  int
	col   int
	issue *mdcheck.Issue
	link  *mdcheck.Link
}

// mergeLinkEvents interleaves the links and scheme Issues of one body in
// source order. Ties on position are stable and put the Issue (the scheme
// error) before its Link.
func mergeLinkEvents(links []mdcheck.Link, issues []mdcheck.Issue) []linkEvent {
	events := make([]linkEvent, 0, len(links)+len(issues))
	for i := range links {
		events = append(events, linkEvent{line: links[i].Line, col: links[i].Col, link: &links[i]})
	}
	for i := range issues {
		events = append(events, linkEvent{line: issues[i].Line, col: issues[i].Col, issue: &issues[i]})
	}
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.line != b.line {
			return a.line < b.line
		}
		if a.col != b.col {
			return a.col < b.col
		}
		return a.issue != nil && b.link != nil
	})
	return events
}

// checkLabelRef applies the inter-slide link rule to one collected link: dest
// is the destination as written, label the `#label` target (empty for an
// http(s) URL), path the field path when the link came from a field, and line
// the file line when known (0 for a field link, which has no retained
// position). It returns the first error and whether one was found.
//
// A destination with a leading '#' is a label reference however it was
// collected; an empty target (`#`) is an error, and an unknown target is an
// error carrying the closest known slide label via internal/suggest.Closest.
// An http(s) URL is allowed. Every other destination is an error — for a body
// this is normally already reported by the merged mdcheck Issue, so the final
// branch is a guard for a link mdcheck accepted but this pass did not expect.
func checkLabelRef(file, label, dest string, path []string, line int, labels map[string]struct{}, candidates []string) (ValidationError, bool) {
	trimmed := strings.TrimSpace(dest)
	if strings.HasPrefix(trimmed, "#") {
		target := strings.TrimPrefix(trimmed, "#")
		if target == "" {
			return New(file, line, path,
				`link target "#" has an empty label`,
				"use a non-empty #label, for example #summary"), true
		}
		if _, ok := labels[target]; ok {
			return ValidationError{}, false
		}
		fix := "use the label of one of the deck's slides"
		if closest := suggest.Closest(target, candidates); closest != "" {
			fix = fmt.Sprintf("did you mean %q?", closest)
		}
		return New(file, line, path,
			fmt.Sprintf("unknown link label %q", target), fix), true
	}
	if isHTTPDestination(trimmed) {
		return ValidationError{}, false
	}
	return New(file, line, path,
		fmt.Sprintf("link destination %q is not allowed", dest),
		"use a #label link or an http(s) URL"), true
}

// knownLabels returns the deck's slide labels as a lookup set for `#label`
// resolution and as a sorted slice for closest-match suggestions. Both the
// horizontal and the vertical slides of every stack contribute a label; labels
// are case-sensitive anchor ids, so comparison is exact.
func knownLabels(d *deck.Deck) (map[string]struct{}, []string) {
	set := make(map[string]struct{})
	var names []string
	add := func(s deck.Slide) {
		if s.Label == "" {
			return
		}
		if _, ok := set[s.Label]; ok {
			return
		}
		set[s.Label] = struct{}{}
		names = append(names, s.Label)
	}
	for _, stack := range d.Stacks {
		add(stack.Slide)
		for _, v := range stack.Vertical {
			add(v)
		}
	}
	sort.Strings(names)
	return set, names
}

// isHTTPDestination reports whether dest is an http or https URL, matching how
// internal/mdcheck and internal/template classify an allowed URL destination.
func isHTTPDestination(dest string) bool {
	u, err := url.Parse(dest)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	}
	return false
}
