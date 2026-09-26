package e2e

// This file is the phase-9 task-2 end-to-end test for the native release
// verification: it runs the whole "native-ci-verification" sequence against the
// real eypres binary on each native runner (macOS arm64, Windows amd64/arm64).
//
// Native CI builds the per-target release binary once and passes it to the
// harness through EYPRES_BINARY (harness.go skips its own build then); when the
// variable is unset — a developer running `go test ./e2e` locally — the harness
// compiles the same CGO-free binary from source. Either way this test drives
// that exact binary.
//
// The sequence:
//
//   - a clean temporary working directory is scaffolded with `eypres init`,
//     which must exit 0;
//   - `eypres start --no-open` runs in its own process group (the harness);
//     its printed URL is parsed and GET / is polled for up to 30s until it
//     answers 200 with BOTH starter slides' content;
//   - every asset the served deck page references — reveal.js, the theme
//     stylesheet, the embedded fonts and logos its url()s point at — is fetched
//     from the local server and must answer 200 with a non-empty body; the page
//     must contain no external (non-loopback) URL;
//   - assertOffline samples the eypres process for the whole run, failing the
//     test on any outbound (non-loopback) connection;
//   - a graceful stop (SIGTERM on macOS, CTRL_BREAK_EVENT to the process group
//     on Windows) must exit 0, release the port and leave no leftover child
//     (the harness asserts the last two in Stop). A forced kill only happens as
//     the harness's timeout fallback, which fails the test.
//
// It builds (or runs) a real binary and drives a real server, so it is skipped
// under -short.

import (
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// nativeReleaseReadyTimeout bounds the wait for the served deck page once the
// harness has already confirmed the URL answers 200.
const nativeReleaseReadyTimeout = 30 * time.Second

// nativeReleasePollInterval is the pause between deck-page polls.
const nativeReleasePollInterval = 50 * time.Millisecond

// nativeHTMLRefRE pulls src="…" and href="…" references out of a served page.
var nativeHTMLRefRE = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["']([^"']+)["']`)

// nativeCSSRefRE pulls url(…) references out of a stylesheet (or an inline
// style attribute). Both quoted and bare forms are matched.
var nativeCSSRefRE = regexp.MustCompile(`(?i)url\(\s*['"]?([^'")]+?)['"]?\s*\)`)

// nativeAbsURLRE finds absolute http(s) URLs anywhere in a served page.
var nativeAbsURLRE = regexp.MustCompile(`(?i)https?://[^\s"'<>()]+`)

// TestNativeRelease is the end-to-end native release verification described at
// the top of this file.
func TestNativeRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e native release test builds or runs the real eypres binary")
	}

	h := NewHarness(t)

	// Report which binary this run exercises: native CI feeds the prebuilt
	// per-target binary through EYPRES_BINARY, a local run lets the harness
	// build one.
	if prebuilt := os.Getenv("EYPRES_BINARY"); prebuilt != "" {
		t.Logf("native release: using prebuilt EYPRES_BINARY=%s", prebuilt)
	} else {
		t.Logf("native release: EYPRES_BINARY unset; harness built %s from source", h.BinaryPath())
	}

	// The harness working directory is clean: init must start from a directory
	// that holds no deck entries.
	assertDirEmpty(t, h.WorkDir())

	// `eypres init` scaffolds the starter deck and exits 0.
	stdout, stderr, code := h.Run("init")
	if code != 0 {
		t.Fatalf("eypres init exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("eypres init stderr = %q, want empty", stderr)
	}
	for _, rel := range initCreatedPaths {
		if !strings.Contains(stdout, rel) {
			t.Errorf("eypres init output does not name %q:\n%s", rel, stdout)
		}
	}

	// Second offline belt: point every proxy variable at a closed loopback port
	// for the eypres process. It must be configured before Start.
	h.EnableOfflineProxy()

	// `eypres start --no-open` runs in its own process group; Start parses the
	// printed URL and polls it until it answers 200.
	h.Start()

	// Sample the eypres process's connections for the whole run, including the
	// graceful stop below. Stop is registered for cleanup so a failure earlier
	// still asserts offline and shuts the process down.
	off := assertOffline(t, h.PID())
	t.Cleanup(off.Stop)

	if got := h.URL(); !strings.HasPrefix(got, "http://127.0.0.1:") || !strings.HasSuffix(got, "/") {
		t.Fatalf("start printed URL %q, want http://127.0.0.1:<port>/", got)
	}
	if !strings.Contains(h.Stdout(), "Serving slides at "+h.URL()) {
		t.Errorf("stdout does not name the served URL %q:\n%s", h.URL(), h.Stdout())
	}

	// Poll GET / for up to 30s until it answers 200 with BOTH starter slides'
	// content. h.Start already confirmed a 200, so this only waits for the deck
	// body to be complete.
	body := waitForDeckPage(t, h, nativeReleaseReadyTimeout, starterSlideContent)

	// The page must reference nothing off the local server.
	assertLoopbackOnlyPage(t, body)

	// Fetch every asset the page (and, transitively, its stylesheets)
	// references, asserting each answers 200 from the local server.
	fetched := crawlNativeAssets(t, h, body)

	for _, want := range []string{
		"/assets/reveal/dist/reveal.js",
		"/assets/reveal/dist/reveal.css",
		"/assets/reveal/dist/reset.css",
		"/assets/reveal/dist/plugin/notes.js",
		"/assets/theme.css",
	} {
		if !contains(fetched, want) {
			t.Errorf("served deck page does not reference %s (fetched %v)", want, fetched)
		}
	}
	// The theme stylesheet's url()s pull in the embedded fonts and logos, so
	// those must have been reached and fetched too.
	for _, prefix := range []string{"/assets/fonts/", "/assets/logo/"} {
		if !hasPathPrefix(fetched, prefix) {
			t.Errorf("no fetched asset under %s (fonts/logos not reached); fetched %v", prefix, fetched)
		}
	}

	// No outbound connection so far.
	off.Check()

	// A graceful stop exits 0, releases the port and leaves no child behind;
	// the harness asserts the exit status, the released port and the absence of
	// leftover children in Stop. A forced kill would only be the timeout
	// fallback, and reaching it fails the test.
	port := h.Port()
	if port == 0 {
		t.Fatal("start did not bind a port")
	}
	h.Stop()
	off.Stop()
	if _, err := h.Get("/"); err == nil {
		t.Errorf("GET / succeeded after Stop; port %d was not released", port)
	}
}

// waitForDeckPage polls GET / until it answers 200 with every want substring,
// or the timeout elapses, and returns the page body.
func waitForDeckPage(t *testing.T, h *Harness, timeout time.Duration, want []string) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastBody string
	var lastErr error
	for {
		body, err := h.GetString("/")
		if err == nil {
			lastBody = body
			missing := false
			for _, w := range want {
				if !strings.Contains(body, w) {
					missing = true
					break
				}
			}
			if !missing {
				return body
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for the deck page with both starter slides; last error: %v\nbody:\n%s",
				timeout, lastErr, lastBody)
		}
		time.Sleep(nativeReleasePollInterval)
	}
}

// assertLoopbackOnlyPage fails the test for any absolute http(s) URL in the
// served page whose host is not loopback. The page is the deck document; its
// assets are scanned separately by crawlNativeAssets.
func assertLoopbackOnlyPage(t *testing.T, doc string) {
	t.Helper()

	for _, raw := range nativeAbsURLRE.FindAllString(doc, -1) {
		u, err := url.Parse(raw)
		if err != nil {
			t.Errorf("served page has an unparseable URL %q: %v", raw, err)
			continue
		}
		if !offlineLoopbackHost(u.Hostname()) {
			t.Errorf("served page references external (non-loopback) URL %q", raw)
		}
	}
}

// nativeDoc is a fetched document whose references still need crawling: an HTML
// page (src/href plus url()) or a stylesheet (url() only).
type nativeDoc struct {
	base string
	body string
	css  bool
}

// crawlNativeAssets fetches root's references and, transitively, the references
// of every stylesheet it reaches. It returns the distinct server paths it
// fetched. It fails the test for a reference that leaves the local server, for
// an asset that does not answer 200, and for an empty asset body.
func crawlNativeAssets(t *testing.T, h *Harness, page string) []string {
	t.Helper()

	visited := map[string]bool{"/": true}
	queue := []nativeDoc{{base: h.BaseURL() + "/", body: page}}
	var fetched []string

	for len(queue) > 0 {
		doc := queue[0]
		queue = queue[1:]

		refs := nativeHTMLRefs(doc.body)
		if doc.css {
			refs = nativeCSSRefs(doc.body)
		}

		for _, ref := range refs {
			path, external := nativeResolveRef(t, doc.base, ref)
			if external {
				t.Errorf("served document %s references external URL %q", doc.base, ref)
				continue
			}
			if path == "" || visited[path] {
				continue
			}
			visited[path] = true

			asset, err := h.GetOK(path)
			if err != nil {
				t.Errorf("referenced asset %s did not resolve: %v", path, err)
				continue
			}
			if len(asset) == 0 {
				t.Errorf("referenced asset %s resolved with an empty body", path)
				continue
			}
			fetched = append(fetched, path)

			if strings.HasSuffix(strings.ToLower(strings.SplitN(path, "?", 2)[0]), ".css") {
				queue = append(queue, nativeDoc{base: h.BaseURL() + path, body: string(asset), css: true})
			}
		}
	}
	return fetched
}

// nativeHTMLRefs returns the src/href and url() references in an HTML document.
func nativeHTMLRefs(doc string) []string {
	var refs []string
	for _, m := range nativeHTMLRefRE.FindAllStringSubmatch(doc, -1) {
		refs = append(refs, m[1])
	}
	return append(refs, nativeCSSRefs(doc)...)
}

// nativeCSSRefs returns the url() references in a stylesheet.
func nativeCSSRefs(doc string) []string {
	var refs []string
	for _, m := range nativeCSSRefRE.FindAllStringSubmatch(doc, -1) {
		refs = append(refs, m[1])
	}
	return refs
}

// nativeResolveRef turns a reference found in a served document into a
// server-relative path. external is true only when the reference points off the
// local server (an absolute or protocol-relative non-loopback URL); path is ""
// when the reference is not a fetchable server path (a fragment, a data:/mailto:
// URL, or the document root).
func nativeResolveRef(t *testing.T, base, ref string) (path string, external bool) {
	t.Helper()

	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "#") {
		return "", false
	}
	lower := strings.ToLower(ref)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "blob:") {
		return "", false
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		t.Errorf("parse base URL %q: %v", base, err)
		return "", false
	}
	u, err := baseURL.Parse(ref)
	if err != nil {
		t.Errorf("resolve reference %q against %q: %v", ref, base, err)
		return "", false
	}
	if u.Host != "" && !offlineLoopbackHost(u.Hostname()) {
		return "", true
	}
	if u.Path == "" || u.Path == "/" {
		return "", false
	}
	return u.Path, false
}

// hasPathPrefix reports whether any path in paths starts with prefix.
func hasPathPrefix(paths []string, prefix string) bool {
	for _, p := range paths {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
