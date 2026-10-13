package template

import (
	"io/fs"
	"path"
	"sort"
	"strings"
)

// This file implements checkImageRoots, the root-aware resolver and existence
// check for an image field's authored path (image-field-roots). It is the
// reusable resolver the WithImageExists callbacks close over: the whole-deck
// validator (validate.checkFields) closes over the deck's assets tree and the
// owning library layer's media/ tree, and the load-time example check
// (checkLibraryExample) closes over the owning layer's media/ alone, since no
// deck exists yet. validateImage (checkvalues.go) turns a missing path into the
// image ValueError, suggesting the closest candidate.

// The two root names an image field's `roots:` may declare. They are the only
// two the manifest parser accepts; checkImageRoots resolves them by name.
const (
	imageRootAssets = "assets"
	imageRootMedia  = "media"
)

// checkImageRoots resolves and existence-checks an image field's authored path
// against the field's declared roots, in their written order, and returns the
// candidate files the field accepts so a missing path can carry a closest-match
// suggestion (image-field-roots).
//
// Root and path syntax. A declared root names the tree the authored path points
// at, and the path is written relative to that tree's own base:
//
//   - `assets` resolves in assets, the deck filesystem rooted at the deck
//     directory; the tree is the deck's `assets/` directory and the path is
//     written deck-relative with the `assets/` prefix, e.g. `assets/team/x.svg`;
//   - `media` resolves in media, the filesystem of the library layer that owns
//     the field's template, rooted at that layer's `media/` directory; the path
//     is written media-relative with no `media/` prefix, e.g. `bgs/right-8.jpg`
//     — the same base the layout `media` helper and the enum `variants_from`
//     expansion use (media-paths-library-relative).
//
// With no declared roots the path resolves under the deck's `assets/` exactly
// as an `assets` root, so a root-less field keeps the default behaviour.
//
// A path is accepted when it names an existing file among the candidates: the
// files under each declared root's tree, restricted to f.Dir (a subdirectory
// within the root: `assets/<dir>/…` for an assets root, `<dir>/…` for a media
// root) when set, and to an allowed file-name extension when f.Extensions is
// set. The path and every candidate are compared in the same written form, so
// an assets path carries its `assets/` prefix while a media path does not.
//
// Nil filesystem = root not available. A declared root whose filesystem is nil
// is skipped, so candidates are gathered only from the resolvable roots and the
// path is accepted when it matches one of them. This lets the load-time example
// check, which has only the owning layer's media, pass nil assets: an
// assets/no-roots field is then simply not existence-checked. When NO declared
// root is resolvable the call reports exists=true with no candidates — "not
// checkable", distinct from "missing" — so a caller never reports an image
// error it cannot substantiate. When at least one root is resolvable and the
// path matches no file, it reports exists=false plus the sorted, de-duplicated
// candidate files for a closest-match suggestion.
func checkImageRoots(f *Field, authored string, assets, media fs.FS) (exists bool, candidates []string) {
	if f == nil {
		return true, nil
	}

	// dir, when set, is the subdirectory of a root the field accepts. A dir
	// that escapes the root can match nothing, so every path is then missing.
	dir := ""
	if f.Dir != "" {
		clean := path.Clean(f.Dir)
		if clean == "." || path.IsAbs(clean) ||
			clean == ".." || strings.HasPrefix(clean, "../") {
			return false, nil
		}
		dir = clean
	}

	roots := f.Roots
	if len(roots) == 0 {
		roots = []string{imageRootAssets}
	}

	// candidateFiles walks base within fsys and returns every regular file, as
	// an fs path, whose name carries an allowed extension (every file when none
	// is declared), in sorted order. A missing or unreadable base yields no
	// candidates rather than an error: an existence check has no error to
	// report through this callback.
	candidateFiles := func(fsys fs.FS, base string) []string {
		var out []string
		_ = fs.WalkDir(fsys, base, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			if !imageExtensionAllowed(p, f.Extensions) {
				return nil
			}
			out = append(out, p)
			return nil
		})
		sort.Strings(out)
		return out
	}

	cleaned := path.Clean(authored)
	var found []string
	available := false
	for _, name := range roots {
		var (
			fsys fs.FS
			base string
		)
		switch name {
		case imageRootAssets:
			// The deck filesystem is rooted at the deck directory; its
			// assets/ subdirectory is the tree and paths carry the prefix.
			fsys, base = assets, imageRootAssets
		case imageRootMedia:
			// The media filesystem is rooted at the layer's media/
			// directory, so media paths are media-relative and the walk
			// starts at the root itself.
			fsys, base = media, "."
		default:
			// An unknown root cannot be resolved; the manifest parser
			// rejects one at load, so this is defensive only.
			continue
		}
		if fsys == nil {
			continue
		}
		available = true
		if dir != "" {
			if base == "." {
				base = dir
			} else {
				base = path.Join(base, dir)
			}
		}
		for _, candidate := range candidateFiles(fsys, base) {
			if candidate == cleaned {
				return true, nil
			}
			found = append(found, candidate)
		}
	}

	if !available {
		return true, nil
	}
	sort.Strings(found)
	out := found[:0]
	for _, candidate := range found {
		if len(out) == 0 || out[len(out)-1] != candidate {
			out = append(out, candidate)
		}
	}
	return false, out
}

// imageExtensionAllowed reports whether the file name carries one of exts, the
// field's declared `extensions:` (image-field-roots). An empty exts allows
// every name. The comparison ignores a leading dot and case, so `jpg`, `.jpg`
// and `JPG` are the same extension. The name is an io/fs path, so path.Ext —
// not filepath.Ext — reads its extension.
func imageExtensionAllowed(name string, exts []string) bool {
	if len(exts) == 0 {
		return true
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	for _, want := range exts {
		if strings.TrimPrefix(strings.ToLower(want), ".") == ext {
			return true
		}
	}
	return false
}
