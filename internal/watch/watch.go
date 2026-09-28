// Package watch watches a deck directory for filesystem changes and reports
// them as a debounced stream of events.
//
// It is the file-watching half of live reload (domain.service.watch-deck): the
// watched paths are the deck's slides/ subtree, its assets/ subtree, its
// resolved template-library root's structure — the root itself, its
// library.yaml and the slides/, sections/, themes/ and media/ subtrees, but no
// other top-level entry in the root (the configured `templates:` path, else the
// local templates/; see external-template-library) — and its kalide.yaml. The
// watcher never reads, validates or renders a deck; it only reports that
// something under those paths changed. internal/server consumes the events to
// re-validate and re-render the deck and to push a reload to the open browser
// (requirements.requirement.live-reload).
package watch

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/template"
)

// AssetsDir is the fixed name of the deck's asset directory at the root of a
// deck, alongside deck.SlidesDir ("slides") and deck.ConfigFile
// ("kalide.yaml"). It is watched recursively so an edit to any served asset
// triggers a reload. Unlike slides/, it is optional in a deck, so a missing
// assets/ directory is not an error.
const AssetsDir = "assets"

// TemplatesDir is the fixed name of the local template library directory at
// the root of a deck, alongside deck.SlidesDir and AssetsDir. It aliases
// template.TemplatesDir and is the fallback library root when kalide.yaml has
// no `templates:` key; otherwise the configured path is the resolved root
// (external-template-library). The resolved root is watched recursively so an
// edit anywhere in the library triggers a reload. Like assets/, a missing
// local templates/ directory is not an error at startup: the root watch picks
// it up if it is created later.
const TemplatesDir = template.TemplatesDir

// libraryWatchDirs are the template-library top-level directories whose
// contents the watcher follows: slides/, sections/, themes/ and media/
// (templates-dir-layout). Together with the library root's library.yaml file
// they are the library structure; every other top-level entry in the library
// root — a `.git/` directory, a README, the root AGENTS.md, an editor lockfile
// — is not part of the library and is ignored, so a git operation in a
// git-hosted library never triggers a reload.
var libraryWatchDirs = []string{
	template.SlidesDir,
	template.SectionsDir,
	template.ThemesDir,
	template.MediaDir,
}

// mutate is the set of fsnotify operations that count as a change to the deck.
// A pure Chmod (the only remaining operation) does not change served content
// and is ignored so attribute-only events cannot cause a spurious reload.
const mutate = fsnotify.Create | fsnotify.Write | fsnotify.Remove | fsnotify.Rename

// Event is one debounced batch of filesystem changes to a deck.
//
// Watch emits exactly one Event per burst of changes: every relevant
// filesystem event observed within debounce of the previous one is coalesced
// into a single Event, which is delivered once the filesystem has been quiet
// for debounce.
//
// Paths holds the changed paths that made up the burst, relative to the watched
// root and slash-separated, sorted and de-duplicated. It is informational: a
// path may name something that has already gone again by the time the Event is
// delivered (a file created and removed within one burst), so a consumer should
// treat an Event as "the deck may have changed" and re-validate from scratch
// rather than act on the paths individually.
type Event struct {
	// Paths are the deck-relative, slash-separated paths whose changes made up
	// this burst, sorted and de-duplicated.
	Paths []string
}

// Watch starts watching the deck rooted at root and returns a channel of
// debounced change events plus a function that stops the watch.
//
// Watched paths:
//
//   - the slides/ subtree (deck.SlidesDir) recursively;
//   - the assets/ subtree (AssetsDir) recursively;
//   - the resolved template-library root's structure — the configured
//     `templates:` path in kalide.yaml (relative to root, or absolute; it may
//     lie outside root), else the local templates/ (TemplatesDir) — namely the
//     root itself (non-recursively), the top-level library.yaml and the
//     slides/, sections/, themes/ and media/ subtrees; every other top-level
//     entry in the root (e.g. .git/, README.md, AGENTS.md) is ignored; and
//   - the kalide.yaml file (deck.ConfigFile) at the root.
//
// fsnotify is not recursive, so Watch walks slides/, assets/ and each of the
// library structure directories at startup and registers every directory it
// finds; the library root itself is watched non-recursively so no unrelated
// entry is followed. A directory created later is picked up from the create
// event that the parent's watch reports and has its subtree added; a directory
// removed or renamed has its watch (and its descendants') dropped. A missing
// slides/, assets/ or library directory is not an error: the root watch sees a
// local one if it is created later.
//
// Debounce: each relevant event resets a timer, and a single Event is sent only
// after no relevant event has arrived for debounce (one event per burst). No
// change is ever dropped: the channel has a one-event buffer, and once that is
// full the watcher applies backpressure by waiting until a consumer receives
// before it can deliver the next burst.
//
// The returned stop function closes the watcher, waits for the event loop to
// finish and returns any error from closing the underlying fsnotify watcher. It
// is safe to call more than once. After it returns, the event channel is closed,
// so a consumer ranging over it terminates.
//
// Watch returns an error only if the underlying watcher cannot be created or
// root cannot be watched. The caller owns the returned channel and must call
// stop to release the watcher's resources.
func Watch(root string, debounce time.Duration) (<-chan Event, func() error, error) {
	if debounce < 0 {
		debounce = 0
	}

	// Resolve the root to an absolute path so the paths fsnotify reports can be
	// made relative to it on every backend: kqueue reports a watch's path as it
	// was registered, while the Linux and Windows backends always report
	// absolute paths.
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, fmt.Errorf("watch %s: %w", root, err)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, fmt.Errorf("watch %s: %w", root, err)
	}

	// Resolve the deck's template-library root from kalide.yaml (the
	// configured `templates:` path, else the local templates/) so the resolved
	// root — possibly an external directory outside the deck root — is watched
	// too (external-template-library). A missing or unresolvable library is
	// not an error: the deck's slides/, assets/ and kalide.yaml are still
	// watched, and the resolved root is picked up later if it appears.
	library := libraryRoot(root)

	w := &watcher{
		root:     root,
		library:  library,
		fsw:      fsw,
		debounce: debounce,
		watched:  make(map[string]struct{}),
		out:      make(chan Event, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}

	// The root is watched non-recursively. kalide.yaml is observed through it
	// rather than by watching the file directly: editors usually save by
	// writing a temporary file and renaming it over kalide.yaml, which replaces
	// the inode and would silently lose a direct file watch. The root watch
	// also notices a slides/ or assets/ directory that appears after startup.
	if err := w.add(root); err != nil {
		_ = fsw.Close()
		return nil, nil, fmt.Errorf("watch %s: %w", root, err)
	}
	w.addTree(filepath.Join(root, deck.SlidesDir))
	w.addTree(filepath.Join(root, AssetsDir))
	if library != "" {
		// Only the library's own structure is followed: the root itself
		// (non-recursively) and its slides/, sections/, themes/ and media/
		// subtrees. Every other top-level entry in the library root — a .git/
		// directory, a README, the root AGENTS.md, an editor lockfile — is
		// not part of the library and is ignored.
		_ = w.add(library)
		for _, dir := range libraryWatchDirs {
			w.addTree(filepath.Join(library, dir))
		}
	}

	go w.loop()

	var once sync.Once
	var closeErr error
	stop := func() error {
		once.Do(func() {
			close(w.stop)
			<-w.done
			closeErr = fsw.Close()
		})
		return closeErr
	}
	return w.out, stop, nil
}

// watcher owns one fsnotify watcher and the debounce state. After construction
// its watched set and debounce state are only touched by the single event-loop
// goroutine started by Watch.
type watcher struct {
	root string

	// library is the deck's resolved template-library root (external-template-
	// library): the configured `templates:` path resolved against root, or the
	// local templates/ when the key is absent. It may be outside root (begins
	// with ".." relative to it). It is "" when no root could be resolved; a
	// change to its structure (the root, its library.yaml, or anything at or
	// beneath slides/, sections/, themes/ or media/) is a deck change even when
	// it lies outside root.
	library string

	fsw      *fsnotify.Watcher
	debounce time.Duration

	// watched is the set of directories (absolute paths) with an active watch,
	// so a removed subtree can be dropped and a re-add can be skipped.
	watched map[string]struct{}

	out  chan Event
	stop chan struct{}
	done chan struct{}
}

// add registers one directory (an absolute path) with the underlying watcher,
// remembering it. It is a no-op for a directory already watched.
func (w *watcher) add(dir string) error {
	if _, ok := w.watched[dir]; ok {
		return nil
	}
	if err := w.fsw.Add(dir); err != nil {
		return err
	}
	w.watched[dir] = struct{}{}
	return nil
}

// addTree registers every directory in the subtree rooted at dir. It is
// best-effort: a subtree that does not exist yet, or that disappears while it
// is being walked, adds nothing rather than failing the watch.
func (w *watcher) addTree(dir string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = w.add(p)
		}
		return nil
	})
}

// addRelevantTree registers a newly created directory subtree. For the resolved
// library root itself it registers only the library's structure — the root
// (non-recursively) and its slides/, sections/, themes/ and media/ subtrees —
// never an unrelated entry such as .git/ (templates-dir-layout). Any other
// relevant directory is registered recursively.
func (w *watcher) addRelevantTree(dir string) {
	if w.library != "" && filepath.Clean(dir) == w.library {
		_ = w.add(dir)
		for _, d := range libraryWatchDirs {
			w.addTree(filepath.Join(dir, d))
		}
		return
	}
	w.addTree(dir)
}

// dropTree forgets every watched directory at or beneath dir and removes the
// underlying watches. fsnotify may already have dropped a deleted directory;
// the resulting Remove error is safe to ignore.
func (w *watcher) dropTree(dir string) {
	prefix := dir + string(filepath.Separator)
	for p := range w.watched {
		if p == dir || strings.HasPrefix(p, prefix) {
			_ = w.fsw.Remove(p)
			delete(w.watched, p)
		}
	}
}

// rel reports whether ev is relevant to the deck and, if so, its path relative
// to the root as a slash-separated string. Relevant paths are kalide.yaml at
// the root, anything at or beneath slides/ or assets/, and the resolved
// library's structure (external-template-library) — the root, its library.yaml
// or anything at or beneath slides/, sections/, themes/ or media/ — including
// an external root outside the deck, whose reported path then begins with
// "../". A path outside the root's watched subtrees and the library's
// structure is ignored.
func (w *watcher) rel(ev fsnotify.Event) (string, bool) {
	r, err := filepath.Rel(w.root, ev.Name)
	if err != nil {
		return "", false
	}
	r = filepath.ToSlash(r)
	if r == "." {
		return "", false
	}
	// The resolved library root may sit outside the deck root (e.g.
	// `templates: ../shared-lib`), where r is "../shared-lib/…": treat it as a
	// deck change and report the same stable deck-relative path.
	if w.libraryRelevant(ev.Name) {
		return r, true
	}
	if r == ".." || strings.HasPrefix(r, "../") {
		return "", false
	}
	switch {
	case r == deck.ConfigFile:
		return r, true
	case r == deck.SlidesDir, strings.HasPrefix(r, deck.SlidesDir+"/"):
		return r, true
	case r == AssetsDir, strings.HasPrefix(r, AssetsDir+"/"):
		return r, true
	default:
		return "", false
	}
}

// libraryRelevant reports whether name is part of the resolved template
// library's structure: the library root itself, its library.yaml, or anything
// at or beneath slides/, sections/, themes/ or media/. Every other top-level
// entry in the library root (e.g. .git/, README.md, AGENTS.md) is not part of
// the library and is ignored, so it never triggers a reload. It is false when
// no library root was resolved.
func (w *watcher) libraryRelevant(name string) bool {
	if w.library == "" {
		return false
	}
	name = filepath.Clean(name)
	if name == w.library {
		return true
	}
	if name == filepath.Join(w.library, template.LibraryFile) {
		return true
	}
	for _, dir := range libraryWatchDirs {
		if atOrUnder(filepath.Join(w.library, dir), name) {
			return true
		}
	}
	return false
}

// atOrUnder reports whether name is dir itself or a path beneath dir. Both are
// expected to be absolute, cleaned OS paths.
func atOrUnder(dir, name string) bool {
	rel, err := filepath.Rel(dir, name)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// libraryRoot returns the deck's resolved template-library root as an absolute
// OS path, or "" when none can be resolved. configured is the raw `templates:`
// value read from the deck's kalide.yaml; it resolves through the template
// loader's single resolution point (external-template-library). Resolution
// requires the directory to exist, but a missing/unresolvable library must not
// fail the watch: when the key is absent, the local templates/ path is still
// returned so a directory created there after startup is picked up by the root
// watch; when a configured path does not resolve, "" is returned and only the
// deck's own paths are watched.
func libraryRoot(deckRoot string) string {
	configured := configuredTemplates(deckRoot)
	if resolved, err := template.ResolveLibraryRoot(deckRoot, configured); err == nil {
		return resolved
	}
	if configured == "" {
		return filepath.Join(deckRoot, template.TemplatesDir)
	}
	return ""
}

// configuredTemplates returns the deck's raw top-level `templates:` value as
// written in kalide.yaml, or "" when the file cannot be read, the key is absent
// or empty, or the config cannot be decoded. It mirrors internal/server's
// reader: reading the raw key is what lets the library root be resolved before
// a theme registry exists (deck.LoadConfig needs one, and the registry itself
// comes from the not-yet-resolved library).
func configuredTemplates(deckRoot string) string {
	data, err := os.ReadFile(filepath.Join(deckRoot, deck.ConfigFile))
	if err != nil {
		return ""
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ""
	}
	configured, _ := raw["templates"].(string)
	return configured
}

// loop is the single event-loop goroutine: it reads fsnotify events, keeps the
// watched set in step with directories appearing and disappearing, coalesces
// changes into bursts and delivers one Event per burst.
func (w *watcher) loop() {
	defer close(w.done)
	defer close(w.out)

	var (
		timer *time.Timer
		tick  <-chan time.Time
	)

	// arm starts or restarts the quiet-period timer.
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(w.debounce)
			tick = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(w.debounce)
	}

	pending := make(map[string]struct{})

	// flush delivers the accumulated burst, if any, as one Event, waiting for
	// a consumer if the buffer is full until the watch is stopped.
	flush := func() {
		timer = nil
		tick = nil
		if len(pending) == 0 {
			return
		}
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		pending = make(map[string]struct{})
		select {
		case w.out <- Event{Paths: paths}:
		case <-w.stop:
		}
	}

	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if ev.Op&mutate == 0 {
				continue
			}
			r, relevant := w.rel(ev)
			if !relevant {
				continue
			}
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					w.addRelevantTree(ev.Name)
				}
			}
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				w.dropTree(ev.Name)
			}
			pending[r] = struct{}{}
			arm()

		case _, ok := <-w.fsw.Errors:
			// fsnotify errors (for example an overrun) do not carry a path and
			// cannot be acted on here; the watcher stays up. Errors surfaced by
			// fsnotify are advisory and the deck is re-read on every event.
			if !ok {
				return
			}

		case <-tick:
			flush()

		case <-w.stop:
			return
		}
	}
}
