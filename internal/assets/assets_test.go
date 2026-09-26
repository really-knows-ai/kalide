package assets

import (
	"io/fs"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustRead(t, tt.fsys, tt.path)
		})
	}

	t.Run("theme.css not embedded", func(t *testing.T) {
		if _, err := fs.ReadFile(FS, "theme.css"); err == nil {
			t.Fatalf("theme.css unexpectedly embedded; themes come only from a project's templates/themes")
		}
	})
}
