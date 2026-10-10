package server

import (
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
)

// TestRewriteThemeCSS covers rewriteThemeCSS (theme-shared-media) purely over
// stylesheet bytes — no filesystem, no server — so it runs under -short.
//
// Both reserved prefixes under test are read from the canonical accessors
// internal/template.MediaURLPrefix and internal/template.ThemeURLPrefix rather
// than hardcoded: they are the same tokens phase-01's load-time validation
// reads, so this test cannot drift from the validation path.
//
// rewriteThemeCSS takes the serving layer's media/themes bases as arguments
// (plan.phase-04.task-21). Every case below states the bases it passes
// explicitly, and the expected served URLs are those bases joined with the
// cleaned reference path, built from the route constants the media and theme
// routes actually serve under: the primary layer's
// server.MediaPath/server.ThemesPath (primaryThemeCSSBases) and the external
// `templates:` layer's server.ExternalMediaPath/server.ExternalThemesPath
// (externalThemeCSSBases — the same value mediaHandler mounts the external
// routes with), so the rewriter's output is pinned to the routes it must match
// for whichever layer serves the stylesheet.
func TestRewriteThemeCSS(t *testing.T) {
	prefix := template.MediaURLPrefix()
	themePrefix := template.ThemeURLPrefix()
	if !strings.HasSuffix(MediaPath, "/") {
		t.Fatalf("MediaPath = %q, want a trailing slash so MediaPath + p is the served URL", MediaPath)
	}
	if !strings.HasSuffix(ThemesPath, "/") {
		t.Fatalf("ThemesPath = %q, want a trailing slash so ThemesPath + name + \"/\" + p is the served URL", ThemesPath)
	}
	if !strings.HasSuffix(ExternalMediaPath, "/") {
		t.Fatalf("ExternalMediaPath = %q, want a trailing slash so ExternalMediaPath + p is the served URL", ExternalMediaPath)
	}
	if !strings.HasSuffix(ExternalThemesPath, "/") {
		t.Fatalf("ExternalThemesPath = %q, want a trailing slash so ExternalThemesPath + name + \"/\" + p is the served URL", ExternalThemesPath)
	}

	// mediaURL is the expected rewritten form: double-quoted, standardised,
	// and rooted at the given serving layer's media base — the same URL the
	// layer's media route serves. The base argument is always the canonical
	// route constant (which carries exactly one trailing slash); a case that
	// supplies a base missing its slash still expects the canonical one, so
	// base normalisation is asserted against the literal route constant rather
	// than against the production normaliser.
	mediaURL := func(base, clean string) string {
		return `url("` + base + clean + `")`
	}

	// themeURL is the expected rewritten form for a reserved theme:<name>/
	// reference: double-quoted and rooted at the given serving layer's theme
	// base, the route the layer's themeFileHandler serves under.
	themeURL := func(base, name, clean string) string {
		return `url("` + base + name + "/" + clean + `")`
	}

	// Every case states the exact bytes expected; a case that must not be
	// rewritten sets want equal to in, so "unchanged" is asserted
	// byte-for-byte rather than inferred. The primary layer's cases pass
	// primaryThemeCSSBases (MediaPath/ThemesPath) explicitly; the external
	// cases pass externalThemeCSSBases (ExternalMediaPath/ExternalThemesPath);
	// the normalisation cases pass a base missing its trailing slash.
	tests := []struct {
		name  string
		in    string
		bases themeCSSBases
		want  string
	}{
		{
			name:  "single-quoted media url rewrites",
			in:    "a { background: url('" + prefix + "fonts/x.woff2'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + mediaURL(MediaPath, "fonts/x.woff2") + "; }",
		},
		{
			name:  "double-quoted media url rewrites",
			in:    "a { background: url(\"" + prefix + "fonts/x.woff2\"); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + mediaURL(MediaPath, "fonts/x.woff2") + "; }",
		},
		{
			name:  "unquoted media url rewrites",
			in:    "a { background: url(" + prefix + "fonts/x.woff2); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + mediaURL(MediaPath, "fonts/x.woff2") + "; }",
		},
		{
			name:  "whitespace-padded media url rewrites",
			in:    "a { background: url(  '" + prefix + "fonts/x.woff2'  ); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + mediaURL(MediaPath, "fonts/x.woff2") + "; }",
		},
		{
			name:  "media url path is cleaned before joining MediaPath",
			in:    "a { background: url('" + prefix + "fonts/../img/logo.svg'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + mediaURL(MediaPath, "img/logo.svg") + "; }",
		},
		{
			name:  "single-quoted theme url rewrites",
			in:    "a { background: url('" + themePrefix + "default/fonts/x.woff2'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + themeURL(ThemesPath, "default", "fonts/x.woff2") + "; }",
		},
		{
			name:  "double-quoted theme url rewrites",
			in:    "a { background: url(\"" + themePrefix + "default/fonts/x.woff2\"); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + themeURL(ThemesPath, "default", "fonts/x.woff2") + "; }",
		},
		{
			name:  "unquoted theme url rewrites",
			in:    "a { background: url(" + themePrefix + "default/fonts/x.woff2); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + themeURL(ThemesPath, "default", "fonts/x.woff2") + "; }",
		},
		{
			name:  "whitespace-padded theme url rewrites",
			in:    "a { background: url(  '" + themePrefix + "default/fonts/x.woff2'  ); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + themeURL(ThemesPath, "default", "fonts/x.woff2") + "; }",
		},
		{
			name:  "theme url path is cleaned before joining ThemesPath",
			in:    "a { background: url('" + themePrefix + "default/fonts/../img/logo.svg'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: " + themeURL(ThemesPath, "default", "img/logo.svg") + "; }",
		},
		{
			name:  "media reference inside an @imported stylesheet body rewrites identically",
			in:    "@import url('base.css');\n.logo { background: url('" + prefix + "img/logo.svg'); }\n",
			bases: primaryThemeCSSBases,
			want:  "@import url('base.css');\n.logo { background: " + mediaURL(MediaPath, "img/logo.svg") + "; }\n",
		},
		{
			name:  "theme reference inside an @imported stylesheet body rewrites identically",
			in:    "@import url('base.css');\n.logo { background: url('" + themePrefix + "default/img/logo.svg'); }\n",
			bases: primaryThemeCSSBases,
			want:  "@import url('base.css');\n.logo { background: " + themeURL(ThemesPath, "default", "img/logo.svg") + "; }\n",
		},
		{
			name:  "@import url with single-quoted media target is rewritten",
			in:    "@import url('" + prefix + "x.css');\n",
			bases: primaryThemeCSSBases,
			want:  "@import url(\"" + MediaPath + "x.css\");\n",
		},
		{
			name:  "@import url with double-quoted media target is rewritten",
			in:    "@import url(\"" + prefix + "x.css\");\n",
			bases: primaryThemeCSSBases,
			want:  "@import url(\"" + MediaPath + "x.css\");\n",
		},
		{
			name:  "@import url with unquoted media target is rewritten",
			in:    "@import url(" + prefix + "x.css);\n",
			bases: primaryThemeCSSBases,
			want:  "@import url(\"" + MediaPath + "x.css\");\n",
		},
		{
			name:  "@import url with theme target is rewritten",
			in:    "@import url('" + themePrefix + "default/theme.css');\n",
			bases: primaryThemeCSSBases,
			want:  "@import " + themeURL(ThemesPath, "default", "theme.css") + ";\n",
		},
		{
			name:  "bare double-quoted @import with media target is canonicalised to url()",
			in:    "@import \"" + prefix + "x.css\";\n",
			bases: primaryThemeCSSBases,
			want:  "@import url(\"" + MediaPath + "x.css\");\n",
		},
		{
			name:  "bare single-quoted @import with theme target is canonicalised to url()",
			in:    "@import '" + themePrefix + "default/x.css';\n",
			bases: primaryThemeCSSBases,
			want:  "@import " + themeURL(ThemesPath, "default", "x.css") + ";\n",
		},

		// The external `templates:` layer's stylesheets: the same reserved
		// prefixes rewrite onto the layer's own prefixes, never the primary
		// layer's. These mirror the primary forms above (url() in both quoting
		// styles, path cleaning, and both @import forms).
		{
			name:  "external single-quoted media url rewrites to ExternalMediaPath",
			in:    "a { background: url('" + prefix + "fonts/x.woff2'); }",
			bases: externalThemeCSSBases,
			want:  "a { background: " + mediaURL(ExternalMediaPath, "fonts/x.woff2") + "; }",
		},
		{
			name:  "external unquoted theme url rewrites to ExternalThemesPath",
			in:    "a { background: url(" + themePrefix + "default/fonts/x.woff2); }",
			bases: externalThemeCSSBases,
			want:  "a { background: " + themeURL(ExternalThemesPath, "default", "fonts/x.woff2") + "; }",
		},
		{
			name:  "external media url path is cleaned before joining ExternalMediaPath",
			in:    "a { background: url('" + prefix + "fonts/../img/logo.svg'); }",
			bases: externalThemeCSSBases,
			want:  "a { background: " + mediaURL(ExternalMediaPath, "img/logo.svg") + "; }",
		},
		{
			name:  "external theme url path is cleaned before joining ExternalThemesPath",
			in:    "a { background: url('" + themePrefix + "default/fonts/../img/logo.svg'); }",
			bases: externalThemeCSSBases,
			want:  "a { background: " + themeURL(ExternalThemesPath, "default", "img/logo.svg") + "; }",
		},
		{
			name:  "external @import url with media target is rewritten",
			in:    "@import url('" + prefix + "x.css');\n",
			bases: externalThemeCSSBases,
			want:  "@import url(\"" + ExternalMediaPath + "x.css\");\n",
		},
		{
			name:  "external @import url with theme target is rewritten",
			in:    "@import url('" + themePrefix + "default/theme.css');\n",
			bases: externalThemeCSSBases,
			want:  "@import " + themeURL(ExternalThemesPath, "default", "theme.css") + ";\n",
		},
		{
			name:  "external bare double-quoted @import with media target is canonicalised to url()",
			in:    "@import \"" + prefix + "x.css\";\n",
			bases: externalThemeCSSBases,
			want:  "@import url(\"" + ExternalMediaPath + "x.css\");\n",
		},
		{
			name:  "external bare single-quoted @import with theme target is canonicalised to url()",
			in:    "@import '" + themePrefix + "default/x.css';\n",
			bases: externalThemeCSSBases,
			want:  "@import " + themeURL(ExternalThemesPath, "default", "x.css") + ";\n",
		},

		// Base normalisation: a serving base missing its trailing slash joins
		// to the same single-slash route prefix the routes use. The expected
		// output is the canonical route constant, so these cases fail if the
		// helper does not add the missing slash. The companion "no doubling"
		// subtest below covers a base that already carries its single slash.
		{
			name: "primary bases missing a trailing slash normalise to one",
			in: "a { background: url('" + prefix + "fonts/x.woff2'); }\n" +
				"b { background: url('" + themePrefix + "default/bg.png'); }\n",
			bases: themeCSSBases{
				mediaBase:  strings.TrimSuffix(MediaPath, "/"),
				themesBase: strings.TrimSuffix(ThemesPath, "/"),
			},
			want: "a { background: " + mediaURL(MediaPath, "fonts/x.woff2") + "; }\n" +
				"b { background: " + themeURL(ThemesPath, "default", "bg.png") + "; }\n",
		},
		{
			name: "external bases missing a trailing slash normalise to one",
			in: "a { background: url('" + prefix + "fonts/x.woff2'); }\n" +
				"b { background: url('" + themePrefix + "default/bg.png'); }\n",
			bases: themeCSSBases{
				mediaBase:  strings.TrimSuffix(ExternalMediaPath, "/"),
				themesBase: strings.TrimSuffix(ExternalThemesPath, "/"),
			},
			want: "a { background: " + mediaURL(ExternalMediaPath, "fonts/x.woff2") + "; }\n" +
				"b { background: " + themeURL(ExternalThemesPath, "default", "bg.png") + "; }\n",
		},

		{
			name:  "theme-owned relative url stays byte-for-byte",
			in:    ".f { src: url('fonts/theme.woff2'); }",
			bases: primaryThemeCSSBases,
			want:  ".f { src: url('fonts/theme.woff2'); }",
		},
		{
			name:  "parent-relative url stays byte-for-byte",
			in:    ".f { src: url('../x'); }",
			bases: primaryThemeCSSBases,
			want:  ".f { src: url('../x'); }",
		},
		{
			name:  "root-absolute url stays byte-for-byte",
			in:    ".f { src: url('/abs'); }",
			bases: primaryThemeCSSBases,
			want:  ".f { src: url('/abs'); }",
		},
		{
			name:  "http url stays byte-for-byte",
			in:    ".f { src: url('http://x/y.woff2'); }",
			bases: primaryThemeCSSBases,
			want:  ".f { src: url('http://x/y.woff2'); }",
		},
		{
			name:  "data url stays byte-for-byte",
			in:    ".f { src: url('data:font/woff2;base64,AAAA'); }",
			bases: primaryThemeCSSBases,
			want:  ".f { src: url('data:font/woff2;base64,AAAA'); }",
		},
		{
			name:  "protocol-relative url stays byte-for-byte",
			in:    ".f { src: url('//host/x'); }",
			bases: primaryThemeCSSBases,
			want:  ".f { src: url('//host/x'); }",
		},
		{
			name:  "non-url text stays byte-for-byte",
			in:    "body { color: red; }\n@import \"base.css\";\n",
			bases: primaryThemeCSSBases,
			want:  "body { color: red; }\n@import \"base.css\";\n",
		},
		{
			name:  "whole stylesheet with nothing to rewrite is returned unchanged",
			in:    "@import url('base.css');\nbody { color: red; }\n.f { src: url('fonts/theme.woff2'); }\n",
			bases: primaryThemeCSSBases,
			want:  "@import url('base.css');\nbody { color: red; }\n.f { src: url('fonts/theme.woff2'); }\n",
		},
		{
			name:  "empty stylesheet stays empty",
			in:    "",
			bases: primaryThemeCSSBases,
			want:  "",
		},
		{
			name:  "reserved prefix with empty path stays byte-for-byte",
			in:    "a { background: url('" + prefix + "'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + prefix + "'); }",
		},
		{
			name:  "reserved prefix cleaning to dot stays byte-for-byte",
			in:    "a { background: url('" + prefix + ".'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + prefix + ".'); }",
		},
		{
			name:  "reserved prefix cleaning to dot-dot stays byte-for-byte",
			in:    "a { background: url('" + prefix + "..'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + prefix + "..'); }",
		},
		{
			name:  "reserved prefix with absolute path stays byte-for-byte",
			in:    "a { background: url('" + prefix + "/abs'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + prefix + "/abs'); }",
		},
		{
			name:  "reserved prefix escaping with dot-dot stays byte-for-byte",
			in:    "a { background: url('" + prefix + "../x'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + prefix + "../x'); }",
		},
		{
			name:  "theme prefix with empty name stays byte-for-byte",
			in:    "a { background: url('" + themePrefix + "/x'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + themePrefix + "/x'); }",
		},
		{
			name:  "theme prefix with empty path stays byte-for-byte",
			in:    "a { background: url('" + themePrefix + "default/'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + themePrefix + "default/'); }",
		},
		{
			name:  "theme path cleaning to dot stays byte-for-byte",
			in:    "a { background: url('" + themePrefix + "default/.'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + themePrefix + "default/.'); }",
		},
		{
			name:  "theme path cleaning to dot-dot stays byte-for-byte",
			in:    "a { background: url('" + themePrefix + "default/..'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + themePrefix + "default/..'); }",
		},
		{
			name:  "theme path escaping with dot-dot stays byte-for-byte",
			in:    "a { background: url('" + themePrefix + "default/../x'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + themePrefix + "default/../x'); }",
		},
		{
			name:  "theme path absolute stays byte-for-byte",
			in:    "a { background: url('" + themePrefix + "default//abs'); }",
			bases: primaryThemeCSSBases,
			want:  "a { background: url('" + themePrefix + "default//abs'); }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteThemeCSS([]byte(tt.in), tt.bases)
			if string(got) != tt.want {
				t.Errorf("rewriteThemeCSS(%q, %+v)\n = %q\nwant %q", tt.in, tt.bases, got, tt.want)
			}
		})
	}

	// The zero-argument form is the pre-layered default: it must be identical
	// to passing primaryThemeCSSBases explicitly, i.e. byte-for-byte today's
	// output. Every primary case above re-runs through the zero-argument call
	// and must produce the same bytes.
	t.Run("zero-argument form equals explicit primary bases", func(t *testing.T) {
		for _, tt := range tests {
			if tt.bases != primaryThemeCSSBases {
				continue
			}
			got := rewriteThemeCSS([]byte(tt.in))
			if string(got) != tt.want {
				t.Errorf("zero-argument rewriteThemeCSS(%q)\n = %q\nwant %q (the explicit primaryThemeCSSBases output)", tt.in, got, tt.want)
			}
		}
	})

	// A base that already carries its single trailing slash stays single: the
	// normaliser must not append a second one. The canonical route constants
	// end in exactly one slash, so a correct rewrite contains the constant's
	// own URL and never the doubled prefix.
	t.Run("a single trailing slash is not doubled", func(t *testing.T) {
		in := "a { background: url('" + prefix + "fonts/x.woff2'); }\n" +
			"b { background: url('" + themePrefix + "default/bg.png'); }\n"

		primaryOut := string(rewriteThemeCSS([]byte(in), primaryThemeCSSBases))
		if !strings.Contains(primaryOut, mediaURL(MediaPath, "fonts/x.woff2")) {
			t.Errorf("primary rewrite = %q, want it to contain %q", primaryOut, mediaURL(MediaPath, "fonts/x.woff2"))
		}
		if strings.Contains(primaryOut, MediaPath+"/") || strings.Contains(primaryOut, ThemesPath+"/") {
			t.Errorf("primary rewrite doubled a slash:\n%s", primaryOut)
		}

		externalOut := string(rewriteThemeCSS([]byte(in), externalThemeCSSBases))
		if !strings.Contains(externalOut, mediaURL(ExternalMediaPath, "fonts/x.woff2")) {
			t.Errorf("external rewrite = %q, want it to contain %q", externalOut, mediaURL(ExternalMediaPath, "fonts/x.woff2"))
		}
		if strings.Contains(externalOut, ExternalMediaPath+"/") || strings.Contains(externalOut, ExternalThemesPath+"/") {
			t.Errorf("external rewrite doubled a slash:\n%s", externalOut)
		}
	})

	// A media: reference is rewritten under the media base and a theme:<name>/
	// reference under the themes base of the bases that were passed, never the
	// other layer's: a base set produces only its own prefixes.
	t.Run("a base set emits only its own prefixes", func(t *testing.T) {
		in := "a { background: url('" + prefix + "fonts/x.woff2'); }\n" +
			"b { background: url('" + themePrefix + "default/bg.png'); }\n"

		primaryOut := string(rewriteThemeCSS([]byte(in), primaryThemeCSSBases))
		if !strings.Contains(primaryOut, mediaURL(MediaPath, "fonts/x.woff2")) {
			t.Errorf("primary rewrite = %q, want it to contain %q", primaryOut, mediaURL(MediaPath, "fonts/x.woff2"))
		}
		if !strings.Contains(primaryOut, themeURL(ThemesPath, "default", "bg.png")) {
			t.Errorf("primary rewrite = %q, want it to contain %q", primaryOut, themeURL(ThemesPath, "default", "bg.png"))
		}
		if strings.Contains(primaryOut, ExternalMediaPath) || strings.Contains(primaryOut, ExternalThemesPath) {
			t.Errorf("primary rewrite leaked an external prefix:\n%s", primaryOut)
		}

		externalOut := string(rewriteThemeCSS([]byte(in), externalThemeCSSBases))
		if !strings.Contains(externalOut, mediaURL(ExternalMediaPath, "fonts/x.woff2")) {
			t.Errorf("external rewrite = %q, want it to contain %q", externalOut, mediaURL(ExternalMediaPath, "fonts/x.woff2"))
		}
		if !strings.Contains(externalOut, themeURL(ExternalThemesPath, "default", "bg.png")) {
			t.Errorf("external rewrite = %q, want it to contain %q", externalOut, themeURL(ExternalThemesPath, "default", "bg.png"))
		}
		if strings.Contains(externalOut, MediaPath) || strings.Contains(externalOut, ThemesPath) {
			t.Errorf("external rewrite leaked a primary prefix:\n%s", externalOut)
		}
	})

	// A rewritten reference has no reserved prefix left, so rewriting the
	// output again must be a no-op: the rewrite is idempotent, under either
	// layer's bases.
	t.Run("rewrite is idempotent", func(t *testing.T) {
		in := "a { background: url('" + prefix + "fonts/x.woff2'); }\n" +
			"b { background: url('" + themePrefix + "default/bg.png'); }\n"

		once := rewriteThemeCSS([]byte(in), primaryThemeCSSBases)
		if !strings.Contains(string(once), mediaURL(MediaPath, "fonts/x.woff2")) {
			t.Fatalf("first rewrite = %q, want it to contain %q", once, mediaURL(MediaPath, "fonts/x.woff2"))
		}
		if !strings.Contains(string(once), themeURL(ThemesPath, "default", "bg.png")) {
			t.Fatalf("first rewrite = %q, want it to contain %q", once, themeURL(ThemesPath, "default", "bg.png"))
		}
		twice := rewriteThemeCSS(once, primaryThemeCSSBases)
		if string(twice) != string(once) {
			t.Errorf("second rewrite changed already-rewritten output:\n once = %q\ntwice = %q", once, twice)
		}

		extOnce := rewriteThemeCSS([]byte(in), externalThemeCSSBases)
		if !strings.Contains(string(extOnce), mediaURL(ExternalMediaPath, "fonts/x.woff2")) {
			t.Fatalf("first external rewrite = %q, want it to contain %q", extOnce, mediaURL(ExternalMediaPath, "fonts/x.woff2"))
		}
		extTwice := rewriteThemeCSS(extOnce, externalThemeCSSBases)
		if string(extTwice) != string(extOnce) {
			t.Errorf("second external rewrite changed already-rewritten output:\n once = %q\ntwice = %q", extOnce, extTwice)
		}
	})
}
