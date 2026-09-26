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

	"github.com/really-knows-ai/ey-present/internal/assets"
	"github.com/really-knows-ai/ey-present/internal/deck"
	"github.com/really-knows-ai/ey-present/internal/validate"
)

// TestEmbeddedAssetsServed is the phase-8 integration proof of
// global.constraint.go-static-embedded-binary for the server surface: a real
// listener on 127.0.0.1 serves every page asset out of the binary's embedded
// internal/assets FS while the working directory holds only the deck — an
// eypres.yaml and a slides/ directory, with no assets/, reveal/, fonts/, logo/
// or theme.css anywhere on disk.
//
// It starts a real server (Listen), renders the initial page through the real
// built-in reload pipeline (Reloader), and drives it with a real HTTP client
// over the bound port. It fetches each asset family explicitly — reveal.js core
// (reveal.js, reveal.css, reset.css, notes.js), the theme stylesheet, a brand
// font and a logo — plus the /templates gallery and (after NewErrorPage +
// SetPage) the full-page error document, asserting 200 and non-empty bytes with
// the right media types. It also extracts every /assets/… URL the rendered deck,
// gallery and error pages reference and fetches each, so the page the browser
// receives is proven to resolve entirely from the binary.
//
// Because it binds a real port and reads a real deck directory, it is skipped
// under -short.
func TestEmbeddedAssetsServed(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test binds a real port and reads a real deck directory from disk")
	}

	dir := onlyDeckDir(t)
	// The working directory itself is the deck: no asset files live here. The
	// embedded tree is compiled into the binary, so a request can only succeed
	// if it is served from there.
	t.Chdir(dir)

	srv := mustListen(t, Options{Root: dir})

	// Render the initial deck page through the real built-in pipeline
	// (validate.Validate + render.RenderDeck), which also injects the live-reload
	// client script.
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

	// Explicit asset families: name, URL path under AssetsPath, and a substring
	// the Content-Type must carry. A font is checked separately because its
	// media type comes from content sniffing, not a registered extension.
	families := []struct {
		name string
		path string
		ct   string
	}{
		{"reveal.js core", "reveal/dist/reveal.js", "javascript"},
		{"reveal.js styles", "reveal/dist/reveal.css", "text/css"},
		{"reveal.js reset", "reveal/dist/reset.css", "text/css"},
		{"reveal.js notes plugin", "reveal/dist/plugin/notes.js", "javascript"},
		{"default theme stylesheet", "theme.css", "text/css"},
		{"EY logo", "logo/logo_full-dark.svg", "svg"},
	}
	for _, f := range families {
		status, header, body := httpGetAsset(t, client, base+AssetsPath+f.path)
		if status != http.StatusOK {
			t.Errorf("GET %s%s status %d, want 200", AssetsPath, f.path, status)
			continue
		}
		if len(body) == 0 {
			t.Errorf("GET %s%s served an empty body", AssetsPath, f.path)
		}
		ct := header.Get("Content-Type")
		if !strings.Contains(ct, f.ct) {
			t.Errorf("GET %s%s Content-Type = %q, want it to contain %q", AssetsPath, f.path, ct, f.ct)
		}
		assertAbsentOnDisk(t, dir, f.path)
	}

	// A brand font: 200, real WOFF2 bytes, and a binary (never textual) media
	// type. Nothing named fonts/ exists in the deck directory.
	const fontPath = "fonts/EYInterstate-Regular.woff2"
	status, header, body := httpGetAsset(t, client, base+AssetsPath+fontPath)
	if status != http.StatusOK {
		t.Errorf("GET %s%s status %d, want 200", AssetsPath, fontPath, status)
	}
	if len(body) == 0 {
		t.Errorf("GET %s%s served an empty body", AssetsPath, fontPath)
	}
	if len(body) >= 4 && string(body[:4]) != "wOF2" {
		t.Errorf("GET %s%s body does not begin with the WOFF2 signature (got %q)", AssetsPath, fontPath, body[:4])
	}
	if ct := header.Get("Content-Type"); ct == "" {
		t.Errorf("GET %s%s has no Content-Type", AssetsPath, fontPath)
	} else if strings.HasPrefix(ct, "text/") {
		t.Errorf("GET %s%s Content-Type = %q, want a binary font type", AssetsPath, fontPath, ct)
	}
	assertAbsentOnDisk(t, dir, fontPath)

	// The /templates gallery is served from the embedded gallery page and the
	// compiled-in registry, over the real port.
	status, header, body = httpGetAsset(t, client, base+galleryPath)
	if status != http.StatusOK {
		t.Fatalf("GET %s status %d, want 200 (body %q)", galleryPath, status, body)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET %s Content-Type = %q, want text/html", galleryPath, ct)
	}
	gallery := string(body)
	if !strings.Contains(gallery, `class="ey-gallery__name">title`) {
		t.Errorf("gallery does not list the built-in title template: %q", truncate(gallery))
	}
	assertPageAssetsResolve(t, client, base, gallery, []string{"/assets/theme.css", "/assets/logo/logo_full-light.svg"})

	// The rendered deck page: fetched first as the initial page, with every
	// /assets/… it references resolving from the binary.
	status, header, body = httpGetAsset(t, client, base+rootPath)
	if status != http.StatusOK {
		t.Fatalf("GET %s status %d, want 200 (body %q)", rootPath, status, body)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET %s Content-Type = %q, want text/html", rootPath, ct)
	}
	deckPage := string(body)
	if !strings.Contains(deckPage, "Key messages") {
		t.Errorf("deck page does not carry the rendered starter deck: %q", truncate(deckPage))
	}
	assertPageAssetsResolve(t, client, base, deckPage, []string{
		"/assets/reveal/dist/reset.css",
		"/assets/reveal/dist/reveal.css",
		"/assets/reveal/dist/reveal.js",
		"/assets/reveal/dist/plugin/notes.js",
		"/assets/theme.css",
	})

	// The full-page error document, published through the Page seam exactly as
	// the reloader publishes a broken deck, still resolves its embedded logo and
	// theme over the real port.
	verr := validate.New("slides/2-content.md", 7, []string{"heading"}, "required", "add a heading: value")
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
	assertPageAssetsResolve(t, client, base, errDoc, []string{"/assets/theme.css", "/assets/logo/logo_full-dark.svg"})
}

// onlyDeckDir writes the embedded starter deck (eypres.yaml + slides/) into a
// fresh temp dir and returns it. It asserts the directory holds nothing else:
// no assets/ tree and no asset file of any kind, so every request the test
// makes can only be satisfied from the binary's embedded FS.
func onlyDeckDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	starter := assets.Starter()
	err := fs.WalkDir(starter, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(starter, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatalf("write starter deck to %s: %v", dir, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read deck dir %s: %v", dir, err)
	}
	for _, e := range entries {
		switch e.Name() {
		case deck.ConfigFile, deck.SlidesDir:
		default:
			t.Fatalf("deck dir %s holds unexpected entry %q; it must hold only %s and %s/",
				dir, e.Name(), deck.ConfigFile, deck.SlidesDir)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, deckAssetsDir)); !os.IsNotExist(err) {
		t.Fatalf("deck dir %s has an assets/ path on disk (%v); the embedded tree must be the only source", dir, err)
	}
	return dir
}

// assertAbsentOnDisk fails when name (a path under AssetsPath) exists on disk in
// the deck directory, proving the previously served copy came from the binary.
func assertAbsentOnDisk(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("asset %s exists on disk at %s (%v); the served copy must be embedded", AssetsPath+name, p, err)
	}
}

// pageAssetRefs returns the distinct /assets/… URLs referenced by a served
// HTML document (href/src attributes; the page text never contains quotes in a
// bare URL, so the trailing-quote exclusion is exact).
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
