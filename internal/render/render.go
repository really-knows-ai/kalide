// Package render turns a parsed, validated slide into the reveal.js markup the
// deck page embeds.
//
// A slide is rendered by executing its template's Go html/template layout with
// the author's validated field and section data, and then:
//
//   - number and date fields are formatted through internal/template's format
//     catalogue (the field's `<field>_format` sibling, or its default_format);
//   - text fields are rendered as inline Markdown HTML (or passed as plain,
//     html/template-escaped text when the field is `plain`);
//   - the slide body and every section body are rendered as the accepted
//     Markdown subset to block HTML;
//   - the slide's anchor id (its deck label) is emitted on the layout's root
//     <section>;
//   - a `# notes` section is emitted as a reveal.js
//     `<aside class="notes">…</aside>` inside the slide, where the embedded
//     notes plugin reads it.
//
// Controlled effects — fragments, transitions and backgrounds — are never
// invented here: they live in the compiled-in template layouts, exactly as the
// author-facing schema forbids an author from setting them.
//
// RenderSlide consumes the PARSED, VALIDATED model (internal/slide.Slide plus
// the template registry that produced it); it never re-parses Markdown or
// re-checks a value. The deck page itself (the .reveal > .slides document) is
// RenderDeck's job, in its own unit.
//
// html/template auto-escaping stays on: the only values inserted without
// re-escaping are the ones goldmark produced from Markdown (which escapes its
// text and, by the markdown subset, can contain no raw HTML) and the trusted
// label, attribute-escaped explicitly.
package render

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	htmltmpl "html/template"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
)

// RenderError reports one slide that could not be rendered: its file identity
// (when known) and the underlying cause. It is a programming/definition failure
// rather than a deck-author error, because RenderSlide is only ever handed
// content that has already passed whole-deck validation.
type RenderError struct {
	// File is the slide's identity, as carried by slide.Slide.File. It is
	// empty for a failure that has no slide (for example a nil registry).
	File string

	// Err is the underlying cause.
	Err error
}

// Error renders the positioned failure.
func (e *RenderError) Error() string {
	if e.File == "" {
		return "render: " + e.Err.Error()
	}
	return e.File + ": " + e.Err.Error()
}

// Unwrap returns the underlying cause.
func (e *RenderError) Unwrap() error { return e.Err }

// RenderSlide renders one parsed, validated slide as a single reveal.js
// <section> fragment, ready to be placed directly inside the deck page's
// `.reveal > .slides` container.
//
// parsed is the slide's parsed structure (internal/slide.Parse); its Template
// must name a slide-usage template. label is the slide's deck label (the text
// after the dash in its filename, deck.Slide.Label), which becomes the anchor id
// on the root <section>; unique-slide-labels guarantees it is unique across the
// deck. reg is the populated template registry the slide was validated against
// — typically built from the project's templates/ library
// (template.NewRegistryFromLibrary). funcMap is the library's layout func map
// (template.LayoutFuncMap): the four helpers — `media` (bound to the served
// templates/media URL base), `section`, `dict` and `list` — plus the
// number/date format functions. RenderSlide rebinds the render-time `section`
// helper over the parse-resolvable stub LayoutFuncMap registers before any
// layout executes (section-helper). A nil funcMap falls back to
// template.LayoutFuncMap(nil, ""), under which `media` always fails as if no
// templates/media directory existed.
//
// The layout is executed with a map[string]any keyed by field name, as the
// template layouts document: text fields carry rendered inline HTML (plain
// fields carry escaped text), number and date fields carry their formatted
// display string, `body` carries the rendered Markdown body HTML, and each
// declared section name carries a slice of TRUSTED HTML in source order — one
// element per instance, each rendered bottom-up through its own
// layout.html.tmpl — so a layout splices a child with
// `{{ range .blocks }}{{ . }}{{ end }}` rather than ranging over data maps
// (template-language). The format functions and the four helpers are exposed
// to the layout through funcMap, so a library layout may format a value,
// resolve a media URL, construct a `dict`/`list` argument, or call another
// section through `section` itself (template-media, template-language,
// section-helper).
//
// The layout also carries the reserved context entries (template-context):
// `deck` is the deck-wide data from cfg — title, author, date and the author's
// properties — and `slide` is the current slide's render-time metadata: the
// string position label number and the integer total. cfg is the deck
// configuration the slide belongs to, meta is its modelled deck position
// (deck.Slide, supplying PositionLabel), and total is the deck's slide-file
// count (deck.Deck.Total). A nil cfg yields a well-formed but empty deck
// context. The reserved `item` entry is absent from the slide layout — a slide
// has no siblings — but every section instance, at every depth, executes with
// it (item-context). None of these reserved entries is a helper: the layout
// helper set is the four funcMap helpers `media`, `section`, `dict` and
// `list`. RenderSlide constructs the renderer before parsing anything and
// parseLayouts binds its renderer-backed `section` helper into the shared
// namespace, overriding the parse-resolvable stub from LayoutFuncMap, so every
// reachable layout that calls `{{ section "<name>" [fields] [body] }}` both
// parses and executes against the render-time implementation (section-helper).
// Because the slide layout is the top of the composition tree, a call it makes
// has the slide as its parent, not a section: callerFields stays nil while
// callerCtx carries the slide's deck/slide context (section-template-context).
//
// When parsed has a `# notes` section, RenderSlide appends an
// `<aside class="notes">` inside the slide with its rendered body; the embedded
// reveal.js notes plugin reads it. When it has none, no aside is emitted.
//
// A definition failure (unknown or non-slide template, missing layout, an
// unresolvable format) is returned as a *RenderError. RenderSlide is
// deterministic: the same slide always renders to the same fragment.
//
// single-binary: slide rendering stays in-process and offline — goldmark and
// html/template compiled into the binary, layouts from the in-memory registry —
// with no network, filesystem or external lookup, so the same slide renders
// OS-neutrally and identically across all six supported targets (darwin/arm64,
// darwin/amd64, windows/amd64, windows/arm64, linux/amd64, linux/arm64)
// (requirements.requirement.single-binary).
func RenderSlide(parsed *slide.Slide, label string, cfg *deck.Config, meta deck.Slide, total int, reg *template.Registry, funcMap htmltmpl.FuncMap) (htmltmpl.HTML, error) {
	if parsed == nil {
		return "", &RenderError{Err: errors.New("nil slide")}
	}
	if reg == nil {
		return "", &RenderError{File: parsed.File, Err: errors.New("nil template registry")}
	}

	slideTmpl, ok := reg.Lookup(parsed.Template)
	if !ok || slideTmpl == nil {
		return "", &RenderError{File: parsed.File, Err: fmt.Errorf("unknown slide template %q", parsed.Template)}
	}
	if slideTmpl.Usage != template.UsageSlide {
		return "", &RenderError{File: parsed.File, Err: fmt.Errorf("template %q is a %s template, not a slide template", parsed.Template, slideTmpl.Usage)}
	}

	if funcMap == nil {
		funcMap = template.LayoutFuncMap(nil, "")
	}
	// The renderer is constructed before parseLayouts because parseLayouts
	// installs its renderer-backed `section` helper on the shared namespace;
	// root is assigned from the parse result below, before any layout executes.
	r := &renderer{
		reg:     reg,
		formats: template.BuiltinFormats,
		// The slide layout executes at the top of the composition tree, so a
		// `{{ section … }}` call it makes has the slide, not a section, as its
		// parent: callerFields stays nil (zero value) while callerCtx carries
		// the slide's deck/slide context (section-template-context).
		callerCtx: &sectionCtx{cfg: cfg, meta: meta, total: total},
	}

	// The shared layout namespace is parsed before any section data is built:
	// each section instance executes its own layout.html.tmpl through it
	// (renderSection), so it must exist before slideData renders the sections
	// bottom-up into trusted HTML (template-language). parseLayouts installs
	// the renderer-backed `section` helper, so every reachable layout using
	// `{{ section … }}` parses and executes against it (section-helper).
	root, err := parseLayouts(r, slideTmpl, reg, funcMap)
	if err != nil {
		return "", &RenderError{File: parsed.File, Err: err}
	}
	r.root = root

	data, err := r.slideData(parsed, slideTmpl, cfg, meta, total)
	if err != nil {
		return "", &RenderError{File: parsed.File, Err: err}
	}

	var buf bytes.Buffer
	if err := root.ExecuteTemplate(&buf, layoutName(slideTmpl), data); err != nil {
		return "", &RenderError{File: parsed.File, Err: fmt.Errorf("execute layout: %w", err)}
	}

	out := addAnchor(buf.String(), label)
	if parsed.Notes != nil {
		notes, err := renderBody(parsed.Notes.Body)
		if err != nil {
			return "", &RenderError{File: parsed.File, Err: err}
		}
		out = addNotes(out, `<aside class="notes">`+string(notes)+`</aside>`)
	}
	return htmltmpl.HTML(out), nil
}

// renderer carries the collaborators every field conversion needs — the
// template registry (to resolve a section-template field's nested schema and a
// section instance's own template) and the number/date format catalogue — plus
// the parsed layout namespace every layout, at any composition depth, executes
// against. root is set by RenderSlide before any section data is built, so a
// section instance can execute its own layout.html.tmpl (template-language).
//
// It also carries the CALLING instance's context, so the render-time `section`
// helper — bound once into the shared layout namespace — can resolve a
// `{{ section … }}` call made while some layout executes: the calling
// instance's field values, which the target reads as `.item.parent`
// (item-context), and the deck/slide context the helper-rendered target
// template executes with (template-context). The slide layout has no section
// parent, so callerFields is nil there; callerCtx is set for the slide too,
// carrying its deck/slide context.
type renderer struct {
	reg     *template.Registry
	formats *template.Format
	root    *htmltmpl.Template

	// callerFields is the field values of the section instance whose layout is
	// currently executing — the value a `{{ section … }}` call made there
	// passes as the target's `.item.parent` (item-context). It is nil while
	// the slide layout executes, whose parent is the slide, not a section, so
	// a call made from a slide layout resolves with a nil parent.
	callerFields map[string]any

	// callerCtx is the deck/slide execution context of the instance whose
	// layout is currently executing: a `{{ section … }}` call made there
	// renders its target with the same reserved `cfg`/`meta`/`total`
	// deck/slide entries (template-context), so the helper-rendered template
	// sees the slide's deck and metadata at every composition depth. It is set
	// for the slide layout as well (with a nil parent), so a call from a slide
	// carries the slide's context.
	callerCtx *sectionCtx
}

// slideData builds the LAYOUT execution context for one slide: its converted
// field values, its rendered body, its top-level sections, the reserved
// `deck`/`slide` entries (template-context), and the slide's own reserved
// `.raw`/`.data` context (raw-source-context, section-data-context). cfg, meta
// and total are threaded from RenderSlide/RenderDeck. Each top-level section
// instance is rendered bottom-up through its own layout, deepest first, and
// grouped by its declared name as a list of trusted HTML — one element per
// instance, in source order — so the slide layout receives rendered section
// markup, not data maps, at the top level too (template-language). The layout
// map's `deck`/`slide`/`raw`/`data` entries are set directly; a top-level
// section instance's own `deck`/`slide`/`item`/`raw`/`data` entries are set by
// values/renderSection.
//
// single-binary: the slide context is built in-process with no external
// lookup — from the parsed slide, the registry and cfg only, with no
// environment, clock, filesystem or network access — so it is offline and
// OS-neutral, identical across all six supported targets (darwin/arm64,
// darwin/amd64, windows/amd64, windows/arm64, linux/amd64, linux/arm64)
// (requirements.requirement.single-binary).
func (r *renderer) slideData(s *slide.Slide, tmpl *template.Template, cfg *deck.Config, meta deck.Slide, total int) (map[string]any, error) {
	data, err := r.values(s.Frontmatter, tmpl, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.Body) != "" {
		body, err := renderBody(s.Body)
		if err != nil {
			return nil, err
		}
		data["body"] = body
	}

	// Top-level section instances are rendered through their own layouts,
	// bottom-up, and grouped by their declared name as lists of trusted HTML,
	// preserving source order, so a layout ranges over them in the order the
	// author wrote them and splices rendered markup rather than data maps
	// (template-language). Each instance executes with the reserved
	// `.deck`/`.slide`/`.item` context (template-context,
	// section-template-context, item-context); a top-level instance's
	// `.item.parent` is nil, since its parent is the slide, not a section.
	counts := make(map[string]int, len(s.Sections))
	for i := range s.Sections {
		counts[s.Sections[i].Name]++
	}
	for i := range s.Sections {
		sec := &s.Sections[i]
		if sec.Template == "" {
			continue
		}
		secTmpl, ok := r.reg.Lookup(sec.Template)
		if !ok || secTmpl == nil {
			continue
		}
		secCtx := &sectionCtx{
			cfg:   cfg,
			meta:  meta,
			total: total,
			item:  itemContext(sec.Index, counts[sec.Name], sec.Name, sec.Template, nil),
		}
		rendered, err := r.renderSection(sec, secTmpl, secCtx)
		if err != nil {
			return nil, err
		}
		list, _ := data[sec.Name].([]htmltmpl.HTML)
		data[sec.Name] = append(list, rendered)
	}

	// The reserved `deck` and `slide` entries are injected into the layout map
	// here; the layout has no sibling place, so it carries no `item`. Each
	// section instance's map carries its own `deck`/`slide`/`item` copy, set
	// by values/renderSection. A declared field or section named deck, slide,
	// item, raw or data is rejected at load time, so none can collide with a
	// field value.
	data["deck"] = deckContext(cfg)
	data["slide"] = slideContext(meta, total)

	// The reserved `.raw` entry is the source view of the slide's OWN authored
	// frontmatter and body (raw-source-context), alongside the converted field
	// values and rendered body the same map already carries under its own
	// names; the reserved `.data` entry is the slide's authored top-level
	// sections as data, one entry per authored instance, parented on the slide
	// (nil) (section-data-context). Both reflect AUTHORED instances only: a
	// `{{ section … }}` call is a direct render call, never part of the parsed
	// tree, so its sectionHelper render contributes no entry to `.data` and no
	// element to the rendered-HTML section list built above.
	data["raw"] = r.rawContext(s.Frontmatter, tmpl, s.Body)
	data["data"] = r.dataContext(s.Sections, nil)
	return data, nil
}

// renderSection renders one section instance through its own layout.html.tmpl,
// deepest first: it first renders every child section instance (each through
// its own layout, recursively) and attaches them to its execution context as
// lists of trusted HTML — one element per child instance, in source order — so
// a parent layout splices each child's rendered markup with
// `{{ range .blocks }}{{ . }}{{ end }}` rather than ranging over data maps
// (template-language). The returned HTML is what the parent receives, so
// section markup composes bottom-up at every depth and at the top level.
//
// secCtx is the instance's execution context. Its own reserved `.raw` context —
// the source view of its authored frontmatter and body — and `.data` context —
// its authored child sections as data, each child's `.item.parent` being this
// instance's source field values, as dataContext's parent rule requires — are
// built here and merged into the instance map by values, next to the converted
// field values and the reserved `deck`/`slide`/`item` entries, at every
// composition depth (raw-source-context, section-data-context). Its body is
// rendered, and its resolved template's layout runs in the shared namespace
// (renderer.root) with `deck`/`slide`/`item`/`raw`/`data` plus the instance's
// own fields. A child's rendered `.item.parent` is this instance's field values
// (item-context); at the top level the parent is the slide, so the descriptor
// carries nil.
//
// The instance's field values are also published as the renderer's caller
// context for the duration of its layout execution and restored afterwards, so
// a `{{ section … }}` call the layout makes resolves its target's `.item.parent`
// from this instance and inherits its deck/slide context (item-context,
// template-context). A call made from a slide layout sees a nil parent, since
// RenderSlide publishes the slide context with no calling instance.
func (r *renderer) renderSection(sec *slide.Section, tmpl *template.Template, secCtx *sectionCtx) (htmltmpl.HTML, error) {
	// Every section instance, at every composition depth, carries its own
	// reserved `.raw` (its authored source field values and body) and `.data`
	// (its authored children as data, parented on its own source field values)
	// next to its converted field values and `deck`/`slide`/`item`. They are
	// built before values so values merges them into the instance map.
	secCtx.raw = r.rawContext(sec.Frontmatter, tmpl, sec.Body)
	secCtx.data = r.dataContext(sec.Children, secCtx.raw)

	data, err := r.values(sec.Frontmatter, tmpl, secCtx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(sec.Body) != "" {
		body, err := renderBody(sec.Body)
		if err != nil {
			return "", err
		}
		data["body"] = body
	}

	// Sibling counts are per declared section name, matching slide.Section's
	// Index, so the descriptor's index/number/count/first/last describe the
	// instance's place among its same-name siblings under this parent.
	counts := make(map[string]int, len(sec.Children))
	for i := range sec.Children {
		counts[sec.Children[i].Name]++
	}
	for i := range sec.Children {
		child := &sec.Children[i]
		if child.Template == "" {
			continue
		}
		childTmpl, ok := r.reg.Lookup(child.Template)
		if !ok || childTmpl == nil {
			continue
		}
		childCtx := &sectionCtx{
			cfg:    secCtx.cfg,
			meta:   secCtx.meta,
			total:  secCtx.total,
			item:   itemContext(child.Index, counts[child.Name], child.Name, child.Template, data),
			parent: data,
		}
		rendered, err := r.renderSection(child, childTmpl, childCtx)
		if err != nil {
			return "", err
		}
		list, _ := data[child.Name].([]htmltmpl.HTML)
		data[child.Name] = append(list, rendered)
	}

	// Publish this instance as the caller context for the duration of its
	// layout execution: a nested `{{ section … }}` call the layout makes reads
	// these field values as its target's `.item.parent` and inherits this
	// instance's deck/slide context (item-context, template-context). The
	// previous context is restored once the layout returns, so an enclosing
	// layout keeps resolving against itself.
	prevFields, prevCtx := r.callerFields, r.callerCtx
	r.callerFields = data
	r.callerCtx = secCtx
	defer func() {
		r.callerFields = prevFields
		r.callerCtx = prevCtx
	}()

	var buf bytes.Buffer
	if err := r.root.ExecuteTemplate(&buf, layoutName(tmpl), data); err != nil {
		return "", fmt.Errorf("execute layout: %w", err)
	}
	return htmltmpl.HTML(buf.String()), nil
}

// sectionHelper is the render-time `section` layout helper (section-helper):
// it executes a target section template as a one-item group, exactly as
// renderSection executes one authored section instance, but for a call written
// in a layout rather than an instance present in the parsed tree. It is bound
// into the shared layout function map, so a
// `{{ section "<name>" [fields] [body] }}` call parses and executes against it
// instead of the parse-resolvable stub (RenderSlide/parseLayouts).
//
// The variadic arguments mirror the load-time exampleSectionHelper
// (internal/template): at most a fields map and a body string, each optional;
// a fields argument that is not a map or a body argument that is not a string
// is a programming error. Resolution and validation are delegated in full to
// template.ResolveSectionCall — the target must be a defined section-usage
// template, the supplied fields are defaults-first validated against the
// target's schema, and a present body is validated against the target's body
// rule — so the render-time helper reuses the phase-02 resolution rather than
// duplicating it. The caller's own field values (renderer.callerFields) are
// passed as the call's parent, so the target's one-item `.item.parent` is the
// calling instance's fields; it is nil when the call is made from a slide
// layout, whose parent is the slide (item-context).
//
// The effective values are converted with values exactly as renderSection
// converts an instance's: a text field becomes inline HTML, a number or date is
// formatted, and the reserved `deck`/`slide`/`item` entries are merged in with
// `.item` the one-item descriptor ResolveSectionCall built. The call's body,
// when present, is rendered to block HTML and exposed as `body`
// (template-language). The reserved `.raw` entry is the source view of what the
// call supplied — the supplied fields plus the body — and `.data` is empty
// because the helper passes no child sections, so a call adds no entry to the
// caller's `.data` and no element to its rendered section list
// (raw-source-context, section-data-context).
//
// Before the target layout executes, the renderer's caller context is set to
// this instance — its effective field values become a nested call's
// `.item.parent`, and its deck/slide context is inherited — and restored
// afterwards, so a `{{ section … }}` call the target's own layout makes
// resolves against this instance (item-context, template-context). The target's
// layout runs through the shared namespace (renderer.root), so it is a
// layout already parsed with this same helper bound, not a fresh parse. The
// result is the target's trusted HTML, which the calling layout splices
// unescaped.
func (r *renderer) sectionHelper(name string, args ...any) (htmltmpl.HTML, error) {
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

	// ResolveSectionCall applies the target's defaults first, validates the
	// effective fields with CheckValues, checks the body against the target's
	// body rule, and builds the one-item .item descriptor whose parent is the
	// calling instance's fields (nil from a slide layout).
	call, err := template.ResolveSectionCall(r.reg.Lookup, name, fields, body, r.callerFields)
	if err != nil {
		return "", fmt.Errorf("section helper %q: %w", name, err)
	}

	// The target is the sole instance of its own one-item group, so it
	// executes with the calling slide's deck/slide context and its own .item
	// descriptor; it has no authored children of its own to render. Its
	// reserved `.raw`/`.data` context (raw-source-context,
	// section-data-context) is supplied to values and merged into the instance
	// map there, exactly as a rendered instance's is: `.raw` is the source view
	// of what the call supplied — the supplied fields before defaults are
	// applied, so a default is never mistaken for authored source, plus the
	// body — and `.data` is empty, because the helper passes no child sections
	// and so contributes no authored instance to the data view.
	caller := r.callerCtx
	if caller == nil {
		caller = &sectionCtx{}
	}
	secCtx := &sectionCtx{
		cfg:   caller.cfg,
		meta:  caller.meta,
		total: caller.total,
		item:  call.Item,
		raw:   r.rawContext(fields, call.Template, call.Body),
		data:  map[string]any{},
	}
	data, err := r.values(call.Values, call.Template, secCtx)
	if err != nil {
		return "", fmt.Errorf("section helper %q: %w", name, err)
	}
	if strings.TrimSpace(call.Body) != "" {
		rendered, err := renderBody(call.Body)
		if err != nil {
			return "", fmt.Errorf("section helper %q: %w", name, err)
		}
		data["body"] = rendered
	}

	// A nested {{ section … }} call in the target's layout reads this
	// instance's field values as its .item.parent and inherits the same
	// deck/slide context. Restore the previous context once it returns, so
	// the caller's own layout keeps resolving against itself.
	prevFields, prevCtx := r.callerFields, r.callerCtx
	r.callerFields = call.Values
	r.callerCtx = secCtx
	defer func() {
		r.callerFields = prevFields
		r.callerCtx = prevCtx
	}()

	var buf bytes.Buffer
	if err := r.root.ExecuteTemplate(&buf, layoutName(call.Template), data); err != nil {
		return "", fmt.Errorf("section helper %q: execute target layout: %w", name, err)
	}
	return htmltmpl.HTML(buf.String()), nil
}

// deckContext builds the reserved `deck` execution context
// (deck-data-in-templates): the deck-wide config values under title, author
// and date, plus the author-declared properties under properties. title is
// always present; author and date are empty strings when omitted; properties is
// always a non-nil map. Values keep the type they had in the config, so a
// string property is escaped text via html/template while a number, boolean or
// date stays typed — nothing here is Markdown-rendered or pre-rendered as safe
// HTML (deck-properties). A nil cfg yields a well-formed but empty context.
func deckContext(cfg *deck.Config) map[string]any {
	title, author, date := "", "", ""
	properties := map[string]any{}
	if cfg != nil {
		title, author, date = cfg.Title, cfg.Author, cfg.Date
		if cfg.Properties != nil {
			properties = cfg.Properties
		}
	}
	return map[string]any{
		"title":      title,
		"author":     author,
		"date":       date,
		"properties": properties,
	}
}

// slideContext builds the reserved `slide` execution context (slide-metadata):
// number is the STRING position label of s — the number with the lower-cased
// letter appended only for a vertical slide (deck.Slide.PositionLabel) — and
// total is the integer number of slide files in the deck, derived at render
// time (deck.Deck.Total, threaded in by RenderDeck). Neither value is authored
// frontmatter and number is never a flattened index or bare filename integer.
func slideContext(s deck.Slide, total int) map[string]any {
	return map[string]any{
		"number": s.PositionLabel(),
		"total":  total,
	}
}

// itemContext builds the reserved `.item` descriptor a section instance is
// executed with (item-context): the instance's zero-based `index` and one-based
// `number` among the sibling instances of the same declared section name, the
// `count` of those siblings, whether it is `first` or `last`, the declared
// `section` name, the resolved `template` name, and `parent` — the enclosing
// section instance's field values, or nil at the top level. A slide layout has
// no sibling place, so no descriptor is built for it and its execution context
// carries no `item` entry (template-context, section-template-context).
func itemContext(index, count int, section, tmpl string, parent map[string]any) map[string]any {
	return map[string]any{
		"index":    index,
		"number":   index + 1,
		"count":    count,
		"first":    index == 0,
		"last":     index == count-1,
		"section":  section,
		"template": tmpl,
		"parent":   parent,
	}
}

// sectionCtx carries the reserved `.deck`/`.slide`/`.item` context
// (template-context, section-template-context, item-context) threaded into
// values/fieldValue when they are constructing a SECTION INSTANCE — top-level
// (slideData), nested through a section-template-as-type field, or a list item
// of that type. A nil sectionCtx means the map under construction is the slide
// LAYOUT map, which already carries its own `deck`/`slide` entries (slideData)
// and must not gain a second, redundant pair here.
type sectionCtx struct {
	cfg   *deck.Config
	meta  deck.Slide
	total int

	// item is the reserved `.item` descriptor for the instance whose map is
	// under construction (item-context), or nil when the map is not a section
	// instance: the slide LAYOUT map and a section-template-as-type field
	// value are data-only and carry no sibling descriptor.
	item map[string]any

	// parent is the enclosing section instance's field values, the value a
	// nested instance's descriptor exposes as `.item.parent`
	// (item-context). It is nil at the top level, where the parent is the
	// slide, not a section instance.
	parent map[string]any

	// raw is the instance's reserved `.raw` context (raw-source-context): the
	// source view of the author's original values, built by the caller with
	// rawContext. It is nil when the map under construction is not a section
	// instance — the slide LAYOUT map (whose `raw` slideData sets directly) and
	// a section-template-as-type field value (data-only, like its missing
	// `.item`) — and values then adds no `raw` entry.
	raw map[string]any

	// data is the instance's reserved `.data` context
	// (section-data-context): the instance's authored child sections as data,
	// built by the caller with dataContext. It is nil when the map under
	// construction is not a section instance, on the same terms as raw, and
	// values then adds no `data` entry. A section instance with no authored
	// children carries a non-nil but empty map, so a section template always
	// sees `.data` at every depth.
	data map[string]any
}

// values converts one map of validated field data against t's field schema,
// returning the layout-ready map. It is used for a slide's frontmatter, a
// section instance's frontmatter, and a nested section-template-as-type value,
// so the same rules apply at every depth. The reserved `template:` selector and
// any unknown key are simply absent from the field schema and are ignored here;
// validation already rejected an unknown key.
//
// secCtx is non-nil exactly when out is a SECTION INSTANCE map (top-level or
// nested): the reserved `deck`/`slide` entries are merged into it and, when
// the instance has a sibling descriptor, the reserved `item` entry too
// (template-context, section-template-context, item-context). When the caller
// supplied them, the reserved `raw` (the instance's source values,
// raw-source-context) and `data` (its authored child sections as data,
// section-data-context) entries are merged in as well, next to `deck`/`slide`/
// `item`, so a section template sees the full template context at every depth
// while its declared fields stay directly addressable under their own names.
// The slide-frontmatter call passes nil, since that map is the slide LAYOUT
// map and slideData sets `deck`/`slide` (and the layout's own `raw`/`data`)
// on it directly; a slide layout has no siblings, so it never gains an `item`
// entry. A section-template-as-type field value keeps `deck`/`slide` but is
// data-only and carries no `item`, `raw` or `data`.
func (r *renderer) values(data map[string]any, t *template.Template, secCtx *sectionCtx) (map[string]any, error) {
	out := make(map[string]any, len(t.Fields))
	for i := range t.Fields {
		f := &t.Fields[i]
		raw, present := data[f.Name]
		if !present {
			if f.Default == nil {
				continue
			}
			raw = f.Default
		}
		v, err := r.fieldValue(f, raw, data, secCtx)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Name, err)
		}
		out[f.Name] = v
	}
	if secCtx != nil {
		out["deck"] = deckContext(secCtx.cfg)
		out["slide"] = slideContext(secCtx.meta, secCtx.total)
		if secCtx.item != nil {
			out["item"] = secCtx.item
		}
		// The reserved `.raw`/`.data` entries are merged next to `deck`/`slide`/
		// `item` when the caller built them for this section instance. They are
		// absent — not nil — for a data-only section-template-as-type value and
		// for the slide layout, so nothing renders as `<no value>`.
		if secCtx.raw != nil {
			out["raw"] = secCtx.raw
		}
		if secCtx.data != nil {
			out["data"] = secCtx.data
		}
	}
	return out, nil
}

// rawContext builds the reserved `.raw` execution-context entry for a slide or
// section instance (raw-source-context): the author's ORIGINAL source values,
// alongside the converted ones the same map carries under the field names. It
// holds the source value of every declared field the author supplied — for a
// text field, the Markdown exactly as written, before inline-Markdown
// rendering — keyed by the field's own name, and the instance's original body
// source under `body` when the author supplied one. The converted values stay
// directly addressable under their own names; `.raw` only adds this source
// view, and it is data, not a helper, so exposing it adds nothing to the layout
// function set (template-context).
//
// data is the instance's pre-conversion source map — a slide's or section's
// decoded frontmatter, or a section helper call's supplied fields — and t is
// the resolved template whose field schema names the declared fields. Only
// declared fields are copied: the reserved `template:` selector, a
// `<field>_format` sibling and any other undeclared key are not fields. A field
// the author did not supply is absent, and a field's declared default is not
// copied, because a default is not an authored source value; a body the author
// did not supply is likewise absent. A nil t or data yields a well-formed
// (possibly body-only) context, so a caller may pass them freely.
func (r *renderer) rawContext(data map[string]any, t *template.Template, body string) map[string]any {
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

// dataContext builds the reserved `.data` execution-context entry for a slide's
// or section instance's authored child sections (section-data-context): the same
// authored instances the rendered-HTML list groups under each declared section
// name, exposed in parallel as DATA. The result is keyed by declared section
// name; each key maps to a source-ordered list with one entry per authored
// instance of that section, and each entry is a map carrying:
//
//   - `raw`: the instance's original source field values and body source
//     (rawContext) — the source view of its fields, alongside the converted
//     values the rendered instance exposes under their own names;
//   - `item`: the instance's sibling descriptor (itemContext) — its zero-based
//     `index` and one-based `number` among the same-name siblings, their
//     `count`, `first`/`last`, the declared `section` name, the resolved
//     `template` name, and `parent` — the enclosing instance's source field
//     values, or nil for a top-level section whose parent is the slide;
//   - `data`: the instance's own child sections as data, keyed and shaped the
//     same way (the recursion), so the whole authored section tree is
//     addressable as data.
//
// sections is one parent's authored child sections, in source order, exactly as
// the parsed slide tree carries them (slide.Slide.Sections or
// slide.Section.Children); the walk mirrors slideData/renderSection's instance
// model — siblings grouped by declared name and counted per name, each
// instance's template resolved from the registry. parent is the enclosing
// instance's source field values (the same map exposed as that instance's
// `.raw`), or nil for a slide's top-level sections; a nested instance's own
// children read its `.raw` as their parent, so `.item.parent` in the data view
// is the enclosing instance's source fields.
//
// Only authored instances appear: a section invoked by the `section` helper is a
// direct render call, never part of the parsed tree, so it is absent from
// `.data` exactly as it is absent from the rendered-HTML list. `.data` is data,
// not a helper, so exposing it adds nothing to the layout function set
// (template-context, section-data-context).
func (r *renderer) dataContext(sections []slide.Section, parent map[string]any) map[string]any {
	out := make(map[string]any, len(sections))
	counts := make(map[string]int, len(sections))
	for i := range sections {
		counts[sections[i].Name]++
	}
	for i := range sections {
		sec := &sections[i]
		tmpl, _ := r.reg.Lookup(sec.Template)
		raw := r.rawContext(sec.Frontmatter, tmpl, sec.Body)
		entry := map[string]any{
			"raw":  raw,
			"item": itemContext(sec.Index, counts[sec.Name], sec.Name, sec.Template, parent),
			"data": r.dataContext(sec.Children, raw),
		}
		list, _ := out[sec.Name].([]map[string]any)
		out[sec.Name] = append(list, entry)
	}
	return out
}

// fieldValue converts one validated field value to its layout representation.
// data is the mapping the value came from, so a number or date field can find
// its `<field>_format` sibling; it may be nil for a list element, which carries
// no selector of its own. secCtx carries the reserved `.deck`/`.slide` context
// through a section-template-as-type field's nested construction and a list
// item's recursion, so a nested section-template-as-type value (and each item
// of a list of that type) is built with the same slide context as the section
// instance it lives in, at every composition depth (section-template-context).
// Such a value is data-only and its layout is never executed, so it carries no
// `.item` descriptor (template-language).
func (r *renderer) fieldValue(f *template.Field, raw any, data map[string]any, secCtx *sectionCtx) (any, error) {
	switch f.Type {
	case template.FieldText:
		s, ok := raw.(string)
		if !ok {
			// A non-string value (already rejected by validation) is left as
			// text for html/template to escape.
			return raw, nil
		}
		if f.Plain {
			// A plain field strips Markdown styles silently and is rendered as
			// escaped text, so "**bold**" reads "bold" rather than showing its
			// markers.
			return plainText(s), nil
		}
		return renderInline(s), nil

	case template.FieldNumber, template.FieldDate:
		return r.formatted(f, raw, data)

	case template.FieldList:
		items, ok := asSlice(raw)
		if !ok || f.Item == nil {
			return raw, nil
		}
		out := make([]any, len(items))
		for i, elem := range items {
			v, err := r.fieldValue(f.Item, elem, nil, secCtx)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil

	case template.FieldSectionTemplate:
		nested, ok := r.reg.Lookup(f.SectionTemplate)
		if !ok || nested == nil {
			return raw, nil
		}
		m, ok := asMap(raw)
		if !ok {
			return raw, nil
		}
		// A section template used as a field type stays data-only: its value
		// is a map of typed field values, never HTML, and its layout is
		// never executed. It keeps the instance's reserved `.deck`/`.slide`
		// context (a section-template-as-type value is still a section
		// template's data) but not `.item`: a field value is not an instance
		// among sibling sections (template-language, section-template-context).
		var nestedCtx *sectionCtx
		if secCtx != nil {
			nestedCtx = &sectionCtx{cfg: secCtx.cfg, meta: secCtx.meta, total: secCtx.total}
		}
		return r.values(m, nested, nestedCtx)

	default:
		// boolean, enum, link and image values render as written.
		return raw, nil
	}
}

// formatted renders a number or date field through the format selected by its
// `<field>_format` sibling, or the field's default_format, using the same
// catalogue the schema checker validated against. With no effective format a
// number is rendered without a trailing ".0" and a date is normalised to
// YYYY-MM-DD, so a layout never prints a raw time.Time.
func (r *renderer) formatted(f *template.Field, raw any, data map[string]any) (any, error) {
	chosen := ""
	if data != nil {
		chosen, _ = data[f.Name+"_format"].(string)
	}
	fn, err := r.formats.Resolve(f, chosen)
	if err != nil {
		return nil, err
	}
	if fn == nil {
		switch f.Type {
		case template.FieldDate:
			return dateString(raw), nil
		case template.FieldNumber:
			return numberString(raw), nil
		}
		return raw, nil
	}
	return fn(raw)
}

// numberString renders a number value without a trailing ".0" for whole
// values; a non-numeric value (already rejected by validation) is passed
// through.
func numberString(raw any) any {
	switch v := raw.(type) {
	case float64:
		return formatNumber(v)
	case float32:
		return formatNumber(float64(v))
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return raw
	}
}

// formatNumber renders a whole float without a decimal point.
func formatNumber(n float64) string {
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// dateString normalises a date value to YYYY-MM-DD when no named format
// applies. Validation accepts a time.Time or a YYYY-MM-DD string.
func dateString(raw any) string {
	switch v := raw.(type) {
	case time.Time:
		return v.Format("2006-01-02")
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// parseLayouts parses the slide template and every layout reachable from it —
// declared section templates and section-template-as-type fields, at every
// depth — into one html/template namespace keyed by layout name, so a composed
// layout can invoke another with `{{ template "<name>" . }}`. It returns that
// shared namespace: RenderSlide executes the slide layout through it and hands
// it to the renderer so every section instance, at any depth, executes its own
// layout.html.tmpl against the same namespace (template-language). funcMap is
// the library's layout func map (template.LayoutFuncMap): the helpers `media`,
// `section`, `dict` and `list` plus the number/date format functions, installed
// on the namespace so every library layout using a helper or a format function
// parses and executes (template-media, template-language).
//
// r's renderer-backed `section` helper is installed on the namespace here, at
// the per-template func-map binding point, overriding the parse-resolvable stub
// template.LayoutFuncMap registers (RenderSlide/parseLayouts): every reachable
// layout that calls `{{ section "<name>" [fields] [body] }}` therefore parses
// and, once the namespace executes, resolves against the render-time
// implementation rather than the stub (section-helper). The caller's funcMap is
// copied before the override, so binding a renderer never mutates a map the
// caller reuses across renders.
//
// Parse time is unaffected by the reserved execution context
// (`.deck.properties`, `.slide`, `.item`): those keys are injected only when
// the parsed layout is later executed (see slideData / renderSection / values
// / fieldValue), not while its text is being parsed here.
//
// single-binary: layout parsing uses only the in-process html/template engine:
// layouts come from the registry's already-loaded Layout.Text (project library
// / embedded sources), nothing is fetched, and namespace keys are layout names,
// not OS paths, so no separator handling is involved; parsing is offline and
// OS-neutral, identical across all six supported targets (darwin/arm64,
// darwin/amd64, windows/amd64, windows/arm64, linux/amd64, linux/arm64)
// (requirements.requirement.single-binary).
func parseLayouts(r *renderer, slideTmpl *template.Template, reg *template.Registry, funcMap htmltmpl.FuncMap) (*htmltmpl.Template, error) {
	// Bind the renderer-backed `section` helper into the namespace's func map,
	// overriding the parse-resolvable stub. The map is copied so a reused
	// caller map is never mutated with a method value that captures one
	// renderer. The namespace carries its own name only so html/template has a
	// handle; it deliberately differs from every layout name. Naming the
	// namespace after the slide's own layout would make
	// namespace.New(layoutName(slideTmpl)) shadow that layout with an empty
	// associated template, so executing the slide layout would fail with "is
	// an incomplete template".
	layoutFuncs := make(htmltmpl.FuncMap, len(funcMap)+1)
	for name, fn := range funcMap {
		layoutFuncs[name] = fn
	}
	layoutFuncs["section"] = r.sectionHelper
	namespace := htmltmpl.New("layouts").Funcs(layoutFuncs)
	for _, t := range reachableTemplates(slideTmpl, reg) {
		if t.Layout.Text == "" {
			return nil, fmt.Errorf("template %q has no layout text", t.Name)
		}
		if _, err := namespace.New(layoutName(t)).Parse(t.Layout.Text); err != nil {
			return nil, fmt.Errorf("template %q: parse layout: %w", t.Name, err)
		}
	}
	return namespace, nil
}

// reachableTemplates returns tmpl followed by every template reachable from it
// through declared sections, section-template-as-type fields and section-helper
// call edges, each once, in declaration order. The section recursion is at every
// depth: a child section template declared by another section template is
// reached through that template's own Sections, so every nested child section
// template is included and parsed into the shared namespace and can later
// execute its own layout (template-language, section-template-context).
//
// A layout's `{{ section "name" … }}` calls are followed too, after the
// declared-section and field walk: template.NewSection(reg.Lookup).HelperRefs
// re-parses each reached layout with the canonical layout func map and returns
// its helper-call targets, so a helper-only target — one that is not also a
// declared child of its caller — is still pulled into the shared namespace and
// the render-time helper can execute it. Helper edges are followed transitively
// to the same closure as declared edges, bounded by seen. Missing names are
// skipped: validation has already rejected an undefined name, and a layout
// simply cannot invoke a template that does not exist.
//
// A HelperRefs error is deliberately not propagated. reachableTemplates cannot
// return one, load already validated every layout, and the only way HelperRefs
// can fail is a layout that does not re-parse with the canonical func map —
// which is exactly what parseLayouts' own Parse reports for the same text, so
// skipping the edges here loses no error that the caller does not already
// surface.
func reachableTemplates(root *template.Template, reg *template.Registry) []*template.Template {
	seen := make(map[string]bool)
	var out []*template.Template

	// One Section bound to the registry's resolver serves every layout; it
	// holds no per-call state, only the resolver.
	section := template.NewSection(reg.Lookup)

	var add func(t *template.Template)
	add = func(t *template.Template) {
		if t == nil || seen[t.Name] {
			return
		}
		seen[t.Name] = true
		out = append(out, t)
		for i := range t.Sections {
			for _, name := range t.Sections[i].Accepted {
				if st, ok := reg.Lookup(name); ok && st != nil {
					add(st)
				}
			}
		}
		for i := range t.Fields {
			addFieldTemplate(&t.Fields[i], reg, add)
		}
		if refs, err := section.HelperRefs(t); err == nil {
			for _, name := range refs {
				if st, ok := reg.Lookup(name); ok && st != nil {
					add(st)
				}
			}
		}
	}
	add(root)
	return out
}

// addFieldTemplate follows a field's section-template reference, recursing
// through a list's single item type.
func addFieldTemplate(f *template.Field, reg *template.Registry, add func(*template.Template)) {
	switch f.Type {
	case template.FieldSectionTemplate:
		if st, ok := reg.Lookup(f.SectionTemplate); ok {
			add(st)
		}
	case template.FieldList:
		if f.Item != nil {
			addFieldTemplate(f.Item, reg, add)
		}
	}
}

// layoutName is the name a template's layout is parsed and executed under,
// falling back to the template name when the content loader did not set one.
func layoutName(t *template.Template) string {
	if t.Layout.Name != "" {
		return t.Layout.Name
	}
	return t.Name
}

// inlineParagraphTags are the paragraph open/close tags goldmark emits around
// an inline-mode text field. mdcheck validates a text field as exactly one
// paragraph, so removing that wrapper — and only that wrapper — yields the
// field's inline HTML.
const (
	inlineOpenTag  = "<p>"
	inlineCloseTag = "</p>\n"
)

// renderInline renders an inline-Markdown text field to HTML without the
// paragraph wrapper goldmark adds around a block. Text fields are validated in
// mdcheck's inline mode, so the single paragraph is the parser's wrapper, not
// author content. goldmark escapes text and code spans, and the accepted subset
// admits no raw HTML, so the result is safe to inject unescaped.
func renderInline(src string) htmltmpl.HTML {
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := goldmark.New().Convert([]byte(src), &buf); err != nil {
		// Convert only fails when the destination writer fails; a bytes.Buffer
		// never does, so this is unreachable and the raw source is a safe
		// fallback for html/template to escape.
		return htmltmpl.HTML(src)
	}
	out := buf.String()
	if strings.HasPrefix(out, inlineOpenTag) && strings.HasSuffix(out, inlineCloseTag) {
		out = out[len(inlineOpenTag) : len(out)-len(inlineCloseTag)]
	}
	return htmltmpl.HTML(out)
}

// plainText returns an inline-Markdown field's visible text with all styles
// removed, for a `plain` field: "**bold**" becomes "bold". goldmark's Text and
// String nodes carry the visible text while every style wrapper (emphasis,
// code span, link) contributes only its markers, which are omitted here.
// html/template escapes the result on output.
func plainText(src string) string {
	if src == "" {
		return ""
	}
	source := []byte(src)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	var b strings.Builder
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		writeNodeText(&b, n, source)
	}
	return b.String()
}

// writeNodeText appends the visible text of n and its descendants. goldmark's
// Text nodes carry source segments, String and CodeSpan nodes carry literal
// text. Every other node is a style wrapper whose markers are intentionally
// omitted.
func writeNodeText(b *strings.Builder, n ast.Node, src []byte) {
	switch t := n.(type) {
	case *ast.Text:
		b.Write(t.Segment.Value(src))
		if t.SoftLineBreak() || t.HardLineBreak() {
			b.WriteByte(' ')
		}
	case *ast.String:
		b.Write(t.Value)
	case *ast.CodeSpan:
		// An inline code span stores its text directly and is treated as a
		// leaf, so its text is not written twice.
		b.Write(t.Text(src))
		return
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		writeNodeText(b, c, src)
	}
}

// renderBody renders a body — a slide body, a section body or the notes body —
// as the accepted Markdown subset to block HTML, using goldmark's CommonMark
// core with no extensions, exactly as internal/mdcheck validated it.
func renderBody(src string) (htmltmpl.HTML, error) {
	if strings.TrimSpace(src) == "" {
		return "", nil
	}
	var buf bytes.Buffer
	if err := goldmark.New().Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return htmltmpl.HTML(buf.String()), nil
}

// addAnchor emits the slide's anchor id on the layout's root <section>, which
// is the reveal.js slide element. The label is attribute-escaped even though
// the deck filename grammar only admits [A-Za-z0-9_-]. When a layout emits no
// <section> at all, the fragment is wrapped in one, so the slide still has a
// stable anchor.
func addAnchor(fragment, label string) string {
	if label == "" {
		return fragment
	}
	attr := ` id="` + html.EscapeString(label) + `"`
	i := strings.Index(fragment, "<section")
	if i < 0 || !tagBoundary(fragment, i+len("<section")) {
		return "<section" + attr + ">" + fragment + "</section>"
	}
	j := i + len("<section")
	return fragment[:j] + attr + fragment[j:]
}

// addNotes inserts the speaker-notes aside inside the slide element, before its
// closing </section>, so the reveal.js notes plugin finds it as a child of the
// current slide. With no closing </section> the aside is appended.
func addNotes(fragment, notes string) string {
	if notes == "" {
		return fragment
	}
	i := strings.LastIndex(fragment, "</section>")
	if i < 0 {
		return fragment + notes
	}
	return fragment[:i] + notes + fragment[i:]
}

// tagBoundary reports whether a "<section" occurrence is a real tag opening,
// rather than the prefix of some other name such as "<sectional>".
func tagBoundary(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	switch s[i] {
	case ' ', '\t', '\n', '\r', '>', '/':
		return true
	}
	return false
}

// asSlice returns v as a []any for any slice or array kind, so YAML-decoded
// sequences render whatever their element type.
func asSlice(v any) ([]any, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// asMap returns v as a map[string]any, accepting the string-keyed and
// interface-keyed maps a YAML decoder may produce.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	}
	return nil, false
}
