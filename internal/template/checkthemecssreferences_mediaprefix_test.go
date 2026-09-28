package template

import (
	"io/fs"
	"path"
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-01.task-9: it proves
// checkThemeCSSReferences treats a url() carrying the reserved MediaURLPrefix()
// ("media:") differently from every other reference. Such a url() resolves
// against the library's shared media/ sub-filesystem (mediaFS) exactly like a
// layout `media "path"` argument: an existing file passes, while a missing
// file, an absolute path, a `..` escape and a nil mediaFS each fail as a
// *LibraryError naming the CSS file and a non-zero line. An @import target
// carrying media: (or any other scheme) is a templates error naming the CSS
// file and line and is NEVER followed: it is rejected before the target is
// looked up, so it fails even when mediaFS happens to contain the file.
//
// Everything is in-memory: the *LibraryTheme is hand-built by
// importTestThemeFixture (task-7) and mediaFS is a fstest.MapFS (or nil), so no
// real filesystem is touched. The prefix under test is read through
// MediaURLPrefix(), never hardcoded, so this can never drift from the source.

// TestCheckThemeCSSReferencesMediaPrefix is table-driven over the reserved
// `media:` cases. Each error case names the CSS file the error must be
// positioned on, the exact non-zero line and a message substring.
func TestCheckThemeCSSReferencesMediaPrefix(t *testing.T) {
	const root = "templates"
	themeDir := path.Join("templates", ThemesDir, "default")
	themeCSSPath := path.Join(themeDir, ThemeStylesheet)
	importedPath := path.Join(themeDir, "x.css")

	// mediaWithFont is the library's shared media/ tree: it owns exactly one
	// font. It also owns the CSS file that a media:-prefixed @import below
	// names, so an @import rejection that still fires proves the target was
	// never resolved against mediaFS.
	mediaWithFont := fstest.MapFS{
		"fonts/x.woff2": &fstest.MapFile{Data: []byte("woff")},
		"x.css":         &fstest.MapFile{Data: []byte("/* shared media css */")},
	}

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
			name:       "media: url() single-quoted naming an existing media file passes",
			stylesheet: `a{background:url('media:fonts/x.woff2');}`,
			mediaFS:    mediaWithFont,
			wantErr:    false,
		},
		{
			name:       "media: url() double-quoted naming an existing media file passes",
			stylesheet: `a{background:url("media:fonts/x.woff2");}`,
			mediaFS:    mediaWithFont,
			wantErr:    false,
		},
		{
			name:       "media: url() unquoted naming an existing media file passes",
			stylesheet: `a{background:url(media:fonts/x.woff2);}`,
			mediaFS:    mediaWithFont,
			wantErr:    false,
		},
		{
			name:       "missing media: url() errors at the CSS file and line",
			stylesheet: "a{}\na{background:url('media:missing.woff2');}\n",
			mediaFS:    mediaWithFont,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   2,
			wantSubstr: "media file not found",
		},
		{
			name:       "absolute path after the media: prefix is rejected",
			stylesheet: "a{}\n\n a{background:url('media:/abs');}\n",
			mediaFS:    mediaWithFont,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   3,
			wantSubstr: "must be relative to templates/media",
		},
		{
			name:       "parent escape after the media: prefix is rejected",
			stylesheet: "a{}\na{background:url('media:../escape');}\n",
			mediaFS:    mediaWithFont,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   2,
			wantSubstr: "must not escape templates/media",
		},
		{
			name:       "nil mediaFS makes a media: url() fail",
			stylesheet: `a{background:url('media:fonts/x.woff2');}`,
			mediaFS:    nil,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "no templates/media directory",
		},
		{
			name:       "url() form @import carrying media: is rejected and never followed",
			stylesheet: `@import url('media:x.css');`,
			// mediaFS owns x.css: had the import been followed, the
			// stylesheet would resolve; the reserved-prefix error shows it
			// was rejected before any lookup.
			mediaFS:    mediaWithFont,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "reserved",
		},
		{
			name:       "double-quoted url() @import carrying media: is rejected and never followed",
			stylesheet: `@import url("media:x.css");`,
			mediaFS:    mediaWithFont,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "reserved",
		},
		{
			name:       "bare quoted-string @import carrying media: is rejected and never followed",
			stylesheet: `@import 'media:x.css';`,
			mediaFS:    mediaWithFont,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "reserved",
		},
		{
			name:       "bare quoted-string @import carrying media: fails even with no media dir",
			stylesheet: `@import 'media:x.css';`,
			mediaFS:    nil,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "reserved",
		},
		{
			name:       "@import url() carrying an http scheme is rejected",
			stylesheet: "a{}\n@import url('http://x/y.css');\n",
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   2,
			wantSubstr: "must be a relative path inside its theme directory",
		},
		{
			name:       "bare quoted-string @import carrying a data scheme is rejected",
			stylesheet: `@import 'data:text/css,body{}';`,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "must be a relative path inside its theme directory",
		},
		{
			name:       "media: url() in an imported theme-dir CSS passes",
			stylesheet: `@import "x.css";`,
			files: map[string][]byte{
				"x.css": []byte("a{background:url('media:fonts/x.woff2');}\n"),
			},
			mediaFS: mediaWithFont,
			wantErr: false,
		},
		{
			name:       "media: url() in an imported theme-dir CSS is validated and located in that file",
			stylesheet: `@import "x.css";`,
			files: map[string][]byte{
				"x.css": []byte("/* intro */\na{background:url('media:missing.woff2');}\n"),
			},
			mediaFS:    mediaWithFont,
			wantErr:    true,
			wantPath:   importedPath,
			wantLine:   2,
			wantSubstr: "media file not found",
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
