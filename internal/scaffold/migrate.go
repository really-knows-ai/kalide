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
// The rest of phase 3 fills in the pieces declared here:
//
//   - registeredMigrations kalide's ordered migration set (empty today).
//   - readLibraryFormat reads a library's raw `format:` integer tolerantly.
//   - implementedFormat reports the highest format the binary implements.
//     TODO(upgrade/plan.phase-03.task-5)
//   - unsupportedFormatError the unknown/newer refusal.
//     TODO(upgrade/plan.phase-03.task-6)
//   - migrateLibrary the orchestrator, delegating to the testable seam
//     migrateLibraryWith. TODO(upgrade/plan.phase-03.task-7)
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

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
// implements (today firstLibraryFormat, `1`). The comparison is on the format
// integer, never on the version string (domain.value.version): a
// `dev`/unstamped build implements the format of the source it was built from,
// so it never means "newer".
//
// TODO(upgrade/plan.phase-03.task-5): return the implemented format.
func implementedFormat() int {
	return 0
}

// unsupportedFormatError is the refusal for a library.yaml `format:` the binary
// does not recognize — unknown, or newer than implementedFormat. It names the
// format found and the format supported; the command exits non-zero and leaves
// the library's files byte-for-byte untouched. It is never guessed or silently
// migrated (global.constraint.upgrade-unknown-format-reported).
//
// TODO(upgrade/plan.phase-03.task-6): define the refusal error and its message.
type unsupportedFormatError struct {
	// found is the format integer read from the library's library.yaml.
	found int
	// supported is the highest format the running binary implements
	// (implementedFormat).
	supported int
}

// Error renders the refusal, naming the format found and the format supported.
//
// TODO(upgrade/plan.phase-03.task-6): render the author-facing message.
func (e *unsupportedFormatError) Error() string {
	return fmt.Sprintf("scaffold: unsupportedFormatError: not implemented yet (format found %d, format supported %d)", e.found, e.supported)
}

// migrateLibrary is the migration-half orchestrator: it applies kalide's
// registered migrations to the library in dir, from the library's current
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
// TODO(upgrade/plan.phase-03.task-7): implement the orchestrator. It delegates
// to migrateLibraryWith(dir, registeredMigrations()); production may only ever
// pass registeredMigrations(), which stays empty.
func migrateLibrary(dir string) error {
	return migrateLibraryWith(dir, registeredMigrations())
}

// migrateLibraryWith is migrateLibrary's testability seam: the orchestrator
// over an explicit migration set, so tests can drive ordering,
// at-most-once/idempotence, atomic rollback and per-migration validation
// failure with synthetic migrations (none exist today, because the registered
// set is empty). Production may only ever pass registeredMigrations(); only
// test files may use this seam, and no fake step is ever added to
// registeredMigrations() to make a test pass.
//
// TODO(upgrade/plan.phase-03.task-7): apply migs in order, each at most once,
// validating each result and rolling a failing multi-file migration back so
// every affected file is left as it was.
func migrateLibraryWith(dir string, migs []formatMigration) error {
	return fmt.Errorf("scaffold: migrateLibrary: not implemented yet (dir %s, %d migrations)", dir, len(migs))
}
