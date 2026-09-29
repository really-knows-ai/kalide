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

	prefix := template.MediaURLPrefix()
	themePrefix := template.ThemeURLPrefix()
	mediaURL := func(clean string) string { return `url("` + MediaPath + clean + `")` }
	themeURL := func(name, clean string) string { return `url("` + ThemesPath + name + "/" + clean + `")` }

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
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got, want := string(raw), "<svg></svg>"; got != want {
			t.Errorf("non-CSS media body = %q, want raw %q", got, want)
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

	t.Run("media stylesheet is served text/css with reserved-prefix references rewritten", func(t *testing.T) {
		resp, err := http.Get(ts.URL + MediaPath + "style.css")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
			t.Errorf("Content-Type = %q, want it to start with text/css", ct)
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body := string(raw)

		// The media: reference becomes the media route URL and the theme:<name>/
		// reference the theme route URL, in both the url() and the @import form;
		// the relative url('logo.svg') stays byte-for-byte.
		if want := mediaURL("fonts/x.woff2"); !strings.Contains(body, want) {
			t.Errorf("body missing rewritten %s:\n%s", want, body)
		}
		if want := themeURL("plain", "logo.svg"); !strings.Contains(body, want) {
			t.Errorf("body missing rewritten %s:\n%s", want, body)
		}
		if want := `@import url("` + ThemesPath + `plain/theme.css")`; !strings.Contains(body, want) {
			t.Errorf("body missing canonicalised %s:\n%s", want, body)
		}
		if !strings.Contains(body, "url('logo.svg')") {
			t.Errorf("media-owned relative url('logo.svg') was not left unchanged:\n%s", body)
		}
		if strings.Contains(body, prefix) {
			t.Errorf("body still contains reserved prefix %q:\n%s", prefix, body)
		}
		if strings.Contains(body, themePrefix) {
			t.Errorf("body still contains reserved prefix %q:\n%s", themePrefix, body)
		}
		assertRewrittenURLsResolve(t, ts.URL, body)
	})

	t.Run("non-CSS media file is served raw", func(t *testing.T) {
		resp, err := http.Get(ts.URL + MediaPath + "notes.txt")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got, want := string(raw), "url('media:fonts/x.woff2')\n"; got != want {
			t.Errorf("non-CSS media body = %q, want raw %q", got, want)
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
// slide template (so template.LoadLibrary succeeds), one theme and a media
// tree owning a raw SVG, a raw non-CSS text file, and a stylesheet reaching
// both reserved prefixes (media: and theme:<name>/) in the url() and @import
// forms.
func writeMediaFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"templates/library.yaml":                  "name: media-fixture\nformat: 1\n",
		"templates/slides/hello/template.yaml":    "fields:\n  - name: title\n    type: text\n    required: true\n",
		"templates/slides/hello/layout.html.tmpl": "<section><h1>{{.title}}</h1></section>",
		"templates/slides/hello/example.md":       "---\ntitle: Hi\n---\n",
		"templates/themes/plain/theme.css":        "body { margin: 0; }\n",
		"templates/themes/plain/logo.svg":         "<svg>plain</svg>",
		"templates/media/logo.svg":                "<svg></svg>",
		"templates/media/fonts/x.woff2":           "woff",
		"templates/media/notes.txt":               "url('media:fonts/x.woff2')\n",
		"templates/media/style.css": "/* media css */\n" +
			"@import url('theme:plain/theme.css');\n" +
			".a { background: url('logo.svg'); }\n" +
			".b { background: url('media:fonts/x.woff2'); }\n" +
			".c { background: url('theme:plain/logo.svg'); }\n",
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
// theme route's serve-time reserved-prefix rewrite (theme-shared-media): a real
// on-disk library is loaded through template.LoadLibrary and theme.LoadDir,
// mediaHandler is mounted behind a real HTTP server, and the bytes a client
// receives for a theme stylesheet have every reserved-prefix reference — both
// media: (shared media/) and theme:<name>/ (a sibling theme) — rewritten to the
// URL the corresponding route serves, in the url() form and in both @import
// forms.
//
// It covers theme.css itself, a stylesheet the theme imports from its own
// directory, and a stylesheet it imports from another theme reached through
// theme:<name>/; proves a theme-owned relative url() and a relative @import
// survive byte-for-byte; and fetches every rewritten URL at the route it names
// so each rewrite is known to resolve rather than merely look right. It reads
// a real filesystem, so it is guarded with -short.
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
	themePrefix := template.ThemeURLPrefix()
	// mediaURL and themeURL are the served forms the rewriter emits for a
	// reserved-prefix reference, built from each route's own prefix rather than
	// hardcoded.
	mediaURL := func(clean string) string { return `url("` + MediaPath + clean + `")` }
	themeURL := func(name, clean string) string { return `url("` + ThemesPath + name + "/" + clean + `")` }

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

	t.Run("theme.css reserved-prefix refs and both @import forms rewritten, relative kept", func(t *testing.T) {
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
		if want := themeURL("other", "logo.svg"); !strings.Contains(body, want) {
			t.Errorf("body missing rewritten %s:\n%s", want, body)
		}
		// The cross-theme @import target is canonicalised to the url() form
		// rooted at the themes route.
		if want := "@import url(\"" + ThemesPath + "other/other.css\")"; !strings.Contains(body, want) {
			t.Errorf("body missing canonicalised %s:\n%s", want, body)
		}
		if !strings.Contains(body, "url('fonts/theme.woff2')") {
			t.Errorf("theme-owned url('fonts/theme.woff2') was not left unchanged:\n%s", body)
		}
		if !strings.Contains(body, "@import url('more.css')") {
			t.Errorf("relative @import url('more.css') was not left unchanged:\n%s", body)
		}
		if strings.Contains(body, prefix) {
			t.Errorf("body still contains reserved prefix %q:\n%s", prefix, body)
		}
		if strings.Contains(body, themePrefix) {
			t.Errorf("body still contains reserved prefix %q:\n%s", themePrefix, body)
		}
	})

	t.Run("stylesheet imported from the theme's own directory is rewritten too", func(t *testing.T) {
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

	t.Run("stylesheet imported from another theme is rewritten in turn", func(t *testing.T) {
		body, _, status := get(t, ts.URL+ThemesPath+"other/other.css")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if want := themeURL("shared", "fonts/theme.woff2"); !strings.Contains(body, want) {
			t.Errorf("body missing rewritten %s:\n%s", want, body)
		}
		if want := mediaURL("extra.css"); !strings.Contains(body, want) {
			t.Errorf("body missing rewritten %s:\n%s", want, body)
		}
		if strings.Contains(body, prefix) {
			t.Errorf("body still contains reserved prefix %q:\n%s", prefix, body)
		}
		if strings.Contains(body, themePrefix) {
			t.Errorf("body still contains reserved prefix %q:\n%s", themePrefix, body)
		}
	})

	t.Run("every rewritten URL resolves at the route it names", func(t *testing.T) {
		for _, stylesheet := range []string{"shared/theme.css", "shared/more.css", "other/other.css"} {
			body, _, status := get(t, ts.URL+ThemesPath+stylesheet)
			if status != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", stylesheet, status)
			}
			assertRewrittenURLsResolve(t, ts.URL, body)
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
// through a reserved-prefix media: url() and a sibling theme ("other") through
// a reserved-prefix theme:<name>/ url(), keeps its own tree through a relative
// url(), imports a stylesheet from its own directory via a relative @import and
// one from the sibling theme via a theme:<name>/ @import. The sibling theme's
// stylesheet reaches back into shared/ and into media/. The templates/media/
// tree owns the font theme.css references and the CSS file the imported
// stylesheets reference.
func writeThemeMediaFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"templates/library.yaml": "name: theme-media-fixture\nformat: 1\n",
		"templates/themes/shared/theme.css": "@import url('more.css');\n" +
			"@import url('theme:other/other.css');\n\n" +
			"body { background: url('media:fonts/X.woff2'); }\n" +
			".logo { background: url('theme:other/logo.svg'); }\n" +
			".f { src: url('fonts/theme.woff2'); }\n",
		"templates/themes/shared/more.css":          ".m { background: url('media:extra.css'); }\n",
		"templates/themes/shared/fonts/theme.woff2": "theme-font",
		"templates/themes/other/theme.css":          "/* other theme */\n",
		"templates/themes/other/other.css": ".o { background: url('theme:shared/fonts/theme.woff2'); }\n" +
			".o2 { background: url('media:extra.css'); }\n",
		"templates/themes/other/logo.svg": "<svg>other</svg>",
		"templates/media/fonts/X.woff2":   "woff",
		"templates/media/extra.css":       "/* shared media css */\n",
	}
	for name, data := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), data)
	}
}

// assertRewrittenURLsResolve fetches every absolute url(...) target appearing
// in body — the double-quoted shape rewriteThemeCSS emits — and requires each
// to be served 200 at the route it names (themes/ for a theme:<name>/ rewrite,
// media/ for a media: one). Relative targets — an untouched relative reference
// left byte-for-byte — are skipped.
func assertRewrittenURLsResolve(t *testing.T, baseURL, body string) {
	t.Helper()
	for _, m := range themeCSSURLPattern.FindAllStringSubmatch(body, -1) {
		target := m[1]
		if !strings.HasPrefix(target, "/") {
			continue
		}
		resp, err := http.Get(baseURL + target)
		if err != nil {
			t.Fatalf("GET %s: %v", target, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("rewritten URL %s status = %d, want 200", target, resp.StatusCode)
		}
	}
}
