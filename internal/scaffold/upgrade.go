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
// only the post-refresh+migrate end-to-end pass and the refresh half's undo: it
// records the original bytes of every file it refreshes or writes and, when the
// end-to-end validation fails, restores those bytes and removes the files it
// created, leaving the project exactly as it was.
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
	"fmt"
	"strings"
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
// TODO(upgrade/plan.phase-04.task-3): implement the orchestration — run
// refreshScaffold then migrateLibrary, end-to-end validate the result after
// both halves (deck through kalide start's loader, library through the template
// loader), return the UpgradeReport, and, on a validation failure, restore every
// file the refresh half wrote (including removing a newly written AGENTS.md) so
// the project is left exactly as it was
// (global.constraint.upgrade-never-breaks-project,
// global.constraint.never-serve-broken-deck). The format check must run before
// refreshing — or the refresh half must be rolled back — so an
// unsupportedFormatError leaves no refresh write on disk
// (global.constraint.upgrade-unknown-format-reported). This skeleton only
// establishes detection.
func Upgrade(dir string) (*UpgradeReport, error) {
	kind, err := detectProject(dir)
	if err != nil {
		return nil, err
	}
	return newUpgradeReport(kind, nil), nil
}
