package template

import (
	"bytes"
	"errors"
	htmltemplate "html/template"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/really-knows-ai/kalide/internal/displaypath"
)

// This file is the unit-test deliverable for plan.phase-01.task-5: LoadLibrary's
// templates-dir-validation checks against an in-memory fstest.MapFS, exercised
// one validation step at a time so each failure's positioned *LibraryError can
// be asserted precisely, plus the fail-fast ordering across steps and the
// MediaFunc/LayoutFuncMap behavior LoadLibrary's step-3 layout parse and step-6
// reference checks rely on.
//
// Steps 4 (checkLibraryBuild) and 7 (checkLibraryExamples) are no-op hooks in
// phase 1 (phase-2 tasks 11/12 fill them in): every fixture here is built so it
// never depends on those hooks doing real validation — a fixture that reaches
// step 4 or step 7 only needs to observe that LoadLibrary returns successfully
// (or fails for an earlier-step reason), never that the hooks reject anything.

// minimalLibraryYAML is a valid library.yaml: name + format, no description.
const minimalLibraryYAML = "name: demo\nformat: 1\n"

// validManifest is a valid, minimal template.yaml with no `usage:` key (usage
// is derived from directory, not declared).
const validManifest = "name: hello\ndescription: a hello template\n"

// validExample is a placeholder example.md; step 7 (example validation) is a
// no-op in phase 1, so its content is never actually checked here.
const validExample = "# Example\n\nHello.\n"

// baseLibraryFS returns a minimal, fully valid library: library.yaml plus one
// slide template (slides/hello) with a plain layout and no media/theme
// references. Callers mutate a copy of the returned map to introduce one
// failure at a time.
func baseLibraryFS() fstest.MapFS {
	return fstest.MapFS{
		"library.yaml":                  {Data: []byte(minimalLibraryYAML)},
		"slides/hello/template.yaml":    {Data: []byte(validManifest)},
		"slides/hello/layout.html.tmpl": {Data: []byte("<section><h1>{{.Title}}</h1></section>")},
		"slides/hello/example.md":       {Data: []byte(validExample)},
	}
}

// cloneMapFS returns a shallow copy of fsys so a test can add/remove/replace
// entries without mutating a shared base fixture.
func cloneMapFS(fsys fstest.MapFS) fstest.MapFS {
	out := make(fstest.MapFS, len(fsys))
	for k, v := range fsys {
		out[k] = v
	}
	return out
}

func asLibraryError(t *testing.T, err error) *LibraryError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var libErr *LibraryError
	if !errors.As(err, &libErr) {
		t.Fatalf("expected *LibraryError, got %T: %v", err, err)
	}
	return libErr
}

func TestLoadLibraryValid(t *testing.T) {
	fsys := baseLibraryFS()
	// A fully-valid library also carries the inert library-root AGENTS.md
	// guide (library-guide-layout): the loader must still reach success, and
	// it must not expose the guide as a template.
	fsys["AGENTS.md"] = &fstest.MapFile{Data: []byte("# library/template-author guide\n\nAuthor templates.\n")}
	lib, err := LoadLibrary(fsys, "templates_dir")
	// Root is under a non-standard name here to prove root is a parameter,
	// not hardcoded; LoadLibrary is rooted at whatever root names.
	_ = lib
	if err == nil {
		t.Fatal("expected LoadLibrary to fail: fsys has no top-level root dir named templates_dir")
	}

	// Now root the fixture at "templates", the conventional TemplatesDir name,
	// by nesting every entry one level deeper.
	rooted := fstest.MapFS{}
	for k, v := range fsys {
		rooted[TemplatesDir+"/"+k] = v
	}
	lib, err = LoadLibrary(rooted, TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil", err)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}
	if lib.Meta.Name != "demo" || lib.Meta.Format != 1 {
		t.Errorf("Meta = %+v, want Name=demo Format=1", lib.Meta)
	}
	if _, _, ok := lib.TemplateByName("hello"); !ok {
		t.Error("TemplateByName(hello) not found in loaded library")
	}
	if lib.HasMedia {
		t.Error("HasMedia = true, want false: fixture has no media/ directory")
	}

	// named-library-definitions (plan.phase-04.task-14): a library.yaml that
	// also declares the optional named-definition sections — groups:,
	// field_sets: and types: — loads them onto the loading layer's
	// LibraryLayer.Definitions. The sections are additive to format 1:
	// loadLibraryDefinitions never inspects the format key, so the library
	// still declares `format: 1` and LibraryMeta stays name/description/format
	// only (it is untouched and stays comparable).
	t.Run("named definitions land on the layer and leave LibraryMeta alone", func(t *testing.T) {
		const defsLibraryYAML = `name: demo
description: a demo library
format: 1
groups:
  accents:
    - emphasis
    - note
field_sets:
  meta:
    - name: subtitle
      type: text
    - name: weight
      type: number
types:
  colour:
    type: enum
    variants:
      - red
      - green
      - blue
`
		fsys := baseLibraryFS()
		fsys["library.yaml"] = &fstest.MapFile{Data: []byte(defsLibraryYAML)}

		lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
		if err != nil {
			t.Fatalf("LoadLibrary() error = %v, want nil", err)
		}

		// The named-definition sections do not change format 1, and they never
		// reach LibraryMeta, which stays name/description/format only.
		wantMeta := LibraryMeta{Name: "demo", Description: "a demo library", Format: 1}
		if lib.Meta != wantMeta {
			t.Errorf("Meta = %+v, want %+v (name/description/format only, format unchanged at 1)", lib.Meta, wantMeta)
		}

		// layerFromLibrary copies the parsed definitions onto the loading
		// layer's Definitions (the single-layer load path).
		if len(lib.Layers) != 1 {
			t.Fatalf("Layers = %d, want exactly 1", len(lib.Layers))
		}
		defs := lib.Layers[0].Definitions
		if defs == nil {
			t.Fatal("Layers[0].Definitions = nil, want the parsed named definitions")
		}
		assertStringSlice(t, `Definitions.Groups["accents"]`, defs.Groups["accents"], []string{"emphasis", "note"})
		if got := defs.FieldSets["meta"]; len(got) != 2 ||
			got[0].Name != "subtitle" || got[0].Type != FieldText ||
			got[1].Name != "weight" || got[1].Type != FieldNumber {
			t.Errorf(`Definitions.FieldSets["meta"] = %+v, want subtitle:text then weight:number`, got)
		}
		colour := defs.Types["colour"]
		if colour.Name != "colour" || colour.Type != FieldEnum {
			t.Errorf(`Definitions.Types["colour"] = %+v, want name=colour type=enum`, colour)
		}
		assertStringSlice(t, `Definitions.Types["colour"].Variants`, colour.Variants, []string{"red", "green", "blue"})

		// loadLibraryMeta parses the sections through loadLibraryDefinitions
		// and keeps the result on the single-layer *Library's unexported
		// definitions carrier — the fallback NewRegistryFromLibrary consults
		// for the layerless Library step 4 builds, while layerFromLibrary also
		// copies them onto the layer asserted above.
		single := &Library{}
		if err := loadLibraryMeta(fsys, "templates", single); err != nil {
			t.Fatalf("loadLibraryMeta() error = %v, want nil", err)
		}
		if single.definitions == nil {
			t.Fatal("loadLibraryMeta left the single-layer *Library's definitions carrier nil")
		}
		assertStringSlice(t, `single.definitions.Groups["accents"]`, single.definitions.Groups["accents"], []string{"emphasis", "note"})
		if single.Meta != wantMeta {
			t.Errorf("loadLibraryMeta Meta = %+v, want %+v", single.Meta, wantMeta)
		}
	})
}

// rootedFS nests every entry of fsys one level under TemplatesDir, so
// LoadLibrary can be called with root == TemplatesDir the way a real caller
// would.
func rootedFS(fsys fstest.MapFS) fstest.MapFS {
	out := make(fstest.MapFS, len(fsys))
	for k, v := range fsys {
		out[TemplatesDir+"/"+k] = v
	}
	return out
}

// TestLoadLibraryIgnoresOtherTopLevelEntries proves step 2 validates only the
// library structure: a top-level entry that is not part of the library —
// .git/, .gitignore, README.md, an arbitrary directory, the inert AGENTS.md —
// is ignored and never blocks the load. This is what lets a git-hosted
// template library load.
func TestLoadLibraryIgnoresOtherTopLevelEntries(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["AGENTS.md"] = &fstest.MapFile{Data: []byte("# library guide\n")}
	fsys["README.md"] = &fstest.MapFile{Data: []byte("# notes\n")}
	fsys[".gitignore"] = &fstest.MapFile{Data: []byte("*.tmp\n")}
	fsys[".git/config"] = &fstest.MapFile{Data: []byte("[core]\n")}
	fsys["bogus/file.txt"] = &fstest.MapFile{Data: []byte("x")}

	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil (non-library top-level entries are ignored)", err)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}
	if _, _, ok := lib.TemplateByName("hello"); !ok {
		t.Error("TemplateByName(hello) not found in loaded library")
	}
}

// TestLoadLibraryStep3UsageKey proves step 3 rejects a template.yaml
// declaring a reserved `usage:` key, positioned at the offending line.
func TestLoadLibraryStep3UsageKey(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["slides/hello/template.yaml"] = &fstest.MapFile{
		Data: []byte("name: hello\nusage: slide\n"),
	}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, "slides/hello/"+ManifestFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if libErr.Line != 2 {
		t.Errorf("Line = %d, want 2 (the usage: line)", libErr.Line)
	}
	if !strings.Contains(libErr.Message, "usage") {
		t.Errorf("Message = %q, want it to mention the usage key", libErr.Message)
	}
}

// TestLoadLibraryStep2CrossKindDuplicateNaming proves step 2 rejects a name
// declared in both templates/slides and templates/sections, naming both
// paths in its message.
func TestLoadLibraryStep2CrossKindDuplicateNaming(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["sections/hello/template.yaml"] = &fstest.MapFile{Data: []byte(validManifest)}
	fsys["sections/hello/layout.html.tmpl"] = &fstest.MapFile{Data: []byte("<div></div>")}
	fsys["sections/hello/example.md"] = &fstest.MapFile{Data: []byte(validExample)}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	if !strings.Contains(libErr.Message, displaypath.Display(TemplatesDir, "slides/hello")) ||
		!strings.Contains(libErr.Message, displaypath.Display(TemplatesDir, "sections/hello")) {
		t.Errorf("Message = %q, want it to name both slides/hello and sections/hello paths", libErr.Message)
	}
}

// TestLoadLibraryStep3MissingTemplateFile proves step 3 rejects a template
// directory missing one of its three required files.
func TestLoadLibraryStep3MissingTemplateFile(t *testing.T) {
	fsys := baseLibraryFS()
	delete(fsys, "slides/hello/example.md")
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, "slides/hello/"+ExampleFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, ExampleFile) {
		t.Errorf("Message = %q, want it to mention %s", libErr.Message, ExampleFile)
	}
}

// TestLoadLibraryStep1BadName proves step 1 rejects a library.yaml name that
// does not match the naming convention.
func TestLoadLibraryStep1BadName(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["library.yaml"] = &fstest.MapFile{Data: []byte("name: Not_Valid\nformat: 1\n")}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, LibraryFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if libErr.Line != 1 {
		t.Errorf("Line = %d, want 1 (the name: line)", libErr.Line)
	}
	if !strings.Contains(libErr.Message, "not a valid name") {
		t.Errorf("Message = %q, want it to say the name is invalid", libErr.Message)
	}
}

// TestLoadLibraryStep1BadFormat proves step 1 rejects an unsupported
// library.yaml format value, and that a malformed library.yaml (step 1) or
// template.yaml manifest (step 3) is reported as a positioned *LibraryError
// carrying the offending source line and — when the offending line is a plain
// scalar YAML mis-read (a `:` inside it, or a first character of `*`/`&`/`%`/`@`
// that YAML reads as an alias, anchor, directive or reserved indicator) — the
// quoting hint (requirements.requirement.yaml-error-messages).
func TestLoadLibraryStep1BadFormat(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["library.yaml"] = &fstest.MapFile{Data: []byte("name: demo\nformat: 2\n")}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, LibraryFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, "unsupported library format") {
		t.Errorf("Message = %q, want it to say the format is unsupported", libErr.Message)
	}

	// yaml-error-messages: a library.yaml document that is not well-formed YAML
	// fails at step 1 before any key validation and is reported through
	// parseLibraryErrorLine, so the step-1 error is positioned at the offending
	// source line and, for a plain-scalar mis-read, carries the quoting hint.
	t.Run("malformed library.yaml reports the line and the quoting hint", func(t *testing.T) {
		cases := []struct {
			name        string
			yaml        string
			wantLine    int
			wantMessage string
			wantHint    bool
		}{
			{
				name:        "colon inside a plain scalar",
				yaml:        "name: demo\nformat: 1\ndescription: Hello: world\n",
				wantLine:    3,
				wantMessage: "mapping values are not allowed in this context",
				wantHint:    true,
			},
			{
				name:        "plain scalar beginning with an alias indicator",
				yaml:        "name: demo\nformat: 1\ndescription: *\n",
				wantLine:    3,
				wantMessage: "did not find expected alphabetic or numeric character",
				wantHint:    true,
			},
			{
				name:        "plain scalar beginning with an anchor indicator",
				yaml:        "name: demo\nformat: 1\ndescription: &\n",
				wantLine:    3,
				wantMessage: "did not find expected alphabetic or numeric character",
				wantHint:    true,
			},
			{
				name:        "plain scalar beginning with a directive indicator",
				yaml:        "name: demo\nformat: 1\ndescription: %example.com\n",
				wantLine:    3,
				wantMessage: "found character that cannot start any token",
				wantHint:    true,
			},
			{
				name:        "plain scalar beginning with a reserved indicator",
				yaml:        "name: demo\nformat: 1\ndescription: @example.com\n",
				wantLine:    3,
				wantMessage: "found character that cannot start any token",
				wantHint:    true,
			},
			{
				name:        "malformed YAML that is not a plain-scalar mis-read carries no hint",
				yaml:        "name: demo\nformat: 1\ndescription: \"unterminated\n",
				wantLine:    3,
				wantMessage: "found unexpected end of stream",
				wantHint:    false,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fsys := baseLibraryFS()
				fsys["library.yaml"] = &fstest.MapFile{Data: []byte(tc.yaml)}
				_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
				libErr := asLibraryError(t, err)
				if want := displaypath.Display(TemplatesDir, LibraryFile); libErr.Path != want {
					t.Errorf("Path = %q, want %q", libErr.Path, want)
				}
				if libErr.Line != tc.wantLine {
					t.Errorf("Line = %d, want %d (the offending source line)", libErr.Line, tc.wantLine)
				}
				if !strings.Contains(libErr.Message, tc.wantMessage) {
					t.Errorf("Message = %q, want it to contain %q", libErr.Message, tc.wantMessage)
				}
				assertQuotingHint(t, libErr, tc.wantHint)
			})
		}
	})

	// The same treatment applies to a malformed template.yaml manifest, caught
	// at step 3 and reported at the manifest's own path and line.
	t.Run("malformed template.yaml manifest reports the line and the quoting hint", func(t *testing.T) {
		cases := []struct {
			name        string
			manifest    string
			wantLine    int
			wantMessage string
			wantHint    bool
		}{
			{
				name:        "colon inside a plain scalar",
				manifest:    "name: hello\ndescription: Hello: world\n",
				wantLine:    2,
				wantMessage: "mapping values are not allowed in this context",
				wantHint:    true,
			},
			{
				name:        "plain scalar beginning with an alias indicator",
				manifest:    "name: hello\ndescription: *\n",
				wantLine:    2,
				wantMessage: "did not find expected alphabetic or numeric character",
				wantHint:    true,
			},
			{
				name:        "plain scalar beginning with an anchor indicator",
				manifest:    "name: hello\ndescription: &\n",
				wantLine:    2,
				wantMessage: "did not find expected alphabetic or numeric character",
				wantHint:    true,
			},
			{
				name:        "plain scalar beginning with a directive indicator",
				manifest:    "name: hello\ndescription: %example.com\n",
				wantLine:    2,
				wantMessage: "found character that cannot start any token",
				wantHint:    true,
			},
			{
				name:        "plain scalar beginning with a reserved indicator",
				manifest:    "name: hello\ndescription: @example.com\n",
				wantLine:    2,
				wantMessage: "found character that cannot start any token",
				wantHint:    true,
			},
			{
				name:        "malformed YAML that is not a plain-scalar mis-read carries no hint",
				manifest:    "name: hello\ndescription: \"unterminated\n",
				wantLine:    2,
				wantMessage: "found unexpected end of stream",
				wantHint:    false,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fsys := baseLibraryFS()
				fsys["slides/hello/template.yaml"] = &fstest.MapFile{Data: []byte(tc.manifest)}
				_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
				libErr := asLibraryError(t, err)
				wantPath := displaypath.Display(TemplatesDir, "slides/hello/"+ManifestFile)
				if libErr.Path != wantPath {
					t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
				}
				if libErr.Line != tc.wantLine {
					t.Errorf("Line = %d, want %d (the offending source line)", libErr.Line, tc.wantLine)
				}
				if !strings.Contains(libErr.Message, tc.wantMessage) {
					t.Errorf("Message = %q, want it to contain %q", libErr.Message, tc.wantMessage)
				}
				assertQuotingHint(t, libErr, tc.wantHint)
			})
		}
	})
}

// assertQuotingHint checks a *LibraryError's quoting hint against want: when
// want is true the error carries exactly the plain-scalar quoting hint the
// loader shares with the field checker, and when false it carries no hint at
// all. It pins the literal hint text, so the assertion is independent of the
// yamlQuotingHint constant.
func assertQuotingHint(t *testing.T, libErr *LibraryError, want bool) {
	t.Helper()
	const wantHint = "quote the value as a string"
	if !want {
		if libErr.Hint != "" {
			t.Errorf("Hint = %q, want none (the offending line is not a plain-scalar mis-read)", libErr.Hint)
		}
		return
	}
	if libErr.Hint != wantHint {
		t.Errorf("Hint = %q, want %q", libErr.Hint, wantHint)
	}
}

// TestLoadLibraryStep5MissingThemeCSS proves step 5 rejects a theme
// directory missing theme.css.
func TestLoadLibraryStep5MissingThemeCSS(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["themes/plain/other.txt"] = &fstest.MapFile{Data: []byte("x")}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, "themes/plain/"+ThemeStylesheet)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, ThemeStylesheet) {
		t.Errorf("Message = %q, want it to mention %s", libErr.Message, ThemeStylesheet)
	}
}

// TestLoadLibraryStep6ThemeURLEscapesDir proves step 6 rejects a theme.css
// url() reference that escapes its own theme directory with `..`.
func TestLoadLibraryStep6ThemeURLEscapesDir(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["themes/plain/theme.css"] = &fstest.MapFile{
		Data: []byte("body { background: url('../../etc/passwd'); }"),
	}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, "themes/plain/"+ThemeStylesheet)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, "escapes its theme directory") {
		t.Errorf("Message = %q, want it to say the url escapes the theme directory", libErr.Message)
	}
}

// TestLoadLibraryStep6MediaMissing proves step 6 rejects a layout `media`
// call naming a file that does not exist under templates/media.
func TestLoadLibraryStep6MediaMissing(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["media/logo.svg"] = &fstest.MapFile{Data: []byte("<svg/>")}
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<img src="{{media "missing.png"}}">`),
	}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, "slides/hello/"+LayoutFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, "not found") {
		t.Errorf("Message = %q, want it to say the media file was not found", libErr.Message)
	}
}

// TestLoadLibraryStep6MediaDotDot proves step 6 rejects a layout `media` call
// whose argument escapes templates/media with a `..` segment.
func TestLoadLibraryStep6MediaDotDot(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<img src="{{media "../secret.png"}}">`),
	}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, "slides/hello/"+LayoutFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, `must not escape`) {
		t.Errorf("Message = %q, want it to say the media path must not escape templates/media", libErr.Message)
	}
}

// TestLoadLibraryStep6MediaAbsolute proves step 6 rejects a layout `media`
// call whose argument is an absolute path.
func TestLoadLibraryStep6MediaAbsolute(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<img src="{{media "/etc/passwd"}}">`),
	}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, "slides/hello/"+LayoutFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, "must be relative") {
		t.Errorf("Message = %q, want it to say the media path must be relative", libErr.Message)
	}
}

// TestLoadLibraryFailFastOrdering proves LoadLibrary reports the earliest
// step's error even when a later step would also fail: a fixture broken at
// both step 1 (invalid library.yaml) and step 5 (missing theme.css) must
// report the step-1 error.
func TestLoadLibraryFailFastOrdering(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["library.yaml"] = &fstest.MapFile{Data: []byte("name: demo\nformat: 2\n")}
	fsys["themes/plain/other.txt"] = &fstest.MapFile{Data: []byte("x")}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	if !strings.Contains(libErr.Message, "unsupported library format") {
		t.Errorf("Message = %q, want the step-1 (bad format) error, not a later step's", libErr.Message)
	}
}

// TestLoadLibraryUndocumentedFuncFailsParse proves a layout calling a
// function outside the documented func set fails to parse, positioned at its
// layout file (html/template's own parse error carries the file:line, since
// LoadLibrary names the template with its full display path).
func TestLoadLibraryUndocumentedFuncFailsParse(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte("<section>{{ sprigify .Title }}</section>"),
	}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := displaypath.Display(TemplatesDir, "slides/hello/"+LayoutFile)
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, "sprigify") {
		t.Errorf("Message = %q, want it to name the undocumented function", libErr.Message)
	}
}

// TestLoadLibraryMediaRendersServedURL proves a valid `media` call in a
// layout resolves and, when the parsed layout is executed, produces the
// served URL MediaFunc computes (base "/media" plus the cleaned path).
func TestLoadLibraryMediaRendersServedURL(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["media/logo.svg"] = &fstest.MapFile{Data: []byte("<svg/>")}
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<img src="{{media "logo.svg"}}">`),
	}
	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil", err)
	}
	if !lib.HasMedia {
		t.Fatal("HasMedia = false, want true: fixture has templates/media/logo.svg")
	}
	tmpl, _, ok := lib.TemplateByName("hello")
	if !ok {
		t.Fatal("template hello not found")
	}
	var buf bytes.Buffer
	if err := tmpl.Layout.Execute(&buf, nil); err != nil {
		t.Fatalf("Layout.Execute() error = %v", err)
	}
	if got, want := buf.String(), `<img src="/media/logo.svg">`; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
}

// TestLayoutFuncMapHTMLVsAutoEscaped proves a layout's html/template
// auto-escapes a plain string value while a template.HTML value (e.g. an
// already-rendered body fragment, as the renderer passes body/text/section)
// passes through unescaped.
func TestLayoutFuncMapHTMLVsAutoEscaped(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<div>{{.Body}}</div><span>{{.Title}}</span>`),
	}
	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil", err)
	}
	tmpl, _, ok := lib.TemplateByName("hello")
	if !ok {
		t.Fatal("template hello not found")
	}

	// html/template distinguishes template.HTML (trusted) from a plain string
	// (auto-escaped) purely by the field's static Go type.
	type htmlData struct {
		Body  htmltemplate.HTML
		Title string
	}

	var buf bytes.Buffer
	if err := tmpl.Layout.Execute(&buf, htmlData{
		Body:  htmltemplate.HTML("<b>bold</b>"),
		Title: "<script>alert(1)</script>",
	}); err != nil {
		t.Fatalf("Layout.Execute() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "<div><b>bold</b></div>") {
		t.Errorf("rendered = %q, want the template.HTML Body passed through unescaped", got)
	}
	if strings.Contains(got, "<script>alert(1)</script>") {
		t.Errorf("rendered = %q, want the plain string Title auto-escaped", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("rendered = %q, want the plain string Title HTML-escaped", got)
	}
}

// ---------------------------------------------------------------------------
// plan.phase-01.task-10: unit tests for the multi-layer merge
// (mergeLibraryLayers, loadlibrary.go) and cross-layer name uniqueness
// (checkLayerUniqueness, libraryunique.go).
//
// Both functions operate on already-loaded *LibraryLayer values — loadLibrary's
// output, not a filesystem — so these tests hand-build layers whose RootPath,
// loaded maps and media sub-filesystem have exactly the shape loadLibrary
// fills, reproducing the loaded manifest/theme-relative paths so each error
// reads the way a real load produces it.

// testLayer returns a hand-built LibraryLayer whose loaded maps are
// initialized and whose Root is an empty in-memory fs.FS, mirroring the shape
// loadLibrary returns. root becomes the layer's RootPath: the path the
// uniqueness errors are qualified with (LibraryLayer.RootPath).
func testLayer(root string) *LibraryLayer {
	return &LibraryLayer{
		Root:     fstest.MapFS{},
		RootPath: root,
		Slides:   map[string]*LibraryTemplate{},
		Sections: map[string]*LibraryTemplate{},
		Themes:   map[string]*LibraryTheme{},
	}
}

// addTestSlide adds a hand-built slide template named name to layer, with the
// Dir and ManifestPath loadLibrary's loadLibraryTemplate fills in
// (<root>/slides/<name>/template.yaml).
func addTestSlide(layer *LibraryLayer, name string) *LibraryTemplate {
	lt := &LibraryTemplate{
		Name:         name,
		Kind:         KindSlide,
		Dir:          path.Join(SlidesDir, name),
		ManifestPath: displaypath.Display(layer.RootPath, path.Join(SlidesDir, name, ManifestFile)),
	}
	layer.Slides[name] = lt
	return lt
}

// addTestSection adds a hand-built section template named name to layer, with
// the Dir and ManifestPath loadLibrary fills in
// (<root>/sections/<name>/template.yaml).
func addTestSection(layer *LibraryLayer, name string) *LibraryTemplate {
	lt := &LibraryTemplate{
		Name:         name,
		Kind:         KindSection,
		Dir:          path.Join(SectionsDir, name),
		ManifestPath: displaypath.Display(layer.RootPath, path.Join(SectionsDir, name, ManifestFile)),
	}
	layer.Sections[name] = lt
	return lt
}

// addTestTheme adds a hand-built theme named name to layer, with the Dir
// loadLibrary's loadLibraryTheme fills in (<name> under themes/).
func addTestTheme(layer *LibraryLayer, name string) *LibraryTheme {
	th := &LibraryTheme{
		Name: name,
		Dir:  path.Join(ThemesDir, name),
	}
	layer.Themes[name] = th
	return th
}

// setTestMedia gives layer a media/ sub-filesystem holding one file per
// relative path, keyed exactly as loadLibrary's fs.Sub(sub, MediaDir) would: a
// media path is a direct fs.FS path within it.
func setTestMedia(layer *LibraryLayer, rels ...string) {
	fsys := fstest.MapFS{}
	for _, rel := range rels {
		fsys[rel] = &fstest.MapFile{Data: []byte(rel)}
	}
	layer.HasMedia = true
	layer.Media = fsys
}

// assertLayerDuplicateError asserts err is a *LibraryError that names the
// duplicated name and both carrying paths — Path is the first layer's path and
// the message names both — under the given namespace word ("template",
// "theme" or "media path").
func assertLayerDuplicateError(t *testing.T, err error, namespace, name, firstPath, secondPath string) {
	t.Helper()
	libErr := asLibraryError(t, err)
	if libErr.Path != firstPath {
		t.Errorf("Path = %q, want the first layer's path %q", libErr.Path, firstPath)
	}
	if libErr.Line != 0 {
		t.Errorf("Line = %d, want 0 (a whole-path uniqueness error)", libErr.Line)
	}
	if !strings.Contains(libErr.Message, namespace) {
		t.Errorf("Message = %q, want it to name the %s namespace", libErr.Message, namespace)
	}
	if !strings.Contains(libErr.Message, name) {
		t.Errorf("Message = %q, want it to name %q", libErr.Message, name)
	}
	if !strings.Contains(libErr.Message, firstPath) {
		t.Errorf("Message = %q, want it to name the first path %q", libErr.Message, firstPath)
	}
	if !strings.Contains(libErr.Message, secondPath) {
		t.Errorf("Message = %q, want it to name the second path %q", libErr.Message, secondPath)
	}
}

// TestMergeLibraryLayersZeroLayers proves mergeLibraryLayers of no layers
// yields an empty, media-less Library with initialized (non-nil) maps, so a
// caller can read the merged value without a nil check.
func TestMergeLibraryLayersZeroLayers(t *testing.T) {
	for _, layers := range [][]*LibraryLayer{nil, {}} {
		merged := mergeLibraryLayers(layers)
		if merged == nil {
			t.Fatal("mergeLibraryLayers() = nil, want a non-nil empty Library")
		}
		if merged.Slides == nil || merged.Sections == nil || merged.Themes == nil {
			t.Errorf("merged maps = (slides %v, sections %v, themes %v), want all non-nil",
				merged.Slides, merged.Sections, merged.Themes)
		}
		if len(merged.Slides) != 0 || len(merged.Sections) != 0 || len(merged.Themes) != 0 {
			t.Errorf("merged maps = %d/%d/%d entries, want all empty",
				len(merged.Slides), len(merged.Sections), len(merged.Themes))
		}
		if merged.Layers != nil {
			t.Errorf("Layers = %v, want nil", merged.Layers)
		}
		if merged.HasMedia {
			t.Error("HasMedia = true, want false")
		}
		if merged.RootPath != "" {
			t.Errorf("RootPath = %q, want empty", merged.RootPath)
		}
	}
}

// TestMergeLibraryLayersSingleLayer proves a one-layer merge is exactly the
// Library libraryFromLayer builds: the layer's own content, metadata, root and
// media, with Layers carrying that one layer.
func TestMergeLibraryLayersSingleLayer(t *testing.T) {
	layer := testLayer("/only")
	layer.Meta = LibraryMeta{Name: "only", Format: 1}
	slide := addTestSlide(layer, "hello")
	section := addTestSection(layer, "item")
	theme := addTestTheme(layer, "plain")
	setTestMedia(layer, "logo.svg")

	merged := mergeLibraryLayers([]*LibraryLayer{layer})
	if merged == nil {
		t.Fatal("mergeLibraryLayers() = nil, want a Library")
	}
	if len(merged.Layers) != 1 || merged.Layers[0] != layer {
		t.Fatalf("Layers = %v, want exactly the input layer", merged.Layers)
	}
	if merged.RootPath != "/only" {
		t.Errorf("RootPath = %q, want %q", merged.RootPath, "/only")
	}
	if merged.Meta != layer.Meta {
		t.Errorf("Meta = %+v, want the layer's %+v", merged.Meta, layer.Meta)
	}
	if merged.Slides["hello"] != slide {
		t.Error("Slides[hello] is not the layer's own template")
	}
	if merged.Sections["item"] != section {
		t.Error("Sections[item] is not the layer's own template")
	}
	if merged.Themes["plain"] != theme {
		t.Error("Themes[plain] is not the layer's own theme")
	}
	if !merged.HasMedia {
		t.Error("HasMedia = false, want true")
	}
	if _, err := fs.Stat(merged.Media, "logo.svg"); err != nil {
		t.Errorf("merged.Media does not resolve the layer's logo.svg: %v", err)
	}
}

// TestMergeLibraryLayersDisjointUnion proves two disjoint layers merge into
// one Library carrying every entry of both, with Layers preserving the ordered
// inputs.
func TestMergeLibraryLayersDisjointUnion(t *testing.T) {
	external := testLayer("/ext")
	addTestSlide(external, "ext-slide")
	addTestSection(external, "ext-section")
	addTestTheme(external, "ext-theme")

	local := testLayer("/local")
	addTestSlide(local, "local-slide")
	addTestSection(local, "local-section")
	addTestTheme(local, "local-theme")

	merged := mergeLibraryLayers([]*LibraryLayer{external, local})

	if len(merged.Layers) != 2 || merged.Layers[0] != external || merged.Layers[1] != local {
		t.Fatalf("Layers = %v, want [external local] in resolution order", merged.Layers)
	}
	for _, name := range []string{"ext-slide", "local-slide"} {
		if merged.Slides[name] == nil {
			t.Errorf("Slides[%q] missing from the merged union", name)
		}
	}
	for _, name := range []string{"ext-section", "local-section"} {
		if merged.Sections[name] == nil {
			t.Errorf("Sections[%q] missing from the merged union", name)
		}
	}
	for _, name := range []string{"ext-theme", "local-theme"} {
		if merged.Themes[name] == nil {
			t.Errorf("Themes[%q] missing from the merged union", name)
		}
	}
}

// TestMergeLibraryLayersEqualStanding proves the layers merge with equal
// standing: a layer's entries survive in the merged maps as that layer's own
// objects regardless of which layer contributes them, so neither layer
// overrides or displaces the other.
func TestMergeLibraryLayersEqualStanding(t *testing.T) {
	external := testLayer("/ext")
	extSlide := addTestSlide(external, "ext-slide")

	local := testLayer("/local")
	localSlide := addTestSlide(local, "local-slide")

	// Neither name is carried by the other layer (duplicates are rejected by
	// checkLayerUniqueness before merge), so both must survive.
	merged := mergeLibraryLayers([]*LibraryLayer{external, local})
	if merged.Slides["ext-slide"] != extSlide {
		t.Error("Slides[ext-slide] is not the external layer's own template")
	}
	if merged.Slides["local-slide"] != localSlide {
		t.Error("Slides[local-slide] is not the local layer's own template")
	}
}

// TestMergeLibraryLayersPrimaryFromLocalLayer proves the merged Meta, Root and
// RootPath come from the primary layer — the deck-root local templates/ layer,
// which (the hand-built layers carrying no role) is the last in resolution
// order — while every layer's own metadata and root stay verbatim in Layers.
func TestMergeLibraryLayersPrimaryFromLocalLayer(t *testing.T) {
	external := testLayer("/ext")
	external.Root = fstest.MapFS{"external.root": &fstest.MapFile{Data: []byte("external")}}
	external.Meta = LibraryMeta{Name: "external", Description: "the external layer", Format: 1}

	local := testLayer("/local")
	local.Root = fstest.MapFS{"local.root": &fstest.MapFile{Data: []byte("local")}}
	local.Meta = LibraryMeta{Name: "local", Description: "the local layer", Format: 1}

	merged := mergeLibraryLayers([]*LibraryLayer{external, local})

	if merged.Meta != local.Meta {
		t.Errorf("Meta = %+v, want the local (primary) layer's %+v", merged.Meta, local.Meta)
	}
	if merged.RootPath != "/local" {
		t.Errorf("RootPath = %q, want the local (primary) layer's %q", merged.RootPath, "/local")
	}
	if _, err := fs.Stat(merged.Root, "local.root"); err != nil {
		t.Errorf("merged.Root does not resolve the local (primary) layer's file: %v", err)
	}
	if _, err := fs.Stat(merged.Root, "external.root"); err == nil {
		t.Error("merged.Root resolves the external layer's file, want the local (primary) layer's root only")
	}
	if merged.Layers[0].Meta != external.Meta || merged.Layers[1].Meta != local.Meta {
		t.Errorf("Layers metas = (%+v, %+v), want (%+v, %+v) verbatim",
			merged.Layers[0].Meta, merged.Layers[1].Meta, external.Meta, local.Meta)
	}
	if merged.Layers[0].RootPath != "/ext" {
		t.Errorf("Layers[0].RootPath = %q, want the external layer's %q", merged.Layers[0].RootPath, "/ext")
	}
}

// TestMergeLibraryLayersRetainsPerLayerMedia proves each layer's own media
// sub-filesystem stays addressable through Layers, and the merged Media is the
// primary (local) layer's — the external layer's media is reachable only
// through its own layer, so there is no merged media namespace.
func TestMergeLibraryLayersRetainsPerLayerMedia(t *testing.T) {
	external := testLayer("/ext")
	setTestMedia(external, "ext.png", "shared/logo.svg")
	local := testLayer("/local")
	setTestMedia(local, "local.png")

	merged := mergeLibraryLayers([]*LibraryLayer{external, local})

	if !merged.HasMedia {
		t.Error("HasMedia = false, want true (both layers carry media)")
	}
	if _, err := fs.Stat(merged.Layers[0].Media, "ext.png"); err != nil {
		t.Errorf("Layers[0].Media (external) does not resolve ext.png: %v", err)
	}
	if _, err := fs.Stat(merged.Layers[1].Media, "local.png"); err != nil {
		t.Errorf("Layers[1].Media (local) does not resolve local.png: %v", err)
	}
	if _, err := fs.Stat(merged.Media, "local.png"); err != nil {
		t.Errorf("merged.Media (the primary local layer's) does not resolve local.png: %v", err)
	}
	if _, err := fs.Stat(merged.Media, "ext.png"); err == nil {
		t.Error("merged.Media resolves the external layer's file, want the primary (local) layer's media only")
	}
}

// TestMergeLibraryLayersHasMediaFromPrimaryLayer proves HasMedia and Media come
// from the PRIMARY layer only (HasMedia = primary.HasMedia), not the union of
// the layers: the local (primary) layer's media is exposed on the merged
// Library, while media carried only by the non-primary external layer leaves
// merged.HasMedia false and merged.Media nil. A local layer selected by its
// LayerRoleLocal wins even when it is not the last layer, and a single layer is
// its own primary.
func TestMergeLibraryLayersHasMediaFromPrimaryLayer(t *testing.T) {
	t.Run("media only on the primary local layer", func(t *testing.T) {
		external := testLayer("/ext")
		local := testLayer("/local")
		setTestMedia(local, "local.png")

		merged := mergeLibraryLayers([]*LibraryLayer{external, local})
		if !merged.HasMedia {
			t.Error("HasMedia = false, want true: the primary (local) layer carries media")
		}
		if merged.Layers[0].HasMedia {
			t.Error("Layers[0].HasMedia = true, want false: the external layer has no media")
		}
		if !merged.Layers[1].HasMedia {
			t.Error("Layers[1].HasMedia = false, want true")
		}
		if _, err := fs.Stat(merged.Media, "local.png"); err != nil {
			t.Errorf("merged.Media does not resolve the primary layer's local.png: %v", err)
		}
	})

	t.Run("media only on the non-primary external layer", func(t *testing.T) {
		external := testLayer("/ext")
		setTestMedia(external, "ext.png")
		local := testLayer("/local")

		merged := mergeLibraryLayers([]*LibraryLayer{external, local})
		if merged.HasMedia {
			t.Error("HasMedia = true, want false: only the non-primary external layer carries media")
		}
		if merged.Media != nil {
			t.Errorf("merged.Media = %v, want nil: the primary (local) layer has no media", merged.Media)
		}
		if !merged.Layers[0].HasMedia {
			t.Error("Layers[0].HasMedia = false, want true: the external layer still carries its own media")
		}
		if _, err := fs.Stat(merged.Layers[0].Media, "ext.png"); err != nil {
			t.Errorf("Layers[0].Media does not resolve ext.png: %v", err)
		}
	})

	t.Run("primary chosen by role: LayerRoleLocal wins even when not last", func(t *testing.T) {
		// local is FIRST here and external LAST, so a last-layer fallback would
		// pick the external layer; the layer's Role must pick the local layer.
		local := testLayer("/local")
		local.Role = LayerRoleLocal
		external := testLayer("/ext")
		external.Role = LayerRoleExternal
		setTestMedia(external, "ext.png")

		merged := mergeLibraryLayers([]*LibraryLayer{local, external})
		if merged.HasMedia {
			t.Error("HasMedia = true, want false: the primary (local) layer carries no media, though the trailing external layer does")
		}
		if merged.Media != nil {
			t.Errorf("merged.Media = %v, want nil: the primary (local) layer has no media", merged.Media)
		}
		if merged.RootPath != "/local" {
			t.Errorf("RootPath = %q, want the role-selected local layer %q", merged.RootPath, "/local")
		}
	})

	t.Run("single media-carrying layer", func(t *testing.T) {
		only := testLayer("/only")
		setTestMedia(only, "only.png")

		merged := mergeLibraryLayers([]*LibraryLayer{only})
		if !merged.HasMedia {
			t.Error("HasMedia = false, want true for a single media-carrying layer")
		}
		if _, err := fs.Stat(merged.Media, "only.png"); err != nil {
			t.Errorf("merged.Media does not resolve only.png: %v", err)
		}
	})
}

// TestCheckLayerUniquenessNoDuplicates proves disjoint layers pass: every
// slide, section, theme and media path is carried by at most one layer, and a
// nil entry is ignored.
func TestCheckLayerUniquenessNoDuplicates(t *testing.T) {
	external := testLayer("/ext")
	addTestSlide(external, "ext-slide")
	addTestSection(external, "ext-section")
	addTestTheme(external, "ext-theme")
	setTestMedia(external, "ext.png")

	local := testLayer("/local")
	addTestSlide(local, "local-slide")
	addTestSection(local, "local-section")
	addTestTheme(local, "local-theme")
	setTestMedia(local, "local.png")

	if err := checkLayerUniqueness([]*LibraryLayer{external, local}); err != nil {
		t.Fatalf("checkLayerUniqueness() = %v, want nil for disjoint layers", err)
	}
	if err := checkLayerUniqueness([]*LibraryLayer{external, nil, local}); err != nil {
		t.Fatalf("checkLayerUniqueness() with a nil layer = %v, want nil (nil ignored)", err)
	}
}

// TestCheckLayerUniquenessSlideDuplicate proves a slide name carried by two
// layers is rejected, naming the name and both manifest paths.
func TestCheckLayerUniquenessSlideDuplicate(t *testing.T) {
	external := testLayer("/ext")
	addTestSlide(external, "dup")
	local := testLayer("/local")
	addTestSlide(local, "dup")

	err := checkLayerUniqueness([]*LibraryLayer{external, local})
	assertLayerDuplicateError(t, err, "template", "dup",
		filepath.Join("/ext", SlidesDir, "dup", ManifestFile), filepath.Join("/local", SlidesDir, "dup", ManifestFile))
}

// TestCheckLayerUniquenessSectionDuplicate proves a section name carried by two
// layers is rejected, naming the name and both manifest paths.
func TestCheckLayerUniquenessSectionDuplicate(t *testing.T) {
	external := testLayer("/ext")
	addTestSection(external, "dup")
	local := testLayer("/local")
	addTestSection(local, "dup")

	err := checkLayerUniqueness([]*LibraryLayer{external, local})
	assertLayerDuplicateError(t, err, "template", "dup",
		filepath.Join("/ext", SectionsDir, "dup", ManifestFile), filepath.Join("/local", SectionsDir, "dup", ManifestFile))
}

// TestCheckLayerUniquenessCrossKindDuplicate proves slides and sections share
// one template namespace across layers: a slide in one layer and a section in
// another collide just as two same-kind templates do, in both directions.
func TestCheckLayerUniquenessCrossKindDuplicate(t *testing.T) {
	t.Run("external slide vs local section", func(t *testing.T) {
		external := testLayer("/ext")
		addTestSlide(external, "dup")
		local := testLayer("/local")
		addTestSection(local, "dup")

		err := checkLayerUniqueness([]*LibraryLayer{external, local})
		assertLayerDuplicateError(t, err, "template", "dup",
			filepath.Join("/ext", SlidesDir, "dup", ManifestFile), filepath.Join("/local", SectionsDir, "dup", ManifestFile))
	})

	t.Run("external section vs local slide", func(t *testing.T) {
		external := testLayer("/ext")
		addTestSection(external, "dup")
		local := testLayer("/local")
		addTestSlide(local, "dup")

		err := checkLayerUniqueness([]*LibraryLayer{external, local})
		assertLayerDuplicateError(t, err, "template", "dup",
			filepath.Join("/ext", SectionsDir, "dup", ManifestFile), filepath.Join("/local", SlidesDir, "dup", ManifestFile))
	})
}

// TestCheckLayerUniquenessThemeDuplicate proves a theme name carried by two
// layers is rejected, naming the name and both theme directory paths
// (<root>/themes/<name>).
func TestCheckLayerUniquenessThemeDuplicate(t *testing.T) {
	external := testLayer("/ext")
	addTestTheme(external, "dup")
	local := testLayer("/local")
	addTestTheme(local, "dup")

	err := checkLayerUniqueness([]*LibraryLayer{external, local})
	assertLayerDuplicateError(t, err, "theme", "dup",
		filepath.Join("/ext", ThemesDir, "dup"), filepath.Join("/local", ThemesDir, "dup"))
}

// TestCheckLayerUniquenessMediaDuplicate proves a media path carried by two
// layers is rejected, naming the relative path and its full location under
// both layer roots (<root>/media/<rel>).
func TestCheckLayerUniquenessMediaDuplicate(t *testing.T) {
	t.Run("top-level file", func(t *testing.T) {
		external := testLayer("/ext")
		setTestMedia(external, "logo.svg", "ext-only.png")
		local := testLayer("/local")
		setTestMedia(local, "logo.svg")

		err := checkLayerUniqueness([]*LibraryLayer{external, local})
		assertLayerDuplicateError(t, err, "media path", "logo.svg",
			filepath.Join("/ext", MediaDir, "logo.svg"), filepath.Join("/local", MediaDir, "logo.svg"))
	})

	t.Run("nested file", func(t *testing.T) {
		external := testLayer("/ext")
		setTestMedia(external, "video/intro.mp4")
		local := testLayer("/local")
		setTestMedia(local, "video/intro.mp4")

		err := checkLayerUniqueness([]*LibraryLayer{external, local})
		assertLayerDuplicateError(t, err, "media path", "video/intro.mp4",
			filepath.Join("/ext", MediaDir, "video", "intro.mp4"), filepath.Join("/local", MediaDir, "video", "intro.mp4"))
	})
}

// TestCheckLayerUniquenessDeterministicFirstViolation proves the check reports
// the same first violation across runs even when several namespaces collide at
// once. The layers carry template collisions ("aaa", "bbb"), a theme collision
// ("aaa") and a media collision ("aaa.png"); the fixed namespace order
// (templates, themes, media) and the sorted-name order inside a namespace make
// the template "aaa" collision the one reported every time.
func TestCheckLayerUniquenessDeterministicFirstViolation(t *testing.T) {
	external := testLayer("/ext")
	addTestSlide(external, "aaa")
	addTestSlide(external, "bbb")
	addTestTheme(external, "aaa")
	setTestMedia(external, "aaa.png")

	local := testLayer("/local")
	addTestSlide(local, "aaa")
	addTestSlide(local, "bbb")
	addTestTheme(local, "aaa")
	setTestMedia(local, "aaa.png")

	const runs = 100
	var first string
	for i := 0; i < runs; i++ {
		libErr := asLibraryError(t, checkLayerUniqueness([]*LibraryLayer{external, local}))
		if first == "" {
			first = libErr.Error()
			continue
		}
		if libErr.Error() != first {
			t.Fatalf("run %d: error = %q, want the same first violation %q", i, libErr.Error(), first)
		}
	}

	if !strings.Contains(first, `template "aaa"`) {
		t.Errorf("first violation = %q, want the template %q collision", first, "aaa")
	}
	if strings.Contains(first, `theme "aaa"`) {
		t.Errorf("first violation = %q, want the templates namespace checked before themes", first)
	}
	if strings.Contains(first, "media path") {
		t.Errorf("first violation = %q, want the templates namespace checked before media", first)
	}
	if !strings.Contains(first, filepath.Join("/ext", SlidesDir, "aaa", ManifestFile)) ||
		!strings.Contains(first, filepath.Join("/local", SlidesDir, "aaa", ManifestFile)) {
		t.Errorf("first violation = %q, want both colliding manifest paths", first)
	}
}
