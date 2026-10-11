package scaffold

// This file hosts the unit tests for the refresh half of `kalide upgrade`
// (internal/scaffold/refresh.go): detectProject and detectSeedDeck, the two
// detection functions, and refreshScaffold, the never-clobber refresh engine
// (requirements.requirement.upgrade-refresh-owned-scaffold,
// global.constraint.upgrade-never-clobbers,
// requirements.constraint.upgrade-owned-scaffold-scope).
//
// The unit cases are unit tier: every fixture is built in a t.TempDir, the tests
// reach no network, no server and no git, and they run under -short. The
// integration aggregate TestRefreshScaffold
// (upgrade/plan.phase-02.task-6) is int tier: it builds real projects on disk
// with scaffold.Init, scaffold.InitExternal and scaffold.InitLibrary and
// re-validates them through the real loaders, so it is guarded with
// testing.Short and skipped by `go test -short`.
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
	"github.com/really-knows-ai/kalide/internal/theme"
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
// decisions for the no-arg seed deck: an absent author-guide pair — the
// AGENTS_KALIDE.md full guide and the AGENTS.md pointer — is written (the
// absent-write exception), an absent seed starter file is never re-created, a
// present byte-identical catalogued released member is refreshed, an
// author-edited member is skipped, and the report lists every owned member path
// (global.constraint.upgrade-never-clobbers,
// requirements.constraint.upgrade-owned-scaffold-scope).
func TestRefreshScaffoldDeckDecisions(t *testing.T) {
	t.Run("absent deck-root author guides are written", func(t *testing.T) {
		root := refreshSeedDeck(t)
		for _, rel := range []string{agentGuideFullGuideName, agentsGuideName} {
			if err := os.Remove(refreshAbs(root, rel)); err != nil {
				t.Fatalf("remove %s: %v", rel, err)
			}
		}

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, refreshAbs(root, agentGuideFullGuideName)), mustReadSeed(agentGuideFullGuideName); !bytes.Equal(got, want) {
			t.Errorf("written %s does not equal the embedded deck full guide", agentGuideFullGuideName)
		}
		if got, want := refreshRead(t, refreshAbs(root, agentsGuideName)), mustReadSeed(agentsGuideName); !bytes.Equal(got, want) {
			t.Errorf("written %s does not equal the embedded deck pointer", agentsGuideName)
		}
		for _, rel := range []string{agentGuideFullGuideName, agentsGuideName} {
			if !refreshIn(report.refreshed, rel) {
				t.Errorf("report.refreshed = %v, want it to name the written %s", report.refreshed, rel)
			}
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
// (requirements.constraint.upgrade-owned-scaffold-scope): an absent library-root
// AGENTS_KALIDE.md full guide and AGENTS.md pointer are written, a catalogued
// library themes/default/theme.css is refreshed to the seeded default, an
// author-edited library theme is skipped and reported, and a library's own
// slides/ and sections/ (its template content) are never written.
func TestRefreshScaffoldLibrary(t *testing.T) {
	t.Run("absent library-root author guides are written", func(t *testing.T) {
		root := refreshLibrary(t)
		for _, rel := range []string{agentGuideFullGuideName, agentsGuideName} {
			if err := os.Remove(refreshAbs(root, rel)); err != nil {
				t.Fatalf("remove %s: %v", rel, err)
			}
		}

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, refreshAbs(root, agentGuideFullGuideName)), mustReadLibraryFullGuide(); !bytes.Equal(got, want) {
			t.Errorf("written %s does not equal the embedded library full guide", agentGuideFullGuideName)
		}
		if got, want := refreshRead(t, refreshAbs(root, agentsGuideName)), mustReadLibraryGuide(); !bytes.Equal(got, want) {
			t.Errorf("written %s does not equal the embedded library pointer", agentsGuideName)
		}
		for _, rel := range []string{agentGuideFullGuideName, agentsGuideName} {
			if !refreshIn(report.refreshed, rel) {
				t.Errorf("report.refreshed = %v, want it to name the written %s", report.refreshed, rel)
			}
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
// requirements.constraint.upgrade-owned-scaffold-scope): the seed-deck
// predicate needs BOTH no non-empty `templates:` key AND a local templates/
// directory, so a deck whose kalide.yaml sets a non-empty `templates:` is never
// a seed deck even when a local templates/ directory exists;
// refreshScaffold creates no local templates/ and no starter slide for such a
// deck, and a local templates/ it does carry is author content — a library
// layer loaded alongside the external library (layered-template-library) — left
// byte-for-byte and mtime-untouched, even when it holds files named exactly
// like the no-arg seed's owned paths, never refreshed and never deleted.
func TestRefreshScaffoldExternalDeck(t *testing.T) {
	externalConfig := "title: External\ntemplates: ../shared-lib\n"

	t.Run("the seed-deck predicate scopes the owned set", func(t *testing.T) {
		// Two decks differ only in their `templates:` key; both carry a local
		// templates/ holding the seed's own templates/library.yaml path. With no
		// `templates:` key the deck IS a no-arg seed deck, so that path is in
		// its owned set and is named in the report; with a non-empty
		// `templates:` key it is an external-library deck and the same path is
		// author content, never named and never touched.
		localLibraryRel := deckLibraryFilePath
		localLibrary := []byte("templates: []\n")

		seedRoot := t.TempDir()
		refreshWrite(t, filepath.Join(seedRoot, deck.ConfigFile), []byte("title: Mine\n"))
		refreshWrite(t, refreshAbs(seedRoot, localLibraryRel), localLibrary)
		refreshPin(t, refreshAbs(seedRoot, localLibraryRel))
		seedCfg, err := refreshDeckConfig(seedRoot, nil)
		if err != nil {
			t.Fatalf("refreshDeckConfig(%s) error = %v", seedRoot, err)
		}
		if !detectSeedDeck(seedRoot, seedCfg) {
			t.Error("detectSeedDeck(no-arg deck with a local templates/) = false, want true")
		}
		seedReport, err := refreshScaffold(seedRoot, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold(%s) error = %v", seedRoot, err)
		}
		if !refreshIn(seedReport.refreshed, localLibraryRel) && !refreshIn(seedReport.skipped, localLibraryRel) {
			t.Errorf("report = %v, want a no-arg seed deck's owned %s named", refreshReportSet(seedReport), localLibraryRel)
		}

		extRoot := t.TempDir()
		refreshWrite(t, filepath.Join(extRoot, deck.ConfigFile), []byte(externalConfig))
		refreshWrite(t, refreshAbs(extRoot, localLibraryRel), localLibrary)
		refreshPin(t, refreshAbs(extRoot, localLibraryRel))
		extCfg, err := refreshDeckConfig(extRoot, nil)
		if err != nil {
			t.Fatalf("refreshDeckConfig(%s) error = %v", extRoot, err)
		}
		if detectSeedDeck(extRoot, extCfg) {
			t.Error("detectSeedDeck(external-library deck with a local templates/) = true, want false")
		}
		extReport, err := refreshScaffold(extRoot, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold(%s) error = %v", extRoot, err)
		}
		refreshAssertUnchanged(t, refreshAbs(extRoot, localLibraryRel), localLibrary)
		if refreshIn(extReport.refreshed, localLibraryRel) || refreshIn(extReport.skipped, localLibraryRel) {
			t.Errorf("report = %v, want an external-library deck's author %s never named",
				refreshReportSet(extReport), localLibraryRel)
		}
	})

	t.Run("no local templates and no starter slide are created", func(t *testing.T) {
		root := t.TempDir()
		refreshWrite(t, filepath.Join(root, deck.ConfigFile), []byte(externalConfig))

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}

		refreshAssertAbsent(t, filepath.Join(root, template.TemplatesDir))
		refreshAssertAbsent(t, refreshAbs(root, starterSlidePath))
		// The refreshed deck is exactly kalide.yaml plus the author-guide pair
		// it wrote: refreshScaffold created no local templates/ and no starter
		// slide.
		if got, want := listTree(t, root), []string{agentsGuideName, agentGuideFullGuideName, deck.ConfigFile}; !slices.Equal(got, want) {
			t.Errorf("deck tree after refresh = %v, want %v (no local templates/, no starter slide)", got, want)
		}

		// The external-library deck owns its deck-root author-guide pair both
		// halves of which were absent and so were written.
		if got := refreshRead(t, refreshAbs(root, agentsGuideName)); !bytes.Equal(got, mustReadSeed(agentsGuideName)) {
			t.Errorf("written %s does not equal the embedded deck pointer", agentsGuideName)
		}
		if got := refreshRead(t, refreshAbs(root, agentGuideFullGuideName)); !bytes.Equal(got, mustReadSeed(agentGuideFullGuideName)) {
			t.Errorf("written %s does not equal the embedded deck full guide", agentGuideFullGuideName)
		}
		refreshAssertReport(t, report, []string{agentsGuideName, agentGuideFullGuideName})
	})

	t.Run("a local templates layer with seed names is author content and never refreshed", func(t *testing.T) {
		root := t.TempDir()
		refreshWrite(t, filepath.Join(root, deck.ConfigFile), []byte(externalConfig))

		// The local templates/ of an external-library deck is a library layer
		// loaded alongside the external library (layered-template-library),
		// author content outside upgrade's scoped refresh set. Populate it with
		// files whose names ARE the no-arg seed's owned paths — the seed
		// templates/library.yaml, the hello template's three files, the seed
		// default theme and the deck-root starter slide — each edited to an
		// OLDER released version, so a wrong seed-deck classification would
		// rewrite them (catalogLookup would match the injected digests, and the
		// pinned mtime would move)
		// (requirements.constraint.upgrade-owned-scaffold-scope).
		oldLibrary := []byte("# an older released seed library.yaml\ntemplates: []\n")
		oldHello := []byte("name: hello\nfields: []\n")
		oldLayout := []byte("{{/* an older released hello layout */}}<section></section>\n")
		oldExample := []byte("# an older released hello example\n")
		oldStarter := []byte("# an older released starter slide\n")
		refreshSetCatalog(t, refreshCatalogFixture{
			deckLibraryFileIdentity:   {oldLibrary},
			deckHelloManifestIdentity: {oldHello},
			deckHelloLayoutIdentity:   {oldLayout},
			deckHelloExampleIdentity:  {oldExample},
			deckStarterSlideIdentity:  {oldStarter},
		})

		// The seed default theme is written at the running binary's OWN bytes
		// (the case a seed deck would treat as already-current): a wrong
		// classification would still name it in the report, which the check
		// below forbids.
		themeRel := refreshDeckThemePath
		type seedNamedFile struct {
			rel  string
			data []byte
		}
		seedNamed := []seedNamedFile{
			{deckLibraryFilePath, oldLibrary},
			{helloSlideDirPath + "/" + template.ManifestFile, oldHello},
			{helloSlideDirPath + "/" + template.LayoutFile, oldLayout},
			{helloSlideDirPath + "/" + template.ExampleFile, oldExample},
			{themeRel, mustReadSeed(themeRel)},
			{starterSlidePath, oldStarter},
		}
		for _, f := range seedNamed {
			p := refreshAbs(root, f.rel)
			refreshWrite(t, p, f.data)
			refreshPin(t, p)
		}
		authorRel := template.TemplatesDir + "/notes.txt"
		author := []byte("leftover local template notes\n")
		refreshWrite(t, refreshAbs(root, authorRel), author)
		refreshPin(t, refreshAbs(root, authorRel))

		// Fingerprint the whole local templates/ tree before the refresh: it
		// must be byte- and mtime-identical afterwards — nothing refreshed,
		// overwritten, created or deleted.
		before := refreshFingerprint(t, filepath.Join(root, template.TemplatesDir))

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}

		refreshAssertTreeUntouched(t, filepath.Join(root, template.TemplatesDir), before)
		for _, f := range seedNamed {
			refreshAssertUnchanged(t, refreshAbs(root, f.rel), f.data)
		}
		refreshAssertUnchanged(t, refreshAbs(root, authorRel), author)
		for _, f := range seedNamed {
			if refreshIn(report.refreshed, f.rel) || refreshIn(report.skipped, f.rel) {
				t.Errorf("report names %s; an external-library deck's local templates/ layer is author content", f.rel)
			}
		}
		if refreshIn(report.refreshed, authorRel) || refreshIn(report.skipped, authorRel) {
			t.Errorf("report names %s; an external-library deck's local templates/ layer is author content", authorRel)
		}
		refreshAssertReport(t, report, []string{agentsGuideName, agentGuideFullGuideName})
	})
}

// TestRefreshScaffoldCrossIdentity covers catalogLookup's identity scoping
// through the refresh engine
// (global.constraint.upgrade-known-version-catalog): a file whose bytes match a
// DIFFERENT owned file's catalogued digest is skipped, not refreshed. The seed
// and library default theme.css legitimately share identical bytes, so that
// shared digest is catalogued under BOTH theme identities; the split author
// guides — the deck/library AGENTS_KALIDE.md full guides (distinct digests) and
// the shared AGENTS.md pointer bytes — are catalogued under their own guide
// identities only, and are the true cross-identity cases.
func TestRefreshScaffoldCrossIdentity(t *testing.T) {
	t.Run("the deck full-guide bytes do not refresh the library theme", func(t *testing.T) {
		root := refreshLibrary(t)
		theme := refreshAbs(root, themeCSSPath)
		foreign := mustReadSeed(agentGuideFullGuideName)
		if bytes.Equal(foreign, []byte(DefaultThemeCSS())) {
			t.Fatal("fixture degenerate: the deck full-guide bytes equal the seeded default theme")
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
				report.refreshed, agentGuideFullGuideName, themeCSSPath)
		}
	})

	t.Run("the library full-guide bytes do not refresh the deck theme", func(t *testing.T) {
		root := refreshSeedDeck(t)
		theme := refreshAbs(root, refreshDeckThemePath)
		foreign := mustReadLibraryFullGuide()
		refreshWrite(t, theme, foreign)
		refreshPin(t, theme)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, theme, foreign)
		if refreshIn(report.refreshed, refreshDeckThemePath) {
			t.Errorf("report.refreshed = %v; bytes catalogued for library:%s must not refresh deck:%s",
				report.refreshed, agentGuideFullGuideName, refreshDeckThemePath)
		}
	})

	t.Run("the AGENTS.md pointer bytes do not refresh a theme", func(t *testing.T) {
		// The deck and library pointer bytes are identical (the same fixed
		// reference file) and so share one digest, but that digest is
		// catalogued under the AGENTS.md identities, never a theme identity: a
		// theme holding the pointer bytes is skipped, not refreshed.
		root := refreshLibrary(t)
		theme := refreshAbs(root, themeCSSPath)
		foreign := mustReadSeed(agentsGuideName)
		if bytes.Equal(foreign, []byte(DefaultThemeCSS())) {
			t.Fatal("fixture degenerate: the AGENTS.md pointer bytes equal the seeded default theme")
		}
		refreshWrite(t, theme, foreign)
		refreshPin(t, theme)

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, theme, foreign)
		if refreshIn(report.refreshed, themeCSSPath) {
			t.Errorf("report.refreshed = %v; bytes catalogued for %s must not refresh library:%s",
				report.refreshed, agentsGuideName, themeCSSPath)
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
			agentsGuideName:         mustReadLibraryGuide(),
			agentGuideFullGuideName: mustReadLibraryFullGuide(),
			themeCSSPath:            []byte(DefaultThemeCSS()),
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

// refreshFileState is one path's kind, bytes and modification time in a tree
// fingerprint.
type refreshFileState struct {
	dir   bool
	bytes string
	mtime time.Time
}

// refreshFingerprint maps every path under root — slash-separated and relative
// to root, directories included — to its content and modification time.
// Comparing two fingerprints detects any write, touch, creation or deletion
// under root, not merely changed bytes: a rewrite that happened to write
// identical bytes would still move the mtime
// (global.constraint.upgrade-never-clobbers).
func refreshFingerprint(t *testing.T, root string) map[string]refreshFileState {
	t.Helper()

	fingerprint := make(map[string]refreshFileState)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		state := refreshFileState{dir: d.IsDir(), mtime: info.ModTime()}
		if !d.IsDir() {
			data, readErr := os.ReadFile(p)
			if readErr != nil {
				return readErr
			}
			state.bytes = string(data)
		}
		fingerprint[filepath.ToSlash(rel)] = state
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprint %s: %v", root, err)
	}
	return fingerprint
}

// refreshAssertTreeUntouched asserts every path in before is still present
// under root with the same kind, bytes and mtime, and that no path was added:
// the whole tree is byte-for-byte and mtime-identical. It is the strongest
// "nothing was read-modified-written, refreshed or deleted" observation
// (global.constraint.upgrade-never-clobbers).
func refreshAssertTreeUntouched(t *testing.T, root string, before map[string]refreshFileState) {
	t.Helper()

	after := refreshFingerprint(t, root)
	for rel, want := range before {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		got, ok := after[rel]
		if !ok {
			t.Errorf("%s was removed, want it untouched", abs)
			continue
		}
		if got.dir != want.dir || got.bytes != want.bytes {
			t.Errorf("%s changed, want it byte-for-byte untouched", abs)
		}
		if !got.mtime.Equal(want.mtime) {
			t.Errorf("%s mtime = %v, want the untouched %v; the path was rewritten", abs, got.mtime, want.mtime)
		}
	}
	for rel := range after {
		if _, ok := before[rel]; !ok {
			t.Errorf("%s was created, want nothing written under the tree", filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
}

// refreshLoadDeck loads a scaffolded deck through the real loaders and fails the
// test on any error: theme.LoadDir builds the registry over the resolved
// template library, deck.LoadConfig validates kalide.yaml against it, and
// template.LoadDeckLibrary validates the deck's template root. libraryRoot is
// the resolved library directory and configured is the raw `templates:` value
// ("" for a no-arg seed deck's local templates/).
func refreshLoadDeck(t *testing.T, deckRoot, libraryRoot, configured string) {
	t.Helper()

	themes, err := theme.LoadDir(os.DirFS(libraryRoot), template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir(%s) error = %v, want the refreshed deck's themes to load", libraryRoot, err)
	}
	if _, err := deck.LoadConfig(os.DirFS(deckRoot), deck.ConfigFile, themes); err != nil {
		t.Fatalf("deck.LoadConfig(%s) error = %v, want the refreshed deck to load", deckRoot, err)
	}
	if _, err := template.LoadDeckLibrary(deckRoot, configured); err != nil {
		t.Fatalf("template.LoadDeckLibrary(%s, %q) error = %v, want the refreshed template root to load",
			deckRoot, configured, err)
	}
}

// refreshLoadLibrary loads a refreshed template library through the real loader
// and fails the test on any error. A library is a template root itself, so it is
// loaded as its own single root through template.LoadLibraryAt: the library
// directory becomes its own fs.FS root, and a nested <dir>/templates/ inside it
// is an ordinary top-level entry, never merged as a second local layer. The
// full templates-dir-validation checks still run over that one root.
func refreshLoadLibrary(t *testing.T, libraryRoot string) {
	t.Helper()

	if _, err := template.LoadLibraryAt(libraryRoot); err != nil {
		t.Fatalf("template.LoadLibraryAt(%s) error = %v, want the refreshed library to load", libraryRoot, err)
	}
}

// TestRefreshScaffold is the integration aggregate for the refresh half
// (upgrade/plan.phase-02.task-6). It builds real projects on disk with
// scaffold.Init (a no-arg seed deck), scaffold.InitExternal (an external-library
// deck) and scaffold.InitLibrary (a library), runs detectProject +
// refreshScaffold over each, and re-validates the upgraded result through the
// real loaders — template.LoadDeckLibrary for the template root and
// deck.LoadConfig for the deck. It covers the never-clobber guarantees end to
// end (global.constraint.upgrade-never-clobbers,
// global.constraint.upgrade-never-breaks-project, agent-authoring,
// library-agent-guide, external-template-library) plus the required
// external-library, idempotence and old-catalogued-guide cases.
//
// The test proves the aggregate behaviour the unit cases decompose: author
// content is byte-for-byte identical, a deleted starter file is not re-created,
// an external-library deck gets no local templates/ or starter slide and leaves
// its configured library byte- and mtime-untouched, an old catalogued AGENTS.md
// (deck and library) is migrated to the current pointer while the current
// AGENTS_KALIDE.md full guide is written, a second run is a no-op, and the
// project still loads cleanly. It uses the real filesystem under a t.TempDir
// only: no server, no network and no git, so it is int tier and guarded with
// testing.Short.
func TestRefreshScaffold(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: builds real projects on disk and re-validates them through the loaders")
	}

	t.Run("a seed deck preserves author content, does not re-create a deleted starter, and still loads", func(t *testing.T) {
		root := refreshSeedDeck(t)

		// Author content: an author-edited kalide-owned member (the default
		// theme) that matches no catalogued released version, plus a slide the
		// author added. Both must survive byte-for-byte, and the theme is a
		// present member the engine must skip rather than refresh.
		authorTheme := []byte("/* my own theme */\n:root { --accent: rebeccapurple; }\n")
		refreshWrite(t, refreshAbs(root, refreshDeckThemePath), authorTheme)
		refreshPin(t, refreshAbs(root, refreshDeckThemePath))

		authorSlideRel := deck.SlidesDir + "/2-author.md"
		authorSlide := []byte("---\ntemplate: hello\ntitle: My own slide\n---\nMy own words.\n")
		refreshWrite(t, refreshAbs(root, authorSlideRel), authorSlide)
		refreshPin(t, refreshAbs(root, authorSlideRel))

		// A starter file the author deleted must not be re-created.
		starter := refreshAbs(root, starterSlidePath)
		if err := os.Remove(starter); err != nil {
			t.Fatalf("remove %s: %v", starter, err)
		}

		kind, err := detectProject(root)
		if err != nil {
			t.Fatalf("detectProject(%s) error = %v, want a deck", root, err)
		}
		if kind != projectKindDeck {
			t.Fatalf("detectProject(%s) kind = %v, want projectKindDeck", root, kind)
		}
		report, err := refreshScaffold(root, kind, nil)
		if err != nil {
			t.Fatalf("refreshScaffold(%s) error = %v", root, err)
		}

		refreshAssertUnchanged(t, refreshAbs(root, refreshDeckThemePath), authorTheme)
		refreshAssertUnchanged(t, refreshAbs(root, authorSlideRel), authorSlide)
		refreshAssertAbsent(t, starter)
		if len(report.refreshed) != 0 {
			t.Errorf("report.refreshed = %v, want no write for a deck whose only non-current members are author content", report.refreshed)
		}
		if !refreshIn(report.skipped, starterSlidePath) {
			t.Errorf("report.skipped = %v, want it to name the deleted starter slide %s", report.skipped, starterSlidePath)
		}
		if !refreshIn(report.skipped, refreshDeckThemePath) {
			t.Errorf("report.skipped = %v, want it to name the author-edited %s", report.skipped, refreshDeckThemePath)
		}
		refreshAssertReport(t, report, refreshMemberPathSet(deckOwnedMembers))

		// The upgraded deck still loads cleanly through the real loaders.
		refreshLoadDeck(t, root, refreshAbs(root, template.TemplatesDir), "")
	})

	t.Run("a library preserves author content and still loads", func(t *testing.T) {
		root := refreshLibrary(t)

		// Author-edited seeded theme: present and matching no catalogued
		// released version, so it must be skipped, never refreshed.
		authorTheme := []byte("/* my own library theme */\n:root { --accent: teal; }\n")
		refreshWrite(t, refreshAbs(root, themeCSSPath), authorTheme)
		refreshPin(t, refreshAbs(root, themeCSSPath))

		kind, err := detectProject(root)
		if err != nil {
			t.Fatalf("detectProject(%s) error = %v, want a library", root, err)
		}
		if kind != projectKindLibrary {
			t.Fatalf("detectProject(%s) kind = %v, want projectKindLibrary", root, kind)
		}
		report, err := refreshScaffold(root, kind, nil)
		if err != nil {
			t.Fatalf("refreshScaffold(%s) error = %v", root, err)
		}

		refreshAssertUnchanged(t, refreshAbs(root, themeCSSPath), authorTheme)
		if len(report.refreshed) != 0 {
			t.Errorf("report.refreshed = %v, want no write for a library whose only non-current member is author content", report.refreshed)
		}
		if !refreshIn(report.skipped, themeCSSPath) {
			t.Errorf("report.skipped = %v, want it to name the author-edited %s", report.skipped, themeCSSPath)
		}
		refreshAssertReport(t, report, refreshMemberPathSet(libraryOwnedMembers))

		// The upgraded library still loads cleanly through the real loader.
		refreshLoadLibrary(t, root)
	})

	t.Run("an external-library deck leaves its configured library untouched and still loads", func(t *testing.T) {
		parent := t.TempDir()
		libraryRoot := filepath.Join(parent, "shared-lib")
		if err := InitLibrary(libraryRoot); err != nil {
			t.Fatalf("InitLibrary(%s) error = %v, want a fresh library", libraryRoot, err)
		}
		deckRoot := filepath.Join(parent, "deck")
		// The `kalide init <path>` form: a real external-library deck.
		if err := Init(deckRoot, "../shared-lib"); err != nil {
			t.Fatalf("Init(%s, ../shared-lib) error = %v, want a fresh external-library deck", deckRoot, err)
		}

		// Fingerprint the configured library before the refresh: nothing under
		// it may change, be added or be deleted.
		before := refreshFingerprint(t, libraryRoot)

		kind, err := detectProject(deckRoot)
		if err != nil {
			t.Fatalf("detectProject(%s) error = %v, want a deck", deckRoot, err)
		}
		if kind != projectKindDeck {
			t.Fatalf("detectProject(%s) kind = %v, want projectKindDeck", deckRoot, kind)
		}
		report, err := refreshScaffold(deckRoot, kind, nil)
		if err != nil {
			t.Fatalf("refreshScaffold(%s) error = %v", deckRoot, err)
		}

		// The configured `templates:` library is byte- and mtime-untouched.
		refreshAssertTreeUntouched(t, libraryRoot, before)

		// No local templates/ and no starter slide for an external-library deck.
		refreshAssertAbsent(t, filepath.Join(deckRoot, template.TemplatesDir))
		refreshAssertAbsent(t, refreshAbs(deckRoot, starterSlidePath))

		// The two halves of the deck-root author-guide pair are the only owned
		// members and were already current.
		refreshAssertReport(t, report, []string{agentsGuideName, agentGuideFullGuideName})
		if len(report.refreshed) != 0 {
			t.Errorf("report.refreshed = %v, want no write for an already-current external-library deck", report.refreshed)
		}

		// The upgraded deck still loads cleanly through the real loaders.
		refreshLoadDeck(t, deckRoot, libraryRoot, "../shared-lib")
	})

	t.Run("an old catalogued deck guide is migrated to the pointer and the full guide written", func(t *testing.T) {
		root := refreshSeedDeck(t)
		guide := refreshAbs(root, agentsGuideName)
		full := refreshAbs(root, agentGuideFullGuideName)
		// The migration scenario: the AGENTS.md holds a released full guide AND
		// no AGENTS_KALIDE.md exists, so the refresh run both migrates AGENTS.md
		// to the short pointer and writes the current full guide.
		if err := os.Remove(full); err != nil {
			t.Fatalf("remove %s: %v", agentGuideFullGuideName, err)
		}
		old := []byte("# an older released kalide deck guide\n\nOld guidance.\n")
		refreshWrite(t, guide, old)
		// The embedded catalogue holds digests only, so the seam presents the
		// old released guide's digest for the real deck-guide identity.
		refreshSetCatalog(t, refreshCatalogFixture{deckGuideIdentity: {old}})

		kind, err := detectProject(root)
		if err != nil {
			t.Fatalf("detectProject(%s) error = %v, want a deck", root, err)
		}
		report, err := refreshScaffold(root, kind, nil)
		if err != nil {
			t.Fatalf("refreshScaffold(%s) error = %v", root, err)
		}

		if got, want := refreshRead(t, guide), mustReadSeed(agentsGuideName); !bytes.Equal(got, want) {
			t.Errorf("old catalogued deck guide was not migrated to the current pointer")
		}
		if got, want := refreshRead(t, full), mustReadSeed(agentGuideFullGuideName); !bytes.Equal(got, want) {
			t.Errorf("the current %s full guide was not written", agentGuideFullGuideName)
		}
		for _, rel := range []string{agentsGuideName, agentGuideFullGuideName} {
			if !refreshIn(report.refreshed, rel) {
				t.Errorf("report.refreshed = %v, want it to name the refreshed %s", report.refreshed, rel)
			}
		}
	})

	t.Run("an old catalogued library guide is migrated to the pointer and the full guide written", func(t *testing.T) {
		root := refreshLibrary(t)
		guide := refreshAbs(root, agentsGuideName)
		full := refreshAbs(root, agentGuideFullGuideName)
		if err := os.Remove(full); err != nil {
			t.Fatalf("remove %s: %v", agentGuideFullGuideName, err)
		}
		old := []byte("# an older released kalide library guide\n\nOld guidance.\n")
		refreshWrite(t, guide, old)
		refreshSetCatalog(t, refreshCatalogFixture{libraryGuideIdentity: {old}})

		kind, err := detectProject(root)
		if err != nil {
			t.Fatalf("detectProject(%s) error = %v, want a library", root, err)
		}
		report, err := refreshScaffold(root, kind, nil)
		if err != nil {
			t.Fatalf("refreshScaffold(%s) error = %v", root, err)
		}

		if got, want := refreshRead(t, guide), mustReadLibraryGuide(); !bytes.Equal(got, want) {
			t.Errorf("old catalogued library guide was not migrated to the current pointer")
		}
		if got, want := refreshRead(t, full), mustReadLibraryFullGuide(); !bytes.Equal(got, want) {
			t.Errorf("the current %s full guide was not written", agentGuideFullGuideName)
		}
		for _, rel := range []string{agentsGuideName, agentGuideFullGuideName} {
			if !refreshIn(report.refreshed, rel) {
				t.Errorf("report.refreshed = %v, want it to name the refreshed %s", report.refreshed, rel)
			}
		}
	})

	t.Run("a second refresh is a no-op", func(t *testing.T) {
		t.Run("deck", func(t *testing.T) {
			root := refreshSeedDeck(t)
			guide := refreshAbs(root, agentsGuideName)
			old := []byte("# an older released kalide deck guide\n")
			refreshWrite(t, guide, old)
			refreshSetCatalog(t, refreshCatalogFixture{deckGuideIdentity: {old}})

			kind, err := detectProject(root)
			if err != nil {
				t.Fatalf("detectProject(%s) error = %v, want a deck", root, err)
			}
			first, err := refreshScaffold(root, kind, nil)
			if err != nil {
				t.Fatalf("first refreshScaffold(%s) error = %v", root, err)
			}
			if !refreshIn(first.refreshed, agentsGuideName) {
				t.Fatalf("first refresh.refreshed = %v, want the old guide refreshed", first.refreshed)
			}

			// Pin every member after the first run: the second run must not
			// rewrite any of them.
			for _, member := range deckOwnedMembers {
				refreshPin(t, refreshAbs(root, member.path))
			}
			second, err := refreshScaffold(root, kind, nil)
			if err != nil {
				t.Fatalf("second refreshScaffold(%s) error = %v", root, err)
			}
			if len(second.refreshed) != 0 {
				t.Errorf("second refresh.refreshed = %v, want a no-op", second.refreshed)
			}
			refreshAssertReport(t, second, refreshMemberPathSet(deckOwnedMembers))
			for _, member := range deckOwnedMembers {
				refreshAssertUnchanged(t, refreshAbs(root, member.path), mustReadSeed(member.path))
			}
		})

		t.Run("library", func(t *testing.T) {
			root := refreshLibrary(t)

			kind, err := detectProject(root)
			if err != nil {
				t.Fatalf("detectProject(%s) error = %v, want a library", root, err)
			}
			if _, err := refreshScaffold(root, kind, nil); err != nil {
				t.Fatalf("first refreshScaffold(%s) error = %v", root, err)
			}

			for _, member := range libraryOwnedMembers {
				refreshPin(t, refreshAbs(root, member.path))
			}
			second, err := refreshScaffold(root, kind, nil)
			if err != nil {
				t.Fatalf("second refreshScaffold(%s) error = %v", root, err)
			}
			if len(second.refreshed) != 0 {
				t.Errorf("second refresh.refreshed = %v, want a no-op", second.refreshed)
			}
			refreshAssertReport(t, second, refreshMemberPathSet(libraryOwnedMembers))
			refreshAssertUnchanged(t, refreshAbs(root, agentsGuideName), mustReadLibraryGuide())
			refreshAssertUnchanged(t, refreshAbs(root, themeCSSPath), []byte(DefaultThemeCSS()))
		})
	})
}

// TestRefreshScaffoldAgentGuidePointer covers the refresh half's
// agent-guide-reference scheme through refreshScaffold
// (requirements.requirement.agent-guide-reference,
// requirements.requirement.agent-authoring,
// global.constraint.upgrade-never-clobbers): the AGENTS.md pointer four-case
// scheme (absent -> created; already carrying the reference line -> untouched;
// an author file without it -> the reference line appended with the author's
// bytes preserved; byte-identical to a known released full guide -> migrated to
// the short pointer) and the AGENTS_KALIDE.md full guide's absent-write
// exception (written only when absent, never refreshed over a present file —
// even one byte-identical to a catalogued released full guide). The cases run
// the real refreshScaffold over a seed deck (deck form) and a template library
// (library form); the released full guide is presented through the
// refreshSetCatalog seam, because the embedded catalogue holds digests only.
func TestRefreshScaffoldAgentGuidePointer(t *testing.T) {
	t.Run("an absent deck pointer is created", func(t *testing.T) {
		root := refreshSeedDeck(t)
		pointer := refreshAbs(root, agentsGuideName)
		if err := os.Remove(pointer); err != nil {
			t.Fatalf("remove %s: %v", agentsGuideName, err)
		}

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, pointer), mustReadSeed(agentsGuideName); !bytes.Equal(got, want) {
			t.Errorf("created %s does not equal the embedded pointer", agentsGuideName)
		}
		if !refreshIn(report.refreshed, agentsGuideName) {
			t.Errorf("report.refreshed = %v, want the created %s", report.refreshed, agentsGuideName)
		}
	})

	t.Run("a deck pointer already carrying the reference line is untouched", func(t *testing.T) {
		root := refreshSeedDeck(t)
		pointer := refreshAbs(root, agentsGuideName)
		author := []byte("# My own guide\n\n" + agentGuideReferenceLine + "\n\nMore of my notes.\n")
		refreshWrite(t, pointer, author)
		refreshPin(t, pointer)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, pointer, author)
		if !refreshIn(report.skipped, agentsGuideName) {
			t.Errorf("report.skipped = %v, want the already-referencing %s", report.skipped, agentsGuideName)
		}
		if refreshIn(report.refreshed, agentsGuideName) {
			t.Errorf("report.refreshed = %v, want the already-referencing %s untouched", report.refreshed, agentsGuideName)
		}
	})

	t.Run("an author deck pointer without the reference line is appended", func(t *testing.T) {
		root := refreshSeedDeck(t)
		pointer := refreshAbs(root, agentsGuideName)
		// No trailing newline: the appended line is separated by a newline.
		author := []byte("# My own guide\n\nMy notes and no trailing newline.")
		refreshWrite(t, pointer, author)
		refreshPin(t, pointer)

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		want := []byte("# My own guide\n\nMy notes and no trailing newline.\n" + agentGuideReferenceLine + "\n")
		if got := refreshRead(t, pointer); !bytes.Equal(got, want) {
			t.Errorf("%s = %q, want the author bytes preserved and the reference line appended, %q", agentsGuideName, got, want)
		}
		if !refreshIn(report.refreshed, agentsGuideName) {
			t.Errorf("report.refreshed = %v, want the appended %s", report.refreshed, agentsGuideName)
		}
	})

	t.Run("a deck pointer that is a known released full guide is migrated", func(t *testing.T) {
		root := refreshSeedDeck(t)
		pointer := refreshAbs(root, agentsGuideName)
		old := []byte("# an older released kalide deck full guide\n\nOld guidance.\n")
		refreshWrite(t, pointer, old)
		refreshSetCatalog(t, refreshCatalogFixture{deckGuideIdentity: {old}})

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, pointer), mustReadSeed(agentsGuideName); !bytes.Equal(got, want) {
			t.Errorf("released full guide was not migrated to the short pointer")
		}
		if !refreshIn(report.refreshed, agentsGuideName) {
			t.Errorf("report.refreshed = %v, want the migrated %s", report.refreshed, agentsGuideName)
		}
	})

	t.Run("an absent deck full guide is written", func(t *testing.T) {
		root := refreshSeedDeck(t)
		full := refreshAbs(root, agentGuideFullGuideName)
		if err := os.Remove(full); err != nil {
			t.Fatalf("remove %s: %v", agentGuideFullGuideName, err)
		}

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, full), mustReadSeed(agentGuideFullGuideName); !bytes.Equal(got, want) {
			t.Errorf("written %s does not equal the embedded deck full guide", agentGuideFullGuideName)
		}
		if !refreshIn(report.refreshed, agentGuideFullGuideName) {
			t.Errorf("report.refreshed = %v, want the written %s", report.refreshed, agentGuideFullGuideName)
		}
	})

	t.Run("a present deck full guide is never refreshed, even when catalogued released", func(t *testing.T) {
		root := refreshSeedDeck(t)
		full := refreshAbs(root, agentGuideFullGuideName)
		author := []byte("# my own kalide guide, hand-edited\n\nDo not touch.\n")
		refreshWrite(t, full, author)
		refreshPin(t, full)
		// Even a byte sequence catalogued as a released version of the full
		// guide identity is left alone: the full guide is the absent-write
		// exception only, never a whole-file refresh member.
		refreshSetCatalog(t, refreshCatalogFixture{deckFullGuideIdentity: {author}})

		report, err := refreshScaffold(root, projectKindDeck, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		refreshAssertUnchanged(t, full, author)
		if !refreshIn(report.skipped, agentGuideFullGuideName) {
			t.Errorf("report.skipped = %v, want the present %s", report.skipped, agentGuideFullGuideName)
		}
		if refreshIn(report.refreshed, agentGuideFullGuideName) {
			t.Errorf("report.refreshed = %v, want the present %s never refreshed", report.refreshed, agentGuideFullGuideName)
		}
	})

	t.Run("a library pointer that is a known released full guide is migrated and the full guide written", func(t *testing.T) {
		root := refreshLibrary(t)
		pointer := refreshAbs(root, agentsGuideName)
		full := refreshAbs(root, agentGuideFullGuideName)
		if err := os.Remove(full); err != nil {
			t.Fatalf("remove %s: %v", agentGuideFullGuideName, err)
		}
		old := []byte("# an older released kalide library full guide\n\nOld guidance.\n")
		refreshWrite(t, pointer, old)
		refreshSetCatalog(t, refreshCatalogFixture{libraryGuideIdentity: {old}})

		report, err := refreshScaffold(root, projectKindLibrary, nil)
		if err != nil {
			t.Fatalf("refreshScaffold error = %v", err)
		}
		if got, want := refreshRead(t, pointer), mustReadLibraryGuide(); !bytes.Equal(got, want) {
			t.Errorf("released library full guide was not migrated to the short pointer")
		}
		if got, want := refreshRead(t, full), mustReadLibraryFullGuide(); !bytes.Equal(got, want) {
			t.Errorf("the current %s full guide was not written", agentGuideFullGuideName)
		}
		for _, rel := range []string{agentsGuideName, agentGuideFullGuideName} {
			if !refreshIn(report.refreshed, rel) {
				t.Errorf("report.refreshed = %v, want it to name the refreshed %s", report.refreshed, rel)
			}
		}
	})
}
