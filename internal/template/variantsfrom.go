package template

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// This file implements expandVariantsFrom, the load-time expansion of an enum
// field's `variants_from:` glob (enum-variants-from). An enum field declares
// its closed value set inline with `variants:` or, instead, as a glob with
// `variants_from:`; the glob is written relative to the root of the library
// layer that owns the field's template (e.g. `media/bgs/*.jpg`) and names
// files under that layer's media/ tree. The field's effective variants are the
// matching files, each written relative to the layer's media/ tree (e.g.
// `bgs/right-8.jpg`), in sorted order — the same base the `image` field's
// media root and the layout `media` helper use (media-paths-library-relative).
//
// Because the glob is resolved against the owning layer's own root, an
// external layer's glob resolves within that external library alone and never
// reaches the deck's local templates/ or another layer
// (external-library-self-contained). expandVariantsFrom is a pure fs.FS
// function, so it stays in-binary, offline and OS-neutral across all six
// supported targets (single-binary).
//
// expandVariantsFrom is called by the build-time variant checks
// (checkFieldVariants) while a library layer loads, before any content is
// validated, so an enum field's effective variants are known both to the
// schema engine's membership check and to `kalide templates <name>`. Its
// caller supplies the owning layer's root filesystem — the LibraryLayer.Root
// that has "media", "slides", "sections" and "themes" as direct entries.

// expandVariantsFrom expands an enum field's `variants_from:` glob into the
// field's effective variant set (enum-variants-from). f is the enum field; its
// VariantsFrom is the glob, and root is the filesystem of the library layer
// that owns the field's template, rooted so that "media" is a direct entry.
//
// The glob is a layer-root-relative io/fs pattern using path.Match syntax (the
// syntax fs.Glob accepts), for example `media/bgs/*.jpg`, and every file it
// matches that lies under the layer's `media/` tree becomes a variant, written
// media-relative (the `media/` prefix dropped), for example `bgs/right-8.jpg`.
// A match that is not a regular file (a directory) is not a variant, and a
// match outside `media/` is ignored: the spec's glob names files under the
// layer's media/ tree, so only those are the field's valid values. The result
// is sorted and deterministic, identical across runs.
//
// An empty VariantsFrom means no expansion: it returns (nil, nil), and the
// caller leaves the field's inline Variants untouched. A glob that is absolute,
// or that escapes the layer root with a `..` segment, is rejected as a
// templates error rather than allowed to read outside the layer; a glob that is
// otherwise valid but matches no file under the layer's `media/` tree is a
// templates error naming the glob. The error is a plain error naming the glob —
// the caller adds the offending template context (its name and manifest path)
// when it reports the load failure.
func expandVariantsFrom(f *Field, root fs.FS) ([]string, error) {
	if f == nil || f.VariantsFrom == "" {
		return nil, nil
	}
	pattern := f.VariantsFrom

	// The glob is relative to the layer root. Clean it first so an interior
	// `..` (media/x/../*.jpg) is resolved, then reject a cleaned pattern that
	// is absolute or still climbs out of the root: such a glob can name nothing
	// under the layer's media/ tree, and os.DirFS would otherwise let it read
	// outside the layer.
	clean := path.Clean(pattern)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("variants_from %q must be a path relative to the library layer's root", pattern)
	}

	// fs.Glob reports only ErrBadPattern; a missing directory or an unreadable
	// one matches nothing rather than erroring. A bad pattern is a malformed
	// manifest value, reported without pretending it matched no file.
	matches, err := fs.Glob(root, clean)
	if err != nil {
		return nil, fmt.Errorf("variants_from %q is not a valid glob: %v", pattern, err)
	}

	prefix := MediaDir + "/"
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, ok := strings.CutPrefix(match, prefix)
		if !ok || rel == "" {
			continue
		}
		info, statErr := fs.Stat(root, match)
		if statErr != nil || info.IsDir() {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("variants_from %q matches no file under the library layer's media/ tree", pattern)
	}
	return out, nil
}
