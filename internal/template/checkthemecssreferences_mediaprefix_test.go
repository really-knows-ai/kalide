package template

import (
	"io/fs"
	"path"
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-01 task-11: it proves
// checkThemeCSSReferences treats a reference carrying the reserved
// MediaURLPrefix() ("media:") as a pointer into the library's shared media/
// tree, for BOTH a url() and an @import target. A url() resolves against
// mediaFS exactly like a layout `media "path"` argument (an existing file
// passes; a missing file, an absolute path, a `..` escape and a nil mediaFS
// each fail as a *LibraryError naming the CSS file and line). An @import
// carrying media: is followed into mediaFS and the imported stylesheet's OWN
// relative references are then checked with media/ as its tree: a relative
// sibling inside media/ resolves, while a relative url() or @import that
// leaves media/ (e.g. '../themes/a/x.woff2', '../library.yaml') is rejected
// with the MEDIA CSS file's display path and line.
//
// Everything is in-memory: the *LibraryTheme is hand-built by
// importTestThemeFixture and mediaFS is a fstest.MapFS (or nil), so no real
// filesystem is touched. The prefix under test is read through
// MediaURLPrefix(), never hardcoded, so this can never drift from the source.

// TestCheckThemeCSSReferencesMediaPrefix is table-driven over the reserved
// `media:` cases. Each error case names the CSS file the error must be
// positioned on, the exact non-zero line and a message substring.
func TestCheckThemeCSSReferencesMediaPrefix(t *testing.T) {
	const root = "templates"
	themeDir := path.Join("templates", ThemesDir, "default")
	themeCSSPath := path.Join(themeDir, ThemeStylesheet)
	importedPath := path.Join(themeDir, "x.css")
	mediaCSSPath := path.Join("templates", MediaDir, "x.css")

	// mediaWithFont is the library's shared media/ tree: it owns exactly one
	// font plus a CSS file, so an @import carrying media: has a stylesheet to
	// follow.
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
			name:       "url() form @import carrying media: is followed",
			stylesheet: `@import url('media:x.css');`,
			mediaFS:    mediaWithFont,
			wantErr:    false,
		},
		{
			name:       "double-quoted url() @import carrying media: is followed",
			stylesheet: `@import url("media:x.css");`,
			mediaFS:    mediaWithFont,
			wantErr:    false,
		},
		{
			name:       "bare quoted-string @import carrying media: is followed",
			stylesheet: `@import 'media:x.css';`,
			mediaFS:    mediaWithFont,
			wantErr:    false,
		},
		{
			name:       "bare quoted-string @import carrying media: fails with no media dir",
			stylesheet: `@import 'media:x.css';`,
			mediaFS:    nil,
			wantErr:    true,
			wantPath:   themeCSSPath,
			wantLine:   1,
			wantSubstr: "no templates/media directory",
		},
		{
			name:       "media: @import's clean relative sibling resolves inside media/",
			stylesheet: `@import 'media:x.css';`,
			mediaFS: fstest.MapFS{
				"x.css":         &fstest.MapFile{Data: []byte("a{background:url('fonts/x.woff2');}\n")},
				"fonts/x.woff2": &fstest.MapFile{Data: []byte("woff")},
			},
			wantErr: false,
		},
		{
			name:       "media: @import's own broken relative ref is located in the media CSS",
			stylesheet: `@import 'media:x.css';`,
			mediaFS: fstest.MapFS{
				"x.css": &fstest.MapFile{Data: []byte("/* intro */\na{background:url('fonts/missing.woff2');}\n")},
			},
			wantErr:    true,
			wantPath:   mediaCSSPath,
			wantLine:   2,
			wantSubstr: "media file not found",
		},
		{
			name:       "media: @import's own relative url() leaving media/ is rejected",
			stylesheet: `@import 'media:x.css';`,
			mediaFS: fstest.MapFS{
				"x.css": &fstest.MapFile{Data: []byte("a{background:url('../themes/a/x.woff2');}\n")},
			},
			wantErr:    true,
			wantPath:   mediaCSSPath,
			wantLine:   1,
			wantSubstr: "escapes media/",
		},
		{
			name:       "media: @import's own relative @import leaving media/ is rejected",
			stylesheet: `@import 'media:x.css';`,
			mediaFS: fstest.MapFS{
				"x.css": &fstest.MapFile{Data: []byte("@import '../library.yaml';\n")},
			},
			wantErr:    true,
			wantPath:   mediaCSSPath,
			wantLine:   1,
			wantSubstr: "escapes media/",
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
			th, lib := importTestThemeFixture(tc.stylesheet, tc.files, tc.mediaFS)
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
