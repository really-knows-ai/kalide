package assets

import (
	"io/fs"
	"path"
	"strings"
	"testing"
)

// mustRead fails the test unless the embedded asset exists and is non-empty.
func mustRead(t *testing.T, fsys fs.FS, name string) {
	t.Helper()
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("embedded asset %q: %v", name, err)
	}
	if len(data) == 0 {
		t.Fatalf("embedded asset %q is empty", name)
	}
}

func TestFSRevealAndTheme(t *testing.T) {
	tests := []struct {
		name string
		fsys fs.FS
		path string
	}{
		{name: "reveal.js core", fsys: Reveal(), path: "dist/reveal.js"},
		{name: "reveal.js stylesheet", fsys: Reveal(), path: "dist/reveal.css"},
		{name: "reveal.js reset stylesheet", fsys: Reveal(), path: "dist/reset.css"},
		{name: "reveal.js notes plugin", fsys: Reveal(), path: "dist/plugin/notes.js"},
		{name: "reveal.js LICENSE", fsys: Reveal(), path: "LICENSE"},
		{name: "default theme CSS", fsys: FS, path: "theme.css"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustRead(t, tt.fsys, tt.path)
		})
	}
}

func TestFSLogoVariants(t *testing.T) {
	for _, name := range []string{
		"logo_full-light.svg",
		"logo_full-dark.svg",
		"logo_small-light.svg",
		"logo_small-dark.svg",
	} {
		t.Run(name, func(t *testing.T) {
			mustRead(t, Logo(), name)
		})
	}
}

func TestFSFonts(t *testing.T) {
	fonts := Fonts()

	var files []string
	err := fs.WalkDir(fonts, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(name, ".woff2") {
			files = append(files, name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded fonts: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no .woff2 fonts embedded")
	}

	var interstate, gothic bool
	for _, name := range files {
		mustRead(t, fonts, name)
		switch base := path.Base(name); {
		case strings.HasPrefix(base, "EYInterstate"):
			interstate = true
		case strings.HasPrefix(base, "EYGothic"):
			gothic = true
		}
	}
	if !interstate {
		t.Fatalf("no EYInterstate .woff2 font embedded; found %v", files)
	}
	if !gothic {
		t.Fatalf("no EYGothic .woff2 font embedded; found %v", files)
	}
}
