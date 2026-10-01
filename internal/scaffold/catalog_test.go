package scaffold

// This file hosts the unit tests for catalogLookup, the identity-scoped digest
// lookup the refresh half consumes (global.constraint.upgrade-known-version-
// catalog, requirements.requirement.upgrade-refresh-owned-scaffold): given an
// owned-file identity and a byte slice, catalogLookup reports whether those
// bytes are an exact, catalogued released version of THAT file — never of a
// different owned file whose digest happens to collide.
//
// The tests are unit tier: they run under -short and reach no network and no
// git. Every fixture is derived from the package's own embedded writers
// (derivedShippedScaffold — mustReadSeed, mustReadLibraryGuide, DefaultThemeCSS)
// rather than restating paths or digests by hand, so a scaffold file that moves
// or changes bytes cannot leave a lookup test pinned to a stale literal.
// catalogLookup itself is pure — it hashes caller-supplied bytes and reads only
// the embedded catalogue — so no filesystem fixture is needed here; any test
// that does touch disk must confine itself to t.TempDir().
//
// upgrade/plan.phase-01.task-10 creates this file with the shared scaffolding
// below (the owned-file identity fixtures and the shipped-bytes/digest
// accessors). upgrade/plan.phase-01.task-11 completes TestCatalogLookup with the
// concrete cross-identity assertions.

import (
	"bytes"
	"path"
	"slices"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// catalogDeckSlideName is the filename of the deck-root starter slide the no-arg
// seed ships (slides/1-hello.md). It is a fixture name only: the slide's bytes
// come from the embedded seed through catalogShippedBytes, never from this
// literal.
const catalogDeckSlideName = "1-hello.md"

// catalogHelloTemplateName is the name of the seed's example slide template
// (templates/slides/hello/). Like catalogDeckSlideName it only names the fixture
// path; the bytes are derived.
const catalogHelloTemplateName = "hello"

// The owned-file identities the lookup tests exercise, built with the same
// form+path schema scaffoldversions.json uses (catalogOwnedIdentity) and the
// canonical path elements the writers use, so an assertion names an owned file
// rather than a hand-copied identity string.
//
// catalogUnownedConfig is deliberately NOT a kalide-owned whole-file member: it
// is the author-edited deck configuration, so it has no catalogue identity and
// catalogLookup must reject it for any bytes.
var (
	catalogDeckAGENTS   = catalogOwnedIdentity(catalogIdentityDeckForm, deckGuideSeedPath)
	catalogDeckThemeCSS = catalogOwnedIdentity(catalogIdentityDeckForm,
		path.Join(template.TemplatesDir, template.ThemesDir, theme.DefaultName, template.ThemeStylesheet))
	catalogDeckDefaultSlide = catalogOwnedIdentity(catalogIdentityDeckForm,
		path.Join(deck.SlidesDir, catalogDeckSlideName))
	catalogDeckHelloExample = catalogOwnedIdentity(catalogIdentityDeckForm,
		path.Join(template.TemplatesDir, template.SlidesDir, catalogHelloTemplateName, template.ExampleFile))

	catalogLibraryAGENTS   = catalogOwnedIdentity(catalogIdentityLibraryForm, deckGuideSeedPath)
	catalogLibraryThemeCSS = catalogOwnedIdentity(catalogIdentityLibraryForm,
		path.Join(template.ThemesDir, theme.DefaultName, template.ThemeStylesheet))

	catalogUnownedConfig = catalogOwnedIdentity(catalogIdentityDeckForm, deck.ConfigFile)
)

// catalogShippedBytes returns the exact bytes the running kalide ships for the
// kalide-owned scaffold file identified by identity, derived from the actual
// writers through derivedShippedScaffold — the same derivation the catalog drift
// self-test uses. It fails the test when identity names no shipped file, so a
// fixture that drifts out of the shipped set is named rather than silently
// looking up nothing.
func catalogShippedBytes(t *testing.T, identity string) []byte {
	t.Helper()

	files := derivedShippedScaffold(t)
	for _, file := range files {
		if file.identity == identity {
			return file.data
		}
	}
	t.Fatalf("no shipped scaffold file with identity %q; the running binary ships %v",
		identity, shippedScaffoldIdentities(files))
	return nil
}

// catalogShippedDigest returns the lowercase-hex SHA-256 digest of the bytes the
// running kalide ships for identity, hashed with the same algorithm
// catalogLookup, the catalogue and the drift self-test share
// (shippedScaffoldDigest).
func catalogShippedDigest(t *testing.T, identity string) string {
	t.Helper()
	return shippedScaffoldDigest(catalogShippedBytes(t, identity))
}

// TestCatalogLookup verifies catalogLookup's identity-scoped resolution: a
// catalogued released digest resolves only through the owned-file identity it
// was catalogued under, an uncatalogued or author-edited digest resolves to
// nothing, and an unknown identity resolves to nothing even for bytes that are
// catalogued under some other identity.
//
// The concrete cross-identity cases — the seed/library default theme.css
// collision, a slides/1-hello.md digest presented under AGENTS.md, and an
// unknown identity — are implemented by upgrade/plan.phase-01.task-11, which
// consumes the scaffolding above. This skeleton keeps the file compiling and
// green until then.
func TestCatalogLookup(t *testing.T) {
	t.Run("scaffolding: the shipped lookup fixtures derive to bytes", func(t *testing.T) {
		fixtures := []string{
			catalogDeckAGENTS,
			catalogDeckThemeCSS,
			catalogDeckDefaultSlide,
			catalogDeckHelloExample,
			catalogLibraryAGENTS,
			catalogLibraryThemeCSS,
		}
		for _, identity := range fixtures {
			if len(catalogShippedBytes(t, identity)) == 0 {
				t.Errorf("shipped scaffold file %q has no bytes", identity)
				continue
			}
			if got, want := catalogShippedDigest(t, identity),
				shippedScaffoldDigest(catalogShippedBytes(t, identity)); got != want {
				t.Errorf("catalogShippedDigest(%q) = %s, want %s", identity, got, want)
			}
		}

		// The cross-identity assertions task-11 adds are only meaningful while
		// the seed genuinely makes these owned files share bytes: the deck's
		// templates/themes/default/theme.css and a library's
		// themes/default/theme.css (DefaultThemeCSS derives it from the seed),
		// and the seed example template and the generated starter slide. If
		// either pair diverges, that case would pass vacuously, so fail here
		// instead.
		for _, pair := range []struct{ left, right, why string }{
			{
				catalogDeckThemeCSS, catalogLibraryThemeCSS,
				"the seed/library default theme.css collision catalogLookup's identity keying must not cross-match",
			},
			{
				catalogDeckHelloExample, catalogDeckDefaultSlide,
				"the example template / starter slide collision catalogLookup must not cross-match",
			},
		} {
			left, right := catalogShippedBytes(t, pair.left), catalogShippedBytes(t, pair.right)
			if !bytes.Equal(left, right) {
				t.Errorf("%s and %s no longer share bytes; %s", pair.left, pair.right, pair.why)
			}
			if got, want := catalogShippedDigest(t, pair.left), catalogShippedDigest(t, pair.right); got != want {
				t.Errorf("%s and %s share bytes but digests %s and %s differ", pair.left, pair.right, got, want)
			}
		}
	})

	t.Run("a catalogued digest resolves through its own identity", func(t *testing.T) {
		for _, identity := range []string{
			catalogDeckAGENTS,
			catalogDeckThemeCSS,
			catalogDeckDefaultSlide,
			catalogDeckHelloExample,
			catalogLibraryAGENTS,
			catalogLibraryThemeCSS,
		} {
			data := catalogShippedBytes(t, identity)
			if !catalogLookup(identity, data) {
				t.Errorf("catalogLookup(%q, its own shipped bytes) = false, want true", identity)
				continue
			}
			// Resolution must come through this identity's own entry: the same
			// shipped bytes' digest is recorded under exactly this identity,
			// with the releases that shipped them.
			digest := shippedScaffoldDigest(data)
			if versions := knownCatalog.versionsForDigest(identity, digest); len(versions) == 0 {
				t.Errorf("versionsForDigest(%q, %s) is empty; catalogLookup matched but the identity's entry carries no releases",
					identity, digest)
			}
		}
	})

	t.Run("a digest shared with another owned file never cross-matches", func(t *testing.T) {
		// The embedded seed makes the deck's templates/themes/default/theme.css
		// and a library's themes/default/theme.css byte-identical, so one digest
		// legitimately appears under BOTH identities. Each must resolve only
		// through its own entry, and neither may match a third identity that does
		// not catalogue those bytes.
		deckTheme := catalogShippedBytes(t, catalogDeckThemeCSS)
		libraryTheme := catalogShippedBytes(t, catalogLibraryThemeCSS)
		if !bytes.Equal(deckTheme, libraryTheme) {
			t.Fatalf("%s and %s no longer share bytes; the collision this case exists for is gone",
				catalogDeckThemeCSS, catalogLibraryThemeCSS)
		}
		if !catalogLookup(catalogDeckThemeCSS, deckTheme) {
			t.Errorf("catalogLookup(%q, its own shipped bytes) = false, want true", catalogDeckThemeCSS)
		}
		if !catalogLookup(catalogLibraryThemeCSS, libraryTheme) {
			t.Errorf("catalogLookup(%q, its own shipped bytes) = false, want true", catalogLibraryThemeCSS)
		}

		// The identical digest is held in two SEPARATE per-identity entries, not
		// one digest-wide entry: the seed theme shipped since before the library
		// theme existed (v0.5.0), a release the library identity never shipped.
		digest := shippedScaffoldDigest(deckTheme)
		deckVersions := knownCatalog.versionsForDigest(catalogDeckThemeCSS, digest)
		libraryVersions := knownCatalog.versionsForDigest(catalogLibraryThemeCSS, digest)
		if len(deckVersions) == 0 || len(libraryVersions) == 0 {
			t.Fatalf("shared digest %s is not catalogued under both theme identities (deck=%v, library=%v)",
				digest, deckVersions, libraryVersions)
		}
		if !slices.Contains(deckVersions, "v0.5.0") {
			t.Errorf("versionsForDigest(%q, %s) = %v, want the seed's v0.5.0 release among them",
				catalogDeckThemeCSS, digest, deckVersions)
		}
		if slices.Contains(libraryVersions, "v0.5.0") {
			t.Errorf("versionsForDigest(%q, %s) = %v, want the library's own releases only (v0.5.0 predates it)",
				catalogLibraryThemeCSS, digest, libraryVersions)
		}

		// Identity scoping: the shared theme digest is catalogued under the two
		// theme identities only, so presenting the very same bytes under any other
		// owned identity must return false. A digest-keyed (identity-blind) lookup
		// would wrongly report true here.
		for _, identity := range []string{catalogDeckAGENTS, catalogLibraryAGENTS, catalogDeckHelloExample} {
			if catalogLookup(identity, deckTheme) {
				t.Errorf("catalogLookup(%q, the shared default theme.css bytes) = true; identity scoping is broken", identity)
			}
		}

		// The example-template / starter-slide pair collides the same way (the
		// generated slides/1-hello.md is the example rendered), and must likewise
		// resolve only under its own identity.
		example := catalogShippedBytes(t, catalogDeckHelloExample)
		slide := catalogShippedBytes(t, catalogDeckDefaultSlide)
		if !bytes.Equal(example, slide) {
			t.Fatalf("%s and %s no longer share bytes; the collision this case exists for is gone",
				catalogDeckHelloExample, catalogDeckDefaultSlide)
		}
		if !catalogLookup(catalogDeckHelloExample, example) {
			t.Errorf("catalogLookup(%q, its own shipped bytes) = false, want true", catalogDeckHelloExample)
		}
		if !catalogLookup(catalogDeckDefaultSlide, slide) {
			t.Errorf("catalogLookup(%q, its own shipped bytes) = false, want true", catalogDeckDefaultSlide)
		}
		if catalogLookup(catalogDeckThemeCSS, example) {
			t.Errorf("catalogLookup(%q, the example/hello bytes) = true; identity scoping is broken", catalogDeckThemeCSS)
		}
	})

	t.Run("an uncatalogued, author-edited or unknown-identity lookup resolves to nothing", func(t *testing.T) {
		// (b) A digest taken from slides/1-hello.md presented under the AGENTS.md
		// identity must not resolve: AGENTS.md is an owned file, but its own
		// catalogue entry does not carry the slide's digest.
		slide := catalogShippedBytes(t, catalogDeckDefaultSlide)
		if !catalogLookup(catalogDeckDefaultSlide, slide) {
			t.Fatalf("catalogLookup(%q, its own shipped bytes) = false; the AGENTS.md negative would be vacuous",
				catalogDeckDefaultSlide)
		}
		for _, identity := range []string{catalogDeckAGENTS, catalogLibraryAGENTS} {
			if catalogLookup(identity, slide) {
				t.Errorf("catalogLookup(%q, slides/1-hello.md bytes) = true; a digest catalogued for another owned file must not cross-match",
					identity)
			}
		}

		// (c) An unknown identity — a file outside the owned scope, e.g.
		// deck:kalide.yaml, the author-edited deck config — resolves to nothing
		// regardless of the digest: neither bytes catalogued under some other
		// identity nor arbitrary author bytes may match it.
		if digests := knownCatalog.digestsForIdentity(catalogUnownedConfig); len(digests) != 0 {
			t.Fatalf("catalogue unexpectedly carries identity %q (digests %v); it is not a whole-file owned member",
				catalogUnownedConfig, digests)
		}
		catalogued := catalogShippedBytes(t, catalogDeckThemeCSS)
		for _, identity := range []string{catalogUnownedConfig, "deck:not-owned.txt", "library:kalide.yaml"} {
			if catalogLookup(identity, catalogued) {
				t.Errorf("catalogLookup(%q, catalogued bytes) = true; an unknown identity must resolve to nothing", identity)
			}
			if catalogLookup(identity, []byte("author-edited content\n")) {
				t.Errorf("catalogLookup(%q, author bytes) = true; an unknown identity must resolve to nothing", identity)
			}
		}

		// An uncatalogued/author-edited digest still resolves to nothing under
		// its OWN identity: flipping a byte moves shipped bytes off every
		// catalogued version of that file.
		for _, identity := range []string{
			catalogDeckAGENTS,
			catalogDeckThemeCSS,
			catalogDeckDefaultSlide,
			catalogDeckHelloExample,
			catalogLibraryAGENTS,
			catalogLibraryThemeCSS,
		} {
			data := catalogShippedBytes(t, identity)
			mutated := slices.Clone(data)
			mutated[0] ^= 0x80
			if catalogLookup(identity, mutated) {
				t.Errorf("catalogLookup(%q, author-edited bytes) = true, want false", identity)
			}
		}
	})
}

// catalogV090DeckAGENTSDigest and catalogV090LibraryAGENTSDigest are the SHA-256
// digests of the AGENTS.md guides the v0.9.0 release shipped. They are named
// literals, not derived shippers: the catalogue records digests only, never
// historical contents (global.constraint.upgrade-known-version-catalog), and the
// running tree ships the newer v0.10.0 bytes, so no shipped file hashes to these
// (catalogShippedDigest derives only the current version). They are the
// regression TestCatalogRollForwardV090 guards: the v0.9.0 release shipped these
// AGENTS.md bytes, and the catalogue must carry them under their OWN identities
// so a project left at v0.9.0 is refreshable.
const (
	catalogV090DeckAGENTSDigest    = "4ee150bbf809ab5d42952a78b5f5892378ce2988231f669faf77682d48cc2d70"
	catalogV090LibraryAGENTSDigest = "53513b1643dcffa67d633ca5aa45177c1ac2f37a85aacd802a6ab70b2bf44511"
)

// TestCatalogRollForwardV090 is the unit regression guard for the v0.9.0
// AGENTS.md digests' roll-forward into scaffoldversions.json
// (global.constraint.upgrade-known-version-catalog): the v0.9.0 release shipped
// exactly catalogV090DeckAGENTSDigest for deck:AGENTS.md and
// catalogV090LibraryAGENTSDigest for library:AGENTS.md, and both must be
// catalogued under their own identity, never under the other's.
//
// catalogLookup resolves caller bytes through the per-identity digest map
// digestsForIdentity enumerates and versionsForDigest reads. A historical
// release's bytes are not shipped — the catalogue holds digests only — so there
// is no SHA-256 preimage to hand catalogLookup for these two digests; the
// digest-keyed recognition is asserted through that same map, which is exactly
// the membership catalogLookup tests. The two live AGENTS.md identities are
// additionally driven through catalogLookup itself with the current shipped
// bytes to prove the identity scoping is real, not a property of the map alone.
func TestCatalogRollForwardV090(t *testing.T) {
	// The v0.9.0 (identity, digest) pairs, each with the other AGENTS.md
	// identity its digest must never resolve under.
	v090 := []struct {
		identity      string
		digest        string
		otherIdentity string
	}{
		{catalogDeckAGENTS, catalogV090DeckAGENTSDigest, catalogLibraryAGENTS},
		{catalogLibraryAGENTS, catalogV090LibraryAGENTSDigest, catalogDeckAGENTS},
	}

	t.Run("each v0.9.0 AGENTS.md digest is catalogued under its own identity", func(t *testing.T) {
		for _, tc := range v090 {
			digests := knownCatalog.digestsForIdentity(tc.identity)
			if !slices.Contains(digests, tc.digest) {
				t.Errorf("digestsForIdentity(%q) = %v, want the v0.9.0 digest %s catalogued under it",
					tc.identity, digests, tc.digest)
			}
			versions := knownCatalog.versionsForDigest(tc.identity, tc.digest)
			if !slices.Contains(versions, "v0.9.0") {
				t.Errorf("versionsForDigest(%q, %s) = %v, want the v0.9.0 release label",
					tc.identity, tc.digest, versions)
			}
		}
	})

	t.Run("the v0.9.0 digests are historical, not the current shipped bytes", func(t *testing.T) {
		// Non-vacuity: were a current shipped AGENTS.md to hash to a v0.9.0
		// digest, the "own identity" case above would pass for the wrong reason.
		for _, tc := range v090 {
			if current := catalogShippedDigest(t, tc.identity); current == tc.digest {
				t.Errorf("the running %s bytes hash to the v0.9.0 digest %s; the roll-forward case is not exercising a historical release",
					tc.identity, tc.digest)
			}
		}
	})

	t.Run("a v0.9.0 digest never resolves under the other AGENTS.md identity", func(t *testing.T) {
		for _, tc := range v090 {
			if slices.Contains(knownCatalog.digestsForIdentity(tc.otherIdentity), tc.digest) {
				t.Errorf("digestsForIdentity(%q) unexpectedly carries %s's v0.9.0 digest %s; identity scoping is broken",
					tc.otherIdentity, tc.identity, tc.digest)
			}
			if versions := knownCatalog.versionsForDigest(tc.otherIdentity, tc.digest); len(versions) != 0 {
				t.Errorf("versionsForDigest(%q, %s) = %v, want none: the digest is catalogued for %q only",
					tc.otherIdentity, tc.digest, versions, tc.identity)
			}
		}
	})

	t.Run("negative: a mutated v0.9.0 digest resolves to nothing", func(t *testing.T) {
		// Flipping one hex digit moves the digest off every catalogued version,
		// so both the listing and the digest-keyed lookup (the map catalogLookup
		// tests) must report it unknown; were either to accept the mutant, this
		// guard would pass vacuously.
		for _, tc := range v090 {
			mutated := tc.digest
			if mutated[0] == '0' {
				mutated = "1" + mutated[1:]
			} else {
				mutated = "0" + mutated[1:]
			}
			if mutated == tc.digest {
				t.Fatalf("mutating %s produced the same digest; the negative self-check is vacuous", tc.digest)
			}
			if slices.Contains(knownCatalog.digestsForIdentity(tc.identity), mutated) {
				t.Errorf("digestsForIdentity(%q) unexpectedly carries the mutated digest %s", tc.identity, mutated)
			}
			if versions := knownCatalog.versionsForDigest(tc.identity, mutated); len(versions) != 0 {
				t.Errorf("versionsForDigest(%q, %s) = %v, want none for a mutated digest", tc.identity, mutated, versions)
			}
		}
	})

	t.Run("catalogLookup scopes the live AGENTS.md identities", func(t *testing.T) {
		// Drive the identity-scoped lookup itself on the two identities the
		// v0.9.0 entries belong to: each identity's current shipped bytes resolve
		// only through their own identity, never the other's.
		deck := catalogShippedBytes(t, catalogDeckAGENTS)
		library := catalogShippedBytes(t, catalogLibraryAGENTS)
		if !catalogLookup(catalogDeckAGENTS, deck) {
			t.Errorf("catalogLookup(%q, its own shipped bytes) = false, want true", catalogDeckAGENTS)
		}
		if !catalogLookup(catalogLibraryAGENTS, library) {
			t.Errorf("catalogLookup(%q, its own shipped bytes) = false, want true", catalogLibraryAGENTS)
		}
		if catalogLookup(catalogDeckAGENTS, library) {
			t.Errorf("catalogLookup(%q, the library AGENTS.md bytes) = true; the AGENTS.md identities are not scoped", catalogDeckAGENTS)
		}
		if catalogLookup(catalogLibraryAGENTS, deck) {
			t.Errorf("catalogLookup(%q, the deck AGENTS.md bytes) = true; the AGENTS.md identities are not scoped", catalogLibraryAGENTS)
		}
	})
}
