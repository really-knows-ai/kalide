// This file implements writeHelloSeed: the minimal, unbranded starter deck
// `kalide init` now writes (cli-init-minimal-seed), replacing the EY starter
// previously copied from internal/assets.Starter.
//
// The seed is embedded here, in internal/scaffold, rather than in
// internal/assets: it is scaffold's own content, not a compiled-in asset
// other packages serve or reference. It ships one minimal slide template
// (templates/slides/hello), one neutral theme (templates/themes/default)
// carrying only system fonts and neutral colours, and one slide that uses
// the template — enough for the written deck to load and validate cleanly
// through template.LoadLibrary and template.NewRegistryFromLibrary without
// any section, font, logo, media or EY-branded reference anywhere in it.
package scaffold

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/really-knows-ai/kalide/internal/template"
)

// seedFS is the embedded seed tree, rooted so that "templates", "slides" and
// "kalide.yaml" are its direct entries — the same layout writeHelloSeed
// copies to the target deck directory, and the same layout LoadLibrary
// expects when given "templates" as its root.
//
//go:embed seed
var seedEmbed embed.FS

// seedRoot is the sub-tree of seedEmbed rooted at "seed", resolved once: the
// embed directive above guarantees it exists, so resolving lazily would only
// add error handling that can never trigger at runtime.
var seedRoot = mustSeedSub(seedEmbed, "seed")

// writeHelloSeed walks the embedded seed tree and copies every file to the
// same relative path under dir, creating parent directories as needed. The
// seed tree's layout is exactly the layout of a deck directory (kalide.yaml,
// slides/, templates/), so the relative path of each embedded file is its
// destination path.
//
// The one special case is the deck-root AGENTS.md agent guide
// (agent-authoring): it is written only when no AGENTS.md exists at the
// target. A pre-existing guide is left byte-for-byte untouched and never
// blocks init; every other seed file is copied as usual.
func writeHelloSeed(dir string) error {
	return fs.WalkDir(seedRoot, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("init: read embedded seed: %w", err)
		}
		if path == "." {
			return nil
		}

		target := filepath.Join(dir, filepath.FromSlash(path))
		if entry.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("init: create %s: %w", filepath.ToSlash(path), err)
			}
			return nil
		}

		// The deck-root agent guide (agent-authoring) is copied like every
		// other seed file, but only when no AGENTS.md already exists at the
		// target: a pre-existing one is left byte-for-byte untouched and
		// never blocks init.
		if path == "AGENTS.md" {
			if _, statErr := os.Lstat(target); statErr == nil {
				return nil
			} else if !errors.Is(statErr, fs.ErrNotExist) {
				return fmt.Errorf("init: inspect %s: %w", filepath.ToSlash(path), statErr)
			}
		}

		data, err := fs.ReadFile(seedRoot, path)
		if err != nil {
			return fmt.Errorf("init: read embedded %s: %w", filepath.ToSlash(path), err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("init: create %s: %w", filepath.ToSlash(filepath.Dir(path)), err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("init: write %s: %w", filepath.ToSlash(path), err)
		}
		return nil
	})
}

// mustSeedSub returns the sub-tree of fsys rooted at dir, panicking when dir
// is not an embedded directory. The embed directive above is the source of
// truth, so a failure here is a programming error, not a runtime condition.
func mustSeedSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("scaffold: missing embedded sub-tree " + dir + ": " + err.Error())
	}
	return sub
}

// validateSeed loads and validates the embedded seed's templates/ library
// exactly as a written deck's would be loaded: through
// template.LoadLibrary and, inside it, template.NewRegistryFromLibrary. It
// is the compile-time self-test that the embedded seed satisfies
// cli-init-minimal-seed's requirement that the written deck loads and
// validates cleanly; a table-driven test in scaffold_test.go (owned by the
// test-implementer) is the executable form of this same check.
func validateSeed() error {
	_, err := template.LoadLibrary(seedRoot, template.TemplatesDir)
	return err
}

// init runs validateSeed once, at package load: a broken embedded seed is a
// compiled-in programming error, exactly like a theme registration failure
// (internal/theme.builtin), so it panics at startup rather than surfacing as
// a confusing runtime error the first time someone runs `kalide init`.
func init() {
	if err := validateSeed(); err != nil {
		panic("scaffold: embedded hello seed fails template.LoadLibrary validation: " + err.Error())
	}
}
