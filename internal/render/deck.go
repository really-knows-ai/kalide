package render

import (
	"bytes"
	"errors"
	"fmt"
	htmltmpl "html/template"
	"strings"

	"github.com/really-knows-ai/kalide/internal/assets"
	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// deckPageName is the deck page template's path within the embedded pages
// sub-tree (assets.DeckPage). The sub-tree is rooted at that page and holds the
// full-page html/template shells.
const deckPageName = "deck.html.tmpl"

// assetsURLPrefix is the URL the embedded asset tree is mounted under by the
// phase-7 HTTP server, and what the deck page's <link>/<script> URLs assume
// ("/assets/reveal/…").
const assetsURLPrefix = "/assets/"

// themesURLPrefix is the URL prefix a project theme's on-disk files are
// served under: templates/themes/<name>/<file> becomes
// themesURLPrefix + "<name>/<file>". It must match internal/server's
// ThemesPath exactly (media.go) — internal/server mounts the handler that
// serves this space, internal/render only bakes the URL into the rendered
// page, and the two packages cannot share the constant directly since server
// imports render.
const themesURLPrefix = "/assets/templates/themes/"

// RenderDeck renders a whole validated deck into the offline reveal.js page:
// the embedded assets.DeckPage "deck.html.tmpl" executed with the deck's title,
// the theme stylesheet resolved from the phase-2 theme registry, and the
// `navigation` value passed straight through as reveal.js's navigationMode.
//
// cfg is the deck's configuration (internal/deck.LoadConfig); Title drives the
// document <title> (empty falls back to "kalide"), Theme selects the stylesheet
// in themeReg and Navigation is the reveal.js navigationMode — "default",
// "linear" or "grid", with an omitted/empty value resolving to reveal.js's own
// default (navigation-mode). d is the deck's ordered slide model
// (internal/deck.LoadSlides): each numbered slide is its own `<section>`, and a
// numbered slide with letter slides is wrapped — together with them — in a
// plain outer `<section>` whose first child is the numbered slide's own
// `<section>` and whose remaining children are its letter slides in letter
// order (the vertical stack), in the model's order (vertical-slides). parsed
// holds the slides' parsed structures
// (internal/slide.Parse), indexed here by slide.Slide.File, which must be the
// slide's deck path (deck.Slide.Path) — the convention internal/validate uses.
// reg is the populated template registry the deck was validated against —
// typically built from the project's templates/ library
// (template.NewRegistryFromLibrary); themeReg is the project theme registry
// (theme.LoadDir) and must be non-nil — callers always construct one from the
// project's on-disk theme library, so a nil themeReg is a caller error and
// RenderDeck reports it rather than silently substituting a default. funcMap is
// the library's layout func map (template.LayoutFuncMap: the helpers `media`
// — bound to the served templates/media URL base — `section`, `dict` and
// `list`, plus the format functions), threaded into every slide's RenderSlide
// call so a library layout using any helper renders (template-media,
// template-language, section-helper); RenderSlide rebinds the render-time
// `section` helper over the func map's parse-resolvable stub. RenderDeck
// derives the deck's slide-file count once (deck.Deck.Total) and threads cfg
// plus each slide's modelled
// position into RenderSlide, so every slide layout executes with the reserved
// `deck` and `slide` context (deck-data-in-templates, slide-metadata).
//
// The output is the complete offline page. reveal.js and the speaker-notes
// plugin are loaded from the embedded assets, the theme stylesheet from
// templates/themes/<name>/theme.css served under themesURLPrefix, and
// Reveal.initialize enables hash anchors and the navigationMode.
// Print-to-PDF is reveal.js core support: loading the served page with
// `?print-pdf` uses the print styles bundled in the embedded dist/reveal.css,
// so there is no separate print plugin or stylesheet and no export command
// (pdf-via-print). The page's "live-reload" block is an empty named block the
// phase-7 server overrides to inject its client script; RenderDeck leaves it
// wired and does not implement reloading (live-reload).
//
// single-binary: deck rendering stays in-process and offline — the page
// references only root-relative local URLs, the embedded reveal.js under
// assetsURLPrefix and the project theme under themesURLPrefix (or the
// /assets/theme.css fallback), never an external host; the page shell is read
// from the embedded assets.DeckPage, and theme URLs are built with "/" joins
// only, so the output is OS-neutral and identical across all six supported
// targets (darwin/arm64, darwin/amd64, windows/amd64, windows/arm64,
// linux/amd64, linux/arm64) (requirements.requirement.single-binary).
//
// A nil config, deck, registry or slide model, a slide missing its parsed
// structure, and any slide or page execution failure are returned as a
// *RenderError. RenderDeck is deterministic: the same deck always renders to
// the same page HTML.
func RenderDeck(cfg *deck.Config, d *deck.Deck, parsed []*slide.Slide, reg *template.Registry, themeReg *theme.Registry, funcMap htmltmpl.FuncMap) (htmltmpl.HTML, error) {
	if cfg == nil {
		return "", &RenderError{Err: errors.New("nil deck config")}
	}
	if d == nil {
		return "", &RenderError{Err: errors.New("nil deck")}
	}
	if reg == nil {
		return "", &RenderError{Err: errors.New("nil template registry")}
	}
	if themeReg == nil {
		return "", &RenderError{Err: errors.New("nil theme registry")}
	}

	themeCSS, err := themeStylesheet(cfg.Theme, themeReg)
	if err != nil {
		return "", &RenderError{Err: err}
	}

	byFile := make(map[string]*slide.Slide, len(parsed))
	for _, s := range parsed {
		if s != nil {
			byFile[s.File] = s
		}
	}

	// Horizontal slides in the model's order; a horizontal slide with letter
	// slides is wrapped, together with them, in a plain outer <section> whose
	// first child is the numbered slide's own <section> and whose remaining
	// children are the letter slides in order (the vertical stack). The
	// deck's slide-file count is derived once, at render time, and threaded
	// into every slide's reserved `slide` context (slide-metadata).
	total := d.Total()
	var slides strings.Builder
	for i := range d.Stacks {
		stack := &d.Stacks[i]

		horizontal, err := renderDeckSlide(byFile, stack.Slide, cfg, total, reg, funcMap)
		if err != nil {
			return "", err
		}
		if len(stack.Vertical) > 0 {
			var inner strings.Builder
			for j := range stack.Vertical {
				vertical, err := renderDeckSlide(byFile, stack.Vertical[j], cfg, total, reg, funcMap)
				if err != nil {
					return "", err
				}
				inner.WriteString(string(vertical))
			}
			horizontal = htmltmpl.HTML(wrapVerticalStack(string(horizontal), inner.String()))
		}
		slides.WriteString(string(horizontal))
	}

	return renderPage(cfg, themeCSS, slides.String())
}

// renderPage executes the embedded assets.DeckPage page shell for a deck whose
// already-rendered `.reveal > .slides` markup is slides: the document title
// (cfg.Title), the resolved theme stylesheet URL (themeCSS), the reveal.js
// navigationMode (cfg.Navigation) and the trusted slide markup (slides). It is
// the single page-shell construction path shared by RenderDeck (which passes
// every slide's markup) and RenderSlidePage (which passes one selected slide's),
// so the whole-deck page the server serves and the standalone single-slide page
// `kalide render <slide> --full` writes are built from the same shell, head,
// embedded assets and theme stylesheet and differ only in the markup placed
// inside the container. A page parse or execution failure is returned as a
// *RenderError.
func renderPage(cfg *deck.Config, themeCSS, slides string) (htmltmpl.HTML, error) {
	page, err := htmltmpl.ParseFS(assets.DeckPage(), deckPageName)
	if err != nil {
		return "", &RenderError{Err: fmt.Errorf("parse deck page %q: %w", deckPageName, err)}
	}

	data := map[string]any{
		"Title":      cfg.Title,
		"ThemeCSS":   themeCSS,
		"Navigation": cfg.Navigation,
		"Slides":     htmltmpl.HTML(slides),
	}

	var buf bytes.Buffer
	if err := page.ExecuteTemplate(&buf, deckPageName, data); err != nil {
		return "", &RenderError{Err: fmt.Errorf("execute deck page %q: %w", deckPageName, err)}
	}
	return htmltmpl.HTML(buf.String()), nil
}

// RenderSlidePage renders ONE selected slide of a deck as a complete standalone
// page: the same offline reveal.js page shell, head, embedded assets and theme
// stylesheet RenderDeck serves (assets.DeckPage executed through the shared
// renderPage path), holding ONLY the selected slide's own `<section>` — no
// outer vertical-stack wrapper — in its `.reveal > .slides` container.
//
// Unlike the whole-deck page, the container holds just one slide, but that slide
// is rendered from the WHOLE deck's identity: total is d.Total(), the whole
// deck's slide-file count, and meta is the selected slide's own modelled
// position (deck.Slide, supplying PositionLabel), so the embedded `<section>` is
// byte-identical to the `--fragment` markup — internal/render.RenderSlide called
// with the same parsed structure, label, cfg, meta, total, registry and func map
// — and a layout reading `.slide.number`/`.slide.total` sees the same values in
// the standalone page, the served whole-deck page and the fragment
// (requirements.requirement.cli-render). It is deliberately NOT a render of a
// one-slide projection of the deck, which would report total 1.
//
// d is the deck's ordered slide model (internal/deck.LoadSlides, the WHOLE
// deck); meta is the selected slide's modelled position (deck.Slide); parsed is
// that slide's parsed structure (internal/slide.Parse). reg is the populated
// template registry the deck was validated against — typically built from the
// project's templates/ library (template.NewRegistryFromLibrary); themeReg is
// the project theme registry (theme.LoadDir) and must be non-nil — callers
// always construct one from the project's on-disk theme library, so a nil
// themeReg is a caller error and RenderSlidePage reports it rather than silently
// substituting a default. funcMap is the library's layout func map
// (template.LayoutFuncMap), threaded through RenderSlide exactly as RenderDeck
// threads it.
//
// A nil config, deck, registry or theme registry, a slide that cannot be
// rendered, and any page execution failure are returned as a *RenderError, as
// RenderDeck reports them. RenderSlidePage is deterministic: the same deck and
// selected slide always render to the same page HTML.
//
// single-binary: like RenderDeck, the page is built in-process and offline — the
// embedded reveal.js under assetsURLPrefix and the project theme under
// themesURLPrefix (or the /assets/theme.css fallback), never an external host —
// so the output is OS-neutral and identical across all six supported targets
// (darwin/arm64, darwin/amd64, windows/amd64, windows/arm64, linux/amd64,
// linux/arm64) (requirements.requirement.single-binary).
func RenderSlidePage(cfg *deck.Config, d *deck.Deck, meta deck.Slide, parsed *slide.Slide, reg *template.Registry, themeReg *theme.Registry, funcMap htmltmpl.FuncMap) (htmltmpl.HTML, error) {
	if cfg == nil {
		return "", &RenderError{Err: errors.New("nil deck config")}
	}
	if d == nil {
		return "", &RenderError{Err: errors.New("nil deck")}
	}
	if reg == nil {
		return "", &RenderError{Err: errors.New("nil template registry")}
	}
	if themeReg == nil {
		return "", &RenderError{Err: errors.New("nil theme registry")}
	}

	themeCSS, err := themeStylesheet(cfg.Theme, themeReg)
	if err != nil {
		return "", &RenderError{Err: err}
	}

	// The selected slide renders through the same RenderSlide call the
	// `--fragment` path makes — the WHOLE deck's slide-file count as total and
	// the slide's own modelled position as meta — so the embedded <section> is
	// byte-identical to that fragment and .slide.number/.slide.total count
	// across the whole deck (requirements.requirement.cli-render).
	total := d.Total()
	slideHTML, err := RenderSlide(parsed, meta.Label, cfg, meta, total, reg, funcMap)
	if err != nil {
		return "", err
	}

	return renderPage(cfg, themeCSS, string(slideHTML))
}

// renderDeckSlide renders one modelled slide (a horizontal slide or a vertical
// one) through RenderSlide, looking its parsed structure up by its deck path.
// cfg and total are the deck configuration and the deck's slide-file count,
// threaded straight through to RenderSlide so the slide layout executes with
// its reserved `deck` and `slide` context (template-context); funcMap is the
// library's layout func map, threaded through the same way.
func renderDeckSlide(byFile map[string]*slide.Slide, s deck.Slide, cfg *deck.Config, total int, reg *template.Registry, funcMap htmltmpl.FuncMap) (htmltmpl.HTML, error) {
	parsed, ok := byFile[s.Path]
	if !ok || parsed == nil {
		return "", &RenderError{File: s.Path, Err: errors.New("no parsed slide for deck slide")}
	}
	return RenderSlide(parsed, s.Label, cfg, s, total, reg, funcMap)
}

// themeStylesheet resolves a deck config's theme name to the URL of its
// stylesheet, through the project theme registry (theme.LoadDir,
// theme-selection). An empty name resolves to the default theme. An unknown
// name carries the registry's positioned unknown-theme error. A theme with no
// stylesheet yields "" so the deck page's own default link applies. The URL is
// themesURLPrefix + "<name>/<stylesheet>", matching where internal/server's
// mediaHandler serves templates/themes/<name>/** from disk.
func themeStylesheet(name string, reg *theme.Registry) (string, error) {
	if name == "" {
		name = theme.DefaultName
	}
	t, err := reg.Lookup(name)
	if err != nil {
		return "", err
	}
	if t.Stylesheet == "" {
		return "", nil
	}
	return themesURLPrefix + t.Name + "/" + t.Stylesheet, nil
}

// wrapVerticalStack groups a numbered slide's rendered <section> (horizontal)
// with the rendered <section>s of its letter slides (verticals) into one plain
// wrapper <section> with no id or class of its own. reveal.js treats a
// <section> that directly contains other <section>s as a pure vertical-stack
// container: it is not itself a slide, so the numbered slide must remain its
// own child <section> — keeping its own anchor id and its layout classes —
// rather than having the letter slides inserted into it. The wrapper is closed
// by </section>. With empty verticals the horizontal fragment is returned
// unchanged, so a stack with no letter slides stays a single <section>.
func wrapVerticalStack(horizontal string, verticals string) string {
	if verticals == "" {
		return horizontal
	}
	return "<section>" + horizontal + verticals + "</section>"
}
