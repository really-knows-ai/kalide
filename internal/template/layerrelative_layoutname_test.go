package template

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-01.task-7: a library
// layout is named by its layer-relative forward-slash path
// (slides|sections/<name>/layout.html.tmpl), identical on every OS and wherever
// the layer sits on disk. A `{{ template "<name>" . }}` name that is not that
// exact form — one carrying a backslash, a templates/ prefix, or an absolute
// (root-qualified) path — names no library layout and must fail to resolve,
// even when a real layout exists at the corresponding layer-relative name
// (portable-layout-names).

// TestLayerRelativeLayoutNameRejected proves a library layout's name must be
// its layer-relative forward-slash path. Each case builds a real library with a
// `card` section registered at sections/card/layout.html.tmpl, then has an
// unguarded `outer` slide layout include card by a spelling that denotes the
// same file on some OS or as a root-joined path — a backslash path, a
// templates/ prefix, or an absolute root-qualified path. None of those is the
// registered name, so the include names no template: LoadLibrary's step-7
// example execution fails and the failure is a no-such-template error
// positioned at outer's own on-disk layout path, quoting the rejected name.
//
// The library is single-layer and in-memory, so the test runs under -short. It
// reuses baseLibraryFS, rootedFS and asLibraryError.
func TestLayerRelativeLayoutNameRejected(t *testing.T) {
	// The one real, layer-relative layout name the library registers.
	cardLayout := SectionsDir + "/card/" + LayoutFile

	cases := []struct {
		name   string
		target string
	}{
		{
			// A Windows-style separator path: it names the same file on disk
			// as sections/card/layout.html.tmpl, but the registered name uses
			// forward slashes on every OS, so it must not resolve.
			name:   "backslash separators",
			target: `sections\card\layout.html.tmpl`,
		},
		{
			// A root-relative spelling with the templates/ prefix: the prefix
			// is not part of a layout name (the name is relative to the layer
			// root).
			name:   "templates/ prefix",
			target: TemplatesDir + "/" + SectionsDir + "/card/" + LayoutFile,
		},
		{
			// An absolute, root-joined path: a name is never an OS path.
			name:   "absolute root-qualified path",
			target: "/abs/root/" + SectionsDir + "/card/" + LayoutFile,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := baseLibraryFS()

			// card: a real layout at its layer-relative name, so the only
			// reason the include fails is the rejected spelling.
			fsys["sections/card/template.yaml"] = &fstest.MapFile{Data: []byte("name: card\n")}
			fsys["sections/card/layout.html.tmpl"] = &fstest.MapFile{Data: []byte(`<i class="card">card</i>`)}
			fsys["sections/card/example.md"] = &fstest.MapFile{Data: []byte("# card\n")}

			// outer: a schema-free slide whose layout includes card by the
			// rejected spelling, unguarded so step 7 reaches it.
			fsys["slides/outer/template.yaml"] = &fstest.MapFile{Data: []byte("name: outer\n")}
			fsys["slides/outer/layout.html.tmpl"] = &fstest.MapFile{
				Data: []byte(`<div>{{ template ` + strconv.Quote(tc.target) + ` . }}</div>`),
			}
			fsys["slides/outer/example.md"] = &fstest.MapFile{Data: []byte("# outer\n")}

			lib, err := LoadLibrary(rootedFS(fsys), TemplatesDir)
			if err == nil {
				t.Fatalf("LoadLibrary() = %+v, nil; want the rejected name %q to fail to resolve", lib, tc.target)
			}
			if lib != nil {
				t.Fatalf("LoadLibrary() Library = %+v, want nil with the error", lib)
			}

			libErr := asLibraryError(t, err)
			// The failure is positioned at the invoking layout's on-disk path,
			// never at a layer-relative name.
			if want := filepath.Join(TemplatesDir, SlidesDir, "outer", LayoutFile); libErr.Path != want {
				t.Errorf("Path = %q, want the invoking layout's on-disk path %q", libErr.Path, want)
			}
			if !strings.Contains(libErr.Message, "no such template") {
				t.Errorf("Message = %q, want a no-such-template error", libErr.Message)
			}
			// The rejected name is quoted, and no layer-relative name is ever
			// printed as a file location.
			if q := strconv.Quote(tc.target); !strings.Contains(libErr.Message, q) {
				t.Errorf("Message = %q, want it to quote the rejected name %s", libErr.Message, q)
			}
			if strings.Contains(libErr.Message, cardLayout+":") {
				t.Errorf("Message = %q prints the layer-relative name %q as a file location", libErr.Message, cardLayout)
			}
		})
	}
}
