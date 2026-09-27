// This file implements InitExternal: the external-library form of
// `kalide init` (requirements.requirement.cli-init,
// requirements.requirement.external-template-library).
//
// `kalide init <path>` points a new deck at an existing template library
// instead of copying the embedded hello seed into a local templates/. It
// validates the library at path AND the deck's theme (optional, default
// `default`) fail-fast — before writing anything — and then writes
// kalide.yaml with `templates: <path>`, an empty slides/ (NO starter slide)
// and an empty assets/. It writes no local templates/: the library is the one
// at path (requirements.requirement.cli-init-minimal-seed).
//
// Validation reuses the loader's single root-resolution point and its
// templates-dir-validation checks (template.LoadDeckLibrary), which resolve a
// relative path against the deck root and use an absolute path as given. The
// deck's theme is resolved against that same library through the theme
// registry (theme.LoadDir + Registry.Lookup), mirroring `kalide start`
// (requirements.requirement.theme-selection): a library that is load-valid
// but does not define the deck's theme cannot host the deck, so it is refused
// and nothing is written. InitExternal is filesystem-only and never reaches
// the network (requirements.requirement.single-binary).
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// externalDeckTitle is the title written into the kalide.yaml of a deck
// created by `kalide init <path>`, matching the no-arg seed's title. The title
// is required by deck-config; the author edits it afterwards.
const externalDeckTitle = "My presentation"

// externalConfig is the subset of kalide.yaml `kalide init <path>` writes: the
// required title and the external library path. It is marshalled through yaml
// so a path that needs quoting (one containing `: ` or `#`, say) is written
// safely, and the path is stored exactly as given.
type externalConfig struct {
	Title     string `yaml:"title"`
	Templates string `yaml:"templates"`
}

// InitExternal writes an external-library deck into dir: kalide.yaml carrying
// `templates: externalPath`, an empty slides/ and an empty assets/, and no
// local templates/ (external-template-library, cli-init). No starter slide is
// written.
//
// externalPath is the template library path exactly as it should appear in
// kalide.yaml: a relative path is resolved against dir (the deck root) for
// validation and an absolute path is used as given. Before writing anything,
// InitExternal refuses when slides/, assets/ or kalide.yaml already exists in
// dir (a pre-existing local templates/ is exempt: it is ignored and left
// untouched, cli-init-refuse-existing), then validates the library at
// externalPath and confirms the deck's theme (default `default`) resolves in
// it. A missing or invalid library, or an unresolvable theme, aborts with a
// non-zero error naming the path and writes nothing.
//
// dir may be relative or absolute and need not exist yet; it is created as
// needed. InitExternal is deterministic and uses only the local filesystem: it
// never reaches the network.
func InitExternal(dir, externalPath string) error {
	if externalPath == "" {
		return fmt.Errorf("init: external library path is empty")
	}

	// Refuse (writing nothing) when a deck-owned path is already present. An
	// existing local templates/ is exempt for the external form: the deck
	// references externalPath and the local directory is left untouched.
	conflicts, err := existingBlockPaths(dir, true)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return blockRefusal(conflicts)
	}

	// Validate the external library AND the deck's default theme fail-fast,
	// before any write. LoadDeckLibrary resolves externalPath against dir and
	// runs the full templates-dir-validation checks at the resolved root; a
	// configured path never falls back to a local templates/.
	library, err := template.LoadDeckLibrary(dir, externalPath)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	themes, err := theme.LoadDir(os.DirFS(library.RootPath), template.ThemesDir)
	if err != nil {
		return fmt.Errorf("init: external template library %s: %w", externalPath, err)
	}
	if _, err := themes.Lookup(theme.DefaultName); err != nil {
		return fmt.Errorf("init: external template library %s: %w", externalPath, err)
	}

	// Write the deck: kalide.yaml, an empty slides/ and an empty assets/. The
	// external library is referenced by path, so there is no local templates/
	// and no starter slide.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("init: create %s: %w", dir, err)
	}
	data, err := yaml.Marshal(externalConfig{Title: externalDeckTitle, Templates: externalPath})
	if err != nil {
		return fmt.Errorf("init: write kalide.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kalide.yaml"), data, 0o644); err != nil {
		return fmt.Errorf("init: write kalide.yaml: %w", err)
	}
	for _, name := range []string{"slides", "assets"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			return fmt.Errorf("init: create %s/: %w", name, err)
		}
	}
	return nil
}
