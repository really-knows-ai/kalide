package scaffold

// This file holds TestAgentGuidesMatchFormat, the agent-guide drift self-test
// (requirements.requirement.agent-guide-drift-check, phase-05 tasks 1-2), plus
// its per-guide scope tables and minimal helpers.
//
// The implemented format vocabulary is derived from the exported canonical
// accessors — template.ManifestFieldTypes, template.BodyModes,
// template.ReservedNames, template.ContextKeys, template.ManifestTopLevelKeys,
// template.LibraryMetaKeys, template.MediaURLPrefix / template.ThemeURLPrefix
// (the reserved theme-CSS prefixes) and template.LayoutFuncMap (the v1 helper
// set) — plus deck.ConfigKeys — and the two full AGENTS_KALIDE.md guides are
// read through mustReadSeed and mustReadLibraryFullGuide, while the AGENTS.md
// pointers are separately asserted to carry the reference line. The
// `{{ template }}` layout-composition tokens and the composition statements
// have no canonical accessor, so they are authored scope alongside deckGuideCLI;
// so are the delta's keys that only unexported switches parse — the section
// declaration keys (name/accepted/min/max/max_depth), the reserved item-context
// key item.depth (`.item.depth`), the image path keys (roots/dir/extensions)
// and the enum keys (variants/variants_from) — see
// agentGuideAuthoredScopeGroups. The vocabulary is never restated as a
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
//     complete format reference), including the reserved `media:`/`theme:<name>/`
//     prefixes, the `{{ template }}` layout-composition tokens and the delta's
//     authored keys (section declaration keys, the reserved item-context key
//     item.depth, image path keys and enum keys);
//   - library-guide scope: the templates/ library layout, the library.yaml
//     keys, the template.yaml schema and body modes, the layout language and its
//     `{{ template }}` composition tokens, the reserved context names
//     deck/slide/item/raw/data, the layout helpers, the reserved context keys,
//     every field type, the themes/media rules and the reserved
//     `media:`/`theme:<name>/` prefixes, plus the same delta authored keys.
//
// Negative assertion: the kalide.yaml config keys and the init/version CLI are
// deliberately OUTSIDE the library-guide scope, so their absence from
// libraryguide/AGENTS_KALIDE.md is never asserted and cannot fail the test. The
// test also never asserts their textual absence — the library guide is free to
// mention them; it just is not required to.
//
// Matching is context-aware (feedback-17): a token counts only where the format
// actually declares it — as a complete backtick code span, as a YAML mapping
// key (`theme:`, optionally backticked), as a fenced-code-block word, or as a
// `kalide <cmd>` command. Ordinary prose never satisfies a format token, and a
// token that is merely a substring of another identifier (`init` in
// `init-library`, `theme` in `themes/` or `theme.css`) does not count. The
// negative self-check subtest pins that behaviour.
//
// The layout-composition statements (local-to-external allowed, external-to-
// local a templates error, the invoking-layer media binding and the `section`
// rebind) are the one family matched differently: they are readable prose, not
// format tokens, so a group marked `literal` matches each statement as a
// whitespace-normalised literal phrase (agentGuideProseContains), tolerating
// the guides' hard line wrapping. Every format token — the `{{ template }}`
// action and layer-relative layout-name form included — keeps the context-aware
// matching above.
//
// A reserved URL prefix (`media:`, `theme:`, feedback-8) is special-cased: the
// token is a prefix that ends in `:`, so it is inherently ambiguous with a YAML
// mapping key (`theme: default`) and with ordinary prose. It therefore counts
// only where the format actually uses it: inside an inline backtick code span,
// or as a `url()`/`@import` argument (immediately preceded, allowing whitespace
// and one optional opening quote, by `url(` or `@import`). In particular the
// deck guide's `theme: default` config example no longer satisfies `theme:`,
// while the `media:` checks keep passing.
//
// Fenced snippets are validated, not ignored (feedback-18): the ```-fenced
// blocks in each guide are extracted and the standalone ones are run through
// the real parsers — a kalide.yaml snippet through deck.LoadConfig, a slide
// source through internal/slide.Parse, a library.yaml snippet through
// template.LoadLibrary, and a template.yaml snippet through template.LoadLibrary
// on an in-memory fs.FS that supplies the person section template its
// `accepted: [person]` reference needs, the owning layer's named definitions
// (groups/field_sets/types) and the media/ files a `variants_from` glob expands
// against. A library.yaml snippet declaring named definitions is additionally
// required to have them parsed onto its layer. The fixture's delta provisions
// are themselves exercised by the "the snippet fixture loads the delta's
// template.yaml forms" self-check, so they cannot rot. A snippet that is
// intentionally illustrative and not standalone-loadable (a layout tree, the
// CLI usage listing, a `media` action fragment, a section-instance fragment, an
// error message) must be declared, with a reason, in
// agentGuideIllustrativeSnippets; any fenced snippet that is neither validated
// nor declared there fails the test, so no snippet is ever silently skipped.
//
// Inline template snippets are validated too: every single-line backtick code
// span carrying a Go template action (`{{ … }}`) is parsed with the canonical
// template.LayoutFuncMap, so the `section`/`dict`/`list` layout examples the
// guides show are load-checked even though they are inline rather than fenced.

import (
	"fmt"
	htmltemplate "html/template"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// deckGuideSeedPath is the deck full guide's path within the embedded seed
// tree (AGENTS_KALIDE.md); it is also the path writeHelloSeed's pair writer
// writes the full guide to in a scaffolded deck, and the bytes both drift
// guides are read from.
const deckGuideSeedPath = agentGuideFullGuideName

// deckPointerSeedPath is the deck AGENTS.md pointer's path within the embedded
// seed tree; it is also the path the pair writer writes the pointer to in a
// scaffolded deck, and the path the catalog AGENTS.md identities are keyed by.
const deckPointerSeedPath = agentsGuideName

// deckGuideCLI is the full CLI command set the deck guide must carry
// (agent-guide-drift-check, agent-authoring). There is no canonical accessor
// for command names, so this one scope list is deliberately authored here: it
// is scope, not format vocabulary. It carries the whole user-facing command
// set — including the config, check, render and help commands the typed-records
// delta added — and the deck-guide CLI scope subtest asserts every entry is
// documented.
var deckGuideCLI = []string{
	"init",
	"init-library",
	"start",
	"templates",
	"templates <name>",
	"upgrade",
	"version",
	"config",
	"check",
	"render",
	"help",
}

// agentGuideTokenGroup is one named family of implemented tokens, so a drift
// failure names the family the missing token belongs to.
type agentGuideTokenGroup struct {
	name   string
	tokens []string
	// literal marks the group's tokens as readable composition statements, not
	// format tokens: each is matched as a whitespace-normalised literal phrase
	// anywhere in the guide (agentGuideProseContains) rather than by the
	// context-aware format-token rules. It exists only for the layout-
	// composition prose a guide must teach (guide-layout-composition); every
	// format token keeps the context-aware matching below.
	literal bool
}

// TestAgentGuidesMatchFormat is the per-guide drift self-test: it derives the
// implemented format vocabulary from the exported canonical accessors (never
// from a hand-copied fixture table) and asserts both a union check across the
// two embedded guides and per-guide scope checks, that the embedded seed deck
// the guides document still loads and validates, and that the guides' own
// fenced snippets load through the real parsers.
func TestAgentGuidesMatchFormat(t *testing.T) {
	deckGuide, libraryGuide := agentGuideTexts(t)

	t.Run("union: every implemented format token appears in at least one guide", func(t *testing.T) {
		for _, group := range agentGuideFormatGroups(t) {
			if len(group.tokens) == 0 {
				t.Errorf("canonical %s set is empty; an accessor derived no tokens", group.name)
				continue
			}
			for _, token := range group.tokens {
				if agentGuideGroupCarries(group, deckGuide, token) || agentGuideGroupCarries(group, libraryGuide, token) {
					continue
				}
				t.Errorf("implemented %s %q appears in neither guide; document it in seed/AGENTS_KALIDE.md and/or libraryguide/AGENTS_KALIDE.md", group.name, token)
			}
		}
	})

	t.Run("deck guide: every kalide.yaml key and the full CLI set", func(t *testing.T) {
		agentGuideAssertGuideCarries(t, "deck guide (seed/AGENTS_KALIDE.md)", deckGuide, []agentGuideTokenGroup{
			{name: "kalide.yaml key", tokens: deck.ConfigKeys()},
			{name: "CLI command", tokens: deckGuideCLI},
		})
	})

	t.Run("deck guide: the shared format vocabulary", func(t *testing.T) {
		agentGuideAssertGuideCarries(t, "deck guide (seed/AGENTS_KALIDE.md)", deckGuide, agentGuideFormatGroups(t))
	})

	t.Run("library guide: layout, keys and format", func(t *testing.T) {
		agentGuideAssertGuideCarries(t, "library guide (libraryguide/AGENTS_KALIDE.md)", libraryGuide, agentGuideLibraryScopeGroups(t))
	})

	t.Run("the AGENTS.md pointers carry the full-guide reference line", func(t *testing.T) {
		// The AGENTS.md files are pointers, not guides: each must carry the
		// exact reference line the pair writer's scheme keys on, so a reader
		// lands on the full AGENTS_KALIDE.md guide. This is the agent-guide-
		// reference contract, asserted on the real embedded pointer bytes.
		pointers := []struct {
			name string
			data []byte
		}{
			{"seed/" + deckPointerSeedPath, mustReadSeed(deckPointerSeedPath)},
			{"libraryguide/" + deckPointerSeedPath, mustReadLibraryGuide()},
		}
		for _, pointer := range pointers {
			if !strings.Contains(string(pointer.data), agentGuideReferenceLine) {
				t.Errorf("%s does not carry the exact reference line %q; the pointer must name the full guide",
					pointer.name, agentGuideReferenceLine)
			}
		}
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

	t.Run("negative: a guide that omits a layout helper is detected", func(t *testing.T) {
		// The drift test must fail, not pass vacuously, when a helper the
		// implementation exposes is missing from a guide. Build a synthetic
		// guide carrying every helper but the last as a real inline code span
		// and assert the missing one is not satisfied: that is exactly the
		// membership decision agentGuideAssertGuideCarries reds on.
		helpers := agentGuideLayoutHelpers(t)
		if len(helpers) < 2 {
			t.Fatalf("template.LayoutFuncMap exposes %d helper(s), want at least 2 to exercise a missing helper", len(helpers))
		}
		var b strings.Builder
		for _, h := range helpers[:len(helpers)-1] {
			fmt.Fprintf(&b, "`%s` ", h)
		}
		synthetic := b.String()
		for i, h := range helpers {
			want := i < len(helpers)-1
			if got := agentGuideContainsToken(synthetic, h); got != want {
				t.Errorf("agentGuideContainsToken(synthetic guide, %q) = %v, want %v; a helper absent from a guide must not be satisfied", h, got, want)
			}
		}
	})

	t.Run("context-aware matching rejects substring-only presence", func(t *testing.T) {
		// The negative cases are the feedback-17 regressions: a token present
		// only inside a longer word, or only in ordinary prose, must NOT count,
		// so removing the token from its real declaration fails the drift test.
		absent := []struct{ text, token string }{
			{"kalide init-library <path>", "init"},
			{"themes/default/", "theme"},
			{"theme.css", "theme"},
			{"The deck title is required.", "title"},
			{"The presentation date, or none.", "date"},
			{"A file format is documented elsewhere.", "format"},
			{"The slide text is ordinary prose.", "text"},
			{"Templates live under templates/.", "templates"},
			{"`{{ media \"logo.svg\" }}`", "media:"},
			{"the media layout helper renders a file", "media:"},
			{"theme: default", "theme:"},
			{"theme:", "theme:"},
			{"The nested sections are bounded by a max_depth value.", "max_depth"},
			{"The item depth grows with each nesting level.", "item.depth"},
			{"Images resolve under the declared roots in order.", "roots"},
			{"Variants from a glob expand at load.", "variants_from"},
		}
		for _, tc := range absent {
			if agentGuideContainsToken(tc.text, tc.token) {
				t.Errorf("agentGuideContainsToken(%q, %q) = true, want false: a substring or prose mention must not satisfy the token", tc.text, tc.token)
			}
		}

		present := []struct{ text, token string }{
			{"kalide init [path]", "init"},
			{"`theme`", "theme"},
			{"theme: default", "theme"},
			{"`title`", "title"},
			{"title: My deck", "title"},
			{"`section-template`", "section-template"},
			{"`.deck.title`", "deck.title"},
			{"`optional`", "optional"},
			{"```\nmode: optional\n```", "optional"},
			{"`media:`", "media:"},
			{"```\nurl('media:fonts/x.woff2')\n```", "media:"},
			{"`theme:<name>/`", "theme:"},
			{"@import 'theme:default/theme.css'\nurl('theme:default/bg.png')", "theme:"},
			{"`max_depth: N`", "max_depth"},
			{"`.item.depth`", "item.depth"},
			{"roots: [assets, media]", "roots"},
			{"dir: logos", "dir"},
			{"extensions: [svg, png]", "extensions"},
			{"`variants_from`", "variants_from"},
		}
		for _, tc := range present {
			if !agentGuideContainsToken(tc.text, tc.token) {
				t.Errorf("agentGuideContainsToken(%q, %q) = false, want true", tc.text, tc.token)
			}
		}
	})

	t.Run("the embedded seed deck the guides document still loads and validates", func(t *testing.T) {
		// The genuinely loadable artifact both guides document is the embedded
		// seed deck; several guide fragments are illustrative and not
		// standalone-loadable, so this runs the real loader path over the
		// embedded seed, while the guide's own fenced snippets are exercised by
		// the next subtest.
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

	t.Run("the guides' fenced snippets load through the real parsers", func(t *testing.T) {
		agentGuideAssertSnippetsLoad(t, "deck guide (seed/AGENTS_KALIDE.md)", deckGuide)
		agentGuideAssertSnippetsLoad(t, "library guide (libraryguide/AGENTS_KALIDE.md)", libraryGuide)
	})

	t.Run("the snippet fixture loads the delta's template.yaml forms", func(t *testing.T) {
		// The in-memory library the guides' own snippets load through must
		// itself carry the delta's forms, or a snippet that uses them would be
		// reported as a load failure unrelated to the guide. This self-check
		// loads one template.yaml exercising every fixture provision: a named
		// type (`type: colour`), a named field set (`- include: meta`), a named
		// accept-list (`accepted: [@callouts]`), an image field's media root and
		// an enum field's variants_from glob — the forms the guides document
		// (named-library-definitions, image-field-roots, enum-variants-from).
		manifest := "description: delta fixture\n" +
			"fields:\n" +
			"  - name: accent\n" +
			"    type: colour\n" +
			"  - name: logo\n" +
			"    type: image\n" +
			"    roots: [media]\n" +
			"    dir: bgs\n" +
			"    extensions: [jpg]\n" +
			"  - name: background\n" +
			"    type: enum\n" +
			"    variants_from: media/bgs/*.jpg\n" +
			"  - include: meta\n" +
			"sections:\n" +
			"  - name: people\n" +
			"    accepted: [\"@callouts\"]\n" +
			"body:\n" +
			"  mode: optional\n"
		if _, err := template.LoadLibrary(agentGuideManifestFS(manifest), template.TemplatesDir); err != nil {
			t.Errorf("the snippet fixture fails to load a delta template.yaml (named type/include/@group, image media root, variants_from): %v", err)
		}
	})

	t.Run("the guides' inline template snippets parse with the layout func map", func(t *testing.T) {
		agentGuideAssertInlineSnippetsParse(t, "deck guide (seed/AGENTS_KALIDE.md)", deckGuide)
		agentGuideAssertInlineSnippetsParse(t, "library guide (libraryguide/AGENTS_KALIDE.md)", libraryGuide)
	})
}

// agentGuideTexts reads the two embedded full guides (the deck's
// seed/AGENTS_KALIDE.md and the library's libraryguide/AGENTS_KALIDE.md) as
// strings through the same helpers the scaffold writers and the catalog drift
// derivation use, and fails when either is empty. The AGENTS.md pointer bytes
// are asserted separately; the drift vocabulary lives in the full guides.
func agentGuideTexts(t *testing.T) (deckGuide, libraryGuide string) {
	t.Helper()

	deckGuide = string(mustReadSeed(deckGuideSeedPath))
	libraryGuide = string(mustReadLibraryFullGuide())
	if strings.TrimSpace(deckGuide) == "" {
		t.Fatalf("embedded deck full guide %s is empty", deckGuideSeedPath)
	}
	if strings.TrimSpace(libraryGuide) == "" {
		t.Fatal("embedded library full guide is empty")
	}
	return deckGuide, libraryGuide
}

// agentGuideFormatGroups returns the format vocabulary the union check and the
// deck-guide scope check carry, grouped by family: the accessor-derived format
// tokens, the authored layout-composition tokens and the deck guide's
// composition statements, and the delta's authored scope groups
// (agentGuideAuthoredScopeGroups). The composition and delta families have no
// canonical accessor (the composition documents the {{ template }} composition
// the loader implements; the delta families are keys parsed only inside
// unexported switches), so they are authored scope here, exactly as
// deckGuideCLI is.
func agentGuideFormatGroups(t *testing.T) []agentGuideTokenGroup {
	t.Helper()

	groups := []agentGuideTokenGroup{
		{name: "field type", tokens: template.ManifestFieldTypes()},
		{name: "body mode", tokens: template.BodyModes()},
		{name: "reserved name", tokens: template.ReservedNames()},
		{name: "layout helper", tokens: agentGuideLayoutHelpers(t)},
		{name: "context key", tokens: template.ContextKeys()},
		{name: "template.yaml top-level key", tokens: template.ManifestTopLevelKeys()},
		{name: "library.yaml key", tokens: template.LibraryMetaKeys()},
		{name: "reserved URL prefix", tokens: []string{template.MediaURLPrefix(), template.ThemeURLPrefix()}},
		{name: "layout composition token", tokens: agentGuideCompositionTokens(t)},
		{name: "layout composition statement", tokens: agentGuideDeckCompositionStatements, literal: true},
	}
	return append(groups, agentGuideAuthoredScopeGroups()...)
}

// agentGuideAuthoredScopeGroups returns the delta's authored token families —
// the format keys the implementation accepts but no exported accessor
// enumerates, so they cannot be derived and are authored here exactly as
// deckGuideCLI and the layout-composition families are. They belong to BOTH
// guides' scopes (the delta is documented in each), so they are appended to
// both the deck-guide scope (agentGuideFormatGroups) and the library-guide
// scope (agentGuideLibraryScopeGroups); a guide that omits one fails the drift
// test.
//
//   - section declaration key: name, accepted, min, max and max_depth are
//     parsed only by parseManifestSection's unexported inline switch (there is
//     no SectionKeys accessor to read max_depth from), and the delta adds
//     max_depth to that set.
//   - reserved item-context key: item.depth (`.item.depth` in a layout) is
//     reserved by the render/example context (item-context) but is outside
//     template.ContextKeys and template.ReservedNames, so it is authored here.
//   - image path key: the image field's roots, dir and extensions options
//     (image-field-roots) are parsed only by parseManifestField's unexported
//     switch.
//   - enum key: the enum field's variants and variants_from keys
//     (enum-variants-from) are parsed only by the same unexported switch.
func agentGuideAuthoredScopeGroups() []agentGuideTokenGroup {
	return []agentGuideTokenGroup{
		{name: "section declaration key", tokens: []string{"name", "accepted", "min", "max", "max_depth"}},
		{name: "reserved item-context key", tokens: []string{"item.depth"}},
		{name: "image path key", tokens: []string{"roots", "dir", "extensions"}},
		{name: "enum key", tokens: []string{"variants", "variants_from"}},
	}
}

// agentGuideCompositionTokens returns the `{{ template }}` layout-composition
// tokens both guides must carry (guide-layout-composition, example-layout-
// composition): the layer-relative layout name for a slide and for a section
// layout, and the `{{ template "<layout name>" . }}` action. The name form is
// built from the layout-directory constants so a renamed directory carries
// through; the action is authored here — the composition syntax has no
// canonical accessor.
func agentGuideCompositionTokens(t *testing.T) []string {
	t.Helper()

	return []string{
		`{{ template "<layout name>" . }}`,
		template.SlidesDir + "/<name>/" + template.LayoutFile,
		template.SectionsDir + "/<name>/" + template.LayoutFile,
	}
}

// agentGuideDeckCompositionStatements are the readable composition statements
// the deck guide must carry (guide-layout-composition): the deck's local layer
// may include an external-layer layout (allowed), an external-layer layout that
// includes a local-layer layout is a templates error, and an included layout's
// media helper binds to the invoking layer while a `section` call rebinds to
// the target's own layer.
var agentGuideDeckCompositionStatements = []string{
	"a local-layer layout may also include",
	"external-layer layout that includes a local-layer layout is a templates error",
	"helper binds to the invoking layer's",
	"call rebinds to the called template's own layer",
}

// agentGuideLibraryCompositionStatements are the same readable composition
// statements in the library guide's wording (guide-layout-composition): a
// deck's local-layer layout may include this library's layout, a layout here
// that includes a layout existing only in the deck's local layer is a templates
// error, and an included layout's media helper binds to the invoking layer
// while a `section` call rebinds to the target's own layer.
var agentGuideLibraryCompositionStatements = []string{
	"local-layer layout may additionally include an external library's layout",
	"a layout here that includes a layout existing only in the deck's local layer is a templates error",
	"helper binds to the invoking layer's",
	"call rebinds to the called template's own layer",
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

// agentGuideLibraryReservedContextNames returns the reserved top-level
// template-context names the library guide owns: deck, slide, item, raw and
// data. deck/slide/raw/data are the namespace roots of template.ContextKeys;
// item is the section-only context name ContextKeys does not enumerate (it is
// present only inside a section instance), so it is added explicitly. Every
// result is cross-checked against template.ReservedNames, so a context name the
// implementation no longer reserves fails here rather than being silently
// demanded of the guide.
func agentGuideLibraryReservedContextNames(t *testing.T) []string {
	t.Helper()

	names := make(map[string]bool)
	for _, key := range template.ContextKeys() {
		key = strings.TrimPrefix(key, ".")
		if i := strings.IndexByte(key, '.'); i >= 0 {
			key = key[:i]
		}
		names[key] = true
	}
	names["item"] = true

	reserved := make(map[string]bool)
	for _, name := range template.ReservedNames() {
		reserved[name] = true
	}
	out := make([]string, 0, len(names))
	for name := range names {
		if !reserved[name] {
			t.Errorf("library-guide reserved context name %q is not in template.ReservedNames", name)
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// agentGuideLibraryScopeGroups returns the token families the library guide
// owns (agent-guide-drift-check, library-agent-guide): the templates/ library
// layout, the library.yaml keys, the template.yaml schema and body modes, the
// layout language and its {{ template }} composition tokens and statements, the
// reserved context names deck/slide/item/raw/data, the layout helpers, the
// reserved context keys, every field type, the themes/media rules and the
// reserved `media:`/`theme:<name>/` prefixes, plus the delta's authored scope
// groups (agentGuideAuthoredScopeGroups) — the section-declaration keys, the
// reserved item-context key, the image path keys and the enum keys. It
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
	groups := []agentGuideTokenGroup{
		{name: "library layout entry", tokens: layout},
		{name: "library.yaml key", tokens: template.LibraryMetaKeys()},
		{name: "template.yaml top-level key", tokens: template.ManifestTopLevelKeys()},
		{name: "body mode", tokens: template.BodyModes()},
		{name: "layout language token", tokens: []string{template.LayoutFile, "html/template"}},
		{name: "layout composition token", tokens: agentGuideCompositionTokens(t)},
		{name: "layout composition statement", tokens: agentGuideLibraryCompositionStatements, literal: true},
		{name: "reserved name", tokens: agentGuideLibraryReservedContextNames(t)},
		{name: "layout helper", tokens: agentGuideLayoutHelpers(t)},
		{name: "context key", tokens: template.ContextKeys()},
		{name: "field type", tokens: template.ManifestFieldTypes()},
		{name: "themes/media rule token", tokens: []string{template.ThemeStylesheet, template.MediaURLPrefix(), template.ThemeURLPrefix(), "url()", "media \""}},
	}
	return append(groups, agentGuideAuthoredScopeGroups()...)
}

// agentGuideAssertGuideCarries asserts guide contains every token in every
// group, naming the guide and the token family in each failure. A format token
// is matched context-aware (agentGuideContainsToken), never as a raw substring;
// a literal group's composition statement is matched as a whitespace-normalised
// phrase (agentGuideProseContains). Both modes are selected by
// agentGuideGroupCarries.
func agentGuideAssertGuideCarries(t *testing.T, guideName, guide string, groups []agentGuideTokenGroup) {
	t.Helper()

	for _, group := range groups {
		if len(group.tokens) == 0 {
			t.Errorf("%s: canonical %s set is empty; an accessor derived no tokens", guideName, group.name)
			continue
		}
		for _, token := range group.tokens {
			if !agentGuideGroupCarries(group, guide, token) {
				t.Errorf("%s is missing the implemented %s %q", guideName, group.name, token)
			}
		}
	}
}

// agentGuideGroupCarries reports whether guide carries token under the group's
// matching mode: a literal group's composition statement is matched as a
// whitespace-normalised phrase, a format-token group's token by the
// context-aware rules.
func agentGuideGroupCarries(group agentGuideTokenGroup, guide, token string) bool {
	if group.literal {
		return agentGuideProseContains(guide, token)
	}
	return agentGuideContainsToken(guide, token)
}

// agentGuideProseContains reports whether guide carries phrase as a literal
// composition statement, tolerating the guides' hard line wrapping: both the
// guide and the phrase are whitespace-normalised (every run of whitespace,
// newlines included, collapses to one space) before the substring test, so a
// statement reflowed across a different line break still matches.
func agentGuideProseContains(guide, phrase string) bool {
	if phrase == "" {
		return false
	}
	return strings.Contains(agentGuideCollapseWhitespace(guide), agentGuideCollapseWhitespace(phrase))
}

// agentGuideCollapseWhitespace collapses every run of whitespace in s to a
// single space, so a phrase matches across the guides' hard line wraps.
func agentGuideCollapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
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

// agentGuideContainsToken reports whether guide carries token in a
// context-aware form: as a complete inline code span or fenced-code-block word,
// as a YAML mapping key (`token:`), or as a `kalide <token>` command. Ordinary
// prose never satisfies a token, and a token that is only a substring of a
// longer identifier (`init` in `init-library`, `theme` in `themes/`) does not
// count. Context keys (`deck.title`) are also accepted with their canonical
// leading dot (`.deck.title`).
//
// A reserved URL prefix (`media:`, `theme:`) is matched by a stricter,
// dedicated rule (agentGuideContainsReservedPrefix): because the token itself
// ends in `:`, the ordinary YAML-mapping-key and fenced-word paths would accept
// a bare `theme:` mapping key or prose, so those paths are bypassed for it.
func agentGuideContainsToken(guide, token string) bool {
	if token == "" {
		return false
	}
	if strings.HasSuffix(token, ":") {
		return agentGuideContainsReservedPrefix(guide, token)
	}
	for _, candidate := range agentGuideTokenCandidates(token) {
		if agentGuideCodeRegionsContain(guide, candidate) {
			return true
		}
		// YAML mapping-key form: `theme:`, optionally backticked.
		if agentGuideTextHasToken(guide, candidate+":") {
			return true
		}
		if agentGuideHasCommandForm(guide, candidate) {
			return true
		}
	}
	return false
}

// agentGuideContainsReservedPrefix reports whether guide carries a reserved URL
// prefix token (`media:`, `theme:`) in a context that actually declares it.
// Such a token is a prefix ending in `:`, so it is inherently ambiguous with a
// YAML mapping key (`theme: default`) and with ordinary prose; an ordinary
// code-region match would accept the deck guide's `theme: default` config
// example. It therefore counts only inside an inline backtick code span, or as
// a `url()`/`@import` argument (agentGuideReservedPrefixArg).
func agentGuideContainsReservedPrefix(guide, token string) bool {
	for _, span := range agentGuideInlineCodeSpans(guide) {
		if agentGuideTextHasToken(span, token) {
			return true
		}
	}
	return agentGuideReservedPrefixArg(guide, token)
}

// agentGuideReservedPrefixArg reports whether text carries a reserved URL
// prefix as the argument of a `url()` reference or an `@import` target: the
// token must be immediately preceded by `url(` or `@import`, allowing only
// whitespace and one optional opening quote in between. That context is what
// distinguishes a real prefix use — `url('theme:default/theme.css')`,
// `@import 'theme:default/theme.css'` — from the bare YAML mapping key
// `theme: default`, which has no `url(`/`@import` before it and so can no
// longer satisfy the `theme:` token.
func agentGuideReservedPrefixArg(text, token string) bool {
	if token == "" {
		return false
	}
	re := regexp.MustCompile(`(?:url\(|@import)[ \t]*['"]?` + regexp.QuoteMeta(token))
	return re.MatchString(text)
}

// agentGuideTokenCandidates returns the spellings of token that count. A
// dotted context key (`deck.title`) is written with a leading dot in the guides
// (`.deck.title`), so that alternate spelling is included; every other token is
// matched as written.
func agentGuideTokenCandidates(token string) []string {
	out := []string{token}
	if strings.Contains(token, ".") && !strings.HasPrefix(token, ".") {
		out = append(out, "."+token)
	}
	return out
}

// agentGuideCodeRegionsContain reports whether token appears inside an inline
// backtick code span or a fenced code block of guide, with identifier
// boundaries so a substring of a longer word never counts.
func agentGuideCodeRegionsContain(guide, token string) bool {
	for _, span := range agentGuideInlineCodeSpans(guide) {
		if agentGuideTextHasToken(span, token) {
			return true
		}
	}
	for _, block := range agentGuideFencedBlocks(guide) {
		if agentGuideTextHasToken(block, token) {
			return true
		}
	}
	return false
}

// agentGuideTextHasToken reports whether token occurs in text bounded by
// non-identifier characters. A trailing boundary is required only when the
// token ends in an identifier character, so tokens ending in punctuation
// (`media "`, `url()`, `slides/<name>/`) still match while `theme` does not
// match `theme.css`.
func agentGuideTextHasToken(text, token string) bool {
	if token == "" {
		return false
	}
	left := `(^|[^A-Za-z0-9_.-])`
	right := ``
	if agentGuideIdentifierByte(token[len(token)-1]) {
		right = `($|[^A-Za-z0-9_.-])`
	}
	return regexp.MustCompile(left + regexp.QuoteMeta(token) + right).MatchString(text)
}

// agentGuideHasCommandForm reports whether guide carries `kalide <token>` as a
// command reference, with a right boundary that keeps `init` from matching the
// `kalide init-library` command.
func agentGuideHasCommandForm(guide, token string) bool {
	right := ``
	if agentGuideIdentifierByte(token[len(token)-1]) {
		right = `($|[^A-Za-z0-9_-])`
	}
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_-])kalide[ \t]+` + regexp.QuoteMeta(token) + right)
	return re.MatchString(guide)
}

// agentGuideIdentifierByte reports whether b can be part of an identifier-like
// format token.
func agentGuideIdentifierByte(b byte) bool {
	switch {
	case b >= '0' && b <= '9', b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z':
		return true
	case b == '_', b == '-':
		return true
	}
	return false
}

// agentGuideInlineCodeSpan matches a single-line backtick code span.
var agentGuideInlineCodeSpan = regexp.MustCompile("`[^`\n]*`")

// agentGuideInlineCodeSpans returns the contents of every inline backtick code
// span in guide.
func agentGuideInlineCodeSpans(guide string) []string {
	var spans []string
	for _, m := range agentGuideInlineCodeSpan.FindAllString(guide, -1) {
		spans = append(spans, strings.Trim(m, "`"))
	}
	return spans
}

// agentGuideFencedBlocks returns the body of every ```-fenced code block in
// guide, following CommonMark fences: a fence opens on a run of at least three
// backticks (with an optional info string) and closes on a line of at least as
// many backticks carrying no info string.
func agentGuideFencedBlocks(guide string) []string {
	lines := strings.Split(guide, "\n")
	var blocks []string
	var current []string
	openLen := 0
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			n := 0
			for n < len(trimmed) && trimmed[n] == '`' {
				n++
			}
			info := strings.TrimSpace(trimmed[n:])
			switch {
			case !inBlock:
				inBlock = true
				openLen = n
				current = nil
			case n >= openLen && info == "":
				blocks = append(blocks, strings.Join(current, "\n"))
				inBlock = false
			default:
				// A fence line that cannot close the block (for example a
				// nested plain fence in an illustrative example) is content.
				current = append(current, line)
			}
			continue
		}
		if inBlock {
			current = append(current, line)
		}
	}
	if inBlock {
		blocks = append(blocks, strings.Join(current, "\n"))
	}
	return blocks
}

// agentGuideSnippet is one fenced block extracted from a guide.
type agentGuideSnippet struct {
	line int    // 1-based line of the opening fence
	info string // the fence's info string, "" for a plain fence
	body string // the block's content, fences excluded
}

// agentGuideSnippets returns every ```-fenced block in guide in source order.
func agentGuideSnippets(guide string) []agentGuideSnippet {
	lines := strings.Split(guide, "\n")
	var out []agentGuideSnippet
	var current []string
	openLen := 0
	var open *agentGuideSnippet
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			n := 0
			for n < len(trimmed) && trimmed[n] == '`' {
				n++
			}
			info := strings.TrimSpace(trimmed[n:])
			switch {
			case open == nil:
				open = &agentGuideSnippet{line: i + 1, info: info}
				openLen = n
				current = nil
			case n >= openLen && info == "":
				open.body = strings.Join(current, "\n")
				out = append(out, *open)
				open = nil
			default:
				current = append(current, line)
			}
			continue
		}
		if open != nil {
			current = append(current, line)
		}
	}
	if open != nil {
		open.body = strings.Join(current, "\n")
		out = append(out, *open)
	}
	return out
}

// agentGuideSnippetIsSlide reports whether body is a slide source: it opens with
// the `---` slide frontmatter delimiter and names a `template:`.
func agentGuideSnippetIsSlide(body string) bool {
	return strings.HasPrefix(strings.TrimSpace(body), slide.SlideDelimiter) &&
		strings.Contains(body, "\ntemplate:")
}

// agentGuideIllustrativeSnippets declares the fenced snippets that are
// intentionally illustrative and not standalone-loadable, keyed by
// agentGuideSnippetFingerprint. Any fenced snippet that is neither validated by
// a parser nor declared here makes the test fail, so an illustrative snippet is
// skipped on purpose, never silently ignored.
var agentGuideIllustrativeSnippets = map[string]string{
	"|kalide.yaml deck-wide settings": "the deck-directory tree is a layout illustration, not a kalide.yaml document",
	"|templates/":                     "the templates/ directory tree is a layout illustration, not a library.yaml document",
	"markdown|# people":               "the section-instance example is a fragment inside a slide, not a standalone slide source",
	`|{{ media "logo.svg" }}`:         "a layout action fragment; media resolves against a library at render time, so it is not standalone-loadable",
	"|slides/3-team.md:12 › column[1] › people[0] › name: required — add a name: value": "an error-message illustration, not a document",
	"|kalide init [path] Create a deck in the current directory.":                       "the CLI usage listing, not a parseable document",
}

// agentGuideSnippetFingerprint keys an illustrative snippet by its info string
// and first non-blank content line, normalised so trivial whitespace changes do
// not invalidate the declaration.
func agentGuideSnippetFingerprint(s agentGuideSnippet) string {
	return s.info + "|" + agentGuideFirstContentLine(s.body)
}

// agentGuideFirstContentLine returns body's first non-blank line with runs of
// whitespace normalised to single spaces.
func agentGuideFirstContentLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			return strings.Join(fields, " ")
		}
	}
	return ""
}

// agentGuideAssertSnippetsLoad extracts every fenced snippet from guide and
// either runs it through the real parser its shape names or asserts it is
// declared illustrative.
func agentGuideAssertSnippetsLoad(t *testing.T, guideName, guide string) {
	t.Helper()

	themes, err := theme.LoadDir(seedRoot, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("%s: theme.LoadDir(embedded seed) error = %v", guideName, err)
	}
	lib, err := template.LoadLibrary(seedRoot, template.TemplatesDir)
	if err != nil {
		t.Fatalf("%s: template.LoadLibrary(embedded seed) error = %v", guideName, err)
	}
	catalogue, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("%s: template.NewRegistryFromLibrary(embedded seed) error = %v", guideName, err)
	}

	snippets := agentGuideSnippets(guide)
	if len(snippets) == 0 {
		t.Fatalf("%s: no fenced snippets found, want the guide's examples to be extractable", guideName)
	}

	for _, snippet := range snippets {
		where := fmt.Sprintf("%s:%d", guideName, snippet.line)
		switch {
		case snippet.info == "yaml":
			agentGuideValidateYAMLSnippet(t, where, snippet.body, themes)
		case agentGuideSnippetIsSlide(snippet.body):
			if _, err := slide.Parse(where, []byte(snippet.body), catalogue); err != nil {
				t.Errorf("%s: slide source snippet does not parse: %v", where, err)
			}
		default:
			fingerprint := agentGuideSnippetFingerprint(snippet)
			if reason, ok := agentGuideIllustrativeSnippets[fingerprint]; ok && reason != "" {
				continue
			}
			t.Errorf("%s: fenced snippet (info %q, first line %q) is neither validated nor declared in agentGuideIllustrativeSnippets",
				where, snippet.info, agentGuideFirstContentLine(snippet.body))
		}
	}
}

// agentGuideAssertInlineSnippetsParse parses every single-line inline backtick
// code span of guide that carries a Go template action (`{{ … }}`) with the
// canonical template.LayoutFuncMap. This load-checks the layout examples the
// guides show inline — the `section`, `dict` and `list` helper calls among them
// — the same func map the loader's parse check binds, so a helper example the
// func map cannot parse fails the drift test rather than passing unexamined. A
// guide with no inline template snippet fails, so the check cannot pass
// vacuously.
func agentGuideAssertInlineSnippetsParse(t *testing.T, guideName, guide string) {
	t.Helper()

	funcs := template.LayoutFuncMap(nil, "/media")
	checked := 0
	for _, span := range agentGuideInlineCodeSpans(guide) {
		if !strings.Contains(span, "{{") {
			continue
		}
		checked++
		if _, err := htmltemplate.New("inline").Funcs(funcs).Parse(span); err != nil {
			t.Errorf("%s: inline template snippet %q does not parse under the layout func map: %v", guideName, span, err)
		}
	}
	if checked == 0 {
		t.Errorf("%s: no inline template snippet found, want the guide's layout examples to be extractable", guideName)
	}
}

// agentGuideValidateYAMLSnippet classifies a ```yaml snippet by its top-level
// keys and runs it through the matching real parser: a kalide.yaml document
// (title) through deck.LoadConfig, a library.yaml document (name/format)
// through template.LoadLibrary, and a template.yaml manifest or manifest
// fragment (fields/sections/body) through template.LoadLibrary on an in-memory
// library. A library.yaml snippet that declares named definitions
// (groups/field_sets/types) is additionally required to have them parsed onto
// its loading layer, so the delta's definitions section is validated, not
// silently ignored (named-library-definitions).
func agentGuideValidateYAMLSnippet(t *testing.T, where, body string, themes *theme.Registry) {
	t.Helper()

	switch {
	case agentGuideManifestFragment.MatchString(body):
		fsys := agentGuideManifestFS(body)
		if _, err := template.LoadLibrary(fsys, template.TemplatesDir); err != nil {
			t.Errorf("%s: template.yaml snippet does not load: %v", where, err)
		}
	case agentGuideDeckConfigKey.MatchString(body):
		fsys := fstest.MapFS{
			deck.ConfigFile: &fstest.MapFile{Data: []byte(body)},
		}
		if _, err := deck.LoadConfig(fsys, deck.ConfigFile, themes); err != nil {
			t.Errorf("%s: kalide.yaml snippet does not load: %v", where, err)
		}
	case agentGuideLibraryMetaKey.MatchString(body):
		fsys := fstest.MapFS{
			template.TemplatesDir + "/" + template.LibraryFile: &fstest.MapFile{Data: []byte(body)},
		}
		lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
		if err != nil {
			t.Errorf("%s: library.yaml snippet does not load: %v", where, err)
			return
		}
		if agentGuideLibraryDefinitionsKey.MatchString(body) && !agentGuideHasDefinitions(lib.Layers) {
			t.Errorf("%s: library.yaml snippet declares named definitions but the loader parsed none onto its layer", where)
		}
	default:
		t.Errorf("%s: yaml snippet is neither a kalide.yaml, library.yaml nor template.yaml fragment:\n%s", where, body)
	}
}

// agentGuideHasDefinitions reports whether any loading layer carries named
// definitions — a named accept-list, field group or field type
// (named-library-definitions).
func agentGuideHasDefinitions(layers []*template.LibraryLayer) bool {
	for _, layer := range layers {
		defs := layer.Definitions
		if defs != nil && (len(defs.Groups) > 0 || len(defs.FieldSets) > 0 || len(defs.Types) > 0) {
			return true
		}
	}
	return false
}

// agentGuideFixtureLibraryMeta is the in-memory library.yaml the manifest
// fixture supplies. Besides the required name/format it carries the owning
// layer's named definitions — a named accept-list (groups), a named field group
// (field_sets) and a named field type (types) — so a guide's template.yaml
// snippet may reference them as `@name`, `- include: name` and `type: name` and
// still load through the real parser, which resolves each name against the
// Definitions of the layer that owns the referencing template
// (named-library-definitions).
const agentGuideFixtureLibraryMeta = "name: guide-snippet\n" +
	"format: 1\n" +
	"groups:\n" +
	"  callouts: [person]\n" +
	"field_sets:\n" +
	"  meta:\n" +
	"    - name: subtitle\n" +
	"      type: text\n" +
	"      nullable: true\n" +
	"types:\n" +
	"  colour:\n" +
	"    type: enum\n" +
	"    variants: [red, green, blue]\n"

// agentGuideFixtureMedia lists the media/ files the manifest fixture supplies,
// keyed by their layer-relative path. They let an enum field's `variants_from`
// glob (for example media/bgs/*.jpg) expand against real files and an image
// field's media root resolve, so a guide's template.yaml snippet carrying
// either loads through the real parser (enum-variants-from, image-field-roots).
var agentGuideFixtureMedia = []string{
	template.MediaDir + "/bgs/left-1.jpg",
	template.MediaDir + "/bgs/right-8.jpg",
}

// agentGuideManifestFS builds an in-memory template library whose hello slide
// manifest is the snippet under test. The library supplies a person section
// template so a manifest's `accepted: [person]` reference resolves, the owning
// layer's named definitions so a `type:`/`include:`/`@group` reference resolves
// (agentGuideFixtureLibraryMeta), the media/ files a `variants_from` glob
// expands against (agentGuideFixtureMedia), and a minimal example whose
// frontmatter names title only when the snippet declares a title field.
func agentGuideManifestFS(manifest string) fstest.MapFS {
	example := "---\ntemplate: hello\n---\nAn example body.\n"
	if agentGuideManifestTitleField.MatchString(manifest) {
		example = "---\ntemplate: hello\ntitle: Example\n---\nAn example body.\n"
	}
	dir := template.TemplatesDir + "/"
	fsys := fstest.MapFS{
		dir + template.LibraryFile:                                      &fstest.MapFile{Data: []byte(agentGuideFixtureLibraryMeta)},
		dir + template.SlidesDir + "/hello/" + template.ManifestFile:    &fstest.MapFile{Data: []byte(manifest)},
		dir + template.SlidesDir + "/hello/" + template.LayoutFile:      &fstest.MapFile{Data: []byte("{{ .title }}{{ .body }}")},
		dir + template.SlidesDir + "/hello/" + template.ExampleFile:     &fstest.MapFile{Data: []byte(example)},
		dir + template.SectionsDir + "/person/" + template.ManifestFile: &fstest.MapFile{Data: []byte("body:\n  mode: disallowed\n")},
		dir + template.SectionsDir + "/person/" + template.LayoutFile:   &fstest.MapFile{Data: []byte("{{ .body }}")},
		dir + template.SectionsDir + "/person/" + template.ExampleFile:  &fstest.MapFile{Data: []byte("A person.\n")},
	}
	for _, media := range agentGuideFixtureMedia {
		fsys[dir+media] = &fstest.MapFile{Data: []byte("fixture media")}
	}
	return fsys
}

// agentGuideManifestFragment matches a top-level template.yaml schema key.
var agentGuideManifestFragment = regexp.MustCompile(`(?m)^(fields|sections|body)\s*:`)

// agentGuideDeckConfigKey matches the required kalide.yaml key.
var agentGuideDeckConfigKey = regexp.MustCompile(`(?m)^title\s*:`)

// agentGuideLibraryMetaKey matches a required library.yaml key.
var agentGuideLibraryMetaKey = regexp.MustCompile(`(?m)^(name|format)\s*:`)

// agentGuideLibraryDefinitionsKey matches a library.yaml named-definition key.
var agentGuideLibraryDefinitionsKey = regexp.MustCompile(`(?m)^(groups|field_sets|types)\s*:`)

// agentGuideManifestTitleField matches a declared field named title.
var agentGuideManifestTitleField = regexp.MustCompile(`(?m)^\s*-\s*name:\s*title\s*$`)
