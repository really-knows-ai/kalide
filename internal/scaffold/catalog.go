// This file hosts the embedded known-version scaffold digest catalog
// (global.constraint.upgrade-known-version-catalog): the catalogue of every
// released version of every kalide-owned scaffold file, holding identity /
// version metadata and content digests ONLY — never historical file contents
// and never presentation assets (global.constraint.no-embedded-template-assets).
//
// The catalogue is generated, not hand-maintained at build time:
// scaffoldversions.json (owned by an earlier task in this phase) records, for
// each owned-file identity named by
// requirements.constraint.upgrade-owned-scaffold-scope, the SHA-256 digest of
// each released version of that file. The refresh half (phase 2) uses
// catalogLookup to decide whether a file on disk is a known released version
// of THAT identity — and so may safely be refreshed — rather than author
// content, which is never touched (global.constraint.upgrade-never-clobbers).
//
// The lookup is keyed by owned-file identity, never by digest alone:
// internal/scaffold's own embedded seed makes two owned files share identical
// bytes (the deck's templates/themes/default/theme.css and a library's
// themes/default/theme.css are the same file), so a digest that appears under
// one identity must never match a different identity. Matching across
// identities is the correctness bug this catalogue's keying exists to prevent.
package scaffold

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
)

// catalogEmbed is the embedded known-version digest catalogue, rooted at the
// package directory so that "scaffoldversions.json" is its direct entry.
//
//go:embed scaffoldversions.json
var catalogEmbed embed.FS

// catalogFileName is the catalogue's name within catalogEmbed. It is the same
// file the refresh half and the drift self-test read, and it is the single
// source of truth for the catalogue's contents.
const catalogFileName = "scaffoldversions.json"

// scaffoldCatalog is the catalog data model: the decoded scaffoldversions.json,
// keyed by owned-file identity.
//
// The model is deliberately identity-scoped and content-free. Each owned-file
// identity holds only the SHA-256 digests of the versions kalide released for
// that file, and, per digest, the release labels that shipped those bytes — no
// file contents and no presentation assets
// (global.constraint.no-embedded-template-assets).
//
// Keying by identity is what stops a digest shared with a different owned file
// from matching across identities: the embedded seed makes the deck's
// templates/themes/default/theme.css and a library's
// themes/default/theme.css byte-identical, so the same digest legitimately
// appears under two identities and must match only the identity it was
// catalogued under.
type scaffoldCatalog struct {
	// byIdentity maps an owned-file identity (for example "deck:AGENTS.md")
	// to that file's catalogue entry.
	byIdentity map[string]catalogIdentity
}

// catalogIdentity is one owned file's catalogue entry: its released content
// digests, each mapped to the released version labels that shipped those bytes.
//
// One digest entry serves every release whose bytes were unchanged between
// them (a release that did not change the file adds no new digest), so the
// version metadata answers both "is this digest a known release of this file?"
// and "which releases shipped it?".
type catalogIdentity struct {
	// digests maps a lowercase-hex SHA-256 content digest to the released
	// version labels that shipped bytes with that digest, in catalogue order
	// and deduplicated.
	digests map[string][]string
}

// identities returns every owned-file identity in the catalogue, sorted for
// deterministic iteration.
//
// It is the identity-set accessor the drift self-test consumes to assert the
// catalogue covers exactly the requirements.constraint.upgrade-owned-scaffold-
// scope set — no extra identity (notably not kalide.yaml) and none missing.
func (c scaffoldCatalog) identities() []string {
	names := make([]string, 0, len(c.byIdentity))
	for name := range c.byIdentity {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// digestsForIdentity returns every lowercase-hex SHA-256 digest catalogued for
// identity, sorted for deterministic iteration, or nil when identity is
// unknown.
//
// The drift self-test consumes it to assert a currently shipped file's digest
// is catalogued under its own identity. The refresh half's lookup consumes it
// (directly or through catalogLookup) to resolve bytes against one identity
// only, never against a digest alone: a digest that appears under a different
// identity is not a match here.
func (c scaffoldCatalog) digestsForIdentity(identity string) []string {
	entry, ok := c.byIdentity[identity]
	if !ok {
		return nil
	}
	digests := make([]string, 0, len(entry.digests))
	for digest := range entry.digests {
		digests = append(digests, digest)
	}
	slices.Sort(digests)
	return digests
}

// versionsForDigest returns the released version labels that shipped digest for
// the owned file identified by identity, in catalogue (release) order, or nil
// when identity is unknown or carries no such digest.
//
// It is the version-metadata accessor: a digest shared with a different
// identity resolves to that identity's releases only, never another file's.
// The returned slice is a copy, so callers cannot mutate the decoded catalogue.
func (c scaffoldCatalog) versionsForDigest(identity, digest string) []string {
	entry, ok := c.byIdentity[identity]
	if !ok {
		return nil
	}
	versions, ok := entry.digests[digest]
	if !ok {
		return nil
	}
	return slices.Clone(versions)
}

// catalogFile, catalogFileIdentity and catalogDigest mirror the on-disk JSON
// schema exactly:
//
//	{ "identities": [ { "identity": string,
//	                    "digests": [ { "digest": string,
//	                                   "versions": [string] } ] } ] }
//
// These are unexported, decode-only shapes: nothing outside this file depends
// on the wire format, so the internal model can change without touching the
// generated catalogue.
type catalogFile struct {
	Identities []catalogFileIdentity `json:"identities"`
}

type catalogFileIdentity struct {
	Identity string          `json:"identity"`
	Digests  []catalogDigest `json:"digests"`
}

type catalogDigest struct {
	Digest   string   `json:"digest"`
	Versions []string `json:"versions"`
}

// knownCatalog is the scaffoldversions.json catalogue, decoded once at package
// load. The embed directive above and the generated file are the source of
// truth, so a decode failure is a compiled-in programming error, not a runtime
// condition: it panics at startup rather than surfacing later as a confusing
// lookup miss.
var knownCatalog = mustLoadCatalog()

// mustLoadCatalog decodes the embedded catalogue, panicking on any failure.
// It mirrors the package's other embedded-content self-tests
// (helloseed.go's init), which treat an invalid embedded asset as a
// programming error caught at load.
func mustLoadCatalog() scaffoldCatalog {
	catalog, err := parseCatalog()
	if err != nil {
		panic("scaffold: embedded known-version catalogue is invalid: " + err.Error())
	}
	return catalog
}

// parseCatalog reads and decodes scaffoldversions.json into the catalog data
// model. It holds the whole file to the declared schema only: identity /
// digest / version fields, nothing else. It is the parsing seam the catalog
// tests exercise without touching the embedded bytes.
func parseCatalog() (scaffoldCatalog, error) {
	data, err := catalogEmbed.ReadFile(catalogFileName)
	if err != nil {
		return scaffoldCatalog{}, fmt.Errorf("read embedded %s: %w", catalogFileName, err)
	}

	var raw catalogFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return scaffoldCatalog{}, fmt.Errorf("parse %s: %w", catalogFileName, err)
	}

	catalog := scaffoldCatalog{byIdentity: make(map[string]catalogIdentity, len(raw.Identities))}
	for _, identity := range raw.Identities {
		if identity.Identity == "" {
			return scaffoldCatalog{}, fmt.Errorf("parse %s: identity entry with empty identity", catalogFileName)
		}
		if _, dup := catalog.byIdentity[identity.Identity]; dup {
			return scaffoldCatalog{}, fmt.Errorf("parse %s: duplicate identity %q", catalogFileName, identity.Identity)
		}
		entry := catalogIdentity{digests: make(map[string][]string, len(identity.Digests))}
		for _, digest := range identity.Digests {
			if digest.Digest == "" {
				return scaffoldCatalog{}, fmt.Errorf("parse %s: identity %q has an empty digest", catalogFileName, identity.Identity)
			}
			if _, dup := entry.digests[digest.Digest]; dup {
				return scaffoldCatalog{}, fmt.Errorf("parse %s: identity %q repeats digest %q", catalogFileName, identity.Identity, digest.Digest)
			}
			entry.digests[digest.Digest] = dedupeVersions(digest.Versions)
		}
		catalog.byIdentity[identity.Identity] = entry
	}
	return catalog, nil
}

// dedupeVersions returns labels with duplicates removed, preserving first-seen
// (catalogue/release) order. The catalogue records, per digest, only the
// releases that shipped those bytes — one digest entry serves every release
// whose bytes were unchanged — so a repeated label is a data error; collapsing
// it here keeps the version-metadata accessors' answers unique.
func dedupeVersions(labels []string) []string {
	if len(labels) == 0 {
		return labels
	}
	seen := make(map[string]struct{}, len(labels))
	unique := make([]string, 0, len(labels))
	for _, label := range labels {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		unique = append(unique, label)
	}
	return unique
}

// catalogLookup reports whether data is the exact bytes of a catalogued,
// released version of the kalide-owned scaffold file identified by identity.
//
// It hashes data with SHA-256 (lowercase hex — the algorithm the catalogue,
// the drift self-test and the refresh half all share) and asks whether THAT
// identity's entry carries the digest. An unknown identity, or bytes matching
// no catalogued version of the named identity, reports false: the refresh half
// then leaves the file alone, so author content and every uncatalogued byte
// sequence are never rewritten (global.constraint.upgrade-never-clobbers).
//
// The lookup key is the identity, never the digest alone: the seed makes the
// deck's templates/themes/default/theme.css and a library's
// themes/default/theme.css byte-identical, so the same digest legitimately
// appears under two identities and must match only the identity it was
// catalogued under.
//
// TODO(upgrade/plan.phase-01.task-7): task 7 owns the complete lookup API. It
// may widen or reshape this signature (for example to accept an
// already-computed digest, or to also return the matching version metadata that
// task 6 exposes) and is responsible for the identity-scoping tests
// (upgrade/plan.phase-01.task-11). The minimal body here already enforces the
// cross-identity rule so this file compiles and cannot silently match a
// foreign digest.
func catalogLookup(identity string, data []byte) bool {
	entry, ok := knownCatalog.byIdentity[identity]
	if !ok {
		return false
	}
	sum := sha256.Sum256(data)
	_, ok = entry.digests[hex.EncodeToString(sum[:])]
	return ok
}
