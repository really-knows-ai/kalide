package theme

import (
	"fmt"
	"io/fs"
	"path"
)

// This file implements LoadDir, which builds a *Registry from a project's
// templates/themes directory (theme-selection, project-templates): each
// direct subdirectory is one theme, named after its directory, requiring a
// theme.css stylesheet; every other file in the directory is loaded too, so
// url(…) references inside theme.css (fonts, logos, …) resolve.
//
// LoadDir reports a plain path-qualified error — "path[:0]: message" as the
// file:line-less form internal/deck's positioned errors use — for a missing
// or invalid themes directory, or a theme subdirectory missing its required
// theme.css. There is no built-in fallback: a missing or broken themes/
// directory is always reported, never silently substituted with a
// compiled-in theme (no-built-in-fallback).

// Stylesheet is the fixed required file inside a theme directory, matching
// internal/template.ThemeStylesheet.
const Stylesheet = "theme.css"

// dirError formats a path-qualified error with no line, matching the
// file: message convention internal/deck's positioned errors and
// internal/template's LibraryError use.
func dirError(p, format string, args ...any) error {
	return fmt.Errorf("%s: %s", p, fmt.Sprintf(format, args...))
}

// LoadDir loads every theme subdirectory directly beneath root within fsys —
// typically root == "templates/themes" — into a fresh *Registry. It returns
// an error naming root when root does not exist or is not a directory, and
// an error naming the offending theme subdirectory when it does not contain
// theme.css.
//
// A themes directory with no subdirectories at all loads to an empty,
// non-nil registry: LoadDir does not require at least one theme to exist,
// leaving that check (if any) to its caller.
func LoadDir(fsys fs.FS, root string) (*Registry, error) {
	info, err := fs.Stat(fsys, root)
	if err != nil {
		return nil, dirError(root, "themes directory not found")
	}
	if !info.IsDir() {
		return nil, dirError(root, "exists but is not a directory")
	}

	sub, err := fs.Sub(fsys, root)
	if err != nil {
		return nil, dirError(root, "not usable as a directory: %v", err)
	}

	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, dirError(root, "read directory: %v", err)
	}

	reg := NewRegistry()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		dir := path.Join(root, name)

		if _, statErr := fs.Stat(sub, path.Join(name, Stylesheet)); statErr != nil {
			return nil, dirError(dir, "%s not found", Stylesheet)
		}

		themeFS, subErr := fs.Sub(sub, name)
		if subErr != nil {
			return nil, dirError(dir, "not usable as a directory: %v", subErr)
		}

		t := Theme{Name: name, Stylesheet: Stylesheet, Assets: themeFS}
		if err := reg.Register(t); err != nil {
			return nil, dirError(dir, "%v", err)
		}
	}
	return reg, nil
}
