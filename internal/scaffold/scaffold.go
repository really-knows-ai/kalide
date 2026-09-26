// Package scaffold creates a new starter deck on disk for `eypres init`
// (requirements.requirement.cli-init and
// requirements.requirement.cli-init-refuse-existing).
//
// Init is filesystem-only and has no knowledge of the command line: internal/cli
// resolves the current directory and turns Init's error into the process exit
// code. The starter content is not written by this package — it is the tree
// embedded in internal/assets and read through assets.Starter, so the scaffolded
// deck is byte-for-byte the content verified at build time and can never drift
// from a copy kept in this package.
package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/really-knows-ai/ey-present/internal/assets"
)

// blockPaths are the three entries a scaffolded deck owns. If any of them is
// already present, Init refuses: the command has no --force and never overwrites
// deck content. The display form matches how the requirement names them
// (`slides/`, `assets/`, `eypres.yaml`).
//
// Other entries in the target directory — a .git directory, a README, an editor
// lock file — are irrelevant to the deck and do NOT block init.
var blockPaths = []struct {
	path    string // name tested against the filesystem
	display string // name used in the refusal message
}{
	{path: "slides", display: "slides/"},
	{path: "assets", display: "assets/"},
	{path: "eypres.yaml", display: "eypres.yaml"},
}

// Init writes a starter deck into dir: slides/ holding the two embedded starter
// slides, an empty assets/ directory for the author's images, and eypres.yaml.
//
// It first checks that none of slides/, assets/ or eypres.yaml already exists.
// If any does, Init writes nothing at all and returns an error naming every
// existing blocking path; there is no --force and no partial overwrite. When the
// directory is clear, Init copies every file in the embedded assets.Starter tree
// to the same relative path under dir, creating the directories along the way,
// and then creates the empty assets/ directory (an empty directory is not
// embeddable, so it is made explicitly rather than copied).
//
// dir may be relative or absolute and need not exist yet; it is created as
// needed. Init is deterministic and uses only the local filesystem: it never
// reaches the network.
func Init(dir string) error {
	conflicts, err := existingBlockPaths(dir)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("init: cannot create the starter deck: %s already present; eypres init never overwrites (remove it or run in an empty directory)",
			strings.Join(conflicts, ", "))
	}

	if err := copyStarter(dir); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		return fmt.Errorf("init: create assets/: %w", err)
	}
	return nil
}

// existingBlockPaths reports the blockPaths that are present in dir, in the
// fixed order above so the refusal message is deterministic. An entry counts as
// present whether it is a file, a directory or a symlink (Lstat does not follow
// the link), so a broken or dangling slides/ still blocks init rather than
// being silently replaced.
func existingBlockPaths(dir string) ([]string, error) {
	var conflicts []string
	for _, block := range blockPaths {
		_, err := os.Lstat(filepath.Join(dir, block.path))
		switch {
		case err == nil:
			conflicts = append(conflicts, block.display)
		case errors.Is(err, fs.ErrNotExist):
			// Not present: this path does not block.
		default:
			return nil, fmt.Errorf("init: inspect %s: %w", block.display, err)
		}
	}
	return conflicts, nil
}

// copyStarter walks the embedded assets.Starter tree and copies every file to
// the same relative path under dir, creating parent directories as needed. The
// starter tree's layout is exactly the layout of a deck directory, so the
// relative path of each embedded file is its destination path.
func copyStarter(dir string) error {
	starter := assets.Starter()
	return fs.WalkDir(starter, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("init: read embedded starter: %w", err)
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

		data, err := fs.ReadFile(starter, path)
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
