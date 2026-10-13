package cli

// This file implements `kalide render [slide] [-o dir] [--full|--fragment]`
// (phase-10 task 2): requirements.requirement.cli-render.
//
// render writes a deck's rendered slide HTML without serving it: no server, no
// port, no network (single-binary). It loads the deck's resolved library
// layers and theme exactly as `kalide start` does, validates the whole deck
// fail-fast, and then:
//
//   - with a slide argument, writes the selected slide's HTML to stdout;
//   - without one, writes one file per slide into the directory named by
//     `-o dir`, each named for its slide's filename stem —
//     `<number>[letter]-<label>.html` (e.g. `3-team.html` for
//     `slides/3-team.md`) — in deck order.
//
// The slide argument selects by position label first (deck.Slide.PositionLabel:
// `3` or `3a`), then by the deck-unique label (the filename's `<label>`); a
// position-label match takes precedence, and an argument matching no slide is
// an author error with a closest-match suggestion.
//
// `--full` (the default) writes a complete standalone page whose reveal.js
// container holds that slide: the same page shell, head, embedded assets and
// theme stylesheet the server serves, built through
// internal/render.RenderSlidePage over the whole deck so the embedded
// `<section>` is byte-identical to the `--fragment` output.
// `--fragment` writes only the slide's `<section>` markup — the fragment the
// deck page embeds — through internal/render.RenderSlide. The two flags are
// mutually exclusive; giving both is a usage error (exit 2). Output is
// deterministic: the same deck renders byte-identical HTML across runs, in
// stable deck order with the slide's label as its anchor id.
//
// A validation error exits 1 in the error-reporting format; a usage error
// exits 2.

import (
	"fmt"
	htmltmpl "html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/displaypath"
	"github.com/really-knows-ai/kalide/internal/render"
	"github.com/really-knows-ai/kalide/internal/server"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/suggest"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// renderOptions are the parsed `kalide render` flags.
type renderOptions struct {
	// slide is the positional slide selector — a position label or a
	// deck-unique label — or "" when none was given (the one-file-per-slide
	// form).
	slide string

	// outDir is the `-o` directory the one-file-per-slide form writes into,
	// or "" when the flag was omitted.
	outDir string

	// fragment selects `--fragment`: only the slide's `<section>` markup. When
	// false the default `--full` page is written.
	fragment bool
}

// runRender implements `kalide render [slide] [-o dir] [--full|--fragment]`.
// It returns the process status code: 0 on success, 1 on a validation or
// author error, 2 for a usage error.
func runRender(args []string, stdout, stderr io.Writer) int {
	opts, err := parseRenderArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "kalide render: %v\n\n", err)
		fmt.Fprint(stderr, usage)
		return 2
	}

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "kalide render: %v\n", err)
		return 1
	}
	root = displaypath.CanonicalRoot(root)

	deckFS := os.DirFS(root)
	configured := ""
	if data, readErr := fs.ReadFile(deckFS, deck.ConfigFile); readErr == nil {
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) == nil {
			configured, _ = raw["templates"].(string)
		}
	}

	library, err := template.LoadDeckLibrary(root, configured)
	if err != nil {
		fmt.Fprintf(stderr, "kalide render: %v\n", err)
		return 1
	}
	themeReg, err := loadLibraryThemeRegistry(library)
	if err != nil {
		fmt.Fprintf(stderr, "kalide render: %v\n", err)
		return 1
	}
	registry, err := template.NewRegistryFromLibrary(library)
	if err != nil {
		fmt.Fprintf(stderr, "kalide render: %v\n", err)
		return 1
	}

	// Validate the whole deck fail-fast before rendering anything: a broken
	// deck exits 1 with the single first error in the error-reporting format,
	// byte-for-byte the string `kalide start` prints (error-reporting).
	if verr, invalid := validate.Validate(deckFS, registry, themeReg); invalid {
		fmt.Fprintln(stderr, validate.Format(verr))
		return 1
	}

	cfg, err := deck.LoadConfig(deckFS, deck.ConfigFile, themeReg)
	if err != nil {
		fmt.Fprintf(stderr, "kalide render: %v\n", err)
		return 1
	}
	d, err := deck.LoadSlides(deckFS, deck.SlidesDir)
	if err != nil {
		fmt.Fprintf(stderr, "kalide render: %v\n", err)
		return 1
	}

	// The layout func map's fallback `media` helper binds the primary layer's
	// media at the primary served base, exactly as the server's reload pipeline
	// builds it; internal/render dispatches each layout to its owning layer.
	funcMap := template.LayoutFuncMap(library.Media, server.MediaPath)

	if opts.slide != "" {
		meta, ok := findSlide(d, opts.slide)
		if !ok {
			fmt.Fprintln(stderr, renderUnknownSlideMessage(opts.slide, slideSelectors(d)))
			return 1
		}
		parsed, err := parseDeckSlide(deckFS, meta.Path, registry)
		if err != nil {
			fmt.Fprintf(stderr, "kalide render: %v\n", err)
			return 1
		}
		html, err := renderOneSlide(cfg, d, meta, parsed, registry, themeReg, funcMap, opts.fragment)
		if err != nil {
			fmt.Fprintf(stderr, "kalide render: %v\n", err)
			return 1
		}
		fmt.Fprint(stdout, html)
		return 0
	}

	// The one-file-per-slide form requires -o (parseRenderArgs enforces it).
	if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "kalide render: %v\n", err)
		return 1
	}
	for _, meta := range deckOrder(d) {
		parsed, err := parseDeckSlide(deckFS, meta.Path, registry)
		if err != nil {
			fmt.Fprintf(stderr, "kalide render: %v\n", err)
			return 1
		}
		html, err := renderOneSlide(cfg, d, meta, parsed, registry, themeReg, funcMap, opts.fragment)
		if err != nil {
			fmt.Fprintf(stderr, "kalide render: %v\n", err)
			return 1
		}
		name := meta.PositionLabel() + "-" + meta.Label + ".html"
		if err := os.WriteFile(filepath.Join(opts.outDir, name), []byte(html), 0o644); err != nil {
			fmt.Fprintf(stderr, "kalide render: %v\n", err)
			return 1
		}
	}
	return 0
}

// parseRenderArgs parses the arguments after the `render` command word: an
// optional slide selector, an optional `-o dir` (or `-o=dir`) and the
// mutually exclusive `--full`/`--fragment` flags. Anything else, a missing
// `-o` directory, both output flags, or the no-slide form without `-o` is a
// usage error.
func parseRenderArgs(args []string) (renderOptions, error) {
	var opts renderOptions
	sawFull, sawFragment := false, false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--full":
			sawFull = true
		case arg == "--fragment":
			sawFragment = true
		case arg == "-o":
			if i+1 >= len(args) {
				return renderOptions{}, fmt.Errorf("option %s needs a directory", arg)
			}
			i++
			opts.outDir = args[i]
		case strings.HasPrefix(arg, "-o="):
			opts.outDir = strings.TrimPrefix(arg, "-o=")
		case strings.HasPrefix(arg, "-"):
			return renderOptions{}, fmt.Errorf("unknown option %q", arg)
		default:
			if opts.slide != "" {
				return renderOptions{}, fmt.Errorf("unexpected argument %q", arg)
			}
			opts.slide = arg
		}
	}

	if sawFull && sawFragment {
		return renderOptions{}, fmt.Errorf("options --full and --fragment are mutually exclusive")
	}
	opts.fragment = sawFragment
	if opts.slide == "" && opts.outDir == "" {
		return renderOptions{}, fmt.Errorf("no slide given and no -o directory: name a slide or pass -o")
	}
	return opts, nil
}

// renderOneSlide renders one slide either as its standalone `--full` page or as
// its `--fragment` `<section>` markup. d is the WHOLE deck: the fragment and the
// standalone page both render the selected slide from the whole deck's identity,
// so the embedded `<section>` is byte-identical and `.slide.number`/`.slide.total`
// count across the whole deck (requirements.requirement.cli-render).
func renderOneSlide(cfg *deck.Config, d *deck.Deck, meta deck.Slide, parsed *slide.Slide, reg *template.Registry, themeReg *theme.Registry, funcMap htmltmpl.FuncMap, fragment bool) (string, error) {
	if fragment {
		frag, err := render.RenderSlide(parsed, meta.Label, cfg, meta, d.Total(), reg, funcMap)
		if err != nil {
			return "", err
		}
		return string(frag), nil
	}
	// The standalone page reuses the shared page construction
	// (internal/render.RenderSlidePage over the deck-page shell, head, embedded
	// assets and theme stylesheet) and holds only this slide's `<section>`,
	// rendered from the whole deck's identity so it matches the `--fragment`
	// output byte for byte.
	page, err := render.RenderSlidePage(cfg, d, meta, parsed, reg, themeReg, funcMap)
	if err != nil {
		return "", err
	}
	return string(page), nil
}

// parseDeckSlide reads and parses one slide file by its deck-relative path.
func parseDeckSlide(fsys fs.FS, path string, reg *template.Registry) (*slide.Slide, error) {
	src, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err
	}
	return slide.Parse(path, src, reg)
}

// deckOrder returns the deck's slides in presentation order: each horizontal
// stack slide followed by its vertical slides, in the model's order.
func deckOrder(d *deck.Deck) []deck.Slide {
	if d == nil {
		return nil
	}
	out := make([]deck.Slide, 0, d.Total())
	for i := range d.Stacks {
		out = append(out, d.Stacks[i].Slide)
		out = append(out, d.Stacks[i].Vertical...)
	}
	return out
}

// findSlide returns the slide selected by sel: a position-label match first
// (deck.Slide.PositionLabel), then a deck-unique label match. ok is false when
// no slide matches either.
func findSlide(d *deck.Deck, sel string) (deck.Slide, bool) {
	slides := deckOrder(d)
	for _, s := range slides {
		if s.PositionLabel() == sel {
			return s, true
		}
	}
	for _, s := range slides {
		if s.Label == sel {
			return s, true
		}
	}
	return deck.Slide{}, false
}

// slideSelectors returns the names a slide can be selected by — its position
// label and its deck-unique label — in deck order, de-duplicated, for the
// closest-match suggestion of an unknown selector.
func slideSelectors(d *deck.Deck) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range deckOrder(d) {
		for _, name := range []string{s.PositionLabel(), s.Label} {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// renderUnknownSlideMessage builds the author-facing message for a slide
// selector that matches no slide: the offending selector, the closest-match
// "did you mean …?" suggestion when internal/suggest finds one, and the sorted
// list of slides it could name. It mirrors templateUnknownMessage's style.
func renderUnknownSlideMessage(sel string, selectors []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "kalide render: unknown slide %q", sel)
	if s := suggest.Closest(sel, selectors); s != "" {
		fmt.Fprintf(&b, ": did you mean %q?", s)
	}
	if len(selectors) > 0 {
		fmt.Fprintf(&b, " (available slides: %s)", strings.Join(selectors, ", "))
	}
	return b.String()
}
