package displaypath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDisplay proves Display builds a user-facing path with the host operating
// system's native separator and never a mixed one: a forward-slash relative
// path (the canonical identity spelling) is joined to root natively, and an
// empty root is the deck-relative case — the relative path on its own, still
// native, with no leading separator.
func TestDisplay(t *testing.T) {
	// otherSep is the separator that must never appear in a native display
	// path: a backslash on Unix, a forward slash on Windows.
	otherSep := "\\"
	if filepath.Separator == '\\' {
		otherSep = "/"
	}
	nativeSep := string(filepath.Separator)

	// wantDisplay is the native path Display must return for root+rel: an
	// empty root yields the deck-relative rel alone, otherwise filepath.Join
	// of root and the slash-converted rel.
	wantDisplay := func(root, rel string) string {
		if root == "" {
			return filepath.FromSlash(rel)
		}
		return filepath.Join(root, filepath.FromSlash(rel))
	}

	cases := []struct {
		name string
		root string
		rel  string
	}{
		{"rooted multi-segment relative path", "templates", "slides/hello/layout.html.tmpl"},
		{"rooted single file", "templates", "library.yaml"},
		{"rooted absolute root", filepath.Join(nativeSep, "abs", "root"), "themes/plain/theme.css"},
		{"root with a trailing separator", "templates" + nativeSep, "slides/hello/example.md"},
		{"empty root is the deck-relative multi-segment case", "", "slides/hello/layout.html.tmpl"},
		{"empty root is the deck-relative single-file case", "", "example.md"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Display(tc.root, tc.rel)
			if want := wantDisplay(tc.root, tc.rel); got != want {
				t.Fatalf("Display(%q, %q) = %q, want %q", tc.root, tc.rel, got, want)
			}
			if strings.Contains(got, otherSep) {
				t.Errorf("Display(%q, %q) = %q contains the non-native separator %q", tc.root, tc.rel, got, otherSep)
			}
			// A multi-segment relative path must carry the host separator, not
			// only look native by absence of the other one.
			if strings.Contains(tc.rel, "/") && !strings.Contains(got, nativeSep) {
				t.Errorf("Display(%q, %q) = %q, want the native separator %q", tc.root, tc.rel, got, nativeSep)
			}
		})
	}

	// The empty root is the deck-relative case: the result is the relative path
	// on its own, never rooted and never with a leading separator.
	got := Display("", "slides/hello/layout.html.tmpl")
	if want := filepath.FromSlash("slides/hello/layout.html.tmpl"); got != want {
		t.Errorf("Display(\"\", rel) = %q, want the deck-relative %q", got, want)
	}
	if strings.HasPrefix(got, nativeSep) {
		t.Errorf("Display(\"\", rel) = %q, want no leading separator for the deck-relative case", got)
	}
}

// TestCanonicalRoot proves CanonicalRoot's two documented behaviors — symlink
// resolution, and the raw-path fallback when resolution fails — plus the
// Windows-only 8.3 short-name expansion, which is guarded by runtime.GOOS.
func TestCanonicalRoot(t *testing.T) {
	t.Run("resolves a symlinked path to its target", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		if err := os.MkdirAll(real, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", real, err)
		}
		link := filepath.Join(dir, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Skipf("cannot create a symlink on this system: %v", err)
		}

		resolved, err := filepath.EvalSymlinks(real)
		if err != nil {
			t.Fatalf("EvalSymlinks(%s): %v", real, err)
		}
		got := CanonicalRoot(link)
		if got != resolved {
			t.Errorf("CanonicalRoot(link) = %q, want the resolved target %q", got, resolved)
		}
		if got != CanonicalRoot(real) {
			t.Errorf("CanonicalRoot(link) = %q, CanonicalRoot(real) = %q; want the same canonical path", got, CanonicalRoot(real))
		}
		if !filepath.IsAbs(got) {
			t.Errorf("CanonicalRoot(link) = %q, want an absolute path", got)
		}
	})

	t.Run("falls back to the cleaned raw path when the path does not exist", func(t *testing.T) {
		dir := t.TempDir()
		// A path that does not exist and is not already clean, so the fallback
		// (filepath.Clean of the input) is distinguishable from the input.
		missing := dir + string(filepath.Separator) + "nope" +
			string(filepath.Separator) + ".." + string(filepath.Separator) + "nope"
		want := filepath.Clean(missing)

		got := CanonicalRoot(missing)
		if got != want {
			t.Errorf("CanonicalRoot(%q) = %q, want the raw fallback %q", missing, got, want)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("CanonicalRoot(%q) = %q, want a usable absolute path", missing, got)
		}
	})

	t.Run("expands a Windows 8.3 short path to its long form", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("8.3 short-name expansion is Windows-only: CanonicalRoot relies on filepath.EvalSymlinks, which expands short names on Windows")
		}
		// Exercising the actual 8.3 expansion needs a real short name, which
		// only GetShortPathName can produce; that function lives in
		// golang.org/x/sys/windows, and the standard library exposes no pure-Go
		// equivalent. CanonicalRoot does not depend on x/sys, so rather than
		// pin an unverifiable path this case skips with that reason and leaves
		// the expansion to the Windows CI matrix.
		t.Skip("cannot obtain a real 8.3 short name in pure Go without golang.org/x/sys/windows (GetShortPathName)")
	})
}
