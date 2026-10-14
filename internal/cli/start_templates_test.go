package cli

// Tests for `kalide start` and `kalide templates` (cli-start,
// cli-templates-list, cli-templates-show), against the project's own
// templates/ library — the phase-3 fixture library
// (internal/template/testdata/library/templates), copied into each test's
// temp working directory.
//
// The start tests pin the contract that matters most: a broken deck is never
// served. `start` in a directory with no templates/ at all must fail cleanly
// naming the missing directory (no-built-in-fallback); with a library but no
// kalide.yaml it must print exactly the single first validation error and
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
//
// The layered cases build a two-layer deck — the fixture library as the
// deck-root templates/ local layer plus a sibling external `templates:`
// library with disjoint names — and assert that `kalide templates` lists the
// merged set from both layers, rejects a name carried by both layers naming
// both manifest paths, and errors naming an invalid external path
// (cli-templates-list, layered-template-library, library-layer-uniqueness).
// Because runStart blocks until a signal, the start path's merged per-layer
// theme loading is covered by exercising template.LoadThemeRegistry directly:
// a theme living only in the external layer resolves in a two-layer deck
// (theme-selection).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/suggest"
	"github.com/really-knows-ai/kalide/internal/template"
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
		// kalide.yaml, so whole-deck validation fails deterministically
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
		if !strings.Contains(stderr, "kalide.yaml") {
			t.Fatalf("Run(start) stderr = %q, want the first error to name kalide.yaml", stderr)
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
		dir := chdirFixtureLibrary(t)
		writeShowFixtures(t, dir)

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
		if strings.Contains(stdout, "Child sections:") {
			t.Errorf("Run(templates hello) stdout = %q, want a slide template's section table headed %q", stdout, "Sections:")
		}

		// A slide template's declared sections use the "declares" verb: the
		// `content` fixture declares a `blocks` section accepting the item
		// section, 1–3 times.
		code, stdout, stderr = runCLI("templates", "content")
		if code != 0 {
			t.Fatalf("Run(templates content) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates content) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "content (slide)") {
			t.Errorf("Run(templates content) stdout = %q, want the name and slide usage", stdout)
		}
		if !strings.Contains(stdout, "Sections:") {
			t.Errorf("Run(templates content) stdout = %q, want a slide template's %q heading", stdout, "Sections:")
		}
		if !strings.Contains(stdout, "blocks (declares item; repeats 1–3)") {
			t.Errorf("Run(templates content) stdout = %q, want the declared section row %q", stdout, "blocks (declares item; repeats 1–3)")
		}
		if strings.Contains(stdout, "Can hold") {
			t.Errorf("Run(templates content) stdout = %q, want a slide template's section rows to use %q", stdout, "declares")
		}
	})

	t.Run("templates show a section-usage template", func(t *testing.T) {
		dir := chdirFixtureLibrary(t)
		writeShowFixtures(t, dir)

		code, stdout, stderr := runCLI("templates", "item")
		if code != 0 {
			t.Fatalf("Run(templates item) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stdout, "item (section)") {
			t.Errorf("Run(templates item) stdout = %q, want the name and section usage", stdout)
		}
		for _, section := range []string{"Fields:", "Child sections:", "Body:", "Example:"} {
			if !strings.Contains(stdout, section) {
				t.Errorf("Run(templates item) stdout = %q, want section %q", stdout, section)
			}
		}
		if !strings.Contains(stdout, "Child sections:\n  none") {
			t.Errorf("Run(templates item) stdout = %q, want the empty child-section heading", stdout)
		}
		if strings.Contains(stdout, "Sections:") {
			t.Errorf("Run(templates item) stdout = %q, want a section template's child sections headed %q", stdout, "Child sections:")
		}

		// A container section template lists the children it can hold, each
		// labelled "Can hold" with the accepted template and repeat bounds:
		// the `group` fixture can hold `items` children accepting the item
		// section, 1–2 times.
		code, stdout, stderr = runCLI("templates", "group")
		if code != 0 {
			t.Fatalf("Run(templates group) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates group) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "group (section)") {
			t.Errorf("Run(templates group) stdout = %q, want the name and section usage", stdout)
		}
		if !strings.Contains(stdout, "Child sections:") {
			t.Errorf("Run(templates group) stdout = %q, want a section template's %q heading", stdout, "Child sections:")
		}
		if !strings.Contains(stdout, "items (Can hold item; repeats 1–2)") {
			t.Errorf("Run(templates group) stdout = %q, want the child-section row %q", stdout, "items (Can hold item; repeats 1–2)")
		}
		if strings.Contains(stdout, "declares") {
			t.Errorf("Run(templates group) stdout = %q, want a section template's child rows to use %q", stdout, "Can hold")
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

	t.Run("templates lists every template merged from both layers", func(t *testing.T) {
		// A two-layer deck: the fixture library is the deck-root local
		// templates/ layer, and a sibling external library is named by the
		// top-level `templates:` key. The layers carry disjoint names, so
		// `kalide templates` lists the local hello slide and item section
		// alongside the external ext-slide and ext-section — the merged,
		// layered library every layer contributes to
		// (cli-templates-list, layered-template-library).
		dir := chdirFixtureLibrary(t)
		writeTestLibrary(t, filepath.Join(dir, "extlib"), "external-library",
			[]string{"ext-slide"}, []string{"ext-section"}, "")
		writeCLIFile(t, filepath.Join(dir, "kalide.yaml"), []byte("templates: extlib\n"))

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

		// Every name from each layer is listed exactly once, with the usage
		// derived from the template's own directory (slide vs section).
		for _, want := range []struct{ name, usage string }{
			{"hello", "slide"},
			{"item", "section"},
			{"ext-slide", "slide"},
			{"ext-section", "section"},
		} {
			row := templateListRow(stdout, want.name)
			if row == "" {
				t.Errorf("Run(templates) stdout = %q, want a row for %q", stdout, want.name)
				continue
			}
			if !strings.Contains(row, want.usage) {
				t.Errorf("Run(templates) row = %q, want usage %q for %q", row, want.usage, want.name)
			}
		}
	})

	t.Run("templates rejects a name carried by both layers naming both paths", func(t *testing.T) {
		// The external layer carries a slide named hello, which the local
		// fixture also carries. The layers have equal standing, so the merge
		// is refused rather than silently resolved: the command fails and the
		// error names the duplicated name and the manifest path that carries
		// it in BOTH layers (library-layer-uniqueness, cli-templates-list).
		dir := chdirFixtureLibrary(t)
		extDir := filepath.Join(dir, "extlib")
		writeTestLibrary(t, extDir, "external-library", []string{"hello"}, nil, "")
		writeCLIFile(t, filepath.Join(dir, "kalide.yaml"), []byte("templates: extlib\n"))

		code, stdout, stderr := runCLI("templates")
		if code == 0 {
			t.Fatalf("Run(templates) exit = 0, want non-zero for a duplicate name across layers")
		}
		if stdout != "" {
			t.Fatalf("Run(templates) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, `template "hello" is declared in both`) {
			t.Fatalf("Run(templates) stderr = %q, want the path-qualified duplicate error", stderr)
		}
		extManifest := filepath.Join(extDir, "slides", "hello", "template.yaml")
		localManifest := filepath.Join(dir, "templates", "slides", "hello", "template.yaml")
		if !strings.Contains(stderr, extManifest) {
			t.Errorf("Run(templates) stderr = %q, want the external path %q", stderr, extManifest)
		}
		if !strings.Contains(stderr, localManifest) {
			t.Errorf("Run(templates) stderr = %q, want the local path %q", stderr, localManifest)
		}
	})

	t.Run("templates errors on an invalid external path naming it", func(t *testing.T) {
		// The deck configures an external `templates:` path that does not
		// exist. The layered resolution reports it naming the offending path
		// (with the `templates:` hint), never falling back to the local
		// templates/ (templates-dir-required, cli-templates-list).
		dir := chdirFixtureLibrary(t)
		writeCLIFile(t, filepath.Join(dir, "kalide.yaml"), []byte("templates: missing-lib\n"))

		code, stdout, stderr := runCLI("templates")
		if code == 0 {
			t.Fatalf("Run(templates) exit = 0, want non-zero for an invalid external path")
		}
		if stdout != "" {
			t.Fatalf("Run(templates) stdout = %q, want empty", stdout)
		}
		missing := filepath.Join(dir, "missing-lib")
		if !strings.Contains(stderr, missing) {
			t.Fatalf("Run(templates) stderr = %q, want it to name the invalid external path %q", stderr, missing)
		}
		if !strings.Contains(stderr, "directory not found") {
			t.Errorf("Run(templates) stderr = %q, want the missing-directory reason", stderr)
		}
		if !strings.Contains(stderr, "path in kalide.yaml") {
			t.Errorf("Run(templates) stderr = %q, want the `templates:` hint", stderr)
		}
	})

	t.Run("an external-layer theme resolves in a two-layer deck", func(t *testing.T) {
		// A theme living only in the external `templates:` layer is
		// resolvable in a two-layer deck: template.LoadThemeRegistry loads each
		// layer's own themes/ from that layer's root and merges them, so the
		// external theme is in the registry while the local layer — carrying
		// no themes/ directory at all — is skipped rather than erroring
		// (layered-template-library, theme-selection). runStart blocks until a
		// signal, so the theme registry builder is exercised directly.
		dir := t.TempDir()
		writeTestLibrary(t, filepath.Join(dir, "templates"), "local-library",
			[]string{"local-slide"}, nil, "")
		writeTestLibrary(t, filepath.Join(dir, "extlib"), "external-library",
			[]string{"ext-slide"}, nil, "ext-theme")
		writeCLIFile(t, filepath.Join(dir, "kalide.yaml"), []byte("templates: extlib\n"))

		lib, err := template.LoadDeckLibrary(dir, "extlib")
		if err != nil {
			t.Fatalf("template.LoadDeckLibrary: %v", err)
		}
		if len(lib.Layers) != 2 {
			t.Fatalf("len(Layers) = %d, want 2 (external + local)", len(lib.Layers))
		}

		themeReg, err := template.LoadThemeRegistry(lib)
		if err != nil {
			t.Fatalf("template.LoadThemeRegistry: %v (a layer without themes/ must not error)", err)
		}
		if _, err := themeReg.Lookup("ext-theme"); err != nil {
			t.Errorf("Lookup(ext-theme) = error %v, want the theme contributed only by the external layer", err)
		}
	})

	t.Run("a single-root library with no themes/ keeps theme.LoadDir's missing-themes error", func(t *testing.T) {
		// A single-layer library keeps theme.LoadDir's exact behaviour: with
		// no themes/ directory the registry load fails naming the missing
		// directory rather than inventing an empty registry, so the shared
		// loader preserves the single-root contract while it merges layers
		// (layered-template-library, theme-selection).
		dir := t.TempDir()
		writeTestLibrary(t, filepath.Join(dir, template.TemplatesDir), "solo-library",
			[]string{"solo-slide"}, nil, "")

		lib, err := template.LoadDeckLibrary(dir, "")
		if err != nil {
			t.Fatalf("template.LoadDeckLibrary: %v", err)
		}
		if len(lib.Layers) != 1 {
			t.Fatalf("len(Layers) = %d, want 1 (single local root)", len(lib.Layers))
		}

		_, err = template.LoadThemeRegistry(lib)
		if err == nil {
			t.Fatal("template.LoadThemeRegistry succeeded over a single-root library with no themes/, want theme.LoadDir's missing-themes error")
		}
		if !strings.Contains(err.Error(), template.ThemesDir) {
			t.Errorf("template.LoadThemeRegistry error = %q, want it to name the missing %q directory", err, template.ThemesDir)
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("template.LoadThemeRegistry error = %q, want it to say the directory was not found", err)
		}
	})

	t.Run("templates hides an internal template by default and lists it with --all", func(t *testing.T) {
		// An `internal: true` template is resolvable and usable, but the
		// default listing omits it; the `--all` listing mode includes it with
		// its name, usage and description (internal-templates,
		// cli-templates-list).
		dir := chdirFixtureLibrary(t)
		writeInternalFixture(t, dir)

		code, stdout, stderr := runCLI("templates")
		if code != 0 {
			t.Fatalf("Run(templates) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates) stderr = %q, want empty", stderr)
		}
		if strings.Contains(stdout, "hidden") {
			t.Errorf("Run(templates) stdout = %q, want the internal template hidden by default", stdout)
		}

		code, stdout, stderr = runCLI("templates", "--all")
		if code != 0 {
			t.Fatalf("Run(templates --all) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates --all) stderr = %q, want empty", stderr)
		}
		row := templateListRow(stdout, "hidden")
		if row == "" {
			t.Fatalf("Run(templates --all) stdout = %q, want the internal template listed", stdout)
		}
		if !strings.Contains(row, "slide") {
			t.Errorf("Run(templates --all) row = %q, want the internal template's usage", row)
		}
	})

	t.Run("templates show prints a record field's nested fields and an enum's variants", func(t *testing.T) {
		// A list field whose item is an inline record prints that record's
		// nested fields indented beneath it — recursively, including a
		// nullable field — and an enum field prints its effective variants
		// beneath its own line (record-item-type).
		dir := chdirFixtureLibrary(t)
		writeRecordFixture(t, dir)

		code, stdout, stderr := runCLI("templates", "recorded")
		if code != 0 {
			t.Fatalf("Run(templates recorded) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(templates recorded) stderr = %q, want empty", stderr)
		}
		for _, want := range []string{
			"widgets (type: list; optional)",
			"    label (type: text; required)",
			"    note (type: text; optional; nullable)",
			"    parts (type: list; optional)",
			"      depth (type: number; optional)",
			"    one of fast, slow",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(templates recorded) stdout = %q, want %q", stdout, want)
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

// writeShowFixtures adds two templates to the copied fixture library at dir
// that exercise the usage-dependent child-section documentation:
//
//   - `content`, a slide template declaring a `blocks` section that accepts
//     the item section, 1–3 times (the slide side prints Sections:/declares);
//   - `group`, a container section template that can hold `items` children
//     accepting the item section, 1–2 times (the section side prints
//     Child sections:/Can hold).
//
// Their examples use the same heading-depth nesting the loader validates
// recursively, so adding them does not break `kalide templates <name>`'s
// library load.
func writeShowFixtures(t *testing.T, dir string) {
	t.Helper()
	const contentManifest = `description: a content slide declaring a blocks section
fields:
  - name: heading
    type: text
    required: true
sections:
  - name: blocks
    accepted: [item]
    min: 1
    max: 3
body:
  mode: optional
`
	const contentExample = "---\ntemplate: content\nheading: Content\n---\n\n# blocks\n```\ntitle: First item\n```\n"
	const groupManifest = `description: a container section holding item children
fields:
  - name: title
    type: text
sections:
  - name: items
    accepted: [item]
    min: 1
    max: 2
body:
  mode: optional
`
	const groupExample = "```\ntemplate: group\ntitle: Container\n```\n\n# items\n```\ntitle: Nested item\n```\n"

	for _, f := range []struct{ path, data string }{
		{"templates/slides/content/template.yaml", contentManifest},
		{"templates/slides/content/layout.html.tmpl", "<section>{{.heading}}{{range .blocks}}{{.}}{{end}}</section>"},
		{"templates/slides/content/example.md", contentExample},
		{"templates/sections/group/template.yaml", groupManifest},
		{"templates/sections/group/layout.html.tmpl", "<div>{{with .title}}{{.}}{{end}}{{range .items}}{{.}}{{end}}</div>"},
		{"templates/sections/group/example.md", groupExample},
	} {
		p := filepath.Join(dir, f.path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(f.data), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

// templateListRow returns the line of `kalide templates` list output that
// names the template, or "" when no line does. It lets a test assert the
// usage shown for a specific template without a substring match against the
// same word elsewhere in the output.
func templateListRow(stdout, name string) string {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	return ""
}

// writeTestLibrary writes a minimal valid template library directly at dir,
// for use as an external `templates:` layer in these CLI tests: library.yaml
// with the given name, one media-free slide per slideNames and one section per
// sectionNames (writeTestTemplate), and, when theme is non-empty, one theme
// directory with a minimal theme.css. The templates carry no fields or
// sections, so their placeholder example needs no matching authoring — the
// same shape the template package's integration tests build.
func writeTestLibrary(t *testing.T, dir, name string, slideNames, sectionNames []string, theme string) {
	t.Helper()
	writeCLIFile(t, filepath.Join(dir, "library.yaml"), []byte("name: "+name+"\nformat: 1\n"))
	for _, n := range slideNames {
		writeTestTemplate(t, dir, "slides", n)
	}
	for _, n := range sectionNames {
		writeTestTemplate(t, dir, "sections", n)
	}
	if theme != "" {
		writeCLIFile(t, filepath.Join(dir, "themes", theme, "theme.css"), []byte("body { color: black; }\n"))
	}
}

// writeTestTemplate writes one template directory dir/kindDir/name with a
// schema-free manifest, a plain layout and a placeholder example.
func writeTestTemplate(t *testing.T, dir, kindDir, name string) {
	t.Helper()
	base := filepath.Join(dir, kindDir, name)
	writeCLIFile(t, filepath.Join(base, "template.yaml"), []byte("name: "+name+"\n"))
	writeCLIFile(t, filepath.Join(base, "layout.html.tmpl"), []byte("<section><h1>{{.Title}}</h1></section>"))
	writeCLIFile(t, filepath.Join(base, "example.md"), []byte("# "+name+"\n"))
}

// writeCLIFile writes data to path, creating the parent directory tree first.
func writeCLIFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeInternalFixture adds an `internal: true` slide template to the copied
// fixture library at dir, so a lister can prove it is hidden from the default
// listing and shown with `--all` (internal-templates).
func writeInternalFixture(t *testing.T, dir string) {
	t.Helper()
	const manifest = "description: an internal helper slide\ninternal: true\nbody:\n  mode: optional\n"
	const example = "---\ntemplate: hidden\n---\n"
	for _, f := range []struct{ path, data string }{
		{"templates/slides/hidden/template.yaml", manifest},
		{"templates/slides/hidden/layout.html.tmpl", "<section>internal</section>"},
		{"templates/slides/hidden/example.md", example},
	} {
		writeCLIFile(t, filepath.Join(dir, f.path), []byte(f.data))
	}
}

// writeRecordFixture adds a slide template to the copied fixture library at dir
// whose list field's item is an inline record — carrying a required field, a
// nullable field and a nested list-of-record — and which declares an enum
// field, so `templates <name>` can print the record's nested fields recursively
// and the enum's effective variants (record-item-type).
func writeRecordFixture(t *testing.T, dir string) {
	t.Helper()
	const manifest = `description: a template with a record list and an enum
fields:
  - name: widgets
    type: list
    item:
      type: record
      fields:
        - name: label
          type: text
          required: true
        - name: note
          type: text
          nullable: true
        - name: parts
          type: list
          item:
            type: record
            fields:
              - name: depth
                type: number
  - name: mode
    type: enum
    variants: [fast, slow]
body:
  mode: optional
`
	const example = "---\ntemplate: recorded\nwidgets:\n  - label: one\n    parts:\n      - depth: 1\nmode: fast\n---\n"
	for _, f := range []struct{ path, data string }{
		{"templates/slides/recorded/template.yaml", manifest},
		{"templates/slides/recorded/layout.html.tmpl", "<section><h1>{{.mode}}</h1></section>"},
		{"templates/slides/recorded/example.md", example},
	} {
		writeCLIFile(t, filepath.Join(dir, f.path), []byte(f.data))
	}
}
