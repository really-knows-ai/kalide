package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the integration-test deliverable for plan.phase-04.task-13
// (gfm-tables, table-opt-in, table-errors). It drives the REAL template loader
// (LoadLibrary over os.DirFS at a t.TempDir()) so the helper/child literal-body
// table gate is proved through the whole pipeline, not the unit site alone:
//
//   - a section-helper call's literal body table is refused at load when the
//     target template has not opted in through `body.tables` — the refusal is
//     CheckBody's table pass (mdcheck's opt-out refusal), positioned at the
//     calling layout, with guidance naming the `tables` option;
//   - a child constructor's literal body table is refused the same way,
//     against the instantiated child template's body rule;
//   - each target loads cleanly once it opts in (a sound literal table body is
//     accepted by the table pass); and
//   - the table error is emitted exactly once: the refusal string appears once
//     in the load error, never duplicated — CheckBody's table pass is the
//     single source for a section-helper or child-constructor literal body,
//     and no other load step (the static checkSections, which inspects a
//     helper call's fields, children and body HEADINGS but not its body rule)
//     re-emits it.
//
// The literal table reaches CheckBody because ResolveSectionCallLayered checks
// the helper target's body (section.go:1224) and ResolveChildCall checks the
// instantiated child template's body (child.go:414), both with no
// WithoutTableCheck option — exactly the two callers WithoutTableCheck's doc
// names as the ones that rely on CheckBody's table pass. Both are reached at
// templates-dir-validation step 7 (checkLibraryExamples) when the host slide's
// layout executes its `{{ section … }}` call; CheckBody's table pass is the
// sole source, so the error surfaces once.
//
// The tests touch the real filesystem, so they are guarded with testing.Short()
// and skipped under `go test -short`. They reuse the loader fixtures
// writeSectionHelperIntLibrary, templateIntFiles, slideIntFiles,
// sectionIntFiles, mergeIntFiles (sectionhelper_int_test.go) and asLibraryError
// (library_test.go).

// helperBodyTableLiteral is the literal body table every case carries, written
// with \n escapes so it can sit inside a Go template string literal in a layout
// (html/template unquotes the escapes to real newlines, which is what mdcheck's
// table gate and parser then read).
const helperBodyTableLiteral = `| a | b |\n| --- | --- |\n| 1 | 2 |`

// helperBodyTableRefused asserts LoadLibrary refused the literal body table
// exactly once: the returned *LibraryError is positioned at wantPath (the
// calling layout), its message carries the opt-out refusal and guidance, and
// the refusal string appears exactly once (no duplicate emission from a second
// load step).
func helperBodyTableRefused(t *testing.T, lib *Library, err error, wantPath string) {
	t.Helper()
	if err == nil {
		t.Fatalf("LoadLibrary() = %+v, nil; want the literal body table refused", lib)
	}
	if lib != nil {
		t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
	}
	libErr := asLibraryError(t, err)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want the calling layout %q", libErr.Path, wantPath)
	}
	// The refusal is reached through the step-7 example execution of the
	// calling layout (the only step that executes a `{{ section … }}` call),
	// never a step-4 whole-manifest static error — which would be positioned
	// at the template.yaml and carry no execute context. This pins CheckBody's
	// table pass as the single source.
	if !strings.Contains(libErr.Message, "execute:") {
		t.Errorf("Message = %q, want a step-7 execute failure from CheckBody's table pass", libErr.Message)
	}
	if !strings.Contains(libErr.Message, tableOptOutMessage) {
		t.Errorf("Message = %q, want it to contain the opt-out refusal %q", libErr.Message, tableOptOutMessage)
	}
	if !strings.Contains(libErr.Message, "`tables` option") {
		t.Errorf("Message = %q, want guidance naming the `tables` option", libErr.Message)
	}
	if !strings.Contains(libErr.Message, tableOptOutFix) {
		t.Errorf("Message = %q, want the opt-out guidance %q", libErr.Message, tableOptOutFix)
	}
	if n := strings.Count(libErr.Message, tableOptOutMessage); n != 1 {
		t.Errorf("Message carries %q %d time(s), want exactly once: CheckBody's table pass is the single source",
			tableOptOutMessage, n)
	}
}

// helperBodyTableLoads asserts an opted-in target accepts the literal body
// table and the library loads cleanly.
func helperBodyTableLoads(t *testing.T, projectDir, what string) {
	t.Helper()
	lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: the opted-in %s accepts the literal table body", err, what)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}
}

// TestHelperBodyTables drives the helper/child literal-body table gate through
// the real template loader. A section-helper call and a child constructor each
// carry the same literal body table: the target that does not opt in refuses
// it once (guidance naming the `tables` option), and the target that opts in
// loads cleanly.
func TestHelperBodyTables(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: real filesystem")
	}

	// The target templates' `body` rules: no tables option (opt out) versus
	// `body.tables` with room for the two-column literal table (opt in).
	const (
		footerNoOptIn = "name: footer\nbody:\n  mode: optional\n"
		footerOptIn   = "name: footer\nbody:\n  mode: optional\n  tables:\n    max_tables: 2\n"
		cardNoOptIn   = "name: card\nbody:\n  mode: optional\n"
		cardOptIn     = "name: card\nbody:\n  mode: optional\n  tables:\n    max_tables: 2\n"
	)

	t.Run("a section-helper literal body table", func(t *testing.T) {
		host := templateIntFiles(SlidesDir, "host", "name: host\n",
			`{{ section "footer" nil "`+helperBodyTableLiteral+`" }}`, "# host\n")
		wantPath := filepath.Join(TemplatesDir, SlidesDir, "host", LayoutFile)

		t.Run("is refused at load when the target does not opt in", func(t *testing.T) {
			files := mergeIntFiles(host,
				sectionIntFiles("footer", footerNoOptIn, "<footer></footer>"))
			projectDir := writeSectionHelperIntLibrary(t, files)
			lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
			helperBodyTableRefused(t, lib, err, wantPath)
		})

		t.Run("loads when the target opts in", func(t *testing.T) {
			files := mergeIntFiles(host,
				sectionIntFiles("footer", footerOptIn, "<footer></footer>"))
			projectDir := writeSectionHelperIntLibrary(t, files)
			helperBodyTableLoads(t, projectDir, "section-helper target")
		})
	})

	t.Run("a child-constructor literal body table", func(t *testing.T) {
		// group is the `section` helper target; it declares the `cards` child
		// section the child constructor fills with the card template, whose
		// own body rule the literal table is checked against.
		host := templateIntFiles(SlidesDir, "host", "name: host\n",
			`{{ section "group" nil nil (child "cards" nil "`+helperBodyTableLiteral+`") }}`, "# host\n")
		// group declares the `cards` child section, so its example is parsed at
		// step 7: it carries no heading (a heading would be read as a nested
		// instance), only a body.
		group := templateIntFiles(SectionsDir, "group",
			"name: group\nsections:\n  - name: cards\n    accepted: [card]\n",
			"<div>{{ range .cards }}{{ . }}{{ end }}</div>", "Group.\n")
		wantPath := filepath.Join(TemplatesDir, SlidesDir, "host", LayoutFile)

		t.Run("is refused at load when the target does not opt in", func(t *testing.T) {
			files := mergeIntFiles(host, group,
				sectionIntFiles("card", cardNoOptIn, "<i></i>"))
			projectDir := writeSectionHelperIntLibrary(t, files)
			lib, err := LoadLibrary(os.DirFS(projectDir), TemplatesDir)
			helperBodyTableRefused(t, lib, err, wantPath)
		})

		t.Run("loads when the target opts in", func(t *testing.T) {
			files := mergeIntFiles(host, group,
				sectionIntFiles("card", cardOptIn, "<i></i>"))
			projectDir := writeSectionHelperIntLibrary(t, files)
			helperBodyTableLoads(t, projectDir, "child target")
		})
	})
}
