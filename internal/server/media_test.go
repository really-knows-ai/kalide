package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// TestMediaHandler covers mediaHandler (template-media) against a real
// project templates/ library and theme registry on a real temp directory:
// media and theme files are served with an extension-derived Content-Type,
// a request escaping its root with ".." or an absolute path is rejected, and
// a missing file 404s. It reads a real filesystem, so it is guarded with
// -short.
func TestMediaHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real templates/ library from disk")
	}

	dir := t.TempDir()
	writeMediaFixture(t, dir)

	fsys := os.DirFS(dir)
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	themes, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}

	handler := mediaHandler(lib, themes)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	t.Run("media file served with extension Content-Type", func(t *testing.T) {
		resp, err := http.Get(ts.URL + MediaPath + "logo.svg")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "svg") {
			t.Errorf("Content-Type = %q, want it to mention svg", ct)
		}
	})

	t.Run("theme file served with extension Content-Type", func(t *testing.T) {
		resp, err := http.Get(ts.URL + ThemesPath + "plain/theme.css")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "css") {
			t.Errorf("Content-Type = %q, want it to mention css", ct)
		}
	})

	t.Run("path traversal is rejected", func(t *testing.T) {
		for _, path := range []string{
			MediaPath + "../library.yaml",
			MediaPath + "..%2Flibrary.yaml",
		} {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want 404", path, resp.StatusCode)
			}
		}
	})

	t.Run("absolute path is rejected", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, ts.URL+MediaPath, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.URL.Path = MediaPath + "/etc/passwd"
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing media file 404s", func(t *testing.T) {
		resp, err := http.Get(ts.URL + MediaPath + "does-not-exist.svg")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing theme 404s", func(t *testing.T) {
		resp, err := http.Get(ts.URL + ThemesPath + "no-such-theme/theme.css")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing file within a known theme 404s", func(t *testing.T) {
		resp, err := http.Get(ts.URL + ThemesPath + "plain/does-not-exist.css")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})
}

// writeMediaFixture writes a minimal real templates/ library to dir: one
// slide template (so template.LoadLibrary succeeds), one theme and one
// media file.
func writeMediaFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"templates/library.yaml":                  "name: media-fixture\nformat: 1\n",
		"templates/slides/hello/template.yaml":    "fields:\n  - name: title\n    type: text\n    required: true\n",
		"templates/slides/hello/layout.html.tmpl": "<section><h1>{{.title}}</h1></section>",
		"templates/slides/hello/example.md":       "---\ntitle: Hi\n---\n",
		"templates/themes/plain/theme.css":        "body { margin: 0; }\n",
		"templates/media/logo.svg":                "<svg></svg>",
	}
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

// TestThemeRouteIntMediaRewrite is the end-to-end integration proof of the
// theme route's serve-time media: rewrite (theme-shared-media): a real
// on-disk library is loaded through template.LoadLibrary and theme.LoadDir,
// mediaHandler is mounted behind a real HTTP server, and the bytes a client
// receives for a theme stylesheet have every reserved-prefix media: url()
// rewritten to the served templates/media/ URL.
//
// It covers theme.css itself and a second stylesheet in the same theme
// directory (the kind theme.css reaches via @import), proves a theme-owned
// relative url() survives byte-for-byte, and fetches the rewritten URL at the
// media route so the rewrite is known to resolve rather than merely look
// right. It reads a real filesystem, so it is guarded with -short.
func TestThemeRouteIntMediaRewrite(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real templates/ library from disk")
	}

	dir := t.TempDir()
	writeThemeMediaFixture(t, dir)

	fsys := os.DirFS(dir)
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	themes, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}

	ts := httptest.NewServer(mediaHandler(lib, themes))
	t.Cleanup(ts.Close)

	prefix := template.MediaURLPrefix()
	// mediaURL is the served form the rewriter emits for a reserved-prefix
	// reference, built from the route's own prefix rather than hardcoded.
	mediaURL := func(clean string) string { return `url("` + MediaPath + clean + `")` }

	get := func(t *testing.T, url string) (string, http.Header, int) {
		t.Helper()
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body %s: %v", url, err)
		}
		return string(body), resp.Header, resp.StatusCode
	}

	t.Run("theme.css media: url rewritten and theme-owned relative url kept", func(t *testing.T) {
		body, header, status := get(t, ts.URL+ThemesPath+"shared/theme.css")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
			t.Errorf("Content-Type = %q, want it to start with text/css", ct)
		}
		if want := mediaURL("fonts/X.woff2"); !strings.Contains(body, want) {
			t.Errorf("body missing rewritten %s:\n%s", want, body)
		}
		if strings.Contains(body, prefix) {
			t.Errorf("body still contains reserved prefix %q:\n%s", prefix, body)
		}
		if !strings.Contains(body, "url('fonts/theme.woff2')") {
			t.Errorf("theme-owned url('fonts/theme.woff2') was not left unchanged:\n%s", body)
		}
	})

	t.Run("imported stylesheet served through the same route is rewritten too", func(t *testing.T) {
		body, _, status := get(t, ts.URL+ThemesPath+"shared/more.css")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if want := mediaURL("extra.css"); !strings.Contains(body, want) {
			t.Errorf("body missing rewritten %s:\n%s", want, body)
		}
		if strings.Contains(body, prefix) {
			t.Errorf("body still contains reserved prefix %q:\n%s", prefix, body)
		}
	})

	t.Run("rewritten media url resolves at the media route", func(t *testing.T) {
		body, _, status := get(t, ts.URL+MediaPath+"fonts/X.woff2")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if body != "woff" {
			t.Errorf("media body = %q, want %q", body, "woff")
		}
	})

	t.Run("unknown theme 404s", func(t *testing.T) {
		if _, _, status := get(t, ts.URL+ThemesPath+"no-such-theme/theme.css"); status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", status)
		}
	})

	t.Run("missing file within a known theme 404s", func(t *testing.T) {
		if _, _, status := get(t, ts.URL+ThemesPath+"shared/does-not-exist.css"); status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", status)
		}
	})
}

// writeThemeMediaFixture writes a real, load-valid templates/ library to dir:
// one theme ("shared") whose theme.css reaches the library's shared media tree
// through a reserved-prefix media: url() and its own tree through a relative
// url(), imports a second stylesheet in the same theme directory, and a
// templates/media/ tree owning both the font theme.css references and the CSS
// file the imported stylesheet references.
func writeThemeMediaFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"templates/library.yaml": "name: theme-media-fixture\nformat: 1\n",
		"templates/themes/shared/theme.css": "@import url('more.css');\n\n" +
			"body { background: url('media:fonts/X.woff2'); }\n" +
			".f { src: url('fonts/theme.woff2'); }\n",
		"templates/themes/shared/more.css":          ".m { background: url('media:extra.css'); }\n",
		"templates/themes/shared/fonts/theme.woff2": "theme-font",
		"templates/media/fonts/X.woff2":             "woff",
		"templates/media/extra.css":                 "/* shared media css */\n",
	}
	for name, data := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), data)
	}
}
