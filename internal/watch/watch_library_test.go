// This file is the integration-test deliverable for plan.phase-02.task-6
// (external-template-library) and plan.phase-04.task-16 (layered-template-library,
// live-reload, templates-live-reload). It proves watch.Watch on a deck whose
// kalide.yaml sets `templates:` to an external library root watches that root,
// and — when the deck also carries its own deck-root templates/ layer — watches
// BOTH layer roots, so an edit under either layer fires one debounced event with
// the changed path relative to the deck root (the external root's paths begin
// with "../"). A file outside both layers' structure stays irrelevant, a layer
// root or structure directory created after startup is registered and its later
// edits fire, and a single-layer deck (local only, external only) behaves as
// before.
//
// It uses a real temporary directory and a real fsnotify watcher, so it is an
// integration test and is skipped under -short. It reuses the helpers declared
// in watch_test.go (testDebounce, waitEvent, assertQuiet, expectPath,
// expectPathPrefix, mustMkdir, mustWrite).
package watch_test

import (
	"path/filepath"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/watch"
)

// externalLibraryEventPrefix is the deck-relative, slash-separated prefix the
// watcher reports for a change under the resolved external library root: the
// library sits at ../shared-lib relative to the deck root, so the reported
// Event.Paths begin with this.
const externalLibraryEventPrefix = "../shared-lib"

// localLibraryEventPrefix is the deck-relative, slash-separated prefix the
// watcher reports for a change under the deck-root templates/ local layer.
const localLibraryEventPrefix = "templates"

// TestWatchExternalLibraryIntegration covers the resolved external library root
// the watcher registers from `templates: ../shared-lib`.
func TestWatchExternalLibraryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test uses a real filesystem watcher")
	}

	t.Run("edit under the resolved external root yields one debounced event", func(t *testing.T) {
		deckRoot, libRoot, events := newExternalLibraryWatchDeck(t)
		_ = deckRoot

		// Overwrite an existing library file: a Write event under the
		// external root, which must be recognised as a deck change and
		// reported relative to the deck root (../shared-lib/…).
		mustWrite(t, filepath.Join(libRoot, "slides", "hello", "template.yaml"),
			"description: edited external hello\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, externalLibraryEventPrefix+"/slides/hello/template.yaml")
		assertQuiet(t, events)
	})

	t.Run("new file under the external root is reported relative to the deck", func(t *testing.T) {
		_, libRoot, events := newExternalLibraryWatchDeck(t)

		mustWrite(t, filepath.Join(libRoot, "themes", "default", "extra.css"), "body {}\n")

		ev := waitEvent(t, events)
		expectPathPrefix(t, ev, externalLibraryEventPrefix+"/themes/default")
		assertQuiet(t, events)
	})

	t.Run("kalide.yaml change stays relevant", func(t *testing.T) {
		deckRoot, _, events := newExternalLibraryWatchDeck(t)

		// A change to kalide.yaml must still be reported, so a retargeted
		// `templates:` path is noticed.
		mustWrite(t, filepath.Join(deckRoot, deck.ConfigFile),
			"title: Edited\ntemplates: ../shared-lib\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, deck.ConfigFile)
		assertQuiet(t, events)
	})

	t.Run("changes outside the deck and the resolved library are ignored", func(t *testing.T) {
		deckRoot, _, events := newExternalLibraryWatchDeck(t)
		parent := filepath.Dir(deckRoot)

		// A sibling of both the deck and the resolved library, and a plain
		// file in the shared parent directory: neither is watched.
		mustMkdir(t, filepath.Join(parent, "elsewhere"))
		mustWrite(t, filepath.Join(parent, "elsewhere", "file.txt"), "x\n")
		mustWrite(t, filepath.Join(parent, "outside.txt"), "x\n")

		assertQuiet(t, events)
	})

	t.Run("a change to the library.yaml under the resolved root is reported", func(t *testing.T) {
		_, libRoot, events := newExternalLibraryWatchDeck(t)

		// library.yaml is part of the library structure at the root of the
		// resolved external root, so an edit to it must be reported.
		mustWrite(t, filepath.Join(libRoot, "library.yaml"),
			"name: shared-lib\nformat: 1\nupdated: true\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, externalLibraryEventPrefix+"/library.yaml")
		assertQuiet(t, events)
	})

	t.Run("a non-library top-level entry in the resolved root is ignored", func(t *testing.T) {
		_, libRoot, events := newExternalLibraryWatchDeck(t)

		// A git repository and a README at the top level of the resolved
		// library root are not part of the library structure: the watcher
		// follows only library.yaml and the slides/, sections/, themes/ and
		// media/ subtrees, so neither may produce an event.
		mustMkdir(t, filepath.Join(libRoot, ".git"))
		mustWrite(t, filepath.Join(libRoot, ".git", "config"), "[core]\n\trepositoryformatversion = 0\n")
		mustWrite(t, filepath.Join(libRoot, "README.md"), "# shared-lib\n")

		assertQuiet(t, events)
	})

	t.Run("a plain file directly in the resolved root is ignored", func(t *testing.T) {
		_, libRoot, events := newExternalLibraryWatchDeck(t)

		// Relevance is not a blanket "anything under the root": a plain file
		// sitting directly in the library root, neither library.yaml nor a
		// structure directory, is not part of the library and is ignored.
		mustWrite(t, filepath.Join(libRoot, "notes.txt"), "scratch\n")

		assertQuiet(t, events)
	})
}

// TestWatchLayeredLibraryIntegration covers a deck that draws from two library
// layers at once: an external `templates: ../shared-lib` root and its own
// deck-root templates/ layer. Both roots are resolved at startup, so the
// watcher must register each root's structure and fire on an edit under either
// one, while a path outside both layers' structure stays irrelevant.
func TestWatchLayeredLibraryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test uses a real filesystem watcher")
	}

	t.Run("edits under the external layer root fire naming the changed files", func(t *testing.T) {
		_, libRoot, _, events := newLayeredWatchDeck(t)

		// A slide manifest, its layout, a theme stylesheet and a media file,
		// written as one burst under the external root: all are library
		// structure, so the single debounced event must name each of them
		// relative to the deck root (../shared-lib/…).
		mustWrite(t, filepath.Join(libRoot, "slides", "hello", "template.yaml"),
			"description: edited external hello\n")
		mustWrite(t, filepath.Join(libRoot, "slides", "hello", "layout.html.tmpl"),
			"<html>edited</html>\n")
		mustWrite(t, filepath.Join(libRoot, "themes", "default", "theme.css"),
			"body { margin: 1; }\n")
		mustWrite(t, filepath.Join(libRoot, "media", "hello.svg"), "<svg>edited</svg>\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, externalLibraryEventPrefix+"/slides/hello/template.yaml")
		expectPath(t, ev, externalLibraryEventPrefix+"/slides/hello/layout.html.tmpl")
		expectPath(t, ev, externalLibraryEventPrefix+"/themes/default/theme.css")
		expectPath(t, ev, externalLibraryEventPrefix+"/media/hello.svg")
		assertQuiet(t, events)
	})

	t.Run("edits under the deck-root local layer fire naming the changed files", func(t *testing.T) {
		_, _, localRoot, events := newLayeredWatchDeck(t)

		// The same four file kinds under the deck-root templates/ layer: the
		// watcher must follow this second layer exactly like the external
		// one, reporting paths relative to the deck root (templates/…).
		mustWrite(t, filepath.Join(localRoot, "slides", "local", "template.yaml"),
			"description: edited local\n")
		mustWrite(t, filepath.Join(localRoot, "slides", "local", "layout.html.tmpl"),
			"<html>edited</html>\n")
		mustWrite(t, filepath.Join(localRoot, "themes", "localth", "theme.css"),
			"body { margin: 1; }\n")
		mustWrite(t, filepath.Join(localRoot, "media", "local.svg"), "<svg>edited</svg>\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, localLibraryEventPrefix+"/slides/local/template.yaml")
		expectPath(t, ev, localLibraryEventPrefix+"/slides/local/layout.html.tmpl")
		expectPath(t, ev, localLibraryEventPrefix+"/themes/localth/theme.css")
		expectPath(t, ev, localLibraryEventPrefix+"/media/local.svg")
		assertQuiet(t, events)
	})

	t.Run("a file outside both layers' structure stays irrelevant", func(t *testing.T) {
		deckRoot, libRoot, localRoot, events := newLayeredWatchDeck(t)
		parent := filepath.Dir(deckRoot)

		// Plain files directly in either layer root are not part of the
		// library; a non-structure directory under the local layer is not
		// followed either; and neither is a deck-root file or a sibling of
		// the deck. With both layers resolved, a change that touches no
		// layer's structure must stay quiet.
		mustWrite(t, filepath.Join(libRoot, "notes.txt"), "scratch\n")
		mustWrite(t, filepath.Join(localRoot, "README.md"), "# local\n")
		mustMkdir(t, filepath.Join(localRoot, "scratch"))
		mustWrite(t, filepath.Join(localRoot, "scratch", "file.txt"), "x\n")
		mustWrite(t, filepath.Join(deckRoot, "README.md"), "# deck\n")
		mustWrite(t, filepath.Join(parent, "outside.txt"), "x\n")

		assertQuiet(t, events)
	})
}

// TestWatchLayeredLibraryLateRootIntegration covers a library layer root and a
// structure directory that appear only after Watch has started: the root watch
// (or a resolved root's watch) must register the new subtree so its later edits
// fire without a restart.
func TestWatchLayeredLibraryLateRootIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test uses a real filesystem watcher")
	}

	t.Run("deck-root templates/ created after startup is registered and its edits fire", func(t *testing.T) {
		// The deck configures no `templates:` key and has no templates/ at
		// startup, so no layer resolves and the deck-root templates/ path is
		// retained for the root watch to pick up. Creating the local layer
		// root later must register it and its structure.
		root, events := newWatchDeck(t)

		mustMkdir(t, filepath.Join(root, watch.TemplatesDir))
		ev := waitEvent(t, events)
		expectPathPrefix(t, ev, localLibraryEventPrefix)

		// The themes/ directory itself is the first thing created under the
		// new layer root; its own create event (and possibly the nested late/
		// one) registers the subtree. Assert on the themes/ prefix, not the
		// nested directory: whether late/ is named in this burst depends on
		// whether its parent was already watched when late/ appeared.
		mustMkdir(t, filepath.Join(root, watch.TemplatesDir, "themes", "late"))
		ev = waitEvent(t, events)
		expectPathPrefix(t, ev, localLibraryEventPrefix+"/themes")

		mustWrite(t, filepath.Join(root, watch.TemplatesDir, "themes", "late", "theme.css"), "body {}\n")
		ev = waitEvent(t, events)
		expectPath(t, ev, localLibraryEventPrefix+"/themes/late/theme.css")
		assertQuiet(t, events)
	})

	t.Run("a structure dir created after startup under the external layer root is registered", func(t *testing.T) {
		// The external root resolves at startup, but its media/ directory does
		// not exist yet: creating it must be treated as library structure and
		// followed, so a file written inside it later fires.
		_, libRoot, events := newExternalLibraryWatchDeck(t)

		mustMkdir(t, filepath.Join(libRoot, "media"))
		ev := waitEvent(t, events)
		expectPathPrefix(t, ev, externalLibraryEventPrefix+"/media")

		mustWrite(t, filepath.Join(libRoot, "media", "logo.svg"), "<svg/>\n")
		ev = waitEvent(t, events)
		expectPath(t, ev, externalLibraryEventPrefix+"/media/logo.svg")
		assertQuiet(t, events)
	})

	t.Run("a structure dir created after startup under the deck-root local layer is registered", func(t *testing.T) {
		// Both layers resolve at startup; a new slide directory appearing under
		// the local layer's slides/ must be followed so its manifest edit fires.
		_, _, localRoot, events := newLayeredWatchDeck(t)

		sub := filepath.Join(localRoot, "slides", "fresh")
		mustMkdir(t, sub)
		ev := waitEvent(t, events)
		expectPathPrefix(t, ev, localLibraryEventPrefix+"/slides/fresh")

		mustWrite(t, filepath.Join(sub, "template.yaml"), "description: fresh\n")
		ev = waitEvent(t, events)
		expectPath(t, ev, localLibraryEventPrefix+"/slides/fresh/template.yaml")
		assertQuiet(t, events)
	})
}

// TestWatchSingleLayerLibraryIntegration covers the single-layer decks, whose
// watcher behaviour is unchanged: a deck with no `templates:` key and a
// deck-root templates/ layer watches that local layer, and a deck with only an
// external `templates:` root (TestWatchExternalLibraryIntegration) watches that
// external one.
func TestWatchSingleLayerLibraryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test uses a real filesystem watcher")
	}

	t.Run("local-only deck watches the deck-root templates layer", func(t *testing.T) {
		_, localRoot, events := newLocalOnlyWatchDeck(t)

		mustWrite(t, filepath.Join(localRoot, "slides", "local", "template.yaml"),
			"description: edited local\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, localLibraryEventPrefix+"/slides/local/template.yaml")
		assertQuiet(t, events)
	})
}

// newExternalLibraryWatchDeck builds a real temporary deck at <parent>/deck
// whose kalide.yaml sets `templates: ../shared-lib` (and which has no local
// templates/), a valid external library at the sibling <parent>/shared-lib,
// and starts a real Watch on the deck at testDebounce. It stops the watch when
// the subtest ends and returns the deck root, the external library root and
// the event channel.
func newExternalLibraryWatchDeck(t *testing.T) (deckRoot, libRoot string, events <-chan watch.Event) {
	t.Helper()
	parent := t.TempDir()
	deckRoot = filepath.Join(parent, "deck")
	libRoot = filepath.Join(parent, "shared-lib")

	// The deck: slides/, assets/ and a kalide.yaml pointing at the external
	// library, with no local templates/ directory at all.
	mustMkdir(t, filepath.Join(deckRoot, deck.SlidesDir))
	mustMkdir(t, filepath.Join(deckRoot, watch.AssetsDir))
	mustWrite(t, filepath.Join(deckRoot, deck.SlidesDir, "1-intro.md"), "# One\n")
	mustWrite(t, filepath.Join(deckRoot, watch.AssetsDir, "logo.txt"), "logo\n")
	mustWrite(t, filepath.Join(deckRoot, deck.ConfigFile), "title: D\ntemplates: ../shared-lib\n")

	// The external library: a real, valid library at a sibling of the deck.
	mustMkdir(t, filepath.Join(libRoot, "slides", "hello"))
	mustMkdir(t, filepath.Join(libRoot, "themes", "default"))
	mustWrite(t, filepath.Join(libRoot, "library.yaml"), "name: shared-lib\nformat: 1\n")
	mustWrite(t, filepath.Join(libRoot, "slides", "hello", "template.yaml"),
		"description: external hello\n")
	mustWrite(t, filepath.Join(libRoot, "themes", "default", "theme.css"), "body { margin: 0; }\n")

	events, stop, err := watch.Watch(deckRoot, testDebounce)
	if err != nil {
		t.Fatalf("Watch(%s): %v", deckRoot, err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	return deckRoot, libRoot, events
}

// newLayeredWatchDeck builds a real temporary deck at <parent>/deck whose
// kalide.yaml sets `templates: ../shared-lib` AND which carries its own
// deck-root templates/ layer, so two layers resolve at startup: the external
// library at the sibling <parent>/shared-lib and the local templates/. Both
// layers use distinct names so they never collide. It starts a real Watch at
// testDebounce, stops it when the subtest ends, and returns the deck root, the
// external library root, the local layer root and the event channel.
func newLayeredWatchDeck(t *testing.T) (deckRoot, libRoot, localRoot string, events <-chan watch.Event) {
	t.Helper()
	parent := t.TempDir()
	deckRoot = filepath.Join(parent, "deck")
	libRoot = filepath.Join(parent, "shared-lib")
	localRoot = filepath.Join(deckRoot, watch.TemplatesDir)

	// The deck: slides/, assets/ and a kalide.yaml pointing at the external
	// library, plus its own deck-root templates/ local layer.
	mustMkdir(t, filepath.Join(deckRoot, deck.SlidesDir))
	mustMkdir(t, filepath.Join(deckRoot, watch.AssetsDir))
	mustWrite(t, filepath.Join(deckRoot, deck.SlidesDir, "1-intro.md"), "# One\n")
	mustWrite(t, filepath.Join(deckRoot, watch.AssetsDir, "logo.txt"), "logo\n")
	mustWrite(t, filepath.Join(deckRoot, deck.ConfigFile), "title: D\ntemplates: ../shared-lib\n")

	// The external layer: a real library at a sibling of the deck.
	writeLibraryLayer(t, libRoot, "shared-lib", "hello", "default")
	// The local layer: the deck-root templates/ directory.
	writeLibraryLayer(t, localRoot, "local", "local", "localth")

	events, stop, err := watch.Watch(deckRoot, testDebounce)
	if err != nil {
		t.Fatalf("Watch(%s): %v", deckRoot, err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	return deckRoot, libRoot, localRoot, events
}

// newLocalOnlyWatchDeck builds a real temporary deck at <parent>/deck whose
// kalide.yaml configures no `templates:` key and which carries a deck-root
// templates/ local layer, plus slides/, assets/ and a kalide.yaml. It starts a
// real Watch at testDebounce, stops it when the subtest ends, and returns the
// deck root, the local layer root and the event channel.
func newLocalOnlyWatchDeck(t *testing.T) (deckRoot, localRoot string, events <-chan watch.Event) {
	t.Helper()
	parent := t.TempDir()
	deckRoot = filepath.Join(parent, "deck")
	localRoot = filepath.Join(deckRoot, watch.TemplatesDir)

	mustMkdir(t, filepath.Join(deckRoot, deck.SlidesDir))
	mustMkdir(t, filepath.Join(deckRoot, watch.AssetsDir))
	mustWrite(t, filepath.Join(deckRoot, deck.SlidesDir, "1-intro.md"), "# One\n")
	mustWrite(t, filepath.Join(deckRoot, watch.AssetsDir, "logo.txt"), "logo\n")
	mustWrite(t, filepath.Join(deckRoot, deck.ConfigFile), "title: D\n")

	writeLibraryLayer(t, localRoot, "local", "local", "localth")

	events, stop, err := watch.Watch(deckRoot, testDebounce)
	if err != nil {
		t.Fatalf("Watch(%s): %v", deckRoot, err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	return deckRoot, localRoot, events
}

// writeLibraryLayer populates dir as a template-library layer: its library.yaml
// plus one slide (a manifest and a layout), one section, one theme and one
// media file, so the watcher has a real library structure to follow under
// slides/, sections/, themes/ and media/.
func writeLibraryLayer(t *testing.T, dir, name, slide, theme string) {
	t.Helper()
	mustMkdir(t, filepath.Join(dir, "slides", slide))
	mustMkdir(t, filepath.Join(dir, "sections", slide+"-section"))
	mustMkdir(t, filepath.Join(dir, "themes", theme))
	mustMkdir(t, filepath.Join(dir, "media"))
	mustWrite(t, filepath.Join(dir, "library.yaml"), "name: "+name+"\nformat: 1\n")
	mustWrite(t, filepath.Join(dir, "slides", slide, "template.yaml"), "description: "+slide+"\n")
	mustWrite(t, filepath.Join(dir, "slides", slide, "layout.html.tmpl"), "<html>"+slide+"</html>\n")
	mustWrite(t, filepath.Join(dir, "sections", slide+"-section", "template.yaml"), "description: section\n")
	mustWrite(t, filepath.Join(dir, "themes", theme, "theme.css"), "body { margin: 0; }\n")
	mustWrite(t, filepath.Join(dir, "media", slide+".svg"), "<svg>"+slide+"</svg>\n")
}
