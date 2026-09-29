package mdcheck

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// testFile is the identity every table-driven case is checked as; positions in
// the expected Issues are therefore file-absolute for this name.
const testFile = "slide.md"

// issue is a small constructor so the expected-Issue tables stay readable.
func issue(line, col int, kind Kind, message, guidance string) Issue {
	return Issue{File: testFile, Line: line, Col: col, Kind: kind, Message: message, Guidance: guidance}
}

// sectionMarkerGuidance is the guidance Check reports for a heading inside a
// template that declares child sections: every heading at any depth is a
// section marker there (markdown-allowed-subset).
const sectionMarkerGuidance = "headings at any depth are section markers inside a template that declares child sections; move the text into the section's own body or a field"

// TestCheckAllowed is the accept side of the allow-list: every block and inline
// construct the Markdown subset permits must produce no Issue at all.
func TestCheckAllowed(t *testing.T) {
	tests := []struct {
		name string
		mode Mode
		src  string
	}{
		{"empty body", BodyMode, ""},
		{"empty inline", InlineMode, ""},
		{"paragraph", BodyMode, "plain paragraph\n"},
		{"level-2 heading", BodyMode, "## Heading\n"},
		{"level-3 heading", BodyMode, "### Heading\n"},
		{"bullet list", BodyMode, "- a\n- b\n"},
		{"numbered list", BodyMode, "1. a\n2. b\n"},
		{"single nesting level bullet", BodyMode, "- a\n  - b\n"},
		{"single nesting level numbered", BodyMode, "1. a\n   1. b\n"},
		{"bold italic code", BodyMode, "**bold** and *italic* and `code` and _em_\n"},
		{"bold italic combined", BodyMode, "***both***\n"},
		{"inline links", BodyMode, "[text](https://example.com) and [x](#label)\n"},
		{"hard line break", BodyMode, "line one  \nline two\n"},
		{"two headings", BodyMode, "## H\n\n### H3\n"},
		{"mixed accepted document", BodyMode, "## Section\n\nIntro **bold** *italic* `code` [link](#x).\n\n- one\n- two\n\n1. first\n2. second\n\n- outer\n  - inner\n"},
		{"inline mode inline constructs", InlineMode, "plain **bold** *italic* `code` [text](https://example.com)\n"},

		// Extension-syntax false positives that must not be flagged.
		{"equals in prose", BodyMode, "a == b\n"},
		{"tilde ranges", BodyMode, "~5 to ~10\n"},
		{"clock time", BodyMode, "10:30:45\n"},
		{"caret in prose", BodyMode, "x ^ y\n"},
		{"word-adjacent tildes", BodyMode, "foo~bar~baz\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(testFile, []byte(tt.src), Options{Mode: tt.mode})
			if len(got) != 0 {
				t.Fatalf("Check(%q, mode %s) = %v issues, want none", tt.src, tt.mode, got)
			}
		})
	}

	// The subheading gate: `##` and `###` are accepted as ordinary Markdown
	// subheadings where the enclosing template declares no child sections;
	// where it does declare child sections every heading at any depth is a
	// section marker the parser consumes, so none is accepted; and `#`
	// (reserved for section markers) and depths 4-6 stay refused in subheading
	// position either way (markdown-allowed-subset).
	t.Run("subheading policy", func(t *testing.T) {
		for _, s := range []string{
			"## Heading\n",
			"### Heading\n",
			"## H\n\n### H3\n",
			"intro text\n\n## Heading\n\n### Deeper\n",
		} {
			if got := Check(testFile, []byte(s), Options{Mode: BodyMode, TemplateDeclaresChildSections: false}); len(got) != 0 {
				t.Errorf("Check(%q) with no declared child sections = %v issues, want none", s, got)
			}
		}

		if got := Check(testFile, []byte("## Heading\n"), Options{Mode: BodyMode, TemplateDeclaresChildSections: true}); len(got) == 0 {
			t.Error("Check(## Heading) with declared child sections = no issues, want the subheading refused")
		}

		for _, s := range []string{"# H1\n", "#### H4\n", "##### H5\n", "###### H6\n"} {
			if got := Check(testFile, []byte(s), Options{Mode: BodyMode, TemplateDeclaresChildSections: false}); len(got) == 0 {
				t.Errorf("Check(%q) = no issues, want the heading refused in subheading position", s)
			}
		}
	})
}

// TestCheckDisallowed is the reject side: one case per disallowed construct,
// asserting the exact file, line, column, kind, message and guidance.
func TestCheckDisallowed(t *testing.T) {
	tests := []struct {
		name     string
		mode     Mode
		src      string
		declares bool
		want     []Issue
	}{
		{
			name: "image",
			mode: BodyMode,
			src:  "![alt](x.png)\n",
			want: []Issue{issue(1, 3, KindDisallowedConstruct,
				"images are not allowed in Markdown",
				"use the slide's image field instead of a Markdown image")},
		},
		{
			name: "image mid-paragraph",
			mode: BodyMode,
			src:  "see ![alt](x.png) here\n",
			want: []Issue{issue(1, 7, KindDisallowedConstruct,
				"images are not allowed in Markdown",
				"use the slide's image field instead of a Markdown image")},
		},
		{
			name: "table",
			mode: BodyMode,
			src:  "| a | b |\n| --- | --- |\n| 1 | 2 |\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"tables are not allowed",
				"use a section template or a list instead of a Markdown table")},
		},
		{
			name: "blockquote",
			mode: BodyMode,
			src:  "> quote\n",
			want: []Issue{issue(1, 3, KindDisallowedConstruct,
				"block quotes are not allowed",
				"remove the > quote markers and keep the text in the body")},
		},
		{
			name: "fenced code block",
			mode: BodyMode,
			src:  "```go\ncode\n```\n",
			want: []Issue{issue(2, 1, KindDisallowedConstruct,
				"fenced code blocks are not allowed",
				"remove the ``` code fence, or use inline code with backticks")},
		},
		{
			name: "indented code block",
			mode: BodyMode,
			src:  "    code\n",
			want: []Issue{issue(1, 5, KindDisallowedConstruct,
				"indented code blocks are not allowed",
				"remove the leading indentation, or use inline code with backticks")},
		},
		{
			name: "thematic break",
			mode: BodyMode,
			src:  "---\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"thematic breaks (---) are not allowed",
				"remove the --- line")},
		},
		{
			name: "indented thematic break",
			mode: BodyMode,
			src:  "  ---\n",
			want: []Issue{issue(1, 3, KindDisallowedConstruct,
				"thematic breaks (---) are not allowed",
				"remove the --- line")},
		},
		{
			name: "block raw HTML",
			mode: BodyMode,
			src:  "<div>hi</div>\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"raw HTML is not allowed",
				"write Markdown instead; HTML is not rendered")},
		},
		{
			name: "inline HTML br",
			mode: BodyMode,
			src:  "a <br> b\n",
			want: []Issue{issue(1, 3, KindDisallowedConstruct,
				"inline raw HTML is not allowed",
				"remove the HTML tag and use Markdown formatting")},
		},
		{
			name: "inline HTML span",
			mode: BodyMode,
			src:  "text <span>more\n",
			want: []Issue{issue(1, 6, KindDisallowedConstruct,
				"inline raw HTML is not allowed",
				"remove the HTML tag and use Markdown formatting")},
		},
		{
			name: "angle-bracket autolink",
			mode: BodyMode,
			src:  "<https://example.com>\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"angle-bracket autolinks are not allowed",
				"use an inline link: [text](https://example.com)")},
		},
		{
			name: "setext heading equals underline",
			mode: BodyMode,
			src:  "Title\n===\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"setext headings are not allowed",
				"use ATX headings instead: ## or ### at the start of the line")},
		},
		{
			name: "setext heading dash underline",
			mode: BodyMode,
			src:  "Sub\n---\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"setext headings are not allowed",
				"use ATX headings instead: ## or ### at the start of the line")},
		},
		{
			name: "reference-style link and definition",
			mode: BodyMode,
			src:  "[text][ref]\n\n[ref]: https://example.com\n",
			want: []Issue{
				issue(1, 2, KindDisallowedConstruct,
					"reference-style links are not allowed",
					"use an inline link: [text](url)"),
				issue(3, 1, KindDisallowedConstruct,
					"link reference definitions are not allowed",
					"use an inline link: [text](url)"),
			},
		},
		{
			name: "shortcut reference link and definition",
			mode: BodyMode,
			src:  "[ref]\n\n[ref]: https://example.com\n",
			want: []Issue{
				issue(1, 2, KindDisallowedConstruct,
					"reference-style links are not allowed",
					"use an inline link: [text](url)"),
				issue(3, 1, KindDisallowedConstruct,
					"link reference definitions are not allowed",
					"use an inline link: [text](url)"),
			},
		},
		{
			name: "link reference definition alone",
			mode: BodyMode,
			src:  "[ref]: https://example.com\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"link reference definitions are not allowed",
				"use an inline link: [text](url)")},
		},
		{
			name: "list nested two levels",
			mode: BodyMode,
			src:  "- a\n  - b\n    - c\n",
			want: []Issue{issue(3, 7, KindUnsupportedConstruct,
				"lists may be nested at most one level deep",
				"flatten the nested list into the parent list or a new slide")},
		},
		{
			name: "level-1 heading reserved for sections",
			mode: BodyMode,
			src:  "# H1\n",
			want: []Issue{issue(1, 3, KindUnsupportedConstruct,
				"heading level 1 is not allowed",
				"use ## or ###; # is reserved for slide sections and ####+ is not supported")},
		},
		{
			name: "level-4 heading",
			mode: BodyMode,
			src:  "#### H4\n",
			want: []Issue{issue(1, 6, KindUnsupportedConstruct,
				"heading level 4 is not allowed",
				"use ## or ###; # is reserved for slide sections and ####+ is not supported")},
		},
		{
			name: "level-5 heading in subheading position",
			mode: BodyMode,
			src:  "##### H5\n",
			want: []Issue{issue(1, 7, KindUnsupportedConstruct,
				"heading level 5 is not allowed",
				"use ## or ###; # is reserved for slide sections and ####+ is not supported")},
		},
		{
			name: "level-6 heading in subheading position",
			mode: BodyMode,
			src:  "###### H6\n",
			want: []Issue{issue(1, 8, KindUnsupportedConstruct,
				"heading level 6 is not allowed",
				"use ## or ###; # is reserved for slide sections and ####+ is not supported")},
		},

		// The subheading gate: when the enclosing template declares child
		// sections, every heading at any depth is a section marker the parser
		// consumes as a nested section instance, so no heading is accepted as
		// a Markdown subheading (markdown-allowed-subset).
		{
			name:     "level-1 heading when the template declares child sections",
			mode:     BodyMode,
			declares: true,
			src:      "# H1\n",
			want: []Issue{issue(1, 3, KindUnsupportedConstruct,
				"heading level 1 is not allowed", sectionMarkerGuidance)},
		},
		{
			name:     "level-2 heading when the template declares child sections",
			mode:     BodyMode,
			declares: true,
			src:      "## H2\n",
			want: []Issue{issue(1, 4, KindUnsupportedConstruct,
				"heading level 2 is not allowed", sectionMarkerGuidance)},
		},
		{
			name:     "level-3 heading when the template declares child sections",
			mode:     BodyMode,
			declares: true,
			src:      "### H3\n",
			want: []Issue{issue(1, 5, KindUnsupportedConstruct,
				"heading level 3 is not allowed", sectionMarkerGuidance)},
		},
		{
			name:     "level-6 heading when the template declares child sections",
			mode:     BodyMode,
			declares: true,
			src:      "###### H6\n",
			want: []Issue{issue(1, 8, KindUnsupportedConstruct,
				"heading level 6 is not allowed", sectionMarkerGuidance)},
		},

		// Extension-syntax heuristics in body text.
		{
			name: "strikethrough",
			mode: BodyMode,
			src:  "~~strike~~\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"strikethrough (~~text~~) is not supported",
				"remove the ~~ markers or use plain text")},
		},
		{
			name: "highlight",
			mode: BodyMode,
			src:  "==highlight==\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"highlight (==text==) is not supported",
				"remove the == markers")},
		},
		{
			name: "superscript",
			mode: BodyMode,
			src:  "^sup^\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"superscript (^text^) is not supported",
				"remove the ^ markers or write the text normally")},
		},
		{
			name: "subscript",
			mode: BodyMode,
			src:  "a ~sub~ b\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"subscript (~text~) is not supported",
				"remove the ~ markers or write the text normally")},
		},
		{
			name: "emoji shortcode",
			mode: BodyMode,
			src:  "a :smile: b\n",
			want: []Issue{issue(1, 1, KindDisallowedConstruct,
				"emoji shortcodes (:name:) are not supported",
				"remove the :name: shortcode or paste the emoji character instead")},
		},

		// Inline-text mode rejects every block construct.
		{
			name: "inline mode heading",
			mode: InlineMode,
			src:  "## Heading\n",
			want: []Issue{issue(1, 4, KindBlockConstruct,
				"a heading is not allowed in an inline text field",
				"use inline formatting only: bold, italic, inline code or a link")},
		},
		{
			name: "inline mode blockquote",
			mode: InlineMode,
			src:  "> quote\n",
			want: []Issue{issue(1, 3, KindBlockConstruct,
				"a block quote is not allowed in an inline text field",
				"use inline formatting only: bold, italic, inline code or a link")},
		},
		{
			name: "inline mode fenced code",
			mode: InlineMode,
			src:  "```\ncode\n```\n",
			want: []Issue{issue(2, 1, KindBlockConstruct,
				"a fenced code block is not allowed in an inline text field",
				"use inline formatting only: bold, italic, inline code or a link")},
		},
		{
			name: "inline mode indented code",
			mode: InlineMode,
			src:  "    code\n",
			want: []Issue{issue(1, 5, KindBlockConstruct,
				"an indented code block is not allowed in an inline text field",
				"use inline formatting only: bold, italic, inline code or a link")},
		},
		{
			name: "inline mode thematic break",
			mode: InlineMode,
			src:  "---\n",
			want: []Issue{issue(0, 0, KindBlockConstruct,
				"a thematic break is not allowed in an inline text field",
				"use inline formatting only: bold, italic, inline code or a link")},
		},
		{
			name: "inline mode list",
			mode: InlineMode,
			src:  "- a\n",
			want: []Issue{issue(1, 3, KindBlockConstruct,
				"a list is not allowed in an inline text field",
				"use inline formatting only: bold, italic, inline code or a link")},
		},
		{
			name: "inline mode second paragraph",
			mode: InlineMode,
			src:  "a\n\nb\n",
			want: []Issue{issue(3, 1, KindBlockConstruct,
				"a paragraph is not allowed in an inline text field",
				"use inline formatting only: bold, italic, inline code or a link")},
		},
		{
			name: "inline mode block raw HTML",
			mode: InlineMode,
			src:  "<div>x</div>\n",
			want: []Issue{issue(1, 1, KindBlockConstruct,
				"raw HTML is not allowed in an inline text field",
				"use inline formatting only: bold, italic, inline code or a link")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(testFile, []byte(tt.src), Options{Mode: tt.mode, TemplateDeclaresChildSections: tt.declares})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Check(%q, mode %s)\n got: %#v\nwant: %#v", tt.src, tt.mode, got, tt.want)
			}
		})
	}
}

// TestCheckExtensionSyntaxInCodeAndDestinations covers the two places the
// extension heuristics must never look: inline code content and link
// destinations (both are not plain ast.Text segments).
func TestCheckExtensionSyntaxInCodeAndDestinations(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"all extensions in inline code", "`~~strike~~ ==x== ^sup^ ~sub~ :smile:`\n"},
		{"all extensions in link destination", "[text](https://example.com/~~x~~/==y==/^z^/~s~/:e:)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Check(testFile, []byte(tt.src), Options{}); len(got) != 0 {
				t.Fatalf("Check(%q) = %v, want no issues", tt.src, got)
			}
		})
	}
}

// TestCheckBackslashEscapes verifies that escaped delimiters are literal text
// and are not matched by the extension heuristics.
func TestCheckBackslashEscapes(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"all escaped delimiters", "\\~sub\\~ \\=\\=hi\\=\\= \\^sup\\^ \\:smile\\: \\*bold\\* \\# h \\[link\\]\n"},
		{"escaped tildes inside text", "a\\~b\\~c\n"},
		{"escaped strike run", "\\~\\~strike\\~\\~\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Check(testFile, []byte(tt.src), Options{}); len(got) != 0 {
				t.Fatalf("Check(%q) = %v, want no issues", tt.src, got)
			}
		})
	}
}

// TestCheckStartLineOffset verifies that Options.StartLine makes the reported
// line file-absolute while columns stay fragment-relative.
func TestCheckStartLineOffset(t *testing.T) {
	got := Check("slides/3-team.md", []byte("## Heading\nbad ~~strike~~\n"), Options{StartLine: 10})
	want := []Issue{issue(11, 1, KindDisallowedConstruct,
		"strikethrough (~~text~~) is not supported",
		"remove the ~~ markers or use plain text")}
	want[0].File = "slides/3-team.md"
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Check with StartLine 10\n got: %#v\nwant: %#v", got, want)
	}
}

// TestCheckFixtures exercises real testdata files through the public API.
func TestCheckFixtures(t *testing.T) {
	t.Run("allowed fixture has no issues", func(t *testing.T) {
		src := readFixture(t, "allowed.md")
		if got := Check("allowed.md", src, Options{}); len(got) != 0 {
			t.Fatalf("Check(allowed.md) = %v issues, want none", got)
		}
	})

	t.Run("disallowed fixture reports each construct with position", func(t *testing.T) {
		src := readFixture(t, "disallowed.md")
		got := Check("disallowed.md", src, Options{})
		want := []Issue{
			issue(3, 1, KindDisallowedConstruct,
				"strikethrough (~~text~~) is not supported",
				"remove the ~~ markers or use plain text"),
			issue(3, 1, KindDisallowedConstruct,
				"highlight (==text==) is not supported",
				"remove the == markers"),
			issue(5, 13, KindDisallowedConstruct,
				"images are not allowed in Markdown",
				"use the slide's image field instead of a Markdown image"),
			issue(7, 3, KindDisallowedConstruct,
				"block quotes are not allowed",
				"remove the > quote markers and keep the text in the body"),
			issue(9, 4, KindDisallowedConstruct,
				"reference-style links are not allowed",
				"use an inline link: [text](url)"),
			issue(11, 1, KindDisallowedConstruct,
				"link reference definitions are not allowed",
				"use an inline link: [text](url)"),
		}
		for i := range want {
			want[i].File = "disallowed.md"
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Check(disallowed.md)\n got: %#v\nwant: %#v", got, want)
		}
	})
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return src
}
