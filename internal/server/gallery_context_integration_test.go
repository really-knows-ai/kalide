package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
)

// gallerySlideContextProbeTemplateYAML, gallerySlideContextProbeLayout and
// gallerySlideContextProbeExample make up a slide-usage template whose layout
// reads the reserved `.deck.title`/`.deck.properties`/`.slide.number` context
// (template-context): the gallery has no served deck, so gallerySlideExample
// must supply a well-formed but empty non-nil deck context rather than let a
// nil reach the template, or executing this layout would fail with a
// required-context error.
const (
	gallerySlideContextProbeTemplateYAML = `description: slide template probing the gallery's non-nil deck context
fields:
  - name: title
    type: text
    required: true
body:
  mode: optional
`

	gallerySlideContextProbeLayout = `<section>
  <h1>{{.title}}</h1>
  <span class="gallery-deck-title">{{.deck.title}}</span>
  <span class="gallery-slide-number">{{.slide.number}}</span>
  <span class="gallery-audience">{{index .deck.properties "audience"}}</span>
</section>
`

	gallerySlideContextProbeExample = `---
template: gallerycontextprobeslide
title: Example
---
An example slide probing the gallery's deck context.
`

	// gallerySectionContextProbeTemplateYAML, gallerySectionContextProbeLayout
	// and gallerySectionContextProbeExample make up a section-usage template
	// whose layout reads the same reserved context: gallerySectionExample
	// wraps it in a synthetic slide, which must also thread a well-formed
	// non-nil deck context through, at the nested section-instance depth
	// (section-template-context).
	gallerySectionContextProbeTemplateYAML = `description: section template probing the gallery's non-nil deck context
fields:
  - name: title
    type: text
body:
  mode: optional
`

	gallerySectionContextProbeLayout = `<div class="gallery-section-probe">
  <span class="gallery-section-deck-title">{{.deck.title}}</span>
  <span class="gallery-section-slide-number">{{.slide.number}}</span>
  <span class="gallery-section-audience">{{index .deck.properties "audience"}}</span>
</div>
`

	gallerySectionContextProbeExample = "```\ntitle: Example section\n```\n"
)

// TestGalleryPreviewsExecuteWithNonNilDeckContext proves the `/templates`
// gallery previews (server.gallerySlideExample and gallerySectionExample)
// execute a layout reading `.deck.title`/`.deck.properties`/`.slide.number`
// without a required-context execution error, even though the gallery has no
// served deck: it builds a real on-disk library (the fixture library plus a
// slide-usage and a section-usage probe template), loads it through
// template.LoadLibrary/NewRegistryFromLibrary — the same load path the running
// server uses — and asserts the gallery page renders both previews' reserved
// context reads (as empty/zero values) rather than falling back to the
// "Example unavailable" placeholder. It reads the real filesystem, so it is
// skipped under -short.
func TestGalleryPreviewsExecuteWithNonNilDeckContext(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real template library from disk")
	}

	dir := t.TempDir()
	copyGalleryFixtureLibrary(t, dir)
	writeGalleryProbeTemplate(t, dir, "slides", "gallerycontextprobeslide",
		gallerySlideContextProbeTemplateYAML, gallerySlideContextProbeLayout, gallerySlideContextProbeExample)
	writeGalleryProbeTemplate(t, dir, "sections", "gallerycontextprobesection",
		gallerySectionContextProbeTemplateYAML, gallerySectionContextProbeLayout, gallerySectionContextProbeExample)

	fsys := os.DirFS(dir)
	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	funcMap := template.LayoutFuncMap(lib.Media, MediaPath)

	rec := httptest.NewRecorder()
	galleryHandler(reg, funcMap).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, galleryPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status %d, want 200 (body %q)", galleryPath, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if strings.Contains(body, "Example unavailable") {
		t.Fatalf("gallery reports an unavailable example (a required-context execution error), want both previews to execute:\n%s", body)
	}

	for _, want := range []string{
		// The slide-usage probe's layout: an empty deck title/audience and the
		// zero-value slide position label, all well-formed (never a nil
		// dereference or a required-context execution error).
		`<span class="gallery-deck-title"></span>`,
		`<span class="gallery-slide-number">0</span>`,
		`<span class="gallery-audience"></span>`,
		// The section-usage probe's layout, rendered inside the synthetic
		// wrapper slide gallerySectionExample builds.
		`<span class="gallery-section-deck-title"></span>`,
		`<span class="gallery-section-slide-number">0</span>`,
		`<span class="gallery-section-audience"></span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("gallery body does not contain %q:\n%s", want, body)
		}
	}
}

// copyGalleryFixtureLibrary copies the phase-3 fixture library's templates/
// tree into dir, so LoadLibrary has a complete, valid library to load
// alongside the probe templates this test adds.
func copyGalleryFixtureLibrary(t *testing.T, dir string) {
	t.Helper()
	src := filepath.Join(fixtureLibraryDir, "templates")
	dst := filepath.Join(dir, "templates")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read fixture library dir %s: %v", src, err)
	}
	for _, e := range entries {
		copyGalleryDirRecursive(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
	}
}

// copyGalleryDirRecursive recursively copies src to dst, both real
// directories or files.
func copyGalleryDirRecursive(t *testing.T, src, dst string) {
	t.Helper()
	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat %s: %v", src, err)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dst, err)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatalf("read dir %s: %v", src, err)
		}
		for _, e := range entries {
			copyGalleryDirRecursive(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
		}
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// writeGalleryProbeTemplate writes one template's manifest, layout and
// example under dir/templates/<kind>/<name>/.
func writeGalleryProbeTemplate(t *testing.T, dir, kind, name, templateYAML, layout, example string) {
	t.Helper()
	probeDir := filepath.Join(dir, "templates", kind, name)
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", probeDir, err)
	}
	for fname, data := range map[string]string{
		"template.yaml":    templateYAML,
		"layout.html.tmpl": layout,
		"example.md":       example,
	} {
		if err := os.WriteFile(filepath.Join(probeDir, fname), []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", filepath.Join(probeDir, fname), err)
		}
	}
}
