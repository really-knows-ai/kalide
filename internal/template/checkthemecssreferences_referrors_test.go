package template

import (
	"path"
	"strings"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-01.task-8: it proves
// checkThemeCSSReferences validates a theme stylesheet's non-@import url()
// references (templates-dir-validation step 6). A relative url() naming an
// existing theme-owned file passes; a missing relative url() errors; and an
// absolute path, a `..` escape, or any scheme (http:, https:, data:, a
// protocol-relative //) is rejected. Every error must be a *LibraryError
// qualified with the offending CSS file and its non-zero 1-based line —
// including a url() inside an @imported theme-dir CSS, whose line points into
// that imported file.
//
// Everything is in-memory: the *LibraryTheme is hand-built by
// importTestThemeFixture (task-7) and no filesystem is touched. The reserved
// `media:` prefix is deliberately out of scope here — task-9 covers it.

// TestCheckThemeCSSReferencesRefErrors is table-driven over the theme.css
// url() cases. Each error case names the CSS file the error must be positioned
// on, the exact non-zero line and a message substring.
func TestCheckThemeCSSReferencesRefErrors(t *testing.T) {
	const root = "templates"
	themeDir := path.Join("templates", ThemesDir, "default")
	themeCSSPath := path.Join(themeDir, ThemeStylesheet)
	importedPath := path.Join(themeDir, "x.css")

	cases := []struct {
		name       string
		stylesheet string
		files      map[string][]byte
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th := importTestThemeFixture(tc.stylesheet, tc.files)
			err := checkThemeCSSReferences(root, th, nil)
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
