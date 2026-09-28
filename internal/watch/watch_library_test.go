// This file is the integration-test deliverable for plan.phase-02.task-6:
// watching the resolved external template-library root
// (external-template-library). It proves watch.Watch on a deck whose
// kalide.yaml sets `templates: ../shared-lib` (and has no local templates/)
// registers the resolved external root and emits one debounced event per burst
// for an edit under it — and for a kalide.yaml change — reporting the changed
// path relative to the deck root (which, for the external root, begins with
// "../"), while changes outside the deck and the resolved library are ignored.
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
