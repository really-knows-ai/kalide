package e2e

// This file is the phase-7 task-11 end-to-end test for `kalide start` and its
// live reload. It drives the real kalide binary the harness builds
// (harness.go) instead of any internal package, so it exercises the whole path
// an author takes:
//
//   - `start --no-open` prints the URL and serves the deck's page, with the
//     live-reload client script injected;
//   - editing a watched slide triggers the debounced watcher, a re-validate +
//     re-render and an SSE `data: reload` on the event stream;
//   - a breaking edit swaps the page for the full-page error carrying the
//     single first formatted validation error, prints the identical error to the
//     terminal, and a fixing edit recovers the deck;
//   - /templates serves the gallery of the project's templates/ library;
//   - `kalide templates` and `kalide templates <name>` print the list and one
//     template's documentation, both read from the project's templates/
//     library;
//   - a graceful stop exits 0, releases the port and leaves no child behind
//     (the harness asserts the last two in Stop).
//
// It builds a binary and drives a real server, so it is skipped under -short.
//
// The one import from the project is internal/server's DefaultReloadPath: the
// test connects to exactly the SSE route the server registers, so the path is
// pinned to its single source of truth rather than re-spelled here.

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/really-knows-ai/kalide/internal/server"
)

// The fixture deck. Since `kalide start` now requires a valid project
// templates/ library (theme.LoadDir, deck.LoadConfig against it), the test
// provisions one with `kalide init` (the embedded hello seed) before Start
// and drives the seed's own `hello` template: a required `title` field and an
// optional Markdown body, both always-valid, so two minimal slides can be
// edited and one broken deterministically.
const (
	deckConfig = `title: Integration Deck
author: Ada Lovelace
date: 2026-09-25
theme: default
navigation: grid
`

	introSlide = `---
template: hello
title: Integration Deck
---
A real on-disk deck.

# notes
Greet the audience.

`
)

// agendaSlide renders the second (editable) slide with the given subtitle in
// its body. Every value is valid: the hello template requires only the title
// field and takes an optional body.
func agendaSlide(subtitle string) string {
	return fmt.Sprintf(`---
template: hello
title: Agenda
---
%s

`, subtitle)
}

// brokenAgendaSlide drops the required `title` field, so whole-deck validation
// reports exactly one error: the required-field violation of the hello slide.
const brokenAgendaSlide = `---
template: hello
---
now broken

`

// wantBrokenError is the single formatted first error (validate.Format) the
// broken deck produces: the title field is required and missing. The browser
// error page and the terminal print this identical string.
const wantBrokenError = `slides/2-agenda.md › title: required: field "title" is required but missing — add a title: value`

// helloTemplateYAML and helloLayoutTmpl are the seed's own
// templates/slides/hello manifest and layout (internal/scaffold/seed), kept
// here so a broken/edited templates/ tree can be restored to the exact seed
// state the rest of the test (gallery, `kalide templates`) depends on.
const helloTemplateYAML = `description: "Hello slide: a required title and an optional body."
fields:
  - name: title
    type: text
    required: true
    max_length: 80
    description: Slide title.
body:
  mode: optional
`

// helloLayoutTmpl renders the hello layout with a marker paragraph appended,
// so a templates/ edit produces a distinctive, greppable change in the served
// page without altering the fields the rest of the test depends on.
func helloLayoutTmpl(marker string) string {
	return fmt.Sprintf(`{{/*
  hello — slide-usage template layout.

  Context: map[string]any keyed by field name. %s is required; %s
  is the slide's Markdown body, rendered as HTML, when the author gave one.
*/}}
<section class="hello-slide">
  <h1 class="hello-title">{{ .title }}</h1>
  <p class="hello-marker">%s</p>
  {{ with .body }}<div class="hello-body">{{ . }}</div>{{ end }}
</section>
`, "`title`", "`body`", marker)
}

// TestStartLiveReload is the end-to-end live-reload test described above.
func TestStartLiveReload(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

	// `kalide start` now requires a valid project templates/ library
	// (theme.LoadDir, deck.LoadConfig resolving `theme` against it): seed one
	// with the embedded hello seed before writing the fixture deck over it.
	if _, stderr, code := h.Run("init"); code != 0 {
		t.Fatalf("kalide init exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	h.WriteFile("kalide.yaml", []byte(deckConfig))
	h.WriteFile("slides/1-hello.md", []byte(introSlide))
	h.WriteFile("slides/2-agenda.md", []byte(agendaSlide("What we will cover")))

	h.Start()

	// The command printed the URL it bound and serves the deck's first page.
	if got := h.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") || !strings.HasSuffix(got, "/") {
		t.Fatalf("start printed URL %q, want http://127.0.0.1:<port>/", got)
	}
	if !strings.Contains(h.Stdout(), "Serving slides at "+h.URL()) {
		t.Errorf("stdout does not name the served URL %q:\n%s", h.URL(), h.Stdout())
	}

	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	for _, want := range []string{"Integration Deck", "Agenda", "A real on-disk deck"} {
		if !strings.Contains(body, want) {
			t.Errorf("served deck page is missing %q:\n%s", want, body)
		}
	}
	// The live-reload client script is injected into the served page and points
	// at the SSE route the test connects to.
	if !strings.Contains(body, "new EventSource(") || !strings.Contains(body, server.DefaultReloadPath) {
		t.Errorf("served page does not carry the live-reload script for %s:\n%s",
			server.DefaultReloadPath, body)
	}

	// Editing a watched slide triggers an SSE reload. Edits are repeated until
	// the broadcast is observed, covering the small window between the stream's
	// initial `: connected` flush and its subscribe; every edit is a valid deck.
	s := openSSE(t, h)
	defer s.Close()

	waitLine(t, s, ": connected", 5*time.Second)

	var marker string
	deadline := time.Now().Add(10 * time.Second)
	for i := 1; ; i++ {
		marker = fmt.Sprintf("Edit %d", i)
		h.WriteFile("slides/2-agenda.md", []byte(agendaSlide(marker)))
		if drainFor(s, "data: reload", 500*time.Millisecond) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no SSE reload after editing a slide\nstdout:\n%s\nstderr:\n%s",
				h.Stdout(), h.Stderr())
		}
	}
	// The deck is re-rendered before the reload is broadcast, so the edited
	// subtitle is what "/" now serves.
	waitFor(t, 5*time.Second, "the edited slide to be served", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, marker)
	})
	s.Close()

	// Editing a templates/ slide template also live-reloads: internal/watch
	// now watches the templates/ tree (task-1/task-8) and the built-in
	// pipeline reloads the project's templates/ Library fresh on every
	// reload (task-2/task-3) rather than serving one cached at construction,
	// so a layout edit is picked up exactly like a slide edit.
	ts := openSSE(t, h)
	defer ts.Close()
	waitLine(t, ts, ": connected", 5*time.Second)

	var layoutMarker string
	layoutDeadline := time.Now().Add(10 * time.Second)
	for i := 1; ; i++ {
		layoutMarker = fmt.Sprintf("layout-edit-%d", i)
		h.WriteFile("templates/slides/hello/layout.html.tmpl", []byte(helloLayoutTmpl(layoutMarker)))
		if drainFor(ts, "data: reload", 500*time.Millisecond) {
			break
		}
		if time.Now().After(layoutDeadline) {
			t.Fatalf("no SSE reload after editing a templates/ layout\nstdout:\n%s\nstderr:\n%s",
				h.Stdout(), h.Stderr())
		}
	}
	waitFor(t, 5*time.Second, "the edited template layout to be served", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, layoutMarker)
	})
	ts.Close()

	// Breaking templates/ (invalid YAML in a template.yaml) shows the error
	// page — a broken library publishes the error page, never a stale or
	// broken deck (never-serve-broken-deck).
	h.WriteFile("templates/slides/hello/template.yaml", []byte("description: [this is not valid yaml\n"))
	waitFor(t, 10*time.Second, "the templates/ error page to be served", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, "Deck error") && strings.Contains(b, "error-page__message")
	})
	brokenTemplatesBody, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET / (broken templates/ error page): %v", err)
	}
	if strings.Contains(brokenTemplatesBody, layoutMarker) {
		t.Errorf("error page still serves the stale deck page after breaking templates/:\n%s", brokenTemplatesBody)
	}

	// Restoring templates/ (fixing the template.yaml back to the seed's own
	// manifest) recovers the deck.
	h.WriteFile("templates/slides/hello/template.yaml", []byte(helloTemplateYAML))
	waitFor(t, 10*time.Second, "the deck to recover after fixing templates/", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, "Integration Deck") && !strings.Contains(b, "Deck error")
	})

	// A breaking edit: GET / becomes the full-page error carrying the single
	// first formatted error, and the same error is printed to the terminal.
	h.WriteFile("slides/2-agenda.md", []byte(brokenAgendaSlide))
	waitFor(t, 10*time.Second, "the full-page error to be served", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(html.UnescapeString(b), wantBrokenError)
	})

	errBody, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET / (error page): %v", err)
	}
	unescaped := html.UnescapeString(errBody)
	if !strings.Contains(unescaped, wantBrokenError) {
		t.Errorf("error page does not carry the first formatted error %q:\n%s", wantBrokenError, unescaped)
	}
	if !strings.Contains(errBody, "Deck error") || !strings.Contains(errBody, "error-page__message") {
		t.Errorf("served page is not the full-page error shell:\n%s", errBody)
	}
	waitFor(t, 5*time.Second, "the error to reach the terminal", func() bool {
		return strings.Contains(h.Stderr(), wantBrokenError)
	})

	// Fixing the edit recovers the deck: the next valid render swaps the error
	// page back to the presentation.
	h.WriteFile("slides/2-agenda.md", []byte(agendaSlide("Recovered")))
	waitFor(t, 10*time.Second, "the deck to recover", func() bool {
		b, err := h.GetString("/")
		return err == nil && strings.Contains(b, "Recovered") && strings.Contains(b, "Agenda")
	})

	// GET /templates serves the gallery of the project's templates/ library —
	// just the hello seed's one template now that the gallery documents the
	// project library instead of the compiled-in built-ins.
	gallery, err := h.GetString("/templates")
	if err != nil {
		t.Fatalf("GET /templates: %v", err)
	}
	if !strings.Contains(gallery, `class="gallery-page__name">hello`) {
		t.Errorf("gallery does not list the project template %q:\n%s", "hello", gallery)
	}

	// `kalide templates` lists the project's templates/ library; `kalide
	// templates <name>` shows one template's documentation sections.
	stdout, stderr, code := h.Run("templates")
	if code != 0 {
		t.Fatalf("kalide templates exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide templates stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Templates:") {
		t.Errorf("kalide templates output is missing the list header:\n%s", stdout)
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("kalide templates output does not name %q:\n%s", "hello", stdout)
	}

	stdout, stderr, code = h.Run("templates", "hello")
	if code != 0 {
		t.Fatalf("kalide templates hello exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide templates hello stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "hello (slide)") {
		t.Errorf("kalide templates hello output is missing the identity line:\n%s", stdout)
	}
	for _, section := range []string{"Fields:", "Sections:", "Body:", "Example:"} {
		if !strings.Contains(stdout, section) {
			t.Errorf("kalide templates hello output is missing section %q:\n%s", section, stdout)
		}
	}

	// A graceful stop exits 0, releases the port and leaves no child behind;
	// the harness asserts the exit status, the released port and the absence of
	// leftover children in Stop.
	port := h.Port()
	if port == 0 {
		t.Fatal("start did not bind a port")
	}
	h.Stop()
	if _, err := h.Get("/"); err == nil {
		t.Errorf("GET / succeeded after Stop; port %d was not released", port)
	}
}

// sseStream is a live event stream from the running server: the response body
// plus a goroutine that feeds its lines to a channel.
type sseStream struct {
	lines chan string
	body  io.ReadCloser
	once  sync.Once
}

// openSSE connects to the server's SSE endpoint and returns the stream. It
// fails the test if the endpoint does not answer 200 text/event-stream.
func openSSE(t *testing.T, h *Harness) *sseStream {
	t.Helper()

	resp, err := h.Get(server.DefaultReloadPath)
	if err != nil {
		t.Fatalf("GET %s: %v", server.DefaultReloadPath, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("GET %s status %s, want 200", server.DefaultReloadPath, resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		_ = resp.Body.Close()
		t.Fatalf("GET %s Content-Type = %q, want text/event-stream", server.DefaultReloadPath, ct)
	}

	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return &sseStream{lines: lines, body: resp.Body}
}

// Close ends the event stream. It is safe to call more than once.
func (s *sseStream) Close() {
	s.once.Do(func() { _ = s.body.Close() })
}

// waitLine fails the test unless a line containing token arrives within
// timeout.
func waitLine(t *testing.T, s *sseStream, token string, timeout time.Duration) {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				t.Fatalf("event stream closed before %q arrived", token)
			}
			if strings.Contains(line, token) {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out after %s waiting for event stream %q", timeout, token)
		}
	}
}

// drainFor reads the event stream for up to d and reports whether a line
// containing token arrived. It returns false when the stream closes.
func drainFor(s *sseStream, token string, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return false
			}
			if strings.Contains(line, token) {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

// waitFor polls cond every 50ms until it is true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, desc)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
