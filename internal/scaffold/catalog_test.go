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
		t.Skip("concrete lookups implemented by upgrade/plan.phase-01.task-11")
	})

	t.Run("a digest shared with another owned file never cross-matches", func(t *testing.T) {
		t.Skip("concrete lookups implemented by upgrade/plan.phase-01.task-11")
	})

	t.Run("an uncatalogued, author-edited or unknown-identity lookup resolves to nothing", func(t *testing.T) {
		t.Skip("concrete lookups implemented by upgrade/plan.phase-01.task-11")
	})
}
