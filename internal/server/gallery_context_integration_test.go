package server

import (
	"io/fs"
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

// galleryLayeredExternalSectionName / galleryLayeredLocalSlideName name the two
// templates the two-layer gallery fixture contributes: a SECTION from the
// external `templates:` layer and a SLIDE from the deck-root templates/ local
// layer. The descriptions are the manifest one-liners the gallery shows, so an
// assertion on them proves the page documents that layer's template (not just a
// name that could come from either).
const (
	galleryLayeredExternalSectionName = "extsection"
	galleryLayeredLocalSlideName      = "localslide"

	galleryLayeredExternalSectionDescription = "external section description"
	galleryLayeredLocalSlideDescription      = "local slide description"

	// galleryLayeredExternalMediaFile and galleryLayeredLocalMediaFile exist
	// ONLY in their own layer's media/ tree, so the rendered gallery preview's
	// media URL names the layer that actually resolved it: a preview bound to
	// the wrong layer's media filesystem cannot resolve the other layer's file.
	galleryLayeredExternalMediaFile = "ext-logo.svg"
	galleryLayeredLocalMediaFile    = "local-logo.svg"
)

// TestGalleryListsTemplatesFromBothLayers is the int-tier proof
// (plan.phase-04.task-18) that the `/templates` gallery documents the deck's
// MERGED two-layer library — the external `templates:` layer and the deck-root
// templates/ local layer — and renders each layer's example preview against
// that layer's own media root (templates-gallery, layered-template-library).
//
// It writes a real two-layer deck to temporary directories: the external layer
// contributes a section template whose layout resolves its own
// media/ext-logo.svg, and the local layer contributes a slide template whose
// layout resolves its own media/local-logo.svg. It loads the merged library
// through template.LoadDeckLibrary — the same path `kalide start` uses — builds
// the registry and func map the way server.Listen does, and serves
// galleryHandler directly, so it is skipped under -short.
//
// The test proves three things:
//
//   - listing both layers: the gallery page shows the external layer's section
//     AND the local layer's slide, by name and by manifest description.
//   - the two-layer registryWithTemplate path: a SECTION example is rendered by
//     gallerySectionExample, which places the synthetic wrapper slide in the
//     section's OWN owning layer through registryWithTemplate. Because the
//     external section's media file exists only in the external layer, its
//     preview resolving `{{ media "ext-logo.svg" }}` to the external per-layer
//     prefix `/assets/templates/external/media/` (and not the primary
//     `/assets/templates/media/`) is the end-to-end proof that
//     registryWithTemplate carried the external owning layer through the
//     provenance-preserving rebuild. assertGalleryRegistryWithTemplateTwoLayer
//     additionally pins that rebuild directly: the rebuilt registry's
//     LayerOf(wrapper) is the external-role layer carrying the external layer's
//     media filesystem, while the caller's registry is never mutated.
//   - the local layer's slide preview keeps the primary media base, so the two
//     layers' previews do not cross prefixes.
func TestGalleryListsTemplatesFromBothLayers(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads a real two-layer library from disk")
	}

	deckRoot := t.TempDir()
	externalRoot := t.TempDir()
	writeGalleryLayeredLibrary(t, deckRoot, externalRoot)

	lib, err := template.LoadDeckLibrary(deckRoot, externalRoot)
	if err != nil {
		t.Fatalf("template.LoadDeckLibrary: %v", err)
	}
	if len(lib.Layers) != 2 {
		t.Fatalf("merged library has %d layers, want 2 (external + local)", len(lib.Layers))
	}
	if lib.Layers[0].Role != template.LayerRoleExternal || lib.Layers[1].Role != template.LayerRoleLocal {
		t.Fatalf("layer roles = [%v, %v], want [external, local]", lib.Layers[0].Role, lib.Layers[1].Role)
	}

	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	// The merged registry carries each template's owning-layer provenance: the
	// section is the external layer's and the slide is the local layer's.
	if layer := reg.LayerOf(galleryLayeredExternalSectionName); layer == nil || layer.Role != template.LayerRoleExternal {
		t.Fatalf("reg.LayerOf(%q) = %v, want the external layer", galleryLayeredExternalSectionName, layer)
	}
	if layer := reg.LayerOf(galleryLayeredLocalSlideName); layer == nil || layer.Role != template.LayerRoleLocal {
		t.Fatalf("reg.LayerOf(%q) = %v, want the local layer", galleryLayeredLocalSlideName, layer)
	}

	funcMap := template.LayoutFuncMap(lib.Media, MediaPath)
	rec := httptest.NewRecorder()
	galleryHandler(reg, funcMap).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, galleryPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status %d, want 200 (body %q)", galleryPath, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if strings.Contains(body, "Example unavailable") {
		t.Fatalf("gallery reports an unavailable example; both layers' previews must render:\n%s", body)
	}

	// The gallery lists templates from BOTH layers: the external section and
	// the local slide, each by name and by its manifest description.
	for _, want := range []string{
		galleryLayeredExternalSectionName,
		galleryLayeredExternalSectionDescription,
		galleryLayeredLocalSlideName,
		galleryLayeredLocalSlideDescription,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("gallery body does not document %q from the merged library:\n%s", want, body)
		}
	}

	// The external section's preview resolved its media in the EXTERNAL layer
	// (registryWithTemplate placed the wrapper in the section's owning layer):
	// the external prefix, not the primary one.
	externalWant := `<img src="` + ExternalMediaPath + galleryLayeredExternalMediaFile + `">`
	if !strings.Contains(body, externalWant) {
		t.Errorf("external section's gallery preview did not resolve its media under the external prefix %q:\n%s", externalWant, body)
	}
	if bad := `<img src="` + MediaPath + galleryLayeredExternalMediaFile + `">`; strings.Contains(body, bad) {
		t.Errorf("external section's media was resolved under the primary prefix %q:\n%s", bad, body)
	}

	// The local slide's preview resolved its media in the LOCAL layer: the
	// primary prefix.
	localWant := `<img src="` + MediaPath + galleryLayeredLocalMediaFile + `">`
	if !strings.Contains(body, localWant) {
		t.Errorf("local slide's gallery preview did not resolve its media under the primary prefix %q:\n%s", localWant, body)
	}
	if bad := `<img src="` + ExternalMediaPath + galleryLayeredLocalMediaFile + `">`; strings.Contains(body, bad) {
		t.Errorf("local slide's media was resolved under the external prefix %q:\n%s", bad, body)
	}

	assertGalleryRegistryWithTemplateTwoLayer(t, reg)
}

// assertGalleryRegistryWithTemplateTwoLayer pins the mechanism the external
// section's gallery preview depends on (registryWithTemplate): over a two-layer
// registry, rebuilding with a synthetic wrapper owned by the external layer
// yields a registry whose LayerOf(wrapper) is that external-role layer carrying
// the external layer's media filesystem, and the caller's registry is left
// unmutated. It builds the same wrapper gallerySectionExample builds
// (galleryWrapperTemplate) for the external section.
func assertGalleryRegistryWithTemplateTwoLayer(t *testing.T, reg *template.Registry) {
	t.Helper()

	const wrapperName = "__gallery_two_layer_probe"
	wrapper, wrapperLT := galleryWrapperTemplate(wrapperName, galleryLayeredExternalSectionName)

	rebuilt, err := registryWithTemplate(reg, wrapper, wrapperLT, reg.LayerOf(galleryLayeredExternalSectionName))
	if err != nil {
		t.Fatalf("registryWithTemplate: %v", err)
	}
	if _, ok := reg.Lookup(wrapperName); ok {
		t.Fatalf("registryWithTemplate mutated the caller's registry: it now holds %q", wrapperName)
	}

	got := rebuilt.LayerOf(wrapperName)
	if got == nil {
		t.Fatalf("rebuilt registry LayerOf(%q) = nil, want the external layer", wrapperName)
	}
	if got.Role != template.LayerRoleExternal {
		t.Fatalf("rebuilt wrapper layer Role = %v, want %v", got.Role, template.LayerRoleExternal)
	}
	if got.Media == nil {
		t.Fatal("rebuilt wrapper layer carries no media filesystem")
	}
	if _, err := fs.Stat(got.Media, galleryLayeredExternalMediaFile); err != nil {
		t.Errorf("rebuilt wrapper layer's media does not resolve the external layer's %s: %v", galleryLayeredExternalMediaFile, err)
	}
}

// writeGalleryLayeredLibrary writes the two-layer gallery fixture to deckRoot
// (its deck-root templates/ local layer) and externalRoot (the external
// `templates:` layer). The local layer holds one slide template (localslide)
// whose layout resolves its own media/local-logo.svg; the external layer holds
// one section template (extsection) whose layout resolves its own
// media/ext-logo.svg. The two layers share no slide/section/theme/media name,
// so the merged library passes library-layer-uniqueness, and each media file
// exists only in its own layer, so a preview's media URL identifies the layer
// that resolved it.
func writeGalleryLayeredLibrary(t *testing.T, deckRoot, externalRoot string) {
	t.Helper()

	localRoot := filepath.Join(deckRoot, template.TemplatesDir)
	writeFile(t, filepath.Join(localRoot, template.LibraryFile), "name: local-lib\nformat: 1\n")
	writeFile(t, filepath.Join(localRoot, template.SlidesDir, galleryLayeredLocalSlideName, template.ManifestFile),
		"description: "+galleryLayeredLocalSlideDescription+"\n"+
			"fields:\n"+
			"  - name: title\n"+
			"    type: text\n"+
			"body:\n"+
			"  mode: optional\n")
	writeFile(t, filepath.Join(localRoot, template.SlidesDir, galleryLayeredLocalSlideName, template.LayoutFile),
		`<section class="local-slide"><h1>{{.title}}</h1><img src="{{ media "`+galleryLayeredLocalMediaFile+`" }}"></section>`+"\n")
	writeFile(t, filepath.Join(localRoot, template.SlidesDir, galleryLayeredLocalSlideName, template.ExampleFile),
		"---\ntemplate: "+galleryLayeredLocalSlideName+"\ntitle: Local\n---\n")
	writeFile(t, filepath.Join(localRoot, template.MediaDir, galleryLayeredLocalMediaFile), "<svg>local</svg>\n")

	writeFile(t, filepath.Join(externalRoot, template.LibraryFile), "name: ext-lib\nformat: 1\n")
	writeFile(t, filepath.Join(externalRoot, template.SectionsDir, galleryLayeredExternalSectionName, template.ManifestFile),
		"description: "+galleryLayeredExternalSectionDescription+"\n"+
			"fields:\n"+
			"  - name: title\n"+
			"    type: text\n"+
			"body:\n"+
			"  mode: optional\n")
	writeFile(t, filepath.Join(externalRoot, template.SectionsDir, galleryLayeredExternalSectionName, template.LayoutFile),
		`<div class="ext-section"><h2>{{.title}}</h2><img src="{{ media "`+galleryLayeredExternalMediaFile+`" }}"></div>`+"\n")
	writeFile(t, filepath.Join(externalRoot, template.SectionsDir, galleryLayeredExternalSectionName, template.ExampleFile),
		"```\ntitle: External section\n```\n")
	writeFile(t, filepath.Join(externalRoot, template.MediaDir, galleryLayeredExternalMediaFile), "<svg>ext</svg>\n")
}
