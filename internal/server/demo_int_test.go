package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/assets"
	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/render"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// demoProjectDir is the repository's non-EY demo project (examples/demo),
// relative to this package. It is a real project on disk: its own
// kalide.yaml, slides/, assets/ and templates/ library — never copied into a
// temp dir and never embedded into the binary (demo-project).
const demoProjectDir = "../../examples/demo"

// TestDemoProject proves examples/demo (commit b45b652) is a real,
// independently loadable project: its templates/ library loads via
// template.LoadLibrary, the whole deck validates clean through
// internal/validate.Validate, internal/render.RenderDeck succeeds against it,
// and its templates/media and templates/themes/<name> files are served by
// server.mediaHandler with an extension-derived Content-Type — all read
// straight off disk via os.DirFS, never from the embedded internal/assets
// tree. It reads the real filesystem, so it is skipped under -short.
func TestDemoProject(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test reads examples/demo from disk")
	}

	if _, err := os.Stat(demoProjectDir); err != nil {
		t.Fatalf("examples/demo not found at %s: %v", demoProjectDir, err)
	}

	fsys := os.DirFS(demoProjectDir)

	lib, err := template.LoadLibrary(fsys, template.TemplatesDir)
	if err != nil {
		t.Fatalf("template.LoadLibrary: %v", err)
	}
	reg, err := template.NewRegistryFromLibrary(lib)
	if err != nil {
		t.Fatalf("template.NewRegistryFromLibrary: %v", err)
	}
	themeReg, err := theme.LoadDir(fsys, template.TemplatesDir+"/"+template.ThemesDir)
	if err != nil {
		t.Fatalf("theme.LoadDir: %v", err)
	}

	t.Run("whole deck validates clean", func(t *testing.T) {
		if verr, invalid := validate.Validate(fsys, reg, themeReg); invalid {
			t.Fatalf("validate.Validate rejected examples/demo: %s", validate.Format(verr))
		}
	})

	t.Run("render.RenderDeck succeeds", func(t *testing.T) {
		cfg, err := deck.LoadConfig(fsys, deck.ConfigFile, themeReg)
		if err != nil {
			t.Fatalf("deck.LoadConfig: %v", err)
		}
		d, err := deck.LoadSlides(fsys, deck.SlidesDir)
		if err != nil {
			t.Fatalf("deck.LoadSlides: %v", err)
		}
		if len(d.Stacks) == 0 {
			t.Fatal("examples/demo has no slides")
		}

		var parsed []*slide.Slide
		for i := range d.Stacks {
			stack := &d.Stacks[i]
			ordered := append([]deck.Slide{stack.Slide}, stack.Vertical...)
			for _, s := range ordered {
				src, rerr := fs.ReadFile(fsys, s.Path)
				if rerr != nil {
					t.Fatalf("read %s: %v", s.Path, rerr)
				}
				ps, perr := slide.Parse(s.Path, src, reg)
				if perr != nil {
					t.Fatalf("slide.Parse(%s): %v", s.Path, perr)
				}
				parsed = append(parsed, ps)
			}
		}

		funcMap := template.LayoutFuncMap(lib.Media, MediaPath)
		page, err := render.RenderDeck(cfg, d, parsed, reg, themeReg, funcMap)
		if err != nil {
			t.Fatalf("render.RenderDeck: %v", err)
		}
		if !strings.Contains(string(page), "<!DOCTYPE html>") {
			t.Errorf("rendered page missing HTML5 doctype:\n%s", page)
		}
	})

	t.Run("mediaHandler serves templates/media and templates/themes from disk", func(t *testing.T) {
		handler := mediaHandler(lib, themeReg)
		ts := httptest.NewServer(handler)
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + MediaPath + "badge.svg")
		if err != nil {
			t.Fatalf("GET media: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("media status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "svg") {
			t.Errorf("media Content-Type = %q, want it to mention svg", ct)
		}

		themeResp, err := http.Get(ts.URL + ThemesPath + "demo/theme.css")
		if err != nil {
			t.Fatalf("GET theme: %v", err)
		}
		defer themeResp.Body.Close()
		if themeResp.StatusCode != http.StatusOK {
			t.Fatalf("theme status = %d, want 200", themeResp.StatusCode)
		}
		if ct := themeResp.Header.Get("Content-Type"); !strings.Contains(ct, "css") {
			t.Errorf("theme Content-Type = %q, want it to mention css", ct)
		}
	})

	t.Run("examples/demo is not embedded in the binary", func(t *testing.T) {
		// examples/demo is read from disk via os.DirFS above, never from
		// internal/assets' embed.FS. Confirm the embedded tree carries no
		// "examples" sub-tree and none of its content-addressed markers.
		entries, err := fs.ReadDir(assets.FS, ".")
		if err != nil {
			t.Fatalf("fs.ReadDir(assets.FS, \".\"): %v", err)
		}
		for _, e := range entries {
			if e.Name() == "examples" {
				t.Fatalf("embedded assets.FS unexpectedly contains an %q entry", e.Name())
			}
		}
		if _, err := fs.Stat(assets.FS, "examples/demo/kalide.yaml"); err == nil {
			t.Fatal("embedded assets.FS unexpectedly resolves examples/demo/kalide.yaml")
		}
		if _, err := fs.Stat(assets.FS, "examples/demo/templates/media/badge.svg"); err == nil {
			t.Fatal("embedded assets.FS unexpectedly resolves examples/demo/templates/media/badge.svg")
		}
	})
}
