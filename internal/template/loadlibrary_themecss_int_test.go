package template

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for plan.phase-01.task-10:
// LoadLibrary's step 6 (checkThemeCSSReferences) exercised against a real
// library on disk (os.DirFS over a t.TempDir()), not a hand-built
// *LibraryTheme. It proves the whole load path — loadLibraryTheme walking the
// theme directory into th.Files, and the scanner following theme.css's
// @imports in both forms — loads a library whose theme CSS reaches both the
// theme's own files (a relative url('fonts/…')) and the library's shared
// media/ tree (url('media:…'), including a CSS file media/ owns), and that a
// broken reference anywhere in that walk fails step 6 with a *LibraryError
// qualified with the offending CSS file and its 1-based line.
//
// These tests touch the real filesystem, so they are guarded with
// testing.Short(): they are skipped under `go test -short`.
//
// The fixture deliberately has NO slide/section templates: templates are
// optional, so a theme-and-media-only library is load-valid, which keeps this
// test focused on step 6.

// intThemeCSSValid is the valid theme.css: it @imports two stylesheets, one in
// each @import form (bare quoted-string and url()), and itself uses both a
// theme-owned relative url() (theme.css line 5) and a media:-prefixed url()
// (line 6) resolving against the library's shared media/ tree.
const intThemeCSSValid = `@import 'imports/a.css';
@import url('imports/b.css');

body {
  background: url('fonts/theme.woff2');
  cursor: url('media:fonts/shared.woff2');
}
`

// intThemeImportA is the imported stylesheet reached through the bare
// quoted-string @import form; it reaches the CSS file the library's media/
// tree owns via a media:-prefixed url().
const intThemeImportA = `/* theme a */
.a { background: url('media:extra.css'); }
`

// intThemeImportB is the imported stylesheet reached through the url() @import
// form; its relative url() resolves against the theme dir from the imports/
// subdirectory.
const intThemeImportB = `/* theme b */
.b { background: url('../fonts/theme.woff2'); }
`

// writeThemeCSSIntLibrary builds a real on-disk library under a fresh temp dir:
// library.yaml, the given theme.css plus themeFiles (keyed by their path
// relative to the theme directory), and a shared media/ tree owning a font and
// a CSS file. It returns the project root to pass to os.DirFS.
func writeThemeCSSIntLibrary(t *testing.T, themeCSS string, themeFiles map[string]string) string {
	t.Helper()
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, TemplatesDir)

	writeFile(t, filepath.Join(root, LibraryFile), []byte("name: demo\nformat: 1\n"))
	writeFile(t, filepath.Join(root, ThemesDir, "plain", ThemeStylesheet), []byte(themeCSS))
	// The theme-owned font every case's relative url('fonts/theme.woff2')
	// resolves to (and imports/b.css reaches as '../fonts/theme.woff2').
	writeFile(t, filepath.Join(root, ThemesDir, "plain", "fonts", "theme.woff2"), []byte("woff"))
	for rel, data := range themeFiles {
		writeFile(t, filepath.Join(root, ThemesDir, "plain", filepath.FromSlash(rel)), []byte(data))
	}

	// The library's shared media/ tree: a font reached via url('media:…') and
	// the CSS file the library's media/ directory owns.
	writeFile(t, filepath.Join(root, MediaDir, "fonts", "shared.woff2"), []byte("woff"))
	writeFile(t, filepath.Join(root, MediaDir, "extra.css"), []byte("/* shared media css */"))

	return projectDir
}

// TestLoadLibraryIntThemeCSSReferences is table-driven over a real on-disk
// library: one valid case (both @import forms followed; theme-owned relative
// and media:-prefixed url()s resolving) and broken-reference cases, each of
// which must fail with a *LibraryError whose Path is the offending CSS file
// and whose Line is the correct 1-based line — including when the broken
// reference sits in an @imported stylesheet rather than theme.css itself.
func TestLoadLibraryIntThemeCSSReferences(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	themeDir := path.Join(TemplatesDir, ThemesDir, "plain")
	themeCSSPath := path.Join(themeDir, ThemeStylesheet)
	importAPath := path.Join(themeDir, "imports", "a.css")
	importBPath := path.Join(themeDir, "imports", "b.css")

	// validImports accompanies the valid theme.css: both imported stylesheets
	// resolve and themselves only reference files that exist.
	validImports := map[string]string{
		"imports/a.css": intThemeImportA,
		"imports/b.css": intThemeImportB,
	}

	cases := []struct {
		name       string
		themeCSS   string
		themeFiles map[string]string
		wantErr    bool
		wantPath   string
		wantLine   int
		wantSubstr string
	}{
		{
			name:       "valid theme.css imports both forms and reaches theme-owned and media files",
			themeCSS:   intThemeCSSValid,
			themeFiles: validImports,
			wantErr:    false,
		},
		{
			name:       "missing media: url() fails at the theme.css line",
			themeCSS:   "@import 'imports/a.css';\n\nbody { background: url('media:missing.woff2'); }\n",
			themeFiles: validImports,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   3,
			wantSubstr: "media file not found",
		},
		{
			name:       "@import url() carrying media: is rejected at the theme.css line",
			themeCSS:   "@import 'imports/a.css';\n@import url('media:x.css');\n",
			themeFiles: validImports,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   2,
			wantSubstr: "reserved",
		},
		{
			name:       "url() escaping the theme directory fails at the theme.css line",
			themeCSS:   "@import 'imports/a.css';\n\nbody { background: url('../../etc/passwd'); }\n",
			themeFiles: validImports,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   3,
			wantSubstr: "escapes its theme directory",
		},
		{
			name:       "missing theme-owned relative url() fails at the theme.css line",
			themeCSS:   "@import 'imports/a.css';\n\nbody { background: url('fonts/missing.woff2'); }\n",
			themeFiles: validImports,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   3,
			wantSubstr: "file not found inside theme directory",
		},
		{
			name:     "broken media: reference in a bare-quoted @import is located in the imported CSS",
			themeCSS: intThemeCSSValid,
			themeFiles: map[string]string{
				"imports/a.css": "/* theme a */\n.a { background: url('media:missing.woff2'); }\n",
				"imports/b.css": intThemeImportB,
			},
			wantErr:    true,
			wantPath:   importAPath,
			wantLine:   2,
			wantSubstr: "media file not found",
		},
		{
			name:     "broken relative url() in a url()-form @import is located in the imported CSS",
			themeCSS: intThemeCSSValid,
			themeFiles: map[string]string{
				"imports/a.css": intThemeImportA,
				"imports/b.css": "/* theme b */\n.b { background: url('img/missing.svg'); }\n",
			},
			wantErr:    true,
			wantPath:   importBPath,
			wantLine:   2,
			wantSubstr: "file not found inside theme directory",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := writeThemeCSSIntLibrary(t, tc.themeCSS, tc.themeFiles)

			fsys := os.DirFS(projectDir)
			lib, err := LoadLibrary(fsys, TemplatesDir)

			if !tc.wantErr {
				if err != nil {
					t.Fatalf("LoadLibrary() error = %v, want nil", err)
				}
				if lib == nil {
					t.Fatal("LoadLibrary() returned nil Library with nil error")
				}
				if _, ok := lib.Themes["plain"]; !ok {
					t.Fatalf("theme plain not loaded: %v", lib.Themes)
				}
				if !lib.HasMedia {
					t.Error("HasMedia = false, want true: fixture has templates/media")
				}
				return
			}

			if lib != nil {
				t.Fatalf("LoadLibrary() Library = %+v, want nil on error", lib)
			}
			libErr := asLibraryError(t, err)
			if libErr.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", libErr.Path, tc.wantPath)
			}
			if libErr.Line != tc.wantLine {
				t.Errorf("Line = %d, want %d", libErr.Line, tc.wantLine)
			}
			if !strings.Contains(libErr.Message, tc.wantSubstr) {
				t.Errorf("Message = %q, want it to contain %q", libErr.Message, tc.wantSubstr)
			}
		})
	}
}
