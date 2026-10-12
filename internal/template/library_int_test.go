package template

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/displaypath"
)

// This file is the integration-test deliverable for plan.phase-01.task-6:
// LoadLibrary against a real filesystem (os.DirFS over a temp dir), covering
// the no-built-in-fallback behavior when a project has no usable templates/
// directory, and a full on-disk library — slides, sections, a theme with an
// owned font file, and media including a video — loading cleanly.
//
// It also carries plan.phase-01.task-12's integration coverage of the layered
// load: LoadDeckLibrary over a real external + local two-layer deck, where a
// slide, a theme and a media file from each layer all resolve from the merged
// Library, each layer's own root and media stay addressable through Layers, and
// a theme name or a media path carried by both layers is rejected naming both
// paths with equal standing (layered-template-library, library-layer-uniqueness,
// theme-selection). The slides/sections merge, cross-kind uniqueness and dedupe
// of LoadDeckLibrary are covered by TestLoadDeckLibraryMergedLayers
// (resolve_test.go) and are not repeated here.
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
// *LibraryError naming the expected templates/ path with the `kalide init`
// hint when a project has no templates/ directory at all — no built-in
// fallback is substituted.
func TestLoadLibraryIntMissingTemplatesDir(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}
	projectDir := t.TempDir()
	// A project root with no templates/ entry whatsoever.
	writeFile(t, filepath.Join(projectDir, "kalide.yaml"), []byte("title: demo\n"))

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
		t.Errorf("Hint = %q, want it to mention `kalide init`", libErr.Hint)
	}
}

// TestLoadLibraryIntTemplatesIsRegularFile proves LoadLibrary reports a
// *LibraryError naming the expected templates/ path with the `kalide init`
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
		t.Errorf("Hint = %q, want it to mention `kalide init`", libErr.Hint)
	}
}

// containsHint reports whether hint mentions the `kalide init` remediation.
func containsHint(hint string) bool {
	return hint == "run `kalide init` to create one"
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

	// Nested-composition section template: a section template may itself
	// declare `sections:` with the same schema as a slide's, so composition
	// nests (template-composition). group holds `items` children accepting
	// the item section, and its example nests a child instance under a
	// top-level `# items` heading.
	writeFile(t, filepath.Join(root, "sections", "group", "template.yaml"),
		[]byte("description: a group of item children\n"+
			"sections:\n  - name: items\n    accepted: [item]\n    min: 1\n    max: 4\n"))
	writeFile(t, filepath.Join(root, "sections", "group", "layout.html.tmpl"),
		[]byte("<ul>{{range .items}}<li>{{.}}</li>{{end}}</ul>"))
	writeFile(t, filepath.Join(root, "sections", "group", "example.md"), []byte("# items\n"))

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
	// The nested container section loaded with its declared child section.
	group, kind, ok := lib.TemplateByName("group")
	if !ok || kind != KindSection {
		t.Fatalf("TemplateByName(group) = (%v, %q, %v), want (non-nil, %q, true)", group, kind, ok, KindSection)
	}
	if group.Definition == nil {
		t.Fatal("checkLibraryBuild left group.Definition nil")
	}
	if got := group.Definition.Sections; len(got) != 1 || got[0].Name != "items" ||
		len(got[0].Accepted) != 1 || got[0].Accepted[0] != "item" {
		t.Errorf("group.Sections = %+v, want one %q section accepting item", got, "items")
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

// TestLoadDeckLibraryMergedThemeAndMediaLayers covers LoadDeckLibrary's merged
// load over a real external + local two-layer deck with themes and media
// (layered-template-library, library-layer-uniqueness, theme-selection). It
// complements TestLoadDeckLibraryMergedLayers (resolve_test.go), which covers
// the slides/sections merge, cross-kind uniqueness and dedupe; here a slide, a
// theme and a media file defined in each layer all resolve from the merged
// Library, each contributing layer's own root is retained in Layers[i].RootPath
// and its media stays addressable through Layers[i].Media, and a theme name or a
// media path carried by both layers is rejected naming both paths. The layers
// merge with equal standing: a duplicate is an error whoever carries it, never a
// silent override, so both carrying paths are always named.
func TestLoadDeckLibraryMergedThemeAndMediaLayers(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// writeLayerWithThemeAndMedia writes a minimal valid library at dir with
	// the given metadata name, one slide, one theme (theme.css) and one media
	// file, so a two-layer deck can show every kind resolving from each layer.
	writeLayerWithThemeAndMedia := func(dir, name, slide, theme, media string) {
		t.Helper()
		writeLibraryWithTemplates(t, dir, name, []string{slide}, nil)
		writeFile(t, filepath.Join(dir, ThemesDir, theme, ThemeStylesheet),
			[]byte("body { color: black; }"))
		writeFile(t, filepath.Join(dir, MediaDir, media), []byte(media))
	}

	t.Run("both layers' slide, theme and media resolve from the merged library", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLayerWithThemeAndMedia(localRoot, "local", "local-slide", "local-theme", "local.png")
		external := t.TempDir()
		writeLayerWithThemeAndMedia(external, "external", "ext-slide", "ext-theme", "ext.png")

		lib, err := LoadDeckLibrary(deckRoot, external)
		if err != nil {
			t.Fatalf("LoadDeckLibrary: %v", err)
		}

		for _, name := range []string{"ext-slide", "local-slide"} {
			if _, _, ok := lib.TemplateByName(name); !ok {
				t.Errorf("TemplateByName(%q) not found: the merged library must carry both layers' slides", name)
			}
		}
		for _, name := range []string{"ext-theme", "local-theme"} {
			if _, ok := lib.Themes[name]; !ok {
				t.Errorf("Themes[%q] not found: the merged library must carry both layers' themes", name)
			}
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

		// Each layer's own themes and media stay addressable through its own
		// layer, even though the merged Library exposes only the primary
		// (local, deck-root templates/) layer's media.
		if _, ok := lib.Layers[0].Themes["ext-theme"]; !ok {
			t.Error("Layers[0].Themes[ext-theme] not found")
		}
		if _, ok := lib.Layers[1].Themes["local-theme"]; !ok {
			t.Error("Layers[1].Themes[local-theme] not found")
		}
		if !lib.Layers[0].HasMedia {
			t.Error("Layers[0].HasMedia = false, want true (the external layer carries media/)")
		}
		if _, err := fs.Stat(lib.Layers[0].Media, "ext.png"); err != nil {
			t.Errorf("Layers[0].Media does not resolve the external layer's ext.png: %v", err)
		}
		if !lib.Layers[1].HasMedia {
			t.Error("Layers[1].HasMedia = false, want true (the local layer carries media/)")
		}
		if _, err := fs.Stat(lib.Layers[1].Media, "local.png"); err != nil {
			t.Errorf("Layers[1].Media does not resolve the local layer's local.png: %v", err)
		}
		if _, err := fs.Stat(lib.Media, "local.png"); err != nil {
			t.Errorf("merged.Media does not resolve the local (primary) layer's local.png: %v", err)
		}
		if _, err := fs.Stat(lib.Media, "ext.png"); err == nil {
			t.Error("merged.Media resolves the external layer's ext.png, want the local (primary) layer's media only")
		}
		if !lib.HasMedia {
			t.Error("HasMedia = false, want true")
		}
	})

	t.Run("a theme name carried by both layers errors naming both theme paths", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLayerWithThemeAndMedia(localRoot, "local", "local-slide", "dup", "local.png")
		external := t.TempDir()
		writeLayerWithThemeAndMedia(external, "external", "ext-slide", "dup", "ext.png")

		lib, err := LoadDeckLibrary(deckRoot, external)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		// Both paths are named, so neither layer's theme silently wins: the
		// layers have equal standing (library-layer-uniqueness).
		assertLayerDuplicateError(t, err, "theme", "dup",
			filepath.Join(external, ThemesDir, "dup"), filepath.Join(localRoot, ThemesDir, "dup"))
	})

	t.Run("a media path carried by both layers errors naming both media paths", func(t *testing.T) {
		deckRoot := t.TempDir()
		localRoot := filepath.Join(deckRoot, TemplatesDir)
		writeLayerWithThemeAndMedia(localRoot, "local", "local-slide", "local-theme", "dup.png")
		external := t.TempDir()
		writeLayerWithThemeAndMedia(external, "external", "ext-slide", "ext-theme", "dup.png")

		lib, err := LoadDeckLibrary(deckRoot, external)
		if lib != nil {
			t.Fatalf("LoadDeckLibrary() Library = %+v, want nil on error", lib)
		}
		// Both media paths are named, so neither layer's file silently wins:
		// the layers have equal standing (library-layer-uniqueness).
		assertLayerDuplicateError(t, err, "media path", "dup.png",
			filepath.Join(external, MediaDir, "dup.png"), filepath.Join(localRoot, MediaDir, "dup.png"))
	})
}

// TestCheckLayoutReferencesRootSpellingStabilityInt proves layout-reference
// containment is decided on the layer-relative path, not on how the root is
// spelled: the same escaping reference (and the same contained reference)
// yields the same verdict whether the root is bare, trailing-separator or
// "./"-prefixed. Only the layer-relative path drives the accept/reject
// decision; the root appears solely in the message, so a differently-spelled
// root can never turn an escaping reference into an accepted one (or vice
// versa). This is the checkLayoutReferences half of the containment rewrite
// that task-17's theme-CSS case pins.
//
// checkLayoutReferences is the seam the loader calls per layout at step 6, so
// it is driven directly here; the verdict does not depend on a successful load.
func TestCheckLayoutReferencesRootSpellingStabilityInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// Three spellings of one root (templates/): bare, trailing-separator and
	// "./"-prefixed. All denote the same templates/ tree.
	spellings := []string{
		TemplatesDir,
		TemplatesDir + string(os.PathSeparator),
		"." + string(os.PathSeparator) + TemplatesDir,
	}

	cases := []struct {
		name    string
		layout  string
		wantErr bool
		substr  string
	}{
		{
			// Three ".." from slides/hello leaves the templates/ tree, so the
			// reference escapes the layer regardless of how the root is spelled.
			name:    "an escaping literal src is rejected for every root spelling",
			layout:  `<img src="../../../secret.png">`,
			wantErr: true,
			substr:  "must stay inside",
		},
		{
			name:    "a contained literal src is accepted for every root spelling",
			layout:  `<img src="img/logo.svg">`,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, root := range spellings {
				lt := &LibraryTemplate{
					Dir:        SlidesDir + "/hello",
					LayoutText: tc.layout,
					LayoutPath: displaypath.Display(root, SlidesDir+"/hello/"+LayoutFile),
				}
				err := checkLayoutReferences(root, lt, nil)
				if tc.wantErr {
					if err == nil {
						t.Errorf("checkLayoutReferences(root=%q) = nil, want the containment rejection", root)
						continue
					}
					libErr := asLibraryError(t, err)
					if !strings.Contains(libErr.Message, tc.substr) {
						t.Errorf("root %q: Message = %q, want it to contain %q", root, libErr.Message, tc.substr)
					}
					continue
				}
				if err != nil {
					t.Errorf("checkLayoutReferences(root=%q) = %v, want nil", root, err)
				}
			}
		})
	}
}
