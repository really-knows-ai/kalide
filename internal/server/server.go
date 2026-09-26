// Package server serves a validated deck over HTTP on the loopback interface.
//
// It is the http-server component of `eypres start`
// (solution.component.http-server): `eypres start` validates the whole deck
// first, then hands the rendered page to Listen, which binds 127.0.0.1 only and
// serves the presentation together with every asset it references.
//
// Binding (requirements.requirement.cli-start, solution.note.design-decisions):
// the server binds 127.0.0.1 only, never another interface. With no explicit
// port it tries the default 8080 and falls back through 8081..8099 in order
// (20 ports); if every one is busy Listen returns a plain-language error
// advising the author to choose a port with --port. An explicit port is used
// exactly: if it is busy Listen returns an error naming that port and
// suggesting another port or omitting --port — it never silently falls back.
// The bound URL is available from (Server).URL.
//
// Serving: the current Page is served at "/" (a rendered deck, or the
// full-page error document), and the AssetsPath prefix serves the embedded
// internal/assets tree (reveal.js, fonts, logo, theme.css, pages) first and the
// deck's own assets/ directory after it, so the deck page's `/assets/…` URLs
// and author image paths written `assets/…` both resolve
// (global.constraint.go-static-embedded-binary).
//
// This unit is the listener and the static serving plus the /templates
// gallery; it does NOT implement live reload or the SSE endpoint (phase-7
// task 4, reload.go). It exposes the seams those tasks build on:
//
//   - Page is the document served at "/"; (Server).SetPage swaps it, so a
//     reloader can publish a freshly rendered deck or a full-page error
//     without touching the listener.
//   - (Server).Handle registers an additional route (the SSE endpoint) on the
//     same mux and listener. Listen registers the /templates gallery itself.
//   - NewErrorPage renders the embedded full-page error shell from the single
//     formatted validation error — the document a reloader publishes when a
//     watched edit breaks the deck (requirements.requirement.live-reload-error-page).
//
// internal/cli consumes Listen for `eypres start` (phase-7 task 6): once the
// deck validates it calls Listen, prints (Server).URL, and releases the port
// with (Server).Shutdown.
package server

import (
	"bytes"
	"context"
	"fmt"
	htmltmpl "html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/really-knows-ai/ey-present/internal/assets"
	"github.com/really-knows-ai/ey-present/internal/validate"
)

const (
	// Host is the interface the server binds, and the only one it ever binds.
	// It is a loopback address, so the deck is reachable from this machine
	// only (requirements.requirement.cli-start).
	Host = "127.0.0.1"

	// DefaultPort is the first port tried when the author does not pass
	// --port.
	DefaultPort = 8080

	// MaxPort is the last port tried before Listen gives up; the fallback is
	// bounded to DefaultPort..MaxPort, 20 ports in total.
	MaxPort = 8099

	// AssetsPath is the URL prefix under which the embedded asset tree and
	// the deck's assets/ directory are mounted. It is the prefix
	// internal/render builds every deck-page asset URL from
	// (render's assetsURLPrefix), so "/assets/reveal/dist/reveal.js",
	// "/assets/theme.css" and an author's "assets/pic.png" all resolve here.
	AssetsPath = "/assets/"

	// rootPath is the pattern of the deck-page handler: every request that is
	// not under AssetsPath reaches handlePage, which serves "/" and 404s the
	// rest.
	rootPath = "/"

	// deckAssetsDir is the deck's own asset directory at the root of a deck,
	// matching internal/watch.AssetsDir and internal/deck's slide layout.
	deckAssetsDir = "assets"

	// errorPageName is the full-page error shell's path within the embedded
	// pages sub-tree (assets.Pages()).
	errorPageName = "error.html.tmpl"

	// readHeaderTimeout bounds how long a client may take to send its request
	// headers, so an idle connection cannot pin a goroutine.
	readHeaderTimeout = 10 * time.Second
)

// Page is the document the server currently serves at "/". It is the seam the
// phase-7 reloader swaps: rendering a fresh deck or a full-page error produces
// a new Page and SetPage publishes it atomically, so a request never observes a
// half-rendered document.
type Page struct {
	// Doc is the complete HTML document served at "/". When empty, requests
	// receive a 503 instead.
	Doc htmltmpl.HTML

	// Title is the deck title, or "" when unknown (the pages fall back to
	// "eypres").
	Title string

	// Err is the formatted first validation error shown when Doc is the
	// error page, and "" when Doc is a valid deck. It is informational: Doc
	// already contains the rendered message.
	Err string
}

// NewDeckPage returns the Page serving a rendered deck document, as produced by
// internal/render.RenderDeck. title is the deck title.
func NewDeckPage(doc htmltmpl.HTML, title string) *Page {
	return &Page{Doc: doc, Title: title}
}

// NewErrorPage renders the embedded full-page error shell for verr, the single
// first validation error, and returns it as the Page to serve. It applies
// validate.Format exactly as terminal output does, so the browser and the
// terminal show the identical message
// (requirements.requirement.error-reporting,
// requirements.requirement.live-reload-error-page). title is the deck title.
//
// The phase-7 reloader calls this when a watched edit breaks the deck, then
// publishes the result with SetPage. The server itself keeps running and
// recovers on the next valid render.
func NewErrorPage(title string, verr validate.ValidationError) (*Page, error) {
	page, err := htmltmpl.New(errorPageName).ParseFS(assets.Pages(), errorPageName)
	if err != nil {
		return nil, fmt.Errorf("server: parse error page %q: %w", errorPageName, err)
	}
	message := validate.Format(verr)
	var buf bytes.Buffer
	if err := page.ExecuteTemplate(&buf, errorPageName, map[string]any{
		"Title":   title,
		"Message": message,
	}); err != nil {
		return nil, fmt.Errorf("server: execute error page %q: %w", errorPageName, err)
	}
	return &Page{Doc: htmltmpl.HTML(buf.String()), Title: title, Err: message}, nil
}

// Options configures Listen.
type Options struct {
	// Port is the explicitly requested port. Zero means no --port was given:
	// the default 8080 is tried, then 8081..8099 in order. A non-zero port is
	// used exactly; if it is busy Listen fails rather than falling back.
	Port int

	// Root is the deck directory. Its assets/ subdirectory is served at
	// AssetsPath after the embedded tree, so an author's `assets/…` image
	// paths resolve. Empty (with no FS) disables deck assets.
	Root string

	// FS is the filesystem holding the deck, rooted at the deck directory.
	// When nil, os.DirFS(Root) is used; a test may pass an in-memory
	// filesystem instead. Only its assets/ subdirectory is served.
	FS fs.FS

	// Page is the document initially served at "/". When nil the server
	// answers 503 until SetPage supplies one; `eypres start` always passes
	// the rendered deck.
	Page *Page
}

// Server is a running HTTP server bound to loopback. Obtain one from Listen.
type Server struct {
	url     string
	ln      net.Listener
	mux     *http.ServeMux
	httpSrv *http.Server
	page    atomic.Pointer[Page]
}

// Listen binds the server to 127.0.0.1 and starts serving.
//
// Port selection follows cli-start: with Options.Port == 0 it tries
// DefaultPort and then 8081..8099 in order; when all 20 are busy it returns an
// error advising --port. A non-zero Options.Port is used exactly and a busy one
// returns an error naming the port and suggesting another or omitting --port,
// with no fallback. In every case only 127.0.0.1 is bound.
//
// The returned Server is already accepting requests, so the caller should
// register any extra routes with Handle before printing the URL or opening a
// browser. Stop it with Shutdown or Close to release the port.
func Listen(opts Options) (*Server, error) {
	ln, err := listen(opts.Port)
	if err != nil {
		return nil, err
	}

	s := &Server{
		url: "http://" + ln.Addr().String() + rootPath,
		ln:  ln,
	}
	s.page.Store(opts.Page)

	mux := http.NewServeMux()
	mux.HandleFunc(rootPath, s.handlePage)
	mux.Handle(AssetsPath, http.StripPrefix(AssetsPath, newAssetHandler(assets.FS, deckAssetsFS(opts))))

	s.mux = mux
	// The /templates gallery (phase-7 task 5) is a fixed route every server
	// serves, so it is registered here rather than by the caller: `eypres
	// start` prints the URL only after Listen returns, so the gallery is
	// reachable as soon as the deck is. It is populated from the compiled-in
	// template registry (gallery.go).
	s.Handle(galleryPath, galleryHandler(nil))

	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() { _ = s.httpSrv.Serve(ln) }()
	return s, nil
}

// listen binds a loopback listener for an explicit port (non-zero, no
// fallback) or selects the first free port in DefaultPort..MaxPort.
func listen(port int) (net.Listener, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port %d: choose a port between 1 and 65535", port)
	}
	if port != 0 {
		return listenExplicit(port)
	}
	for p := DefaultPort; p <= MaxPort; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(Host, strconv.Itoa(p)))
		if err == nil {
			return ln, nil
		}
		if !isAddrInUse(err) {
			return nil, fmt.Errorf("listen on %s:%d: %w", Host, p, err)
		}
	}
	return nil, fmt.Errorf("no free port in %d–%d: choose a port with --port", DefaultPort, MaxPort)
}

// listenExplicit binds exactly the requested loopback port. A busy port is an
// error naming it, never a fallback (requirements.requirement.cli-start).
func listenExplicit(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(Host, strconv.Itoa(port)))
	if err != nil {
		if isAddrInUse(err) {
			return nil, fmt.Errorf("port %d is already in use: choose another port with --port, or omit --port and let eypres pick a free one", port)
		}
		return nil, fmt.Errorf("listen on %s:%d: %w", Host, port, err)
	}
	return ln, nil
}

// isAddrInUse reports whether err is the socket address-in-use condition, on
// every supported target. Go's error text is stable, English and
// platform-specific: Unix reports "address already in use" and Windows reports
// "Only one usage of each socket address …". The check is deliberately
// text-based rather than an errno comparison because Windows raises
// WSAEADDRINUSE, which the Unix syscall.EADDRINUSE does not match, and this
// file must build for both targets without platform-specific files.
//
// It is only consulted after a net.Listen has failed, and its only effect is to
// choose between the busier-port branches (try the next port, or the
// explicit-port error) and re-reporting the bind failure. A false positive
// therefore only re-describes a busy port; it can never hide a real failure or
// bind anywhere but 127.0.0.1.
func isAddrInUse(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address")
}

// deckAssetsFS returns the deck's assets/ subdirectory as a filesystem rooted
// at it, or nil when no deck filesystem was given. A missing assets/
// directory yields a sub-filesystem whose lookups simply fail, which the asset
// handler treats as "nothing here".
func deckAssetsFS(opts Options) fs.FS {
	base := opts.FS
	if base == nil {
		if opts.Root == "" {
			return nil
		}
		base = os.DirFS(opts.Root)
	}
	sub, err := fs.Sub(base, deckAssetsDir)
	if err != nil {
		return nil
	}
	return sub
}

// URL returns the address to print and open, for example
// "http://127.0.0.1:8080/". It carries the actual bound port, so it reflects a
// fallback port when 8080 was busy.
func (s *Server) URL() string { return s.url }

// Addr returns the listener's bound address.
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// SetPage atomically replaces the document served at "/". It is the seam the
// phase-7 reloader uses to publish a freshly rendered deck or a full-page
// error; a request sees either the old document or the new one, never a
// partial one.
func (s *Server) SetPage(p *Page) { s.page.Store(p) }

// Handle registers an additional route on the server's mux, for the phase-7
// SSE endpoint and /templates gallery. It must be called before the URL is
// opened in a browser; registering a duplicate pattern panics, as
// http.ServeMux does.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

// Shutdown gracefully stops the server, releasing the listening port, and
// returns once in-flight requests have finished or ctx is done. It is the
// clean shutdown `eypres start` performs on SIGINT/SIGTERM or Ctrl+C/Ctrl+Break
// (requirements.requirement.cli-start).
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

// Close stops the server immediately, releasing the port. It is Shutdown with a
// background context, for callers with no deadline.
func (s *Server) Close() error { return s.Shutdown(context.Background()) }

// handlePage serves the current Page at "/" and 404s every other non-asset
// path, so an unknown URL never masquerades as the deck.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != rootPath {
		http.NotFound(w, r)
		return
	}
	p := s.page.Load()
	if p == nil || p.Doc == "" {
		http.Error(w, "no deck is loaded", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The document is rebuilt on every reload, so it must never be cached.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, string(p.Doc))
}
