package template

import (
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-01.task-8: the template
// loader's single library-root resolution point, ResolveLibraryRoot, and its
// deck-level entry point LoadDeckLibrary (resolve.go).
//
// ResolveLibraryRoot resolves against the real operating-system filesystem (an
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

// TestResolveLibraryRoot covers ResolveLibraryRoot's path semantics: a
// configured `templates:` path wins and is authoritative (relative resolved
// against the deck root, absolute used as given), an absent key falls back to
// the local templates/ dir, and neither resolving is a *LibraryError naming
// the offending or expected path with no silent fallback.
func TestResolveLibraryRoot(t *testing.T) {
	deckRoot := t.TempDir()
	// A valid local library, so every configured-path case also proves the
	// local templates/ is ignored rather than used as a fallback.
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

	t.Run("configured path wins over an existing local templates dir", func(t *testing.T) {
		got, err := ResolveLibraryRoot(deckRoot, external)
		if err != nil {
			t.Fatalf("ResolveLibraryRoot: %v", err)
		}
		if got == filepath.Join(deckRoot, TemplatesDir) {
			t.Errorf("ResolveLibraryRoot() = %q, want the configured %q, not the local templates/", got, external)
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

// TestLoadDeckLibrary covers LoadDeckLibrary: it validates the library at the
// root ResolveLibraryRoot chose — the configured `templates:` path when set,
// else the local templates/ — and does not fall back when a configured path is
// unusable.
func TestLoadDeckLibrary(t *testing.T) {
	deckRoot := t.TempDir()
	writeLibraryRoot(t, filepath.Join(deckRoot, TemplatesDir), "local")

	t.Run("configured external root is authoritative", func(t *testing.T) {
		external := t.TempDir()
		writeLibraryRoot(t, external, "external")

		lib, err := LoadDeckLibrary(deckRoot, external)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}
		if lib.Meta.Name != "external" {
			t.Errorf("Meta.Name = %q, want the configured library %q", lib.Meta.Name, "external")
		}
		if lib.RootPath != external {
			t.Errorf("RootPath = %q, want the resolved %q", lib.RootPath, external)
		}
	})

	t.Run("relative configured root resolves against the deck root", func(t *testing.T) {
		rel := filepath.Join(deckRoot, "shared-lib")
		writeLibraryRoot(t, rel, "shared")

		lib, err := LoadDeckLibrary(deckRoot, "shared-lib")
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
		if want := path.Join(bad, LibraryFile); libErr.Path != want {
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
		external := t.TempDir()
		writeLibraryRoot(t, external, "external")

		lib, err := LoadDeckLibrary(deckRoot, external)
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
		if want := path.Join(external, SlidesDir, "hello", LayoutFile); tmpl.LayoutPath != want {
			t.Errorf("LayoutPath = %q, want the resolved %q", tmpl.LayoutPath, want)
		}
	})
}
