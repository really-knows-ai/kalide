package template

import (
	"os"
	"path/filepath"
)

// This file is the template loader's single library-root resolution point
// (external-template-library): ResolveLibraryRoot turns a deck's raw
// `templates:` value plus its deck root into the one library root to use, and
// LoadDeckLibrary loads and validates that root through the shared loader body
// in loadlibrary.go. No other place resolves a library root, so there is
// exactly one authority deciding it: a configured `templates:` path is
// authoritative and never silently falls back to a local `templates/`, and the
// error when neither resolves names the offending or expected path.
//
// The root is resolved on the real filesystem, against the directory holding
// kalide.yaml, rather than through the deck's fs.FS: an external library may
// sit above the deck root (e.g. `../shared-lib`), which os.DirFS and
// fs.ValidPath reject.

// ResolveLibraryRoot resolves the template-library root a deck must use.
//
// deckRoot is the directory holding the deck's kalide.yaml, as an
// operating-system path. configured is the deck's raw top-level `templates:`
// value as written (deck.TemplatesPath): "" when the key is absent or empty.
//
// When configured is non-empty it wins and is authoritative: a relative path is
// resolved against deckRoot, an absolute path is used as given, and the local
// templates/ directory is ignored entirely. When configured is empty the local
// templates/ directory under deckRoot is used.
//
// The resolved root must exist and be a directory. When it does not — the
// configured path is missing or not a directory, or (with no key) the local
// templates/ is absent or not a directory — ResolveLibraryRoot returns a
// *LibraryError naming that offending or expected path, with the `kalide init`
// hint only for the absent-local case. There is no built-in fallback, and a
// configured path never silently falls back to the local templates/
// (templates-dir-required).
func ResolveLibraryRoot(deckRoot, configured string) (string, error) {
	if configured != "" {
		root := configured
		if !filepath.IsAbs(root) {
			root = filepath.Join(deckRoot, root)
		}
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil {
			return "", libraryErrorf(root, 0, "%s directory not found", root).
				withHint("check the `templates:` path in kalide.yaml")
		}
		if !info.IsDir() {
			return "", libraryErrorf(root, 0, "%s exists but is not a directory", root).
				withHint("check the `templates:` path in kalide.yaml")
		}
		return root, nil
	}

	root := filepath.Join(deckRoot, TemplatesDir)
	info, err := os.Stat(root)
	if err != nil {
		return "", missingTemplatesDirError(root, root+" directory not found")
	}
	if !info.IsDir() {
		return "", missingTemplatesDirError(root, root+" exists but is not a directory")
	}
	return root, nil
}

// LoadDeckLibrary loads and validates the template library a deck must use,
// resolving its root through ResolveLibraryRoot and then running the full
// templates-dir-validation checks at that resolved root (both the templates and
// the themes under it), returning the first path-qualified templates error.
//
// deckRoot is the directory holding the deck's kalide.yaml; configured is the
// deck's raw `templates:` value as written (deck.TemplatesPath). The resolved
// root may sit outside the deck, so LoadDeckLibrary opens it directly on the
// operating system rather than through the deck's fs.FS. Its errors are
// path-qualified with the resolved root, e.g.
// "<root>/slides/content/template.yaml:12".
func LoadDeckLibrary(deckRoot, configured string) (*Library, error) {
	root, err := ResolveLibraryRoot(deckRoot, configured)
	if err != nil {
		return nil, err
	}
	return loadLibrary(os.DirFS(root), ".", root)
}
