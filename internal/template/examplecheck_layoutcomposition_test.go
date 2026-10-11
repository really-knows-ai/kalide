package template

import (
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-01 tasks 4-6: the
// load-time example check (templates-dir-validation step 7, checkLibraryExample)
// must execute every library layout against the MERGED library layout namespace
// (exampleLayoutNamespace), not the single target layout text alone, so a
// layout's standard `{{ template "<layout name>" . }}` composition call
// resolves (example-layout-composition).
//
// TestCheckLibraryExampleComposesLibraryLayout (task 4): a single-layer,
// in-memory library whose slide "outer" includes slide "hello"'s layout by its
// exact loader-built name loads cleanly, where executing outer's layout in a
// fresh single-layout namespace — the pre-fix behaviour — failed
// "no such template".

// TestCheckLibraryExampleComposesLibraryLayout proves LoadLibrary's step-7
// example check composes a slide layout that includes another library layout.
//
// "outer" is a slide whose layout calls the standard html/template composition
// form `{{ template "<layout name>" . }}`, naming slide "hello"'s layout by the
// exact name loadlibrary.go builds for it — path.Join(root,
// slides/hello/layout.html.tmpl) with root == TemplatesDir (loadlibrary.go:709)
// — which is the name exampleLayoutNamespace registers every library layout
// under. The library is single-layer and in-memory; LoadLibrary must succeed,
// whereas executing outer's layout text alone in a fresh namespace (the
// pre-fix step-7 behaviour) failed "no such template" because no other layout
// was registered for it to invoke.
func TestCheckLibraryExampleComposesLibraryLayout(t *testing.T) {
	// hello's layout carries a marker the composed output can be checked for,
	// so the test proves more than a bare successful load: the include
	// actually renders.
	const helloBody = "hello-composed-body"
	fsys := baseLibraryFS()
	fsys["slides/hello/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<section class="hello">` + helloBody + `</section>`),
	}
	fsys["slides/outer/template.yaml"] = &fstest.MapFile{
		Data: []byte("name: outer\ndescription: composes hello\n"),
	}
	// The exact layout name loadlibrary.go builds and registers:
	// path.Join(root, slides/<name>/layout.html.tmpl), root == TemplatesDir.
	helloLayoutName := TemplatesDir + "/slides/hello/" + LayoutFile
	fsys["slides/outer/layout.html.tmpl"] = &fstest.MapFile{
		Data: []byte(`<div>{{ template "` + helloLayoutName + `" . }}</div>`),
	}
	fsys["slides/outer/example.md"] = &fstest.MapFile{Data: []byte("# outer\n")}

	lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
	if err != nil {
		t.Fatalf("LoadLibrary() error = %v, want nil: outer's `{{ template %q . }}` include must resolve against the merged library layout namespace", err, helloLayoutName)
	}
	if lib == nil {
		t.Fatal("LoadLibrary() returned nil Library with nil error")
	}

	outer, _, ok := lib.TemplateByName("outer")
	if !ok || outer == nil {
		t.Fatal(`TemplateByName("outer") not found in loaded library`)
	}
	// Confirm the exact layout name loadlibrary.go builds:
	// path.Join(root, layoutRel) == root/slides/outer/layout.html.tmpl.
	if got, want := outer.Layout.Name(), TemplatesDir+"/slides/outer/"+LayoutFile; got != want {
		t.Errorf("outer.Layout.Name() = %q, want %q (the layout name loadlibrary.go builds)", got, want)
	}

	// Re-run step 7's execution for outer to observe the composed output
	// (LoadLibrary discards the rendered bytes): the merged namespace must hold
	// hello's layout under helloLayoutName, so the include renders it.
	namespace, err := exampleLayoutNamespace(lib, resolveMergedSlideLayer(lib, "outer"), nil)
	if err != nil {
		t.Fatalf("exampleLayoutNamespace() error = %v", err)
	}
	var buf strings.Builder
	if err := namespace.ExecuteTemplate(&buf, outer.Layout.Name(), map[string]any{
		"deck":  emptyExampleDeckContext(),
		"slide": emptyExampleSlideContext(),
	}); err != nil {
		t.Fatalf("ExecuteTemplate(%q) error = %v, want outer's include to resolve", outer.Layout.Name(), err)
	}
	if got := buf.String(); !strings.Contains(got, helloBody) {
		t.Errorf("outer rendered = %q, want it to contain hello's composed layout %q", got, helloBody)
	}
}
