package server

import (
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
)

// TestRewriteThemeCSS covers rewriteThemeCSS (theme-shared-media) purely over
// stylesheet bytes — no filesystem, no server — so it runs under -short.
//
// The prefix under test is read from the canonical accessor
// internal/template.MediaURLPrefix rather than hardcoded: it is the same token
// phase-01's load-time validation reads, so this test cannot drift from the
// validation path. The expected served URL is built from server.MediaPath, the
// prefix the media route actually serves under, so the rewriter's output is
// pinned to the route it must match.
func TestRewriteThemeCSS(t *testing.T) {
	prefix := template.MediaURLPrefix()
	if !strings.HasSuffix(MediaPath, "/") {
		t.Fatalf("MediaPath = %q, want a trailing slash so MediaPath + p is the served URL", MediaPath)
	}

	// mediaURL is the expected rewritten form: double-quoted, standardised,
	// and rooted at MediaPath — the same URL the layout `media` helper returns.
	mediaURL := func(clean string) string {
		return `url("` + MediaPath + clean + `")`
	}

	// Every case states the exact bytes expected; a case that must not be
	// rewritten sets want equal to in, so "unchanged" is asserted
	// byte-for-byte rather than inferred.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single-quoted media url rewrites",
			in:   "a { background: url('" + prefix + "fonts/x.woff2'); }",
			want: "a { background: " + mediaURL("fonts/x.woff2") + "; }",
		},
		{
			name: "double-quoted media url rewrites",
			in:   "a { background: url(\"" + prefix + "fonts/x.woff2\"); }",
			want: "a { background: " + mediaURL("fonts/x.woff2") + "; }",
		},
		{
			name: "unquoted media url rewrites",
			in:   "a { background: url(" + prefix + "fonts/x.woff2); }",
			want: "a { background: " + mediaURL("fonts/x.woff2") + "; }",
		},
		{
			name: "whitespace-padded media url rewrites",
			in:   "a { background: url(  '" + prefix + "fonts/x.woff2'  ); }",
			want: "a { background: " + mediaURL("fonts/x.woff2") + "; }",
		},
		{
			name: "media url path is cleaned before joining MediaPath",
			in:   "a { background: url('" + prefix + "fonts/../img/logo.svg'); }",
			want: "a { background: " + mediaURL("img/logo.svg") + "; }",
		},
		{
			name: "media reference inside an @imported stylesheet body rewrites identically",
			in:   "@import url('base.css');\n.logo { background: url('" + prefix + "img/logo.svg'); }\n",
			want: "@import url('base.css');\n.logo { background: " + mediaURL("img/logo.svg") + "; }\n",
		},
		{
			name: "@import url with single-quoted media target is never rewritten",
			in:   "@import url('" + prefix + "x');\n",
			want: "@import url('" + prefix + "x');\n",
		},
		{
			name: "@import url with double-quoted media target is never rewritten",
			in:   "@import url(\"" + prefix + "x\");\n",
			want: "@import url(\"" + prefix + "x\");\n",
		},
		{
			name: "@import url with unquoted media target is never rewritten",
			in:   "@import url(" + prefix + "x);\n",
			want: "@import url(" + prefix + "x);\n",
		},
		{
			name: "theme-owned relative url stays byte-for-byte",
			in:   ".f { src: url('fonts/theme.woff2'); }",
			want: ".f { src: url('fonts/theme.woff2'); }",
		},
		{
			name: "parent-relative url stays byte-for-byte",
			in:   ".f { src: url('../x'); }",
			want: ".f { src: url('../x'); }",
		},
		{
			name: "root-absolute url stays byte-for-byte",
			in:   ".f { src: url('/abs'); }",
			want: ".f { src: url('/abs'); }",
		},
		{
			name: "http url stays byte-for-byte",
			in:   ".f { src: url('http://x/y.woff2'); }",
			want: ".f { src: url('http://x/y.woff2'); }",
		},
		{
			name: "data url stays byte-for-byte",
			in:   ".f { src: url('data:font/woff2;base64,AAAA'); }",
			want: ".f { src: url('data:font/woff2;base64,AAAA'); }",
		},
		{
			name: "protocol-relative url stays byte-for-byte",
			in:   ".f { src: url('//host/x'); }",
			want: ".f { src: url('//host/x'); }",
		},
		{
			name: "non-url text stays byte-for-byte",
			in:   "body { color: red; }\n@import \"base.css\";\n",
			want: "body { color: red; }\n@import \"base.css\";\n",
		},
		{
			name: "whole stylesheet with nothing to rewrite is returned unchanged",
			in:   "@import url('base.css');\nbody { color: red; }\n.f { src: url('fonts/theme.woff2'); }\n",
			want: "@import url('base.css');\nbody { color: red; }\n.f { src: url('fonts/theme.woff2'); }\n",
		},
		{
			name: "empty stylesheet stays empty",
			in:   "",
			want: "",
		},
		{
			name: "reserved prefix with empty path stays byte-for-byte",
			in:   "a { background: url('" + prefix + "'); }",
			want: "a { background: url('" + prefix + "'); }",
		},
		{
			name: "reserved prefix cleaning to dot stays byte-for-byte",
			in:   "a { background: url('" + prefix + ".'); }",
			want: "a { background: url('" + prefix + ".'); }",
		},
		{
			name: "reserved prefix cleaning to dot-dot stays byte-for-byte",
			in:   "a { background: url('" + prefix + "..'); }",
			want: "a { background: url('" + prefix + "..'); }",
		},
		{
			name: "reserved prefix with absolute path stays byte-for-byte",
			in:   "a { background: url('" + prefix + "/abs'); }",
			want: "a { background: url('" + prefix + "/abs'); }",
		},
		{
			name: "reserved prefix escaping with dot-dot stays byte-for-byte",
			in:   "a { background: url('" + prefix + "../x'); }",
			want: "a { background: url('" + prefix + "../x'); }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteThemeCSS([]byte(tt.in))
			if string(got) != tt.want {
				t.Errorf("rewriteThemeCSS(%q)\n = %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}

	// A rewritten reference has no reserved prefix left, so rewriting the
	// output again must be a no-op: the rewrite is idempotent.
	t.Run("rewrite is idempotent", func(t *testing.T) {
		in := "a { background: url('" + prefix + "fonts/x.woff2'); }"
		once := rewriteThemeCSS([]byte(in))
		if !strings.Contains(string(once), mediaURL("fonts/x.woff2")) {
			t.Fatalf("first rewrite = %q, want it to contain %q", once, mediaURL("fonts/x.woff2"))
		}
		twice := rewriteThemeCSS(once)
		if string(twice) != string(once) {
			t.Errorf("second rewrite changed already-rewritten output:\n once = %q\ntwice = %q", once, twice)
		}
	})
}
