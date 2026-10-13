package cli

// Tests for `kalide render [slide] [-o dir] [--full|--fragment]`
// (cli-render): the non-serving slide renderer. runRender loads the deck's
// resolved library and theme, validates the whole deck fail-fast, and then
// writes the selected slide's HTML to stdout — or one file per slide into `-o`
// — without opening a server, binding a port or reaching the network.
//
// The cases pin the selector (position label first, then the deck-unique label,
// with position-label precedence; an unknown selector's closest-match error),
// the two usage errors (no slide and no `-o`; `--full` with `--fragment`), the
// one-file-per-slide form's `<number>[letter]-<label>.html` names in deck
// order, the fragment-versus-full output (the fragment is the `<section>` markup
// the full page embeds), and the byte-identical output across runs with the
// slide's label as its anchor id.
//
// The deck fixtures copy the phase-3 fixture library into a fresh temp
// directory (chdirFixtureDeck) and use its `hello` slide template, matching the
// neighbours in cli_test.go/start_templates_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderDeck is a two-slide deck against the fixture library's hello template,
// with deck-unique labels alpha and beta and position labels 1 and 2.
func renderDeck() map[string]string {
	return map[string]string{
		"kalide.yaml":       "title: Render Deck\ntheme: plain\n",
		"slides/1-alpha.md": "---\ntemplate: hello\ntitle: Alpha\n---\n",
		"slides/2-beta.md":  "---\ntemplate: hello\ntitle: Beta\n---\n",
	}
}

// precedenceRenderDeck is a deck whose second slide's deck-unique label ("1")
// collides with the first slide's position label, so a selector matching both
// pins which lookup wins.
func precedenceRenderDeck() map[string]string {
	return map[string]string{
		"kalide.yaml":       "title: Render Deck\ntheme: plain\n",
		"slides/1-alpha.md": "---\ntemplate: hello\ntitle: Alpha\n---\n",
		"slides/2-1.md":     "---\ntemplate: hello\ntitle: Beta\n---\n",
	}
}

func TestRunRender(t *testing.T) {
	t.Run("renders the selected slide by position label to stdout", func(t *testing.T) {
		chdirFixtureDeck(t, renderDeck())

		code, stdout, stderr := runCLI("render", "1")
		if code != 0 {
			t.Fatalf("Run(render 1) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("Run(render 1) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, `id="alpha"`) {
			t.Errorf("Run(render 1) stdout = %q, want the alpha slide's anchor", stdout)
		}

		code, stdout, stderr = runCLI("render", "2")
		if code != 0 {
			t.Fatalf("Run(render 2) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stdout, `id="beta"`) {
			t.Errorf("Run(render 2) stdout = %q, want the beta slide's anchor", stdout)
		}
	})

	t.Run("selects by deck-unique label identically to the position label", func(t *testing.T) {
		chdirFixtureDeck(t, renderDeck())

		code, byPos, stderr := runCLI("render", "1")
		if code != 0 {
			t.Fatalf("Run(render 1) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		code, byLabel, stderr := runCLI("render", "alpha")
		if code != 0 {
			t.Fatalf("Run(render alpha) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if byPos != byLabel {
			t.Errorf("render 1 and render alpha differ:\n  position: %q\n  label:    %q", byPos, byLabel)
		}
	})

	t.Run("position label takes precedence over a matching deck label", func(t *testing.T) {
		chdirFixtureDeck(t, precedenceRenderDeck())

		// "1" is slide 1's position label and slide 2's deck label; the
		// position label wins, so the alpha slide renders.
		code, stdout, stderr := runCLI("render", "1", "--fragment")
		if code != 0 {
			t.Fatalf("Run(render 1 --fragment) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stdout, `id="alpha"`) {
			t.Errorf("Run(render 1 --fragment) stdout = %q, want the position-label slide alpha", stdout)
		}
		if strings.Contains(stdout, `id="1"`) {
			t.Errorf("Run(render 1 --fragment) stdout = %q, want the label match not to win", stdout)
		}

		// The second slide's own position label still selects it, proving its
		// label "1" is a real, distinct anchor.
		code, stdout, stderr = runCLI("render", "2", "--fragment")
		if code != 0 {
			t.Fatalf("Run(render 2 --fragment) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stdout, `id="1"`) {
			t.Errorf("Run(render 2 --fragment) stdout = %q, want the second slide's label anchor", stdout)
		}
	})

	t.Run("unknown slide exits 1 with a closest-match error", func(t *testing.T) {
		chdirFixtureDeck(t, renderDeck())

		code, stdout, stderr := runCLI("render", "alpa")
		if code != 1 {
			t.Fatalf("Run(render alpa) exit = %d, want 1", code)
		}
		if stdout != "" {
			t.Errorf("Run(render alpa) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, `unknown slide "alpa"`) {
			t.Errorf("Run(render alpa) stderr = %q, want the offending selector", stderr)
		}
		if !strings.Contains(stderr, `did you mean "alpha"?`) {
			t.Errorf("Run(render alpa) stderr = %q, want the closest-match suggestion", stderr)
		}
		if !strings.Contains(stderr, "available slides:") {
			t.Errorf("Run(render alpa) stderr = %q, want the available-slides list", stderr)
		}
	})

	t.Run("no slide and no -o is a usage error", func(t *testing.T) {
		chdirFixtureDeck(t, renderDeck())

		code, stdout, stderr := runCLI("render")
		if code != 2 {
			t.Fatalf("Run(render) exit = %d, want 2", code)
		}
		if stdout != "" {
			t.Errorf("Run(render) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "no slide") {
			t.Errorf("Run(render) stderr = %q, want the missing-slide-or-directory error", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(render) stderr = %q, want usage text", stderr)
		}
	})

	t.Run("--full with --fragment is a usage error", func(t *testing.T) {
		chdirFixtureDeck(t, renderDeck())

		code, stdout, stderr := runCLI("render", "alpha", "--full", "--fragment")
		if code != 2 {
			t.Fatalf("Run(render alpha --full --fragment) exit = %d, want 2", code)
		}
		if stdout != "" {
			t.Errorf("Run(render alpha --full --fragment) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "mutually exclusive") {
			t.Errorf("Run(render alpha --full --fragment) stderr = %q, want the mutual-exclusion error", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(render alpha --full --fragment) stderr = %q, want usage text", stderr)
		}
	})

	t.Run("writes one file per slide into -o in deck order", func(t *testing.T) {
		dir := chdirFixtureDeck(t, renderDeck())
		out := filepath.Join(dir, "out")

		code, stdout, stderr := runCLI("render", "-o", out)
		if code != 0 {
			t.Fatalf("Run(render -o) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stdout != "" {
			t.Errorf("Run(render -o) stdout = %q, want empty (output goes to files)", stdout)
		}
		if stderr != "" {
			t.Errorf("Run(render -o) stderr = %q, want empty", stderr)
		}

		// Each file is named for its slide's filename stem,
		// <number>[letter]-<label>.html, and holds the full page that the
		// with-slide form prints to stdout.
		for _, tc := range []struct{ name, sel string }{
			{"1-alpha.html", "1"},
			{"2-beta.html", "2"},
		} {
			data, err := os.ReadFile(filepath.Join(out, tc.name))
			if err != nil {
				t.Fatalf("read %s: %v", tc.name, err)
			}
			_, want, _ := runCLI("render", tc.sel)
			if string(data) != want {
				t.Errorf("file %s differs from `render %s` stdout", tc.name, tc.sel)
			}
		}
	})

	t.Run("fragment is the section markup and full is the complete page", func(t *testing.T) {
		chdirFixtureDeck(t, renderDeck())

		code, fragment, stderr := runCLI("render", "alpha", "--fragment")
		if code != 0 {
			t.Fatalf("Run(render alpha --fragment) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.HasPrefix(fragment, `<section id="alpha"`) {
			t.Errorf("fragment = %q, want the alpha <section> markup with its label anchor", fragment)
		}
		if strings.Contains(fragment, "<!DOCTYPE") {
			t.Errorf("fragment = %q, want no whole-page shell", fragment)
		}

		code, full, stderr := runCLI("render", "alpha")
		if code != 0 {
			t.Fatalf("Run(render alpha) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(full, "<!DOCTYPE") {
			t.Errorf("full = %q, want the complete page shell", full)
		}
		if !strings.Contains(full, fragment) {
			t.Errorf("full page does not embed its own fragment:\n  full:     %q\n  fragment: %q", full, fragment)
		}
	})

	t.Run("output is byte-identical across two runs", func(t *testing.T) {
		dir := chdirFixtureDeck(t, renderDeck())

		_, first, _ := runCLI("render", "1")
		_, second, _ := runCLI("render", "1")
		if first != second {
			t.Errorf("two `render 1` runs differ:\n  first:  %q\n  second: %q", first, second)
		}

		out1 := filepath.Join(dir, "out1")
		out2 := filepath.Join(dir, "out2")
		if code, _, stderr := runCLI("render", "-o", out1); code != 0 {
			t.Fatalf("Run(render -o out1) exit = %d (stderr = %q)", code, stderr)
		}
		if code, _, stderr := runCLI("render", "-o", out2); code != 0 {
			t.Fatalf("Run(render -o out2) exit = %d (stderr = %q)", code, stderr)
		}
		for _, name := range []string{"1-alpha.html", "2-beta.html"} {
			a, err := os.ReadFile(filepath.Join(out1, name))
			if err != nil {
				t.Fatalf("read %s from out1: %v", name, err)
			}
			b, err := os.ReadFile(filepath.Join(out2, name))
			if err != nil {
				t.Fatalf("read %s from out2: %v", name, err)
			}
			if string(a) != string(b) {
				t.Errorf("%s differs between two runs", name)
			}
		}
	})
}
