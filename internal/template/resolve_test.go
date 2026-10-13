package template

import (
	"path/filepath"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for plan.phase-01.task-11: the
// template loader's library-root resolution and layered load (resolve.go and
// loadlibrary.go) over real temp directories.
//
// ResolveLibraryRoot resolves the deck's PRIMARY library root — the external
// `templates:` root when configured, else the deck-root templates/ — and keeps
// its exact single-root signature and semantics; it is not authoritative over
// the local layer (the local templates/ participates through
// ResolveLibraryLayers). ResolveLibraryLayers returns the deck's ORDERED layer
// roots — the external `templates:` root first when configured, then the
// deck-root templates/ whenever it exists. LoadDeckLibrary loads,
// uniqueness-checks and merges each resolved layer into one Library that
// retains each layer's own root. LoadLibraryAt validates one on-disk library
// directory as its own single root.
//
// The roots are resolved against the real operating-system filesystem (an
// external library may sit above the deck root, which the deck fs.FS rejects),
// so these tests build small real temp trees with t.TempDir(). The trees are
// tiny, so the tests still run under -short.
//
// It reuses writeFile (library_int_test.go), asLibraryError (library_test.go)
// and containsHint (library_int_test.go).

// writeLibraryRoot writes a minimal valid template library directly at dir:
// library.yaml (with the given name) plus one slide template, hello. The
// loader treats a library with no slides/sections/themes/media entries as
// valid, so hello is only here so a loaded library can be inspected.
func writeLibraryRoot(t *testing.T, dir, name string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, LibraryFile), []byte("name: "+name+"\nformat: 1\n"))
	writeFile(t, filepath.Join(dir, SlidesDir, "hello", ManifestFile),
		[]byte("name: hello\ndescription: a hello template\n"))
	writeFile(t, filepath.Join(dir, SlidesDir, "hello", LayoutFile),
		[]byte("<section><h1>{{.Title}}</h1></section>"))
	writeFile(t, filepath.Join(dir, SlidesDir, "hello", ExampleFile), []byte("# hello\n"))
}

// writeLibraryWithTemplates writes a minimal valid library directly at dir:
// library.yaml with the given name plus one slide per slideNames and one
// section per sectionNames. Every template gets its three required files with
// a plain, media-free layout (writeLibraryTemplate). A library with no
// templates at all is valid, so both slices may be nil/empty to write just the
// metadata file. Callers needing a custom layout add it with
// writeLibraryTemplate after calling this.
func writeLibraryWithTemplates(t *testing.T, dir, name string, slideNames, sectionNames []string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, LibraryFile), []byte("name: "+name+"\nformat: 1\n"))
	for _, n := range slideNames {
		writeLibraryTemplate(t, dir, SlidesDir, n, "<section><h1>{{.Title}}</h1></section>")
	}
	for _, n := range sectionNames {
		writeLibraryTemplate(t, dir, SectionsDir, n, "<section><h1>{{.Title}}</h1></section>")
	}
}

// writeLibraryTemplate writes one template directory dir/kindDir/name with a
// schema-free manifest, the given layout source and a placeholder example. The
// template declares no fields and no sections, so checkLibraryExample skips its
// example validation (step 7 parses no example block) and only executes its
// layout, which lets a test give a template an arbitrary layout — including a
// `{{ section … }}` helper call — without authoring a matching example.
func writeLibraryTemplate(t *testing.T, dir, kindDir, name, layout string) {
	t.Helper()
	base := filepath.Join(dir, kindDir, name)
	writeFile(t, filepath.Join(base, ManifestFile), []byte("name: "+name+"\n"))
	writeFile(t, filepath.Join(base, LayoutFile), []byte(layout))
	writeFile(t, filepath.Join(base, ExampleFile), []byte("# "+name+"\n"))
}

// TestResolveLibraryRoot covers ResolveLibraryRoot's unchanged single-root,
// primary-root semantics: the configured `templates:` path is the PRIMARY root
// (relative resolved against the deck root, absolute used as given) and is
// returned even when a local templates/ also exists — ResolveLibraryRoot does
// not resolve layers; the local templates/ participates only as the local
// layer through ResolveLibraryLayers. With no configured key the deck-root
// templates/ directory is the primary root, and neither resolving is a
// *LibraryError naming the offending or expected path with no silent fallback.
func TestResolveLibraryRoot(t *testing.T) {
	deckRoot := t.TempDir()
	// A valid local library, so every configured-path case also proves
	// ResolveLibraryRoot returns the primary configured root rather than the
	// local templates/ (which the layered resolver would add as a layer).
	writeLibraryRoot(t, filepath.Join(deckRoot, TemplatesDir), "local")

	relative := filepath.Join(deckRoot, "shared-lib")
	writeLibraryRoot(t, relative, "shared")

	external := t.TempDir()
	writeLibraryRoot(t, external, "external")

	t.Run("relative configured path resolves against the deck root", func(t *testing.T) {
		got, err := ResolveLibraryRoot(deckRoot, "shared-lib")
		if err != nil {
			t.Fatalf("ResolveLibraryRoot: %v", err)
		}
		if got != relative {
			t.Errorf("ResolveLibraryRoot() = %q, want %q", got, relative)
		}
	})

	t.Run("relative configured path resolves against the deck root even with a slash", func(t *testing.T) {
		got, err := ResolveLibraryRoot(deckRoot, "shared-lib/")
		if err != nil {
			t.Fatalf("ResolveLibraryRoot: %v", err)
		}
		if got != relative {
			t.Errorf("ResolveLibraryRoot() = %q, want the cleaned %q", got, relative)
		}
	})

	t.Run("absolute configured path is used as given", func(t *testing.T) {
		got, err := ResolveLibraryRoot(deckRoot, external)
		if err != nil {
			t.Fatalf("ResolveLibraryRoot: %v", err)
		}
		if got != external {
			t.Errorf("ResolveLibraryRoot() = %q, want %q", got, external)
		}
	})

	t.Run("configured path is the primary root when a local templates dir also exists", func(t *testing.T) {
		got, err := ResolveLibraryRoot(deckRoot, external)
		if err != nil {
			t.Fatalf("ResolveLibraryRoot: %v", err)
		}
		if got == filepath.Join(deckRoot, TemplatesDir) {
			t.Errorf("ResolveLibraryRoot() = %q, want the configured primary %q; the local templates/ is the local layer, not the primary root", got, external)
		}
	})

	t.Run("absent key falls back to the local templates dir", func(t *testing.T) {
		got, err := ResolveLibraryRoot(deckRoot, "")
		if err != nil {
			t.Fatalf("ResolveLibraryRoot: %v", err)
		}
		if want := filepath.Join(deckRoot, TemplatesDir); got != want {
			t.Errorf("ResolveLibraryRoot() = %q, want the local %q", got, want)
		}
	})

	t.Run("missing configured path errors naming it, with no fallback", func(t *testing.T) {
		// The local templates/ exists and is valid, so this also proves a
		// configured path is never silently replaced by it.
		want := filepath.Join(deckRoot, "nope")
		got, err := ResolveLibraryRoot(deckRoot, "nope")
		if got != "" {
			t.Errorf("ResolveLibraryRoot() = %q, want empty on error", got)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != want {
			t.Errorf("Path = %q, want the offending configured path %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, "directory not found") {
			t.Errorf("Message = %q, want it to say the directory was not found", libErr.Message)
		}
		if libErr.Hint == "" {
			t.Error("Hint is empty, want a remediation hint naming the `templates:` path")
		}
	})

	t.Run("configured path that is a file errors naming it", func(t *testing.T) {
		file := filepath.Join(deckRoot, "not-a-dir")
		writeFile(t, file, []byte("x"))
		got, err := ResolveLibraryRoot(deckRoot, "not-a-dir")
		if got != "" {
			t.Errorf("ResolveLibraryRoot() = %q, want empty on error", got)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != file {
			t.Errorf("Path = %q, want %q", libErr.Path, file)
		}
		if !strings.Contains(libErr.Message, "not a directory") {
			t.Errorf("Message = %q, want it to say the path is not a directory", libErr.Message)
		}
	})

	t.Run("absent local templates dir errors naming the expected path with the init hint", func(t *testing.T) {
		bare := t.TempDir()
		want := filepath.Join(bare, TemplatesDir)
		got, err := ResolveLibraryRoot(bare, "")
		if got != "" {
			t.Errorf("ResolveLibraryRoot() = %q, want empty on error", got)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != want {
			t.Errorf("Path = %q, want the expected %q", libErr.Path, want)
		}
		if !containsHint(libErr.Hint) {
			t.Errorf("Hint = %q, want the `kalide init` remediation", libErr.Hint)
		}
	})

	t.Run("local templates dir that is a file errors", func(t *testing.T) {
		bare := t.TempDir()
		file := filepath.Join(bare, TemplatesDir)
		writeFile(t, file, []byte("x"))
		_, err := ResolveLibraryRoot(bare, "")
		libErr := asLibraryError(t, err)
		if libErr.Path != file {
			t.Errorf("Path = %q, want the expected %q", libErr.Path, file)
		}
		if !strings.Contains(libErr.Message, "not a directory") {
			t.Errorf("Message = %q, want it to say the path is not a directory", libErr.Message)
		}
	})
}

// TestResolveLibraryLayers covers ResolveLibraryLayers' ordered-layer
// resolution over real temp dirs: the external `templates:` root first when
// configured, the deck-root templates/ whenever it exists, both when both
// resolve, and the path-qualified errors when a configured path is missing or
// not a directory, when a local templates/ exists but is not a directory, and
// the init-hint error naming the expected local templates/ when neither layer
// resolves (external-template-library, templates-dir-required).
func TestResolveLibraryLayers(t *testing.T) {
	t.Run("external library only", func(t *testing.T) {
		deckRoot := t.TempDir() // no local templates/
		external := t.TempDir()
		writeLibraryRoot(t, external, "external")

		got, err := ResolveLibraryLayers(deckRoot, external)
		if err != nil {
			t.Fatalf("ResolveLibraryLayers: %v", err)
		}
		if len(got) != 1 || got[0] != external {
			t.Errorf("ResolveLibraryLayers() = %v, want the external root alone [%q]", got, external)
		}
	})

	t.Run("local library only", func(t *testing.T) {
		deckRoot := t.TempDir()
		local := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryRoot(t, local, "local")

		got, err := ResolveLibraryLayers(deckRoot, "")
		if err != nil {
			t.Fatalf("ResolveLibraryLayers: %v", err)
		}
		if len(got) != 1 || got[0] != local {
			t.Errorf("ResolveLibraryLayers() = %v, want the local root alone [%q]", got, local)
		}
	})

	t.Run("both layers in external-then-local order", func(t *testing.T) {
		deckRoot := t.TempDir()
		local := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryRoot(t, local, "local")
		// A relative configured path resolves against the deck root.
		shared := filepath.Join(deckRoot, "shared-lib")
		writeLibraryRoot(t, shared, "shared")

		got, err := ResolveLibraryLayers(deckRoot, "shared-lib")
		if err != nil {
			t.Fatalf("ResolveLibraryLayers: %v", err)
		}
		if len(got) != 2 || got[0] != shared || got[1] != local {
			t.Errorf("ResolveLibraryLayers() = %v, want [%q %q] (external first, local second)", got, shared, local)
		}
	})

	t.Run("missing configured path errors naming it", func(t *testing.T) {
		deckRoot := t.TempDir()
		writeLibraryRoot(t, filepath.Join(deckRoot, TemplatesDir), "local")
		want := filepath.Join(deckRoot, "nope")

		got, err := ResolveLibraryLayers(deckRoot, "nope")
		if len(got) != 0 {
			t.Errorf("ResolveLibraryLayers() = %v, want no layers on error", got)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != want {
			t.Errorf("Path = %q, want the offending configured path %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, "directory not found") {
			t.Errorf("Message = %q, want it to say the directory was not found", libErr.Message)
		}
		if libErr.Hint == "" {
			t.Error("Hint is empty, want a hint naming the `templates:` path")
		}
	})

	t.Run("configured path that is a file errors naming it", func(t *testing.T) {
		deckRoot := t.TempDir()
		file := filepath.Join(deckRoot, "not-a-dir")
		writeFile(t, file, []byte("x"))

		got, err := ResolveLibraryLayers(deckRoot, "not-a-dir")
		if len(got) != 0 {
			t.Errorf("ResolveLibraryLayers() = %v, want no layers on error", got)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != file {
			t.Errorf("Path = %q, want %q", libErr.Path, file)
		}
		if !strings.Contains(libErr.Message, "not a directory") {
			t.Errorf("Message = %q, want it to say the path is not a directory", libErr.Message)
		}
	})

	t.Run("neither layer errors naming the expected local path with the init hint", func(t *testing.T) {
		deckRoot := t.TempDir()
		want := filepath.Join(deckRoot, TemplatesDir)

		got, err := ResolveLibraryLayers(deckRoot, "")
		if len(got) != 0 {
			t.Errorf("ResolveLibraryLayers() = %v, want no layers on error", got)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != want {
			t.Errorf("Path = %q, want the expected local path %q", libErr.Path, want)
		}
		if !containsHint(libErr.Hint) {
			t.Errorf("Hint = %q, want the `kalide init` remediation", libErr.Hint)
		}
	})

	t.Run("local templates that is a file errors", func(t *testing.T) {
		deckRoot := t.TempDir()
		file := filepath.Join(deckRoot, TemplatesDir)
		writeFile(t, file, []byte("x"))

		got, err := ResolveLibraryLayers(deckRoot, "")
		if len(got) != 0 {
			t.Errorf("ResolveLibraryLayers() = %v, want no layers on error", got)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != file {
			t.Errorf("Path = %q, want the expected %q", libErr.Path, file)
		}
		if !strings.Contains(libErr.Message, "not a directory") {
			t.Errorf("Message = %q, want it to say the path is not a directory", libErr.Message)
		}
	})
}

// TestLoadDeckLibrary covers LoadDeckLibrary's basic resolution and
// path-qualified error behavior: it resolves the deck's layer roots through
// ResolveLibraryLayers, loads the external `templates:` root as the primary
// layer when configured (else the local templates/ as the sole layer), and
// does not fall back when a configured path is unusable. The merged-load cases
// (two layers, uniqueness, dedupe, cross-layer section calls) are
// TestLoadDeckLibraryMergedLayers.
func TestLoadDeckLibrary(t *testing.T) {
	deckRoot := t.TempDir()
	writeLibraryRoot(t, filepath.Join(deckRoot, TemplatesDir), "local")

	t.Run("external library as the sole layer", func(t *testing.T) {
		// A deck root with no local templates/ resolves the configured
		// external root as the deck's only layer.
		bare := t.TempDir()
		external := t.TempDir()
		writeLibraryRoot(t, external, "external")

		lib, err := LoadDeckLibrary(bare, external)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if lib.Meta.Name != "external" {
			t.Errorf("Meta.Name = %q, want the configured library %q", lib.Meta.Name, "external")
		}
		if lib.RootPath != external {
			t.Errorf("RootPath = %q, want the resolved %q", lib.RootPath, external)
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1 (the external root is the only layer)", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != external {
			t.Errorf("Layers[0].RootPath = %q, want %q", lib.Layers[0].RootPath, external)
		}
	})

	t.Run("relative configured root resolves against the deck root", func(t *testing.T) {
		bare := t.TempDir()
		rel := filepath.Join(bare, "shared-lib")
		writeLibraryRoot(t, rel, "shared")

		lib, err := LoadDeckLibrary(bare, "shared-lib")
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if lib.Meta.Name != "shared" {
			t.Errorf("Meta.Name = %q, want %q", lib.Meta.Name, "shared")
		}
		if lib.RootPath != rel {
			t.Errorf("RootPath = %q, want %q", lib.RootPath, rel)
		}
		if _, _, ok := lib.TemplateByName("hello"); !ok {
			t.Error("TemplateByName(hello) not found: the relative root's template was not loaded")
		}
	})

	t.Run("absent key loads the local templates dir", func(t *testing.T) {
		lib, err := LoadDeckLibrary(deckRoot, "")
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if lib.Meta.Name != "local" {
			t.Errorf("Meta.Name = %q, want the local library %q", lib.Meta.Name, "local")
		}
		if want := filepath.Join(deckRoot, TemplatesDir); lib.RootPath != want {
			t.Errorf("RootPath = %q, want %q", lib.RootPath, want)
		}
	})

	t.Run("missing configured root is not replaced by the local templates dir", func(t *testing.T) {
		lib, err := LoadDeckLibrary(deckRoot, "nope")
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		if want := filepath.Join(deckRoot, "nope"); libErr.Path != want {
			t.Errorf("Path = %q, want the offending configured path %q", libErr.Path, want)
		}
	})

	t.Run("validation runs at the resolved root", func(t *testing.T) {
		// An external root that is not a valid library: its library.yaml is
		// invalid. The error must be qualified with the external root, never
		// the deck's valid local templates/.
		bad := t.TempDir()
		writeFile(t, filepath.Join(bad, LibraryFile), []byte("name: Bad_Name\nformat: 1\n"))

		lib, err := LoadDeckLibrary(deckRoot, bad)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		if want := filepath.Join(bad, LibraryFile); libErr.Path != want {
			t.Errorf("Path = %q, want the resolved root's %q", libErr.Path, want)
		}
		if libErr.Line != 1 {
			t.Errorf("Line = %d, want 1 (the name: line in the external library.yaml)", libErr.Line)
		}
		if !strings.Contains(libErr.Message, "not a valid name") {
			t.Errorf("Message = %q, want the external library.yaml's name error", libErr.Message)
		}
	})

	t.Run("valid external library exposes the resolved root's template paths", func(t *testing.T) {
		bare := t.TempDir()
		external := t.TempDir()
		writeLibraryRoot(t, external, "external")

		lib, err := LoadDeckLibrary(bare, external)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		tmpl, kind, ok := lib.TemplateByName("hello")
		if !ok {
			t.Fatal("TemplateByName(hello) not found")
		}
		if kind != KindSlide {
			t.Errorf("Kind = %q, want %q", kind, KindSlide)
		}
		if want := filepath.Join(external, SlidesDir, "hello", LayoutFile); tmpl.LayoutPath != want {
			t.Errorf("LayoutPath = %q, want the resolved %q", tmpl.LayoutPath, want)
		}
	})
}

// TestLoadDeckLibraryMergedLayers covers LoadDeckLibrary's merged load over
// real temp dirs (layered-template-library, library-layer-uniqueness,
// templates-dir-validation):
//
//   - two layers with disjoint names merge into one Library that retains each
//     layer's own root, ordered external then local, with the local deck-root
//     templates/ layer primary (its Meta and RootPath);
//   - a name carried by both layers is rejected naming the name and both
//     paths, including a slide in one layer colliding with a section in the
//     other (one shared template namespace);
//   - a configured `templates:` path naming the deck's own templates/ is one
//     layer, not two (dedupe);
//   - a local template calling an external section passes the merged build —
//     the local layer's per-layer cross-template checks are deferred to the
//     merged library — while a call to an unresolved section name still fails
//     path-qualified to the local template;
//   - each layer's named definitions (groups:/field_sets:/types:) are scoped to
//     that layer: an external-layer template resolves its own layer's
//     type:/include:/@group names, while a name defined only in the other
//     layer is a templates error naming the unresolvable reference, so
//     definitions do not leak across layers (named-library-definitions).
func TestLoadDeckLibraryMergedLayers(t *testing.T) {
	t.Run("external and local layers with disjoint names merge", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryWithTemplates(t, localRoot, "local",
			[]string{"local-slide"}, []string{"local-section"})
		external := t.TempDir()
		writeLibraryWithTemplates(t, external, "external",
			[]string{"ext-slide"}, []string{"ext-section"})

		lib, err := LoadDeckLibrary(deckRoot, external)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		for _, name := range []string{"ext-slide", "local-slide"} {
			if _, _, ok := lib.TemplateByName(name); !ok {
				t.Errorf("TemplateByName(%q) not found: the merged library must carry both layers", name)
			}
		}
		for _, name := range []string{"ext-section", "local-section"} {
			lt, kind, ok := lib.TemplateByName(name)
			if !ok || kind != KindSection {
				t.Errorf("TemplateByName(%q) = (%v, %q, %v), want a section from the merged library", name, lt, kind, ok)
			}
		}
		if lib.Meta.Name != "local" {
			t.Errorf("Meta.Name = %q, want the local (primary) layer's %q", lib.Meta.Name, "local")
		}
		if len(lib.Layers) != 2 {
			t.Fatalf("len(Layers) = %d, want 2", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != external {
			t.Errorf("Layers[0].RootPath = %q, want the external layer %q", lib.Layers[0].RootPath, external)
		}
		if lib.Layers[1].RootPath != localRoot {
			t.Errorf("Layers[1].RootPath = %q, want the local layer %q", lib.Layers[1].RootPath, localRoot)
		}
		if lib.RootPath != localRoot {
			t.Errorf("RootPath = %q, want the local (primary) layer %q", lib.RootPath, localRoot)
		}
	})

	t.Run("a slide name carried by both layers errors naming the name and both paths", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryWithTemplates(t, localRoot, "local", []string{"dup"}, nil)
		external := t.TempDir()
		writeLibraryWithTemplates(t, external, "external", []string{"dup"}, nil)

		lib, err := LoadDeckLibrary(deckRoot, external)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		extManifest := filepath.Join(external, SlidesDir, "dup", ManifestFile)
		localManifest := filepath.Join(localRoot, SlidesDir, "dup", ManifestFile)
		if libErr.Path != extManifest {
			t.Errorf("Path = %q, want the first layer's manifest %q", libErr.Path, extManifest)
		}
		if !strings.Contains(libErr.Message, `"dup"`) {
			t.Errorf("Message = %q, want it to name the duplicated template %q", libErr.Message, "dup")
		}
		if !strings.Contains(libErr.Message, extManifest) {
			t.Errorf("Message = %q, want it to name the external path %q", libErr.Message, extManifest)
		}
		if !strings.Contains(libErr.Message, localManifest) {
			t.Errorf("Message = %q, want it to name the local path %q", libErr.Message, localManifest)
		}
	})

	t.Run("a local slide named like an external section collides", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryWithTemplates(t, localRoot, "local", []string{"dup"}, nil) // slide dup
		external := t.TempDir()
		writeLibraryWithTemplates(t, external, "external", nil, []string{"dup"}) // section dup

		_, err := LoadDeckLibrary(deckRoot, external)
		libErr := asLibraryError(t, err)
		extManifest := filepath.Join(external, SectionsDir, "dup", ManifestFile)
		localManifest := filepath.Join(localRoot, SlidesDir, "dup", ManifestFile)
		if libErr.Path != extManifest {
			t.Errorf("Path = %q, want the first layer's manifest %q", libErr.Path, extManifest)
		}
		if !strings.Contains(libErr.Message, extManifest) || !strings.Contains(libErr.Message, localManifest) {
			t.Errorf("Message = %q, want it to name both cross-kind paths %q and %q", libErr.Message, extManifest, localManifest)
		}
	})

	t.Run("a configured path naming the local templates dir is one layer", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryWithTemplates(t, localRoot, "local", []string{"hello"}, nil)

		lib, err := LoadDeckLibrary(deckRoot, localRoot)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1 (the configured path is the deck's own templates/ dir)", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != localRoot {
			t.Errorf("Layers[0].RootPath = %q, want %q", lib.Layers[0].RootPath, localRoot)
		}
		if _, _, ok := lib.TemplateByName("hello"); !ok {
			t.Error("TemplateByName(hello) not found")
		}
	})

	t.Run("a relative configured path naming the local templates dir is deduped too", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryWithTemplates(t, localRoot, "local", []string{"hello"}, nil)

		lib, err := LoadDeckLibrary(deckRoot, TemplatesDir) // relative "templates"
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1 (the relative configured path resolves to the local templates/)", len(lib.Layers))
		}
	})

	t.Run("a local template calling an external section passes the merged build", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryWithTemplates(t, localRoot, "local", nil, nil)
		writeLibraryTemplate(t, localRoot, SlidesDir, "local-slide", `<section>{{ section "ext-section" }}</section>`)
		external := t.TempDir()
		writeLibraryWithTemplates(t, external, "external", nil, []string{"ext-section"})

		if _, err := LoadDeckLibrary(deckRoot, external); err != nil {
			t.Fatalf("LoadDeckLibrary: %v (a local template may call an external section once the layers are merged)", err)
		}
	})

	t.Run("a local template calling an unknown section still fails", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryWithTemplates(t, localRoot, "local", nil, nil)
		writeLibraryTemplate(t, localRoot, SlidesDir, "local-slide", `<section>{{ section "nope" }}</section>`)
		external := t.TempDir()
		writeLibraryWithTemplates(t, external, "external", nil, []string{"ext-section"})

		_, err := LoadDeckLibrary(deckRoot, external)
		libErr := asLibraryError(t, err)
		wantPath := filepath.Join(localRoot, SlidesDir, "local-slide", ManifestFile)
		if libErr.Path != wantPath {
			t.Errorf("Path = %q, want the local template's manifest %q", libErr.Path, wantPath)
		}
		if !strings.Contains(libErr.Message, "nope") {
			t.Errorf("Message = %q, want it to name the unresolved section %q", libErr.Message, "nope")
		}
	})

	// named-library-definitions (plan.phase-04.task-20): each layer's
	// library.yaml may declare its own named definitions (groups:,
	// field_sets: and types:). A template resolves its `type:`, `include:`
	// and `@group` references against its OWN owning layer's definitions —
	// here the external layer's — and a name defined only in the other layer
	// is an unresolvable reference (a templates error naming it), so
	// definitions never leak across layers.
	t.Run("an external-layer template resolves its own layer's named definitions", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLibraryWithTemplates(t, localRoot, "local", nil, nil)

		external := t.TempDir()
		writeFile(t, filepath.Join(external, LibraryFile), []byte(`name: external
format: 1
groups:
  callouts:
    - ext-note
field_sets:
  meta:
    - name: subtitle
      type: text
    - name: weight
      type: number
types:
  colour:
    type: enum
    variants:
      - red
      - green
      - blue
`))
		extFiles := mergeIntFiles(
			templateIntFiles(SlidesDir, "ext-slide",
				"name: ext-slide\n"+
					"fields:\n"+
					"  - include: meta\n"+
					"  - name: accent\n"+
					"    type: colour\n"+
					"sections:\n"+
					"  - name: asides\n"+
					"    accepted:\n"+
					"      - \"@callouts\"\n",
				`<section class="ext"><span class="ext-subtitle">{{ .subtitle }}</span><span class="ext-accent">{{ .accent }}</span></section>`,
				"---\ntemplate: ext-slide\nsubtitle: Quarterly\nweight: 3\naccent: red\n---\nBody.\n"),
			templateIntFiles(SectionsDir, "ext-note",
				"name: ext-note\nfields:\n  - name: text\n    type: text\n",
				`<aside class="note">{{ .text }}</aside>`,
				"A note.\n"),
		)
		for rel, data := range extFiles {
			writeFile(t, filepath.Join(external, filepath.FromSlash(rel)), []byte(data))
		}

		lib, err := LoadDeckLibrary(deckRoot, external)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v (the external template's own definitions must resolve)", err)
		}
		lt, kind, ok := lib.TemplateByName("ext-slide")
		if !ok || kind != KindSlide {
			t.Fatalf("TemplateByName(ext-slide) = (%v, %q, %v), want (non-nil, %q, true)", lt, kind, ok, KindSlide)
		}
		if lt.Definition == nil {
			t.Fatal("ext-slide.Definition = nil, want the parsed definition")
		}
		def := lt.Definition

		// `- include: meta` expanded the external layer's field set in place.
		if len(def.Fields) != 3 ||
			def.Fields[0].Name != "subtitle" || def.Fields[0].Type != FieldText ||
			def.Fields[1].Name != "weight" || def.Fields[1].Type != FieldNumber {
			t.Fatalf("ext-slide.Fields = %+v, want subtitle:text, weight:number (from include: meta) then accent", def.Fields)
		}
		// `type: colour` expanded the external layer's named enum under the
		// referencing name, keeping the named variants.
		if def.Fields[2].Name != "accent" || def.Fields[2].Type != FieldEnum {
			t.Errorf("ext-slide.Fields[2] = %+v, want the named type colour expanded as accent:enum", def.Fields[2])
		}
		assertStringSlice(t, "accent.Variants", def.Fields[2].Variants, []string{"red", "green", "blue"})
		// `@callouts` expanded the external layer's named group in place.
		if len(def.Sections) != 1 {
			t.Fatalf("ext-slide.Sections = %+v, want one asides section", def.Sections)
		}
		assertStringSlice(t, "asides.Accepted", def.Sections[0].Accepted, []string{"ext-note"})

		// The definitions rode the EXTERNAL layer (Layers[0]), not the primary
		// local one, so the external template resolved them in its own layer.
		if len(lib.Layers) != 2 {
			t.Fatalf("len(Layers) = %d, want 2", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != external {
			t.Errorf("Layers[0].RootPath = %q, want the external layer %q", lib.Layers[0].RootPath, external)
		}
		if lib.Layers[0].Definitions == nil {
			t.Error("Layers[0] (external).Definitions = nil, want the external layer's parsed definitions")
		}
	})

	t.Run("a definition from the other layer is an unresolvable reference", func(t *testing.T) {
		// The LOCAL layer declares the definition; an EXTERNAL template
		// references it. Because a template resolves only in its own layer,
		// the reference is unknown at the external layer's own load (it loads
		// fully, before the merge) and the load fails naming it — a merged
		// definitions set would instead resolve it.
		cases := []struct {
			name     string
			localDef string
			manifest string
			wantRef  string
		}{
			{
				name:     "named type",
				localDef: "types:\n  localshade:\n    type: text\n",
				manifest: "name: ext-slide\nfields:\n  - name: accent\n    type: localshade\n",
				wantRef:  `unknown type "localshade"`,
			},
			{
				name:     "field set",
				localDef: "field_sets:\n  localmeta:\n    - name: subtitle\n      type: text\n",
				manifest: "name: ext-slide\nfields:\n  - include: localmeta\n",
				wantRef:  `unknown field set "localmeta"`,
			},
			{
				name:     "group",
				localDef: "groups:\n  localcallouts:\n    - ext-note\n",
				manifest: "name: ext-slide\nsections:\n  - name: asides\n    accepted:\n      - \"@localcallouts\"\n",
				wantRef:  `unknown group "localcallouts"`,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				deckRoot := t.TempDir()
				localRoot := filepath.Join(deckRoot, TemplatesDir)
				writeFile(t, filepath.Join(localRoot, LibraryFile),
					[]byte("name: local\nformat: 1\n"+tc.localDef))
				external := t.TempDir()
				writeFile(t, filepath.Join(external, LibraryFile), []byte("name: external\nformat: 1\n"))
				writeLibraryTemplate(t, external, SlidesDir, "ext-slide", `<section class="ext"></section>`)
				writeFile(t, filepath.Join(external, SlidesDir, "ext-slide", ManifestFile), []byte(tc.manifest))

				lib, err := LoadDeckLibrary(deckRoot, external)
				if err == nil {
					t.Fatalf("LoadDeckLibrary() = %+v, nil; want the external template's reference to a name only the other layer defines to fail", lib)
				}
				if lib != nil {
					t.Fatalf("LoadDeckLibrary() Library = %+v, want nil with the error", lib)
				}
				libErr := asLibraryError(t, err)
				wantPath := filepath.Join(external, SlidesDir, "ext-slide", ManifestFile)
				if libErr.Path != wantPath {
					t.Errorf("Path = %q, want the external template's manifest %q", libErr.Path, wantPath)
				}
				if !strings.Contains(libErr.Message, tc.wantRef) {
					t.Errorf("Message = %q, want it to name the unresolvable reference %q", libErr.Message, tc.wantRef)
				}
			})
		}
	})
}

// TestLoadLibraryAt covers LoadLibraryAt's single-root contract over real temp
// dirs: it validates one on-disk library directory as its own root, so a
// missing or non-directory root is a *LibraryError carrying the `kalide init`
// hint and qualified with the absolute OS path, and a library directory that
// also carries a nested templates/ directory loads as that one root — the
// nested directory is an ordinary ignored top-level entry, never a second
// local layer (templates-dir-validation).
func TestLoadLibraryAt(t *testing.T) {
	t.Run("a valid library directory loads as its own single root", func(t *testing.T) {
		root := t.TempDir()
		writeLibraryRoot(t, root, "standalone")

		lib, err := LoadLibraryAt(root)
		if err != nil {
			t.Fatalf("LoadLibraryAt: %v", err)
		}
		if lib.Meta.Name != "standalone" {
			t.Errorf("Meta.Name = %q, want %q", lib.Meta.Name, "standalone")
		}
		if lib.RootPath != root {
			t.Errorf("RootPath = %q, want the absolute root %q", lib.RootPath, root)
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != root {
			t.Errorf("Layers[0].RootPath = %q, want %q", lib.Layers[0].RootPath, root)
		}
		if _, _, ok := lib.TemplateByName("hello"); !ok {
			t.Error("TemplateByName(hello) not found")
		}
	})

	t.Run("a missing root errors qualified with the absolute path and the init hint", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "nope")

		lib, err := LoadLibraryAt(missing)
		if lib != nil {
			t.Fatalf("LoadLibraryAt() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != missing {
			t.Errorf("Path = %q, want the absolute missing path %q", libErr.Path, missing)
		}
		if !filepath.IsAbs(libErr.Path) {
			t.Errorf("Path = %q, want an absolute path", libErr.Path)
		}
		if !containsHint(libErr.Hint) {
			t.Errorf("Hint = %q, want the `kalide init` remediation", libErr.Hint)
		}
	})

	t.Run("a root that is a file errors qualified with the absolute path", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-dir")
		writeFile(t, file, []byte("x"))

		lib, err := LoadLibraryAt(file)
		if lib != nil {
			t.Fatalf("LoadLibraryAt() Library = %+v, want nil on error", lib)
		}
		libErr := asLibraryError(t, err)
		if libErr.Path != file {
			t.Errorf("Path = %q, want the absolute file path %q", libErr.Path, file)
		}
		if !filepath.IsAbs(libErr.Path) {
			t.Errorf("Path = %q, want an absolute path", libErr.Path)
		}
		if !strings.Contains(libErr.Message, file) {
			t.Errorf("Message = %q, want it to name the absolute root %q", libErr.Message, file)
		}
	})

	t.Run("a nested templates directory is not merged as a second layer", func(t *testing.T) {
		root := t.TempDir()
		writeLibraryRoot(t, root, "standalone")
		// A nested templates/ subtree carrying the same slide name as the
		// root library: were it merged as a second layer, the duplicate
		// "hello" would collide. LoadLibraryAt must load the root alone.
		writeLibraryRoot(t, filepath.Join(root, TemplatesDir), "nested")

		lib, err := LoadLibraryAt(root)
		if err != nil {
			t.Fatalf("LoadLibraryAt: %v (the nested templates/ is an ordinary ignored top-level entry)", err)
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1 (the nested templates/ is not a second layer)", len(lib.Layers))
		}
		if lib.Layers[0].RootPath != root {
			t.Errorf("Layers[0].RootPath = %q, want the root %q", lib.Layers[0].RootPath, root)
		}
		if lib.Meta.Name != "standalone" {
			t.Errorf("Meta.Name = %q, want the root library's %q (not the nested one)", lib.Meta.Name, "standalone")
		}
	})
}
