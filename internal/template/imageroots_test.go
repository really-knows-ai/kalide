package template

import (
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for the image-field-roots behaviour of
// checkImageRoots, its exported ImageExistsFunc wrapper and the validateImage
// step that consumes them. It pins three things the root resolver must do:
//
//   - a media-root path existence-checks against the owning layer's media/ tree
//     with dir and extensions filtering, and a media path carries no assets/
//     prefix while an assets path does;
//   - a no-such-file value reports the sorted candidate set the field accepts,
//     and CheckValues turns it into the image error with a closest-match
//     suggestion (or the plain "add the file …" fix when none is close) through
//     WithImageExists(ImageExistsFunc(...));
//   - an image field with `roots: [assets]` or with no roots declared still
//     accepts a path under the deck's assets/, and nil filesystems report "not
//     checkable" rather than a false missing.
//
// Fixtures are testing/fstest.MapFS, so no real filesystem or watcher is
// touched and every case stays in-process and deterministic (unit tier).

// deckMediaFS is a media/ tree as the owning layer sees it: rooted at the
// layer's media/ directory, so paths are media-relative with no media/ prefix.
func deckMediaFS() fstest.MapFS {
	return fstest.MapFS{
		"bgs/right-8.jpg":  &fstest.MapFile{Data: []byte("8")},
		"bgs/right-9.jpg":  &fstest.MapFile{Data: []byte("9")},
		"bgs/notes.txt":    &fstest.MapFile{Data: []byte("n")},
		"bgs/sub/deep.jpg": &fstest.MapFile{Data: []byte("d")},
		"other/x.jpg":      &fstest.MapFile{Data: []byte("x")},
	}
}

// deckAssetsFS is a deck filesystem rooted at the deck directory, so its
// assets/ tree is reached with paths that carry the assets/ prefix.
func deckAssetsFS() fstest.MapFS {
	return fstest.MapFS{
		"assets/logo.png":   &fstest.MapFile{Data: []byte("l")},
		"assets/team/x.svg": &fstest.MapFile{Data: []byte("x")},
		"assets/team/y.svg": &fstest.MapFile{Data: []byte("y")},
	}
}

// imageRootsCase is one direct checkImageRoots assertion: the field schema, the
// authored path, the two filesystems, and the expected exists/candidates pair.
type imageRootsCase struct {
	name       string
	field      *Field
	authored   string
	assets     fs.FS
	media      fs.FS
	wantExists bool
	wantCands  []string
}

// TestCheckImageRoots exercises the resolver directly: media-root resolution
// with dir and extensions filtering, assets-root (and the no-roots default)
// resolution of prefixed paths, candidate gathering, and the not-checkable
// result when no declared root is resolvable.
func TestCheckImageRoots(t *testing.T) {
	media := deckMediaFS()
	assets := deckAssetsFS()

	// The media tree's whole candidate set, sorted, is the baseline several
	// filtered cases restrict; the assets set is its small counterpart.
	allMedia := []string{
		"bgs/notes.txt",
		"bgs/right-8.jpg",
		"bgs/right-9.jpg",
		"bgs/sub/deep.jpg",
		"other/x.jpg",
	}
	jpgMedia := []string{"bgs/right-8.jpg", "bgs/right-9.jpg", "bgs/sub/deep.jpg"}
	jpgMediaAll := []string{"bgs/right-8.jpg", "bgs/right-9.jpg", "bgs/sub/deep.jpg", "other/x.jpg"}
	allAssets := []string{"assets/logo.png", "assets/team/x.svg", "assets/team/y.svg"}

	tests := []imageRootsCase{
		{
			name:       "media root file exists",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"media"}},
			authored:   "bgs/right-8.jpg",
			media:      media,
			wantExists: true,
		},
		{
			name:       "media root dir filter accepts in-dir file",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"media"}, Dir: "bgs"},
			authored:   "bgs/right-9.jpg",
			media:      media,
			wantExists: true,
		},
		{
			name:       "media root dir filter excludes out-of-dir file",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"media"}, Dir: "bgs"},
			authored:   "other/x.jpg",
			media:      media,
			wantExists: false,
			wantCands:  allMedia[:4], // dir bgs only, so other/x.jpg is not offered
		},
		{
			name:       "media root extensions filter excludes other extension",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"media"}, Extensions: []string{"jpg"}},
			authored:   "bgs/notes.txt",
			media:      media,
			wantExists: false,
			wantCands:  jpgMediaAll,
		},
		{
			name:       "media root dir and extensions filter both apply",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"media"}, Dir: "bgs", Extensions: []string{"jpg"}},
			authored:   "other/x.jpg",
			media:      media,
			wantExists: false,
			wantCands:  jpgMedia,
		},
		{
			name:       "media root missing reports full candidate set",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"media"}},
			authored:   "bgs/right-88.jpg",
			media:      media,
			wantExists: false,
			wantCands:  allMedia,
		},
		{
			name:       "assets root accepts prefixed path",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"assets"}},
			authored:   "assets/team/x.svg",
			assets:     assets,
			wantExists: true,
		},
		{
			name:       "assets root dir filter restricts candidates",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"assets"}, Dir: "team"},
			authored:   "assets/logo.png",
			assets:     assets,
			wantExists: false,
			wantCands:  []string{"assets/team/x.svg", "assets/team/y.svg"},
		},
		{
			name:       "no roots defaults to the deck assets tree",
			field:      &Field{Name: "img", Type: FieldImage},
			authored:   "assets/logo.png",
			assets:     assets,
			wantExists: true,
		},
		{
			name:       "no roots missing reports assets candidates",
			field:      &Field{Name: "img", Type: FieldImage},
			authored:   "assets/logo.svg",
			assets:     assets,
			wantExists: false,
			wantCands:  allAssets,
		},
		{
			name:       "assets-only root rejects a media-relative path",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"assets"}},
			authored:   "team/x.svg",
			assets:     assets,
			wantExists: false,
			wantCands:  allAssets,
		},
		{
			name:       "both roots gather candidates from each tree",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"assets", "media"}},
			authored:   "nope.jpg",
			assets:     assets,
			media:      media,
			wantExists: false,
			wantCands: []string{
				"assets/logo.png", "assets/team/x.svg", "assets/team/y.svg",
				"bgs/notes.txt", "bgs/right-8.jpg", "bgs/right-9.jpg",
				"bgs/sub/deep.jpg", "other/x.jpg",
			},
		},
		{
			name:       "media-only root is not checkable when media is nil",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"media"}},
			authored:   "bgs/right-8.jpg",
			wantExists: true,
		},
		{
			name:       "no-roots field is not checkable when assets is nil",
			field:      &Field{Name: "img", Type: FieldImage},
			authored:   "assets/logo.png",
			media:      media,
			wantExists: true,
		},
		{
			name:       "unknown root is skipped and nothing is checkable",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"bogus"}},
			authored:   "assets/logo.png",
			assets:     assets,
			wantExists: true,
		},
		{
			name:       "dir escaping the root can match nothing",
			field:      &Field{Name: "img", Type: FieldImage, Roots: []string{"assets"}, Dir: "../secret"},
			authored:   "assets/team/x.svg",
			assets:     assets,
			wantExists: false,
		},
		{
			name:       "nil field is not checkable",
			field:      nil,
			authored:   "anything.png",
			assets:     assets,
			media:      media,
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotExists, gotCands := checkImageRoots(tt.field, tt.authored, tt.assets, tt.media)
			if gotExists != tt.wantExists {
				t.Fatalf("checkImageRoots(%q) exists = %v, want %v (candidates %v)",
					tt.authored, gotExists, tt.wantExists, gotCands)
			}
			if !reflect.DeepEqual(gotCands, tt.wantCands) {
				t.Errorf("checkImageRoots(%q) candidates = %#v, want %#v",
					tt.authored, gotCands, tt.wantCands)
			}
		})
	}
}

// TestCheckValuesImageRoots drives ImageExistsFunc through CheckValues exactly
// as the whole-deck validator does: the WithImageExists callback closes over the
// deck's assets tree and the owning layer's media tree, and a missing path
// becomes the image error carrying the closest accepted candidate.
func TestCheckValuesImageRoots(t *testing.T) {
	media := deckMediaFS()
	assets := deckAssetsFS()

	noRoots := newSlide("imgnoroots", Field{Name: "img", Type: FieldImage})
	assetsOnly := newSlide("imgassets", Field{Name: "img", Type: FieldImage, Roots: []string{"assets"}})
	mediaOnly := newSlide("imgmedia", Field{Name: "img", Type: FieldImage, Roots: []string{"media"}})
	mediaFiltered := newSlide("imgmediafiltered", Field{Name: "img", Type: FieldImage,
		Roots: []string{"media"}, Dir: "bgs", Extensions: []string{"jpg"}})

	tests := []struct {
		name   string
		tmpl   *Template
		data   map[string]any
		assets fs.FS
		media  fs.FS
		want   []wantErr
	}{
		{
			name:   "no roots accepts an assets path",
			tmpl:   noRoots,
			data:   map[string]any{"img": "assets/logo.png"},
			assets: assets,
			media:  media,
		},
		{
			name:   "assets-only root accepts an assets path",
			tmpl:   assetsOnly,
			data:   map[string]any{"img": "assets/team/x.svg"},
			assets: assets,
		},
		{
			name:   "missing assets path suggests the closest candidate",
			tmpl:   assetsOnly,
			data:   map[string]any{"img": "assets/team/z.svg"},
			assets: assets,
			want: []wantErr{we("image", "img", "assets/team/z.svg",
				`image: "assets/team/z.svg" does not exist`,
				`did you mean "assets/team/x.svg"?`)},
		},
		{
			name:   "missing assets path with no close match uses the default fix",
			tmpl:   assetsOnly,
			data:   map[string]any{"img": "assets/zzzzzzzzzz.png"},
			assets: assets,
			want: []wantErr{we("image", "img", "assets/zzzzzzzzzz.png",
				`image: "assets/zzzzzzzzzz.png" does not exist`,
				"add the file under assets/ or fix the path")},
		},
		{
			name:  "media root accepts a media-relative path",
			tmpl:  mediaOnly,
			data:  map[string]any{"img": "bgs/right-8.jpg"},
			media: media,
		},
		{
			name:  "missing media path suggests the closest candidate",
			tmpl:  mediaOnly,
			data:  map[string]any{"img": "bgs/right-7.jpg"},
			media: media,
			want: []wantErr{we("image", "img", "bgs/right-7.jpg",
				`image: "bgs/right-7.jpg" does not exist`,
				`did you mean "bgs/right-8.jpg"?`)},
		},
		{
			name:  "media dir and extensions filter accept an allowed file",
			tmpl:  mediaFiltered,
			data:  map[string]any{"img": "bgs/right-9.jpg"},
			media: media,
		},
		{
			name:  "media extension filter excludes another extension",
			tmpl:  mediaFiltered,
			data:  map[string]any{"img": "bgs/notes.txt"},
			media: media,
			want: []wantErr{we("image", "img", "bgs/notes.txt",
				`image: "bgs/notes.txt" does not exist`,
				"add the file under assets/ or fix the path")},
		},
		{
			name:  "media dir filter excludes an out-of-dir file",
			tmpl:  mediaFiltered,
			data:  map[string]any{"img": "outside/x.jpg"},
			media: media,
			want: []wantErr{we("image", "img", "outside/x.jpg",
				`image: "outside/x.jpg" does not exist`,
				"add the file under assets/ or fix the path")},
		},
		{
			name: "media-only field is not existence-checked without a media fs",
			tmpl: mediaOnly,
			data: map[string]any{"img": "bgs/whatever.jpg"},
		},
		{
			name:   "assets-only root rejects a media-relative path",
			tmpl:   assetsOnly,
			data:   map[string]any{"img": "team/x.svg"},
			assets: assets,
			want: []wantErr{we("image", "img", "team/x.svg",
				`image: "team/x.svg" is not under assets/`,
				"move the image into the deck's assets/ directory and reference it as assets/…")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckValues(tt.data, tt.tmpl, nil,
				WithImageExists(ImageExistsFunc(tt.assets, tt.media)))
			if len(got.Errors) != len(tt.want) {
				t.Fatalf("CheckValues() errors = %d, want %d\n got: %s",
					len(got.Errors), len(tt.want), renderErrors(got.Errors))
			}
			for i, w := range tt.want {
				e := got.Errors[i]
				if e.Rule != w.Rule || e.PathString() != w.Path || e.What != w.What || e.Fix != w.Fix {
					t.Errorf("error[%d] = rule %q path %q what %q fix %q, want rule %q path %q what %q fix %q",
						i, e.Rule, e.PathString(), e.What, e.Fix, w.Rule, w.Path, w.What, w.Fix)
				}
			}
		})
	}
}
