package server

import (
	"context"
	"errors"
	"fmt"
	htmltmpl "html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/render"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
	"github.com/really-knows-ai/kalide/internal/watch"
)

// This file implements Reloader, the live-reload half of `kalide start`
// (requirements.requirement.live-reload,
// requirements.requirement.live-reload-error-page): the SSE endpoint, the
// client script that connects the open browser to it, and the state machine
// that keeps the served page in step with the watched deck.
//
// It consumes the debounced event channel internal/watch.Watch produces (one
// Event per burst of edits to slides/, assets/ or kalide.yaml) and, for each
// event, re-runs the whole-deck validator and the renderer through an injected
// Pipeline:
//
//   - the deck is valid  -> the freshly rendered deck page becomes the server's
//     current page ((Server).SetPage) and every connected browser is told to
//     reload (an SSE `reload` event);
//   - the deck is invalid -> the current page becomes the embedded full-page
//     error document carrying the single first error rendered by
//     validate.Format, the same string is printed to the terminal, and the
//     server keeps running: the next valid edit swaps the deck back in (the
//     error page carries the same live-reload client script, so the fixing edit
//     reloads it automatically).
//
// The validator and renderer are reached only through the Pipeline seam, so
// internal/cli (`kalide start`) injects nothing and gets the real pipeline,
// while tests inject fakes that drive the valid/invalid/recovery transitions
// without touching the filesystem or the renderer.

const (
	// DefaultReloadPath is the URL of the SSE endpoint the Reloader registers
	// on the server's mux when ReloadOptions.Path is empty. The injected
	// client script connects to exactly this path.
	DefaultReloadPath = "/events"

	// reloadEvent is the SSE message the client script acts on. The server
	// sends it as `data: reload\n\n`; the script reloads the page on it.
	reloadEvent = "reload"

	// liveReloadScript is the client script injected into every served page.
	// The two %s placeholders are the event-stream URL and the reload message,
	// both rendered as Go/JS string literals with strconv.Quote. It is
	// deliberately dependency-free and offline: a browser that cannot open an
	// EventSource simply keeps the page as served.
	liveReloadScript = `<script>
(function () {
  if (typeof EventSource === "undefined") return;
  var source = new EventSource(%s);
  source.onmessage = function (event) {
    if (event.data === %s) { window.location.reload(); }
  };
})();
</script>
`
)

// Pipeline is the validator+renderer seam the Reloader drives on every watch
// event (and once on Start for the initial page when none was supplied).
//
// It receives the deck filesystem rooted at the deck directory and must return
// the Page to publish:
//
//   - a VALID deck yields the rendered deck page with Err == "" — typically
//     NewDeckPage(render.RenderDeck(...));
//   - an INVALID deck yields the full-page error document with Err set to the
//     single formatted first error — typically NewErrorPage(title, verr), whose
//     Err carries validate.Format(verr). This is not a Go error: an invalid
//     deck is a normal, recoverable state, so the Reloader publishes the page
//     and prints the identical Err text to the terminal.
//
// The returned error is reserved for an infrastructure/definition failure (the
// filesystem cannot be read, the renderer fails on an already-valid deck, a
// compiled-in template is broken). It is not fatal: the Reloader reports it and
// keeps the current page until the next event, when the pipeline is retried.
//
// Production callers leave ReloadOptions.Pipeline nil and get the built-in
// pipeline (validate.Validate + deck/slide/render). Tests inject a fake to
// exercise the valid → invalid → recovered sequence deterministically.
type Pipeline func(fsys fs.FS) (*Page, error)

// ReloadOptions configures a Reloader. Only Server is required; the rest have
// working defaults, and Pipeline/Registry are the injection points tests use.
type ReloadOptions struct {
	// Server is the running server the reloader keeps in step: it receives
	// (Server).SetPage on every publish and hosts the SSE route. Required.
	Server *Server

	// Root is the deck directory. It is used to derive the deck filesystem
	// when FS is nil, and it is required by the built-in pipeline; a caller
	// injecting its own Pipeline need not set it.
	Root string

	// FS is the deck filesystem rooted at the deck directory. When nil,
	// os.DirFS(Root) is used. A caller injecting its own Pipeline need not set
	// it: the injected pipeline owns its view of the deck.
	FS fs.FS

	// Events is the debounced change channel from internal/watch.Watch. Each
	// received value triggers one re-validate + re-render. It may be nil (a
	// reloader driven only through Start), in which case Run waits on ctx
	// alone. Closing the channel makes Run return.
	Events <-chan watch.Event

	// Page is the document initially published (with the client script
	// injected). `kalide start` passes the page it already rendered; when nil,
	// Start renders one through Pipeline.
	Page *Page

	// Pipeline re-validates and re-renders the whole deck. When nil, the
	// built-in pipeline is used, which needs Registry (or the built-ins) and
	// Themes. It is the seam task-6 and tests inject through.
	Pipeline Pipeline

	// Registry is the template registry the built-in pipeline validates
	// against. When nil, it is built from the project's templates/ library
	// (template.LoadLibrary(fsys, template.TemplatesDir) +
	// template.NewRegistryFromLibrary), loaded once when the Reloader is
	// built. Unused when Pipeline is set. Tests inject a registry to bypass
	// the on-disk library entirely.
	Registry *template.Registry

	// Themes is the theme registry. When nil, it is built from the project's
	// templates/themes directory (theme.LoadDir), loaded once alongside
	// Registry. Unused when Pipeline is set.
	Themes *theme.Registry

	// Title is the fallback document title used for the error page when the
	// deck config cannot be read to supply one.
	Title string

	// Path is the SSE endpoint path. When empty, DefaultReloadPath is used and
	// must begin with "/".
	Path string

	// Log receives the terminal messages: the single formatted deck error,
	// printed verbatim exactly as it appears in the browser, and any
	// infrastructure failure. When nil, os.Stderr is used.
	Log io.Writer
}

// Reloader keeps a running Server's current Page in step with a watched deck.
// Build one with NewReloader, then Start (or Run). It is safe for the SSE
// handler goroutines to broadcast through it concurrently.
type Reloader struct {
	srv *Server

	// root is the deck directory holding kalide.yaml, retained from
	// ReloadOptions.Root. The built-in pipeline resolves the template-library
	// root against it on every run; the deck fsys alone cannot name an
	// external library root because os.DirFS/fs.ValidPath reject ".."
	// (external-template-library).
	root     string
	fsys     fs.FS
	events   <-chan watch.Event
	pipeline Pipeline
	title    string
	path     string
	log      io.Writer
	initial  *Page

	// regOverride and themesOverride are explicit test overrides supplied
	// through ReloadOptions.Registry/ReloadOptions.Themes; nil when a caller
	// injected its own Pipeline or left them unset. When either is nil, the
	// built-in pipeline (renderDefault) loads the project's templates/
	// library fresh on every pipeline run instead of using a cached one, so a
	// library edit is picked up without restarting the server
	// (never-serve-broken-deck): a broken library or deck publishes the
	// positioned error page, never a stale registry/theme set.
	regOverride    *template.Registry
	themesOverride *theme.Registry

	mu       sync.Mutex
	subs     map[chan struct{}]struct{}
	started  bool
	startErr error

	// done is closed when Run returns (or Stop is called), so SSE handlers
	// blocked on the stream wake up and return before the server waits for
	// them to finish during a graceful shutdown.
	done     chan struct{}
	stopOnce sync.Once
}

// NewReloader builds a Reloader from opts. It does not touch the server or the
// deck: register the SSE route and publish the first page with Start, or call
// Run which does both.
//
// The built-in pipeline is assembled only when opts.Pipeline is nil, so a test
// injecting a fake never builds the built-in registry or reads a deck.
func NewReloader(opts ReloadOptions) (*Reloader, error) {
	if opts.Server == nil {
		return nil, errors.New("server: reload: no server given")
	}

	fsys := opts.FS
	if fsys == nil && opts.Root != "" {
		fsys = os.DirFS(opts.Root)
	}

	path := opts.Path
	if path == "" {
		path = DefaultReloadPath
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("server: reload: path %q must begin with /", path)
	}
	// The path is embedded in the injected client script as a JS string
	// literal (strconv.Quote) and in the mux pattern. Reject the few
	// characters that could break out of the script element or are not valid
	// in a request path; it is developer-supplied, never author-supplied.
	if strings.ContainsAny(path, "<>\"'` \t\r\n") {
		return nil, fmt.Errorf("server: reload: path %q contains an invalid character", path)
	}

	logw := opts.Log
	if logw == nil {
		logw = os.Stderr
	}

	r := &Reloader{
		srv:     opts.Server,
		root:    opts.Root,
		fsys:    fsys,
		events:  opts.Events,
		title:   opts.Title,
		path:    path,
		log:     logw,
		initial: opts.Page,
		subs:    make(map[chan struct{}]struct{}),
		done:    make(chan struct{}),
	}

	// An injected pipeline supplies its own view of the deck; only the
	// built-in one needs a filesystem to read a root deck from.
	if opts.Pipeline != nil {
		r.pipeline = opts.Pipeline
		return r, nil
	}
	if fsys == nil {
		return nil, errors.New("server: reload: no deck root or filesystem given")
	}
	// The built-in pipeline resolves the template-library root against the real
	// deck root on every run, so it always needs one: the deck fs.FS cannot
	// express an external library root (os.DirFS/fs.ValidPath reject "..").
	if opts.Root == "" {
		return nil, errors.New("server: reload: no deck root given")
	}

	r.regOverride = opts.Registry
	r.themesOverride = opts.Themes
	r.pipeline = r.renderDefault
	return r, nil
}

// Start registers the SSE endpoint on the server and publishes the initial
// page with the live-reload client script injected, rendering one through the
// pipeline when ReloadOptions.Page was nil. It is idempotent: only the first
// call does anything, and it returns the same result on every call.
//
// `kalide start` calls Start before printing the URL or opening the browser, so
// the very first page a browser loads already carries the client script. Run
// calls Start itself, so callers that use Run need not call it separately.
func (r *Reloader) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return r.startErr
	}
	r.started = true

	r.srv.Handle(r.path, http.HandlerFunc(r.serveEvents))

	p := r.initial
	if p == nil {
		var err error
		p, err = r.pipeline(r.fsys)
		if err != nil {
			r.startErr = err
			return err
		}
	}
	r.publish(p)
	return nil
}

// Run calls Start and then consumes the watch event channel until ctx is done
// or the channel is closed, re-validating and re-rendering the deck once per
// event. It returns nil on either termination; an error only when the initial
// Start fails.
//
// Run is call-once, and it owns the SSE stream lifetime: when it returns it
// closes the signal the stream handlers select on, so a caller that cancels ctx
// before (Server).Shutdown lets the graceful shutdown finish instead of waiting
// on open event streams. A nil Events channel is valid: Run then waits on ctx
// only.
func (r *Reloader) Run(ctx context.Context) error {
	if err := r.Start(); err != nil {
		return err
	}
	defer r.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-r.events:
			if !ok {
				return nil
			}
			r.Reload()
		}
	}
}

// Reload runs the pipeline once for the whole deck, publishes the resulting
// page (deck or full-page error) and, on a successful pipeline run, broadcasts a
// reload to every connected SSE client. It is the single re-validate + re-render
// step Run performs per watch event, exported so a caller that owns the event
// loop (`kalide start`, or a test driving the state machine directly) can drive
// it without a watch channel.
//
// A pipeline infrastructure failure is reported to the log and leaves the
// current page in place; the next call retries.
func (r *Reloader) Reload() {
	p, err := r.pipeline(r.fsys)
	if err != nil {
		fmt.Fprintln(r.log, "kalide: reload failed:", err)
		return
	}
	r.publish(p)
	r.broadcast()
}

// Stop releases every open SSE stream by closing the signal Run's handlers
// select on, so a graceful (Server).Shutdown is not held open by
// long-lived event-stream connections. It is safe to call more than once and
// from any goroutine; Run calls it on return. Stop does not unregister the SSE
// route or close the server.
func (r *Reloader) Stop() {
	r.stopOnce.Do(func() { close(r.done) })
}

// publish injects the client script into p's document, makes it the server's
// current page, and prints p's error — the single formatted first error — to
// the terminal when it is the error page, so browser and terminal show the
// identical string (requirements.requirement.error-reporting,
// requirements.requirement.live-reload-error-page). A nil p is ignored.
func (r *Reloader) publish(p *Page) {
	if p == nil {
		return
	}
	r.srv.SetPage(&Page{
		Doc:   injectLiveReload(p.Doc, r.path),
		Title: p.Title,
		Err:   p.Err,
	})
	if p.Err != "" {
		fmt.Fprintln(r.log, p.Err)
	}
}

// broadcast wakes every connected SSE client with one reload event. A client
// whose single-slot buffer is already full is skipped: one pending reload is
// enough, and a slow client must never block the reload loop.
func (r *Reloader) broadcast() {
	r.mu.Lock()
	subs := make([]chan struct{}, 0, len(r.subs))
	for ch := range r.subs {
		subs = append(subs, ch)
	}
	r.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// subscribe registers a new SSE client and returns its one-slot signal channel.
func (r *Reloader) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch
}

// unsubscribe removes a client. The channel is never closed, so a concurrent
// broadcast can never panic on a send.
func (r *Reloader) unsubscribe(ch chan struct{}) {
	r.mu.Lock()
	delete(r.subs, ch)
	r.mu.Unlock()
}

// serveEvents is the SSE endpoint: it holds one long-lived response open per
// client, flushing a `reload` message for every broadcast, until the client
// disconnects, the reloader stops, or the server shuts the connection down.
func (r *Reloader) serveEvents(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodHead {
		setEventStreamHeaders(w)
		w.WriteHeader(http.StatusOK)
		return
	}
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	setEventStreamHeaders(w)
	w.WriteHeader(http.StatusOK)
	// An initial comment opens the stream so the browser fires `open` and any
	// intermediate proxy starts forwarding; it is not a reload.
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	ch := r.subscribe()
	defer r.unsubscribe(ch)

	for {
		select {
		case <-req.Context().Done():
			return
		case <-r.done:
			return
		case <-ch:
			_, _ = io.WriteString(w, "data: "+reloadEvent+"\n\n")
			flusher.Flush()
		}
	}
}

// setEventStreamHeaders writes the headers every SSE response needs, including
// the no-store directive so a reload is never cached.
func setEventStreamHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

// injectLiveReload inserts the SSE client script into doc at the pages'
// "live-reload" block position: that named block is empty and sits immediately
// before the closing </body> of every page (deck.html.tmpl, error.html.tmpl and
// gallery.html.tmpl), so inserting before the final </body> is exactly where
// the block renders. internal/render owns the deck page's parse and does not
// expose the block to server, so the insertion is done on the rendered document
// rather than by overriding the template block; the result is the same and no
// render API is changed. A document with no </body> gets the script appended.
func injectLiveReload(doc htmltmpl.HTML, eventPath string) htmltmpl.HTML {
	if doc == "" {
		return doc
	}
	script := fmt.Sprintf(liveReloadScript, strconv.Quote(eventPath), strconv.Quote(reloadEvent))
	s := string(doc)
	if i := strings.LastIndex(s, "</body>"); i >= 0 {
		return htmltmpl.HTML(s[:i] + script + s[i:])
	}
	return htmltmpl.HTML(s + script)
}

// renderDefault is the built-in Pipeline: the fail-fast whole-deck validator
// followed, when the deck is valid, by the deck loader + slide parser +
// renderer. It mirrors the fixed deterministic order
// (requirements.requirement.whole-deck-validation): a validation failure is
// published as the full-page error with its single formatted error; only a
// valid deck is parsed and rendered.
//
// On every call it re-loads the project's templates/ library (registry +
// themes) from fsys via loadLibrary, unless a test explicitly overrode
// Registry/Themes, so an edit to the library is picked up on the very next
// reload without restarting the server. A library that fails to load is
// reported the same way as an invalid deck — the full-page error, never a
// stale or partially-loaded library (never-serve-broken-deck).
func (r *Reloader) renderDefault(fsys fs.FS) (*Page, error) {
	lib, reg, themes, err := r.loadLibrary(fsys)
	if err != nil {
		return NewErrorPage(r.title, validate.New("templates", 0, nil, err.Error(), ""))
	}

	var mediaFS fs.FS
	if lib != nil {
		mediaFS = lib.Media
	}
	funcMap := template.LayoutFuncMap(mediaFS, MediaPath)

	if verr, invalid := validate.Validate(fsys, reg, themes); invalid {
		return NewErrorPage(r.errorTitle(fsys, themes), verr)
	}

	cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themes)
	if err != nil {
		return nil, err
	}
	d, err := deck.LoadSlides(fsys, deck.SlidesDir)
	if err != nil {
		return nil, err
	}
	parsed, err := parseAllSlides(fsys, d, reg)
	if err != nil {
		return nil, err
	}
	doc, err := render.RenderDeck(cfg, d, parsed, reg, themes, funcMap)
	// render.RenderDeck threads cfg and each slide's deck.Slide metadata
	// (plus d's slide-file total) through to render.RenderSlide itself, so
	// every served slide layout and every section instance at every depth
	// executes with the reserved `.deck`/`.slide` context
	// (requirement.template-context, requirement.section-template-context).
	if err != nil {
		return nil, err
	}
	return NewDeckPage(doc, cfg.Title), nil
}

// loadLibrary returns the collaborators the built-in pipeline validates and
// renders against for one pipeline run. When Registry and Themes were both
// given as explicit test overrides (ReloadOptions.Registry/Themes), it returns
// them unchanged and never touches the deck — the override escape hatch.
//
// Otherwise it resolves the deck's template-library root afresh from the deck
// config on every call and loads that root: a configured `templates:` path
// (relative resolved against the retained deck root, absolute allowed) is
// authoritative, else the local templates/ (external-template-library). The
// resolution runs against the retained real deck root (r.root), never the
// process cwd and never the `..`-rejecting deck fs.FS, so an external library
// is reachable; nothing is cached, so retargeting `templates:` in kalide.yaml
// or editing the library is reflected on the next call. The registry and
// themes are built from the resolved root. An unresolvable root or library is
// returned as the positioned templates error naming the resolved path, with no
// fallback to a local templates/ (never-serve-broken-deck).
func (r *Reloader) loadLibrary(fsys fs.FS) (*template.Library, *template.Registry, *theme.Registry, error) {
	if r.regOverride != nil && r.themesOverride != nil {
		return nil, r.regOverride, r.themesOverride, nil
	}

	lib, err := template.LoadDeckLibrary(r.root, configuredTemplates(fsys))
	if err != nil {
		return nil, nil, nil, err
	}

	reg := r.regOverride
	if reg == nil {
		reg, err = template.NewRegistryFromLibrary(lib)
		if err != nil {
			return lib, nil, nil, err
		}
	}

	themes := r.themesOverride
	if themes == nil {
		themes, err = theme.LoadDir(os.DirFS(lib.RootPath), template.ThemesDir)
		if err != nil {
			return lib, reg, nil, err
		}
	}

	return lib, reg, themes, nil
}

// configuredTemplates returns the deck's raw top-level `templates:` value as
// written in kalide.yaml, or "" when the file cannot be read, the key is absent
// or empty, or the config cannot be decoded. It is the deck-side input to the
// template loader's single resolution point: it deliberately does not validate
// the config (deck.LoadConfig needs a theme registry, which itself comes from
// the not-yet-resolved library) — the whole-deck validator reports any config
// error afterwards, through the themes loaded from the resolved root.
func configuredTemplates(fsys fs.FS) string {
	data, err := fs.ReadFile(fsys, deck.ConfigFile)
	if err != nil {
		return ""
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ""
	}
	configured, _ := raw["templates"].(string)
	return configured
}

// errorTitle is the title shown on the error page: the deck's configured title
// when the config can still be read, otherwise the configured fallback. When
// themes is nil (the project templates/ library failed to load, so
// deck.LoadConfig has nothing to resolve `theme` against), it falls back to
// the configured title directly rather than calling LoadConfig at all.
func (r *Reloader) errorTitle(fsys fs.FS, themes *theme.Registry) string {
	if themes == nil {
		return r.title
	}
	if cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themes); err == nil && cfg.Title != "" {
		return cfg.Title
	}
	return r.title
}

// parseAllSlides parses every slide of d in model order — a horizontal slide
// before its vertical children — from fsys, keyed by slide path via
// slide.Slide.File, the convention internal/render.RenderDeck and
// internal/validate use.
func parseAllSlides(fsys fs.FS, d *deck.Deck, reg *template.Registry) ([]*slide.Slide, error) {
	var parsed []*slide.Slide
	if d == nil {
		return parsed, nil
	}
	for i := range d.Stacks {
		stack := &d.Stacks[i]
		ordered := make([]deck.Slide, 0, 1+len(stack.Vertical))
		ordered = append(ordered, stack.Slide)
		ordered = append(ordered, stack.Vertical...)
		for _, s := range ordered {
			src, err := fs.ReadFile(fsys, s.Path)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", s.Path, err)
			}
			ps, err := slide.Parse(s.Path, src, reg)
			if err != nil {
				return nil, err
			}
			parsed = append(parsed, ps)
		}
	}
	return parsed, nil
}
