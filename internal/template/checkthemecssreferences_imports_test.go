package template

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-01.task-7: it proves
// checkThemeCSSReferences follows a theme.css @import in both its forms (the
// bare quoted-string form and the url() form), and that it stays bounded and
// cycle-safe while doing so. Everything is in-memory: the *LibraryTheme is
// hand-built and mediaFS is a fstest.MapFS (or nil), so no real filesystem is
// touched.

// importTestThemeFixture builds the *LibraryTheme checkThemeCSSReferences
// expects for a theme at templates/themes/default: theme.css plus the given
// owned files, keyed by their path relative to the theme directory (theme.css
// itself is excluded, as loadLibraryTheme stores it).
func importTestThemeFixture(stylesheet string, files map[string][]byte) *LibraryTheme {
	return &LibraryTheme{
		Name:            "default",
		Dir:             path.Join(ThemesDir, "default"),
		StylesheetPath:  path.Join("templates", ThemesDir, "default", ThemeStylesheet),
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

	cases := []struct {
		name       string
		stylesheet string
		files      map[string][]byte
		mediaFS    fs.FS
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
			th := importTestThemeFixture(tc.stylesheet, tc.files)
			err := checkThemeCSSReferences(root, th, tc.mediaFS)
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
// two ways: a fan-out (theme.css importing maxThemeCSSImports distinct files)
// and a non-terminating-looking chain, each of which must stop at the
// maxThemeCSSImports bound instead of following every stylesheet.
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
		th := importTestThemeFixture(b.String(), files)

		libErr := asLibraryError(t, checkThemeCSSReferences(root, th, nil))
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
		th := importTestThemeFixture("@import \"c1.css\";", files)

		libErr := asLibraryError(t, checkThemeCSSReferences(root, th, nil))
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
}
