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
// TODO(upgrade/plan.phase-01.task-6): task 6 owns the complete data model —
// the identity-keyed digests plus the version metadata each digest shipped in
// (today the versions are parsed but only used to build the digest set; task 6
// may add accessors such as versions-for-digest and the full identity set, and
// may reshape these fields). The field shapes below are the minimal model this
// task's parsing fills and catalogLookup (task 7) reads.
type scaffoldCatalog struct {
	// identities maps an owned-file identity (for example "deck:AGENTS.md")
	// to that file's released digests. Keying by identity is what stops a
	// digest shared with a different owned file from matching across
	// identities.
	identities map[string]catalogIdentity
}

// catalogIdentity is one owned file's catalogue entry: its released content
// digests, each mapped to the released versions that shipped those bytes.
//
// TODO(upgrade/plan.phase-01.task-6): task 6 owns the complete entry model
// (version metadata retrieval, ordering, dedup guarantees).
type catalogIdentity struct {
	// digests maps a lowercase-hex SHA-256 content digest to the released
	// version labels that shipped bytes with that digest. One digest entry
	// serves every release whose bytes were unchanged between them.
	digests map[string][]string
}

// catalogFile, catalogFileIdentity and catalogDigest mirror the on-disk JSON
// schema exactly:
//
//	{ "identities": [ { "identity": string,
//	                    "digests": [ { "digest": string,
//	                                   "versions": [string] } ] } ] }
//
// These are unexported, decode-only shapes: nothing outside this file depends
// on the wire format, so the internal model can change (task 6) without
// touching the generated catalogue.
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

	catalog := scaffoldCatalog{identities: make(map[string]catalogIdentity, len(raw.Identities))}
	for _, identity := range raw.Identities {
		entry := catalogIdentity{digests: make(map[string][]string, len(identity.Digests))}
		for _, digest := range identity.Digests {
			entry.digests[digest.Digest] = digest.Versions
		}
		catalog.identities[identity.Identity] = entry
	}
	return catalog, nil
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
	entry, ok := knownCatalog.identities[identity]
	if !ok {
		return false
	}
	sum := sha256.Sum256(data)
	_, ok = entry.digests[hex.EncodeToString(sum[:])]
	return ok
}
