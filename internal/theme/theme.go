// Package theme implements the project theme registry.
//
// A theme is a named set of CSS design tokens (colours, fonts, spacing) plus
// the asset files that go with it. Themes are loaded from a project's
// templates/themes/<name> directory (theme.LoadDir); the package has no
// compiled-in theme of its own.
//
// The registry exists so a project's themes can be added by registration
// alone, without touching slide templates or the renderer (theme-selection).
// Deck configuration resolves its optional `theme` key through
// Registry.Lookup, which defaults to "default" when omitted (that name must
// then be present in the loaded registry); an unknown name yields a
// structured error naming the available themes and a closest-match "did you
// mean …?" suggestion from internal/suggest (closest-match-suggestions).
package theme

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/really-knows-ai/kalide/internal/suggest"
)

// DefaultName is the name deck configuration resolves the `theme` key to when
// it is omitted; the loaded project registry must have a theme with this name
// for that fallback to succeed.
const DefaultName = "default"

// Theme is one named presentation theme.
type Theme struct {
	// Name is the registry key: the value an author writes for `theme` in
	// eypres.yaml. It is the only field Register requires.
	Name string

	// Stylesheet is the path, within Assets, of the CSS token sheet defining
	// the theme's variables.
	Stylesheet string

	// Assets is the filesystem holding Stylesheet and the theme's other
	// files. It may be nil for a theme that carries no files, such as a
	// registry-only theme in a test.
	Assets fs.FS
}

// CSS returns the contents of the theme's stylesheet.
func (t Theme) CSS() ([]byte, error) {
	if t.Assets == nil {
		return nil, fmt.Errorf("theme %q: no asset filesystem", t.Name)
	}
	if t.Stylesheet == "" {
		return nil, fmt.Errorf("theme %q: no stylesheet", t.Name)
	}
	css, err := fs.ReadFile(t.Assets, t.Stylesheet)
	if err != nil {
		return nil, fmt.Errorf("theme %q: read stylesheet %q: %w", t.Name, t.Stylesheet, err)
	}
	return css, nil
}

// Position locates a theme name in a source file. The zero value means the
// position is unknown.
type Position struct {
	// File is the path of the file the name was written in.
	File string
	// Line is the 1-based line number, or 0 when unknown.
	Line int
}

// UnknownThemeError is returned by Lookup when no registered theme has the
// requested name. It carries the offending name, the available names and the
// closest-match suggestion, plus an optional source position. Registry lookup
// has no source location of its own, so Position is empty unless a caller that
// knows where the name appeared attaches it with WithPosition.
type UnknownThemeError struct {
	// Name is the theme name that was looked up.
	Name string

	// Available is the sorted list of registered theme names.
	Available []string

	// Suggestion is the closest registered name, or "" when none is close.
	Suggestion string

	// Position is where the name was written, when known.
	Position Position
}

// Error renders the unknown-theme message: an optional file:line prefix, the
// offending name, the closest-match suggestion when one exists, and the list of
// available themes.
func (e *UnknownThemeError) Error() string {
	var b strings.Builder
	if e.Position.File != "" {
		b.WriteString(e.Position.File)
		if e.Position.Line > 0 {
			fmt.Fprintf(&b, ":%d", e.Position.Line)
		}
		b.WriteString(": ")
	}
	fmt.Fprintf(&b, "unknown theme %q", e.Name)
	if e.Suggestion != "" {
		fmt.Fprintf(&b, ": did you mean %q?", e.Suggestion)
	}
	fmt.Fprintf(&b, " (available themes: %s)", strings.Join(e.Available, ", "))
	return b.String()
}

// WithPosition returns a copy of e positioned at file:line. Callers that know
// where the theme name was written (the deck config loader) attach it here so
// the same error can be reported with a source location.
func (e *UnknownThemeError) WithPosition(file string, line int) *UnknownThemeError {
	c := *e
	c.Position = Position{File: file, Line: line}
	return &c
}

// Registry is a set of themes addressable by name. The zero value is ready to
// use.
type Registry struct {
	mu     sync.RWMutex
	themes map[string]Theme
}

// NewRegistry returns an empty theme registry.
func NewRegistry() *Registry {
	return &Registry{themes: make(map[string]Theme)}
}

// Register adds t to the registry. It reports an error when t.Name is empty or
// already registered.
func (r *Registry) Register(t Theme) error {
	if t.Name == "" {
		return errors.New("theme: name must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.themes == nil {
		r.themes = make(map[string]Theme)
	}
	if _, exists := r.themes[t.Name]; exists {
		return fmt.Errorf("theme %q is already registered", t.Name)
	}
	r.themes[t.Name] = t
	return nil
}

// Lookup returns the theme registered under name. When no theme has that name
// it returns an *UnknownThemeError listing the available themes and a
// closest-match suggestion.
func (r *Registry) Lookup(name string) (Theme, error) {
	r.mu.RLock()
	t, ok := r.themes[name]
	r.mu.RUnlock()
	if ok {
		return t, nil
	}
	available := r.Names()
	return Theme{}, &UnknownThemeError{
		Name:       name,
		Available:  available,
		Suggestion: suggest.Closest(name, available),
	}
}

// Names returns the registered theme names, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.themes))
	for name := range r.themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
