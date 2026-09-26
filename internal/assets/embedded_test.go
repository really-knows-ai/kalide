package assets

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// readAsset fails the test unless the embedded asset exists, returning its
// contents. It is the reading counterpart to mustRead for checks that inspect a
// file's contents rather than only its presence.
func readAsset(t *testing.T, fsys fs.FS, name string) []byte {
	t.Helper()
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("embedded asset %q: %v", name, err)
	}
	if len(data) == 0 {
		t.Fatalf("embedded asset %q is empty", name)
	}
	return data
}

// TestEmbeddedComplete states the whole embedded-binary completeness contract in
// one place: the vendored reveal.js runtime (core, print-pdf styles bundled in
// dist/reveal.css, the notes plugin, LICENSE) and the three full-page shells.
// Templates, themes, fonts, logos and the starter deck are no longer embedded —
// they live on disk under a project's own templates/ (phase 7). It also proves
// the served pages are OFFLINE: no page references an off-host URL
// (global.constraint.go-static-embedded-binary), and carry no brand asset
// reference (logo/font URL) of their own.
//
// It complements the finer per-accessor tests in assets_test.go.
func TestEmbeddedComplete(t *testing.T) {
	t.Run("reveal.js core, print support and notes plugin", func(t *testing.T) {
		mustRead(t, Reveal(), "dist/reveal.js")
		mustRead(t, Reveal(), "dist/reset.css")
		mustRead(t, Reveal(), "dist/plugin/notes.js")
		mustRead(t, Reveal(), "LICENSE")

		// reveal.js 6.x has no separate print-pdf plugin or stylesheet: the
		// print styles are bundled in dist/reveal.css and ?print-pdf is core.
		css := string(readAsset(t, Reveal(), "dist/reveal.css"))
		for _, marker := range []string{"print-pdf", "@media print"} {
			if !strings.Contains(css, marker) {
				t.Errorf("dist/reveal.css: missing print-pdf support marker %q", marker)
			}
		}
	})

	t.Run("full-page templates", func(t *testing.T) {
		for _, name := range []string{"deck.html.tmpl", "error.html.tmpl", "gallery.html.tmpl"} {
			t.Run(name, func(t *testing.T) {
				mustRead(t, Pages(), name)
			})
		}
		// DeckPage is the narrow view internal/render executes.
		mustRead(t, DeckPage(), "deck.html.tmpl")
	})

	t.Run("no templates, themes, fonts, logo or starter embedded", func(t *testing.T) {
		for _, name := range []string{
			"templates", "fonts", "logo", "starter", "theme.css",
		} {
			if _, err := fs.Stat(FS, name); err == nil {
				t.Errorf("embedded FS unexpectedly contains %q; that content now lives in a project's templates/ on disk", name)
			}
		}

		// Only reveal/, pages/ and LICENSE (reveal's, nested under reveal/) may
		// appear at the embedded FS root.
		entries, err := fs.ReadDir(FS, ".")
		if err != nil {
			t.Fatalf("read embedded FS root: %v", err)
		}
		allowed := map[string]bool{"reveal": true, "pages": true}
		for _, e := range entries {
			if !allowed[e.Name()] {
				t.Errorf("embedded FS root has unexpected entry %q; want only reveal/ and pages/", e.Name())
			}
		}
	})

	t.Run("offline: no external references", func(t *testing.T) {
		type asset struct {
			fsys fs.FS
			path string
		}
		var assets []asset
		for _, p := range []string{"deck.html.tmpl", "error.html.tmpl", "gallery.html.tmpl"} {
			assets = append(assets, asset{Pages(), p})
		}

		for _, a := range assets {
			src := string(readAsset(t, a.fsys, a.path))

			// Comments may legitimately mention a URL, so strip them before
			// scanning for a real external reference.
			if loc := externalURL.FindString(stripComments(src)); loc != "" {
				t.Errorf("%s references external URL %q", a.path, loc)
			}

			// Every src=, href= and url(...) must resolve inside the embedded
			// tree: a path-relative or /assets/ reference, never an off-host
			// host or a protocol-relative //host.
			for _, ref := range assetRefs(src) {
				if isOffHost(ref) {
					t.Errorf("%s: reference %q points off-host; embedded assets must be offline", a.path, ref)
				}
			}
		}
	})

	t.Run("page shells carry no brand asset reference", func(t *testing.T) {
		// No page may reference a font or logo asset URL directly: brand
		// styling comes entirely from the project's own theme stylesheet.
		brandRef := regexp.MustCompile(`(?i)(/assets/(fonts|logo)/|logo_(full|small)-(light|dark)\.svg|\.woff2)`)
		for _, p := range []string{"deck.html.tmpl", "error.html.tmpl", "gallery.html.tmpl"} {
			src := string(readAsset(t, Pages(), p))
			if loc := brandRef.FindString(src); loc != "" {
				t.Errorf("%s: unexpected brand asset reference %q", p, loc)
			}
		}
	})

	// Negative control: prove the offline scan actually flags planted external
	// references rather than passing vacuously.
	t.Run("offline scan detects planted external refs", func(t *testing.T) {
		offending := []string{
			`<script src="https://cdn.example.com/reveal.js"></script>`,
			`<link href="http://fonts.example.com/x.css">`,
			`@font-face { src: url(//cdn.example.com/f.woff2); }`,
		}
		for _, src := range offending {
			var found string
			if loc := externalURL.FindString(stripComments(src)); loc != "" {
				found = loc
			}
			for _, ref := range assetRefs(src) {
				if isOffHost(ref) {
					found = ref
				}
			}
			if found == "" {
				t.Errorf("offline scan missed a planted external reference in %q", src)
			}
		}

		// And that an in-tree reference and a URL mentioned only in a comment
		// are not false positives.
		benign := []struct {
			name string
			src  string
		}{
			{"embedded path", `<link rel="stylesheet" href="/assets/reveal/dist/reveal.css">`},
			{"relative url", `@font-face { src: url('fonts/EYInterstate-Regular.woff2'); }`},
			{"comment mention", `{{/* never at https://cdn.example.com */}}<link href="/assets/theme.css">`},
			{"html comment mention", `<!-- see https://example.com --><link href="/assets/theme.css">`},
			{"css comment mention", "/* no https://example.com */ body { color: red; }"},
		}
		for _, b := range benign {
			if externalURL.FindString(stripComments(b.src)) != "" {
				t.Errorf("%s: false positive external URL", b.name)
			}
			for _, ref := range assetRefs(stripComments(b.src)) {
				if isOffHost(ref) {
					t.Errorf("%s: false positive off-host reference %q", b.name, ref)
				}
			}
		}
	})
}

var (
	goTemplateComment = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`)
	htmlComment       = regexp.MustCompile(`(?s)<!--.*?-->`)
	cssComment        = regexp.MustCompile(`(?s)/\*.*?\*/`)
	externalURL       = regexp.MustCompile(`(?i)https?://`)
	assetRef          = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["']([^"']*)["']|url\(\s*['"]?([^'")]+)`)
)

// stripComments removes Go template, HTML and CSS comments so a URL mentioned in
// documentation is not mistaken for a served asset reference.
func stripComments(src string) string {
	src = goTemplateComment.ReplaceAllString(src, "")
	src = htmlComment.ReplaceAllString(src, "")
	return cssComment.ReplaceAllString(src, "")
}

// assetRefs extracts the src="…", href="…" and url(…) values in src.
func assetRefs(src string) []string {
	var refs []string
	for _, m := range assetRef.FindAllStringSubmatch(src, -1) {
		for _, g := range m[1:] {
			if g != "" {
				refs = append(refs, strings.TrimSpace(g))
			}
		}
	}
	return refs
}

// isOffHost reports whether a reference leaves the embedded tree: an absolute
// http(s) URL or a protocol-relative //host reference.
func isOffHost(ref string) bool {
	lower := strings.ToLower(ref)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(ref, "//")
}
