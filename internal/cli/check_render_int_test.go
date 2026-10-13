package cli

// This file is the integration-test deliverable for
// typed-records/plan.phase-10.task-12 (cli-check, cli-render,
// error-reporting): TestCheckRenderInt drives `kalide check` and
// `kalide render` in-process (cli.Run) over a REAL on-disk fixture library
// and deck — the fixture library copied to a temp directory as templates/,
// plus an external `templates:` layer — rather than the in-memory or
// single-layer fixtures the unit-tier TestRunCheck/TestRunRender use.
//
// The check half builds a TWO-LAYER deck — the fixture library as the
// deck-root templates/ local layer and an external notes library named by the
// top-level `templates:` key — and pins that `kalide check` loads the merged
// layers (the faulting slide selects the external layer's template, so an
// unmerged registry would report an unknown template instead), validates the
// deck, and reports the fault in the single error-reporting format
// (file/line/path/what/fix) — the exact string validate.Format renders and the
// exact string `kalide start` prints before it can serve a broken deck.
//
// The render half pins that `kalide render` writes a slide's full page and its
// fragment with byte-identical output across repeated runs from the same
// on-disk fixture deck, and that the full page embeds its own fragment.
//
// These tests exercise real disk I/O, so the whole test is guarded with
// testing.Short() and skipped under `go test -short`.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// externalNotesLibrary is the deck-relative directory of the external
// `templates:` layer these tests merge with the local fixtures/ templates/.
const externalNotesLibrary = "extlib"

// mergedFaultSlidePath is the deck-relative slide whose fault the check half
// reports; its body is present while its template (the external layer's
// `notes`) declares a disallowed body rule.
const mergedFaultSlidePath = "slides/1-notes.md"

// TestCheckRenderInt is the phase-10 integration test for the non-serving
// `kalide check` and `kalide render` commands: both load and validate a real
// on-disk deck and library in-process, check reporting a fault in the shared
// error-reporting format and render writing byte-identical HTML.
func TestCheckRenderInt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real on-disk fixture library and deck")
	}

	t.Run("check loads the merged library layers and reports a fault in the error-reporting format", func(t *testing.T) {
		dir := chdirFixtureDeck(t, mergedFaultCheckDeck())
		writeExternalNotesLibrary(t, filepath.Join(dir, externalNotesLibrary))

		// The same pipeline runCheck builds: resolve the deck's layers, build
		// every layer's theme registry and the merged template registry, then
		// validate the whole deck. The fault is recomputed here so the test
		// asserts against validate.Format directly rather than a copied string.
		library, err := template.LoadDeckLibrary(dir, externalNotesLibrary)
		if err != nil {
			t.Fatalf("template.LoadDeckLibrary: %v", err)
		}
		if len(library.Layers) != 2 {
			t.Fatalf("len(library.Layers) = %d, want 2 (external + local)", len(library.Layers))
		}
		themeReg, err := loadLibraryThemeRegistry(library)
		if err != nil {
			t.Fatalf("loadLibraryThemeRegistry: %v", err)
		}
		registry, err := template.NewRegistryFromLibrary(library)
		if err != nil {
			t.Fatalf("template.NewRegistryFromLibrary: %v", err)
		}
		verr, invalid := validate.Validate(os.DirFS(dir), registry, themeReg)
		if !invalid {
			t.Fatalf("validate.Validate = valid, want the notes slide's disallowed body to be a fault")
		}
		want := validate.Format(verr)

		code, stdout, stderr := runCLI("check")
		if code != 1 {
			t.Fatalf("Run(check) exit = %d, want 1 (stdout = %q, stderr = %q)", code, stdout, stderr)
		}
		if wantSummary := "kalide check: 1 error\n"; stdout != wantSummary {
			t.Errorf("Run(check) stdout = %q, want %q", stdout, wantSummary)
		}
		if got := strings.TrimRight(stderr, "\n"); got != want {
			t.Errorf("Run(check) stderr = %q, want validate.Format = %q", got, want)
		}

		// The same text `kalide start` prints before it can serve anything: the
		// broken deck never reaches the watcher or the server.
		code, _, startErr := runCLI("start")
		if code == 0 {
			t.Fatalf("Run(start) exit = 0, want non-zero for a broken deck")
		}
		if got := strings.TrimRight(startErr, "\n"); got != want {
			t.Errorf("Run(start) stderr = %q, want the same text as check/validate.Format = %q", got, want)
		}

		// The fault's error-reporting shape: file, line, path, what and fix.
		if verr.File != mergedFaultSlidePath {
			t.Errorf("error.File = %q, want %q", verr.File, mergedFaultSlidePath)
		}
		if verr.Line <= 0 {
			t.Errorf("error.Line = %d, want the body's 1-based source line", verr.Line)
		}
		if !reflect.DeepEqual(verr.Path, []string{"body"}) {
			t.Errorf("error.Path = %v, want [body]", verr.Path)
		}
		if verr.What == "" {
			t.Error("error.What = \"\", want the what-is-wrong text")
		}
		if verr.Fix == "" {
			t.Error("error.Fix = \"\", want the how-to-fix text")
		}
		if !strings.Contains(want, mergedFaultSlidePath+":") ||
			!strings.Contains(want, " › body: ") ||
			!strings.Contains(want, " — ") {
			t.Errorf("validate.Format = %q, want `file:line › body: what — fix`", want)
		}
	})

	t.Run("render writes a slide's full page and its fragment byte-identically across runs", func(t *testing.T) {
		chdirFixtureDeck(t, renderDeck())

		code, full1, stderr := runCLI("render", "alpha")
		if code != 0 {
			t.Fatalf("Run(render alpha) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		_, full2, _ := runCLI("render", "alpha")
		if full1 != full2 {
			t.Errorf("two `render alpha` runs differ:\n  first:  %q\n  second: %q", full1, full2)
		}
		if !strings.Contains(full1, "<!DOCTYPE") {
			t.Errorf("full page = %q, want the complete page shell", full1)
		}

		code, fragment1, stderr := runCLI("render", "alpha", "--fragment")
		if code != 0 {
			t.Fatalf("Run(render alpha --fragment) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		_, fragment2, _ := runCLI("render", "alpha", "--fragment")
		if fragment1 != fragment2 {
			t.Errorf("two `render alpha --fragment` runs differ:\n  first:  %q\n  second: %q", fragment1, fragment2)
		}
		if !strings.HasPrefix(fragment1, `<section id="alpha"`) {
			t.Errorf("fragment = %q, want the slide's <section> markup with its label anchor", fragment1)
		}
		if strings.Contains(fragment1, "<!DOCTYPE") {
			t.Errorf("fragment = %q, want no whole-page shell", fragment1)
		}
		if !strings.Contains(full1, fragment1) {
			t.Errorf("full page does not embed its own fragment:\n  full:     %q\n  fragment: %q", full1, fragment1)
		}
	})
}

// mergedFaultCheckDeck is the two-layer deck the check half validates: the
// fixture library copied to templates/ is the local layer, and the external
// library writeExternalNotesLibrary writes (named by the top-level `templates:`
// key) is the other. slides/1-notes.md selects the external layer's `notes`
// template and carries a body its disallowed body rule rejects, so the reported
// fault proves the external layer was merged in — an unmerged registry would
// have reported the unknown `notes` template instead — and carries a real
// source line.
func mergedFaultCheckDeck() map[string]string {
	return map[string]string{
		"kalide.yaml":       "title: Merged Deck\ntheme: plain\ntemplates: " + externalNotesLibrary + "\n",
		"slides/1-notes.md": "---\ntemplate: notes\ntitle: Notes\n---\n\nThis body is not allowed here.\n",
	}
}

// writeExternalNotesLibrary writes a minimal valid external library at dir: a
// `notes` slide template with a required title field and a disallowed body
// rule, plus the placeholder example that satisfies both. Its manifest carries
// the body rule that makes the deck's body-bearing slide a fault.
func writeExternalNotesLibrary(t *testing.T, dir string) {
	t.Helper()
	const manifest = `description: a notes slide from an external layer
fields:
  - name: title
    type: text
    required: true
body:
  mode: disallowed
`
	const example = "---\ntemplate: notes\ntitle: Example\n---\n"
	writeCLIFile(t, filepath.Join(dir, "library.yaml"), []byte("name: external-notes\nformat: 1\n"))
	writeCLIFile(t, filepath.Join(dir, "slides", "notes", "template.yaml"), []byte(manifest))
	writeCLIFile(t, filepath.Join(dir, "slides", "notes", "layout.html.tmpl"), []byte("<section><h1>{{.title}}</h1></section>"))
	writeCLIFile(t, filepath.Join(dir, "slides", "notes", "example.md"), []byte(example))
}
