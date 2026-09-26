package theme

import (
	"strings"
	"testing"
	"testing/fstest"
)

// TestLoadDir covers LoadDir (theme-selection, no-built-in-fallback): themes
// are discovered by their directory name beneath root, an unknown/missing
// themes directory is a positioned-style path error, and a theme
// subdirectory missing its required theme.css is rejected naming that
// subdirectory.
func TestLoadDir(t *testing.T) {
	t.Run("themes discovered by directory name", func(t *testing.T) {
		fsys := fstest.MapFS{
			"templates/themes/plain/theme.css":  &fstest.MapFile{Data: []byte("body{}")},
			"templates/themes/plain/logo.svg":   &fstest.MapFile{Data: []byte("<svg/>")},
			"templates/themes/sunset/theme.css": &fstest.MapFile{Data: []byte(":root{}")},
		}
		reg, err := LoadDir(fsys, "templates/themes")
		if err != nil {
			t.Fatalf("LoadDir: %v", err)
		}
		if want := []string{"plain", "sunset"}; !equalNames(reg.Names(), want) {
			t.Fatalf("Names() = %v, want %v", reg.Names(), want)
		}

		plain, err := reg.Lookup("plain")
		if err != nil {
			t.Fatalf("Lookup(plain): %v", err)
		}
		if plain.Stylesheet != Stylesheet {
			t.Errorf("plain.Stylesheet = %q, want %q", plain.Stylesheet, Stylesheet)
		}
		if plain.Assets == nil {
			t.Fatal("plain.Assets is nil, want the theme's own sub-filesystem")
		}
		if data, err := plain.CSS(); err != nil || string(data) != "body{}" {
			t.Errorf("plain.CSS() = (%q, %v), want (\"body{}\", nil)", data, err)
		}
		// Every other file in the theme directory is loaded too, so a
		// theme.css url(…) reference to it resolves.
		if _, err := plain.Assets.Open("logo.svg"); err != nil {
			t.Errorf("plain.Assets.Open(logo.svg): %v", err)
		}
	})

	t.Run("empty themes directory loads to an empty non-nil registry", func(t *testing.T) {
		fsys := fstest.MapFS{"templates/themes/.keep": &fstest.MapFile{Data: []byte("")}}
		reg, err := LoadDir(fsys, "templates/themes")
		if err != nil {
			t.Fatalf("LoadDir: %v", err)
		}
		if reg == nil {
			t.Fatal("LoadDir returned a nil registry for an empty themes directory")
		}
		if got := reg.Names(); len(got) != 0 {
			t.Errorf("Names() = %v, want none", got)
		}
	})

	t.Run("missing themes directory is a path error", func(t *testing.T) {
		_, err := LoadDir(fstest.MapFS{}, "templates/themes")
		if err == nil {
			t.Fatal("LoadDir succeeded over a missing themes directory, want an error")
		}
		if !strings.Contains(err.Error(), "templates/themes") {
			t.Errorf("error = %q, want it to name templates/themes", err)
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want it to say the directory was not found", err)
		}
	})

	t.Run("themes path exists but is not a directory", func(t *testing.T) {
		fsys := fstest.MapFS{"templates/themes": &fstest.MapFile{Data: []byte("not a dir")}}
		_, err := LoadDir(fsys, "templates/themes")
		if err == nil {
			t.Fatal("LoadDir succeeded over a non-directory themes path, want an error")
		}
		if !strings.Contains(err.Error(), "templates/themes") || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("error = %q, want it to name templates/themes and say it is not a directory", err)
		}
	})

	t.Run("theme subdirectory missing theme.css is rejected", func(t *testing.T) {
		fsys := fstest.MapFS{
			"templates/themes/plain/logo.svg": &fstest.MapFile{Data: []byte("<svg/>")},
		}
		_, err := LoadDir(fsys, "templates/themes")
		if err == nil {
			t.Fatal("LoadDir succeeded with a theme missing theme.css, want an error")
		}
		if !strings.Contains(err.Error(), "templates/themes/plain") {
			t.Errorf("error = %q, want it to name the offending theme directory", err)
		}
		if !strings.Contains(err.Error(), "theme.css") {
			t.Errorf("error = %q, want it to mention theme.css", err)
		}
	})

	t.Run("first offending theme subdirectory wins deterministically", func(t *testing.T) {
		fsys := fstest.MapFS{
			"templates/themes/aaa/theme.css": &fstest.MapFile{Data: []byte("x")},
			"templates/themes/bbb/logo.svg":  &fstest.MapFile{Data: []byte("x")},
		}
		_, err := LoadDir(fsys, "templates/themes")
		if err == nil {
			t.Fatal("LoadDir succeeded, want the bbb theme (missing theme.css) to fail")
		}
		if !strings.Contains(err.Error(), "templates/themes/bbb") {
			t.Errorf("error = %q, want it to name templates/themes/bbb", err)
		}
	})
}

// equalNames reports whether a and b hold the same strings in order.
func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
