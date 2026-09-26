package cli

// Tests for `eypres start` and `eypres templates` (phase-7 task 9):
// requirements.requirement.cli-start, cli-templates-list and
// cli-templates-show.
//
// The start tests pin the contract that matters most: a broken deck is never
// served. `start` in an empty directory must print exactly the single first
// validation error — byte for byte the phase-5 validate.Format message — and
// exit non-zero, with nothing written to stdout. The expected message is
// computed by calling the same validate.Validate the handler uses, so the test
// fails if the handler ever formats or selects a different error.
//
// The valid-deck serving path is deliberately not exercised here: runStart
// blocks until a shutdown signal, and replacing openURL alone does not make it
// terminate. That end-to-end path belongs to the e2e harness.
//
// The templates tests assert the documented output shape: the list names every
// built-in with its usage and one-line description, `templates <name>` prints
// the Fields/Sections/Body/Example sections, and an unknown name is rejected
// with the closest-match suggestion from internal/suggest.

import (
	"os"
	"strings"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/suggest"
	"github.com/really-knows-ai/ey-present/internal/template"
	"github.com/really-knows-ai/ey-present/internal/validate"
)

func TestStartAndTemplates(t *testing.T) {
	t.Run("start invalid deck prints first error and never serves", func(t *testing.T) {
		// An empty directory has no eypres.yaml, so whole-deck validation
		// fails deterministically before the watcher or server is created.
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatalf("Run(start) exit = 0, want non-zero for an empty deck")
		}
		if strings.Contains(stdout, "Serving slides at") {
			t.Fatalf("Run(start) stdout = %q, want no server for an invalid deck", stdout)
		}
		if stdout != "" {
			t.Fatalf("Run(start) stdout = %q, want empty for an invalid deck", stdout)
		}

		// The expected text is the phase-5 single first error for the same
		// filesystem, formatted by validate.Format and terminated by the
		// handler's Fprintln.
		reg, err := template.Builtins()
		if err != nil {
			t.Fatalf("template.Builtins() error = %v, want nil", err)
		}
		verr, invalid := validate.Validate(os.DirFS(dir), reg, nil)
		if !invalid {
			t.Fatal("validate.Validate(empty dir) reported a valid deck, want invalid")
		}
		want := validate.Format(verr) + "\n"
		if stderr != want {
			t.Fatalf("Run(start) stderr = %q, want exactly the first formatted error %q", stderr, want)
		}
		if !strings.Contains(stderr, "eypres.yaml") {
			t.Fatalf("Run(start) stderr = %q, want the first error to name eypres.yaml", stderr)
		}
	})

	t.Run("start rejects malformed arguments with exit 2", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("start", "--port")
		if code != 2 {
			t.Fatalf("Run(start --port) exit = %d, want 2", code)
		}
		if !strings.Contains(stderr, "option --port needs a number") {
			t.Fatalf("Run(start --port) stderr = %q, want a missing-value error", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Fatalf("Run(start --port) stderr = %q, want usage text", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(start --port) stdout = %q, want empty", stdout)
		}
	})

	t.Run("templates lists every builtin with usage and description", func(t *testing.T) {
		code, stdout, stderr := runCLI("templates")
		if code != 0 {
			t.Fatalf("Run(templates) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "Built-in templates:") {
			t.Fatalf("Run(templates) stdout = %q, want the list header", stdout)
		}

		reg, err := template.Builtins()
		if err != nil {
			t.Fatalf("template.Builtins() error = %v, want nil", err)
		}
		names := reg.TemplateNames()
		for _, name := range names {
			tmpl, ok := reg.Lookup(name)
			if !ok || tmpl == nil {
				t.Fatalf("registry.Lookup(%q) = _, false, want the registered template", name)
			}
			if !strings.Contains(stdout, name) {
				t.Errorf("Run(templates) stdout = %q, want built-in name %q", stdout, name)
			}
			if !strings.Contains(stdout, string(tmpl.Usage)) {
				t.Errorf("Run(templates) stdout = %q, want usage %q for %q", stdout, string(tmpl.Usage), name)
			}
			if !strings.Contains(stdout, tmpl.Description) {
				t.Errorf("Run(templates) stdout = %q, want description for %q", stdout, name)
			}
		}
		// The minimal set is title/content/column; pin the two usages so a
		// regression that drops the usage column is caught even if the
		// description somehow contains the word.
		for _, want := range []string{"title", "content", "column", "slide", "section"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(templates) stdout = %q, want %q", stdout, want)
			}
		}
	})

	t.Run("templates show prints the documentation sections", func(t *testing.T) {
		code, stdout, stderr := runCLI("templates", "content")
		if code != 0 {
			t.Fatalf("Run(templates content) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates content) stderr = %q, want empty", stderr)
		}
		for _, section := range []string{"Fields:", "Sections:", "Body:", "Example:"} {
			if !strings.Contains(stdout, section) {
				t.Errorf("Run(templates content) stdout = %q, want section %q", stdout, section)
			}
		}
		// The content template's heading field is required text.
		if !strings.Contains(stdout, "heading (type: text; required") {
			t.Errorf("Run(templates content) stdout = %q, want the required heading field", stdout)
		}
		// Its columns section accepts `column` and repeats 2–4.
		if !strings.Contains(stdout, "columns (accepts column; repeats 2–4)") {
			t.Errorf("Run(templates content) stdout = %q, want the columns section repeat bounds", stdout)
		}
	})

	t.Run("templates show a section-usage template", func(t *testing.T) {
		code, stdout, stderr := runCLI("templates", "column")
		if code != 0 {
			t.Fatalf("Run(templates column) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stdout, "column (section)") {
			t.Errorf("Run(templates column) stdout = %q, want the name and section usage", stdout)
		}
		for _, section := range []string{"Fields:", "Sections:", "Body:", "Example:"} {
			if !strings.Contains(stdout, section) {
				t.Errorf("Run(templates column) stdout = %q, want section %q", stdout, section)
			}
		}
	})

	t.Run("templates unknown name suggests the closest match", func(t *testing.T) {
		reg, err := template.Builtins()
		if err != nil {
			t.Fatalf("template.Builtins() error = %v, want nil", err)
		}
		names := reg.TemplateNames()

		for _, typo := range []string{"titel", "colum"} {
			want := suggest.Closest(typo, names)
			if want == "" {
				t.Fatalf("suggest.Closest(%q, %v) = \"\", want a suggestion", typo, names)
			}

			code, stdout, stderr := runCLI("templates", typo)
			if code == 0 {
				t.Fatalf("Run(templates %s) exit = 0, want non-zero", typo)
			}
			if stdout != "" {
				t.Fatalf("Run(templates %s) stdout = %q, want empty", typo, stdout)
			}
			if !strings.Contains(stderr, `unknown template "`+typo+`"`) {
				t.Fatalf("Run(templates %s) stderr = %q, want the offending name", typo, stderr)
			}
			if !strings.Contains(stderr, `did you mean "`+want+`"?`) {
				t.Fatalf("Run(templates %s) stderr = %q, want suggestion %q", typo, stderr, want)
			}
			if !strings.Contains(stderr, "available templates:") {
				t.Fatalf("Run(templates %s) stderr = %q, want the available-templates list", typo, stderr)
			}
		}
	})
}
