package template

import (
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-02.task-13 and
// plan.phase-02.task-14: it proves the two hook steps LoadLibrary fills in
// during phase 2 — checkLibraryBuild (step 4) and checkLibraryExamples (step
// 7) — keep the whole-pipeline fail-fast ordering documented on
// loadlibrary.go: the earliest failing step's error wins, never a later
// step's, and step 7 runs only once every earlier step (1-6) has passed.

// stepFixture is a minimal, otherwise-valid one-slide library (no themes, no
// media) so a case can break exactly the steps it names without an unrelated
// step also failing.
func stepFixture() fstest.MapFS {
	return fstest.MapFS{
		"library.yaml":                  {Data: []byte("name: demo\nformat: 1\n")},
		"slides/hello/template.yaml":    {Data: []byte("name: hello\n")},
		"slides/hello/layout.html.tmpl": {Data: []byte("<section><h1>{{.title}}</h1></section>")},
		"slides/hello/example.md":       {Data: []byte("---\n---\nHello.\n")},
	}
}

// TestLoadLibraryBuildChecksFirstError proves LoadLibrary reports the step-4
// (checkLibraryBuild) error first when the library is also broken at step 5
// (missing theme.css) and step 7 (an example that would fail its schema),
// and proves an earlier step (3) still wins over step 4.
func TestLoadLibraryBuildChecksFirstError(t *testing.T) {
	t.Run("step 4 wins over steps 5 and 7", func(t *testing.T) {
		fsys := stepFixture()
		// checkDefinition (step 4, via Register) rejects section name
		// "notes": a local, step-4-only problem that never reaches step 5
		// or step 7.
		fsys["slides/hello/template.yaml"] = &fstest.MapFile{
			Data: []byte("sections:\n  - name: notes\n    accepted: [hello]\n"),
		}
		// Step 5: a theme directory missing its required theme.css.
		fsys["themes/broken/other.txt"] = &fstest.MapFile{Data: []byte("x")}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, "reserved") {
			t.Errorf("Message = %q, want the step-4 reserved-name rejection, not a later step's", libErr.Message)
		}
	})

	t.Run("step 3 wins over step 4", func(t *testing.T) {
		fsys := stepFixture()
		// Step 3: the manifest is missing entirely (delete example.md so
		// step 3 fails on a missing required file) — actually delete the
		// layout file so step 3's file-presence check fails.
		delete(fsys, "slides/hello/layout.html.tmpl")
		// Step 4: also give the manifest a reserved field name, which would
		// fail step 4 if step 3 did not fail first.
		fsys["slides/hello/template.yaml"] = &fstest.MapFile{
			Data: []byte("fields:\n  - name: body\n    type: text\n"),
		}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, LayoutFile) {
			t.Errorf("Message = %q, want the step-3 missing-layout error, not the step-4 reserved-name one", libErr.Message)
		}
	})
}

// TestLoadLibraryExamplesFirstError proves step 7 (checkLibraryExamples)
// reports a positioned file:line error for a schema-violating example.md and
// for a layout execute failure, and that it runs only once steps 1-6 have
// all passed — an earlier-step failure suppresses it entirely.
func TestLoadLibraryExamplesFirstError(t *testing.T) {
	t.Run("invalid example.md reports file:line", func(t *testing.T) {
		fsys := stepFixture()
		fsys["slides/hello/template.yaml"] = &fstest.MapFile{
			Data: []byte("fields:\n  - name: title\n    type: text\n    required: true\n"),
		}
		// example.md's frontmatter omits the required title.
		fsys["slides/hello/example.md"] = &fstest.MapFile{Data: []byte("---\n---\nHello.\n")}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		wantPath := TemplatesDir + "/slides/hello/" + ExampleFile
		if libErr.Path != wantPath {
			t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
		}
		if libErr.Line == 0 {
			t.Error("Line = 0, want the frontmatter's positioned line")
		}
		if !strings.Contains(libErr.Message, "required") {
			t.Errorf("Message = %q, want it to say the required field is missing", libErr.Message)
		}
	})

	t.Run("layout execute failure reports file:line", func(t *testing.T) {
		fsys := stepFixture()
		// A layout that parses (documented func set only) but fails at
		// Execute time: indexing past the end of a nil/empty slice value
		// the render context never supplies.
		fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
			Data: []byte(`<section>{{index .Missing 0}}</section>`),
		}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		wantPath := TemplatesDir + "/slides/hello/" + LayoutFile
		if libErr.Path != wantPath {
			t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
		}
		if !strings.Contains(libErr.Message, "execute") {
			t.Errorf("Message = %q, want it to say layout execution failed", libErr.Message)
		}
	})

	t.Run("an earlier-step failure suppresses step 7", func(t *testing.T) {
		fsys := stepFixture()
		// Step 2: an unexpected top-level entry.
		fsys["bogus/file.txt"] = &fstest.MapFile{Data: []byte("x")}
		// Step 7: a layout that would fail to execute, if reached.
		fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
			Data: []byte(`<section>{{index .Missing 0}}</section>`),
		}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, "bogus") {
			t.Errorf("Message = %q, want the step-2 error, not step 7's execute failure", libErr.Message)
		}
	})
}
