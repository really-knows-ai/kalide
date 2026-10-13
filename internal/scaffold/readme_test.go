package scaffold

// This file holds TestReadmeCommands, the README drift self-test
// (requirements.requirement.readme-commands, plan.phase-11.task-6). It reads
// the repository-root README.md — the user-facing documentation, not the
// embedded seed — and asserts two things:
//
//   - every user-facing command (init, with its optional [path] argument and
//     --local flag, plus init-library, config, start, check, render, templates,
//     templates <name>, upgrade, version and help) has its own documented
//     section; and
//   - every table-of-contents entry links to a heading that actually exists, so
//     every command section is reachable.
//
// Headings are matched by their GitHub-style slug (lowercased, punctuation and
// backticks stripped, spaces turned to hyphens) exactly as the README's anchors
// are written, so a missing command section or a stale/missing anchor fails the
// test. The command list is authored here because this package exposes no
// canonical accessor for command names; it is the readme-commands AC, not a
// format vocabulary.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// readmeHeadingLine matches an ATX heading (`#` through `######`) and captures
// its text, tolerating the closing-`#` run and surrounding whitespace.
var readmeHeadingLine = regexp.MustCompile(`^#{1,6}[ \t]+(.+?)[ \t]*#*[ \t]*$`)

// readmeTOCEntry matches a Markdown list entry that links to an in-document
// anchor and captures the anchor text without its leading `#`.
var readmeTOCEntry = regexp.MustCompile(`^\s*[-*][ \t]+\[[^\]]*\][ \t]*\(#([^)\s]+)\)[ \t]*$`)

// readmeTOCLinkLine matches any Markdown list entry carrying an in-document
// anchor link, whatever the anchor's exact shape. It is the loose detector the
// non-vacuity guard compares the strict parser against, so a malformed
// table-of-contents entry the strict parser skips is caught rather than
// silently ignored.
var readmeTOCLinkLine = regexp.MustCompile(`^\s*[-*][ \t]+.*\]\(#`)

// readmeCommandSlug pairs a user-facing command with the GitHub slug of the
// README heading that documents it. The readme-commands AC names the whole
// user-facing command set — including the config, check, render and help
// commands the typed-records delta added — and the templates <name> form is a
// section of its own, so it is listed separately.
var readmeCommandSlug = []struct {
	command string
	slug    string
}{
	{"init", "kalide-init"},
	{"init-library", "kalide-init-library"},
	{"config", "kalide-config"},
	{"start", "kalide-start"},
	{"check", "kalide-check"},
	{"render", "kalide-render"},
	{"templates", "kalide-templates"},
	{"templates <name>", "kalide-templates-name"},
	{"upgrade", "kalide-upgrade"},
	{"version", "kalide-version"},
	{"help", "kalide-help"},
}

// TestReadmeCommands reads the repository-root README.md and pins the
// readme-commands AC: every user-facing command is documented in its own section
// and reachable from the table of contents, and every table-of-contents entry
// resolves to an existing heading. It fails rather than passing vacuously when
// the README declares no headings or no table-of-contents entries.
func TestReadmeCommands(t *testing.T) {
	readme := mustReadRepoReadme(t)

	headings := readmeHeadingSlugs(readme)
	if len(headings) == 0 {
		t.Fatal("README.md declares no headings; the parser found nothing to match table-of-contents anchors against")
	}
	anchors := readmeTOCAnchors(readme)
	if len(anchors) == 0 {
		t.Fatal("README.md has no table-of-contents entries; the anchor check would pass vacuously")
	}
	if got := readmeTOCLinkLines(readme); got != len(anchors) {
		t.Fatalf("README.md has %d table-of-contents entries but the anchor parser extracted %d; a malformed entry was skipped", got, len(anchors))
	}
	anchorSet := make(map[string]bool, len(anchors))
	for _, anchor := range anchors {
		anchorSet[anchor] = true
	}

	t.Run("every command has a documented, reachable section", func(t *testing.T) {
		for _, want := range readmeCommandSlug {
			if !headings[want.slug] {
				t.Errorf("README.md does not document %q: no heading has the slug %q", want.command, want.slug)
			}
			if !anchorSet[want.slug] {
				t.Errorf("README.md's table of contents does not link to %q (slug %q); the command section is not reachable", want.command, want.slug)
			}
		}
	})

	t.Run("init documents its optional [path] argument and --local flag", func(t *testing.T) {
		if !strings.Contains(readme, "kalide init [path] [--local]") {
			t.Error("README.md does not document `kalide init [path] [--local]`; the optional [path] argument and --local flag must appear")
		}
	})

	t.Run("every table-of-contents entry links to an existing heading", func(t *testing.T) {
		for _, anchor := range anchors {
			if !headings[anchor] {
				t.Errorf("README.md table of contents links to #%s, which is not any heading's slug (stale or missing anchor)", anchor)
			}
		}
	})

	t.Run("the parser derives slugs and sees stale anchors", func(t *testing.T) {
		// A self-check on synthetic markdown so the real-README assertions
		// above cannot pass vacuously through a broken slug or anchor parser:
		// the GitHub slug of `### `kalide init`` must be kalide-init, both list
		// entries must be extracted, and an anchor with no heading must not
		// resolve.
		synthetic := "# Title\n\n## Alpha\n\n### `kalide init`\n\n- [Alpha](#alpha)\n- [Gone](#gone)\n"
		syntheticHeadings := readmeHeadingSlugs(synthetic)
		if !syntheticHeadings["alpha"] || !syntheticHeadings["kalide-init"] {
			t.Errorf("readmeHeadingSlugs(synthetic) = %v, want alpha and kalide-init", syntheticHeadings)
		}
		if syntheticHeadings["gone"] {
			t.Error("readmeHeadingSlugs(synthetic) reported the stale anchor #gone as a heading")
		}
		if got := len(readmeTOCAnchors(synthetic)); got != 2 {
			t.Errorf("readmeTOCAnchors(synthetic) extracted %d entries, want 2", got)
		}
	})
}

// mustReadRepoReadme reads the repository-root README.md. The test file lives at
// <root>/internal/scaffold/readme_test.go, so the file's own location — resolved
// through runtime.Caller — pins the root regardless of the test's working
// directory.
func mustReadRepoReadme(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate the repository root for README.md")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatalf("%s is empty", path)
	}
	return string(data)
}

// readmeHeadingSlugs returns the GitHub-slug of every ATX heading in readme,
// skipping fenced code blocks so an illustrative `#` line never counts as a
// heading. Duplicate slugs collapse to one set entry, as the anchor lookup only
// needs membership.
func readmeHeadingSlugs(readme string) map[string]bool {
	slugs := make(map[string]bool)
	for _, line := range readmeNonCodeLines(readme) {
		if m := readmeHeadingLine.FindStringSubmatch(line); m != nil {
			slugs[readmeSlug(m[1])] = true
		}
	}
	return slugs
}

// readmeTOCAnchors returns the in-document anchor of every Markdown list entry
// that links to a `#…` target — the README's table of contents. Fenced code
// blocks are skipped, so an illustrative markdown link never counts.
func readmeTOCAnchors(readme string) []string {
	var anchors []string
	for _, line := range readmeNonCodeLines(readme) {
		if m := readmeTOCEntry.FindStringSubmatch(line); m != nil {
			anchors = append(anchors, m[1])
		}
	}
	return anchors
}

// readmeTOCLinkLines returns the number of Markdown list entries in readme that
// carry an in-document anchor link. The strict parser's own count is compared
// against this loose count so it cannot silently skip a malformed entry.
func readmeTOCLinkLines(readme string) int {
	n := 0
	for _, line := range readmeNonCodeLines(readme) {
		if readmeTOCLinkLine.MatchString(line) {
			n++
		}
	}
	return n
}

// readmeSlug reproduces GitHub's heading-anchor slug for the README's headings:
// lowercase, drop every character that is not a letter, digit, underscore or
// hyphen (backticks, `<`/`>`, parentheses and slashes among them), then turn
// each space into a hyphen. Every heading and anchor in the README is ASCII, so
// the ASCII-only character class is exact.
func readmeSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == ' ':
			b.WriteRune(r)
		}
	}
	return strings.ReplaceAll(b.String(), " ", "-")
}

// readmeNonCodeLines returns readme's lines with ```/~~~ fenced code blocks
// removed, so heading and table-of-contents parsing never matches content of an
// illustrative snippet. A fence closes on a same-or-longer run of its own
// character.
func readmeNonCodeLines(readme string) []string {
	var out []string
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	for _, line := range strings.Split(readme, "\n") {
		trimmed := strings.TrimSpace(line)
		if n, ch := readmeFenceRun(trimmed); n >= 3 && (ch == '`' || ch == '~') {
			switch {
			case !inFence:
				inFence = true
				fenceChar = ch
				fenceLen = n
			case ch == fenceChar && n >= fenceLen:
				inFence = false
			}
			continue
		}
		if !inFence {
			out = append(out, line)
		}
	}
	return out
}

// readmeFenceRun returns the length of the leading run of one repeated fence
// character (` or ~) in trimmed, and that character; a line not opening with
// three or more such characters reports n < 3.
func readmeFenceRun(trimmed string) (int, byte) {
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0
	}
	ch := trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	return n, ch
}
