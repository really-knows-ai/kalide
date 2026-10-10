package template

import (
	"path"
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
//
// TestLoadLibraryExternalLayerSelfContained additionally proves the per-layer
// self-containment check: an external layer loaded ALONE (LoadLibrary, which
// runs loadLibrary in libraryLayerFull mode — the same mode a deck's external
// `templates:` layer loads in) validates standalone, and a reference it makes
// to a name only a layered deck's local layer carries — a section, a media
// file or a theme — is a templates *LibraryError path-qualified to the
// external file that makes the reference (library-layer-references).

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
		// Step 3: a template directory missing its required example.md.
		delete(fsys, "slides/hello/example.md")
		// Step 7: a layout that would fail to execute, if reached.
		fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
			Data: []byte(`<section>{{index .Missing 0}}</section>`),
		}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if !strings.Contains(libErr.Message, ExampleFile) {
			t.Errorf("Message = %q, want the step-3 missing-example error, not step 7's execute failure", libErr.Message)
		}
	})

	// A container template's example.md nests section instances by heading
	// depth, and step 7 validates them recursively against each child's own
	// resolved template (template-build-checks, nested-section-validation).
	t.Run("a container template's nested example validates", func(t *testing.T) {
		_, err := LoadLibrary(rootedFS(containerExampleFixture()), TemplatesDir)
		if err != nil {
			t.Fatalf("LoadLibrary() error = %v, want nil: the nested example must validate recursively", err)
		}
	})

	t.Run("a nested violation fails at load with the containment path", func(t *testing.T) {
		fsys := containerExampleFixture()
		// The nested `item` child names an unknown field, so the violation is
		// inside the container's child instance, not the container's own
		// frontmatter. It must carry the child's containment path.
		fsys["sections/group/example.md"] = &fstest.MapFile{
			Data: []byte("# items\n```\nlable: First\n```\n"),
		}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		wantPath := TemplatesDir + "/sections/group/" + ExampleFile
		if libErr.Path != wantPath {
			t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
		}
		if libErr.Line == 0 {
			t.Error("Line = 0, want the nested child's positioned line")
		}
		if !strings.Contains(libErr.Message, "items[0]") {
			t.Errorf("Message = %q, want the child instance's containment path %q", libErr.Message, "items[0]")
		}
		if !strings.Contains(libErr.Message, `unknown field "lable"`) {
			t.Errorf("Message = %q, want the child's unknown-field violation", libErr.Message)
		}
	})
}

// containerExampleFixture is a minimal, otherwise-valid library holding a
// container section template (group) whose example.md nests an item child, and
// the leaf child section template (item) it accepts. The container declares
// `items` accepting item, so a nested example exercises the recursive
// heading-depth example validation of step 7 (template-build-checks).
func containerExampleFixture() fstest.MapFS {
	return fstest.MapFS{
		"library.yaml": {Data: []byte("name: container\nformat: 1\n")},

		// A slide template so the library has a slide; its example validates.
		"slides/hello/template.yaml":    {Data: []byte("fields:\n  - name: title\n    type: text\n")},
		"slides/hello/layout.html.tmpl": {Data: []byte("<section>{{.title}}</section>")},
		"slides/hello/example.md":       {Data: []byte("---\ntemplate: hello\ntitle: Hi\n---\n")},

		// The container: declares a child section `items` accepting item.
		"sections/group/template.yaml":    {Data: []byte("sections:\n  - name: items\n    accepted: [item]\n    min: 1\n    max: 4\n")},
		"sections/group/layout.html.tmpl": {Data: []byte("<div>{{range .items}}{{.}}{{end}}</div>")},
		"sections/group/example.md":       {Data: []byte("# items\n```\ntitle: First\n```\nbody\n")},

		// The leaf child section template.
		"sections/item/template.yaml":    {Data: []byte("fields:\n  - name: title\n    type: text\n    required: true\n  - name: label\n    type: text\n")},
		"sections/item/layout.html.tmpl": {Data: []byte("<span>{{.title}}</span>")},
		"sections/item/example.md":       {Data: []byte("```\ntitle: Sample\n```\n")},
	}
}

// externalSelfContainedFixture returns a minimal external template library that
// is self-contained: it carries its own slide, section, theme and media, and
// its slide and theme reference only names defined inside the external layer
// (its own section, its own media file and its own sibling theme). It is the
// baseline an external layer loaded ALONE must validate (loadLibrary in
// libraryLayerFull mode); each local-only subtest of
// TestLoadLibraryExternalLayerSelfContained breaks exactly one reference to
// point it at a name only a layered deck's local layer would carry.
func externalSelfContainedFixture() fstest.MapFS {
	return fstest.MapFS{
		"library.yaml": {Data: []byte("name: external\nformat: 1\n")},

		// The slide calls its own section and references its own media file.
		"slides/hello/template.yaml": {Data: []byte("name: hello\n")},
		"slides/hello/layout.html.tmpl": {Data: []byte(
			`<section>{{ section "ext-section" }}<img src="{{ media "ext.png" }}"></section>`)},
		"slides/hello/example.md": {Data: []byte("---\n---\n")},

		// Its own section, so the slide's helper call resolves in-layer.
		"sections/ext-section/template.yaml":    {Data: []byte("name: ext-section\n")},
		"sections/ext-section/layout.html.tmpl": {Data: []byte("<p>ext</p>")},
		"sections/ext-section/example.md":       {Data: []byte("```\n```\n")},

		// Its own theme, referencing its own sibling theme.
		"themes/ext-theme/theme.css":  {Data: []byte("@import 'theme:ext-accent/theme.css';\n")},
		"themes/ext-accent/theme.css": {Data: []byte("a{color:red;}\n")},

		// Its own media file.
		"media/ext.png": {Data: []byte("png")},
	}
}

// TestLoadLibraryExternalLayerSelfContained is the unit-test deliverable for
// plan.phase-02.task-14: it proves the per-layer self-containment check over
// in-memory external libraries. LoadLibrary runs loadLibrary in libraryLayerFull
// mode — the same mode LoadLibraryAt and a deck's external `templates:` layer
// load in — so it validates the layer it is given as a standalone library.
//
// An external layer loaded ALONE validates when it is self-contained: a
// fixture whose slide calls its own section and references its own media, and
// whose theme references its own sibling theme, loads. But a reference the
// external layer makes to a name only a layered deck's LOCAL (deck-root
// templates/) layer carries fails at that layer's own load, as a templates
// *LibraryError path-qualified to the external file that makes the reference:
//
//   - a `section` helper call to a local-only section fails at step 4
//     (checkLibraryBuild), positioned at the external template's
//     template.yaml;
//   - a `media` call to a local-only media file fails at step 6
//     (checkLayoutReferences), positioned at the external template's
//     layout.html.tmpl;
//   - a `theme:<name>/` reference to a local-only theme fails at step 6 (the
//     theme-CSS scanner), positioned at the external theme's theme.css.
//
// This is the per-layer half of library-layer-references: the merged load
// (LoadDeckLibrary) is where a local-layer template may call an external
// section, but an external layer is never allowed to depend on the local one.
// The self-contained baseline subtest proves the same references resolve when
// the name is carried by the external layer itself; each local-only subtest
// then points exactly one reference at a local-only name.
func TestLoadLibraryExternalLayerSelfContained(t *testing.T) {
	// Names a layered deck's LOCAL (deck-root templates/) layer would carry,
	// but the external fixture above does not: loaded alone, the external
	// layer must reject each reference to them.
	const (
		localOnlySection = "local-section"
		localOnlyTheme   = "local-theme"
		localOnlyMedia   = "local.png"
	)

	t.Run("a self-contained external layer validates standalone", func(t *testing.T) {
		lib, err := LoadLibrary(rootedFS(externalSelfContainedFixture()), TemplatesDir)
		if err != nil {
			t.Fatalf("LoadLibrary() error = %v, want nil: every reference resolves in the external layer itself", err)
		}
		if lib.Meta.Name != "external" {
			t.Errorf("Meta.Name = %q, want %q", lib.Meta.Name, "external")
		}
		if len(lib.Layers) != 1 {
			t.Errorf("len(Layers) = %d, want 1 (the external layer loaded alone)", len(lib.Layers))
		}
	})

	t.Run("a section call to a local-only section errors at the external manifest", func(t *testing.T) {
		fsys := externalSelfContainedFixture()
		// Point the slide's section helper call at a name only the local
		// layer would carry; step 4 resolves it against the external layer's
		// own (section) namespace and rejects it.
		fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
			Data: []byte(`<section>{{ section "` + localOnlySection + `" }}</section>`),
		}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if want := path.Join(TemplatesDir, SlidesDir, "hello", ManifestFile); libErr.Path != want {
			t.Errorf("Path = %q, want the external template's manifest %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, `"`+localOnlySection+`"`) {
			t.Errorf("Message = %q, want it to name the local-only section %q", libErr.Message, localOnlySection)
		}
		if !strings.Contains(libErr.Message, "not a defined template") {
			t.Errorf("Message = %q, want it to say the section is not defined in the external layer", libErr.Message)
		}
	})

	t.Run("a media call to a local-only media file errors at the external layout", func(t *testing.T) {
		fsys := externalSelfContainedFixture()
		// The external layer carries its own media/ tree (ext.png); the
		// referenced file exists only in the local layer, so step 6 rejects
		// the call against the external layer's media.
		fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
			Data: []byte(`<section>{{ media "` + localOnlyMedia + `" }}</section>`),
		}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if want := path.Join(TemplatesDir, SlidesDir, "hello", LayoutFile); libErr.Path != want {
			t.Errorf("Path = %q, want the external template's layout %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, localOnlyMedia) {
			t.Errorf("Message = %q, want it to name the local-only media file %q", libErr.Message, localOnlyMedia)
		}
	})

	t.Run("a theme reference to a local-only theme errors at the external theme.css", func(t *testing.T) {
		fsys := externalSelfContainedFixture()
		// The external layer carries its own themes (ext-theme, ext-accent);
		// the referenced theme exists only in the local layer, so the step-6
		// theme-CSS scanner rejects the reference against the external
		// layer's theme set.
		fsys["themes/ext-theme/theme.css"] = &fstest.MapFile{
			Data: []byte(`body { background: url('` + ThemeURLPrefix() + localOnlyTheme + `/fonts/x.woff2'); }`),
		}

		_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		libErr := asLibraryError(t, err)
		if want := path.Join(TemplatesDir, ThemesDir, "ext-theme", ThemeStylesheet); libErr.Path != want {
			t.Errorf("Path = %q, want the external theme's stylesheet %q", libErr.Path, want)
		}
		if !strings.Contains(libErr.Message, localOnlyTheme) {
			t.Errorf("Message = %q, want it to name the local-only theme %q", libErr.Message, localOnlyTheme)
		}
		if !strings.Contains(libErr.Message, "unknown theme") {
			t.Errorf("Message = %q, want it to say the theme is unknown in the external layer", libErr.Message)
		}
	})
}
