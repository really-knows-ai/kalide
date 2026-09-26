package server

import (
	"bufio"
	"bytes"
	"fmt"
	htmltmpl "html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// fixtureLibraryDir is the phase-3 fixture library's project root, relative
// to this package, holding templates/slides/hello, templates/sections/item,
// templates/themes/plain and templates/media/logo.svg.
const fixtureLibraryDir = "../template/testdata/library"

// TestServer covers the phase-7 unit surface of internal/server: port
// selection and its error messages, the Page seam and recovery through the
// Reloader's Pipeline, the SSE reload broadcast, the /templates gallery and
// embedded asset serving.
func TestServer(t *testing.T) {
	t.Run("port selection default 8080 when free", func(t *testing.T) {
		probe, err := net.Listen("tcp", net.JoinHostPort(Host, strconv.Itoa(DefaultPort)))
		if err != nil {
			t.Skipf("port %d is not free in this environment: %v", DefaultPort, err)
		}
		if err := probe.Close(); err != nil {
			t.Fatalf("release probe listener: %v", err)
		}

		s, err := Listen(Options{Page: testPage("DECK")})
		if err != nil {
			t.Fatalf("Listen: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })

		if got := listenerPort(s); got != DefaultPort {
			t.Fatalf("bound port %d, want default %d", got, DefaultPort)
		}
	})

	t.Run("port selection falls back in order 8080..8099", func(t *testing.T) {
		// Anchor on the default port: it must be free for this process to
		// occupy it, otherwise the environment already blocks the first
		// fallback step and the ordering claim cannot be tested deterministically.
		held := make([]net.Listener, 0, 3)
		for p := DefaultPort; p < DefaultPort+3 && p <= MaxPort; p++ {
			ln, err := net.Listen("tcp", net.JoinHostPort(Host, strconv.Itoa(p)))
			if err != nil {
				if p == DefaultPort {
					t.Skipf("port %d is not free in this environment: %v", DefaultPort, err)
				}
				// Externally busy is just as good for the "in order" claim.
				continue
			}
			held = append(held, ln)
		}
		t.Cleanup(func() {
			for _, ln := range held {
				_ = ln.Close()
			}
		})

		// The next free port after every busy one is where Listen must land.
		free := probeFreePorts(t, DefaultPort, MaxPort)
		if len(free) == 0 {
			t.Fatalf("no free port in %d–%d after releasing probes", DefaultPort, MaxPort)
		}
		want := free[0]
		if want == DefaultPort {
			t.Fatalf("probe reported %d free although it should be held", want)
		}

		s, err := Listen(Options{Page: testPage("DECK")})
		if err != nil {
			t.Fatalf("Listen with ports %d..%d busy: %v", DefaultPort, DefaultPort+2, err)
		}
		t.Cleanup(func() { _ = s.Close() })

		if got := listenerPort(s); got != want {
			t.Fatalf("bound port %d, want %d (first free in %d–%d after busy %d..%d)",
				got, want, DefaultPort, MaxPort, DefaultPort, DefaultPort+2)
		}
		if !strings.HasSuffix(s.URL(), ":"+strconv.Itoa(want)+"/") {
			t.Fatalf("URL %q does not carry the fallback port %d", s.URL(), want)
		}
	})

	t.Run("port selection all 20 busy errors advising --port", func(t *testing.T) {
		// Occupying the whole range can race a port that an external process
		// releases during the subtest; retry a bounded number of times so the
		// assertion tests Listen's behaviour, not the host's port churn.
		var err error
		var s *Server
		for attempt := 1; attempt <= 3; attempt++ {
			held := occupyAll(t, DefaultPort, MaxPort)
			s, err = Listen(Options{Page: testPage("DECK")})
			var busyPort int
			if err == nil {
				busyPort = listenerPort(s)
				_ = s.Close()
			}
			for _, ln := range held {
				_ = ln.Close()
			}
			if err != nil {
				break
			}
			if attempt == 3 {
				t.Fatalf("Listen succeeded on port %d; every port in %d–%d should have been busy", busyPort, DefaultPort, MaxPort)
			}
		}
		if err == nil {
			t.Fatal("Listen succeeded with every port in range busy; want a no-free-port error")
		}
		msg := err.Error()
		for _, want := range []string{strconv.Itoa(DefaultPort), strconv.Itoa(MaxPort), "--port", "choose a port"} {
			if !strings.Contains(msg, want) {
				t.Errorf("all-busy error %q does not mention %q", msg, want)
			}
		}
	})

	t.Run("port selection explicit busy port errors with no fallback", func(t *testing.T) {
		ln, err := net.Listen("tcp", net.JoinHostPort(Host, "0"))
		if err != nil {
			t.Fatalf("reserve a port: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		port := ln.Addr().(*net.TCPAddr).Port

		s, err := Listen(Options{Port: port, Page: testPage("DECK")})
		if err == nil {
			_ = s.Close()
			t.Fatalf("Listen on busy explicit port %d succeeded; want an error (explicit ports never fall back)", port)
		}
		msg := err.Error()
		if !strings.Contains(msg, strconv.Itoa(port)) {
			t.Errorf("explicit-busy error %q does not name port %d", msg, port)
		}
		if !strings.Contains(msg, "--port") {
			t.Errorf("explicit-busy error %q does not suggest --port", msg)
		}
		if !strings.Contains(msg, "kalide") {
			t.Errorf("explicit-busy error %q does not name kalide", msg)
		}
	})

	t.Run("binds loopback only", func(t *testing.T) {
		s := mustListen(t, Options{Page: testPage("DECK")})
		addr, ok := s.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatalf("Addr() is %T, want *net.TCPAddr", s.Addr())
		}
		if !addr.IP.IsLoopback() {
			t.Fatalf("server bound %s; want a loopback address only", addr.IP)
		}
		if !strings.HasPrefix(s.URL(), "http://"+Host+":") {
			t.Fatalf("URL %q does not start with http://%s:", s.URL(), Host)
		}
	})

	t.Run("page state swaps to error and recovers through Pipeline", func(t *testing.T) {
		verr := validate.New("slides/2-team.md", 7, []string{"title"}, "required", "add a title: value")
		errPage, err := NewErrorPage("Deck", verr)
		if err != nil {
			t.Fatalf("NewErrorPage: %v", err)
		}
		if want := validate.Format(verr); errPage.Err != want {
			t.Fatalf("error page Err = %q, want %q", errPage.Err, want)
		}
		if !strings.Contains(string(errPage.Doc), validate.Format(verr)) {
			t.Fatalf("error page document does not carry the formatted error %q", validate.Format(verr))
		}

		deck1 := testPage("DECK-ONE")
		deck2 := testPage("DECK-TWO")
		// Start renders the first (valid) page through the pipeline; the next
		// event publishes the error page, the one after that recovers.
		responses := []*Page{deck1, errPage, deck2}
		calls := 0
		var log bytes.Buffer

		srv := mustListen(t, Options{Page: deck1})
		rl, err := NewReloader(ReloadOptions{
			Server: srv,
			Pipeline: func(fs.FS) (*Page, error) {
				if calls >= len(responses) {
					return nil, fmt.Errorf("pipeline called %d times, only %d responses queued", calls+1, len(responses))
				}
				p := responses[calls]
				calls++
				return p, nil
			},
			Log: &log,
		})
		if err != nil {
			t.Fatalf("NewReloader: %v", err)
		}
		if err := rl.Start(); err != nil {
			t.Fatalf("Reloader.Start: %v", err)
		}
		t.Cleanup(rl.Stop)

		// Initial deck page: the injected live-reload client script must be there.
		body := getBody(t, srv, rootPath)
		if !strings.Contains(body, "DECK-ONE") {
			t.Fatalf("initial page does not serve the deck: %q", body)
		}
		if !strings.Contains(body, "EventSource") || !strings.Contains(body, DefaultReloadPath) {
			t.Fatalf("initial page lacks the injected live-reload script: %q", body)
		}

		// A broken edit publishes the full-page error carrying the same message.
		rl.Reload()
		body = getBody(t, srv, rootPath)
		if !strings.Contains(body, "Deck error") {
			t.Fatalf("error state does not serve the error page: %q", body)
		}
		if want := validate.Format(verr); !strings.Contains(body, want) {
			t.Fatalf("error page does not carry %q: %q", want, body)
		}
		if !strings.Contains(log.String(), validate.Format(verr)) {
			t.Fatalf("terminal log %q does not print the identical error %q", log.String(), validate.Format(verr))
		}

		// The next valid edit recovers: the deck page is served again.
		rl.Reload()
		body = getBody(t, srv, rootPath)
		if !strings.Contains(body, "DECK-TWO") {
			t.Fatalf("recovery did not serve the fixed deck: %q", body)
		}
		if strings.Contains(body, validate.Format(verr)) {
			t.Fatalf("recovered page still shows the error: %q", body)
		}
	})

	t.Run("SSE broadcast delivers a reload event", func(t *testing.T) {
		srv := mustListen(t, Options{Page: testPage("DECK")})
		rl, err := NewReloader(ReloadOptions{
			Server:   srv,
			Pipeline: func(fs.FS) (*Page, error) { return testPage("DECK-RELOADED"), nil },
			Log:      io.Discard,
		})
		if err != nil {
			t.Fatalf("NewReloader: %v", err)
		}
		if err := rl.Start(); err != nil {
			t.Fatalf("Reloader.Start: %v", err)
		}
		t.Cleanup(rl.Stop)

		ts := httptest.NewServer(srv.mux)
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + DefaultReloadPath)
		if err != nil {
			t.Fatalf("GET %s: %v", DefaultReloadPath, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status %d, want 200", DefaultReloadPath, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("SSE Content-Type = %q, want text/event-stream", ct)
		}

		lines := make(chan string, 32)
		go func() {
			br := bufio.NewReader(resp.Body)
			for {
				line, err := br.ReadString('\n')
				if line != "" {
					lines <- line
				}
				if err != nil {
					close(lines)
					return
				}
			}
		}()

		waitSSELine(t, lines, ": connected", 3*time.Second)

		// Trigger reload repeatedly until the client, which subscribes just
		// after opening the stream, observes one broadcast.
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			tick := time.NewTicker(50 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					rl.Reload()
				}
			}
		}()

		waitSSELine(t, lines, "data: reload", 3*time.Second)
	})

	t.Run("templates gallery lists every fixture template with docs", func(t *testing.T) {
		lib, reg, themes := mustGalleryFixtureLibrary(t)
		srv := mustListen(t, Options{Page: testPage("DECK"), Library: lib, Themes: themes})

		rec := serve(srv, http.MethodGet, galleryPath)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status %d, want 200 (body %q)", galleryPath, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("gallery Content-Type = %q, want text/html", ct)
		}
		raw := rec.Body.String()
		body := unescapeText(raw)

		if strings.Contains(strings.ToLower(body), "eypres") {
			t.Errorf("gallery page contains 'eypres':\n%s", body)
		}
		if strings.Contains(body, "Example unavailable") {
			t.Errorf("gallery reports an unavailable example:\n%s", body)
		}
		for _, want := range []string{"Fields", "Sections", "Body"} {
			if !strings.Contains(body, want) {
				t.Errorf("gallery is missing the %q documentation section", want)
			}
		}

		names := reg.TemplateNames()
		if len(names) == 0 {
			t.Fatal("fixture registry has no templates")
		}
		for _, tmpl := range reg.Templates() {
			if tmpl == nil {
				continue
			}
			if !strings.Contains(body, `class="gallery-page__name">`+tmpl.Name) {
				t.Errorf("gallery does not list template %q", tmpl.Name)
			}
			usage := string(tmpl.Usage)
			if !strings.Contains(body, `class="gallery-page__usage">`+usage) {
				t.Errorf("gallery does not show usage %q for template %q", usage, tmpl.Name)
			}
			if tmpl.Description != "" && !strings.Contains(body, tmpl.Description) {
				t.Errorf("gallery does not show the description of %q: %q", tmpl.Name, tmpl.Description)
			}
			for i := range tmpl.Fields {
				name := tmpl.Fields[i].Name
				if !strings.Contains(body, "<code>"+name+"</code>") {
					t.Errorf("gallery does not document field %q of template %q", name, tmpl.Name)
				}
			}
			for i := range tmpl.Sections {
				sec := tmpl.Sections[i]
				if !strings.Contains(body, "<code>"+sec.Name+"</code>") {
					t.Errorf("gallery does not document section %q of template %q", sec.Name, tmpl.Name)
				}
				if sec.Accepted != nil && !strings.Contains(body, strings.Join(sec.Accepted, ", ")) {
					t.Errorf("gallery does not show accepted templates for section %q of %q", sec.Name, tmpl.Name)
				}
			}
			if summary := bodySummary(tmpl.Body); summary != "" && !strings.Contains(body, summary) {
				t.Errorf("gallery does not document the body rule of %q: %q", tmpl.Name, summary)
			}
		}

		// Spot-check the documented names/usage explicitly required by the task.
		for name, usage := range map[string]string{"title": "slide", "content": "slide", "column": "section"} {
			if !strings.Contains(body, `class="gallery-page__name">`+name+`<span class="gallery-page__usage">`+usage) {
				t.Errorf("gallery entry for %q (%s) not found in expected form", name, usage)
			}
		}
		// content declares the columns section accepting column, 2–4 times.
		for _, want := range []string{"<code>columns</code>", "<code>column</code>", "2–4", "max 2 paragraphs"} {
			if !strings.Contains(body, want) {
				t.Errorf("gallery is missing expected content-template documentation %q", want)
			}
		}
	})

	t.Run("gallery serves the project library", func(t *testing.T) {
		fsys := os.DirFS(fixtureLibraryDir)
		lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
		if err != nil {
			t.Fatalf("template.LoadLibrary: %v", err)
		}
		reg, err := template.NewRegistryFromLibrary(lib)
		if err != nil {
			t.Fatalf("template.NewRegistryFromLibrary: %v", err)
		}
		funcMap := template.LayoutFuncMap(lib.Media, MediaPath)

		rec := httptest.NewRecorder()
		galleryHandler(reg, funcMap).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, galleryPath, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status %d, want 200 (body %q)", galleryPath, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `class="gallery-page__name">hello`) {
			t.Errorf("gallery does not list the fixture %q slide template:\n%s", "hello", body)
		}
		if !strings.Contains(body, `class="gallery-page__name">item`) {
			t.Errorf("gallery does not list the fixture %q section template:\n%s", "item", body)
		}
		// The item section's Layout.Name is a full manifest path
		// (templates/sections/item/layout.html.tmpl), not the bare template
		// name "item". Its preview must actually render the example's
		// content through that layout, not fall back to the
		// "Example unavailable" placeholder (regression coverage for the
		// gallerySectionExample fix in commit 02fcf3a).
		if strings.Contains(body, "Example unavailable") {
			t.Errorf("gallery reports an unavailable example for the fixture library:\n%s", body)
		}
		if !strings.Contains(body, `class="item"`) {
			t.Errorf("gallery does not render the item section's <li class=\"item\"> layout output:\n%s", body)
		}
		if !strings.Contains(body, "First item") {
			t.Errorf("gallery does not render the item section example's title %q:\n%s", "First item", body)
		}
		if !strings.Contains(body, "Details about the first item.") {
			t.Errorf("gallery does not render the item section example's body:\n%s", body)
		}
	})

	t.Run("broken templates library serves the error page", func(t *testing.T) {
		dir := t.TempDir()
		// kalide.yaml and slides/ are valid, but templates/ is entirely
		// missing: NewReloader's built-in pipeline must publish the error
		// page, never a stale or partially-loaded deck.
		mustWriteFile(t, filepath.Join(dir, "kalide.yaml"), "title: Broken\n")
		mustWriteFile(t, filepath.Join(dir, "slides", "1-only.md"), "---\ntemplate: hello\ntitle: Hi\n---\n")

		srv := mustListen(t, Options{Page: testPage("PLACEHOLDER")})
		rl, err := NewReloader(ReloadOptions{Server: srv, Root: dir, Log: io.Discard})
		if err != nil {
			t.Fatalf("NewReloader: %v", err)
		}
		if err := rl.Start(); err != nil {
			t.Fatalf("Reloader.Start: %v, want the missing templates/ directory reported as the error page, not a Start failure", err)
		}
		t.Cleanup(rl.Stop)

		body := getBody(t, srv, rootPath)
		if !strings.Contains(body, "Deck error") {
			t.Fatalf("missing templates/ directory did not serve the error page: %q", body)
		}
	})

	t.Run("binds 127.0.0.1", func(t *testing.T) {
		s := mustListen(t, Options{Page: testPage("DECK")})
		addr, ok := s.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatalf("Addr() is %T, want *net.TCPAddr", s.Addr())
		}
		if addr.IP.String() != Host {
			t.Fatalf("server bound %s, want exactly %s", addr.IP, Host)
		}
	})

	t.Run("assets serve the embedded reveal.js runtime", func(t *testing.T) {
		srv := mustListen(t, Options{Page: testPage("DECK")})

		for _, name := range []string{
			"reveal/dist/reveal.js",
			"reveal/dist/reveal.css",
			"reveal/dist/reset.css",
			"reveal/dist/plugin/notes.js",
			"reveal/LICENSE",
		} {
			rec := serve(srv, http.MethodGet, AssetsPath+name)
			if rec.Code != http.StatusOK {
				t.Errorf("GET %s%s status %d, want 200", AssetsPath, name, rec.Code)
				continue
			}
			if rec.Body.Len() == 0 {
				t.Errorf("GET %s%s served an empty body", AssetsPath, name)
			}
			if ct := rec.Header().Get("Content-Type"); ct == "" {
				t.Errorf("GET %s%s has no Content-Type", AssetsPath, name)
			}
		}

		// theme.css, fonts and logos are no longer embedded: they are served
		// from a project's own templates/themes via mediaHandler, not the
		// embedded /assets/ tree.
		for _, name := range []string{
			"theme.css",
			"logo/logo_full-light.svg",
			"fonts/EYInterstate-Regular.woff2",
		} {
			rec := serve(srv, http.MethodGet, AssetsPath+name)
			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s%s status %d, want 404 (no longer embedded)", AssetsPath, name, rec.Code)
			}
		}

		rec := serve(srv, http.MethodGet, AssetsPath+"does/not/exist.txt")
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET a missing asset status %d, want 404", rec.Code)
		}
	})
}

// mustWriteFile writes data to path, creating parent directories as needed.
func mustWriteFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustGalleryFixtureLibrary loads a small in-memory templates/ library
// (title, content, column) standing in for the deleted Go-authored builtins:
// content composes 2..4 "columns" of "column" and carries a 2-paragraph body
// limit, matching the gallery documentation the "templates gallery lists
// every fixture template with docs" subtest asserts.
func mustGalleryFixtureLibrary(t *testing.T) (*template.Library, *template.Registry, *theme.Registry) {
	t.Helper()
	const contentManifest = `description: Content slide with composed columns
fields:
  - name: heading
    type: text
    required: true
sections:
  - name: columns
    accepted: [column]
    min: 2
    max: 4
body:
  mode: optional
  max_paragraphs: 2
`
	const columnManifest = `description: A single column section
fields:
  - name: title
    type: text
`
	fsys := fstest.MapFS{
		"templates/library.yaml":                     {Data: []byte("name: gallery-fixture\nformat: 1\n")},
		"templates/slides/title/template.yaml":       {Data: []byte("description: Title slide\nfields:\n  - name: title\n    type: text\n    required: true\n")},
		"templates/slides/title/layout.html.tmpl":    {Data: []byte("<section>{{.title}}</section>")},
		"templates/slides/title/example.md":          {Data: []byte("---\ntemplate: title\ntitle: Quarterly Business Review\n---\n")},
		"templates/slides/content/template.yaml":     {Data: []byte(contentManifest)},
		"templates/slides/content/layout.html.tmpl":  {Data: []byte("<section>{{.heading}}</section>")},
		"templates/slides/content/example.md":        {Data: []byte("---\ntemplate: content\nheading: H\n---\n\n# columns\n```\ntitle: A\n```\n\n# columns\n```\ntitle: B\n```\n")},
		"templates/sections/column/template.yaml":    {Data: []byte(columnManifest)},
		"templates/sections/column/layout.html.tmpl": {Data: []byte("<div>{{.title}}</div>")},
		"templates/sections/column/example.md":       {Data: []byte("```\ntitle: Sample\n```\n")},
		"templates/themes/default/theme.css":         {Data: []byte("body{}")},
	}

	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	themes, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}
	return lib, reg, themes
}

// testPage returns a deck Page carrying doc as its served document.
func testPage(doc string) *Page {
	return NewDeckPage(htmltmpl.HTML("<html><body>"+doc+"</body></html>"), "Deck")
}

// mustListen starts a server for a test and closes it when the test ends.
func mustListen(t *testing.T, opts Options) *Server {
	t.Helper()
	s, err := Listen(opts)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// serve runs one request through the server's mux and returns the recorder.
func serve(s *Server, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// getBody requests target through the server's mux and returns the body,
// failing the test on a non-200 status.
func getBody(t *testing.T, s *Server, target string) string {
	t.Helper()
	rec := serve(s, http.MethodGet, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status %d, want 200 (body %q)", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// listenerPort returns the port the server is bound to.
func listenerPort(s *Server) int {
	addr, ok := s.Addr().(*net.TCPAddr)
	if !ok {
		return -1
	}
	return addr.Port
}

// probeFreePorts returns the ports in lo..hi this process can currently bind,
// in ascending order, each released immediately after the probe.
func probeFreePorts(t *testing.T, lo, hi int) []int {
	t.Helper()
	var free []int
	for p := lo; p <= hi; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(Host, strconv.Itoa(p)))
		if err != nil {
			continue
		}
		_ = ln.Close()
		free = append(free, p)
	}
	return free
}

// occupyAll binds every currently-free port in lo..hi and returns the held
// listeners, so every port in the range is busy for the duration.
func occupyAll(t *testing.T, lo, hi int) []net.Listener {
	t.Helper()
	var held []net.Listener
	for p := lo; p <= hi; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(Host, strconv.Itoa(p)))
		if err != nil {
			// Already busy (externally); that is just as good.
			continue
		}
		held = append(held, ln)
	}
	return held
}

// waitSSELine reads event-stream lines until one matches want after trimming
// whitespace, or fails the test after timeout.
func waitSSELine(t *testing.T, lines <-chan string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("event stream closed before %q arrived", want)
			}
			if strings.TrimSpace(line) == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out after %s waiting for SSE %q", timeout, want)
		}
	}
}

// unescapeText reverses the html/template escapes applied to page text, so a
// test can compare against the plain source strings.
func unescapeText(s string) string {
	return strings.NewReplacer(
		"&#39;", "'",
		"&#34;", `"`,
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
	).Replace(s)
}
