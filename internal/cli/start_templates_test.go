package cli

// Tests for `eypres start` and `eypres templates` (cli-start,
// cli-templates-list, cli-templates-show), against the project's own
// templates/ library — the phase-3 fixture library
// (internal/template/testdata/library/templates), copied into each test's
// temp working directory.
//
// The start tests pin the contract that matters most: a broken deck is never
// served. `start` in a directory with no templates/ at all must fail cleanly
// naming the missing directory (no-built-in-fallback); with a library but no
// eypres.yaml it must print exactly the single first validation error and
// exit non-zero, with nothing written to stdout.
//
// The valid-deck serving path is deliberately not exercised here: runStart
// blocks until a shutdown signal, and replacing openURL alone does not make it
// terminate. That end-to-end path belongs to the e2e harness.
//
// The templates tests assert the documented output shape against the fixture
// library: the list names the hello slide and item section templates with
// their usage and description, `templates <name>` prints the
// Fields/Sections/Body/Example sections, and an unknown name is rejected with
// the closest-match suggestion from internal/suggest.

import (
	"os"
	"strings"
	"testing"

	"github.com/really-knows-ai/ey-present/internal/suggest"
	"github.com/really-knows-ai/ey-present/internal/template"
)

func TestStartAndTemplates(t *testing.T) {
	t.Run("start without templates/ fails cleanly", func(t *testing.T) {
		// No templates/ directory at all (no-built-in-fallback): the
		// project library fails to load before any deck validation is
		// attempted.
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatalf("Run(start) exit = 0, want non-zero with no templates/ directory")
		}
		if stdout != "" {
			t.Fatalf("Run(start) stdout = %q, want empty for a missing templates/ directory", stdout)
		}
		if !strings.Contains(stderr, "templates") {
			t.Fatalf("Run(start) stderr = %q, want it to name the missing templates/ directory", stderr)
		}
	})

	t.Run("start invalid deck prints first error and never serves", func(t *testing.T) {
		// The fixture templates/ library loads, but the project has no
		// eypres.yaml, so whole-deck validation fails deterministically
		// before the watcher or server is created.
		chdirFixtureLibrary(t)

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

	t.Run("templates lists every project template with usage and description", func(t *testing.T) {
		chdirFixtureLibrary(t)

		code, stdout, stderr := runCLI("templates")
		if code != 0 {
			t.Fatalf("Run(templates) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "Templates:") {
			t.Fatalf("Run(templates) stdout = %q, want the list header", stdout)
		}

		reg := mustFixtureRegistry(t)
		for _, name := range reg.TemplateNames() {
			tmpl, ok := reg.Lookup(name)
			if !ok || tmpl == nil {
				t.Fatalf("registry.Lookup(%q) = _, false, want the registered template", name)
			}
			if !strings.Contains(stdout, name) {
				t.Errorf("Run(templates) stdout = %q, want template name %q", stdout, name)
			}
			if !strings.Contains(stdout, string(tmpl.Usage)) {
				t.Errorf("Run(templates) stdout = %q, want usage %q for %q", stdout, string(tmpl.Usage), name)
			}
		}
		for _, want := range []string{"hello", "item", "slide", "section"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(templates) stdout = %q, want %q", stdout, want)
			}
		}
	})

	t.Run("templates show prints the documentation sections for a slide template", func(t *testing.T) {
		chdirFixtureLibrary(t)

		code, stdout, stderr := runCLI("templates", "hello")
		if code != 0 {
			t.Fatalf("Run(templates hello) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates hello) stderr = %q, want empty", stderr)
		}
		for _, section := range []string{"Fields:", "Sections:", "Body:", "Example:"} {
			if !strings.Contains(stdout, section) {
				t.Errorf("Run(templates hello) stdout = %q, want section %q", stdout, section)
			}
		}
		if !strings.Contains(stdout, "title (type: text; required") {
			t.Errorf("Run(templates hello) stdout = %q, want the required title field", stdout)
		}
	})

	t.Run("templates show a section-usage template", func(t *testing.T) {
		chdirFixtureLibrary(t)

		code, stdout, stderr := runCLI("templates", "item")
		if code != 0 {
			t.Fatalf("Run(templates item) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stdout, "item (section)") {
			t.Errorf("Run(templates item) stdout = %q, want the name and section usage", stdout)
		}
		for _, section := range []string{"Fields:", "Sections:", "Body:", "Example:"} {
			if !strings.Contains(stdout, section) {
				t.Errorf("Run(templates item) stdout = %q, want section %q", stdout, section)
			}
		}
	})

	t.Run("templates unknown name suggests the closest match", func(t *testing.T) {
		chdirFixtureLibrary(t)
		reg := mustFixtureRegistry(t)
		names := reg.TemplateNames()

		for _, typo := range []string{"helo", "iten"} {
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

// mustFixtureRegistry loads the phase-3 fixture library from the current
// working directory's templates/ tree (chdirFixtureLibrary must have been
// called first) and returns the registry it builds.
func mustFixtureRegistry(t *testing.T) *template.Registry {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	lib, err := template.LoadLibrary(os.DirFS(dir), template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	return reg
}
