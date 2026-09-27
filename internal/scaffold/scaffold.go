// Package scaffold creates a new starter deck on disk for `kalide init`
// (requirements.requirement.cli-init,
// requirements.requirement.cli-init-refuse-existing and
// requirements.requirement.cli-init-minimal-seed).
//
// Init is filesystem-only and has no knowledge of the command line: internal/cli
// resolves the current directory and turns Init's error into the process exit
// code. With no argument the starter content is not written by this package's
// caller — it is the minimal "hello" seed embedded in this package
// (helloseed.go): one unbranded slide template, one neutral theme and one slide
// that uses it, so the scaffolded deck is byte-for-byte the content verified at
// build time (helloseed.go's init self-test) and can never drift from a copy
// kept elsewhere. With an external library path, Init instead delegates to
// InitExternal (external.go), which references that library and writes no seed
// at all.
package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// blockPaths are the four entries a scaffolded deck owns. If any of them is
// already present, Init refuses: the command has no --force and never overwrites
// deck content. The display form matches how the requirement names them
// (`slides/`, `templates/`, `assets/`, `kalide.yaml`).
//
// Other entries in the target directory — a .git directory, a README, an editor
// lock file — are irrelevant to the deck and do NOT block init.
var blockPaths = []struct {
	path    string // name tested against the filesystem
	display string // name used in the refusal message
}{
	{path: "slides", display: "slides/"},
	{path: "templates", display: "templates/"},
	{path: "assets", display: "assets/"},
	{path: "kalide.yaml", display: "kalide.yaml"},
}

// Init writes a deck into dir.
//
// With no externalPath — the no-arg `kalide init` — it writes the minimal
// hello seed: slides/ holding the starter slide, templates/ holding the hello
// slide template and the default theme, an empty assets/ directory for the
// author's images, and kalide.yaml. It first checks that none of slides/,
// templates/, assets/ or kalide.yaml already exists. If any does, Init writes
// nothing at all and returns an error naming every existing blocking path;
// there is no --force and no partial overwrite. When the directory is clear,
// Init copies every file in the embedded hello seed to the same relative path
// under dir, creating the directories along the way, and then creates the
// empty assets/ directory (an empty directory is not embeddable, so it is made
// explicitly rather than copied).
//
// When externalPath is non-empty — `kalide init <path>` — Init delegates to
// InitExternal: it validates the external template library and the deck's
// default theme fail-fast, then writes kalide.yaml with `templates: <path>`,
// an empty slides/ and an empty assets/, and NO local templates/ and NO
// starter slide (external-template-library, cli-init).
//
// externalPath is optional: callers that take a single optional library path
// pass it directly, and an empty string is treated as "no path" — the no-arg
// hello-seed form, byte-for-byte unchanged. Passing more than one path is a
// programming error and is rejected.
//
// dir may be relative or absolute and need not exist yet; it is created as
// needed. Init is deterministic and uses only the local filesystem: it never
// reaches the network.
func Init(dir string, externalPath ...string) error {
	if len(externalPath) > 1 {
		return fmt.Errorf("init: at most one external library path is allowed")
	}
	if len(externalPath) == 1 && externalPath[0] != "" {
		return InitExternal(dir, externalPath[0])
	}

	conflicts, err := existingBlockPaths(dir, false)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return blockRefusal(conflicts)
	}

	if err := writeHelloSeed(dir); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		return fmt.Errorf("init: create assets/: %w", err)
	}
	return nil
}

// blockRefusal formats the refusal both init forms share: it names every
// blocking path in blockPaths order and states that init never overwrites, so
// the caller writes nothing.
func blockRefusal(conflicts []string) error {
	return fmt.Errorf("init: cannot create the starter deck: %s already present; kalide init never overwrites (remove it or run in an empty directory)",
		strings.Join(conflicts, ", "))
}

// existingBlockPaths reports the blockPaths that are present in dir, in the
// fixed order above so the refusal message is deterministic. An entry counts as
// present whether it is a file, a directory or a symlink (Lstat does not follow
// the link), so a broken or dangling slides/ still blocks init rather than
// being silently replaced.
//
// exemptTemplates selects the external-library form (`kalide init <path>`,
// cli-init-refuse-existing): when true, a pre-existing local templates/ is not
// a conflict — it is left untouched and ignored while the deck references the
// external library — while slides/, assets/ and kalide.yaml still block. When
// false (the no-arg form) templates/ blocks like every other entry.
func existingBlockPaths(dir string, exemptTemplates bool) ([]string, error) {
	var conflicts []string
	for _, block := range blockPaths {
		if exemptTemplates && block.path == "templates" {
			continue
		}
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
