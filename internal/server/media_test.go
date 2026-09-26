package server

import (
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
