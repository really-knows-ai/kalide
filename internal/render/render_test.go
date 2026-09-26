package render

import (
	"strings"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/slide"
	"github.com/really-knows-ai/ey-present/internal/template"
)

// TestRenderSlide covers RenderSlide on the built-in templates: the layout
// structure and field values of a title slide and a content slide with composed
// `columns` sections, number/date format selection, Markdown body-to-HTML,
// the slide label as the root anchor id, the speaker-notes aside, and
// html/template's escaping of text-field values.
func TestRenderSlide(t *testing.T) {
	reg := mustBuiltinRegistry(t)

	t.Run("title slide layout and fields", func(t *testing.T) {
		parsed := &slide.Slide{
			File:     "slides/1-intro.md",
			Template: "title",
			Frontmatter: map[string]any{
				"title":       "Quarterly Business Review",
				"subtitle":    "Performance, outlook and priorities",
				"date":        "2026-09-25",
				"date_format": "long",
			},
		}

		want := slideLines(
			"",
			`<section id="intro" class="ey-slide ey-slide--title" data-transition="fade" data-background-color="var(--ey-bg-dark)">`,
			`  <h1 class="ey-title">Quarterly Business Review</h1>`,
			`  <p class="ey-subtitle">Performance, outlook and priorities</p>`,
			`  <p class="ey-date">25 September 2026</p>`,
			`</section>`,
		)

		if got := renderSlideString(t, parsed, "intro", reg); got != want {
			t.Errorf("title slide mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("title slide omits absent optional fields", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/1-intro.md",
			Template:    "title",
			Frontmatter: map[string]any{"title": "Just a title"},
		}
		got := renderSlideString(t, parsed, "solo", reg)

		// An omitted subtitle or date must leave no element at all, not an
		// empty one.
		for _, forbidden := range []string{`ey-subtitle`, `ey-date`} {
			if strings.Contains(got, forbidden) {
				t.Errorf("title slide with no subtitle/date still contains %q:\n%s", forbidden, got)
			}
		}
		if !strings.Contains(got, `<h1 class="ey-title">Just a title</h1>`) {
			t.Errorf("title slide is missing the title heading:\n%s", got)
		}
	})

	t.Run("content slide with columns", func(t *testing.T) {
		parsed := contentColumnsSlide()

		want := slideLines(
			"",
			`<section id="growth" class="ey-slide ey-slide--content ey-slide--columns">`,
			`  <h2 class="ey-heading">Where the growth is coming from</h2>`,
			`  <p class="ey-metric fragment">1.25M</p>`,
			`  <p class="ey-date">25 September 2026</p>`,
			`  <div class="ey-body"><p>Revenue is up across every region, led by services.</p>`,
			`</div>`,
			`  `,
			`  <div class="ey-columns">`,
			`    `,
			`<div class="ey-column fragment">`,
			`  <h3 class="ey-column__title">Revenue</h3>`,
			`  <div class="ey-column__body"><p>Recurring revenue grew 18% year over year.</p>`,
			`</div>`,
			`</div>`,
			``,
			`<div class="ey-column fragment">`,
			`  <h3 class="ey-column__title">New customers</h3>`,
			`  <div class="ey-column__body"><p>We added 1,250 new logos in the quarter.</p>`,
			`</div>`,
			`</div>`,
			``,
			`  </div>`,
			`  `,
			`</section>`,
		)

		got := renderSlideString(t, parsed, "growth", reg)
		if got != want {
			t.Errorf("content slide mismatch\n got: %q\nwant: %q", got, want)
		}

		// The `columns` variant marker is driven by the layout enum, and each
		// section instance's rendered content appears in source order.
		first := strings.Index(got, "Recurring revenue grew 18% year over year.")
		second := strings.Index(got, "We added 1,250 new logos in the quarter.")
		if first < 0 || second < 0 || first >= second {
			t.Errorf("columns are not rendered in source order (first=%d second=%d):\n%s", first, second, got)
		}
	})

	t.Run("content default layout has no columns variant", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/2-plain.md",
			Template:    "content",
			Frontmatter: map[string]any{"heading": "Plain"},
		}
		got := renderSlideString(t, parsed, "plain", reg)

		if !strings.Contains(got, `<section id="plain" class="ey-slide ey-slide--content">`) {
			t.Errorf("default content layout is missing its root section class:\n%s", got)
		}
		for _, forbidden := range []string{`ey-slide--columns`, `ey-columns`, `ey-metric`, `ey-date`} {
			if strings.Contains(got, forbidden) {
				t.Errorf("default content slide contains %q:\n%s", forbidden, got)
			}
		}
	})

	t.Run("number formats", func(t *testing.T) {
		cases := []struct {
			name   string
			metric any
			format string // "" means the field's default_format
			want   string
		}{
			{"compact explicit", 1250000, "compact", "1.25M"},
			{"exact", 1250000, "exact", "1,250,000"},
			{"percent", 1.25, "percent", "125%"},
			{"default is compact", 1250000, "", "1.25M"},
			{"integer below one thousand", 42, "", "42"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fm := map[string]any{"heading": "Metric", "show_metric": true, "metric": tc.metric}
				if tc.format != "" {
					fm["metric_format"] = tc.format
				}
				parsed := &slide.Slide{File: "slides/3-metric.md", Template: "content", Frontmatter: fm}
				got := renderSlideString(t, parsed, "metric", reg)

				wantFrag := `<p class="ey-metric fragment">` + tc.want + `</p>`
				if !strings.Contains(got, wantFrag) {
					t.Errorf("metric %v with format %q: missing %q:\n%s", tc.metric, tc.format, wantFrag, got)
				}
			})
		}
	})

	t.Run("date formats", func(t *testing.T) {
		cases := []struct {
			name   string
			format string // "" means the field's default_format
			want   string
		}{
			{"long explicit", "long", "25 September 2026"},
			{"short", "short", "25 Sep 2026"},
			{"default is long", "", "25 September 2026"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fm := map[string]any{"heading": "Date", "as_of": "2026-09-25"}
				if tc.format != "" {
					fm["as_of_format"] = tc.format
				}
				parsed := &slide.Slide{File: "slides/4-date.md", Template: "content", Frontmatter: fm}
				got := renderSlideString(t, parsed, "date", reg)

				wantFrag := `<p class="ey-date">` + tc.want + `</p>`
				if !strings.Contains(got, wantFrag) {
					t.Errorf("as_of with format %q: missing %q:\n%s", tc.format, wantFrag, got)
				}
			})
		}
	})

	t.Run("markdown body to html", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/5-markdown.md",
			Template:    "content",
			Frontmatter: map[string]any{"heading": "Markdown"},
			Body: "First paragraph with **bold** and *italic* and `code`.\n\n" +
				"## A subheading\n\n" +
				"### A deeper heading\n\n" +
				"- one\n- two\n\n" +
				"See [the site](https://example.com).\n",
		}
		got := renderSlideString(t, parsed, "markdown", reg)

		want := `<div class="ey-body">` +
			`<p>First paragraph with <strong>bold</strong> and <em>italic</em> and <code>code</code>.</p>` + "\n" +
			`<h2>A subheading</h2>` + "\n" +
			`<h3>A deeper heading</h3>` + "\n" +
			`<ul>` + "\n" +
			`<li>one</li>` + "\n" +
			`<li>two</li>` + "\n" +
			`</ul>` + "\n" +
			`<p>See <a href="https://example.com">the site</a>.</p>` + "\n" +
			`</div>`
		if !strings.Contains(got, want) {
			t.Errorf("markdown body HTML mismatch\nwant fragment: %q\ngot: %q", want, got)
		}
	})

	t.Run("section body rendered to html", func(t *testing.T) {
		parsed := contentColumnsSlide()
		parsed.Sections[0].Body = "Grew **18%** year over year.\n\n- recurring\n- services\n"
		got := renderSlideString(t, parsed, "growth", reg)

		want := `<div class="ey-column__body">` +
			`<p>Grew <strong>18%</strong> year over year.</p>` + "\n" +
			`<ul>` + "\n" +
			`<li>recurring</li>` + "\n" +
			`<li>services</li>` + "\n" +
			`</ul>` + "\n" +
			`</div>`
		if !strings.Contains(got, want) {
			t.Errorf("section body HTML mismatch\nwant fragment: %q\ngot: %q", want, got)
		}
	})

	t.Run("label is the root section anchor id", func(t *testing.T) {
		parsed := contentColumnsSlide()
		got := renderSlideString(t, parsed, "growth", reg)
		if !strings.Contains(got, `<section id="growth"`) {
			t.Errorf("slide label is not the root anchor id:\n%s", got)
		}

		// The label is attribute-escaped even though the deck filename grammar
		// normally keeps it to [A-Za-z0-9_-].
		escaped := renderSlideString(t, parsed, `a"b<c>&d`, reg)
		if !strings.Contains(escaped, `<section id="a&#34;b&lt;c&gt;&amp;d"`) {
			t.Errorf("slide label is not attribute-escaped:\n%s", escaped)
		}
	})

	t.Run("notes aside only when a notes section exists", func(t *testing.T) {
		withNotes := &slide.Slide{
			File:        "slides/6-notes.md",
			Template:    "content",
			Frontmatter: map[string]any{"heading": "Notes"},
			Notes:       &slide.Notes{Body: "Pause on the metric so the number lands.\n"},
		}
		got := renderSlideString(t, withNotes, "notes", reg)

		wantAside := `<aside class="notes"><p>Pause on the metric so the number lands.</p>` + "\n" + `</aside>`
		if !strings.Contains(got, wantAside) {
			t.Errorf("notes slide is missing the rendered aside %q:\n%s", wantAside, got)
		}
		// The aside must sit inside the slide element, before the closing
		// </section>, so the reveal.js notes plugin finds it as a child.
		aside := strings.Index(got, wantAside)
		close := strings.LastIndex(got, "</section>")
		if aside < 0 || close < 0 || aside > close {
			t.Errorf("notes aside is not inside the slide element (aside=%d close=%d):\n%s", aside, close, got)
		}

		none := &slide.Slide{
			File:        "slides/7-no-notes.md",
			Template:    "content",
			Frontmatter: map[string]any{"heading": "No notes"},
		}
		plain := renderSlideString(t, none, "no-notes", reg)
		if strings.Contains(plain, "aside") {
			t.Errorf("slide without notes emits an aside:\n%s", plain)
		}
	})

	t.Run("text fields are escaped", func(t *testing.T) {
		cases := []struct {
			name string
			in   string
			want string
		}{
			{"ampersand", "R&D and Tom & Jerry", "R&amp;D and Tom &amp; Jerry"},
			{"angle brackets", "5 < 6", "5 &lt; 6"},
			{"double quotes", `He said "hi"`, "He said &quot;hi&quot;"},
			{"mixed", `a="b" & c='d'`, "a=&quot;b&quot; &amp; c='d'"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				parsed := &slide.Slide{
					File:        "slides/8-esc.md",
					Template:    "title",
					Frontmatter: map[string]any{"title": tc.in},
				}
				got := renderSlideString(t, parsed, "esc", reg)

				wantFrag := `<h1 class="ey-title">` + tc.want + `</h1>`
				if !strings.Contains(got, wantFrag) {
					t.Errorf("text field %q is not escaped to %q:\n%s", tc.in, wantFrag, got)
				}
			})
		}
	})

	t.Run("raw html in a text field is omitted", func(t *testing.T) {
		parsed := &slide.Slide{
			File:        "slides/9-script.md",
			Template:    "title",
			Frontmatter: map[string]any{"title": `<script>alert("x") & 'y'</script>`},
		}
		got := renderSlideString(t, parsed, "script", reg)

		// goldmark drops raw HTML rather than passing it through; either way no
		// executable script may reach the deck.
		if strings.Contains(got, "<script>") {
			t.Errorf("raw <script> from a text field reached the output:\n%s", got)
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		parsed := contentColumnsSlide()
		first := renderSlideString(t, parsed, "growth", reg)
		second := renderSlideString(t, parsed, "growth", reg)
		if first != second {
			t.Errorf("RenderSlide is not deterministic:\nfirst:  %q\nsecond: %q", first, second)
		}
	})

	t.Run("definition failures are RenderErrors", func(t *testing.T) {
		parsed := contentColumnsSlide()

		if _, err := RenderSlide(nil, "x", reg); err == nil {
			t.Error("nil slide: want an error")
		} else if _, ok := err.(*RenderError); !ok {
			t.Errorf("nil slide: got %T, want *RenderError", err)
		}

		if _, err := RenderSlide(parsed, "x", nil); err == nil {
			t.Error("nil registry: want an error")
		} else if re, ok := err.(*RenderError); !ok {
			t.Errorf("nil registry: got %T, want *RenderError", err)
		} else if re.File != parsed.File {
			t.Errorf("nil registry: RenderError.File = %q, want %q", re.File, parsed.File)
		}

		unknown := *parsed
		unknown.Template = "does-not-exist"
		if _, err := RenderSlide(&unknown, "x", reg); err == nil {
			t.Error("unknown template: want an error")
		} else if !strings.Contains(err.Error(), "unknown slide template") {
			t.Errorf("unknown template: error %q does not name the unknown template", err)
		}

		sectionAsSlide := *parsed
		sectionAsSlide.Template = "column"
		if _, err := RenderSlide(&sectionAsSlide, "x", reg); err == nil {
			t.Error("section template used as a slide: want an error")
		} else if !strings.Contains(err.Error(), "not a slide template") {
			t.Errorf("section-as-slide: error %q does not reject the usage", err)
		}
	})
}

// contentColumnsSlide builds the parsed model of the built-in content template
// in its columns variant, with two resolved column section instances. It is the
// fixture shared by the content, anchor, notes and determinism subtests.
func contentColumnsSlide() *slide.Slide {
	return &slide.Slide{
		File:     "slides/2-growth.md",
		Template: "content",
		Frontmatter: map[string]any{
			"heading":       "Where the growth is coming from",
			"layout":        "columns",
			"metric":        1250000,
			"metric_format": "compact",
			"show_metric":   true,
			"as_of":         "2026-09-25",
			"as_of_format":  "long",
		},
		Body: "Revenue is up across every region, led by services.",
		Sections: []slide.Section{
			{
				Name:        "columns",
				Template:    "column",
				Frontmatter: map[string]any{"title": "Revenue"},
				Body:        "Recurring revenue grew 18% year over year.",
			},
			{
				Name:        "columns",
				Template:    "column",
				Frontmatter: map[string]any{"title": "New customers"},
				Body:        "We added 1,250 new logos in the quarter.",
			},
		},
	}
}

// renderSlideString renders one slide and returns the fragment as a string,
// failing the test on a render error.
func renderSlideString(t *testing.T, parsed *slide.Slide, label string, reg *template.Registry) string {
	t.Helper()
	out, err := RenderSlide(parsed, label, reg)
	if err != nil {
		t.Fatalf("RenderSlide(%q): %v", label, err)
	}
	return string(out)
}

// slideLines joins golden lines with newlines and appends the trailing newline
// every layout ends with. An empty first element gives the leading newline the
// layout templates' leading comment leaves in the output; a line may be only
// spaces when the layout leaves indentation behind around an omitted branch.
func slideLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}
