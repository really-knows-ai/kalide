// This file implements InitLibrary and DefaultThemeCSS: `kalide init-library
// <path>` (requirements.requirement.init-library).
//
// It creates a new, deck-ready template library at path: a library.yaml, the
// standard top-level layout entries slides/, sections/ and media/ created
// empty, a minimal themes/default/theme.css, and the library-root AGENTS.md
// library/template-author guide (library-agent-guide) written verbatim from the
// embedded guide when none exists. Nothing else is seeded — no starter
// template, no media file, no extra theme — so the library is "empty
// but valid": it loads cleanly through the template loader
// (templates-dir-validation) and its default theme lets a deck whose optional
// `theme` key defaults to `default` resolve immediately (theme-selection).
//
// The name written to library.yaml is derived from the target directory's base
// name and validated against the library naming convention, [a-z0-9][a-z0-9-]*;
// an invalid name is a hard error and is never sanitised, so nothing is written.
// InitLibrary refuses to clobber (no --force): if the target already holds a
// library.yaml or any standard layout entry it fails, writing nothing, while a
// non-existent target is created and an existing directory that is empty or
// holds only unrelated files (a .git directory, a README) is allowed with its
// unrelated files left untouched.
//
// InitLibrary is filesystem-only and has no knowledge of the command line:
// internal/cli passes the argument through and turns InitLibrary's error into
// the process exit code. It never reaches the network or git
// (requirements.requirement.single-binary,
// global.constraint.anonymous-install).
package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// libraryNamePattern is the accepted form for the library name InitLibrary
// derives from the target directory's base name: it must start with a lowercase
// letter or digit and continue with lowercase letters, digits or hyphens
// (project-templates). It is the same convention template.loadLibraryMeta
// enforces when the created library is later loaded.
var libraryNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// libraryBlockPaths are the library-owned top-level entries whose presence makes
// InitLibrary refuse: the metadata file and the four standard layout
// directories. A library has no `templates/` entry of its own — its root IS the
// templates directory — so unlike a deck there is no fifth name.
//
// Other entries in the target directory — a .git directory, a README, an editor
// lock file — are unrelated to the library and do NOT block init.
var libraryBlockPaths = []struct {
	path    string // name tested against the filesystem
	display string // name used in the refusal message
}{
	{path: template.LibraryFile, display: template.LibraryFile},
	{path: template.SlidesDir, display: template.SlidesDir + "/"},
	{path: template.SectionsDir, display: template.SectionsDir + "/"},
	{path: template.ThemesDir, display: template.ThemesDir + "/"},
	{path: template.MediaDir, display: template.MediaDir + "/"},
}

// DefaultThemeCSS returns the content InitLibrary writes to
// themes/default/theme.css: the minimal default theme, using only system fonts
// and neutral colours, mirrored verbatim from the embedded no-arg `kalide init`
// seed's templates/themes/default/theme.css. Deriving it from the one embedded
// seed guarantees the two never drift, so a deck whose `theme` defaults to
// `default` resolves in a freshly created library exactly as it does in a fresh
// deck.
func DefaultThemeCSS() string {
	return string(mustReadSeed(path.Join(
		template.TemplatesDir, template.ThemesDir, theme.DefaultName, template.ThemeStylesheet)))
}

// libraryConfig is the subset of library.yaml InitLibrary writes: the derived
// name, the optional description (omitted when empty) and the required format
// version. It is marshalled through yaml so the fields are written in a stable,
// canonical order.
type libraryConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Format      int    `yaml:"format"`
}

// InitLibrary writes a new, deck-ready template library into dir.
//
// It first derives the library name from dir's base name and validates it
// against [a-z0-9][a-z0-9-]*; an invalid name aborts with nothing written. It
// then refuses (writing nothing) when dir already holds a library.yaml or any
// of the standard layout entries slides/, sections/, themes/ or media/. When
// dir exists but is not a directory, that too is an error. Otherwise dir is
// created as needed and the library is written: library.yaml (name, format 1)
// and the empty slides/, sections/ and media/ directories plus
// themes/default/theme.css (DefaultThemeCSS). It also writes the library-root
// AGENTS.md library/template-author guide (library-agent-guide) verbatim from
// the embedded guide, but only when no AGENTS.md already exists at the target:
// a pre-existing one never blocks and is left byte-for-byte untouched. The
// guide is the one inert non-template root entry the library layout permits
// (library-guide-layout), so the created library still loads cleanly.
//
// dir may be relative or absolute and need not exist yet. InitLibrary is
// deterministic and uses only the local filesystem: it never reaches the
// network and never runs git.
func InitLibrary(dir string) error {
	name, err := libraryName(dir)
	if err != nil {
		return err
	}

	if err := checkLibraryTarget(dir); err != nil {
		return err
	}

	conflicts, err := existingLibraryPaths(dir)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return libraryRefusal(conflicts)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("init-library: create %s: %w", dir, err)
	}

	data, err := yaml.Marshal(libraryConfig{Name: name, Format: 1})
	if err != nil {
		return fmt.Errorf("init-library: write %s: %w", template.LibraryFile, err)
	}
	if err := os.WriteFile(filepath.Join(dir, template.LibraryFile), data, 0o644); err != nil {
		return fmt.Errorf("init-library: write %s: %w", template.LibraryFile, err)
	}

	for _, layout := range []string{template.SlidesDir, template.SectionsDir, template.MediaDir} {
		if err := os.MkdirAll(filepath.Join(dir, layout), 0o755); err != nil {
			return fmt.Errorf("init-library: create %s/: %w", layout, err)
		}
	}

	themeDir := filepath.Join(dir, template.ThemesDir, theme.DefaultName)
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		return fmt.Errorf("init-library: create %s/%s/: %w", template.ThemesDir, theme.DefaultName, err)
	}
	stylesheet := filepath.Join(themeDir, template.ThemeStylesheet)
	if err := os.WriteFile(stylesheet, []byte(DefaultThemeCSS()), 0o644); err != nil {
		return fmt.Errorf("init-library: write %s/%s/%s: %w",
			template.ThemesDir, theme.DefaultName, template.ThemeStylesheet, err)
	}

	// Write the library-root agent guide (library-agent-guide) verbatim from
	// the embedded library guide, only when no AGENTS.md exists at the target.
	// A pre-existing one is left byte-for-byte untouched and never blocks
	// init-library: AGENTS.md is not one of libraryBlockPaths, and the library
	// layout permits it as the single inert non-template root entry
	// (library-guide-layout).
	guide := filepath.Join(dir, "AGENTS.md")
	if _, statErr := os.Lstat(guide); errors.Is(statErr, fs.ErrNotExist) {
		if err := os.WriteFile(guide, mustReadLibraryGuide(), 0o644); err != nil {
			return fmt.Errorf("init-library: write AGENTS.md: %w", err)
		}
	} else if statErr != nil {
		return fmt.Errorf("init-library: inspect AGENTS.md: %w", statErr)
	}
	return nil
}

// libraryName derives the library name from dir's base name and validates it
// against [a-z0-9][a-z0-9-]* (project-templates). An empty base name or one
// that does not match is a hard error naming the offending name; the name is
// never sanitised, so nothing is written.
func libraryName(dir string) (string, error) {
	name := filepath.Base(filepath.Clean(dir))
	if !libraryNamePattern.MatchString(name) {
		return "", fmt.Errorf("init-library: target directory base name %q is not a valid library name (want [a-z0-9][a-z0-9-]*)", name)
	}
	return name, nil
}

// checkLibraryTarget reports an error when dir exists but is not a directory.
// A missing dir is fine: InitLibrary creates it. os.Stat follows a symlink, so
// a symlink to a directory is a directory; any other existing non-directory
// (a file, a device) is an error, consistent with the loader's own root check.
func checkLibraryTarget(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case err == nil && !info.IsDir():
		return fmt.Errorf("init-library: %s exists but is not a directory", dir)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("init-library: inspect %s: %w", dir, err)
	}
	return nil
}

// existingLibraryPaths reports the libraryBlockPaths that are present in dir,
// in the fixed order above so the refusal message is deterministic. An entry
// counts as present whether it is a file, a directory or a symlink (Lstat does
// not follow the link), so a broken or dangling slides/ still blocks init
// rather than being silently replaced.
func existingLibraryPaths(dir string) ([]string, error) {
	var conflicts []string
	for _, block := range libraryBlockPaths {
		_, err := os.Lstat(filepath.Join(dir, block.path))
		switch {
		case err == nil:
			conflicts = append(conflicts, block.display)
		case errors.Is(err, fs.ErrNotExist):
			// Not present: this path does not block.
		default:
			return nil, fmt.Errorf("init-library: inspect %s: %w", block.display, err)
		}
	}
	return conflicts, nil
}

// libraryRefusal formats the refusal: it names every blocking path in
// libraryBlockPaths order and states that init-library never overwrites, so the
// caller writes nothing.
func libraryRefusal(conflicts []string) error {
	return fmt.Errorf("init-library: cannot create the template library: %s already present; kalide init-library never overwrites (remove it or run in an empty directory)",
		strings.Join(conflicts, ", "))
}

// mustReadSeed returns the content of one file in the embedded hello seed,
// named by its slash-separated path relative to the seed root. The embed
// directive in helloseed.go is the source of truth, so a failure here is a
// programming error, not a runtime condition.
func mustReadSeed(name string) []byte {
	data, err := fs.ReadFile(seedRoot, name)
	if err != nil {
		panic("scaffold: missing embedded seed file " + name + ": " + err.Error())
	}
	return data
}

// mustReadLibraryGuide returns the content of the embedded library/template-
// author agent guide (libraryguide/AGENTS.md). The embed directive in
// libraryguide.go is the source of truth, so a failure here is a programming
// error, not a runtime condition; it mirrors mustReadSeed.
func mustReadLibraryGuide() []byte {
	data, err := fs.ReadFile(libraryGuideRoot, "AGENTS.md")
	if err != nil {
		panic("scaffold: missing embedded library guide AGENTS.md: " + err.Error())
	}
	return data
}
