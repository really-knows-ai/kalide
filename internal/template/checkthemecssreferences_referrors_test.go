package template

import (
	"path"
	"strings"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-01 task-12: it proves
// checkThemeCSSReferences validates a theme stylesheet's non-@import url()
// references (templates-dir-validation step 6). A relative url() naming an
// existing theme-owned file passes; a missing relative url() errors; and an
// absolute path, a `..` escape, or any scheme (http:, https:, data:, a
// protocol-relative //) is rejected. It also covers the reserved
// theme:<name>/ prefix: a url() (or @import) naming a file inside a loaded
// sibling theme resolves, while an unknown theme, a missing file, an absolute
// path inside the theme, another scheme and a `..` that escapes the named
// theme directory are each rejected. Every error must be a *LibraryError
// qualified with the offending CSS file and its non-zero 1-based line —
// including a reference inside an @imported CSS, whose line points into that
// imported file, and a relative reference that leaves the tree of the CSS it
// appears in. The reserved `media:` prefix is covered by task-11, not here.
//
// Everything is in-memory: the *LibraryTheme and *Library are hand-built by
// importTestThemeFixture (task-8) and no filesystem is touched.

// TestCheckThemeCSSReferencesRefErrors is table-driven over the theme.css
// url() cases. Each error case names the CSS file the error must be positioned
// on, the exact non-zero line and a message substring.
func TestCheckThemeCSSReferencesRefErrors(t *testing.T) {
	const root = "templates"
	themeDir := path.Join("templates", ThemesDir, "default")
	themeCSSPath := path.Join(themeDir, ThemeStylesheet)
	importedPath := path.Join(themeDir, "x.css")
	accentXPath := path.Join("templates", ThemesDir, "accent", "x.css")

	accentSibling := func(files map[string][]byte) []*LibraryTheme {
		return []*LibraryTheme{importTestSiblingTheme("accent", "/* accent */", files)}
	}

	cases := []struct {
		name       string
		stylesheet string
		files      map[string][]byte
		siblings   []*LibraryTheme
		wantErr    bool
		wantPath   string
		wantLine   int
		wantSubstr string
	}{
		{
			name:       "relative url() naming an existing theme-owned file passes",
			stylesheet: `a{background:url('fonts/x.woff2');}`,
			files: map[string][]byte{
				"fonts/x.woff2": []byte("woff"),
			},
			wantErr: false,
		},
		{
			name:       "relative url() naming an existing file in a subdirectory passes",
			stylesheet: "a{}\na{background:url(img/icon.svg);}",
			files: map[string][]byte{
				"img/icon.svg": []byte("<svg/>"),
			},
			wantErr: false,
		},
		{
			name:       "missing relative url() errors at the file and line",
			stylesheet: "a{color:red;}\na{background:url('missing.woff2');}\n",
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   2,
			wantSubstr: "file not found inside theme directory",
		},
		{
			name:       "absolute path url() is rejected",
			stylesheet: `a{background:url('/abs/x.woff2');}`,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must be a relative path inside its theme directory",
		},
		{
			name:       "parent escape url() is rejected with the escape wording",
			stylesheet: "a{}\na{background:url('../x.woff2');}\n",
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   2,
			wantSubstr: `url("../x.woff2") escapes its theme directory`,
		},
		{
			name:       "http scheme url() is rejected",
			stylesheet: `a{background:url('http://x/y');}`,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must be a relative path inside its theme directory",
		},
		{
			name:       "https scheme url() is rejected",
			stylesheet: `a{background:url('https://x/y');}`,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must be a relative path inside its theme directory",
		},
		{
			name:       "data scheme url() is rejected",
			stylesheet: `a{background:url('data:image/png;base64,AAAA');}`,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must be a relative path inside its theme directory",
		},
		{
			name:       "protocol-relative url() is rejected",
			stylesheet: `a{background:url('//host/x');}`,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must be a relative path inside its theme directory",
		},
		{
			name:       "theme: url() naming an existing sibling theme file passes",
			stylesheet: `a{background:url('theme:accent/fonts/accent.woff2');}`,
			siblings: accentSibling(map[string][]byte{
				"fonts/accent.woff2": []byte("woff"),
			}),
			wantErr: false,
		},
		{
			name:       "theme: url() naming an unknown theme is rejected",
			stylesheet: `a{background:url('theme:ghost/x.woff2');}`,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "unknown theme",
		},
		{
			name:       "theme: url() naming a missing file in a known theme is rejected",
			stylesheet: "a{}\na{background:url('theme:accent/missing.woff2');}\n",
			siblings:   accentSibling(nil),
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   2,
			wantSubstr: "file not found inside theme directory",
		},
		{
			name:       "theme: url() with an absolute path is rejected",
			stylesheet: `a{background:url('theme:accent//abs/x.woff2');}`,
			siblings:   accentSibling(nil),
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must be relative to theme directory",
		},
		{
			name:       "theme: url() carrying another scheme is rejected",
			stylesheet: `a{background:url('other:accent/x.woff2');}`,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must be a relative path inside its theme directory",
		},
		{
			name:       "theme: url() escaping the named theme directory is rejected",
			stylesheet: `a{background:url('theme:accent/../default/x.woff2');}`,
			siblings:   accentSibling(nil),
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must not escape theme directory",
		},
		{
			name:       "@import url() escaping the named theme directory is rejected",
			stylesheet: "@import url('theme:accent/../../library.yaml');\n",
			siblings:   accentSibling(nil),
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must not escape theme directory",
		},
		{
			name:       "@import naming an unknown theme is rejected",
			stylesheet: "@import 'theme:ghost/theme.css';\n",
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "unknown theme",
		},
		{
			name:       "@import naming a missing file in a known sibling theme is rejected",
			stylesheet: "@import 'theme:accent/missing.css';\n",
			siblings:   accentSibling(nil),
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "file not found inside theme directory",
		},
		{
			name:       "url() in an imported CSS naming an existing theme-owned file passes",
			stylesheet: `@import "x.css";`,
			files: map[string][]byte{
				"x.css":   []byte("a{background:url('img.png');}\n"),
				"img.png": []byte("png"),
			},
			wantErr: false,
		},
		{
			name:       "url() in an imported CSS is validated and located in the imported file",
			stylesheet: `@import "x.css";`,
			files: map[string][]byte{
				"x.css": []byte("/* intro */\na{background:url('missing.woff2');}\n"),
			},
			wantErr:    true,
			wantPath:   importedPath,
			wantLine:   2,
			wantSubstr: "file not found inside theme directory",
		},
		{
			name:       "relative url() in a sibling theme's CSS is confined to that theme",
			stylesheet: `@import 'theme:accent/x.css';`,
			siblings: accentSibling(map[string][]byte{
				"x.css": []byte("a{background:url('../default/fonts/theme.woff2');}\n"),
			}),
			wantErr:    true,
			wantPath:   accentXPath,
			wantLine:   1,
			wantSubstr: "escapes its theme directory",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, lib := importTestThemeFixture(tc.stylesheet, tc.files, nil, tc.siblings...)
			err := checkThemeCSSReferences(root, th, lib)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("checkThemeCSSReferences() error = %v, want nil", err)
				}
				return
			}
			libErr := asLibraryError(t, err)
			if libErr.Line == 0 {
				t.Errorf("Line = 0, want a non-zero line")
			}
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
