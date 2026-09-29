package scaffold

// This file hosts the unit tests for the migration half of `kalide upgrade`
// (internal/scaffold/migrate.go): readLibraryFormat and implementedFormat, the
// format anchors; unsupportedFormatError, the unknown/newer refusal; and
// migrateLibrary, the orchestrator that applies kalide's ordered migration set
// through the migrateLibraryWith testability seam
// (requirements.requirement.upgrade-migrate-format,
// requirements.constraint.upgrade-migrations-ordered-atomic,
// global.constraint.upgrade-unknown-format-reported,
// global.constraint.upgrade-never-breaks-project).
//
// The cases are unit tier: every fixture is built in a t.TempDir, the tests
// reach no network, no server and no git, and they run under -short. The
// integration aggregate TestMigrateLibrary (upgrade/plan.phase-03.task-9) is int
// tier and re-validates a migration's result through the real loaders over a
// real library; it is added to this same file and reuses the fixtures below.
//
// The registered migration set is EMPTY and must stay empty, so the only way to
// exercise ordering, at-most-once/idempotence, atomic rollback and per-migration
// validation is the migrateLibraryWith seam declared by task-7: the tests supply
// synthetic formatMigration values through it and never add a fake migration to
// registeredMigrations() (TestRegisteredMigrationsStaysEmpty guards that). The
// tests never touch production data either: migrationTargetFormat is raised for
// one subtest and restored with t.Cleanup.
//
// The synthetic steps cannot advance library.yaml's `format:` themselves: the
// template loader only accepts format 1
// (internal/template.loadLibraryMeta), so validateMigratedLibrary would reject a
// raised format and the run would roll back. The seam tests therefore drive the
// ordered chain with steps that leave the format anchor untouched, exercising the
// orchestrator's ordering, at-most-once and rollback/validation-failure paths; a
// step that leaves the tree byte-identical on a second run demonstrates
// idempotence, and the production format-1 no-op is asserted directly.
//
// snapshot (scaffold_test.go) is reused for byte-for-byte rollback comparisons —
// rollback restores bytes, not mtimes. refreshFingerprint and
// refreshAssertTreeUntouched (refresh_test.go) are reused where the claim is
// "nothing was written at all", a pinned-mtime observation stronger than equal
// bytes.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
)

// migrateLibraryFixture creates a fresh, valid format-1 template library with
// scaffold.InitLibrary and returns its root. The base name must be a valid
// library name, and t.TempDir's base name is not, so the library lives in a
// "shared-lib" subdirectory. The library ships library.yaml (format 1), the
// standard layout directories, the seeded default theme and the library-root
// AGENTS.md, so it loads cleanly through template.LoadDeckLibrary.
func migrateLibraryFixture(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "shared-lib")
	if err := InitLibrary(dir); err != nil {
		t.Fatalf("InitLibrary(%s) error = %v, want a fresh format-1 library", dir, err)
	}
	return dir
}

// migrateDeckFixture creates a fresh no-arg seed deck with scaffold.Init and
// returns its root.
func migrateDeckFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init(%s) error = %v, want a fresh seed deck", dir, err)
	}
	return dir
}

// migrateSetTarget raises migrationTargetFormat for the calling (sub)test and
// restores the production value on cleanup. It is the var-shaped companion of
// the migrateLibraryWith seam: only test files replace it, and only for the
// duration of one (sub)test.
func migrateSetTarget(t *testing.T, target int) {
	t.Helper()

	original := migrationTargetFormat
	migrationTargetFormat = func() int { return target }
	t.Cleanup(func() { migrationTargetFormat = original })
}

// migrateRawLibrary writes a minimal library.yaml declaring format (with a
// valid name) plus one author file, and returns the directory. It is enough for
// detectProject to classify the directory as a library and for
// readLibraryFormat to read any integer — including a value (0, 2) that the
// real loader would reject — so the refusal can be exercised before validation.
func migrateRawLibrary(t *testing.T, format int) string {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, template.LibraryFile),
		fmt.Sprintf("name: shared-lib\nformat: %d\n", format))
	writeFile(t, filepath.Join(dir, "author.txt"), "author content\n")
	return dir
}

// migrateAssertRefused asserts err is the unknown/newer refusal for wantFound,
// naming the format found and the format supported
// (global.constraint.upgrade-unknown-format-reported).
func migrateAssertRefused(t *testing.T, err error, wantFound, wantSupported int) {
	t.Helper()

	var refused *unsupportedFormatError
	if !errors.As(err, &refused) {
		t.Fatalf("error = %v, want *unsupportedFormatError", err)
	}
	if refused.found != wantFound || refused.supported != wantSupported {
		t.Errorf("unsupportedFormatError = {found %d, supported %d}, want {found %d, supported %d}",
			refused.found, refused.supported, wantFound, wantSupported)
	}
	msg := err.Error()
	for _, want := range []string{
		fmt.Sprintf("found format %d", wantFound),
		fmt.Sprintf("supported format %d", wantSupported),
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("unsupportedFormatError.Error() = %q, want it to contain %q", msg, want)
		}
	}
}

// migrateAssertSameTree asserts root still holds exactly before, byte-for-byte.
// It compares contents only, because a rollback restores a file's bytes but not
// its mtime (global.constraint.upgrade-never-breaks-project).
func migrateAssertSameTree(t *testing.T, root string, before map[string]string) {
	t.Helper()

	if after := snapshot(t, root); !reflect.DeepEqual(after, before) {
		t.Errorf("tree %s changed:\n before %v\n after  %v", root, before, after)
	}
}

// TestReadLibraryFormat covers readLibraryFormat, the tolerant reader of a
// library's raw library.yaml `format:` integer: it reads the integer unchanged
// (including values the loader rejects: 0, 2, …), tolerates unknown keys and an
// absent name, and gives a clear error naming the file (or the key) for a
// missing file, a non-mapping document, a non-integer value and an absent key
// (global.constraint.upgrade-unknown-format-reported).
func TestReadLibraryFormat(t *testing.T) {
	reads := []struct {
		name string
		body string
		want int
	}{
		{"reads the format integer", "name: shared-lib\nformat: 1\n", 1},
		{"reads a newer format unchanged", "name: shared-lib\nformat: 2\n", 2},
		{"reads the zero format unchanged", "name: shared-lib\nformat: 0\n", 0},
		{"tolerates unknown keys and an absent name", "format: 3\nextra: ignored\n", 3},
		{"tolerates a comment and other content", "# a comment\nformat: 1\nname: shared-lib\n", 1},
	}
	for _, tc := range reads {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, template.LibraryFile), tc.body)

			got, err := readLibraryFormat(dir)
			if err != nil {
				t.Fatalf("readLibraryFormat(%s) error = %v, want format %d", dir, err, tc.want)
			}
			if got != tc.want {
				t.Errorf("readLibraryFormat(%s) = %d, want %d", dir, got, tc.want)
			}
		})
	}

	t.Run("a missing library.yaml errors naming the file", func(t *testing.T) {
		dir := t.TempDir()

		_, err := readLibraryFormat(dir)
		if err == nil {
			t.Fatalf("readLibraryFormat(%s) error = nil, want a missing-file error", dir)
		}
		msg := err.Error()
		for _, want := range []string{template.LibraryFile, "not found"} {
			if !strings.Contains(msg, want) {
				t.Errorf("readLibraryFormat error = %q, want it to contain %q", msg, want)
			}
		}
	})

	t.Run("a non-integer format errors naming the key", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, template.LibraryFile), "name: shared-lib\nformat: one\n")

		_, err := readLibraryFormat(dir)
		if err == nil {
			t.Fatalf("readLibraryFormat(%s) error = nil, want a non-integer error", dir)
		}
		if msg := err.Error(); !strings.Contains(msg, "format") {
			t.Errorf("readLibraryFormat error = %q, want it to name the %q key", msg, libraryFormatKey)
		}
	})

	t.Run("a missing format key errors naming the key", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, template.LibraryFile), "name: shared-lib\n")

		_, err := readLibraryFormat(dir)
		if err == nil {
			t.Fatalf("readLibraryFormat(%s) error = nil, want a missing-key error", dir)
		}
		if msg := err.Error(); !strings.Contains(msg, libraryFormatKey) {
			t.Errorf("readLibraryFormat error = %q, want it to name the %q key", msg, libraryFormatKey)
		}
	})

	t.Run("a non-mapping document errors naming the file", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, template.LibraryFile), "just a scalar\n")

		_, err := readLibraryFormat(dir)
		if err == nil {
			t.Fatalf("readLibraryFormat(%s) error = nil, want a non-mapping error", dir)
		}
		if msg := err.Error(); !strings.Contains(msg, "mapping") {
			t.Errorf("readLibraryFormat error = %q, want it to report the expected mapping", msg)
		}
	})

	t.Run("an empty file errors naming the file", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, template.LibraryFile), "")

		_, err := readLibraryFormat(dir)
		if err == nil {
			t.Fatalf("readLibraryFormat(%s) error = nil, want an empty-document error", dir)
		}
		if msg := err.Error(); !strings.Contains(msg, "mapping") {
			t.Errorf("readLibraryFormat error = %q, want it to report the expected mapping", msg)
		}
	})
}

// TestImplementedFormat covers implementedFormat, the binary-side format
// anchor: it reports the format of the source the binary was built from
// (firstLibraryFormat, 1) and never a version string. A `dev`/unstamped build
// therefore implements its source's format exactly like a tagged build of the
// same source and is never "newer": a library at implementedFormat is current,
// and only an integer greater than it is newer.
func TestImplementedFormat(t *testing.T) {
	if got, want := implementedFormat(), firstLibraryFormat; got != want {
		t.Errorf("implementedFormat() = %d, want the source format %d", got, want)
	}
	if got, want := implementedFormat(), 1; got != want {
		t.Errorf("implementedFormat() = %d, want 1 (the only accepted library.yaml format)", got)
	}
	// The migration target defaults to implementedFormat; both are the format
	// integer anchor, so an unstamped build never raises the bar a version
	// string could imply.
	if got, want := migrationTargetFormat(), implementedFormat(); got != want {
		t.Errorf("migrationTargetFormat() = %d, want implementedFormat() = %d", got, want)
	}
	// A library at exactly implementedFormat() is current, not newer: the
	// production migration half accepts it as a no-op (see the no-op test).
	dir := migrateLibraryFixture(t)
	if err := migrateLibrary(dir); err != nil {
		t.Errorf("migrateLibrary(%s) error = %v, want a format-1 library accepted as current, never \"newer\"", dir, err)
	}
}

// TestRegisteredMigrationsStaysEmpty guards task-7's contract: the registered
// migration set is kalide-authored, EMPTY today, and is never populated from a
// project's content or by a test. Ordering, at-most-once, rollback and
// validation are exercised through the migrateLibraryWith seam instead
// (requirements.constraint.upgrade-migrations-ordered-atomic).
func TestRegisteredMigrationsStaysEmpty(t *testing.T) {
	if got := registeredMigrations(); len(got) != 0 {
		t.Errorf("registeredMigrations() = %d migration(s), want the registered set empty; "+
			"tests must drive synthetic migrations through migrateLibraryWith, never add one here", len(got))
	}
}

// TestMigrateLibraryUnknownFormatRefused covers the unknown/newer refusal: a
// library declaring format 0 (older/unknown, no migration can carry it forward)
// and one declaring format 2 (newer than the binary) are each refused with
// unsupportedFormatError naming the format found and the format supported, and
// the library's files are left byte-for-byte and mtime-untouched. The refusal is
// produced by production migrateLibrary (registered set empty), never by a
// synthetic step.
func TestMigrateLibraryUnknownFormatRefused(t *testing.T) {
	t.Run("an unknown format 0 is refused naming found vs supported and writes nothing", func(t *testing.T) {
		dir := migrateRawLibrary(t, 0)
		before := refreshFingerprint(t, dir)

		err := migrateLibrary(dir)
		migrateAssertRefused(t, err, 0, 1)
		refreshAssertTreeUntouched(t, dir, before)
	})

	t.Run("a newer format 2 is refused naming found vs supported and writes nothing", func(t *testing.T) {
		dir := migrateRawLibrary(t, 2)
		before := refreshFingerprint(t, dir)

		err := migrateLibrary(dir)
		migrateAssertRefused(t, err, 2, 1)
		refreshAssertTreeUntouched(t, dir, before)
	})
}

// TestMigrateLibraryFormatOneNoOp covers the byte-for-byte no-op: the registered
// set is empty and no format older than 1 exists, so a format-1 library is
// unchanged across runs, and a deck's kalide.yaml carries no version field at
// all, so the migration half reads no format and writes nothing for a deck
// either. Nothing is written on the first or the second run
// (requirements.requirement.upgrade-migrate-format,
// requirements.note.upgrade-deck-vs-library).
func TestMigrateLibraryFormatOneNoOp(t *testing.T) {
	t.Run("a format-1 library is a byte-for-byte no-op across runs", func(t *testing.T) {
		dir := migrateLibraryFixture(t)
		before := refreshFingerprint(t, dir)

		for run := 1; run <= 2; run++ {
			if err := migrateLibrary(dir); err != nil {
				t.Fatalf("migrateLibrary run %d on %s error = %v, want a no-op for a format-1 library", run, dir, err)
			}
		}
		refreshAssertTreeUntouched(t, dir, before)
	})

	t.Run("a deck is a byte-for-byte no-op across runs", func(t *testing.T) {
		dir := migrateDeckFixture(t)
		before := refreshFingerprint(t, dir)

		for run := 1; run <= 2; run++ {
			if err := migrateLibrary(dir); err != nil {
				t.Fatalf("migrateLibrary run %d on %s error = %v, want a no-op for a deck", run, dir, err)
			}
		}
		refreshAssertTreeUntouched(t, dir, before)
	})
}

// TestMigrateLibraryOrderingAndAtMostOnce drives the migrateLibraryWith seam:
// the ordered chain from the library's current format to migrationTargetFormat,
// each step applied at most once. Migrations are supplied out of registry order
// to prove format order is imposed, a duplicated source format is refused with
// nothing applied (at-most-once, never twice), and an idempotent step leaves the
// tree byte-identical on a second run.
func TestMigrateLibraryOrderingAndAtMostOnce(t *testing.T) {
	t.Run("migrations run in format order, each once, regardless of registry order", func(t *testing.T) {
		dir := migrateLibraryFixture(t)
		migrateSetTarget(t, 3)

		var ran []int
		migs := []formatMigration{
			{sourceFormat: 2, apply: func(string) error { ran = append(ran, 2); return nil }},
			{sourceFormat: 1, apply: func(string) error { ran = append(ran, 1); return nil }},
		}

		if err := migrateLibraryWith(dir, migs); err != nil {
			t.Fatalf("migrateLibraryWith(%s) error = %v, want the chain from format 1 to 3", dir, err)
		}
		if want := []int{1, 2}; !reflect.DeepEqual(ran, want) {
			t.Errorf("applied migrations = %v, want %v (format order, each applied at most once)", ran, want)
		}
	})

	t.Run("a duplicate source format is refused and neither step applied", func(t *testing.T) {
		dir := migrateLibraryFixture(t)
		migrateSetTarget(t, 2)

		calls := 0
		duplicate := func(string) error { calls++; return nil }
		migs := []formatMigration{
			{sourceFormat: 1, apply: duplicate},
			{sourceFormat: 1, apply: duplicate},
		}
		before := snapshot(t, dir)

		err := migrateLibraryWith(dir, migs)
		var refused *unsupportedFormatError
		if !errors.As(err, &refused) {
			t.Fatalf("migrateLibraryWith(%s) error = %v, want *unsupportedFormatError for an ambiguous registry", dir, err)
		}
		if calls != 0 {
			t.Errorf("an ambiguous registry applied %d step(s), want none", calls)
		}
		migrateAssertSameTree(t, dir, before)
	})

	t.Run("a second run of an idempotent migration changes nothing", func(t *testing.T) {
		dir := migrateLibraryFixture(t)
		migrateSetTarget(t, 2)

		step := formatMigration{sourceFormat: 1, apply: func(root string) error {
			return os.WriteFile(filepath.Join(root, "migrated.txt"), []byte("migrated\n"), 0o644)
		}}

		if err := migrateLibraryWith(dir, []formatMigration{step}); err != nil {
			t.Fatalf("first migrateLibraryWith(%s) error = %v", dir, err)
		}
		first := snapshot(t, dir)

		if err := migrateLibraryWith(dir, []formatMigration{step}); err != nil {
			t.Fatalf("second migrateLibraryWith(%s) error = %v", dir, err)
		}
		migrateAssertSameTree(t, dir, first)
	})
}

// TestMigrateLibraryRollback covers the all-or-nothing guarantee: a step that
// fails in its own apply, and a step whose result fails per-migration
// validation, each roll the whole run back so every file — including a file an
// earlier step wrote — is left byte-for-byte as it was and no partial migration
// survives (requirements.constraint.upgrade-migrations-ordered-atomic,
// global.constraint.upgrade-never-breaks-project). The error names the failing
// source format and wraps the cause.
func TestMigrateLibraryRollback(t *testing.T) {
	t.Run("a failing apply rolls the whole run back byte-for-byte", func(t *testing.T) {
		dir := migrateLibraryFixture(t)
		migrateSetTarget(t, 3)
		before := snapshot(t, dir)

		sentinel := errors.New("synthetic step 2 failure")
		migs := []formatMigration{
			{sourceFormat: 1, apply: func(root string) error {
				return os.WriteFile(filepath.Join(root, "step-1.txt"), []byte("step 1 ran\n"), 0o644)
			}},
			{sourceFormat: 2, apply: func(root string) error {
				if err := os.WriteFile(filepath.Join(root, "step-2.txt"), []byte("step 2 ran\n"), 0o644); err != nil {
					return err
				}
				return sentinel
			}},
		}

		err := migrateLibraryWith(dir, migs)
		if !errors.Is(err, sentinel) {
			t.Fatalf("migrateLibraryWith(%s) error = %v, want it to wrap the failing step", dir, err)
		}
		if msg := err.Error(); !strings.Contains(msg, "from format 2") {
			t.Errorf("error = %q, want it to name the failing source format 2", msg)
		}
		for _, rel := range []string{"step-1.txt", "step-2.txt"} {
			if _, statErr := os.Lstat(filepath.Join(dir, rel)); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("Lstat(%s) err = %v, want fs.ErrNotExist: rollback must remove a file a failed migration wrote", rel, statErr)
			}
		}
		migrateAssertSameTree(t, dir, before)
	})

	t.Run("a migration whose result fails validation is rolled back", func(t *testing.T) {
		dir := migrateLibraryFixture(t)
		migrateSetTarget(t, 2)
		before := snapshot(t, dir)

		// A step whose result the template loader rejects (only format 1
		// loads), so validateMigratedLibrary fails after apply succeeded.
		migs := []formatMigration{{sourceFormat: 1, apply: func(root string) error {
			return os.WriteFile(filepath.Join(root, template.LibraryFile),
				[]byte("name: shared-lib\nformat: 2\n"), 0o644)
		}}}

		err := migrateLibraryWith(dir, migs)
		if err == nil {
			t.Fatalf("migrateLibraryWith(%s) error = nil, want a per-migration validation failure", dir)
		}
		if msg := err.Error(); !strings.Contains(msg, "does not load") {
			t.Errorf("error = %q, want it to report the failed per-migration validation", msg)
		}
		migrateAssertSameTree(t, dir, before)
	})
}

// TestMigrateLibrary is the integration aggregate for the migration half
// (upgrade/plan.phase-03.task-9). It runs on real on-disk projects — a real
// format-1 library and a real no-arg seed deck built with scaffold.InitLibrary
// and scaffold.Init — and re-validates the result through the real loaders:
// template.LoadDeckLibrary for a library (and a deck's template root) and
// deck.LoadConfig for a deck. It is int tier: guarded with testing.Short, it
// reaches no network, no server and no git and exposes no command surface
// (global.constraint.upgrade-offline,
// global.constraint.upgrade-never-breaks-project).
//
// Three claims:
//
//   - a format-1 library and a deck are byte-for-byte no-ops of the migration
//     half and still load/validate cleanly;
//   - a refused format (0, 2) leaves a real library byte-for-byte untouched;
//   - the loader↔migrator tie
//     (requirements.requirement.upgrade-migrate-format, AC "loadLibraryMeta
//     accepts every format the migrator reads — today that is exactly {1}"): a
//     library at implementedFormat() — and, for every source format in
//     registeredMigrations() (none today, so that loop covers zero cases) — a
//     library at that migration's source format loads through
//     template.LoadDeckLibrary, while implementedFormat()+1 is refused. The
//     test-only tie fails if the implemented format or the loader's accepted
//     set drifts (e.g. a migration registered without loader support) rather
//     than passing vacuously. No production loader change is made here.
func TestMigrateLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: builds real libraries and decks on disk and re-validates them through the loaders")
	}

	t.Run("a format-1 library is a byte-for-byte no-op and still loads", func(t *testing.T) {
		dir := migrateLibraryFixture(t)
		before := refreshFingerprint(t, dir)

		if err := migrateLibrary(dir); err != nil {
			t.Fatalf("migrateLibrary(%s) error = %v, want a no-op for a format-1 library", dir, err)
		}
		refreshAssertTreeUntouched(t, dir, before)

		// The migration half leaves the library loadable through the real
		// loader: a library is a template root itself.
		migrateLoadLibrary(t, dir)
	})

	t.Run("a deck is a byte-for-byte no-op and still loads and validates", func(t *testing.T) {
		dir := migrateDeckFixture(t)
		before := refreshFingerprint(t, dir)

		if err := migrateLibrary(dir); err != nil {
			t.Fatalf("migrateLibrary(%s) error = %v, want a no-op for a deck", dir, err)
		}
		refreshAssertTreeUntouched(t, dir, before)

		// The deck's config validates through deck.LoadConfig and its template
		// root through template.LoadDeckLibrary.
		refreshLoadDeck(t, dir, filepath.Join(dir, template.TemplatesDir), "")
	})

	t.Run("a refused format leaves a real library byte-for-byte untouched", func(t *testing.T) {
		for _, format := range []int{firstLibraryFormat - 1, implementedFormat() + 1} {
			t.Run(fmt.Sprintf("format %d", format), func(t *testing.T) {
				dir := migrateLibraryAt(t, format)
				before := refreshFingerprint(t, dir)

				err := migrateLibrary(dir)
				migrateAssertRefused(t, err, format, implementedFormat())
				refreshAssertTreeUntouched(t, dir, before)
			})
		}
	})

	t.Run("the loader accepts every format the migrator reads and refuses the next", func(t *testing.T) {
		current := implementedFormat()

		// (a) A library at implementedFormat() loads cleanly through the real
		// loader.
		t.Run(fmt.Sprintf("a library at implementedFormat() = %d loads", current), func(t *testing.T) {
			dir := migrateLibraryAt(t, current)
			migrateLoadLibrary(t, dir)
		})

		// (a) For every source format in registeredMigrations() (none today,
		// so this loop covers zero cases), a library at that migration's
		// source format likewise loads. Registering a migration whose source
		// format the loader does not accept fails this test rather than
		// shipping an unmigratable format.
		for _, m := range registeredMigrations() {
			t.Run(fmt.Sprintf("a library at registered source format %d loads", m.sourceFormat), func(t *testing.T) {
				dir := migrateLibraryAt(t, m.sourceFormat)
				migrateLoadLibrary(t, dir)
			})
		}

		// (b) implementedFormat()+1 is refused by the real loader.
		next := current + 1
		t.Run(fmt.Sprintf("a library at implementedFormat()+1 = %d is refused", next), func(t *testing.T) {
			dir := migrateLibraryAt(t, next)

			_, err := template.LoadDeckLibrary(dir, ".")
			if err == nil {
				t.Fatalf("template.LoadDeckLibrary(%s, \".\") error = nil, want format %d refused", dir, next)
			}
			var libErr *template.LibraryError
			if !errors.As(err, &libErr) {
				t.Fatalf("template.LoadDeckLibrary(%s, \".\") error = %v, want *template.LibraryError", dir, err)
			}
			if !strings.Contains(libErr.Message, "unsupported library format") {
				t.Errorf("LoadDeckLibrary error message = %q, want it to report an unsupported library format", libErr.Message)
			}
		})
	})
}

// migrateLibraryAt builds a fresh valid library with InitLibrary and rewrites
// its library.yaml to declare format, so the loader↔migrator tie can present a
// library built at any format integer. Everything but the format integer is the
// real InitLibrary layout, so a format the loader accepts still loads for the
// right reason and a format it rejects fails for the right reason.
func migrateLibraryAt(t *testing.T, format int) string {
	t.Helper()

	dir := migrateLibraryFixture(t)
	writeFile(t, filepath.Join(dir, template.LibraryFile),
		fmt.Sprintf("name: shared-lib\nformat: %d\n", format))
	return dir
}

// migrateLoadLibrary loads a real library root through the real loader and fails
// the test on any error. A library is a template root itself, so it is loaded as
// a deck would load a configured library: "." resolves against the library root.
func migrateLoadLibrary(t *testing.T, libraryRoot string) {
	t.Helper()

	if _, err := template.LoadDeckLibrary(libraryRoot, "."); err != nil {
		t.Fatalf("template.LoadDeckLibrary(%s, \".\") error = %v, want the library to load after the migration half", libraryRoot, err)
	}
}
