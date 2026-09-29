package scaffold

// This file hosts the unit tests for the refresh half of `kalide upgrade`
// (internal/scaffold/refresh.go): detectProject and detectSeedDeck, the two
// detection functions, and refreshScaffold, the never-clobber refresh engine
// (requirements.requirement.upgrade-refresh-owned-scaffold,
// global.constraint.upgrade-never-clobbers,
// requirements.constraint.upgrade-owned-scaffold-scope).
//
// The tests are unit tier: every fixture is built in a t.TempDir, the tests
// reach no network, no server and no git, and they run under -short.
//
// upgrade/plan.phase-02.task-5 creates this file with the unit cases below. The
// integration aggregate TestRefreshScaffold belongs to
// upgrade/plan.phase-02.task-6 and is added to this same file; the shared
// file-level scaffolding here (the seed-deck and library fixtures, the
// controlled-catalogue seam, the pinned-mtime no-write assertion and the report
// accessors) is written so that aggregate can reuse it.
//
// The refresh engine rewrites a member only when the member is present and
// byte-identical to a catalogued released version of THAT identity. The
// catalogue records digests only, never historical contents
// (global.constraint.upgrade-known-version-catalog), and the embedded seed ships
// only the running binary's own versions, so no released-but-older member bytes
// exist in the tree to copy. refreshSetCatalog is therefore the single seam that
// presents an old released version: it extends the decoded catalogue for the
// duration of one subtest. Every other test drives the real embedded catalogue.

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
)

// refreshPinnedTime is a fixed past instant pinned onto a file's mtime before a
// refresh run. A later stat that still reports this instant proves the file was
// NOT rewritten — a stronger observation than equal bytes, because a rewrite
// that happened to write identical bytes would still move the mtime.
var refreshPinnedTime = time.Date(2001, time.February, 3, 4, 5, 6, 0, time.UTC)

// refreshDeckThemePath is the deck-owned default theme's path relative to a deck
// root, built from the same constants refresh.go's deckThemeIdentity uses.
const refreshDeckThemePath = template.TemplatesDir + "/" + themeCSSPath

// refreshSeedDeck creates a fresh no-arg seed deck with scaffold.Init and
// returns its root. The deck therefore ships every kalide-owned deck member at
// the running binary's current bytes plus a kalide.yaml, assets/ and the rest of
// the seed.
func refreshSeedDeck(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init(%s) error = %v, want a fresh seed deck", dir, err)
	}
	return dir
}

// refreshLibrary creates a fresh template library with scaffold.InitLibrary and
// returns its root. The library is created in a subdirectory with a valid
// library name ("shared-lib"), because InitLibrary derives the library name from
// the target's base name and t.TempDir's base name is not a valid
// [a-z0-9][a-z0-9-]* name. The library therefore ships its library.yaml, the
// standard layout directories, the seeded default theme and the library-root
// AGENTS.md guide.
func refreshLibrary(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "shared-lib")
	if err := InitLibrary(dir); err != nil {
		t.Fatalf("InitLibrary(%s) error = %v, want a fresh library", dir, err)
	}
	return dir
}

// refreshAbs joins a project root with a project-relative slash path.
func refreshAbs(root, slashPath string) string {
	return filepath.Join(root, filepath.FromSlash(slashPath))
}

// refreshWrite writes data to path, creating parent directories as needed.
func refreshWrite(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// refreshRead reads path, failing the test on any error.
func refreshRead(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// refreshPin pins path's mtime to refreshPinnedTime, so a later stat detects a
// rewrite.
func refreshPin(t *testing.T, path string) {
	t.Helper()

	if err := os.Chtimes(path, refreshPinnedTime, refreshPinnedTime); err != nil {
		t.Fatalf("pin mtime of %s: %v", path, err)
	}
}

// refreshAssertUnchanged asserts path still holds want byte-for-byte AND still
// carries the pinned mtime, so the comparison observes "no write" rather than
// only "equal bytes" (global.constraint.upgrade-never-clobbers).
func refreshAssertUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()

	if got := refreshRead(t, path); !bytes.Equal(got, want) {
		t.Errorf("%s = %q, want the untouched %q", path, got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.ModTime().Equal(refreshPinnedTime) {
		t.Errorf("%s mtime = %v, want the pinned %v; the path was rewritten", path, info.ModTime(), refreshPinnedTime)
	}
}

// refreshAssertAbsent asserts path does not exist.
func refreshAssertAbsent(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Lstat(%s) err = %v, want fs.ErrNotExist (the path must not be created)", path, err)
	}
}

// refreshReportSet returns every path a report names, refreshed and skipped
// combined, sorted, so a test can compare the report's path set.
func refreshReportSet(report *refreshReport) []string {
	all := make([]string, 0, len(report.refreshed)+len(report.skipped))
	all = append(all, report.refreshed...)
	all = append(all, report.skipped...)
	sort.Strings(all)
	return all
}

// refreshMemberPathSet returns an owned set's member paths, sorted.
func refreshMemberPathSet(members []ownedMember) []string {
	paths := make([]string, 0, len(members))
	for _, member := range members {
		paths = append(paths, member.path)
	}
	sort.Strings(paths)
	return paths
}

// refreshAssertReport asserts the report names exactly the wanted paths, nothing
// missing and nothing repeated.
func refreshAssertReport(t *testing.T, report *refreshReport, want []string) {
	t.Helper()

	got := refreshReportSet(report)
	if !slices.Equal(got, want) {
		t.Errorf("report paths = %v, want %v", got, want)
	}
	if len(got) != len(report.refreshed)+len(report.skipped) {
		t.Errorf("report repeats a path: refreshed %v, skipped %v", report.refreshed, report.skipped)
	}
}

// refreshIn reports whether paths contains want.
func refreshIn(paths []string, want string) bool {
	return slices.Contains(paths, want)
}

// refreshCatalogFixture is a per-identity set of extra released byte slices a
// test wants the catalogue to treat as catalogued versions of that identity.
type refreshCatalogFixture map[string][][]byte

// refreshSetCatalog extends the decoded known-version catalogue with
// test-fixture digests for the duration of the calling (sub)test, restoring the
// real catalogue on cleanup.
//
// It is the one seam that can present an OLD released version to the refresh
// engine: the catalogue holds digests only, never historical contents
// (global.constraint.upgrade-known-version-catalog), so a release older than the
// running binary cannot be reconstructed from the shipped tree. Injecting a
// digest under a real owned identity drives refreshMember's refreshed branch with
// controlled bytes while leaving the production catalogue untouched.
func refreshSetCatalog(t *testing.T, extra refreshCatalogFixture) {
	t.Helper()

	original := knownCatalog
	extended := scaffoldCatalog{byIdentity: make(map[string]catalogIdentity, len(original.byIdentity))}
	for identity, entry := range original.byIdentity {
		digests := make(map[string][]string, len(entry.digests))
		for digest, versions := range entry.digests {
			digests[digest] = versions
		}
		extended.byIdentity[identity] = catalogIdentity{digests: digests}
	}
	for identity, datas := range extra {
		entry, ok := extended.byIdentity[identity]
		if !ok {
			t.Fatalf("refreshSetCatalog: fixture identity %q is not a catalogued owned identity; the identity set is %v",
				identity, extended.identities())
		}
		if entry.digests == nil {
			entry.digests = make(map[string][]string, len(datas))
		}
		for _, data := range datas {
			if len(data) == 0 {
				t.Fatalf("refreshSetCatalog: empty fixture bytes for %q", identity)
			}
			entry.digests[shippedScaffoldDigest(data)] = []string{"test-fixture"}
		}
		extended.byIdentity[identity] = entry
	}

	knownCatalog = extended
	t.Cleanup(func() { knownCatalog = original })
}

// TestDetectProject covers detectProject, the single owner of deck/library
// detection and of the both/neither error (deck-library-upgrade project
// detection): a directory holding only kalide.yaml is a deck, one holding only
// library.yaml is a library, and one holding both or neither is an error naming
// what was found against what was expected and writing nothing.
func TestDetectProject(t *testing.T) {
	t.Run("a deck directory is detected by kalide.yaml", func(t *testing.T) {
		dir := t.TempDir()
		refreshWrite(t, filepath.Join(dir, deck.ConfigFile), []byte("title: Mine\n"))

		before := snapshot(t, dir)
		kind, err := detectProject(dir)
		if err != nil {
			t.Fatalf("detectProject(%s) error = %v, want a deck", dir, err)
		}
		if kind != projectKindDeck {
			t.Errorf("detectProject(%s) kind = %v, want projectKindDeck", dir, kind)
		}
		if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
			t.Errorf("detectProject modified the directory:\n before %v\n after  %v", before, after)
		}
	})

	t.Run("a library directory is detected by library.yaml", func(t *testing.T) {
		dir := t.TempDir()
		refreshWrite(t, filepath.Join(dir, template.LibraryFile), []byte("name: shared-lib\nformat: 1\n"))

		before := snapshot(t, dir)
		kind, err := detectProject(dir)
		if err != nil {
			t.Fatalf("detectProject(%s) error = %v, want a library", dir, err)
		}
		if kind != projectKindLibrary {
			t.Errorf("detectProject(%s) kind = %v, want projectKindLibrary", dir, kind)
		}
		if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
			t.Errorf("detectProject modified the directory:\n before %v\n after  %v", before, after)
		}
	})

	t.Run("both manifests is an error naming found vs expected and writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		refreshWrite(t, filepath.Join(dir, deck.ConfigFile), []byte("title: Mine\n"))
		refreshWrite(t, filepath.Join(dir, template.LibraryFile), []byte("name: shared-lib\nformat: 1\n"))

		before := snapshot(t, dir)
		kind, err := detectProject(dir)
		if err == nil {
			t.Fatalf("detectProject(%s) error = nil, want a both-manifests error", dir)
		}
		if kind != projectKindUnknown {
			t.Errorf("detectProject(%s) kind = %v, want projectKindUnknown on error", dir, kind)
		}
		msg := err.Error()
		for _, want := range []string{deck.ConfigFile, template.LibraryFile, "both", "expected exactly one"} {
			if !strings.Contains(msg, want) {
				t.Errorf("detectProject error = %q, want it to contain %q", msg, want)
			}
		}
		if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
			t.Errorf("detectProject modified the directory on a both-manifests error:\n before %v\n after  %v", before, after)
		}
	})

	t.Run("neither manifest is an error naming found vs expected and writes nothing", func(t *testing.T) {
		dir := t.TempDir()

		before := snapshot(t, dir)
		kind, err := detectProject(dir)
		if err == nil {
			t.Fatalf("detectProject(%s) error = nil, want a neither-manifest error", dir)
		}
		if kind != projectKindUnknown {
			t.Errorf("detectProject(%s) kind = %v, want projectKindUnknown on error", dir, kind)
		}
		msg := err.Error()
		for _, want := range []string{deck.ConfigFile, template.LibraryFile, "neither", "expected exactly one"} {
			if !strings.Contains(msg, want) {
				t.Errorf("detectProject error = %q, want it to contain %q", msg, want)
			}
		}
		if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
			t.Errorf("detectProject modified the directory on a neither-manifest error:\n before %v\n after  %v", before, after)
		}
	})
}

// TestDetectSeedDeck covers detectSeedDeck, the objective no-arg seed-deck
// predicate: true exactly when the deck's kalide.yaml carries no non-empty
// `templates:` key and a local templates/ directory exists. A non-empty
// `templates:` key is decisive, so a leftover local templates/ never turns an
// external-library deck into a seed deck
// (requirements.constraint.upgrade-owned-scaffold-scope). The configuration is
// read through refreshDeckConfig, the same reader refreshScaffold uses, so the
// test exercises reading kalide.yaml as well as the predicate.
func TestDetectSeedDeck(t *testing.T) {
	t.Run("no-arg seed deck with a local templates directory is a seed deck", func(t *testing.T) {
		dir := t.TempDir()
		refreshWrite(t, filepath.Join(dir, deck.ConfigFile), []byte("title: Mine\n"))
		if err := os.MkdirAll(filepath.Join(dir, template.TemplatesDir), 0o755); err != nil {
			t.Fatalf("mkdir templates/: %v", err)
		}

		cfg, err := refreshDeckConfig(dir, nil)
		if err != nil {
			t.Fatalf("refreshDeckConfig(%s) error = %v", dir, err)
		}
		if !detectSeedDeck(dir, cfg) {
			t.Error("detectSeedDeck(no-arg seed deck) = false, want true")
		}
	})

	t.Run("an empty templates key still counts as no-arg", func(t *testing.T) {
		dir := t.TempDir()
		refreshWrite(t, filepath.Join(dir, deck.ConfigFile), []byte("title: Mine\ntemplates: \"\"\n"))
		if err := os.MkdirAll(filepath.Join(dir, template.TemplatesDir), 0o755); err != nil {
			t.Fatalf("mkdir templates/: %v", err)
		}

		cfg, err := refreshDeckConfig(dir, nil)
		if err != nil {
			t.Fatalf("refreshDeckConfig(%s) error = %v", dir, err)
		}
		if !detectSeedDeck(dir, cfg) {
			t.Error("detectSeedDeck(empty templates key with a local templates/) = false, want true")
		}
	})

	t.Run("an external-library deck with a leftover local templates is not a seed deck", func(t *testing.T) {
		dir := t.TempDir()
		refreshWrite(t, filepath.Join(dir, deck.ConfigFile), []byte("title: Mine\ntemplates: ../shared-lib\n"))
		// A leftover local templates/ directory: the non-empty `templates:` key
		// is authoritative and decisive, so it must not make this a seed deck.
		refreshWrite(t, refreshAbs(dir, template.TemplatesDir+"/keep.txt"), []byte("leftover\n"))

		cfg, err := refreshDeckConfig(dir, nil)
		if err != nil {
			t.Fatalf("refreshDeckConfig(%s) error = %v", dir, err)
		}
		if deck.TemplatesPath(cfg) == "" {
			t.Fatal("refreshDeckConfig did not read the non-empty templates: key")
		}
		if detectSeedDeck(dir, cfg) {
			t.Error("detectSeedDeck(external-library deck with a leftover local templates/) = true, want false")
		}
	})

	t.Run("a no-arg deck without a local templates directory is not a seed deck", func(t *testing.T) {
		dir := t.TempDir()
		refreshWrite(t, filepath.Join(dir, deck.ConfigFile), []byte("title: Mine\n"))

		cfg, err := refreshDeckConfig(dir, nil)
		if err != nil {
			t.Fatalf("refreshDeckConfig(%s) error = %v", dir, err)
		}
		if detectSeedDeck(dir, cfg) {
			t.Error("detectSeedDeck(no-arg deck without templates/) = true, want false")
		}
	})
}

// TestRefreshScaffoldDeckDecisions covers refreshScaffold's never-clobber
// decisions for the no-arg seed deck: an absent AGENTS.md guide is written, an
// absent seed starter file is never re-created, a present byte-identical
// catalogued released member is refreshed, an author-edited member is skipped,
// and the report lists every owned member path
// (global.constraint.upgrade-never-clobbers,
// requirements.constraint.upgrade-owned-scaffold-scope).
func TestRefreshScaffoldDeckDecisions(t *testing.T) {
	t.Run("an absent deck-root AGENTS.md guide is written", func(t *testing.T) {
		root := refreshSeedDeck(t)
		guide := refreshAbs(root, agentsGuideName)
		if err := os.Remove(guide); err != nil {
			t.Fatalf("remove %s: %v", guide, err)
		}

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, guide), mustReadSeed(agentsGuideName); !bytes.Equal(got, want) {
			t.Errorf("written %s does not equal the embedded deck guide", agentsGuideName)
		}
		if !refreshIn(report.refreshed, agentsGuideName) {
			t.Errorf("report.refreshed = %v, want it to name the written %s", report.refreshed, agentsGuideName)
		}
	})

	t.Run("an absent seed starter file is never re-created", func(t *testing.T) {
		root := refreshSeedDeck(t)
		starter := refreshAbs(root, starterSlidePath)
		if err := os.Remove(starter); err != nil {
			t.Fatalf("remove %s: %v", starter, err)
		}

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertAbsent(t, starter)
		if !refreshIn(report.skipped, starterSlidePath) {
			t.Errorf("report.skipped = %v, want it to name the absent %s", report.skipped, starterSlidePath)
		}
		if refreshIn(report.refreshed, starterSlidePath) {
			t.Errorf("report.refreshed = %v, want the deleted starter slide never re-created", report.refreshed)
		}
	})

	t.Run("a present catalogued released member is refreshed", func(t *testing.T) {
		root := refreshSeedDeck(t)
		theme := refreshAbs(root, refreshDeckThemePath)
		old := []byte("/* an older released default theme */\n")
		refreshWrite(t, theme, old)
		refreshSetCatalog(t, refreshCatalogFixture{deckThemeIdentity: {old}})

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, theme), mustReadSeed(refreshDeckThemePath); !bytes.Equal(got, want) {
			t.Errorf("%s was not refreshed to the running binary's bytes:\n got %q\nwant %q",
				refreshDeckThemePath, got, want)
		}
		if !refreshIn(report.refreshed, refreshDeckThemePath) {
			t.Errorf("report.refreshed = %v, want it to name the refreshed %s", report.refreshed, refreshDeckThemePath)
		}
	})

	t.Run("an author-edited member is skipped and reported and left byte-for-byte alone", func(t *testing.T) {
		root := refreshSeedDeck(t)
		example := refreshAbs(root, helloSlideDirPath+"/"+template.ExampleFile)
		author := []byte("---\ntemplate: hello\n---\nmy own example, edited by me\n")
		refreshWrite(t, example, author)
		refreshPin(t, example)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, example, author)

		rel := helloSlideDirPath + "/" + template.ExampleFile
		if !refreshIn(report.skipped, rel) {
			t.Errorf("report.skipped = %v, want it to name the author-edited %s", report.skipped, rel)
		}
		if refreshIn(report.refreshed, rel) {
			t.Errorf("report.refreshed = %v, want the author-edited %s untouched", report.refreshed, rel)
		}
	})

	t.Run("the report lists every owned member path", func(t *testing.T) {
		root := refreshSeedDeck(t)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if len(report.refreshed) != 0 {
			t.Errorf("report.refreshed = %v, want no refresh for an already-current seed deck", report.refreshed)
		}
		refreshAssertReport(t, report, refreshMemberPathSet(deckOwnedMembers))
	})
}

// TestRefreshScaffoldLibrary covers the library form
// (requirements.constraint.upgrade-owned-scaffold-scope): an absent
// library-root AGENTS.md guide is written, a catalogued library
// themes/default/theme.css is refreshed to the seeded default, an author-edited
// library theme is skipped and reported, and a library's own slides/ and
// sections/ (its template content) are never written.
func TestRefreshScaffoldLibrary(t *testing.T) {
	t.Run("an absent library-root AGENTS.md guide is written", func(t *testing.T) {
		root := refreshLibrary(t)
		guide := refreshAbs(root, agentsGuideName)
		if err := os.Remove(guide); err != nil {
			t.Fatalf("remove %s: %v", guide, err)
		}

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, guide), mustReadLibraryGuide(); !bytes.Equal(got, want) {
			t.Errorf("written %s does not equal the embedded library guide", agentsGuideName)
		}
		if !refreshIn(report.refreshed, agentsGuideName) {
			t.Errorf("report.refreshed = %v, want it to name the written %s", report.refreshed, agentsGuideName)
		}
	})

	t.Run("a catalogued library theme is refreshed to the seeded default", func(t *testing.T) {
		root := refreshLibrary(t)
		theme := refreshAbs(root, themeCSSPath)
		old := []byte("/* an older released library default theme */\n")
		refreshWrite(t, theme, old)
		refreshSetCatalog(t, refreshCatalogFixture{libraryThemeIdentity: {old}})

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, theme), []byte(DefaultThemeCSS()); !bytes.Equal(got, want) {
			t.Errorf("%s was not refreshed to the seeded default:\n got %q\nwant %q", themeCSSPath, got, want)
		}
		if !refreshIn(report.refreshed, themeCSSPath) {
			t.Errorf("report.refreshed = %v, want it to name the refreshed %s", report.refreshed, themeCSSPath)
		}
	})

	t.Run("an author-edited library theme is skipped and reported and left byte-for-byte alone", func(t *testing.T) {
		root := refreshLibrary(t)
		theme := refreshAbs(root, themeCSSPath)
		author := []byte("/* my own theme */\n:root { --accent: rebeccapurple; }\n")
		refreshWrite(t, theme, author)
		refreshPin(t, theme)

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, theme, author)
		if !refreshIn(report.skipped, themeCSSPath) {
			t.Errorf("report.skipped = %v, want it to name the author-edited %s", report.skipped, themeCSSPath)
		}
		if refreshIn(report.refreshed, themeCSSPath) {
			t.Errorf("report.refreshed = %v, want the author-edited %s untouched", report.refreshed, themeCSSPath)
		}
	})

	t.Run("a library's slides and sections are never written", func(t *testing.T) {
		root := refreshLibrary(t)
		slideRel := template.SlidesDir + "/hello/" + template.ManifestFile
		sectionRel := template.SectionsDir + "/person/" + template.ManifestFile
		slide := refreshAbs(root, slideRel)
		section := refreshAbs(root, sectionRel)
		slideBytes := []byte("name: hello\nfields: []\n")
		sectionBytes := []byte("body:\n  mode: optional\n")
		refreshWrite(t, slide, slideBytes)
		refreshWrite(t, section, sectionBytes)
		refreshPin(t, slide)
		refreshPin(t, section)

		// Force a real write so we know the engine ran, and still assert the
		// library's author template content was never visited.
		if err := os.Remove(refreshAbs(root, agentsGuideName)); err != nil {
			t.Fatalf("remove library guide: %v", err)
		}

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, slide, slideBytes)
		refreshAssertUnchanged(t, section, sectionBytes)

		for _, rel := range []string{slideRel, sectionRel} {
			if refreshIn(report.refreshed, rel) || refreshIn(report.skipped, rel) {
				t.Errorf("report names %s; a library's slides/ and sections/ are author content, never owned members", rel)
			}
		}
		refreshAssertReport(t, report, refreshMemberPathSet(libraryOwnedMembers))
	})
}

// TestRefreshScaffoldExternalDeck covers the external-library deck form
// (external-template-library,
// requirements.constraint.upgrade-owned-scaffold-scope): refreshScaffold creates
// no local templates/ and no starter slide, and a leftover local templates/
// present in the deck is left byte-for-byte untouched — never refreshed, never
// deleted.
func TestRefreshScaffoldExternalDeck(t *testing.T) {
	externalConfig := "title: External\ntemplates: ../shared-lib\n"

	t.Run("no local templates and no starter slide are created", func(t *testing.T) {
		root := t.TempDir()
		refreshWrite(t, filepath.Join(root, deck.ConfigFile), []byte(externalConfig))

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}

		refreshAssertAbsent(t, filepath.Join(root, template.TemplatesDir))
		refreshAssertAbsent(t, refreshAbs(root, starterSlidePath))

		// The external-library deck owns only its deck-root guide, which was
		// absent and so was written.
		if got := refreshRead(t, refreshAbs(root, agentsGuideName)); !bytes.Equal(got, mustReadSeed(agentsGuideName)) {
			t.Errorf("written %s does not equal the embedded deck guide", agentsGuideName)
		}
		refreshAssertReport(t, report, []string{agentsGuideName})
	})

	t.Run("a leftover local templates is left byte-for-byte untouched", func(t *testing.T) {
		root := t.TempDir()
		refreshWrite(t, filepath.Join(root, deck.ConfigFile), []byte(externalConfig))

		// A leftover local templates/ holding a catalogued deck theme (the
		// running binary's bytes) and an author file. Neither is an owned member
		// of an external-library deck, so neither may be refreshed or deleted.
		themeRel := refreshDeckThemePath
		theme := refreshAbs(root, themeRel)
		refreshWrite(t, theme, mustReadSeed(themeRel))
		refreshPin(t, theme)
		authorRel := template.TemplatesDir + "/notes.txt"
		author := refreshAbs(root, authorRel)
		refreshWrite(t, author, []byte("leftover local template notes\n"))
		refreshPin(t, author)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}

		refreshAssertUnchanged(t, theme, mustReadSeed(themeRel))
		refreshAssertUnchanged(t, author, []byte("leftover local template notes\n"))
		for _, rel := range []string{themeRel, authorRel} {
			if refreshIn(report.refreshed, rel) || refreshIn(report.skipped, rel) {
				t.Errorf("report names %s; a leftover local templates/ is out of scope for an external-library deck", rel)
			}
		}
		refreshAssertAbsent(t, refreshAbs(root, starterSlidePath))
		refreshAssertReport(t, report, []string{agentsGuideName})
	})
}

// TestRefreshScaffoldCrossIdentity covers catalogLookup's identity scoping
// through the refresh engine
// (global.constraint.upgrade-known-version-catalog): a file whose bytes match a
// DIFFERENT owned file's catalogued digest is skipped, not refreshed. The seed
// and library default theme.css legitimately share identical bytes, so that
// shared digest is catalogued under BOTH theme identities; the deck guide and
// library guide digests are each catalogued under their own identity only and
// are the true cross-identity cases.
func TestRefreshScaffoldCrossIdentity(t *testing.T) {
	t.Run("the deck guide bytes do not refresh the library theme", func(t *testing.T) {
		root := refreshLibrary(t)
		theme := refreshAbs(root, themeCSSPath)
		foreign := mustReadSeed(agentsGuideName)
		if bytes.Equal(foreign, []byte(DefaultThemeCSS())) {
			t.Fatal("fixture degenerate: the deck guide bytes equal the seeded default theme")
		}
		refreshWrite(t, theme, foreign)
		refreshPin(t, theme)

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, theme, foreign)
		if refreshIn(report.refreshed, themeCSSPath) {
			t.Errorf("report.refreshed = %v; bytes catalogued for deck:%s must not refresh library:%s",
				report.refreshed, agentsGuideName, themeCSSPath)
		}
	})

	t.Run("the library guide bytes do not refresh the deck theme", func(t *testing.T) {
		root := refreshSeedDeck(t)
		theme := refreshAbs(root, refreshDeckThemePath)
		foreign := mustReadLibraryGuide()
		refreshWrite(t, theme, foreign)
		refreshPin(t, theme)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, theme, foreign)
		if refreshIn(report.refreshed, refreshDeckThemePath) {
			t.Errorf("report.refreshed = %v; bytes catalogued for library:AGENTS.md must not refresh deck:%s",
				report.refreshed, refreshDeckThemePath)
		}
	})

	t.Run("the shared theme digest stays a no-op, never a cross-refresh", func(t *testing.T) {
		// The deck's templates/themes/default/theme.css and a library's
		// themes/default/theme.css are byte-identical, and the catalogue
		// deliberately carries that digest under both theme identities (the
		// library theme is derived from the seed theme). The library theme path
		// holding the deck theme's bytes is therefore already the desired
		// library theme: the engine must treat it as a no-op, never rewrite it
		// against the other identity's version.
		root := refreshLibrary(t)
		theme := refreshAbs(root, themeCSSPath)
		shared := mustReadSeed(refreshDeckThemePath)
		if !bytes.Equal(shared, []byte(DefaultThemeCSS())) {
			t.Fatal("fixture degenerate: the deck and library default themes no longer share bytes")
		}
		refreshWrite(t, theme, shared)
		refreshPin(t, theme)

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, theme, shared)
		if !refreshIn(report.skipped, themeCSSPath) {
			t.Errorf("report.skipped = %v, want the already-current shared theme %s", report.skipped, themeCSSPath)
		}
		if refreshIn(report.refreshed, themeCSSPath) {
			t.Errorf("report.refreshed = %v, want the shared theme never cross-refreshed", report.refreshed)
		}
	})
}

// TestRefreshScaffoldAlreadyCurrent covers the already-current no-write rule
// (global.constraint.upgrade-never-clobbers): a member already byte-identical to
// the running kalide's version of that same file is left as-is and NOT
// re-written, observed by the pinned mtime, not merely by equal bytes.
func TestRefreshScaffoldAlreadyCurrent(t *testing.T) {
	t.Run("every deck member already current is not rewritten", func(t *testing.T) {
		root := refreshSeedDeck(t)
		for _, member := range deckOwnedMembers {
			refreshPin(t, refreshAbs(root, member.path))
		}

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if len(report.refreshed) != 0 {
			t.Errorf("report.refreshed = %v, want no write for already-current members", report.refreshed)
		}
		refreshAssertReport(t, report, refreshMemberPathSet(deckOwnedMembers))
		for _, member := range deckOwnedMembers {
			refreshAssertUnchanged(t, refreshAbs(root, member.path), mustReadSeed(member.path))
		}
	})

	t.Run("every library member already current is not rewritten", func(t *testing.T) {
		root := refreshLibrary(t)
		want := map[string][]byte{
			agentsGuideName: mustReadLibraryGuide(),
			themeCSSPath:    []byte(DefaultThemeCSS()),
		}
		for _, member := range libraryOwnedMembers {
			refreshPin(t, refreshAbs(root, member.path))
		}

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if len(report.refreshed) != 0 {
			t.Errorf("report.refreshed = %v, want no write for already-current members", report.refreshed)
		}
		refreshAssertReport(t, report, refreshMemberPathSet(libraryOwnedMembers))
		for rel, data := range want {
			refreshAssertUnchanged(t, refreshAbs(root, rel), data)
		}
	})
}

// TestRefreshScaffoldNeverWritesKalideYAML covers
// requirements.constraint.upgrade-owned-scaffold-scope for the author-edited
// deck config: kalide.yaml is not a whole-file refresh member, for a no-arg seed
// deck and an external-library deck alike, so refreshScaffold never writes it and
// never names it in its report (global.constraint.upgrade-never-clobbers).
func TestRefreshScaffoldNeverWritesKalideYAML(t *testing.T) {
	t.Run("no-arg seed deck", func(t *testing.T) {
		root := refreshSeedDeck(t)
		config := filepath.Join(root, deck.ConfigFile)
		before := refreshRead(t, config)
		refreshPin(t, config)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, config, before)
		if refreshIn(report.refreshed, deck.ConfigFile) || refreshIn(report.skipped, deck.ConfigFile) {
			t.Errorf("report names %s; the author-edited deck config is not an owned member", deck.ConfigFile)
		}
	})

	t.Run("external-library deck", func(t *testing.T) {
		root := t.TempDir()
		config := filepath.Join(root, deck.ConfigFile)
		refreshWrite(t, config, []byte("title: External\ntemplates: ../shared-lib\n"))
		before := refreshRead(t, config)
		refreshPin(t, config)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, config, before)
		if refreshIn(report.refreshed, deck.ConfigFile) || refreshIn(report.skipped, deck.ConfigFile) {
			t.Errorf("report names %s; the author-edited deck config is not an owned member", deck.ConfigFile)
		}
	})
}
