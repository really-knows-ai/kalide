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

// mediaURLPrefix is the served media URL base of a library's
// single/primary layer: a `{{ media "<path>" }}` reference in a layout owned by
// that layer resolves to mediaURLPrefix + "<path>" (joining with a single "/",
// since it is a URL, not an OS path). It must match internal/server.MediaPath
// exactly — the route that serves templates/media/** — but the two packages
// cannot share the constant directly, since internal/server imports
// internal/render; the value is mirrored here and documented on both sides
// (internal/server/media.go carries the agreement note). The trailing slash is
// kept so the literal mirrors server.MediaPath; MediaFunc trims it before
// joining, so either form yields the same URL.
const mediaURLPrefix = "/assets/templates/media/"

// externalMediaURLPrefix is the served media URL base of a TWO-layer deck's
// external `templates:` layer (LibraryLayerRole LayerRoleExternal): a
// `{{ media "<path>" }}` reference in a layout owned by that layer resolves to
// externalMediaURLPrefix + "<path>". The layer is identified by the
// `external` path segment; there is no index and no query tag. The deck-root
// local templates/ layer of the same two-layer deck keeps mediaURLPrefix, and a
// single-layer library keeps it too, so each layer's media is addressed by its
// own prefix rather than a merged namespace (layered-template-library,
// library-layer-references). internal/server mounts the matching external route
// in phase 4; until then these URLs are a transient branch-only mid-plan state
// (see RenderSlide's doc).
const externalMediaURLPrefix = "/assets/templates/external/media/"

// mediaBaseForLayer is the renderer's layer→served-media-base hook
// (renderer.layerMediaBase) for a layered deck: the external `templates:` layer
// resolves under externalMediaURLPrefix, every other layer — the deck-root
// local templates/ layer of a two-layer deck, and any layer reached without a
// role — under the single/primary mediaURLPrefix. A nil layer, which
// funcMapForLayer never passes, falls back to the primary base; a sole-layer
// deck never installs the hook at all, so the existing `media` helper stays
// bound and output remains byte-identical.
func mediaBaseForLayer(layer *template.LibraryLayer) string {
	if layer != nil && layer.Role == template.LayerRoleExternal {
		return externalMediaURLPrefix
	}
	return mediaURLPrefix
}

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
// For a layered deck, RenderSlide binds the slide layout to the slide
// template's OWNING layer. reg.LayerOf(parsed.Template) returns the
// LibraryLayer that contributed the slide template; when its Role is not
// LayerRoleSole the deck is two-layered, and RenderSlide installs the
// renderer's layer→served-media-base hook so each layer's layouts resolve
// `media` against their own Media filesystem and their own served prefix. The
// slide layout itself, like a called section, then executes through
// executeLayeredLayout with its owning layer (replacing the plain shared
// namespace execution); a role of LayerRoleSole — a single-layer library,
// whether the deck-root templates/ or an external library — or no
// owning-layer provenance leaves the hook nil and the slide layout executes
// through the shared namespace exactly as before, so a single-layer library
// renders byte-for-byte as earlier code did.
//
// The per-layer media URL contract is: the single/primary layer — the deck's
// only layer when it has one, otherwise the deck-root local templates/ layer
// of a two-layer deck — keeps today's exact URL
// "/assets/templates/media/<path>"; the external `templates:` layer of a
// two-layer deck emits the distinct per-layer prefix
// "/assets/templates/external/media/<path>" (the layer is identified by the
// `external` path segment; there is no index and no query tag). Because phase 4
// (the server routes) depends on phase 2, the external prefixed URLs RenderSlide
// emits before phase 4 are a transient branch-only mid-plan state: between
// phase 2 and phase 4 the server serves only the unprefixed single/primary
// (local) routes, so an external-layer URL renders but does not yet resolve,
// and the phase-2 render tests exercise the dispatching hook against an
// in-memory media filesystem and an explicit base string, never against live
// server routes; phase-04 tasks 6–8 land the external routes before any
// release.
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
	// The slide template's owning layer identifies the deck's layering: its
	// Role is set by LoadDeckLibrary (external/local for a two-layer deck, sole
	// for a single-layer library). A two-layer deck needs the per-layer media
	// URL scheme installed; a sole layer, or a registry carrying no
	// provenance, leaves the hook nil so every layout keeps the primary `media`
	// helper and output stays byte-identical (see the RenderSlide doc for the
	// URL contract).
	slideLayer := reg.LayerOf(slideTmpl.Name)

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
	if slideLayer != nil && slideLayer.Role != template.LayerRoleSole {
		r.layerMediaBase = mediaBaseForLayer
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
	if err := r.executeLayeredLayout(&buf, slideTmpl, slideLayer, data); err != nil {
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

	// funcMap is the caller's layout func map — the helpers `media`/`section`/
	// `dict`/`list` plus the number/date format functions
	// (template.LayoutFuncMap). parseLayouts stores it here so the render-time
	// `section` helper can build the func map a target executes against: the
	// same map with `media` rebound to the target's owning layer
	// (funcMapForLayer), so a media reference inside a called section resolves
	// in the layer that owns it (section-helper, library-layer-references).
	funcMap htmltmpl.FuncMap

	// layerMediaBase maps a *template.LibraryLayer to the served media URL base
	// its `media` helper resolves under. It is the renderer's per-layer URL hook:
	// RenderSlide installs it (mediaBaseForLayer) exactly when the slide
	// template's owning layer has a two-layer role (LayerRoleExternal or
	// LayerRoleLocal), so the single/primary layer keeps mediaURLPrefix and the
	// external layer gets externalMediaURLPrefix. While it is nil — a sole-layer
	// deck or a registry with no owning-layer provenance — the renderer holds no
	// per-layer URL scheme, so a called section keeps the existing (primary)
	// `media` helper and a single-layer library renders byte-for-byte as before
	// (section-helper, library-layer-references).
	layerMediaBase func(*template.LibraryLayer) string

	// layerNamespaces caches, per owning layer, the html/template namespace a
	// TWO-layer deck's layouts execute against (executeLayeredLayout). Each
	// entry is built once from every registered layout text with that layer's
	// func map (funcMapForLayer) — the whole merged library re-parsed so
	// `{{ template "<name>" . }}` composition still resolves within the layer's
	// namespace — and reused for every layout of that layer. html/template
	// resolves a func map at parse time and cannot rebind `media` on an
	// already-parsed set by cloning, so a layer's namespace must re-Parse the
	// texts (namespaceForLayer).
	layerNamespaces map[*template.LibraryLayer]*htmltmpl.Template

	// sectionDepth counts the section-instance nesting renderChildren is
	// currently inside, so a re-entrant or malformed resolved child tree fails
	// as a render error instead of recursing without bound
	// (maxSectionNesting). It is incremented on entry to renderChildren and
	// restored on exit, so the renderer's depth is zero between slides. A
	// reference cycle is already rejected at load time (Section.Walk), so this
	// is a defensive bound on the resolved tree, mirroring internal/template's
	// checkvalues maxDepth.
	sectionDepth int
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
	data["raw"] = template.RawContext(s.Frontmatter, tmpl, s.Body)
	data["data"] = template.DataContext(r.contextNodes(s.Sections), nil)
	return data, nil
}

// renderSection renders one AUTHORED section instance: it builds the
// instance's own reserved `.raw` context — the source view of its authored
// frontmatter and body — and `.data` context — its authored child sections as
// data, each child's `.item.parent` being this instance's source field values,
// as template.DataContext's parent rule requires — from the shared builders
// (template.RawContext and template.DataContext over this instance's child
// context nodes), so a section template sees its source view and its authored
// children as data at every depth (raw-source-context, section-data-context).
//
// With the instance's reserved context built, it hands the resolved instance to
// renderInstance — the single section-instance path an authored instance and a
// helper child both go through — which converts the field values, renders the
// body and layout, renders and splices the child instances and publishes the
// instance as the caller context for a nested `{{ section … }}` call.
//
// The instance's own template's owning layer (r.reg.LayerOf(tmpl.Name)) is
// passed as the instance's layer, so a declared child section contributed by
// the external layer resolves a media reference in that layer rather than the
// merged library's primary media (library-layer-references).
func (r *renderer) renderSection(sec *slide.Section, tmpl *template.Template, secCtx *sectionCtx) (htmltmpl.HTML, error) {
	// Every section instance, at every composition depth, carries its own
	// reserved `.raw` (its authored source field values and body) and `.data`
	// (its authored children as data, parented on its own source field values)
	// next to its converted field values and `deck`/`slide`/`item`. They are
	// built before values so values merges them into the instance map, and
	// by the same shared builders the load-time example execution uses, so the
	// two halves cannot drift (template-context).
	secCtx.raw = template.RawContext(sec.Frontmatter, tmpl, sec.Body)
	secCtx.data = template.DataContext(r.contextNodes(sec.Children), secCtx.raw)
	return r.renderInstance(sec, tmpl, r.reg.LayerOf(tmpl.Name), secCtx)
}

// renderInstance renders one RESOLVED section instance through its own
// layout.html.tmpl, deepest first, and is the single section-instance path
// every instance goes through: an authored instance rendered by renderSection
// and a helper child instantiated from a `child`/`children` argument by
// renderChildren both enter here, so an authored child and its equivalent
// helper child cannot drift and produce identical rendered HTML and identical
// reserved context (template-context, item-context, section-data-context,
// raw-source-context).
//
// The resolved instance is carried by:
//
//   - sec, whose Frontmatter and Body are its EFFECTIVE field values and body
//     source and whose Children are the child instances it composes (an
//     authored instance); a helper instance carries its children as a resolved
//     tree on secCtx.resolvedChildren instead, and sec then supplies only its
//     field source and body;
//   - tmpl, its resolved section template;
//   - layer, its owning layer (a helper child's resolved layer, or
//     r.reg.LayerOf(tmpl.Name) for an authored instance);
//   - secCtx, its execution context: the reserved `.item` descriptor and the
//     `.raw`/`.data` contexts its caller built for it, plus the deck/slide
//     `cfg`/`meta`/`total`, and — for a helper instance — its resolved child
//     tree (resolvedChildren).
//
// It first renders every child section instance (each through its own reserved
// context, recursively) and attaches them to its execution context as lists of
// trusted HTML — one element per child instance, in source order — so a parent
// layout splices each child's rendered markup with
// `{{ range .blocks }}{{ . }}{{ end }}` rather than ranging over data maps
// (template-language). An authored instance's children render through
// renderSection, rebuilding each `.item` from the parsed tree; a helper
// instance's rendered tree renders through renderChildren, reusing each child's
// resolved `.item` (secCtx.resolvedChildren). The returned HTML is what the
// parent receives, so section markup composes bottom-up at every depth and at
// the top level.
//
// The instance's effective field values are converted with values, which merges
// the reserved `deck`/`slide`/`item`/`raw`/`data` entries into the instance map
// next to the converted field values, at every composition depth
// (template-context, item-context, raw-source-context, section-data-context).
// Its body is rendered to block HTML and exposed as `body`. A child's rendered
// `.item.parent` is this instance's converted field values (item-context); at
// the top level the parent is the slide, so the descriptor carries nil.
//
// The instance's field values are also published as the renderer's caller
// context for the duration of its layout execution and restored afterwards, so
// a `{{ section … }}` call the layout makes resolves its target's `.item.parent`
// from this instance and inherits its deck/slide context (item-context,
// template-context). A call made from a slide layout sees a nil parent, since
// RenderSlide publishes the slide context with no calling instance.
//
// The instance's layout executes with the `media` helper bound to layer when
// the renderer holds a per-layer base hook (executeLayeredLayout), so a
// declared child section contributed by the external layer resolves a media
// reference in that layer rather than the merged library's primary media
// (library-layer-references). A template whose owning layer is unknown, or a
// renderer whose per-layer base hook is unset, executes through the shared
// namespace exactly as before, keeping single-layer output byte-identical.
func (r *renderer) renderInstance(sec *slide.Section, tmpl *template.Template, layer *template.LibraryLayer, secCtx *sectionCtx) (htmltmpl.HTML, error) {
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

	// Render this instance's child instances and attach them to its map as
	// trusted-HTML lists keyed by declared section name — one element per child
	// instance, in passed order — so the instance's own layout splices each
	// child's rendered markup with `{{ range .blocks }}{{ . }}{{ end }}` rather
	// than ranging over data maps (template-language). An instance parsed from
	// the deck (an authored instance) carries slide.Section children and renders
	// each through renderSection, rebuilding its `.item` descriptor from the
	// parsed tree; an instance instantiated from a `child`/`children` argument
	// (a helper instance) carries a resolved template.ResolvedChild tree instead
	// (secCtx.resolvedChildren) and renders it through renderChildren, reusing
	// each child's resolved descriptor. The two are mutually exclusive, so an
	// authored instance and its equivalent helper instance each render through
	// the same instance path with the child lists assembled the same way.
	if secCtx != nil && len(secCtx.resolvedChildren) > 0 {
		lists, _, err := r.renderChildren(secCtx.resolvedChildren, secCtx)
		if err != nil {
			return "", err
		}
		for name, list := range lists {
			existing, _ := data[name].([]htmltmpl.HTML)
			data[name] = append(existing, list...)
		}
	} else {
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
	if err := r.executeLayeredLayout(&buf, tmpl, layer, data); err != nil {
		return "", fmt.Errorf("execute layout: %w", err)
	}
	return htmltmpl.HTML(buf.String()), nil
}

// maxSectionNesting bounds renderChildren's recursion so a re-entrant or
// malformed resolved child tree fails as a render error instead of recursing
// without bound. A real reference cycle is rejected at load time
// (Section.Walk), so this is a defensive bound on the already-resolved tree,
// mirroring internal/template's checkvalues maxDepth. It is deliberately far
// larger than the six heading levels a declared-section chain may nest (a
// helper/child edge adds no heading depth, so a legitimate helper child tree
// may be deeper than six), so it never rejects a tree the loader accepted.
const maxSectionNesting = 64

// renderChildren instantiates a `section` helper call's resolved children as
// real section instances through the single shared instance path
// (renderInstance) and returns the rendered child HTML grouped by declared
// section name together with the target's reserved `.data` context.
// (requirements.requirement.item-context,
// requirements.requirement.section-data-context,
// requirements.requirement.raw-source-context,
// requirements.requirement.section-template-context)
//
// children is SectionCall.Children — the tree ResolveChildren built: each child
// carries its own resolved Call (its instantiated Template, effective Values,
// validated Body, one-item Item descriptor and owning Layer) and its own
// resolved nested children on ResolvedChild.Children. Every child is rendered
// deepest first through renderInstance, so section markup composes bottom-up
// exactly as an authored instance's does.
//
// Each child instance is rendered with:
//
//   - its own sibling `.item` descriptor, used exactly as ResolveChildCall built
//     it (Call.Item) rather than rebuilt from a parsed tree, so its
//     index/number/count/first/last/section/template/parent are the resolved
//     child's (item-context);
//   - its own reserved `.raw` source view, built with the shared
//     template.RawContext from the child's AUTHORED source (ResolvedChild.Source,
//     the supplied fields before the instantiated template's defaults) and its
//     body, so a materialised default never leaks into `.raw`
//     (raw-source-context);
//   - its own reserved `.data` view, built with the shared
//     template.DataContext over the child's own nested children (childNodes),
//     parented on the child's own `.raw`, so a child template sees its
//     grandchildren as data and splices their rendered markup
//     (section-data-context);
//   - the `media` helper of its OWN owning layer (Call.Layer), so a media
//     reference inside an external-layer child resolves in that layer rather
//     than the merged library's primary media (library-layer-references).
//
// Nested children recurse through the same path: the child instance's
// sectionCtx carries ResolvedChild.Children as resolvedChildren, which
// renderInstance renders by calling back into renderChildren, so a helper
// child's own grandchildren keep their resolved descriptors at every depth
// (child-constructor, children-forwarding).
//
// The returned rendered lists are keyed by each child's declared section name
// (its resolved `.item.section`), one element per child in passed order — the
// target's rendered child-section lists, which its layout ranges over to splice
// child markup (template-language). The returned `data` is the target's own
// reserved `.data.<child>` view: the passed children mapped onto the neutral
// template.ContextNode tree (childNodes) and walked by the shared
// template.DataContext, parented on the target's own `.raw` (secCtx.raw),
// mirroring renderSection's contextNodes walk and the load-time exampleChildNodes
// adapter, so the load-time and render-time data views agree
// (section-data-context). A call whose children pass through adds nothing to the
// CALLER's `.data` or its rendered section list: it contributes only to the
// target's context, and only the caller of this function decides where the
// results are published (section-data-context).
//
// A nil secCtx is treated as an empty execution context (no deck/slide); a
// depth counter (maxSectionNesting) bounds the recursion so a re-entrant child
// chain fails with a render error naming the bound instead of looping.
func (r *renderer) renderChildren(children []*template.ResolvedChild, secCtx *sectionCtx) (map[string][]htmltmpl.HTML, map[string]any, error) {
	if secCtx == nil {
		secCtx = &sectionCtx{}
	}
	if r.sectionDepth >= maxSectionNesting {
		return nil, nil, fmt.Errorf("section children nested too deeply: depth exceeds %d", maxSectionNesting)
	}
	r.sectionDepth++
	defer func() { r.sectionDepth-- }()

	// The target's own `.data` view is built by the same shared builder the
	// load-time example runner uses, over the neutral child-node tree, so the
	// two views cannot drift (section-data-context).
	data := template.DataContext(childNodes(children), secCtx.raw)

	lists := make(map[string][]htmltmpl.HTML, len(children))
	for _, child := range children {
		if child == nil || child.Call == nil {
			continue
		}
		rendered, err := r.renderChild(child, secCtx)
		if err != nil {
			return nil, nil, err
		}
		name := childSectionName(child.Call)
		lists[name] = append(lists[name], rendered)
	}
	return lists, data, nil
}

// renderChild renders one resolved child instance through the single shared
// instance path (renderInstance), carrying its resolved `.item` (Call.Item), its
// own `.raw`/`.data` context and its own nested children (ResolvedChild.Children)
// so its grandchildren render through renderChildren rather than the authored
// renderSection path (child-constructor, item-context, raw-source-context,
// section-data-context). parent supplies the deck/slide execution context
// (cfg/meta/total) the child inherits.
//
// renderInstance consumes only the adapted section's effective field source
// (Frontmatter) and body; every other input — the resolved template, layer,
// `.item` and the resolved child tree — is carried directly, so the adapter
// need not reconstruct them from a parsed slide.Section. Its Children are
// carried as resolvedChildren on the child's sectionCtx, so the adapted
// slide.Section carries no parsed children.
func (r *renderer) renderChild(child *template.ResolvedChild, parent *sectionCtx) (htmltmpl.HTML, error) {
	call := child.Call
	raw := template.RawContext(child.Source, call.Template, call.Body)
	childCtx := &sectionCtx{
		cfg:              parent.cfg,
		meta:             parent.meta,
		total:            parent.total,
		item:             call.Item,
		raw:              raw,
		data:             template.DataContext(childNodes(child.Children), raw),
		resolvedChildren: child.Children,
	}
	sec := &slide.Section{
		Frontmatter: child.Source,
		Body:        call.Body,
	}
	rendered, err := r.renderInstance(sec, call.Template, call.Layer, childCtx)
	if err != nil {
		return "", fmt.Errorf("child %q: %w", childSectionName(call), err)
	}
	return rendered, nil
}

// childNodes maps a section-helper call's resolved child tree onto the neutral
// []template.ContextNode the shared template.DataContext walks, so a helper
// call's target exposes its passed children as data exactly as an authored
// parent exposes its child instances (section-data-context,
// children-forwarding). It is the helper-children counterpart of contextNodes —
// the authored slide.Section adapter — and the render-side mirror of
// internal/template's exampleChildNodes, so the load-time and render-time `.data`
// views cannot drift.
//
// Each node's declared name and sibling index come from the child's resolved
// `.item` descriptor (childSectionName/childIndex), its Template is the child's
// resolved template, its Source the child's AUTHORED supplied fields
// (ResolvedChild.Source, before the instantiated template's defaults, so a
// default never appears in the child's `.raw`) and its Body the child's body;
// nested resolved children recurse in passed order, so sibling index/count match
// the descriptors ResolveChildren assigned. A nil child is skipped.
func childNodes(children []*template.ResolvedChild) []template.ContextNode {
	if len(children) == 0 {
		return nil
	}
	nodes := make([]template.ContextNode, 0, len(children))
	for _, child := range children {
		if child == nil || child.Call == nil {
			continue
		}
		call := child.Call
		nodes = append(nodes, template.ContextNode{
			Name:     childSectionName(call),
			Index:    childIndex(call),
			Template: call.Template,
			Source:   child.Source,
			Body:     call.Body,
			Children: childNodes(child.Children),
		})
	}
	return nodes
}

// childSectionName returns the declared section name a resolved child fills: its
// `.item.section`, falling back to the instantiated template's name when the
// descriptor is absent, so the child's `.data` key and rendered-list key match
// its declared section. It mirrors internal/template's exampleChildName.
func childSectionName(call *template.SectionCall) string {
	if call == nil {
		return ""
	}
	if call.Item != nil {
		if s, ok := call.Item["section"].(string); ok && s != "" {
			return s
		}
	}
	if call.Template != nil {
		return call.Template.Name
	}
	return ""
}

// childIndex returns the zero-based sibling index a resolved child fills from
// its `.item.index`, or 0 when the descriptor is absent. It mirrors
// internal/template's exampleChildIndex.
func childIndex(call *template.SectionCall) int {
	if call == nil || call.Item == nil {
		return 0
	}
	if i, ok := call.Item["index"].(int); ok {
		return i
	}
	return 0
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
// (internal/template): an optional fields map, an optional body string and then
// the call's child arguments. A fields argument that is not a map or a body
// argument that is not a string is a programming error; each argument after the
// body must be a *template.ChildRequest returned by the `child`/`children`
// constructors (`{{ section "<name>" [fields] [body] [child…] }}`), and a value
// of any other type is a programming error. Resolution and validation are
// delegated in full to template.ResolveSectionCallLayered — the target must be
// a defined section-usage template, the supplied fields are defaults-first
// validated against the target's schema, a present body is validated against
// the target's body rule, and the child arguments are resolved with
// ResolveChildren against the target's declared sections — so the render-time
// helper reuses the phase-01 resolution rather than duplicating it and the
// resolved SectionCall carries the target's owning layer (r.reg.LayerOf, the
// merged-library provenance) for the target's execution. The caller's own field
// values (renderer.callerFields) are passed as the call's parent, so the
// target's one-item `.item.parent` is the calling instance's fields; it is nil
// when the call is made from a slide layout, whose parent is the slide
// (item-context). A `children "<section>" .data.<section>` argument forwards the
// caller's authored `.data` entries for that section, which ResolveChildren
// expands into child instances keeping each entry's own template, raw fields,
// raw body and nested data (children-forwarding).
//
// The effective values are converted with values exactly as renderSection
// converts an instance's: a text field becomes inline HTML, a number or date is
// formatted, and the reserved `deck`/`slide`/`item` entries are merged in with
// `.item` the one-item descriptor ResolveSectionCall built. The call's body,
// when present, is rendered to block HTML and exposed as `body`
// (template-language). The reserved `.raw` entry is the source view of what the
// call supplied — the supplied fields plus the body, before the target's
// defaults are applied. The call's child arguments are the target's own child
// instances: they are rendered through renderChildren — the same shared
// instance path an authored or helper child goes through — and their results are
// published to the TARGET, not the caller: the target's rendered child-section
// lists are keyed by declared section name (data[sectionName] =
// []htmltmpl.HTML, spliced with `{{ range .column }}{{ . }}{{ end }}`) and its
// own `.data.<child>` view (secCtx.data, built by the shared template.DataContext
// over the passed children) lets the target read `len .data.column` as the
// passed-child count and range over the data entries. The call itself adds
// nothing to the CALLER's `.data` or rendered section list — it returns only its
// target's HTML (raw-source-context, section-data-context, item-context,
// child-constructor, children-forwarding).
//
// Before the target layout executes, the renderer's caller context is set to
// this instance — its effective field values become a nested call's
// `.item.parent`, and its deck/slide context is inherited — and restored
// afterwards, so a `{{ section … }}` call the target's own layout makes
// resolves against this instance (item-context, template-context). The target
// executes with the `media` helper bound to its OWN owning layer (call.Layer)
// when the renderer holds a per-layer base hook, so a media reference inside an
// external-layer section resolves in the external layer rather than the merged
// library's primary media (executeLayeredLayout, library-layer-references);
// otherwise it runs through the shared namespace (renderer.root), a layout
// already parsed with this same helper bound, as before. The result is the
// target's trusted HTML, which the calling layout splices unescaped.
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
	// Everything after the body is a child argument: the value of a `child` or
	// `children` constructor, each a *template.ChildRequest. A wrong type is a
	// programming error, so a malformed call fails the whole execution rather
	// than resolving a partial child set, exactly as a malformed fields/body
	// argument does.
	var children []*template.ChildRequest
	if len(args) > 2 {
		children = make([]*template.ChildRequest, 0, len(args)-2)
		for i, v := range args[2:] {
			c, ok := v.(*template.ChildRequest)
			if !ok {
				return "", fmt.Errorf("section helper %q: child argument %d must be a child/children value, got %T", name, i+1, v)
			}
			children = append(children, c)
		}
	}

	// ResolveSectionCallLayered applies the target's defaults first, validates
	// the effective fields with CheckValues, checks the body against the
	// target's body rule, resolves the child arguments with ResolveChildren
	// against the target's declared sections (defaults-first, accepted-template
	// and min/max, recursively), builds the one-item .item descriptor whose
	// parent is the calling instance's fields (nil from a slide layout), and
	// records the target's owning layer (via r.reg.LayerOf) so its layout
	// executes with that layer's media helper (library-layer-references).
	call, err := template.ResolveSectionCallLayered(r.reg.Lookup, r.reg.LayerOf, name, fields, body, r.callerFields, children...)
	if err != nil {
		return "", fmt.Errorf("section helper %q: %w", name, err)
	}

	// The target is the sole instance of its own one-item group, so it
	// executes with the calling slide's deck/slide context and its own .item
	// descriptor. Its reserved `.raw`/`.data` context (raw-source-context,
	// section-data-context) is supplied to values and merged into the instance
	// map there, exactly as a rendered instance's is: `.raw` is the source view
	// of what the call supplied — the supplied fields before defaults are
	// applied, so a default is never mistaken for authored source, plus the
	// body.
	caller := r.callerCtx
	if caller == nil {
		caller = &sectionCtx{}
	}
	secCtx := &sectionCtx{
		cfg:   caller.cfg,
		meta:  caller.meta,
		total: caller.total,
		item:  call.Item,
		raw:   template.RawContext(fields, call.Template, call.Body),
		data:  map[string]any{},
	}

	// The call's child arguments are the target's own child instances: render
	// them through the shared child path (renderChildren), which instantiates
	// each through renderInstance — the same path an authored child goes
	// through — and returns the rendered lists grouped by declared section name
	// together with the target's own `.data.<child>` view. That view becomes the
	// target's reserved `.data` (so it reads `len .data.column` as the
	// passed-child count); the rendered lists are merged into the target's
	// layout map under each declared section name (so it splices them with
	// `{{ range .column }}{{ . }}{{ end }}`), exactly as renderInstance publishes
	// an instance's authored or resolved children. A call with no child
	// arguments skips the walk and keeps an empty `.data`, so its output is
	// byte-identical to before (child-constructor, children-forwarding,
	// section-data-context, item-context, raw-source-context).
	var childLists map[string][]htmltmpl.HTML
	if len(call.Children) > 0 {
		lists, childData, err := r.renderChildren(call.Children, secCtx)
		if err != nil {
			return "", fmt.Errorf("section helper %q: %w", name, err)
		}
		secCtx.data = childData
		childLists = lists
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
	// Merge the target's rendered child-section lists into its layout map, one
	// trusted-HTML element per passed child in passed order, keyed by declared
	// section name, so the target's own layout splices each child's markup. The
	// call contributes to the TARGET only: nothing here touches the caller's
	// `.data` or rendered section list (section-data-context, template-language).
	for childName, list := range childLists {
		existing, _ := data[childName].([]htmltmpl.HTML)
		data[childName] = append(existing, list...)
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
	if err := r.executeLayeredLayout(&buf, call.Template, call.Layer, data); err != nil {
		return "", fmt.Errorf("section helper %q: execute target layout: %w", name, err)
	}
	return htmltmpl.HTML(buf.String()), nil
}

// executeLayeredLayout executes tmpl's layout into buf with the `media` helper
// bound to layer. When layer is known and the renderer holds a per-layer base
// hook (renderer.layerMediaBase), the layout executes from a namespace built
// from EVERY registered layout text with that layer's func map
// (namespaceForLayer), so a layout can still invoke another layout with
// `{{ template "<name>" . }}` — the composition parseLayouts' shared namespace
// supports — while each `{{ media … }}` reference resolves in the executing
// layout's own layer (library-layer-references). Parsing the single target
// layout text alone would lose the shared namespace and fail such a call with
// "no such template". Otherwise — a nil layer, or a renderer whose layer→base
// hook is unset — the layout executes through the shared namespace exactly as
// before, which keeps single-layer output byte-identical and leaves the
// per-layer URL scheme to a deck whose layers carry no role. It is the shared
// building block the render-time section helper, the declared child-section path
// (renderSection) and the slide's own layout (RenderSlide), all apply in the
// same way.
func (r *renderer) executeLayeredLayout(buf *bytes.Buffer, tmpl *template.Template, layer *template.LibraryLayer, data map[string]any) error {
	if layer == nil || r.layerMediaBase == nil {
		return r.root.ExecuteTemplate(buf, layoutName(tmpl), data)
	}
	namespace, err := r.namespaceForLayer(layer)
	if err != nil {
		return fmt.Errorf("parse target layout: %w", err)
	}
	return namespace.ExecuteTemplate(buf, layoutName(tmpl), data)
}

// namespaceForLayer returns the html/template namespace a layout owned by layer
// executes against for a TWO-layer deck: every registered layout text
// (Registry.Templates — the whole merged library, both layers), parsed together
// with layer's func map (funcMapForLayer — `media` rebound to layer.Media and
// layerMediaBase(layer), `section` bound to this renderer), keyed by layout name
// exactly as parseLayouts keys the shared namespace. Re-parsing the whole set
// rather than the one target layout keeps that shared namespace — and, because
// the set is the registry's full layout set, a superset of parseLayouts'
// reachable walk, it also covers a target reachable only through an authored
// child instance (a schema-free parent declares no sections) — so
// `{{ template "<name>" . }}` composition between layouts resolves instead of
// failing with "no such template", including when the invoked layout is
// contributed by another layer. The result is cached per layer on the renderer
// (renderer.layerNamespaces), so each layer's set is built at most once per
// render. html/template resolves a func map at parse time and cannot rebind
// `media` on an already-parsed namespace by cloning, which is why the texts are
// re-parsed rather than cloned. layer is non-nil; executeLayeredLayout takes the
// skip-the-per-layer-path decision (a nil layer or an unset hook) before calling.
func (r *renderer) namespaceForLayer(layer *template.LibraryLayer) (*htmltmpl.Template, error) {
	if ns, ok := r.layerNamespaces[layer]; ok {
		return ns, nil
	}
	namespace := htmltmpl.New("layouts").Funcs(r.funcMapForLayer(layer))
	for _, t := range r.reg.Templates() {
		if t.Layout.Text == "" {
			return nil, fmt.Errorf("template %q has no layout text", t.Name)
		}
		if _, err := namespace.New(layoutName(t)).Parse(t.Layout.Text); err != nil {
			return nil, fmt.Errorf("template %q: parse layout: %w", t.Name, err)
		}
	}
	if r.layerNamespaces == nil {
		r.layerNamespaces = make(map[*template.LibraryLayer]*htmltmpl.Template)
	}
	r.layerNamespaces[layer] = namespace
	return namespace, nil
}

// funcMapForLayer returns the html/template func map a layout executes against
// when it belongs to layer: the caller's layout func map (renderer.funcMap —
// the helpers `media`/`section`/`dict`/`list` plus the number/date format
// functions) with the renderer-backed `section` helper bound and, when layer is
// known and the renderer holds a per-layer base hook (layerMediaBase), the
// `media` helper rebound to that layer — media filesystem = layer.Media, URL
// base = layerMediaBase(layer) — so every media reference inside the layout
// resolves in the layer that owns it (library-layer-references). A nil layer,
// or an unset hook, leaves `media` as the existing primary helper, so a
// single-layer library renders byte-for-byte as before. It is the func-map half
// of executeLayeredLayout, shared by the declared child-section path
// (renderSection), the render-time section helper and the slide's own layout
// (RenderSlide), each binding its template's own owning layer the same way.
func (r *renderer) funcMapForLayer(layer *template.LibraryLayer) htmltmpl.FuncMap {
	base := r.funcMap
	if base == nil {
		base = template.LayoutFuncMap(nil, "")
	}
	fm := make(htmltmpl.FuncMap, len(base)+1)
	for name, fn := range base {
		fm[name] = fn
	}
	if layer != nil && r.layerMediaBase != nil {
		fm["media"] = template.MediaFunc(layer.Media, r.layerMediaBase(layer))
	}
	fm["section"] = r.sectionHelper
	return fm
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
	// built by the caller with template.DataContext over its contextNodes. It
	// is nil when the map under
	// construction is not a section instance, on the same terms as raw, and
	// values then adds no `data` entry. A section instance with no authored
	// children carries a non-nil but empty map, so a section template always
	// sees `.data` at every depth.
	data map[string]any

	// resolvedChildren, when non-empty, are the instance's own child instances
	// as a resolved tree rather than authored slide.Section values: an instance
	// instantiated from a `child`/`children` argument carries its nested
	// children this way (SectionCall.Children). renderInstance renders them
	// through renderChildren — reusing each child's resolved `.item`, `.raw`,
	// `.data` and owning layer — instead of through renderSection, so a helper
	// child's own grandchildren keep their resolved descriptors and cannot
	// drift from the resolved tree (child-constructor, section-data-context).
	// It is nil for an authored instance, whose children render by the
	// renderSection path. The two paths are mutually exclusive: an instance is
	// either parsed (slide.Section children) or helper-resolved.
	resolvedChildren []*template.ResolvedChild
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

// contextNodes adapts the render-time slide section tree into the neutral
// []template.ContextNode the shared template.DataContext walks
// (section-data-context, raw-source-context, item-context, template-context).
// It replaces the old render-time `.data` builder: it no longer builds the
// `.data` map itself, only the neutral tree, which template.DataContext then
// walks — the same walk the load-time example execution performs over its own
// exampleSection tree (mapped through the parallel exampleContextNodes adapter)
// — so the load-time and render-time `.data` views cannot drift. A node's `raw`
// view is built by template.RawContext from its Source, Body and Template when
// DataContext walks the tree.
//
// sections is one parent's authored child sections, in source order, exactly as
// the parsed slide tree carries them (slide.Slide.Sections or
// slide.Section.Children). Each node's Template is the instance's resolved
// template, looked up in the registry by its declared `Template` name. When the
// declared name is non-empty but the registry cannot resolve it, the node
// still carries a name-only template, so DataContext's `item.template` label
// stays the declared name instead of dropping to "" — matching the previous
// builder's itemContext(…, sec.Template, …). A name-only template declares no
// fields, so its source view copies none, exactly as an unresolved template did
// before.
//
// Only authored instances become nodes: a section invoked by the `section`
// helper is a direct render call, never part of the parsed tree, so it is
// absent from `.data` exactly as it is absent from the rendered-HTML list.
func (r *renderer) contextNodes(sections []slide.Section) []template.ContextNode {
	if len(sections) == 0 {
		return nil
	}
	nodes := make([]template.ContextNode, len(sections))
	for i := range sections {
		sec := &sections[i]
		tmpl, _ := r.reg.Lookup(sec.Template)
		// A name the registry could not return still labels the item, as the
		// previous builder did; a name-only template declares no fields, so
		// the source view copies none, exactly as before.
		if tmpl == nil && sec.Template != "" {
			tmpl = &template.Template{Name: sec.Template}
		}
		nodes[i] = template.ContextNode{
			Name:     sec.Name,
			Index:    sec.Index,
			Template: tmpl,
			Source:   sec.Frontmatter,
			Body:     sec.Body,
			Children: r.contextNodes(sec.Children),
		}
	}
	return nodes
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
	// Store the caller's func map on the renderer so the render-time `section`
	// helper can derive a target's per-layer func map from it (funcMapForLayer)
	// without mutating the caller's map. The namespace copy below overrides the
	// `section` stub with the renderer-backed helper.
	r.funcMap = funcMap
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
// declared-section and field walk: reg.HelperTargets returns each reached
// layout's helper-call targets, re-parsing Layout.Text with the canonical
// layout func map under a lazily memoised registry cache — so a slide's whole
// walk re-parses each layout at most once across RenderSlide calls instead of
// once per render — and a helper-only target, one that is not also a declared
// child of its caller, is still pulled into the shared namespace so the
// render-time helper can execute it. Helper edges are followed transitively to
// the same closure as declared edges, bounded by seen. Missing names are
// skipped: validation has already rejected an undefined name, and a layout
// simply cannot invoke a template that does not exist.
//
// A HelperTargets error is deliberately not propagated: the accessor swallows
// the re-parse error to nil, exactly as the previous Section.HelperRefs call
// swallowed it here. reachableTemplates cannot return an error, load already
// validated every layout, and the only way the re-parse can fail is a layout
// that does not re-parse with the canonical func map — which is exactly what
// parseLayouts' own Parse reports for the same text, so skipping the edges here
// loses no error that the caller does not already surface.
func reachableTemplates(root *template.Template, reg *template.Registry) []*template.Template {
	seen := make(map[string]bool)
	var out []*template.Template

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
		for _, name := range reg.HelperTargets(t.Name) {
			if st, ok := reg.Lookup(name); ok && st != nil {
				add(st)
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
