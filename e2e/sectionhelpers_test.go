package e2e

// This file is the phase-3 task-12 end-to-end test for the render-time
// `section` layout helper and the reserved `.raw` context (section-helper,
// raw-source-context): it drives a served deck through the real kalide binary
// via the harness, with:
//
//   - a slide layout that renders a footer through `{{ section "footer" }}`,
//     where `footer` is a section template reached ONLY through the helper call
//     and never authored in the slide's Markdown — not a declared child section
//     — so the deck fails unless reachableTemplates follows the helper-call edge
//     into the shared namespace (section-helper);
//   - a second helper call that passes the slide's ORIGINAL body through to a
//     `callout` target as `.raw.body`, so the target renders the source body
//     (raw-source-context).
//
// It then asserts the served page carries the helper-rendered footer HTML and
// the passed-through body, proving the render-time section helper and the
// `.raw` context work through the built binary.
//
// It builds and drives the real binary, so it is skipped under -short.

import (
	"strings"
	"testing"
)

// writeSectionHelpersLibrary lays down a minimal, self-contained template
// library under templates/: one theme, the `main` slide template whose layout
// uses the section helper, and the two helper-only section templates `footer`
// and `callout`. Neither section template is a declared child of `main`:
// footer is reached only through `{{ section "footer" }}`, and callout only
// through the body pass-through call `{{ section "callout" (dict "label" …)
// .raw.body }}`.
func writeSectionHelpersLibrary(t *testing.T, h *Harness) {
	t.Helper()

	files := map[string]string{
		"templates/library.yaml": "name: sectionhelpers\n" +
			"description: e2e fixture library for the render-time section helper\n" +
			"format: 1\n",

		"templates/themes/plain/theme.css": "body { margin: 0; }\n",

		// footer: no fields, no body; rendered purely through the helper call.
		"templates/sections/footer/template.yaml": "description: a footer rendered only through the section helper\n" +
			"fields: []\n" +
			"body:\n" +
			"  mode: disallowed\n",
		"templates/sections/footer/layout.html.tmpl": "<footer class=\"e2e-helper-footer\">Rendered by the section helper</footer>\n",
		"templates/sections/footer/example.md":       "# footer\n",

		// callout: declares the `label` field the call supplies and renders the
		// body the caller passes through as `.raw.body`. The loader's step-7
		// example execution now supplies the reserved `.raw` context
		// (checkLibraryExample), so the layout reads `.raw.body` unguarded; the
		// real renderer executes it with the passed-through source body.
		"templates/sections/callout/template.yaml": "description: a callout that renders a helper-supplied label and body\n" +
			"fields:\n" +
			"  - name: label\n" +
			"    type: text\n" +
			"    required: true\n" +
			"body:\n" +
			"  mode: optional\n",
		"templates/sections/callout/layout.html.tmpl": "<div class=\"e2e-callout\">" +
			"<span class=\"e2e-callout-label\">{{.label}}</span>" +
			"<div class=\"e2e-callout-body\">{{if .body}}{{.body}}{{else}}absent{{end}}</div>" +
			"<span class=\"e2e-callout-raw-body\">{{.raw.body}}</span>" +
			"</div>\n",
		"templates/sections/callout/example.md": "```\nlabel: Example label\n```\nExample callout body.\n",

		// main: the slide. Its layout makes the two helper calls, passing the
		// slide's original body through as `.raw.body`. The loader's step-7
		// example execution now supplies the reserved `.raw` context, so the
		// call is unguarded (the CR's own example shape) and loads cleanly; the
		// real renderer supplies the slide's `.raw`, so the body pass-through
		// executes on the served page.
		"templates/slides/main/template.yaml": "description: a slide composing a footer and a body-passing callout through the section helper\n" +
			"fields:\n" +
			"  - name: heading\n" +
			"    type: text\n" +
			"    required: true\n" +
			"    max_length: 80\n" +
			"body:\n" +
			"  mode: optional\n",
		"templates/slides/main/layout.html.tmpl": "<section class=\"e2e-section-helper-slide\">\n" +
			"  <h1 class=\"e2e-heading\">{{.heading}}</h1>\n" +
			"  {{ section \"footer\" }}\n" +
			"  {{ section \"callout\" (dict \"label\" \"Proposition\") .raw.body }}\n" +
			"</section>\n",
		"templates/slides/main/example.md": "---\n" +
			"template: main\n" +
			"heading: Example\n" +
			"---\n",
	}
	for rel, data := range files {
		h.WriteFile(rel, []byte(data))
	}
}

// sectionHelpersSlide is one slide file using the `main` template, with a plain
// body the layout passes through to the `callout` target as `.raw.body`.
func sectionHelpersSlide(heading, body string) string {
	return "---\n" +
		"template: main\n" +
		"heading: " + heading + "\n" +
		"---\n\n" +
		body + "\n"
}

// TestSectionHelperDeckEndToEnd is the end-to-end test described at the top of
// this file. It serves a one-slide deck whose layout composes a helper-only
// `footer` target and passes the slide's original body to a `callout` target,
// then asserts the served page contains the helper-rendered footer HTML and the
// body pass-through.
func TestSectionHelperDeckEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

	h.WriteFile("kalide.yaml", []byte(
		"title: Section Helpers Demo\n"+
			"theme: plain\n",
	))
	writeSectionHelpersLibrary(t, h)

	const body = "The original body of the proposition slide."
	h.WriteFile("slides/1-proposition.md", []byte(sectionHelpersSlide("Proposition", body)))

	h.Start()

	page, err := h.GetString("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}

	for _, want := range []string{
		// The helper-only footer target renders on the slide even though it is
		// neither authored in the slide's Markdown nor declared as a child
		// section: reachableTemplates pulled it into the shared namespace.
		`<footer class="e2e-helper-footer">Rendered by the section helper</footer>`,
		// The helper call's `(dict "label" "Proposition")` field reaches the
		// callout target.
		`<span class="e2e-callout-label">Proposition</span>`,
		// The slide's original body is passed through as `.raw.body`, validated
		// against the target, and rendered by the target as `.body`. The slide
		// layout itself never renders `{{.body}}`, so this text reaches the page
		// only through the helper call.
		`<div class="e2e-callout-body"><p>The original body of the proposition slide.</p>`,
		// The target also sees the passed-through value in its reserved `.raw`
		// context, the source view of the call's body argument (the source body
		// carries a leading newline, so assert the span's closing tag rather
		// than the opening tag's immediate neighbour).
		`The original body of the proposition slide.</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("served deck page does not contain %q:\n%s", want, page)
		}
	}

	h.Stop()
}
