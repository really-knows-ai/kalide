package mdcheck

import (
	"reflect"
	"testing"

	"github.com/yuin/goldmark/ast"
)

// TestExtractLinksAllowed covers the accepted link destinations and the
// extracted label list. Every accepted link is returned with its flattened
// label text and its opening-'[' position.
func TestExtractLinksAllowed(t *testing.T) {
	t.Run("label link", func(t *testing.T) {
		links, issues := ExtractLinks(testFile, []byte("[a](#label)\n"), Options{})
		want := []Link{{Text: "a", Destination: "#label", Label: "label", Line: 1, Col: 2}}
		if !reflect.DeepEqual(links, want) {
			t.Fatalf("links = %#v, want %#v", links, want)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %v, want none", issues)
		}
	})

	t.Run("http link", func(t *testing.T) {
		links, issues := ExtractLinks(testFile, []byte("[a](http://example.com)\n"), Options{})
		want := []Link{{Text: "a", Destination: "http://example.com", Line: 1, Col: 2}}
		if !reflect.DeepEqual(links, want) {
			t.Fatalf("links = %#v, want %#v", links, want)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %v, want none", issues)
		}
	})

	t.Run("https link", func(t *testing.T) {
		links, issues := ExtractLinks(testFile, []byte("[a](https://example.com)\n"), Options{})
		want := []Link{{Text: "a", Destination: "https://example.com", Line: 1, Col: 2}}
		if !reflect.DeepEqual(links, want) {
			t.Fatalf("links = %#v, want %#v", links, want)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %v, want none", issues)
		}
	})

	t.Run("scheme case is matched case-insensitively", func(t *testing.T) {
		links, issues := ExtractLinks(testFile, []byte("[a](HTTP://EXAMPLE.COM)\n"), Options{})
		if len(links) != 1 || links[0].Destination != "HTTP://EXAMPLE.COM" || links[0].Label != "" {
			t.Fatalf("links = %#v, want one http link", links)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %v, want none", issues)
		}
	})

	t.Run("extracted label list in document order", func(t *testing.T) {
		src := "See [first](#one) and [second](https://example.com) plus [third](#three).\n"
		links, issues := ExtractLinks(testFile, []byte(src), Options{})
		want := []Link{
			{Text: "first", Destination: "#one", Label: "one", Line: 1, Col: 6},
			{Text: "second", Destination: "https://example.com", Line: 1, Col: 24},
			{Text: "third", Destination: "#three", Label: "three", Line: 1, Col: 59},
		}
		if !reflect.DeepEqual(links, want) {
			t.Fatalf("links = %#v, want %#v", links, want)
		}
		if len(issues) != 0 {
			t.Fatalf("issues = %v, want none", issues)
		}

		// The label list is exactly the #label targets, in order.
		var labels []string
		for _, l := range links {
			if l.Label != "" {
				labels = append(labels, l.Label)
			}
		}
		if want := []string{"one", "three"}; !reflect.DeepEqual(labels, want) {
			t.Fatalf("labels = %v, want %v", labels, want)
		}
	})

	t.Run("empty source yields nothing", func(t *testing.T) {
		links, issues := ExtractLinks(testFile, nil, Options{})
		if len(links) != 0 || len(issues) != 0 {
			t.Fatalf("links = %v, issues = %v, want none", links, issues)
		}
	})
}

// TestExtractLinksSchemeRejected covers every rejected scheme: mailto, ftp and
// relative (including protocol-relative and absolute filesystem paths).
func TestExtractLinksSchemeRejected(t *testing.T) {
	guidance := "use a #label link or an http(s) URL, for example [text](#label) or [text](https://example.com)"
	tests := []struct {
		name        string
		src         string
		wantMessage string
	}{
		{
			name:        "mailto",
			src:         "[a](mailto:x@example.com)\n",
			wantMessage: `link destination "mailto:x@example.com" uses the unsupported "mailto" scheme`,
		},
		{
			name:        "ftp",
			src:         "[a](ftp://example.com)\n",
			wantMessage: `link destination "ftp://example.com" uses the unsupported "ftp" scheme`,
		},
		{
			name:        "bare relative path",
			src:         "[a](other.md)\n",
			wantMessage: `relative link destination "other.md" is not allowed`,
		},
		{
			name:        "dot-slash relative path",
			src:         "[a](./other.md)\n",
			wantMessage: `relative link destination "./other.md" is not allowed`,
		},
		{
			name:        "parent relative path",
			src:         "[a](../other.md)\n",
			wantMessage: `relative link destination "../other.md" is not allowed`,
		},
		{
			name:        "protocol-relative",
			src:         "[a](//example.com)\n",
			wantMessage: `relative link destination "//example.com" is not allowed`,
		},
		{
			name:        "absolute filesystem path",
			src:         "[a](/assets/x.png)\n",
			wantMessage: `relative link destination "/assets/x.png" is not allowed`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links, issues := ExtractLinks(testFile, []byte(tt.src), Options{})
			// The link is still collected; its rejection is a separate Issue.
			if len(links) != 1 {
				t.Fatalf("links = %#v, want exactly one", links)
			}
			want := []Issue{issue(1, 2, KindLinkScheme, tt.wantMessage, guidance)}
			if !reflect.DeepEqual(issues, want) {
				t.Fatalf("issues = %#v, want %#v", issues, want)
			}
		})
	}
}

// injectedNode is a test-only goldmark node with a Kind outside the allow-list.
// It is never produced by the parser; the walker is handed one directly to
// prove the default-deny path rejects unknown kinds.
type injectedNode struct {
	ast.BaseInline
	kind ast.NodeKind
}

func (n *injectedNode) Kind() ast.NodeKind { return n.kind }

func (n *injectedNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// TestCheckDefaultDeny injects an unknown AST node kind and asserts the walker
// rejects it rather than letting an unrecognised construct through.
func TestCheckDefaultDeny(t *testing.T) {
	src := []byte("hello\n")
	ctx := &walkContext{
		file:      testFile,
		source:    src,
		lines:     newLineIndex(src),
		mode:      BodyMode,
		startLine: 1,
	}
	node := &injectedNode{kind: ast.NewNodeKind("mystery-box")}

	got := visit(ctx, node)
	want := []Issue{issue(0, 0, KindUnsupportedConstruct,
		"mystery-box is not allowed here",
		"use only the supported Markdown subset: paragraphs, ## or ### headings, "+
			"one level of bullet or numbered lists, bold, italic, inline code and [](…) links")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("visit(unknown kind)\n got: %#v\nwant: %#v", got, want)
	}
}
