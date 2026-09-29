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
//     its local templates/.
//   - refreshScaffold applies the never-clobber refresh.
//
// The refresh half is built on the rest of the package's embedded content and
// the catalogue above. It takes the deck's configuration — the caller-loaded
// deck.LoadConfig result, or the raw `templates:` value read with
// deck.TemplatesPath when none is given — and reads and writes only the
// project's own local files. A deck's configured external `templates:` library
// is never opened, so nothing under it is read or written
// (requirements.constraint.upgrade-owned-scaffold-scope); reading only the
// embedded content, the embedded catalogue and the local filesystem, the
// refresh half never reaches the network and never runs git. The project's own
// root resolution and end-to-end validation through template.ResolveLibraryRoot
// and template.LoadDeckLibrary belong to scaffold.Upgrade (phase 4).
package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

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

// deckGuideMember is the deck-root AGENTS.md author guide (agent-authoring):
// the one kalide-owned member every deck form has — a no-arg seed deck and an
// external-library deck alike
// (requirements.constraint.upgrade-owned-scaffold-scope).
var deckGuideMember = ownedMember{identity: deckGuideIdentity, path: agentsGuideName}

// deckOwnedMembers is the kalide-owned set of a no-arg seed deck. It covers the
// whole embedded seed except kalide.yaml, and it includes the starter slide.
var deckOwnedMembers = []ownedMember{
	deckGuideMember,
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

// refreshScaffold is the never-clobber refresh engine. For the detected project
// form it rewrites each kalide-owned member only when the file is present AND
// byte-identical to a catalogued released version of that same identity
// (catalogLookup), writes the two AGENTS.md author guides when absent, and
// never re-creates a deleted seed starter file. Author content, a file whose
// bytes match no catalogued version, and a file already byte-identical to the
// running binary's version of that same file are left byte-for-byte untouched —
// already-current files are a no-op, never re-written. It returns a
// refreshReport naming every refreshed path and every path it left alone
// (requirements.requirement.upgrade-refresh-owned-scaffold,
// global.constraint.upgrade-never-clobbers).
//
// The owned set is scoped to the detected form and, for a deck, to its scaffold
// form: a no-arg seed deck owns the whole embedded seed (deckOwnedMembers), an
// external-library deck — one whose kalide.yaml configures a `templates:`
// library — owns only its deck-root guide, and a template library owns its
// library-root guide and its seeded default theme (libraryOwnedMembers). A
// configured external library is never opened, so nothing under it is read or
// written (requirements.constraint.upgrade-owned-scaffold-scope).
//
// deckRoot is the project root: the directory holding kalide.yaml for a deck,
// or library.yaml for a library. cfg is the deck's already-loaded configuration
// for the deck form (nil for a library); when nil, refreshScaffold derives the
// raw `templates:` value from kalide.yaml itself, without opening the
// configured library.
func refreshScaffold(deckRoot string, kind projectKind, cfg *deck.Config) (*refreshReport, error) {
	report := &refreshReport{}

	switch kind {
	case projectKindLibrary:
		for _, member := range libraryOwnedMembers {
			if err := report.refreshMember(deckRoot, member); err != nil {
				return report, err
			}
		}
		return report, nil

	case projectKindDeck:
		deckCfg, err := refreshDeckConfig(deckRoot, cfg)
		if err != nil {
			return report, err
		}
		members := deckOwnedMembers
		if !detectSeedDeck(deckRoot, deckCfg) {
			// An external-library deck owns only its deck-root guide: its
			// configured library, and any leftover local templates/, are
			// out of scope and left byte-for-byte untouched, and no
			// starter slide is written
			// (requirements.constraint.upgrade-owned-scaffold-scope).
			members = []ownedMember{deckGuideMember}
		}
		for _, member := range members {
			if err := report.refreshMember(deckRoot, member); err != nil {
				return report, err
			}
		}
		return report, nil

	default:
		return report, fmt.Errorf("upgrade: cannot refresh %s: no project form was detected", deckRoot)
	}
}

// refreshDeckConfig returns the deck configuration the refresh engine works
// from: the caller-loaded cfg when given, otherwise a minimal configuration
// carrying only the raw `templates:` value read from kalide.yaml. Reading the
// value directly — instead of loading and validating the whole deck through
// deck.LoadConfig — keeps the refresh engine from opening the configured
// external library: only deck.TemplatesPath is needed to tell a no-arg seed
// deck from an external-library deck (detectSeedDeck), and the full load and
// end-to-end validation belong to scaffold.Upgrade (phase 4).
func refreshDeckConfig(deckRoot string, cfg *deck.Config) (*deck.Config, error) {
	if cfg != nil {
		return cfg, nil
	}
	data, err := os.ReadFile(filepath.Join(deckRoot, deck.ConfigFile))
	if err != nil {
		return nil, fmt.Errorf("upgrade: read %s: %w", deck.ConfigFile, err)
	}
	var raw struct {
		Templates string `yaml:"templates"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("upgrade: parse %s: %w", deck.ConfigFile, err)
	}
	return &deck.Config{Templates: raw.Templates}, nil
}

// refreshMember applies the never-clobber rule to one owned member:
//
//   - absent: a missing AGENTS.md author guide is written from the embedded
//     guide — the one absent-write exception; any other absent member (a
//     deleted seed starter file, or a library's deleted seeded theme) is never
//     re-created and is reported skipped;
//   - present and already the running binary's current bytes: a no-op, left
//     as-is and never re-written, reported skipped;
//   - present and byte-identical to a catalogued released version of its own
//     identity (catalogLookup): rewritten to the current bytes and reported
//     refreshed;
//   - present and matching no catalogued version of its identity (author
//     content, including bytes that match only a DIFFERENT owned file's
//     digest): left byte-for-byte untouched and reported skipped.
func (r *refreshReport) refreshMember(root string, member ownedMember) error {
	target := filepath.Join(root, filepath.FromSlash(member.path))

	data, err := os.ReadFile(target)
	switch {
	case err == nil:
		// Present: fall through to the decision below.
	case errors.Is(err, fs.ErrNotExist):
		if isAuthorGuide(member.identity) {
			if err := writeMember(target, desiredMemberBytes(member)); err != nil {
				return err
			}
			r.markRefreshed(member.path)
			return nil
		}
		r.markSkipped(member.path)
		return nil
	default:
		return fmt.Errorf("upgrade: read %s: %w", member.path, err)
	}

	desired := desiredMemberBytes(member)
	if bytes.Equal(data, desired) {
		// Already the running binary's version of this same file: a no-op,
		// deliberately not re-written (global.constraint.upgrade-never-clobbers).
		r.markSkipped(member.path)
		return nil
	}
	if catalogLookup(member.identity, data) {
		if err := writeMember(target, desired); err != nil {
			return err
		}
		r.markRefreshed(member.path)
		return nil
	}
	r.markSkipped(member.path)
	return nil
}

// writeMember writes the running binary's bytes for an owned member, creating
// the parent directory only when it is missing (the absent-guide case, whose
// parent is the project root).
func writeMember(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("upgrade: create %s: %w", filepath.ToSlash(filepath.Dir(target)), err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return fmt.Errorf("upgrade: write %s: %w", filepath.ToSlash(target), err)
	}
	return nil
}

// isAuthorGuide reports whether identity is one of the two AGENTS.md author
// guides — the only owned members written when absent.
func isAuthorGuide(identity string) bool {
	return identity == deckGuideIdentity || identity == libraryGuideIdentity
}

// desiredMemberBytes returns the running binary's current bytes for an owned
// member: the embedded library guide, the library's seeded default theme
// (DefaultThemeCSS, the bytes InitLibrary writes), or — for every deck seed
// member, whose on-disk path is also its seed path — the embedded seed file at
// the member's own path.
func desiredMemberBytes(member ownedMember) []byte {
	switch member.identity {
	case libraryGuideIdentity:
		return mustReadLibraryGuide()
	case libraryThemeIdentity:
		return []byte(DefaultThemeCSS())
	default:
		return mustReadSeed(member.path)
	}
}
