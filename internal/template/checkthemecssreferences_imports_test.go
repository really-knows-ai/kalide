package template

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-01 tasks 8-10: it
// proves checkThemeCSSReferences follows a theme.css @import in both its forms
// (the bare quoted-string form and the url() form) and ACROSS TREES — into a
// sibling theme through the reserved theme:<name>/ prefix and into the shared
// media/ tree through the reserved media: prefix — that a reference nested in
// an imported stylesheet resolves from that stylesheet's own location and
// owning tree, that an import cycle spanning trees is reported at the
// importing file and line, and that the import walk stays graph-wide bounded
// and cycle-safe. Everything is in-memory: the *LibraryTheme and *Library are
// hand-built by importTestThemeFixture and mediaFS is a fstest.MapFS (or nil),
// so no real filesystem is touched.

// importTestThemeFixture builds the *LibraryTheme checkThemeCSSReferences
// expects for a theme at templates/themes/default: theme.css plus the given
// owned files, keyed by their path relative to the theme directory (theme.css
// itself is excluded, as loadLibraryTheme stores it). It also builds the
// *Library the scanner resolves against — the default theme, any sibling
// themes supplied (kept under their own names so a theme:<name>/ reference has
// a loaded theme to resolve against), and mediaFS as the library's shared
// media/ tree (a nil mediaFS means the library has no templates/media
// directory). The default theme is always the scan's entry point, so the
// existing same-theme cases keep working unchanged.
func importTestThemeFixture(stylesheet string, files map[string][]byte, mediaFS fs.FS, siblings ...*LibraryTheme) (*LibraryTheme, *Library) {
	th := &LibraryTheme{
		Name:            "default",
		Dir:             path.Join(ThemesDir, "default"),
		StylesheetPath:  path.Join("templates", ThemesDir, "default", ThemeStylesheet),
		StylesheetBytes: []byte(stylesheet),
		Files:           files,
	}
	themes := map[string]*LibraryTheme{th.Name: th}
	for _, sib := range siblings {
		themes[sib.Name] = sib
	}
	lib := &Library{
		RootPath: "templates",
		Themes:   themes,
		HasMedia: mediaFS != nil,
		Media:    mediaFS,
	}
	return th, lib
}

// importTestSiblingTheme builds a sibling *LibraryTheme at
// templates/themes/<name>, holding name's theme.css plus its owned files, so a
// theme:<name>/ reference has a loaded theme to resolve against.
func importTestSiblingTheme(name, stylesheet string, files map[string][]byte) *LibraryTheme {
	return &LibraryTheme{
		Name:            name,
		Dir:             path.Join(ThemesDir, name),
		StylesheetPath:  path.Join("templates", ThemesDir, name, ThemeStylesheet),
		StylesheetBytes: []byte(stylesheet),
		Files:           files,
	}
}

// TestCheckThemeCSSReferencesImports is table-driven over the @import cases:
// each error case names the file it must be positioned on, the non-zero line
// and a message substring, and every error must be a *LibraryError.
func TestCheckThemeCSSReferencesImports(t *testing.T) {
	const root = "templates"
	themeDir := path.Join("templates", ThemesDir, "default")
	themeCSSPath := path.Join(themeDir, ThemeStylesheet)
	xPath := path.Join(themeDir, "x.css")
	yPath := path.Join(themeDir, "y.css")
	accentXPath := path.Join("templates", ThemesDir, "accent", "x.css")
	mediaXPath := path.Join("templates", MediaDir, "x.css")

	cases := []struct {
		name       string
		stylesheet string
		files      map[string][]byte
		mediaFS    fs.FS
		siblings   []*LibraryTheme
		wantErr    bool
		wantPath   string
		wantLine   int
		wantSubstr string
	}{
		{
			name:       "bare quoted-string @import is followed",
			stylesheet: `@import "x.css";`,
			files: map[string][]byte{
				"x.css": []byte(`a{background:url("missing.png");}`),
			},
			wantErr:    true,
			wantPath:   xPath,
			wantLine:   1,
			wantSubstr: "file not found",
		},
		{
			name:       "bare single-quoted @import is followed",
			stylesheet: `@import 'x.css';`,
			files: map[string][]byte{
				"x.css": []byte(`a{background:url("missing.png");}`),
			},
			wantErr:    true,
			wantPath:   xPath,
			wantLine:   1,
			wantSubstr: "file not found",
		},
		{
			name:       "url() double-quoted @import is followed",
			stylesheet: `@import url("x.css");`,
			files: map[string][]byte{
				"x.css": []byte(`a{background:url(media:fonts/missing.woff2);}`),
			},
			mediaFS:    fstest.MapFS{},
			wantErr:    true,
			wantPath:   xPath,
			wantLine:   1,
			wantSubstr: "media file not found",
		},
		{
			name:       "url() unquoted @import is followed, and imports nest",
			stylesheet: `@import url(x.css);`,
			files: map[string][]byte{
				"x.css": []byte("@import 'y.css';"),
				"y.css": []byte(`a{background:url("missing.png");}`),
			},
			wantErr:    true,
			wantPath:   yPath,
			wantLine:   1,
			wantSubstr: "file not found",
		},
		{
			name:       "import cycle is reported at the importing file and line",
			stylesheet: "@import \"a.css\";\n",
			files: map[string][]byte{
				"a.css": []byte("/* intro */\n@import \"theme.css\";\n"),
			},
			wantErr:    true,
			wantPath:   path.Join(themeDir, "a.css"),
			wantLine:   2,
			wantSubstr: "import cycle",
		},
		{
			name:       "@import escaping the theme directory errors with file and line",
			stylesheet: "a{}\n\n@import \"../other/theme.css\";\n",
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   3,
			wantSubstr: "escapes its theme directory",
		},
		{
			name:       "theme: @import is followed into a sibling theme and its own relative refs resolve there",
			stylesheet: `@import 'theme:accent/sub/x.css';`,
			siblings: []*LibraryTheme{
				importTestSiblingTheme("accent", "/* accent */", map[string][]byte{
					"sub/x.css":    []byte(`a{background:url('../img/logo.svg');}`),
					"img/logo.svg": []byte("<svg/>"),
				}),
			},
			wantErr: false,
		},
		{
			name:       "theme: @import's own broken relative ref is located in the sibling theme CSS",
			stylesheet: `@import 'theme:accent/x.css';`,
			siblings: []*LibraryTheme{
				importTestSiblingTheme("accent", "/* accent */", map[string][]byte{
					"x.css": []byte("/* intro */\na{background:url('missing.woff2');}\n"),
				}),
			},
			wantErr:    true,
			wantPath:   accentXPath,
			wantLine:   2,
			wantSubstr: "file not found inside theme directory",
		},
		{
			name:       "media: @import is followed and its own relative refs resolve in the media tree",
			stylesheet: `@import 'media:x.css';`,
			mediaFS: fstest.MapFS{
				"x.css":         &fstest.MapFile{Data: []byte("a{background:url('fonts/x.woff2');}\n")},
				"fonts/x.woff2": &fstest.MapFile{Data: []byte("woff")},
			},
			wantErr: false,
		},
		{
			name:       "theme-media import cycle is reported at the media CSS file and line",
			stylesheet: `@import 'media:x.css';`,
			mediaFS: fstest.MapFS{
				"x.css": &fstest.MapFile{Data: []byte("@import 'theme:default/theme.css';\n")},
			},
			wantErr:    true,
			wantPath:   mediaXPath,
			wantLine:   1,
			wantSubstr: "import cycle",
		},
		{
			name:       "theme-theme import cycle is reported at the sibling CSS file and line",
			stylesheet: `@import 'theme:accent/x.css';`,
			siblings: []*LibraryTheme{
				importTestSiblingTheme("accent", "/* accent */", map[string][]byte{
					"x.css": []byte("@import 'theme:default/theme.css';\n"),
				}),
			},
			wantErr:    true,
			wantPath:   accentXPath,
			wantLine:   1,
			wantSubstr: "import cycle",
		},
		{
			name:       "every import resolves cleanly",
			stylesheet: "@import \"x.css\";\n@import url(x-url.css);\n",
			files: map[string][]byte{
				"x.css":     []byte(`a{background:url("img.png");}`),
				"x-url.css": []byte(`b{background:url(media:fonts/x.woff2);}`),
				"img.png":   []byte("png"),
			},
			mediaFS: fstest.MapFS{
				"fonts/x.woff2": &fstest.MapFile{Data: []byte("woff")},
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, lib := importTestThemeFixture(tc.stylesheet, tc.files, tc.mediaFS, tc.siblings...)
			err := checkThemeCSSReferences(root, th, lib)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("checkThemeCSSReferences() error = %v, want nil", err)
				}
				return
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

// TestCheckThemeCSSReferencesImportsBounded proves the import walk is bounded
// three ways: a fan-out (theme.css importing maxThemeCSSImports distinct
// files), a non-terminating-looking chain, and a fan-out spread across the
// default theme, a sibling theme and the shared media/ tree — each of which
// must stop at the same graph-wide maxThemeCSSImports bound instead of
// following every stylesheet.
func TestCheckThemeCSSReferencesImportsBounded(t *testing.T) {
	const root = "templates"
	themeDir := path.Join("templates", ThemesDir, "default")
	themeCSSPath := path.Join(themeDir, ThemeStylesheet)
	n := maxThemeCSSImports + 5

	t.Run("fan-out beyond the bound errors at the limit", func(t *testing.T) {
		files := make(map[string][]byte, n)
		var b strings.Builder
		for i := 1; i <= n; i++ {
			name := fmt.Sprintf("f%d.css", i)
			files[name] = []byte("/* no references */")
			fmt.Fprintf(&b, "@import %q;\n", name)
		}
		th, lib := importTestThemeFixture(b.String(), files, nil)

		libErr := asLibraryError(t, checkThemeCSSReferences(root, th, lib))
		if libErr.Path != themeCSSPath {
			t.Errorf("Path = %q, want %q", libErr.Path, themeCSSPath)
		}
		// Imports 1..63 fit under the bound; the 64th is the first
		// rejected, and it sits on theme.css line 64.
		if libErr.Line != maxThemeCSSImports {
			t.Errorf("Line = %d, want %d", libErr.Line, maxThemeCSSImports)
		}
		if !strings.Contains(libErr.Message, "too many imported stylesheets") {
			t.Errorf("Message = %q, want the import bound error", libErr.Message)
		}
	})

	t.Run("import chain beyond the bound is bounded", func(t *testing.T) {
		files := make(map[string][]byte, n)
		for i := 1; i <= n; i++ {
			content := "/* no references */"
			if i < n {
				content = fmt.Sprintf("@import \"c%d.css\";", i+1)
			}
			files[fmt.Sprintf("c%d.css", i)] = []byte(content)
		}
		// theme.css starts a chain c1 -> c2 -> ... -> c_n.
		th, lib := importTestThemeFixture("@import \"c1.css\";", files, nil)

		libErr := asLibraryError(t, checkThemeCSSReferences(root, th, lib))
		// The stack already holds theme.css plus c1..c63 when c63
		// imports c64, so the bound trips on c63's line 1.
		wantPath := path.Join(themeDir, fmt.Sprintf("c%d.css", maxThemeCSSImports-1))
		if libErr.Path != wantPath {
			t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
		}
		if libErr.Line != 1 {
			t.Errorf("Line = %d, want 1", libErr.Line)
		}
		if !strings.Contains(libErr.Message, "too many imported stylesheets") {
			t.Errorf("Message = %q, want the import bound error", libErr.Message)
		}
	})

	t.Run("bound is graph-wide across themes and media", func(t *testing.T) {
		// 63 stylesheets are pulled in from three different trees — the
		// default theme (d1..d21), the sibling theme accent
		// (theme:accent/a1..a21) and the shared media/ tree
		// (media:m1..m21) — and all fit; the 64th (d22.css) is the first
		// rejected, proving the bound counts the whole graph, not one
		// tree's files.
		defaultFiles := make(map[string][]byte, 22)
		for i := 1; i <= 22; i++ {
			defaultFiles[fmt.Sprintf("d%d.css", i)] = []byte("/* no references */")
		}
		accentFiles := make(map[string][]byte, 21)
		for i := 1; i <= 21; i++ {
			accentFiles[fmt.Sprintf("a%d.css", i)] = []byte("/* no references */")
		}
		mediaFS := fstest.MapFS{}
		for i := 1; i <= 21; i++ {
			mediaFS[fmt.Sprintf("m%d.css", i)] = &fstest.MapFile{Data: []byte("/* no references */")}
		}

		var b strings.Builder
		for i := 1; i <= 21; i++ {
			fmt.Fprintf(&b, "@import %q;\n", fmt.Sprintf("d%d.css", i))
		}
		for i := 1; i <= 21; i++ {
			fmt.Fprintf(&b, "@import %q;\n", fmt.Sprintf("theme:accent/a%d.css", i))
		}
		for i := 1; i <= 21; i++ {
			fmt.Fprintf(&b, "@import %q;\n", fmt.Sprintf("media:m%d.css", i))
		}
		// The 64th import sits on theme.css line 64.
		fmt.Fprintf(&b, "@import %q;\n", "d22.css")

		th, lib := importTestThemeFixture(b.String(), defaultFiles, mediaFS,
			importTestSiblingTheme("accent", "/* accent */", accentFiles))

		libErr := asLibraryError(t, checkThemeCSSReferences(root, th, lib))
		if libErr.Path != themeCSSPath {
			t.Errorf("Path = %q, want %q", libErr.Path, themeCSSPath)
		}
		if libErr.Line != maxThemeCSSImports {
			t.Errorf("Line = %d, want %d", libErr.Line, maxThemeCSSImports)
		}
		if !strings.Contains(libErr.Message, "too many imported stylesheets") {
			t.Errorf("Message = %q, want the import bound error", libErr.Message)
		}
	})
}
