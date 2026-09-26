package server

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// assetHandler serves the URL space under AssetsPath for a request whose
// leading AssetsPath has already been stripped (the mux registers it with
// http.StripPrefix). A name is resolved against the embedded asset tree first
// — the reveal.js dist and the page shells under pages/ — and then against
// the deck's own assets/ directory.
//
// Embedded-first means a deck cannot shadow a file the rendered page depends
// on: the deck page's `/assets/reveal/…` URLs always resolve to the vendored
// copy. A deck's own images live under their own names and no embedded file
// shares one.
//
// Only files are served. A path whose cleaned form escapes its root (an
// encoded "..") and a request for a directory are rejected, so the handler
// never lists or traverses outside the two trees.
type assetHandler struct {
	embedded fs.FS
	deck     fs.FS // the deck's assets/ sub-tree; may be nil
}

// newAssetHandler returns a handler over the embedded tree and, when non-nil,
// the deck's assets/ sub-tree.
func newAssetHandler(embedded, deck fs.FS) http.Handler {
	return &assetHandler{embedded: embedded, deck: deck}
}

// ServeHTTP resolves and serves one asset.
func (h *assetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name, ok := assetName(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if fsys, found := h.firstContaining(name); found {
		serveFileFS(w, r, fsys, name)
		return
	}
	http.NotFound(w, r)
}

// firstContaining returns the first filesystem holding a regular file at name,
// embedded before deck, and whether any does.
func (h *assetHandler) firstContaining(name string) (fs.FS, bool) {
	if isRegularFile(h.embedded, name) {
		return h.embedded, true
	}
	if h.deck != nil && isRegularFile(h.deck, name) {
		return h.deck, true
	}
	return nil, false
}

// assetName normalizes a stripped request path to a clean, relative fs path,
// rejecting anything that could escape a root or name a directory.
func assetName(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	clean := path.Clean("/" + p) // forces a leading slash, collapsing ".."
	if clean == "/" || strings.HasSuffix(clean, "/") {
		return "", false
	}
	return strings.TrimPrefix(clean, "/"), true
}

// isRegularFile reports whether name exists in fsys and is a regular file; a
// directory, a missing path or an unreadable one is not served.
func isRegularFile(fsys fs.FS, name string) bool {
	if fsys == nil {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}

// serveFileFS writes the named file from fsys, delegating range requests,
// HEAD handling, content sniffing and Content-Type to http.ServeContent.
func serveFileFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	f, err := fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	// http.ServeContent uses the name only to sniff a Content-Type; the path
	// is a plain asset name, never a host or scheme. embed.FS's files and
	// os.DirFS's files are both io.ReadSeeker; a filesystem whose file is not
	// (a test's in-memory FS) is buffered instead, which costs range support
	// but still serves the bytes.
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, path.Base(name), info.ModTime(), rs)
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, path.Base(name), info.ModTime(), bytes.NewReader(data))
}
