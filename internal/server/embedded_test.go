package server

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// TestEmbeddedAssetsServed is the phase-3 integration proof that a page's
// asset references resolve from two sources with nothing else on disk
// beyond the deck itself: reveal.js core (embedded in the binary,
// internal/assets) and the project's own templates/media and
// templates/themes/<name> files (served from the project's templates/
// library, template-media). A real listener on 127.0.0.1 serves the whole
// page while the working directory holds only the deck and its own
// templates/ library — no separate assets/ tree beyond the deck's own.
//
// It starts a real server (Listen), renders the initial page through the
// real built-in reload pipeline (Reloader) over the phase-3 fixture library
// copied into the deck directory, and drives it with a real HTTP client over
// the bound port.
//
// Because it binds a real port and reads a real deck directory, it is
// skipped under -short.
func TestEmbeddedAssetsServed(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test binds a real port and reads a real deck directory from disk")
	}

	dir := fixtureDeckDir(t)
	t.Chdir(dir)

	fsys := os.DirFS(dir)
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	themes, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}

	srv := mustListen(t, Options{Root: dir, Library: lib, Themes: themes})

	rl, err := NewReloader(ReloadOptions{Server: srv, Root: dir, Log: io.Discard})
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}
	if err := rl.Start(); err != nil {
		t.Fatalf("Reloader.Start: %v", err)
	}
	t.Cleanup(rl.Stop)

	base := strings.TrimSuffix(srv.URL(), "/")
	client := &http.Client{Timeout: 10 * time.Second}

	// reveal.js core, fetched from the embedded binary.
	for _, path := range []string{
		"reveal/dist/reveal.js",
		"reveal/dist/reveal.css",
		"reveal/dist/reset.css",
		"reveal/dist/plugin/notes.js",
	} {
		status, header, body := httpGetAsset(t, client, base+AssetsPath+path)
		if status != http.StatusOK {
			t.Errorf("GET %s%s status %d, want 200", AssetsPath, path, status)
			continue
		}
		if len(body) == 0 {
			t.Errorf("GET %s%s served an empty body", AssetsPath, path)
		}
		if header.Get("Content-Type") == "" {
			t.Errorf("GET %s%s has no Content-Type", AssetsPath, path)
		}
	}

	// The rendered deck page, served at "/": its media and theme references
	// resolve from the project's own templates/ library on disk.
	status, header, body := httpGetAsset(t, client, base+rootPath)
	if status != http.StatusOK {
		t.Fatalf("GET %s status %d, want 200 (body %q)", rootPath, status, body)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET %s Content-Type = %q, want text/html", rootPath, ct)
	}
	deckPage := string(body)
	if !strings.Contains(deckPage, "<h1>Integration Deck</h1>") {
		t.Errorf("deck page does not carry the rendered fixture title:\n%s", truncate(deckPage))
	}
	assertPageAssetsResolve(t, client, base, deckPage, []string{
		"/assets/reveal/dist/reset.css",
		"/assets/reveal/dist/reveal.css",
		"/assets/reveal/dist/reveal.js",
		"/assets/reveal/dist/plugin/notes.js",
		"/assets/templates/themes/plain/theme.css",
		"/assets/templates/media/logo.svg",
	})

	// The /templates gallery, served from the embedded gallery page and the
	// project's library-built registry.
	status, header, body = httpGetAsset(t, client, base+galleryPath)
	if status != http.StatusOK {
		t.Fatalf("GET %s status %d, want 200 (body %q)", galleryPath, status, body)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET %s Content-Type = %q, want text/html", galleryPath, ct)
	}
	gallery := string(body)
	if !strings.Contains(gallery, `class="gallery-page__name">hello`) {
		t.Errorf("gallery does not list the fixture hello template: %q", truncate(gallery))
	}

	// The full-page error document, published through the Page seam exactly
	// as the reloader publishes a broken deck, still resolves over the real
	// port (it references only the embedded assets, no theme/media).
	verr := validate.New("slides/1-hello.md", 3, []string{"title"}, "required", "add a title: value")
	errPage, err := NewErrorPage("My presentation", verr)
	if err != nil {
		t.Fatalf("NewErrorPage: %v", err)
	}
	srv.SetPage(errPage)

	status, header, body = httpGetAsset(t, client, base+rootPath)
	if status != http.StatusOK {
		t.Fatalf("GET %s (error page) status %d, want 200 (body %q)", rootPath, status, body)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET %s (error page) Content-Type = %q, want text/html", rootPath, ct)
	}
	errDoc := string(body)
	if want := validate.Format(verr); !strings.Contains(errDoc, want) {
		t.Errorf("error page does not carry the formatted error %q: %q", want, truncate(errDoc))
	}
}

// fixtureDeckDir writes a real deck directory: eypres.yaml, one slide using
// the phase-3 fixture library's hello template, and a copy of the fixture
// templates/ library itself, so RenderDeck's media and theme URLs resolve
// from the project's own templates/ tree on disk.
func fixtureDeckDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"eypres.yaml": "title: Integration Deck\n" +
			"theme: plain\n",
		"slides/1-hello.md": "---\ntemplate: hello\ntitle: Integration Deck\n---\n",
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

	err := fs.WalkDir(os.DirFS(fixtureLibraryDir), "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := fs.ReadFile(os.DirFS(fixtureLibraryDir), p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture templates/ into deck dir: %v", err)
	}
	return dir
}

// pageAssetRefs returns the distinct /assets/… URLs referenced by a served
// HTML document (href/src attributes; the page text never contains quotes in
// a bare URL, so the trailing-quote exclusion is exact).
var pageAssetRefsRE = regexp.MustCompile(`"/assets/[^"'\s)]+`)

func pageAssetRefs(doc string) []string {
	seen := make(map[string]struct{})
	var refs []string
	for _, m := range pageAssetRefsRE.FindAllString(doc, -1) {
		ref := strings.TrimPrefix(m, `"`)
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

// assertPageAssetsResolve fetches every /assets/… reference in doc and asserts
// each is a non-empty 200 from the running server, and that doc references each
// of the required paths.
func assertPageAssetsResolve(t *testing.T, client *http.Client, base, doc string, required []string) {
	t.Helper()

	refs := pageAssetRefs(doc)
	if len(refs) == 0 {
		t.Errorf("page references no /assets/… URLs: %q", truncate(doc))
		return
	}
	for _, want := range required {
		found := false
		for _, ref := range refs {
			if ref == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("page does not reference %s (references %v)", want, refs)
		}
	}
	for _, ref := range refs {
		status, header, body := httpGetAsset(t, client, base+ref)
		if status != http.StatusOK {
			t.Errorf("GET %s status %d, want 200", ref, status)
			continue
		}
		if len(body) == 0 {
			t.Errorf("GET %s served an empty body", ref)
		}
		if header.Get("Content-Type") == "" {
			t.Errorf("GET %s has no Content-Type", ref)
		}
	}
}

// httpGetAsset performs one real GET over the network and returns the status,
// headers and fully read body.
func httpGetAsset(t *testing.T, client *http.Client, url string) (int, http.Header, []byte) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, resp.Header, body
}

// truncate keeps a failure message readable without dropping the evidence.
func truncate(s string) string {
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
