package scaffold

// This file holds TestAgentGuidesMatchFormat, the agent-guide drift self-test
// (requirements.requirement.agent-guide-drift-check, phase-05 tasks 1-2), plus
// its per-guide scope tables and minimal helpers.
//
// The implemented format vocabulary is derived ONLY from the exported canonical
// accessors — template.ManifestFieldTypes, template.BodyModes,
// template.ReservedNames, template.ContextKeys, template.ManifestTopLevelKeys,
// template.LibraryMetaKeys, template.MediaURLPrefix (the reserved `media:`
// prefix) and template.LayoutFuncMap (the v1 `media` helper), plus
// deck.ConfigKeys — and the two guide byte streams are read through
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
//     complete format reference), including the reserved `media:` prefix;
//   - library-guide scope: the templates/ library layout, the library.yaml
//     keys, the template.yaml schema and body modes, the layout language, the
//     reserved names deck/slide, the `media` helper, the reserved context keys,
//     every field type, the themes/media rules and the reserved `media:`
//     prefix.
//
// Negative assertion: the kalide.yaml config keys and the init/version CLI are
// deliberately OUTSIDE the library-guide scope, so their absence from
// libraryguide/AGENTS.md is never asserted and cannot fail the test. The test
// also never asserts their textual absence — the library guide is free to
// mention them; it just is not required to.
//
// Matching is context-aware (feedback-17): a token counts only where the format
// actually declares it — as a complete backtick code span, as a YAML mapping
// key (`theme:`, optionally backticked), as a fenced-code-block word, or as a
// `kalide <cmd>` command. Ordinary prose never satisfies a token, and a token
// that is merely a substring of another identifier (`init` in `init-library`,
// `theme` in `themes/` or `theme.css`) does not count. The negative self-check
// subtest pins that behaviour.
//
// Fenced snippets are validated, not ignored (feedback-18): the ```-fenced
// blocks in each guide are extracted and the standalone ones are run through
// the real parsers — a kalide.yaml snippet through deck.LoadConfig, a slide
// source through internal/slide.Parse, a library.yaml snippet through
// template.LoadLibrary, and a template.yaml snippet through template.LoadLibrary
// on an in-memory fs.FS that supplies the person section template its
// `accepted: [person]` reference needs. A snippet that is intentionally
// illustrative and not standalone-loadable (a layout tree, the CLI usage
// listing, a `media` action fragment, a section-instance fragment, an error
// message) must be declared, with a reason, in agentGuideIllustrativeSnippets;
// any fenced snippet that is neither validated nor declared there fails the
// test, so no snippet is ever silently skipped.

import (
	"fmt"
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
				if agentGuideContainsToken(deckGuide, token) || agentGuideContainsToken(libraryGuide, token) {
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
		agentGuideAssertSnippetsLoad(t, "deck guide (seed/AGENTS.md)", deckGuide)
		agentGuideAssertSnippetsLoad(t, "library guide (libraryguide/AGENTS.md)", libraryGuide)
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
		{name: "media URL prefix", tokens: []string{template.MediaURLPrefix()}},
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
// reserved context keys, every field type, the themes/media rules and the
// reserved `media:` prefix. It deliberately omits the kalide.yaml config keys
// and the init/version CLI, which are deck-guide-only.
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
		{name: "themes/media rule token", tokens: []string{template.ThemeStylesheet, template.MediaURLPrefix(), "url()", "media \""}},
	}
}

// agentGuideAssertGuideCarries asserts guide contains every token in every
// group, naming the guide and the token family in each failure. Presence is
// context-aware (agentGuideContainsToken), never a raw substring match.
func agentGuideAssertGuideCarries(t *testing.T, guideName, guide string, groups []agentGuideTokenGroup) {
	t.Helper()

	for _, group := range groups {
		if len(group.tokens) == 0 {
			t.Errorf("%s: canonical %s set is empty; an accessor derived no tokens", guideName, group.name)
			continue
		}
		for _, token := range group.tokens {
			if !agentGuideContainsToken(guide, token) {
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

// agentGuideContainsToken reports whether guide carries token in a
// context-aware form: as a complete inline code span or fenced-code-block word,
// as a YAML mapping key (`token:`), or as a `kalide <token>` command. Ordinary
// prose never satisfies a token, and a token that is only a substring of a
// longer identifier (`init` in `init-library`, `theme` in `themes/`) does not
// count. Context keys (`deck.title`) are also accepted with their canonical
// leading dot (`.deck.title`).
func agentGuideContainsToken(guide, token string) bool {
	if token == "" {
		return false
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
	"|Ada is the first programmer.":   "the trailing prose of the section-instance example, not a standalone artifact",
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

// agentGuideValidateYAMLSnippet classifies a ```yaml snippet by its top-level
// keys and runs it through the matching real parser: a kalide.yaml document
// (title) through deck.LoadConfig, a library.yaml document (name/format)
// through template.LoadLibrary, and a template.yaml manifest or manifest
// fragment (fields/sections/body) through template.LoadLibrary on an in-memory
// library.
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
		if _, err := template.LoadLibrary(fsys, template.TemplatesDir); err != nil {
			t.Errorf("%s: library.yaml snippet does not load: %v", where, err)
		}
	default:
		t.Errorf("%s: yaml snippet is neither a kalide.yaml, library.yaml nor template.yaml fragment:\n%s", where, body)
	}
}

// agentGuideManifestFS builds an in-memory template library whose hello slide
// manifest is the snippet under test. The library supplies a person section
// template so a manifest's `accepted: [person]` reference resolves, and a
// minimal example whose frontmatter names title only when the snippet declares
// a title field.
func agentGuideManifestFS(manifest string) fstest.MapFS {
	example := "---\ntemplate: hello\n---\nAn example body.\n"
	if agentGuideManifestTitleField.MatchString(manifest) {
		example = "---\ntemplate: hello\ntitle: Example\n---\nAn example body.\n"
	}
	dir := template.TemplatesDir + "/"
	return fstest.MapFS{
		dir + template.LibraryFile:                                      &fstest.MapFile{Data: []byte("name: guide-snippet\nformat: 1\n")},
		dir + template.SlidesDir + "/hello/" + template.ManifestFile:    &fstest.MapFile{Data: []byte(manifest)},
		dir + template.SlidesDir + "/hello/" + template.LayoutFile:      &fstest.MapFile{Data: []byte("{{ .title }}{{ .body }}")},
		dir + template.SlidesDir + "/hello/" + template.ExampleFile:     &fstest.MapFile{Data: []byte(example)},
		dir + template.SectionsDir + "/person/" + template.ManifestFile: &fstest.MapFile{Data: []byte("body:\n  mode: disallowed\n")},
		dir + template.SectionsDir + "/person/" + template.LayoutFile:   &fstest.MapFile{Data: []byte("{{ .body }}")},
		dir + template.SectionsDir + "/person/" + template.ExampleFile:  &fstest.MapFile{Data: []byte("A person.\n")},
	}
}

// agentGuideManifestFragment matches a top-level template.yaml schema key.
var agentGuideManifestFragment = regexp.MustCompile(`(?m)^(fields|sections|body)\s*:`)

// agentGuideDeckConfigKey matches the required kalide.yaml key.
var agentGuideDeckConfigKey = regexp.MustCompile(`(?m)^title\s*:`)

// agentGuideLibraryMetaKey matches a required library.yaml key.
var agentGuideLibraryMetaKey = regexp.MustCompile(`(?m)^(name|format)\s*:`)

// agentGuideManifestTitleField matches a declared field named title.
var agentGuideManifestTitleField = regexp.MustCompile(`(?m)^\s*-\s*name:\s*title\s*$`)
