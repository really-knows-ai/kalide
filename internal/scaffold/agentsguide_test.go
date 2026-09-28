package scaffold

// This file holds TestAgentGuidesMatchFormat, the agent-guide drift self-test
// (requirements.requirement.agent-guide-drift-check, phase-05 tasks 1-2), plus
// its per-guide scope tables and minimal helpers.
//
// The implemented format vocabulary is derived ONLY from the exported canonical
// accessors — template.ManifestFieldTypes, template.BodyModes,
// template.ReservedNames, template.ContextKeys, template.ManifestTopLevelKeys,
// template.LibraryMetaKeys and template.LayoutFuncMap (the v1 `media` helper),
// plus deck.ConfigKeys — and the two guide byte streams are read through
// mustReadSeed and mustReadLibraryGuide. It never restates the vocabulary as a
// hand-maintained fixture list: a format token added to the implementation is
// returned by an accessor and immediately has to appear in a guide, so a token
// the union of the two guides omits makes the test fail.
//
// Coverage is per guide so the test never demands from a guide a token its
// declared scope does not own:
//
//   - union check: every implemented format token appears in at least one guide;
//   - deck-guide scope: every kalide.yaml key (deck.ConfigKeys), the full CLI
//     command set, and the shared format vocabulary (the deck guide is the
//     complete format reference);
//   - library-guide scope: the templates/ library layout, the library.yaml
//     keys, the template.yaml schema and body modes, the layout language, the
//     reserved names deck/slide, the `media` helper, the reserved context keys,
//     every field type and the themes/media rules.
//
// Negative assertion: the kalide.yaml config keys and the init/version CLI are
// deliberately OUTSIDE the library-guide scope, so their absence from
// libraryguide/AGENTS.md is never asserted and cannot fail the test. The test
// also never asserts their textual absence — the library guide is free to
// mention them; it just is not required to.

import (
	"sort"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// deckGuideSeedPath is the deck guide's path within the embedded seed tree; it
// is also the path writeHelloSeed copies it to in a scaffolded deck.
const deckGuideSeedPath = "AGENTS.md"

// deckGuideCLI is the full CLI command set the deck guide must carry
// (agent-guide-drift-check, agent-authoring). There is no canonical accessor
// for command names, so this one scope list is deliberately authored here: it
// is scope, not format vocabulary.
var deckGuideCLI = []string{
	"init",
	"init-library",
	"start",
	"templates",
	"templates <name>",
	"version",
}

// agentGuideTokenGroup is one named family of implemented tokens, so a drift
// failure names the family the missing token belongs to.
type agentGuideTokenGroup struct {
	name   string
	tokens []string
}

// TestAgentGuidesMatchFormat is the per-guide drift self-test: it derives the
// implemented format vocabulary from the exported canonical accessors (never
// from a hand-copied fixture table) and asserts both a union check across the
// two embedded guides and per-guide scope checks, and that the embedded seed
// deck the guides document still loads and validates.
func TestAgentGuidesMatchFormat(t *testing.T) {
	deckGuide, libraryGuide := agentGuideTexts(t)

	t.Run("union: every implemented format token appears in at least one guide", func(t *testing.T) {
		for _, group := range agentGuideFormatGroups(t) {
			if len(group.tokens) == 0 {
				t.Errorf("canonical %s set is empty; an accessor derived no tokens", group.name)
				continue
			}
			for _, token := range group.tokens {
				if strings.Contains(deckGuide, token) || strings.Contains(libraryGuide, token) {
					continue
				}
				t.Errorf("implemented %s %q appears in neither guide; document it in seed/AGENTS.md and/or libraryguide/AGENTS.md", group.name, token)
			}
		}
	})

	t.Run("deck guide: every kalide.yaml key and the full CLI set", func(t *testing.T) {
		agentGuideAssertGuideCarries(t, "deck guide (seed/AGENTS.md)", deckGuide, []agentGuideTokenGroup{
			{name: "kalide.yaml key", tokens: deck.ConfigKeys()},
			{name: "CLI command", tokens: deckGuideCLI},
		})
	})

	t.Run("deck guide: the shared format vocabulary", func(t *testing.T) {
		agentGuideAssertGuideCarries(t, "deck guide (seed/AGENTS.md)", deckGuide, agentGuideFormatGroups(t))
	})

	t.Run("library guide: layout, keys and format", func(t *testing.T) {
		agentGuideAssertGuideCarries(t, "library guide (libraryguide/AGENTS.md)", libraryGuide, agentGuideLibraryScopeGroups(t))
	})

	t.Run("negative: deck-only tokens are not required in the library guide", func(t *testing.T) {
		shared := agentGuideTokenSet(agentGuideFormatGroups(t))
		libraryScope := agentGuideTokenSet(agentGuideLibraryScopeGroups(t))

		for _, key := range deck.ConfigKeys() {
			// A config key that is also an implemented format token (for
			// example `date`, a field type) is legitimately required by the
			// library guide's own scope; only the config-key role is deck-only.
			if shared[key] {
				continue
			}
			if libraryScope[key] {
				t.Errorf("kalide.yaml key %q must not be required by the library guide's scope; the library guide is not a duplicate config reference", key)
			}
		}
		for _, cmd := range []string{"init", "version"} {
			if libraryScope[cmd] {
				t.Errorf("deck-only CLI command %q must not be required by the library guide's scope", cmd)
			}
		}
	})

	t.Run("the embedded seed deck the guides document still loads and validates", func(t *testing.T) {
		// The guides' example snippets must still load/validate as far as the
		// format allows. The genuinely loadable artifact is the embedded seed
		// deck both guides document; several guide fragments are illustrative
		// and not standalone-loadable (for example the template.yaml example's
		// `accepted: [person]` names a section template that does not exist in
		// a bare snippet), so this runs the real loader path over the embedded
		// seed rather than executing markdown fences.
		if err := validateSeed(); err != nil {
			t.Fatalf("validateSeed() error = %v, want the embedded seed the guides document to load", err)
		}

		themes, err := theme.LoadDir(seedRoot, template.TemplatesDir+"/"+template.ThemesDir)
		if err != nil {
			t.Fatalf("theme.LoadDir(embedded seed) error = %v", err)
		}
		if _, err := deck.LoadConfig(seedRoot, deck.ConfigFile, themes); err != nil {
			t.Fatalf("deck.LoadConfig(embedded seed) error = %v, want the guide's kalide.yaml example to load", err)
		}
		if _, err := deck.LoadSlides(seedRoot, deck.SlidesDir); err != nil {
			t.Fatalf("deck.LoadSlides(embedded seed) error = %v, want the guide's slide example to load", err)
		}
	})
}

// agentGuideTexts reads the two embedded guides as strings through the same
// helpers the scaffold writers use, and fails when either is empty.
func agentGuideTexts(t *testing.T) (deckGuide, libraryGuide string) {
	t.Helper()

	deckGuide = string(mustReadSeed(deckGuideSeedPath))
	libraryGuide = string(mustReadLibraryGuide())
	if strings.TrimSpace(deckGuide) == "" {
		t.Fatalf("embedded deck guide %s is empty", deckGuideSeedPath)
	}
	if strings.TrimSpace(libraryGuide) == "" {
		t.Fatal("embedded library guide is empty")
	}
	return deckGuide, libraryGuide
}

// agentGuideFormatGroups returns the implemented format vocabulary grouped by
// family, derived ONLY from the exported canonical accessors.
func agentGuideFormatGroups(t *testing.T) []agentGuideTokenGroup {
	t.Helper()

	return []agentGuideTokenGroup{
		{name: "field type", tokens: template.ManifestFieldTypes()},
		{name: "body mode", tokens: template.BodyModes()},
		{name: "reserved name", tokens: template.ReservedNames()},
		{name: "layout helper", tokens: agentGuideLayoutHelpers(t)},
		{name: "context key", tokens: template.ContextKeys()},
		{name: "template.yaml top-level key", tokens: template.ManifestTopLevelKeys()},
		{name: "library.yaml key", tokens: template.LibraryMetaKeys()},
	}
}

// agentGuideLayoutHelpers returns the non-format helper names
// template.LayoutFuncMap exposes: the complete func map minus the built-in
// number/date format functions. That difference is the v1 `media` helper,
// derived from the canonical func-map source rather than its literal name, so
// adding or removing a helper is reflected without editing a fixture.
func agentGuideLayoutHelpers(t *testing.T) []string {
	t.Helper()

	funcs := template.LayoutFuncMap(nil, "/media")
	formats := template.BuiltinFormats.FuncMap()

	var helpers []string
	for name := range funcs {
		if _, isFormat := formats[name]; !isFormat {
			helpers = append(helpers, name)
		}
	}
	sort.Strings(helpers)
	if len(helpers) == 0 {
		t.Fatal("template.LayoutFuncMap exposes no non-format helper; the v1 `media` helper is not registered")
	}
	return helpers
}

// agentGuideLibraryScopeGroups returns the token families the library guide
// owns (agent-guide-drift-check, library-agent-guide): the templates/ library
// layout, the library.yaml keys, the template.yaml schema and body modes, the
// layout language, the reserved deck/slide names, the `media` helper, the
// reserved context keys, every field type and the themes/media rules. It
// deliberately omits the kalide.yaml config keys and the init/version CLI,
// which are deck-guide-only.
func agentGuideLibraryScopeGroups(t *testing.T) []agentGuideTokenGroup {
	t.Helper()

	layout := []string{
		template.LibraryFile,
		template.SlidesDir + "/<name>/",
		template.SectionsDir + "/<name>/",
		template.ThemesDir + "/<name>/",
		template.MediaDir + "/",
	}
	return []agentGuideTokenGroup{
		{name: "library layout entry", tokens: layout},
		{name: "library.yaml key", tokens: template.LibraryMetaKeys()},
		{name: "template.yaml top-level key", tokens: template.ManifestTopLevelKeys()},
		{name: "body mode", tokens: template.BodyModes()},
		{name: "layout language token", tokens: []string{template.LayoutFile, "html/template"}},
		{name: "reserved name", tokens: []string{"deck", "slide"}},
		{name: "layout helper", tokens: agentGuideLayoutHelpers(t)},
		{name: "context key", tokens: template.ContextKeys()},
		{name: "field type", tokens: template.ManifestFieldTypes()},
		{name: "themes/media rule token", tokens: []string{template.ThemeStylesheet, "url()", "media \""}},
	}
}

// agentGuideAssertGuideCarries asserts guide contains every token in every
// group, naming the guide and the token family in each failure.
func agentGuideAssertGuideCarries(t *testing.T, guideName, guide string, groups []agentGuideTokenGroup) {
	t.Helper()

	for _, group := range groups {
		if len(group.tokens) == 0 {
			t.Errorf("%s: canonical %s set is empty; an accessor derived no tokens", guideName, group.name)
			continue
		}
		for _, token := range group.tokens {
			if !strings.Contains(guide, token) {
				t.Errorf("%s is missing the implemented %s %q", guideName, group.name, token)
			}
		}
	}
}

// agentGuideTokenSet flattens token groups into a membership set.
func agentGuideTokenSet(groups []agentGuideTokenGroup) map[string]bool {
	set := make(map[string]bool)
	for _, group := range groups {
		for _, token := range group.tokens {
			set[token] = true
		}
	}
	return set
}
