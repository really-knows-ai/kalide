package e2e

// This file is the phase-06 task-4 end-to-end test for examples/demo, the
// non-EY demo project used as an e2e/integration fixture (see
// examples/demo/eypres.yaml, templates/ and slides/). It copies the fixture to
// a clean temporary directory (never mutating the repo checkout), runs the
// real eypres binary offline against it with `eypres start --no-open`, and
// asserts:
//
//   - the deck page is served (HTTP 200) and carries the demo deck's own
//     content;
//   - templates/media/** and templates/themes/<name>/** are served locally
//     (200, extension-derived Content-Type), through the same mediaHandler
//     routes exercised by TestInitStart/TestNativeRelease;
//   - the process makes no outbound (non-loopback) network connection for the
//     whole run (assertOffline, the phase-9 offline-enforcement pattern also
//     used by offline_test.go);
//   - the server stops gracefully and exits cleanly (h.Stop's post-conditions:
//     exit 0, port released, no leftover children).
//
// A separate grep-guard, TestDemoProjectHasNoEYReferences, asserts the
// examples/demo tree on disk (the actual fixture, not the copy) carries no EY
// brand marker: no EY font family name, no EY logo file name, and no bare "EY"
// token outside the "non-EY" phrasing the fixture uses to document that it is
// deliberately unbranded.
//
// Both tests build a binary (or run one via EYPRES_BINARY), so both are
// skipped under -short.

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/server"
)

// TestDemoProject drives `eypres start --no-open` against a temporary copy of
// examples/demo and asserts it serves cleanly, offline, and stops gracefully.
func TestDemoProject(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real eypres binary")
	}

	h := NewHarness(t)

	// Copy examples/demo into the harness's clean working directory; the
	// fixture in the repo checkout is never touched.
	copyDemoProject(t, h)

	// Belt-and-braces offline enforcement, matching the pattern used elsewhere
	// in this package (offline.go): the proxy environment is set before Start,
	// and the connection sampler is armed once the pid is known.
	h.EnableOfflineProxy()
	h.Start()

	off := assertOffline(t, h.PID())
	t.Cleanup(off.Stop)

	body, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if !strings.Contains(body, "The Demo Project") {
		t.Errorf("served deck page does not carry the demo deck's title:\n%s", body)
	}

	// templates/media/** is served locally: examples/demo/templates/media
	// holds badge.svg.
	mediaResp, err := h.Get(server.MediaPath + "badge.svg")
	if err != nil {
		t.Fatalf("GET %sbadge.svg: %v", server.MediaPath, err)
	}
	mediaBody, err := io.ReadAll(mediaResp.Body)
	_ = mediaResp.Body.Close()
	if err != nil {
		t.Fatalf("read media response body: %v", err)
	}
	if mediaResp.StatusCode != http.StatusOK {
		t.Errorf("GET %sbadge.svg status = %d, want 200 (body = %q)",
			server.MediaPath, mediaResp.StatusCode, mediaBody)
	}
	if ct := mediaResp.Header.Get("Content-Type"); !strings.Contains(ct, "svg") {
		t.Errorf("GET %sbadge.svg Content-Type = %q, want it to mention svg", server.MediaPath, ct)
	}
	if len(mediaBody) == 0 {
		t.Error("GET templates/media/badge.svg served an empty body")
	}

	// templates/themes/<name>/theme.css is served locally: the demo deck's
	// theme is "demo" (examples/demo/eypres.yaml).
	themeResp, err := h.Get(server.ThemesPath + "demo/theme.css")
	if err != nil {
		t.Fatalf("GET %sdemo/theme.css: %v", server.ThemesPath, err)
	}
	themeBody, err := io.ReadAll(themeResp.Body)
	_ = themeResp.Body.Close()
	if err != nil {
		t.Fatalf("read theme response body: %v", err)
	}
	if themeResp.StatusCode != http.StatusOK {
		t.Errorf("GET %sdemo/theme.css status = %d, want 200 (body = %q)",
			server.ThemesPath, themeResp.StatusCode, themeBody)
	}
	if ct := themeResp.Header.Get("Content-Type"); !strings.Contains(ct, "css") {
		t.Errorf("GET %sdemo/theme.css Content-Type = %q, want it to mention css", server.ThemesPath, ct)
	}

	// Graceful stop: exit 0, port released, no leftover children. h.Stop's
	// post-conditions are asserted by checkShutdown; Stop is also registered
	// as a cleanup by Start, so calling it explicitly here just confirms the
	// clean exit within this test rather than only at cleanup time.
	h.Stop()
	off.Stop()
}

// copyDemoProject copies the examples/demo fixture tree into the harness's
// clean working directory, so the test drives the real eypres binary against
// a disposable copy and never mutates the repo checkout.
func copyDemoProject(t *testing.T, h *Harness) {
	t.Helper()

	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("e2e: %v", err)
	}
	src := filepath.Join(root, "examples", "demo")
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		t.Fatalf("e2e: examples/demo fixture not found at %s: %v", src, err)
	}

	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			return os.MkdirAll(h.Path(rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		h.WriteFile(rel, data)
		return nil
	})
	if err != nil {
		t.Fatalf("e2e: copy examples/demo to harness working directory: %v", err)
	}
}

// eyBrandPattern matches EY brand markers that must not appear anywhere under
// examples/demo: EY font family names (EYInterstate, EYGothic...), EY logo
// file names (logo_full-*, logo_small-*), and a bare "EY" token that is not
// part of the "non-EY" phrasing the fixture uses to document that it is
// deliberately unbranded.
var eyBrandPattern = regexp.MustCompile(`(?i)EYInterstate|EYGothic|logo_full|logo_small|(?:^|[^-])\bEY\b`)

// eyDisclaimerPattern matches the fixture's own documentation phrasing that
// deliberately names "EY" to say the fixture is NOT EY-branded (e.g.
// "non-EY presentation", "no EY branding"). Lines matching only this pattern
// are not brand references and are excluded from the eyBrandPattern scan.
var eyDisclaimerPattern = regexp.MustCompile(`(?i)non-EY|no EY\b`)

// TestDemoProjectHasNoEYReferences is the grep-guard: examples/demo must carry
// no EY brand reference (name, font or logo) anywhere in its tree. It reads
// the fixture directly from the repo checkout (not a harness copy), so it is
// not gated on building the eypres binary, but it lives alongside
// TestDemoProject as its companion assertion for the demo-project requirement.
func TestDemoProjectHasNoEYReferences(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("e2e: %v", err)
	}
	demoDir := filepath.Join(root, "examples", "demo")

	err = filepath.WalkDir(demoDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Check the file name itself (catches EY logo/font file names even
		// with no matching content, though examples/demo should have none).
		name := d.Name()
		if eyBrandPattern.MatchString(name) {
			t.Errorf("examples/demo file name %q carries an EY brand reference", p)
		}

		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if eyDisclaimerPattern.MatchString(line) {
				continue
			}
			if eyBrandPattern.MatchString(line) {
				t.Errorf("examples/demo carries an EY brand reference in %s: %q", p, line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("e2e: walk examples/demo: %v", err)
	}
}
