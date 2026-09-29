// This file is the refresh half of `kalide upgrade`: the part that brings a
// deck's or library's kalide-owned scaffold files forward to the running
// binary's versions while never overwriting author content
// (requirements.requirement.upgrade-refresh-owned-scaffold,
// global.constraint.upgrade-never-clobbers).
//
// The refresh half owns only the files kalide itself wrote, and it identifies
// them by owned-file identity — the same identity the phase-1 known-version
// digest catalogue (catalog.go) is keyed by
// (requirements.constraint.upgrade-owned-scaffold-scope). A member on disk is
// rewritten only when it is present AND byte-identical to a catalogued
// released version of that same identity (catalogLookup); a file the author
// edited, a file kalide never released, and a file whose bytes merely match a
// DIFFERENT owned file's digest are all left byte-for-byte alone. The two
// author guides are the exception: the deck-root guide is written when absent
// from the embedded seed (mustReadSeed, seedRoot) and the library-root guide
// from the embedded library guide (mustReadLibraryGuide, libraryGuideRoot).
//
// Detection and the refresh engine are declared here, in the functions the
// rest of phase 2 fills in:
//
//   - detectProject   classifies a directory as a deck or a library, and is
//     the single owner of deck/library detection and of the both/neither
//     error.
//   - detectSeedDeck  reports whether a deck is the no-arg seed deck that owns
//     its local templates/.        TODO(upgrade/plan.phase-02.task-3)
//   - refreshScaffold applies the never-clobber refresh.
//     TODO(upgrade/plan.phase-02.task-4)
//
// The refresh half is built on the rest of the package's embedded content and
// the catalogue above, plus the deck and template loaders: it reads and
// validates a deck with deck.LoadConfig, takes its raw `templates:` value with
// deck.TemplatesPath, and resolves and loads its library through the loader's
// single root-resolution point, template.ResolveLibraryRoot and
// template.LoadDeckLibrary. It reads only the embedded content, the embedded
// catalogue and the local filesystem: it never reaches the network and never
// runs git.
package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// identityDeckPrefix and identityLibraryPrefix are the two owned-file identity
// namespaces: the catalogue keys a deck member as "deck:<path>" and a library
// member as "library:<path>". The deck and library copies of the default theme
// are byte-identical, so keeping them in separate namespaces — and looking each
// up by its own identity — is what stops one from matching the other's digest.
const (
	identityDeckPrefix    = "deck:"
	identityLibraryPrefix = "library:"
)

// agentsGuideName is the file name of both author guides: a deck's at the
// project root (agent-authoring) and a library's at the library root
// (library-agent-guide). Unlike the other owned members these are written when
// absent rather than only refreshed when present.
const agentsGuideName = "AGENTS.md"

// helloSeedName is the embedded seed's starter template name and
// starterSlideName is the seed's starter slide file name; both are fixed by the
// embedded seed tree in helloseed.go.
const (
	helloSeedName    = "hello"
	starterSlideName = "1-hello.md"
)

// Owned-member paths, relative to the project (deck or library) root and
// written slash-separated, as the catalogue identities and the on-disk paths
// use.
const (
	deckLibraryFilePath = template.TemplatesDir + "/" + template.LibraryFile
	helloSlideDirPath   = template.TemplatesDir + "/" + template.SlidesDir + "/" + helloSeedName
	themeCSSPath        = template.ThemesDir + "/" + theme.DefaultName + "/" + template.ThemeStylesheet
	starterSlidePath    = template.SlidesDir + "/" + starterSlideName
)

// Owned-file identities: exactly the identity set
// requirements.constraint.upgrade-owned-scaffold-scope names, one identity per
// owned member, matching the keys catalogLookup and the generated catalogue
// (scaffoldversions.json) use. A deck's kalide.yaml is deliberately absent: it
// is author-edited deck configuration, not a whole-file refresh member.
const (
	deckGuideIdentity         = identityDeckPrefix + agentsGuideName
	deckLibraryFileIdentity   = identityDeckPrefix + deckLibraryFilePath
	deckHelloExampleIdentity  = identityDeckPrefix + helloSlideDirPath + "/" + template.ExampleFile
	deckHelloLayoutIdentity   = identityDeckPrefix + helloSlideDirPath + "/" + template.LayoutFile
	deckHelloManifestIdentity = identityDeckPrefix + helloSlideDirPath + "/" + template.ManifestFile
	deckThemeIdentity         = identityDeckPrefix + template.TemplatesDir + "/" + themeCSSPath
	deckStarterSlideIdentity  = identityDeckPrefix + starterSlidePath

	libraryGuideIdentity = identityLibraryPrefix + agentsGuideName
	libraryThemeIdentity = identityLibraryPrefix + themeCSSPath
)

// ownedMember is one kalide-owned scaffold file: the identity under which its
// content is catalogued and looked up, together with the slash-separated path
// at which it lives relative to the project root. The two travel together
// because a file is refreshed only against its OWN identity: the path locates
// the bytes on disk, the identity decides whether those bytes may be rewritten
// (catalogLookup).
type ownedMember struct {
	identity string
	path     string
}

// deckOwnedMembers is the kalide-owned set of a no-arg seed deck. It covers the
// whole embedded seed except kalide.yaml, and it includes the starter slide.
var deckOwnedMembers = []ownedMember{
	{identity: deckGuideIdentity, path: agentsGuideName},
	{identity: deckLibraryFileIdentity, path: deckLibraryFilePath},
	{identity: deckHelloExampleIdentity, path: helloSlideDirPath + "/" + template.ExampleFile},
	{identity: deckHelloLayoutIdentity, path: helloSlideDirPath + "/" + template.LayoutFile},
	{identity: deckHelloManifestIdentity, path: helloSlideDirPath + "/" + template.ManifestFile},
	{identity: deckThemeIdentity, path: template.TemplatesDir + "/" + themeCSSPath},
	{identity: deckStarterSlideIdentity, path: starterSlidePath},
}

// libraryOwnedMembers is the kalide-owned set of a template library: the
// library-root guide and the seeded default theme. A library's slides/,
// sections/ and media/ hold author content and are never members.
var libraryOwnedMembers = []ownedMember{
	{identity: libraryGuideIdentity, path: agentsGuideName},
	{identity: libraryThemeIdentity, path: themeCSSPath},
}

// refreshReport accumulates the outcome of a refresh run: every project-root
// path refreshScaffold refreshed and every path it left alone (skipped). It is
// the refresh half's contribution to scaffold.UpgradeReport (phase 4): the
// caller reports both lists to the author, so a skipped author-edited file is
// visible rather than silent (global.constraint.upgrade-never-clobbers).
type refreshReport struct {
	refreshed []string
	skipped   []string
}

// markRefreshed records path as refreshed, preserving the order in which the
// refresh engine visited the files.
func (r *refreshReport) markRefreshed(path string) {
	r.refreshed = append(r.refreshed, path)
}

// markSkipped records path as skipped — left byte-for-byte alone because it was
// absent, already current, or not a known released version of its identity —
// preserving the order in which the refresh engine visited the files.
func (r *refreshReport) markSkipped(path string) {
	r.skipped = append(r.skipped, path)
}

// projectKind is the project form detectProject found in a directory.
type projectKind int

const (
	// projectKindUnknown is the zero value: no project form was detected.
	projectKindUnknown projectKind = iota
	// projectKindDeck is a deck directory, identified by kalide.yaml.
	projectKindDeck
	// projectKindLibrary is a template-library directory, identified by
	// library.yaml.
	projectKindLibrary
)

// detectProject classifies the project in dir as a deck (kalide.yaml present)
// or a template library (library.yaml present). It is THE SINGLE OWNER of
// deck/library detection and of the both/neither error: a directory holding
// both manifests, or neither, is an error that names what was found against
// what was expected, and nothing is written (deck-library-upgrade project
// detection).
//
// Presence is tested with os.Lstat, so a manifest that is a file, a directory
// or a symlink all count as found, and a dangling symlink is not silently
// treated as absent. Any other filesystem failure inspecting a manifest is
// returned rather than reported as "absent".
//
// scaffold.Upgrade (phase 4) MUST call this function for detection and must not
// re-implement it or its both/neither error.
func detectProject(dir string) (projectKind, error) {
	hasDeck, err := manifestPresent(filepath.Join(dir, deck.ConfigFile))
	if err != nil {
		return projectKindUnknown, fmt.Errorf("upgrade: inspect %s: %w", deck.ConfigFile, err)
	}
	hasLibrary, err := manifestPresent(filepath.Join(dir, template.LibraryFile))
	if err != nil {
		return projectKindUnknown, fmt.Errorf("upgrade: inspect %s: %w", template.LibraryFile, err)
	}

	switch {
	case hasDeck && hasLibrary:
		return projectKindUnknown, fmt.Errorf(
			"upgrade: cannot detect a project in %s: found both %s (a deck) and %s (a template library); expected exactly one",
			dir, deck.ConfigFile, template.LibraryFile)
	case hasDeck:
		return projectKindDeck, nil
	case hasLibrary:
		return projectKindLibrary, nil
	default:
		return projectKindUnknown, fmt.Errorf(
			"upgrade: cannot detect a project in %s: found neither %s (a deck) nor %s (a template library); expected exactly one",
			dir, deck.ConfigFile, template.LibraryFile)
	}
}

// manifestPresent reports whether path exists, without following symlinks: a
// regular file, a directory or a symlink (even a dangling one) is present. Only
// fs.ErrNotExist is absence; any other Lstat failure is returned.
func manifestPresent(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

// detectSeedDeck reports whether deckRoot is a no-arg seed deck: a deck whose
// kalide.yaml carries no non-empty `templates:` key (deck.TemplatesPath returns
// "") and that has a local templates/ directory. It is false for an
// external-library deck even when a leftover local templates/ is present, so
// the seed-owned set is applied only to the deck that owns it
// (requirements.constraint.upgrade-owned-scaffold-scope).
//
// A non-empty `templates:` key is authoritative and decisive: it makes the
// predicate false without ever consulting the local templates/ directory, so a
// leftover local tree never turns an external-library deck into a seed deck.
// The directory check mirrors the loader's own resolution of the absent-key
// case (template.ResolveLibraryRoot): os.Stat follows symlinks, and only an
// existing directory counts. A missing local templates/, or one that exists but
// is not a directory, is not a seed deck.
func detectSeedDeck(deckRoot string, cfg *deck.Config) bool {
	if deck.TemplatesPath(cfg) != "" {
		return false
	}
	info, err := os.Stat(filepath.Join(deckRoot, template.TemplatesDir))
	return err == nil && info.IsDir()
}

// refreshScaffold is the never-clobber refresh engine. For the detected form it
// rewrites each kalide-owned member only when the file is present AND
// byte-identical to a catalogued released version of that same identity
// (catalogLookup), writes the two author guides when absent, and never
// re-creates a deleted starter file. It returns a refreshReport naming every
// refreshed path and every skipped path
// (requirements.requirement.upgrade-refresh-owned-scaffold,
// global.constraint.upgrade-never-clobbers).
//
// TODO(upgrade/plan.phase-02.task-4): implement the engine. It consumes
// detectSeedDeck for the deck form, reads the embedded content through
// mustReadSeed and mustReadLibraryGuide, and loads the deck through
// deck.LoadConfig and its library through template.ResolveLibraryRoot and
// template.LoadDeckLibrary.
func refreshScaffold(deckRoot string, kind projectKind, cfg *deck.Config) (*refreshReport, error) {
	return nil, fmt.Errorf("scaffold: refreshScaffold: not implemented yet")
}
