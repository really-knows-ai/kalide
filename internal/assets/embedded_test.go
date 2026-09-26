package assets

import (
	"io/fs"
	"path"
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
// dist/reveal.css, the notes plugin, LICENSE), the default theme CSS, the EY
// fonts and logos, every built-in template's layout and example, the starter
// deck (eypres.yaml + slides), and the three full-page shells. It also proves the
// served pages and stylesheets are OFFLINE: no page, layout or theme references
// an off-host URL (global.constraint.go-static-embedded-binary).
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

	t.Run("default theme CSS", func(t *testing.T) {
		mustRead(t, FS, "theme.css")
	})

	t.Run("EY fonts", func(t *testing.T) {
		fonts := Fonts()
		var (
			files              []string
			interstate, gothic bool
		)
		err := fs.WalkDir(fonts, ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(name, ".woff2") {
				return nil
			}
			files = append(files, name)
			mustRead(t, fonts, name)
			switch base := path.Base(name); {
			case strings.HasPrefix(base, "EYInterstate"):
				interstate = true
			case strings.HasPrefix(base, "EYGothic"):
				gothic = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk embedded fonts: %v", err)
		}
		if len(files) == 0 {
			t.Fatal("no .woff2 fonts embedded")
		}
		if !interstate {
			t.Errorf("no EYInterstate .woff2 font embedded; found %v", files)
		}
		if !gothic {
			t.Errorf("no EYGothic .woff2 font embedded; found %v", files)
		}
	})

	t.Run("EY logos", func(t *testing.T) {
		for _, name := range []string{
			"logo_full-light.svg",
			"logo_full-dark.svg",
			"logo_small-light.svg",
			"logo_small-dark.svg",
		} {
			t.Run(name, func(t *testing.T) {
				mustRead(t, Logo(), name)
			})
		}
	})

	t.Run("built-in templates", func(t *testing.T) {
		for _, name := range []string{"title", "content", "column"} {
			for _, file := range []string{"layout.html.tmpl", "example.md"} {
				t.Run(name+"/"+file, func(t *testing.T) {
					mustRead(t, Templates(), name+"/"+file)
				})
			}
		}
	})

	t.Run("starter deck", func(t *testing.T) {
		mustRead(t, Starter(), "eypres.yaml")
		for _, name := range []string{"slides/1-title.md", "slides/2-content.md"} {
			mustRead(t, Starter(), name)
		}

		// The starter tree is the source of truth `eypres init` copies; walking
		// it proves the shipped slide sources are all present and non-empty.
		var slides []string
		err := fs.WalkDir(Starter(), ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasPrefix(name, "slides/") && strings.HasSuffix(name, ".md") {
				slides = append(slides, name)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk starter deck: %v", err)
		}
		if len(slides) < 2 {
			t.Errorf("starter deck embeds %d slides, want at least 2: %v", len(slides), slides)
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

	t.Run("offline: no external references", func(t *testing.T) {
		type asset struct {
			fsys fs.FS
			path string
		}
		assets := []asset{{FS, "theme.css"}}
		for _, p := range []string{"deck.html.tmpl", "error.html.tmpl", "gallery.html.tmpl"} {
			assets = append(assets, asset{Pages(), p})
		}
		for _, l := range []string{"title", "content", "column"} {
			assets = append(assets, asset{Templates(), l + "/layout.html.tmpl"})
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
