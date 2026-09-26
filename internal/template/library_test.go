package template

import (
	"bytes"
	"errors"
	htmltemplate "html/template"
	"strings"
	"testing"
	"testing/fstest"
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

// TestLoadLibraryStep2UnknownTopLevelEntry proves step 2 rejects a top-level
// templates/ entry that is not one of the five documented names.
func TestLoadLibraryStep2UnknownTopLevelEntry(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["bogus/file.txt"] = &fstest.MapFile{Data: []byte("x")}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	if libErr.Path != TemplatesDir+"/bogus" {
		t.Errorf("Path = %q, want %q", libErr.Path, TemplatesDir+"/bogus")
	}
	if !strings.Contains(libErr.Message, "bogus") {
		t.Errorf("Message = %q, want it to name the unexpected entry", libErr.Message)
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
	wantPath := TemplatesDir + "/slides/hello/template.yaml"
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
	if !strings.Contains(libErr.Message, TemplatesDir+"/slides/hello") ||
		!strings.Contains(libErr.Message, TemplatesDir+"/sections/hello") {
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
	wantPath := TemplatesDir + "/slides/hello/example.md"
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
	wantPath := TemplatesDir + "/" + LibraryFile
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
// library.yaml format value.
func TestLoadLibraryStep1BadFormat(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["library.yaml"] = &fstest.MapFile{Data: []byte("name: demo\nformat: 2\n")}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := TemplatesDir + "/" + LibraryFile
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, "unsupported library format") {
		t.Errorf("Message = %q, want it to say the format is unsupported", libErr.Message)
	}
}

// TestLoadLibraryStep5MissingThemeCSS proves step 5 rejects a theme
// directory missing theme.css.
func TestLoadLibraryStep5MissingThemeCSS(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["themes/plain/other.txt"] = &fstest.MapFile{Data: []byte("x")}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	wantPath := TemplatesDir + "/themes/plain/" + ThemeStylesheet
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
	wantPath := TemplatesDir + "/themes/plain/" + ThemeStylesheet
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
	wantPath := TemplatesDir + "/slides/hello/" + LayoutFile
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
	wantPath := TemplatesDir + "/slides/hello/" + LayoutFile
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
	wantPath := TemplatesDir + "/slides/hello/" + LayoutFile
	if libErr.Path != wantPath {
		t.Errorf("Path = %q, want %q", libErr.Path, wantPath)
	}
	if !strings.Contains(libErr.Message, "must be relative") {
		t.Errorf("Message = %q, want it to say the media path must be relative", libErr.Message)
	}
}

// TestLoadLibraryFailFastOrdering proves LoadLibrary reports the earliest
// step's error even when a later step would also fail: a fixture broken at
// both step 2 (unknown top-level entry) and step 5 (missing theme.css) must
// report the step-2 error.
func TestLoadLibraryFailFastOrdering(t *testing.T) {
	fsys := baseLibraryFS()
	fsys["bogus/file.txt"] = &fstest.MapFile{Data: []byte("x")}
	fsys["themes/plain/other.txt"] = &fstest.MapFile{Data: []byte("x")}
	_, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	libErr := asLibraryError(t, err)
	if !strings.Contains(libErr.Message, "bogus") {
		t.Errorf("Message = %q, want the step-2 (unknown top-level entry) error, not a later step's", libErr.Message)
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
	wantPath := TemplatesDir + "/slides/hello/" + LayoutFile
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
