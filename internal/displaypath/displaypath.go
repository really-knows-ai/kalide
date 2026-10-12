// Package displaypath builds user-facing filesystem paths shared across the
// deck, template, validate and server packages.
//
// The paths built here are for display only: they use the host operating
// system's native separator so that messages match what the user sees in their
// file manager or shell. They are never an identity or lookup key — identity
// paths stay forward-slash normalised so the same file is recognised across
// operating systems.
package displaypath

import "path/filepath"

// Display joins root and rel into a path suitable for showing to a user.
//
// rel is expected to use forward slashes (the canonical identity spelling);
// the result uses the host operating system's native separator. When root is
// empty the relative path is returned on its own, which is the deck-relative
// case.
//
// The result is for display only and is never an identity or lookup key.
func Display(root, rel string) string {
	if root == "" {
		return filepath.FromSlash(rel)
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

// CanonicalRoot returns a canonical spelling of dir for stable path display
// and comparison.
//
// It makes the path absolute and resolves symbolic links; on Windows this also
// expands 8.3 short names to their long form. On any error it falls back to
// filepath.Clean of the input as given, so callers always receive a usable
// path.
func CanonicalRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return filepath.Clean(dir)
	}
	return resolved
}
