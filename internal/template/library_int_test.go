package template

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// This file is the integration-test deliverable for plan.phase-01.task-6:
// LoadLibrary against a real filesystem (os.DirFS over a temp dir), covering
// the no-built-in-fallback behavior when a project has no usable templates/
// directory, and a full on-disk library — slides, sections, a theme with an
// owned font file, and media including a video — loading cleanly.
//
// These tests exercise real disk I/O and so are guarded with testing.Short();
// they are skipped under `go test -short`.

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// TestLoadLibraryIntMissingTemplatesDir proves LoadLibrary reports a
// *LibraryError naming the expected templates/ path with the `eypres init`
// hint when a project has no templates/ directory at all — no built-in
// fallback is substituted.
func TestLoadLibraryIntMissingTemplatesDir(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}
	projectDir := t.TempDir()
	// A project root with no templates/ entry whatsoever.
	writeFile(t, filepath.Join(projectDir, "eypres.yaml"), []byte("title: demo\n"))

	fsys := os.DirFS(projectDir)
	lib, err := LoadLibrary(fsys, TemplatesDir)
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil", lib)
	}
	libErr := asLibraryError(t, err)
	if libErr.Path != TemplatesDir {
		t.Errorf("Path = %q, want %q", libErr.Path, TemplatesDir)
	}
	if libErr.Hint == "" || !containsHint(libErr.Hint) {
		t.Errorf("Hint = %q, want it to mention `eypres init`", libErr.Hint)
	}
}

// TestLoadLibraryIntTemplatesIsRegularFile proves LoadLibrary reports a
// *LibraryError naming the expected templates/ path with the `eypres init`
// hint when templates/ exists but is a regular file, not a directory.
func TestLoadLibraryIntTemplatesIsRegularFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}
	projectDir := t.TempDir()
	writeFile(t, filepath.Join(projectDir, TemplatesDir), []byte("not a directory"))

	fsys := os.DirFS(projectDir)
	lib, err := LoadLibrary(fsys, TemplatesDir)
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil", lib)
	}
	libErr := asLibraryError(t, err)
	if libErr.Path != TemplatesDir {
		t.Errorf("Path = %q, want %q", libErr.Path, TemplatesDir)
	}
	if libErr.Hint == "" || !containsHint(libErr.Hint) {
		t.Errorf("Hint = %q, want it to mention `eypres init`", libErr.Hint)
	}
}

// containsHint reports whether hint mentions the `eypres init` remediation.
func containsHint(hint string) bool {
	return hint == "run `eypres init` to create one"
}

// TestLoadLibraryIntFullLibrary proves a complete on-disk library — slides,
// sections, a theme with an owned font file, and media including a video —
// loads cleanly via os.DirFS.
func TestLoadLibraryIntFullLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, TemplatesDir)

	writeFile(t, filepath.Join(root, LibraryFile), []byte("name: demo\nformat: 1\n"))

	// Slide template.
	writeFile(t, filepath.Join(root, "slides", "hello", "template.yaml"), []byte("name: hello\n"))
	writeFile(t, filepath.Join(root, "slides", "hello", "layout.html.tmpl"),
		[]byte(`<section><h1>{{.Title}}</h1><img src="{{media "logo.svg"}}"></section>`))
	writeFile(t, filepath.Join(root, "slides", "hello", "example.md"), []byte("# hello\n"))

	// Section template.
	writeFile(t, filepath.Join(root, "sections", "item", "template.yaml"), []byte("name: item\n"))
	writeFile(t, filepath.Join(root, "sections", "item", "layout.html.tmpl"), []byte("<li>{{.Title}}</li>"))
	writeFile(t, filepath.Join(root, "sections", "item", "example.md"), []byte("# item\n"))

	// Theme with an owned font file.
	writeFile(t, filepath.Join(root, "themes", "plain", "theme.css"), []byte("body { color: black; }"))
	writeFile(t, filepath.Join(root, "themes", "plain", "fonts", "body.woff2"), []byte("fontdata"))

	// Media including a video.
	writeFile(t, filepath.Join(root, "media", "logo.svg"), []byte("<svg/>"))
	writeFile(t, filepath.Join(root, "media", "video", "intro.mp4"), []byte("videodata"))

	fsys := os.DirFS(projectDir)
	lib, err := LoadLibrary(fsys, TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil", err)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}

	if _, _, ok := lib.TemplateByName("hello"); !ok {
		t.Error("slide template hello not loaded")
	}
	if _, _, ok := lib.TemplateByName("item"); !ok {
		t.Error("section template item not loaded")
	}
	th, ok := lib.Themes["plain"]
	if !ok {
		t.Fatal("theme plain not loaded")
	}
	if _, ok := th.Files["fonts/body.woff2"]; !ok {
		t.Errorf("theme plain Files = %v, want fonts/body.woff2 present", th.Files)
	}
	if !lib.HasMedia {
		t.Fatal("HasMedia = false, want true")
	}
	if _, err := fs.Stat(lib.Media, "video/intro.mp4"); err != nil {
		t.Errorf("media video/intro.mp4 not present in Library.Media: %v", err)
	}
}
