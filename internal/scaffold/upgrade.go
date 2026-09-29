// This file is the `kalide upgrade` orchestration entry point
// (requirements.requirement.deck-library-upgrade): the one place that brings an
// existing project in a directory — a deck (kalide.yaml) or a template library
// (library.yaml) — forward to the running binary's scaffold content and format
// in place, once, offline and conservatively
// (global.constraint.upgrade-offline).
//
// It houses the two phase-4 symbols the rest of the command surface consumes:
//
//   - UpgradeReport records the detected project form together with every path
//     Upgrade refreshed and every path it left alone (skipped), so the author
//     sees both rather than only the writes, and renders the report the
//     `kalide upgrade` command prints.
//   - Upgrade orchestrates the two halves and validates the result.
//
// Upgrade composes, and does not duplicate, the machinery the earlier phases
// own:
//
//   - detectProject (refresh.go) is THE SINGLE OWNER of deck/library detection
//     and of the both/neither error: a directory holding both manifests, or
//     neither, is a non-zero error naming what was found against what was
//     expected, with nothing written. Upgrade calls it and never re-implements
//     it or its error.
//   - refreshScaffold (refresh.go) is the never-clobber refresh half
//     (requirements.requirement.upgrade-refresh-owned-scaffold): it rewrites a
//     kalide-owned member only when it is present and byte-identical to a
//     catalogued released version of that same file, writes the two AGENTS.md
//     author guides when absent, never re-creates a deleted seed starter file,
//     and reports every refreshed and every skipped path.
//   - migrateLibrary (migrate.go) is the ordered, versioned format-migration
//     half (requirements.requirement.upgrade-migrate-format): a library's
//     library.yaml `format:` is carried forward through kalide's registered
//     migrations; an unknown or newer format is refused, unchanged and
//     non-zero, naming the format found and the format supported; a deck has no
//     version field and is a no-op on this half.
//
// After BOTH halves have run, Upgrade end-to-end validates the resulting
// project through the same loaders `kalide start` uses — the template library
// loader and the deck loader — so a project is never left broken
// (global.constraint.upgrade-never-breaks-project,
// global.constraint.never-serve-broken-deck). The migration half already owns
// per-migration validation and its own all-or-nothing rollback; Upgrade adds
// the post-refresh+migrate end-to-end pass and an undo that covers both halves.
// For a library it snapshots the whole project before either half runs and
// restores that snapshot when the end-to-end validation fails, so a migration
// that already committed is undone together with the refresh writes. For a deck
// — whose migration half is a no-op — it records the original bytes of every
// file the refresh half writes and, on failure, restores those bytes and removes
// the files it created. In both forms the project is left exactly as it was.
//
// Upgrade takes no command-line path and there is no --force: the conservative
// default is the only behaviour. It reads and writes only the project's own
// local files and the content the running binary already carries — it never
// reaches the network, never runs git and never opens an external-library
// deck's configured `templates:` library
// (global.constraint.upgrade-offline,
// requirements.constraint.upgrade-owned-scaffold-scope).
package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// UpgradeReport is the outcome of an Upgrade run: the detected project form
// (a deck or a template library) together with every project-root path Upgrade
// refreshed and every path it left alone (skipped). Both lists are reported so
// an author-edited file that was deliberately not overwritten is visible rather
// than silent (global.constraint.upgrade-never-clobbers).
//
// The path lists are the refresh half's outcome — refreshReport.refreshed and
// refreshReport.skipped, copied here in visit order (newUpgradeReport). The
// migration half is structural and reports no per-path results of its own
// (migrateLibrary returns only an error), so a run that migrates a library
// still reports the same refreshed/skipped paths; the detected Kind names the
// form the run worked on.
//
// String renders the report exactly as the `kalide upgrade` command prints it.
type UpgradeReport struct {
	// Kind names the project form detectProject found: "deck" for a
	// directory holding kalide.yaml, "library" for one holding library.yaml
	// (projectKind.String).
	Kind string
	// Refreshed names every project-root path Upgrade rewrote or wrote, in
	// visit order.
	Refreshed []string
	// Skipped names every project-root path Upgrade deliberately left alone
	// (already current, author content, or a deleted starter never
	// re-created), in visit order.
	Skipped []string
}

// newUpgradeReport assembles the report for a detected project from the refresh
// half's outcome: the kind detectProject found plus every path refreshScaffold
// refreshed and every path it left alone (refreshReport). The paths are copied
// in visit order, so the returned report owns its slices; a nil refresh report
// (detection only, nothing refreshed yet) yields a report with no paths.
func newUpgradeReport(kind projectKind, refresh *refreshReport) *UpgradeReport {
	report := &UpgradeReport{Kind: kind.String()}
	if refresh != nil {
		report.Refreshed = append(report.Refreshed, refresh.refreshed...)
		report.Skipped = append(report.Skipped, refresh.skipped...)
	}
	return report
}

// String renders the report as the `kalide upgrade` command prints it: a header
// naming the detected project form, then the refreshed paths and the left-alone
// (skipped) paths, one per line under a labelled heading. Both lists always
// render — an empty list shows "(none)" — so a file the refresh deliberately
// left untouched, in particular an author-edited one, is reported rather than
// silent (global.constraint.upgrade-never-clobbers). The returned text has no
// trailing newline.
func (r *UpgradeReport) String() string {
	lines := []string{fmt.Sprintf("kalide upgrade: %s", r.Kind)}
	lines = append(lines, "", fmt.Sprintf("refreshed (%d):", len(r.Refreshed)))
	lines = appendPathLines(lines, r.Refreshed)
	lines = append(lines, "", fmt.Sprintf("skipped (left alone) (%d):", len(r.Skipped)))
	lines = appendPathLines(lines, r.Skipped)
	return strings.Join(lines, "\n")
}

// appendPathLines appends one two-space-indented line per path, or a "(none)"
// placeholder when the list is empty, so a heading is never left bare.
func appendPathLines(lines, paths []string) []string {
	if len(paths) == 0 {
		return append(lines, "  (none)")
	}
	for _, path := range paths {
		lines = append(lines, "  "+path)
	}
	return lines
}

// String names the project form detectProject classified: "deck" for a
// directory holding kalide.yaml, "library" for one holding library.yaml, and
// "unknown" for the zero value — which detectProject reports as an error rather
// than as a project. It is how UpgradeReport renders the detected kind.
func (k projectKind) String() string {
	switch k {
	case projectKindDeck:
		return "deck"
	case projectKindLibrary:
		return "library"
	default:
		return "unknown"
	}
}

// Upgrade brings the project in dir forward to the running binary's scaffold
// content and format, then validates the result end-to-end. It detects the
// project form with detectProject (the single owner of deck/library detection),
// runs the refresh half (refreshScaffold) and the migration half
// (migrateLibrary), validates the resulting project through the loaders
// `kalide start` uses, and returns an UpgradeReport naming every refreshed and
// every skipped path (requirements.requirement.deck-library-upgrade).
//
// dir is the project root: the directory holding kalide.yaml for a deck or
// library.yaml for a library. The command surface passes the current directory;
// Upgrade itself takes no path argument and there is no --force.
//
// The order is conservative and the run is all-or-nothing: a failure after a
// half began undoes that work and leaves the project exactly as it was.
//
//  1. detectProject classifies the project. A directory holding both manifests,
//     or neither, is the single both/neither error and nothing is written.
//  2. For a library, the format is checked BEFORE refreshing
//     (checkSupportedLibraryFormat): an unknown or newer format is refused with
//     phase 3's unsupportedFormatError, returned unchanged, and NO refresh-half
//     write is left on disk — the refusal happens before the refresh engine
//     runs (global.constraint.upgrade-unknown-format-reported). A deck has no
//     version field, so there is nothing to check.
//  3. For a library, a snapshot of the whole project (snapshotTree) is taken
//     BEFORE either half runs, so a failure of the end-to-end validation can
//     restore the migration half's committed changes as well as the refresh
//     half's writes. A deck's migration half is a no-op, so its refresh undo
//     alone is sufficient and no tree snapshot is taken.
//  4. The refresh half's original bytes are recorded (newRefreshUndo) before it
//     runs, so every file it rewrites or writes — including a newly written
//     AGENTS.md — can be put back exactly as it was. A member's path is
//     recorded before its write is attempted, so a member truncated by a
//     mid-write failure is restored too.
//  5. refreshScaffold then migrateLibrary run, refresh first.
//  6. The result is validated end-to-end AFTER both halves through the loaders
//     `kalide start` uses: a library through the template library loader, a deck
//     through the template library loader, its theme registry and the whole-deck
//     validator (deck.LoadConfig included). This is distinct from phase 3's
//     per-migration validation, which the migration half already owns.
//  7. On any failure after a half began, that half's work is undone and the
//     project is left exactly as it was
//     (global.constraint.upgrade-never-breaks-project,
//     global.constraint.never-serve-broken-deck): a library's end-to-end
//     validation failure restores the pre-run tree snapshot (covering both
//     halves); the refresh half's own failure, or a deck's end-to-end failure,
//     restores the recorded original bytes, mode and mtime and removes created
//     files. A failure before either half began (detection, the format refusal)
//     has nothing to undo.
//
// It reads and writes only the project's own local files and the content the
// running binary already carries: no network, no git. End-to-end validation of
// an external-library deck loads its configured `templates:` library (read-only)
// exactly as `kalide start` does, but the refresh half never opens it.
//
// upgradeMigrate and upgradeValidate are the two package-level seams through
// which Upgrade reaches the migration half and the end-to-end validation.
// Production only ever uses their defaults (migrateLibrary and
// validateUpgradedProject); only test files replace them, and they never change
// what a production `kalide upgrade` does.
func Upgrade(dir string) (*UpgradeReport, error) {
	kind, err := detectProject(dir)
	if err != nil {
		return nil, err
	}

	// The format check runs before the refresh half: an unsupported format is
	// refused with nothing written, so no refresh-half write can be left on
	// disk, and the refusal is returned unchanged.
	if kind == projectKindLibrary {
		if err := checkSupportedLibraryFormat(dir); err != nil {
			return nil, err
		}
	}

	undo, err := newRefreshUndo(dir)
	if err != nil {
		return nil, err
	}

	// A library's migration half can commit changes the refresh half's undo does
	// not cover: runMigrations' snapshot is internal to migrateLibraryWith and is
	// discarded once a migration is kept. Snapshot the whole project before
	// either half runs so a failure of the end-to-end validation can restore
	// both halves together, never leaving a partially migrated library
	// (global.constraint.upgrade-never-breaks-project). A deck's migration half
	// is a no-op, so the refresh undo alone restores it.
	var before *treeSnapshot
	if kind == projectKindLibrary {
		before, err = snapshotTree(dir)
		if err != nil {
			return nil, fmt.Errorf("upgrade: snapshot %s before upgrading: %w", dir, err)
		}
	}

	report, err := refreshScaffold(dir, kind, nil)
	if err != nil {
		return nil, undo.revert(report.refreshed, err)
	}
	if err := upgradeMigrate(dir); err != nil {
		return nil, undo.revert(report.refreshed, err)
	}
	if err := upgradeValidate(dir, kind); err != nil {
		if before != nil {
			if restoreErr := before.restore(dir); restoreErr != nil {
				return nil, fmt.Errorf("%w (upgrade could not restore the project exactly: %v)", err, restoreErr)
			}
			return nil, err
		}
		return nil, undo.revert(report.refreshed, err)
	}
	return newUpgradeReport(kind, report), nil
}

// upgradeMigrate is the migration half Upgrade runs for a project. Production
// always runs migrateLibrary (the ordered registeredMigrations set). It is a
// seam so test files can drive migrateLibraryWith with synthetic migrations and
// a raised migrationTargetFormat, exercising Upgrade's end-to-end undo for a
// library whose migration half actually commits a change. Only test files
// replace it, and no fake migration is ever added to registeredMigrations()
// (TestRegisteredMigrationsStaysEmpty guards that).
var upgradeMigrate = migrateLibrary

// upgradeValidate is Upgrade's end-to-end validation (validateUpgradedProject).
// It is the validation seam for Upgrade's end-to-end undo: a library's
// per-migration validation (validateMigratedLibrary) already runs the same
// template library loader the end-to-end pass runs, so a genuinely migrated
// library cannot be newly unloadable through the real loader. Test files replace
// it to force the post-migration failure whose undo must restore both halves;
// production always runs validateUpgradedProject.
var upgradeValidate = validateUpgradedProject

// checkSupportedLibraryFormat is the read-only format guard Upgrade runs before
// refreshing a library. It reads the raw library.yaml `format:` tolerantly
// (readLibraryFormat) and compares it against the format the running binary
// implements (implementedFormat), using the same registered migrations the
// migration half would: a format greater than the implemented format, or one
// with no registered migration chain up to it, is an unknown format and is
// refused with phase 3's unsupportedFormatError naming the format found and the
// format supported (global.constraint.upgrade-unknown-format-reported). It
// writes nothing — it only reads library.yaml — so a refusal leaves the library
// byte-for-byte untouched and no refresh write behind.
func checkSupportedLibraryFormat(dir string) error {
	current, err := readLibraryFormat(dir)
	if err != nil {
		return err
	}
	supported := implementedFormat()
	if current > supported {
		return &unsupportedFormatError{found: current, supported: supported}
	}
	if _, ok := migrationChain(current, supported, registeredMigrations()); !ok {
		return &unsupportedFormatError{found: current, supported: supported}
	}
	return nil
}

// refreshUndo records, before the refresh half runs, the original state of every
// file the refresh half may rewrite or write, so a later failure can put the
// project back exactly as it was. It is the refresh half's undo, complementing
// the migration half's own tree snapshot and rollback: phase 3 restores its
// migrations all-or-nothing, and Upgrade restores the refresh writes on top.
//
// It records only the kalide-owned member paths the refresh engine can touch
// (the deck and library owned sets), not the whole tree, so author content
// outside those paths is never read or rewritten by the undo.
type refreshUndo struct {
	// root is the project root the member paths are relative to.
	root string
	// originals maps each candidate member path (slash-separated, relative to
	// root) to its pre-refresh state. A path absent from the map did not exist
	// and was not written before the refresh; a refreshed path with no entry
	// and no file on disk is a created file.
	originals map[string]undoOriginal
}

// undoOriginal is the recorded pre-refresh state of one member path: whether it
// existed, and, when it did, its bytes, permission bits and modification time.
type undoOriginal struct {
	existed bool
	data    []byte
	mode    fs.FileMode
	modTime time.Time
}

// newRefreshUndo records the pre-refresh state of every member path the refresh
// engine may write, for both project forms (a deck owns the seed set plus the
// deck guide, a library the guide and its seeded theme). Recording the union is
// cheap and harmless: a candidate the refresh half does not write is simply
// never asked to be restored (revert walks the refreshed paths). It reads only
// the local filesystem.
func newRefreshUndo(root string) (*refreshUndo, error) {
	undo := &refreshUndo{root: root, originals: make(map[string]undoOriginal)}
	for _, member := range append(append([]ownedMember{}, deckOwnedMembers...), libraryOwnedMembers...) {
		if _, seen := undo.originals[member.path]; seen {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(member.path))
		info, err := os.Stat(target)
		if errors.Is(err, fs.ErrNotExist) {
			undo.originals[member.path] = undoOriginal{}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("upgrade: inspect %s before refreshing: %w", member.path, err)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return nil, fmt.Errorf("upgrade: record %s before refreshing: %w", member.path, err)
		}
		undo.originals[member.path] = undoOriginal{
			existed: true,
			data:    data,
			mode:    info.Mode(),
			modTime: info.ModTime(),
		}
	}
	return undo, nil
}

// revert restores the original state of every path the refresh half reported as
// refreshed and returns cause unchanged when the restore succeeded: a file the
// refresh rewrote is put back byte-for-byte with its permission bits and
// modification time, and a file the refresh created (a newly written AGENTS.md)
// is removed. A failure to restore is reported together with cause rather than
// swallowed, so the caller never believes the project was restored when it was
// not. It writes only the local filesystem.
func (u *refreshUndo) revert(refreshed []string, cause error) error {
	var failures []string
	for _, path := range refreshed {
		original, ok := u.originals[path]
		if !ok {
			// The refresh wrote a path outside the owned set it may touch.
			// Nothing was recorded for it; report rather than silently leave
			// the write behind.
			failures = append(failures, fmt.Sprintf("%s (not recorded before the refresh)", path))
			continue
		}
		target := filepath.Join(u.root, filepath.FromSlash(path))
		if !original.existed {
			if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
				failures = append(failures, fmt.Sprintf("remove %s: %v", path, err))
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			failures = append(failures, fmt.Sprintf("create %s: %v", filepath.ToSlash(filepath.Dir(target)), err))
			continue
		}
		if err := os.WriteFile(target, original.data, original.mode.Perm()); err != nil {
			failures = append(failures, fmt.Sprintf("write %s: %v", path, err))
			continue
		}
		if err := os.Chmod(target, original.mode.Perm()); err != nil {
			failures = append(failures, fmt.Sprintf("chmod %s: %v", path, err))
			continue
		}
		if err := os.Chtimes(target, original.modTime, original.modTime); err != nil {
			failures = append(failures, fmt.Sprintf("restore mtime of %s: %v", path, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%w (upgrade could not restore the project exactly: %s)", cause, strings.Join(failures, "; "))
	}
	return cause
}

// validateUpgradedProject is Upgrade's end-to-end validation, run AFTER both
// halves: it reloads the resulting project through the same loaders `kalide
// start` uses, so a project that would not serve is never reported as upgraded
// (global.constraint.never-serve-broken-deck,
// global.constraint.upgrade-never-breaks-project). It is distinct from phase 3's
// per-migration validation, which the migration half owns and Upgrade does not
// repeat.
//
// A library is a template root itself, so it is validated through the template
// library loader (template.LoadDeckLibrary at "."). A deck is validated exactly
// as `kalide start` validates before serving: its template library resolves and
// loads, its theme registry builds, and the whole deck validates through
// validate.Validate (which loads kalide.yaml through deck.LoadConfig). Loading
// an external-library deck's configured `templates:` library is read-only and
// mirrors start; the refresh half never opens it.
func validateUpgradedProject(dir string, kind projectKind) error {
	switch kind {
	case projectKindLibrary:
		if _, err := template.LoadDeckLibrary(dir, "."); err != nil {
			return fmt.Errorf("upgrade: validation failed: upgraded library %s does not load: %w", dir, err)
		}
		return nil
	case projectKindDeck:
		if err := validateUpgradedDeck(dir); err != nil {
			return fmt.Errorf("upgrade: validation failed: upgraded deck %s: %w", dir, err)
		}
		return nil
	default:
		return fmt.Errorf("upgrade: validation failed: %s: no project form was detected", dir)
	}
}

// validateUpgradedDeck reloads a refreshed deck through the same pipeline
// `kalide start` runs before serving: resolve and load the deck's template
// library (template.LoadDeckLibrary, honouring a configured `templates:` path),
// build the theme registry from the resolved library (theme.LoadDir), build the
// template registry (template.NewRegistryFromLibrary), and validate the whole
// deck (validate.Validate, including deck.LoadConfig). It reads the local files
// and, for an external-library deck, the configured library read-only.
func validateUpgradedDeck(dir string) error {
	cfg, err := refreshDeckConfig(dir, nil)
	if err != nil {
		return err
	}
	library, err := template.LoadDeckLibrary(dir, deck.TemplatesPath(cfg))
	if err != nil {
		return err
	}
	themes, err := theme.LoadDir(os.DirFS(library.RootPath), template.ThemesDir)
	if err != nil {
		return err
	}
	registry, err := template.NewRegistryFromLibrary(library)
	if err != nil {
		return err
	}
	if verr, invalid := validate.Validate(os.DirFS(dir), registry, themes); invalid {
		return fmt.Errorf("deck does not validate: %s", validate.Format(verr))
	}
	return nil
}
