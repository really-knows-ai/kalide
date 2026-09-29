package scaffold

// This file hosts the build/test-time catalog drift self-test for the shipped
// scaffold (global.constraint.upgrade-known-version-catalog) and the shared
// derivation scaffolding it runs on.
//
// The drift test exists so a kalide-owned scaffold file whose current bytes are
// NOT catalogued in scaffoldversions.json fails the gate: the catalogue is the
// known-version digest set the refresh half (phase 2) resolves against, and a
// shipped writer whose bytes are missing from it means an author's project
// could never be refreshed to the running kalide's version of that file. The
// check mirrors requirements.requirement.agent-guide-drift-check: it derives
// the checked set from the actual writers rather than from a hand-maintained
// list, so adding, renaming or dropping a shipped scaffold file cannot leave
// the catalogue silently stale.
//
// The checked set is derived from the real scaffolding writers, and is the
// same set requirements.constraint.upgrade-owned-scaffold-scope names:
//
//   - the embedded seed tree writeHelloSeed copies (seedRoot), walked exactly
//     as the writer walks it, plus the deck-root AGENTS.md it carries;
//   - the library-root AGENTS.md, read through mustReadLibraryGuide (the
//     accessor InitLibrary writes);
//   - the library's seeded themes/default/theme.css, returned by
//     DefaultThemeCSS() (the accessor InitLibrary writes).
//
// The seed walk's kalide.yaml is filtered out: it is the author-edited deck
// configuration, not a whole-file kalide-owned member, and upgrade never
// refreshes it (requirements.constraint.upgrade-owned-scaffold-scope).
//
// Every derived file is mapped to its catalogue identity — the catalogue keys
// owned files by identity ("deck:<path>" for the seed tree, "library:<path>"
// for the library-owned files), never by digest alone, so a digest legitimately
// shared by two owned files (the seed's templates/themes/default/theme.css and
// a library's themes/default/theme.css are byte-identical) is matched only
// under the identity it was catalogued under (catalogLookup).
//
// This file owns the derivation (upgrade/plan.phase-01.task-8); the assertion
// body that consumes it lives in TestCatalogCoversShippedScaffold below and is
// completed by upgrade/plan.phase-01.task-9.

import (
	"io/fs"
	"path"
	"slices"
	"sort"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// catalogIdentityDeckForm and catalogIdentityLibraryForm are the two owned-file
// identity namespaces scaffoldversions.json uses. A seed-tree file (rooted at
// a deck directory) is catalogued "deck:<path>"; a file InitLibrary writes is
// catalogued "library:<path>". They are schema constants of the catalogue, not
// a hand-maintained file list, so the derivation below can map a writer's
// output to its identity without restating the catalogue's contents.
const (
	catalogIdentityDeckForm    = "deck"
	catalogIdentityLibraryForm = "library"
)

// catalogOwnedIdentity builds a catalogue identity key from an owned form
// ("deck" or "library") and a slash-separated path relative to that form's
// root, matching the scaffoldversions.json schema ("deck:AGENTS.md",
// "library:themes/default/theme.css").
func catalogOwnedIdentity(form, slashPath string) string {
	return form + ":" + slashPath
}

// shippedScaffoldFile is one file the running kalide ships as part of the
// kalide-owned scaffold: the exact bytes its writer produces, and the
// catalogue identity those bytes must be catalogued under. origin names the
// writer that ships the file, so a drift failure points at the code to change
// rather than only at the identity.
type shippedScaffoldFile struct {
	identity string // catalogue identity, e.g. "deck:templates/library.yaml"
	origin   string // the scaffolding writer that ships these bytes
	data     []byte // the exact bytes the running binary writes
}

// derivedShippedScaffold returns every kalide-owned scaffold file the running
// binary ships, paired with its catalogue identity, derived from the actual
// writers rather than from a hand-written list.
//
// It is the single derivation the drift self-test consumes: the seed tree and
// its deck-root AGENTS.md come from the writeHelloSeed walk (derivedSeedScaffold),
// the library-root AGENTS.md from mustReadLibraryGuide, and the library's
// seeded themes/default/theme.css from DefaultThemeCSS(). The result is sorted
// by identity so iteration and any failure output are deterministic.
func derivedShippedScaffold(t *testing.T) []shippedScaffoldFile {
	t.Helper()

	files := derivedSeedScaffold(t)
	files = append(files,
		shippedScaffoldFile{
			identity: catalogOwnedIdentity(catalogIdentityLibraryForm, "AGENTS.md"),
			origin:   "mustReadLibraryGuide (InitLibrary library-root guide)",
			data:     mustReadLibraryGuide(),
		},
		shippedScaffoldFile{
			identity: catalogOwnedIdentity(catalogIdentityLibraryForm,
				path.Join(template.ThemesDir, theme.DefaultName, template.ThemeStylesheet)),
			origin: "DefaultThemeCSS (InitLibrary seeded default theme)",
			data:   []byte(DefaultThemeCSS()),
		},
	)

	sort.Slice(files, func(i, j int) bool { return files[i].identity < files[j].identity })
	return files
}

// derivedSeedScaffold walks the embedded seed tree exactly as writeHelloSeed
// does (fs.WalkDir over seedRoot, files only, relative slash paths) and returns
// each seed file paired with its "deck:<path>" catalogue identity. The seed's
// kalide.yaml is filtered out: it is the author-edited deck configuration, not
// a whole-file kalide-owned member (requirements.constraint.upgrade-owned-
// scaffold-scope), and it has no catalogue identity.
func derivedSeedScaffold(t *testing.T) []shippedScaffoldFile {
	t.Helper()

	var files []shippedScaffoldFile
	err := fs.WalkDir(seedRoot, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." || entry.IsDir() {
			return nil
		}
		if p == deck.ConfigFile {
			return nil
		}
		data, err := fs.ReadFile(seedRoot, p)
		if err != nil {
			return err
		}
		files = append(files, shippedScaffoldFile{
			identity: catalogOwnedIdentity(catalogIdentityDeckForm, p),
			origin:   "writeHelloSeed (embedded seed tree)",
			data:     data,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("derived seed scaffold: walk embedded seed tree: %v", err)
	}
	return files
}

// shippedScaffoldIdentities returns the distinct catalogue identities in
// files, sorted. It is the derivation's identity-set view, so the drift test
// can compare the catalogue's identity set against exactly the set the real
// writers ship.
func shippedScaffoldIdentities(files []shippedScaffoldFile) []string {
	identities := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if _, ok := seen[file.identity]; ok {
			continue
		}
		seen[file.identity] = struct{}{}
		identities = append(identities, file.identity)
	}
	slices.Sort(identities)
	return identities
}

// TestCatalogCoversShippedScaffold is the build/test-time catalog drift
// self-test (global.constraint.upgrade-known-version-catalog): it derives the
// checked shipped-scaffold set from the actual writers with
// derivedShippedScaffold and will assert that every current shipped file's
// SHA-256 is a catalogued entry for its own identity, that the catalogue's
// identity set equals exactly the derived set (no extra identity, notably NOT
// kalide.yaml, and none missing), and — as a negative self-check — that a
// mutated, uncatalogued byte slice is reported as uncatalogued so the gate
// cannot pass vacuously.
//
// The assertion body is implemented by upgrade/plan.phase-01.task-9; this file
// (task 8) provides the derivation and identity mapping it runs on.
func TestCatalogCoversShippedScaffold(t *testing.T) {
	files := derivedShippedScaffold(t)
	if len(files) == 0 {
		t.Fatal("derivedShippedScaffold returned no shipped scaffold files; the derivation is broken")
	}
	t.Skip("catalog drift assertions are implemented by upgrade/plan.phase-01.task-9")
}
