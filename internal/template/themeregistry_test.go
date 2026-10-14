package template

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/theme"
)

// This file is the unit-test deliverable for plan.phase-04.task-10:
// LoadThemeRegistry beside its one definition (themeregistry.go). It covers the
// shared per-layer theme load (shared-theme-registry-load): the single-layer
// path keeps theme.LoadDir's exact missing-themes/ error, the multi-layer path
// loads each layer's themes/ from that layer's own RootPath and merges the
// entries so a theme keeps its contributing layer's files, and a layer with no
// themes/ is skipped cleanly.
//
// LoadThemeRegistry reads each layer through os.DirFS(layer.RootPath), so these
// tests build the layer roots as tiny real temp trees (as resolve_test.go
// does) rather than an in-memory fs.FS; the trees are small, so the tests still
// run under -short. It reuses writeFile (library_int_test.go).

// writeLayerTheme writes one theme directory named name beneath root/themes/,
// with the given theme.css source.
func writeLayerTheme(t *testing.T, root, name, css string) {
	t.Helper()
	writeFile(t, filepath.Join(root, ThemesDir, name, ThemeStylesheet), []byte(css))
}

// TestLoadThemeRegistry covers the shared per-layer theme load
// (shared-theme-registry-load) beside its definition.
func TestLoadThemeRegistry(t *testing.T) {
	t.Run("a single layer keeps theme.LoadDir's exact missing-themes error", func(t *testing.T) {
		root := t.TempDir() // deliberately no themes/ directory
		lib := &Library{
			RootPath: root,
			Layers:   []*LibraryLayer{{RootPath: root}},
		}

		_, err := LoadThemeRegistry(lib)
		if err == nil {
			t.Fatal("LoadThemeRegistry succeeded over a single layer with no themes/, want theme.LoadDir's error")
		}

		// The single-layer path is theme.LoadDir verbatim, so its error must be
		// byte-for-byte the one a direct LoadDir over the same root returns.
		_, wantErr := theme.LoadDir(os.DirFS(root), ThemesDir)
		if wantErr == nil {
			t.Fatal("theme.LoadDir succeeded, want an error to compare against")
		}
		if err.Error() != wantErr.Error() {
			t.Errorf("error = %q, want theme.LoadDir's exact %q", err, wantErr)
		}
		if !strings.Contains(err.Error(), "themes directory not found") {
			t.Errorf("error = %q, want the missing-themes/ message", err)
		}
	})

	t.Run("multiple layers merge each layer's themes from its own root", func(t *testing.T) {
		externalRoot := t.TempDir()
		localRoot := t.TempDir()
		writeLayerTheme(t, externalRoot, "external", "body{external}")
		writeLayerTheme(t, localRoot, "local", "body{local}")
		// The same filename in each theme resolves to that theme's own layer
		// file, proving the merged theme keeps the contributing layer's assets.
		writeFile(t, filepath.Join(externalRoot, ThemesDir, "external", "logo.svg"), []byte("<svg>external</svg>"))
		writeFile(t, filepath.Join(localRoot, ThemesDir, "local", "logo.svg"), []byte("<svg>local</svg>"))

		lib := &Library{
			RootPath: localRoot,
			Layers: []*LibraryLayer{
				{RootPath: externalRoot},
				{RootPath: localRoot},
			},
		}

		reg, err := LoadThemeRegistry(lib)
		if err != nil {
			t.Fatalf("LoadThemeRegistry: %v", err)
		}
		if want := []string{"external", "local"}; !reflect.DeepEqual(reg.Names(), want) {
			t.Fatalf("Names() = %v, want %v (both layers merged)", reg.Names(), want)
		}

		for _, tt := range []struct{ name, css, logo string }{
			{"external", "body{external}", "<svg>external</svg>"},
			{"local", "body{local}", "<svg>local</svg>"},
		} {
			th, err := reg.Lookup(tt.name)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", tt.name, err)
			}
			if css, err := th.CSS(); err != nil || string(css) != tt.css {
				t.Errorf("%s.CSS() = (%q, %v), want (%q, nil)", tt.name, css, err, tt.css)
			}
			if logo, err := fs.ReadFile(th.Assets, "logo.svg"); err != nil || string(logo) != tt.logo {
				t.Errorf("%s logo.svg = (%q, %v), want its own layer's %q", tt.name, logo, err, tt.logo)
			}
		}
	})

	t.Run("a layer with no themes/ contributes no themes without erroring", func(t *testing.T) {
		withThemes := t.TempDir()
		withoutThemes := t.TempDir()
		writeLayerTheme(t, withThemes, "only", "body{only}")

		lib := &Library{
			RootPath: withoutThemes,
			Layers: []*LibraryLayer{
				{RootPath: withoutThemes}, // no themes/
				{RootPath: withThemes},
			},
		}

		reg, err := LoadThemeRegistry(lib)
		if err != nil {
			t.Fatalf("LoadThemeRegistry over a layer with no themes/ errored: %v", err)
		}
		if want := []string{"only"}; !reflect.DeepEqual(reg.Names(), want) {
			t.Errorf("Names() = %v, want %v", reg.Names(), want)
		}
	})

	t.Run("layers that all lack themes load to an empty registry", func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		lib := &Library{
			RootPath: first,
			Layers:   []*LibraryLayer{{RootPath: first}, {RootPath: second}},
		}

		reg, err := LoadThemeRegistry(lib)
		if err != nil {
			t.Fatalf("LoadThemeRegistry with no themes anywhere errored: %v", err)
		}
		if reg == nil {
			t.Fatal("LoadThemeRegistry returned a nil registry, want an empty non-nil one")
		}
		if got := reg.Names(); len(got) != 0 {
			t.Errorf("Names() = %v, want none", got)
		}
	})
}
