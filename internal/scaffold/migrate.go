// This file is the migration half of `kalide upgrade`: the part that carries a
// project's structural/format version forward across kalide versions through an
// ordered, versioned, kalide-authored migration registry
// (requirements.requirement.upgrade-migrate-format,
// requirements.constraint.upgrade-migrations-ordered-atomic).
//
// The format anchors are:
//
//   - the project-side anchor, a template library's required library.yaml
//     `format:` integer (domain.value.library-manifest; only `1` has ever been
//     accepted, so `1` is the only format in existence —
//     internal/template.loadLibraryMeta rejects any other value), read
//     tolerantly by readLibraryFormat so an unknown or newer format can be
//     named (found) before the loader itself would reject it; and
//   - the binary-side anchor, the highest library.yaml format the running
//     binary implements, reported by implementedFormat (today `1`).
//     "Newer" is defined on the format integer, never on the version string
//     (domain.value.version), so a `dev`/unstamped build implements the format
//     of the source it was built from and never means "newer".
//
// A migration is one ordered, versioned step kalide authors
// (domain.value.format-migration): adding one is a kalide change and is NEVER
// inferred from a project's content. Upgrade applies the registered migrations
// from the project's current format to implementedFormat, in order, each at
// most once, preserving the author's values and changing only the
// format/structure a step governs. The registered set is EMPTY today — no
// format older than `1` exists, and a deck's kalide.yaml carries no version
// field at all (its key set is title, author, date, theme, navigation,
// properties, templates; requirements.note.upgrade-deck-vs-library) — so a
// format-1 library and every deck are a byte-for-byte no-op on the migration
// half and no migration is invented from content.
//
// A format the binary does not recognize — unknown, or greater than
// implementedFormat — is REFUSED, never guessed or silently migrated:
// unsupportedFormatError names the format found and the format supported, the
// command exits non-zero, and the library's files are left byte-for-byte
// untouched (global.constraint.upgrade-unknown-format-reported). A migration's
// result is validated before it is kept (a library re-loads through the
// template library loader; a deck through the deck loader only when a
// migration actually ran) and a multi-file migration is all-or-nothing: a
// failing step rolls back so every file it would have written is left as it
// was (global.constraint.upgrade-never-breaks-project). The half reads only
// the project's local files and the content the running binary already
// carries: it never reaches the network and never runs git
// (global.constraint.upgrade-offline).
//
// The pieces are declared here:
//
//   - registeredMigrations kalide's ordered migration set (empty today).
//   - readLibraryFormat reads a library's raw `format:` integer tolerantly.
//   - implementedFormat reports the highest format the binary implements.
//   - unsupportedFormatError the unknown/newer refusal.
//   - migrateLibrary the orchestrator, delegating to the testable seam
//     migrateLibraryWith.
package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/template"
)

// libraryFormatKey is the library.yaml key holding the format integer — the
// project-side format anchor (domain.value.library-manifest). readLibraryFormat
// reads it directly, without going through the loader, so a format the loader
// would reject can still be named.
const libraryFormatKey = "format"

// firstLibraryFormat is the first — and, today, only — library.yaml format that
// has ever existed: internal/template.loadLibraryMeta has accepted only `1`, so
// there is no format older than `1` and no migration to register. It is the
// floor the ordered registry walks from and today's implementedFormat.
const firstLibraryFormat = 1

// formatMigration is one ordered, versioned, kalide-defined migration step: its
// source format integer (the library.yaml `format:` it upgrades FROM) plus the
// transformation it applies to carry the project from that source format to the
// next. It is the element of the finite registry registeredMigrations returns;
// adding a migration is a kalide change and is never inferred from a project's
// content (requirements.constraint.upgrade-migrations-ordered-atomic,
// domain.value.format-migration).
//
// A step preserves the author's values (a library's name/description) and
// changes only the format/structure it governs; its result is validated before
// it is kept (requirements.requirement.upgrade-migrate-format).
type formatMigration struct {
	// sourceFormat is the library.yaml `format:` this step upgrades FROM. The
	// registry is consumed from the project's current format to
	// implementedFormat; each step applies at most once.
	sourceFormat int
	// apply rewrites the library's local files from sourceFormat forward. A
	// multi-file migration is all-or-nothing: a failure must leave every file
	// it would have written as it was
	// (global.constraint.upgrade-never-breaks-project).
	apply func(dir string) error
}

// registeredMigrations returns kalide's ordered migration set: every
// kalide-authored step, in format order, consumed from a project's current
// format to implementedFormat, each applied at most once. The set is EMPTY
// today, and that is intentional: there is no format older than
// firstLibraryFormat (loadLibraryMeta has only ever accepted `format: 1`, so
// no library can be at an older format), and a deck's kalide.yaml carries no
// version field at all. Consuming the empty set therefore leaves a format-1
// library and every deck byte-for-byte unchanged and invents no migration from
// content (requirements.requirement.upgrade-migrate-format,
// requirements.constraint.upgrade-migrations-ordered-atomic).
//
// This set is KALIDE-AUTHORED and finite: adding a step is a kalide source
// change that ships with the corresponding format bump, one format to the next
// (domain.value.format-migration). It must NEVER be populated from a project's
// content — no migration is inferred from what a project happens to contain.
//
// Production may only ever pass this set; it stays empty until kalide ships a
// newer format. Ordering, at-most-once/idempotence, atomic rollback and
// per-migration validation are exercised through the migrateLibraryWith seam,
// never by adding a synthetic step here: only test files may supply migrations,
// and they do so through that seam.
func registeredMigrations() []formatMigration {
	return nil
}

// readLibraryFormat reads the raw library.yaml `format:` integer from dir
// tolerantly and independently of internal/template's loader, so an unknown or
// newer format can be named (found) before template.loadLibraryMeta would
// reject it (global.constraint.upgrade-unknown-format-reported). It is
// local-filesystem only: no network and no git
// (global.constraint.upgrade-offline).
//
// Tolerant means it reads only the `format:` key and imposes none of the
// loader's other constraints: unknown keys, a missing name and any other
// content are ignored, and the integer is returned unchanged — including a
// value (0, 2, …) that loadLibraryMeta would reject as unsupported. The
// caller decides what is supported by comparing against implementedFormat.
//
// A missing library.yaml, a document that is not a mapping, a non-integer
// `format:` value and an absent `format:` key are each a clear error naming
// the offending file (and, where one exists, the offending key).
func readLibraryFormat(dir string) (int, error) {
	path := filepath.Join(dir, template.LibraryFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("scaffold: read library format: %s not found", path)
		}
		return 0, fmt.Errorf("scaffold: read library format: read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return 0, fmt.Errorf("scaffold: read library format: parse %s: %w", path, err)
	}

	// Unwrap the document node to its root mapping, tolerating an empty file.
	mapping := &doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			mapping = nil
		} else {
			mapping = doc.Content[0]
		}
	}
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return 0, fmt.Errorf("scaffold: read library format: %s: expected a mapping of %s keys", path, template.LibraryFile)
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		val := mapping.Content[i+1]
		if key.Value != libraryFormatKey {
			continue
		}
		if val.Kind != yaml.ScalarNode || val.ShortTag() != "!!int" {
			return 0, fmt.Errorf("scaffold: read library format: %s: key %q is not an integer", path, libraryFormatKey)
		}
		var format int
		if err := val.Decode(&format); err != nil {
			return 0, fmt.Errorf("scaffold: read library format: %s: key %q: %w", path, libraryFormatKey, err)
		}
		return format, nil
	}
	return 0, fmt.Errorf("scaffold: read library format: %s: missing required key %q", path, libraryFormatKey)
}

// implementedFormat returns the highest library.yaml format the running binary
// implements — today firstLibraryFormat, `1`. It is the single source of truth
// for the running format: the binary-side anchor a project's current format is
// compared against, and the upper bound migrateLibrary applies the registered
// migrations up to.
//
// The comparison is on the format integer, never on the version string
// (domain.value.version). A `dev`/unstamped build implements the format of the
// source it was built from, exactly like a tagged build of that same source, so
// it never counts as "newer": an unstamped binary reports the format its source
// implements, and a library at that same format is current, not ahead. What
// makes a format "newer" is solely the integer exceeding this value — which is
// why this returns firstLibraryFormat rather than reading a version.
func implementedFormat() int {
	return firstLibraryFormat
}

// unsupportedFormatError is the refusal for a library.yaml `format:` the binary
// does not recognize — unknown, or newer than implementedFormat. It names the
// format found and the format supported; the command exits non-zero and leaves
// the library's files byte-for-byte untouched. It is never guessed or silently
// migrated (global.constraint.upgrade-unknown-format-reported).
//
// The refusal is a read-only value: it carries no filesystem state and writes
// nothing, so a caller (phase-4 Upgrade) returns it unchanged as the non-zero
// refusal and the library's files stay byte-for-byte as they were.
type unsupportedFormatError struct {
	// found is the format integer read from the library's library.yaml.
	found int
	// supported is the highest format the running binary implements
	// (implementedFormat).
	supported int
}

// Error renders the refusal, naming the format found and the format supported
// (implementedFormat), so the command's non-zero report is self-contained and
// the author learns both what the library declared and what this binary can
// carry forward (global.constraint.upgrade-unknown-format-reported).
func (e *unsupportedFormatError) Error() string {
	return fmt.Sprintf("scaffold: unsupported library format: found format %d, supported format %d", e.found, e.supported)
}

// migrationTargetFormat reports the upper bound migrateLibraryWith applies the
// migration chain to — the highest format the running binary implements
// (implementedFormat). It is the var-shaped companion of the migrateLibraryWith
// seam: the registered set is empty and implementedFormat is 1, so a test can
// raise the target to drive more than the single step a format-1 binary
// affords and exercise multi-step ordering, idempotence, rollback and
// per-migration validation through that seam. Production only ever uses the
// default, set here once; only test files replace it, and they restore it
// afterwards. It never changes what the binary implements: implementedFormat is
// still the single source of truth for the running format and the "newer"
// comparison.
var migrationTargetFormat = implementedFormat

// migrateLibrary is the migration-half orchestrator: it applies kalide's
// registered migrations to the project in dir, from the project's current
// format to implementedFormat, in order, each at most once. It preserves the
// author's values and changes only the format/structure a migration governs,
// and validates each result before it is kept; a format the binary does not
// recognize is refused with unsupportedFormatError rather than guessed or
// silently migrated. A deck (kalide.yaml) has no version field, so the
// migration half is a no-op for a deck and nothing is inferred from a deck's
// content; the registered set is empty today, so a format-1 library is
// likewise a byte-for-byte no-op
// (requirements.requirement.upgrade-migrate-format,
// global.constraint.upgrade-unknown-format-reported). It is local-filesystem
// only: no network and no git (global.constraint.upgrade-offline).
//
// It delegates to migrateLibraryWith(dir, registeredMigrations()); production
// may only ever pass registeredMigrations(), which stays empty.
func migrateLibrary(dir string) error {
	return migrateLibraryWith(dir, registeredMigrations())
}

// migrateLibraryWith is migrateLibrary's testability seam: the orchestrator
// over an explicit migration set, so tests can drive ordering,
// at-most-once/idempotence, atomic rollback and per-migration validation
// failure with synthetic migrations (none exist today, because the registered
// set is empty). Production may only ever pass registeredMigrations(); only
// test files may use this seam, and no fake step is ever added to
// registeredMigrations() to make a test pass. Tests may also raise
// migrationTargetFormat to run a chain longer than implementedFormat affords.
//
// It first establishes the project form through detectProject, the single
// owner of deck/library detection. A deck's kalide.yaml has no version field,
// so the migration half is a no-op for a deck: no format is read, nothing is
// written, and no deck version is inferred
// (requirements.note.upgrade-deck-vs-library). For a template library it reads
// the raw format (readLibraryFormat) and applies the ordered chain of
// migrations from that format to migrationTargetFormat(), each validated
// through the template library loader before it is kept. A migration that
// fails — in its own apply or in validation — rolls the whole run back so
// every affected file is byte-for-byte as it was
// (requirements.constraint.upgrade-migrations-ordered-atomic,
// global.constraint.upgrade-never-breaks-project).
func migrateLibraryWith(dir string, migs []formatMigration) error {
	kind, err := detectProject(dir)
	if err != nil {
		return err
	}
	if kind != projectKindLibrary {
		// A deck has no version field: the migration half is a no-op. It
		// reads no format and writes nothing, and no deck version is
		// inferred from the deck's content. Per-migration result validation
		// through deck.LoadConfig therefore never runs for a deck: no deck
		// migration can exist to produce a result to validate until a deck
		// carries a version anchor.
		return nil
	}

	current, err := readLibraryFormat(dir)
	if err != nil {
		return err
	}
	target := migrationTargetFormat()

	// Newer than the running binary: never guessed, never migrated.
	if current > target {
		return &unsupportedFormatError{found: current, supported: target}
	}
	// Already current (today, format 1): a byte-for-byte no-op.
	if current == target {
		return nil
	}

	// The ordered chain from current to target. A gap — an unknown format
	// with no registered migration — is an unrecognized format.
	steps, ok := migrationChain(current, target, migs)
	if !ok {
		return &unsupportedFormatError{found: current, supported: target}
	}
	return runMigrations(dir, steps)
}

// runMigrations applies steps in order, each at most once, validating each
// result through the template library loader before it is kept. The whole run
// is all-or-nothing: the tree is recorded first and restored byte-for-byte if
// any step's apply or validation fails, so a failed run leaves every affected
// file exactly as it was and never leaves a project partially migrated
// (requirements.constraint.upgrade-migrations-ordered-atomic,
// global.constraint.upgrade-never-breaks-project). It is local-filesystem
// only.
func runMigrations(dir string, steps []formatMigration) error {
	before, err := snapshotTree(dir)
	if err != nil {
		return fmt.Errorf("scaffold: migrate library at %s: snapshot before migrating: %w", dir, err)
	}
	for _, step := range steps {
		if err := step.apply(dir); err != nil {
			return rollbackMigration(dir, before, step.sourceFormat, err)
		}
		if err := validateMigratedLibrary(dir); err != nil {
			return rollbackMigration(dir, before, step.sourceFormat, err)
		}
	}
	return nil
}

// migrationChain returns the migrations to apply, in format order, to carry a
// library from current to target: one registered step per format, each applied
// at most once. A registered migration carries the format it upgrades FROM and,
// by construction, carries the project to the next format, so the chain walks
// current, current+1, …, target-1 in that order regardless of the order the
// registry lists them in. ok is false when a format in that range has no
// registered migration (an unknown format that cannot reach target) or the
// registry declares two steps for the same source format (ambiguous, so
// neither is applied).
func migrationChain(current, target int, migs []formatMigration) ([]formatMigration, bool) {
	bySource := make(map[int]formatMigration, len(migs))
	for _, m := range migs {
		if _, dup := bySource[m.sourceFormat]; dup {
			return nil, false
		}
		bySource[m.sourceFormat] = m
	}
	steps := make([]formatMigration, 0, target-current)
	for f := current; f < target; f++ {
		m, ok := bySource[f]
		if !ok {
			return nil, false
		}
		steps = append(steps, m)
	}
	return steps, true
}

// validateMigratedLibrary validates a migrated library's result before it is
// kept: the library re-loads through the template library loader, resolving
// the library root as dir itself. loadLibraryMeta must accept every format the
// migrator reads, so a migration whose result the loader cannot load aborts the
// run rather than being kept
// (requirements.requirement.upgrade-migrate-format). The deck form is not
// validated here: a deck has no version anchor, so no deck migration runs and
// no deck result exists to validate.
func validateMigratedLibrary(dir string) error {
	if _, err := template.LoadDeckLibrary(dir, "."); err != nil {
		return fmt.Errorf("migrated library at %s does not load: %w", dir, err)
	}
	return nil
}

// rollbackMigration restores the pre-run tree after a migration step failed,
// wrapping the failure so the caller learns which source format failed and
// whether the rollback itself succeeded. A rollback failure is reported
// together with the original failure rather than swallowed.
func rollbackMigration(dir string, before *treeSnapshot, sourceFormat int, cause error) error {
	if err := before.restore(dir); err != nil {
		return fmt.Errorf("scaffold: migrate library from format %d: %w (restoring the project failed: %v)",
			sourceFormat, cause, err)
	}
	return fmt.Errorf("scaffold: migrate library from format %d: %w", sourceFormat, cause)
}

// treeSnapshot is the in-memory record of every entry under a project root,
// taken before a migration runs. It is the undo for the migration half: on any
// failure the run is restored byte-for-byte, so a failed migration leaves every
// file it would have written exactly as it was and never leaves a project
// partially migrated (global.constraint.upgrade-never-breaks-project).
//
// Directories and regular files carry their permission bits; a symlink carries
// its target. The root directory itself is not recorded: a migration governs
// the project's files and structure, never the permissions of the project root.
type treeSnapshot struct {
	dirs  map[string]fs.FileMode
	files map[string]snapshotFile
}

// snapshotFile is one recorded non-directory entry: a regular file's bytes and
// mode, or a symlink's target and mode.
type snapshotFile struct {
	data []byte
	mode fs.FileMode
	link string
}

// snapshotTree records every entry under root, keyed by its path relative to
// root. It reads only the local filesystem.
func snapshotTree(root string) (*treeSnapshot, error) {
	s := &treeSnapshot{
		dirs:  make(map[string]fs.FileMode),
		files: make(map[string]snapshotFile),
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			s.dirs[rel] = info.Mode()
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			s.files[rel] = snapshotFile{mode: info.Mode(), link: link}
		case info.Mode().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			s.files[rel] = snapshotFile{mode: info.Mode(), data: data}
		default:
			s.files[rel] = snapshotFile{mode: info.Mode()}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// restore returns the tree under root to the recorded state: everything a
// migration created is removed, then every recorded directory and file is
// written back with its original permission bits and symlinks are re-linked. It
// writes only the local filesystem.
func (s *treeSnapshot) restore(root string) error {
	// Drop everything currently under root; the snapshot is then the only
	// record and can be replayed cleanly. The root directory is kept.
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			return err
		}
	}

	// Directories first, parents before children.
	for _, rel := range shallowestFirst(s.dirs) {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, s.dirs[rel].Perm()); err != nil {
			return err
		}
		if err := os.Chmod(dir, s.dirs[rel].Perm()); err != nil {
			return err
		}
	}

	// Then every recorded file.
	for rel, f := range s.files {
		target := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if f.mode&fs.ModeSymlink != 0 {
			if err := os.Symlink(f.link, target); err != nil {
				return err
			}
			continue
		}
		if err := os.WriteFile(target, f.data, f.mode.Perm()); err != nil {
			return err
		}
		if err := os.Chmod(target, f.mode.Perm()); err != nil {
			return err
		}
	}
	return nil
}

// shallowestFirst returns the recorded directory paths with fewer path elements
// before those with more, so a parent is always created before its children.
func shallowestFirst(dirs map[string]fs.FileMode) []string {
	rels := make([]string, 0, len(dirs))
	for rel := range dirs {
		rels = append(rels, rel)
	}
	sort.Slice(rels, func(i, j int) bool {
		di := strings.Count(rels[i], string(filepath.Separator))
		dj := strings.Count(rels[j], string(filepath.Separator))
		if di != dj {
			return di < dj
		}
		return rels[i] < rels[j]
	})
	return rels
}
