// Package deck loads and models a kalide deck: its deck-wide configuration
// (kalide.yaml) and, in a later task, its ordered slide files.
//
// LoadConfig reads kalide.yaml and validates the deck-wide configuration with
// positioned errors. Deck validation is fail-fast (whole-deck-validation), so
// the configuration is checked first, before slide filenames and slide
// contents.
package deck

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/suggest"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// ConfigFile is the fixed name of the deck configuration file at the root of a
// deck directory.
const ConfigFile = "kalide.yaml"

// Navigation is the deck-wide reveal.js navigationMode (navigation-mode). A
// missing navigation key resolves to NavigationDefault.
const (
	NavigationDefault = "default"
	NavigationLinear  = "linear"
	NavigationGrid    = "grid"
)

// configKeys is the complete set of keys kalide.yaml may contain, in
// declaration order. It is both the unknown-key whitelist and the candidate
// list for closest-match suggestions.
var configKeys = []string{"title", "author", "date", "theme", "navigation", "properties", "templates"}

// ConfigKeys returns a fresh copy of configKeys — the complete set of keys
// kalide.yaml may contain (deck-config) — sorted. It is the single source of
// truth for the deck configuration vocabulary, so callers that must enumerate
// the implemented keys (notably the agent-guide drift self-test) derive them
// from here rather than duplicating the list.
func ConfigKeys() []string {
	out := make([]string, len(configKeys))
	copy(out, configKeys)
	sort.Strings(out)
	return out
}

// dateLayout is the only accepted date form: an ISO calendar date, no time and
// no zone.
const dateLayout = "2006-01-02"

// Config is the deck-wide configuration read from kalide.yaml. It is
// resolved once per deck; `theme` and `navigation` are deck-wide only and are
// never set per slide.
type Config struct {
	// Title is the deck title. Required.
	Title string

	// Author is the presenter, when given.
	Author string

	// Date is the presentation date in YYYY-MM-DD form, or "" when omitted.
	Date string

	// Theme is the resolved theme name (theme-selection). It is
	// theme.DefaultName when the key is omitted.
	Theme string

	// Navigation is the reveal.js navigationMode: NavigationDefault,
	// NavigationLinear or NavigationGrid. It is NavigationDefault when the
	// key is omitted.
	Navigation string

	// Properties is the deck-wide author-declared `properties` block
	// (deck-properties): arbitrary non-empty author keys mapped to simple
	// typed YAML scalars (string, number, boolean or date). It is always
	// non-nil — the empty map when the block is absent, null or empty.
	Properties map[string]any

	// Templates is the optional top-level `templates:` value: the path to
	// the deck's external template library directory
	// (external-template-library). It is kept exactly as written in
	// kalide.yaml and is "" when the key is absent or empty. A relative
	// path is resolved against the deck root by the template loader, not
	// here; use TemplatesPath to read it.
	Templates string
}

// LoadConfig reads and validates the deck configuration at path within fsys —
// typically ConfigFile at the root of a deck directory. themes is the project
// theme registry to resolve the `theme` key against — typically the one
// theme.LoadDir built from the project's templates/themes directory. It must
// not be nil: LoadConfig no longer falls back to any package-global theme
// lookup (theme-selection); the caller is responsible for building and
// passing the registry.
//
// Rules (deck-config):
//   - title is required and must be a string;
//   - author and date are optional; date must be YYYY-MM-DD;
//   - theme is optional, defaults to "default" (which must exist in themes),
//     and must otherwise resolve in themes;
//   - navigation is optional, defaults to "default", and must be default,
//     linear or grid;
//   - properties is optional and, when present, must be a mapping of arbitrary
//     non-empty author keys to simple typed scalars (string, number, boolean or
//     date YYYY-MM-DD); a mapping or sequence value is a config error naming
//     its key. The resolved map is always non-nil;
//   - templates is optional and, when present, must be a string path to the
//     deck's external template library (external-template-library); it is
//     stored exactly as written (empty when the key is absent or empty), and a
//     relative path is later resolved against the deck root by the loader;
//   - any other key is an unknown key, rejected with a closest-match "did you
//     mean …?" suggestion.
//
// Every error that can be tied to a source location is positioned as
// file:line; an unknown theme carries its position through
// theme.UnknownThemeError.WithPosition.
func LoadConfig(fsys fs.FS, path string, themes *theme.Registry) (*Config, error) {
	if themes == nil {
		return nil, fmt.Errorf("%s: no theme registry given", path)
	}

	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		// yaml.v3 already reports the offending line in its error text.
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	mapping := documentMapping(&doc)
	if mapping == nil {
		return nil, positioned(path, 1, `missing required key "title"`)
	}
	if mapping.Kind != yaml.MappingNode {
		return nil, positioned(path, nodeLine(mapping, 1),
			"expected a mapping of config keys, got %s", kindWord(mapping))
	}

	cfg := &Config{
		Theme:      theme.DefaultName,
		Navigation: NavigationDefault,
		Properties: map[string]any{},
	}
	haveTitle := false
	haveTheme := false

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		val := mapping.Content[i+1]
		line := nodeLine(key, nodeLine(mapping, 1))

		switch key.Value {
		case "title":
			if isNull(val) || isString(val) && strings.TrimSpace(val.Value) == "" {
				return nil, positioned(path, line, `missing required key "title"`)
			}
			if !isString(val) {
				return nil, typeError(path, line, key, val)
			}
			cfg.Title = val.Value
			haveTitle = true

		case "author":
			if isNull(val) {
				break
			}
			if !isString(val) {
				return nil, typeError(path, line, key, val)
			}
			cfg.Author = val.Value

		case "date":
			if isNull(val) || isString(val) && val.Value == "" {
				break
			}
			if !isScalar(val) {
				return nil, typeError(path, line, key, val)
			}
			if _, err := time.Parse(dateLayout, val.Value); err != nil {
				return nil, positioned(path, line,
					"key %q: %q is not a valid date, expected YYYY-MM-DD", key.Value, val.Value)
			}
			cfg.Date = val.Value

		case "theme":
			if !isNull(val) && !isString(val) {
				return nil, typeError(path, line, key, val)
			}
			name := theme.DefaultName
			if !isNull(val) && val.Value != "" {
				name = val.Value
			}
			if _, err := themes.Lookup(name); err != nil {
				var unknown *theme.UnknownThemeError
				if errors.As(err, &unknown) {
					return nil, unknown.WithPosition(path, line)
				}
				return nil, positioned(path, line, "theme %q: %v", name, err)
			}
			cfg.Theme = name
			haveTheme = true

		case "navigation":
			if !isNull(val) && !isString(val) {
				return nil, typeError(path, line, key, val)
			}
			mode := NavigationDefault
			if !isNull(val) && val.Value != "" {
				mode = val.Value
			}
			switch mode {
			case NavigationDefault, NavigationLinear, NavigationGrid:
				cfg.Navigation = mode
			default:
				return nil, positioned(path, line,
					"key %q: unknown navigation mode %q (valid values: %s)",
					key.Value, mode, strings.Join([]string{NavigationDefault, NavigationLinear, NavigationGrid}, ", "))
			}

		case "properties":
			props, err := parseProperties(path, key, val)
			if err != nil {
				return nil, err
			}
			cfg.Properties = props

		case "templates":
			if !isNull(val) && !isString(val) {
				return nil, typeError(path, line, key, val)
			}
			if !isNull(val) {
				cfg.Templates = val.Value
			}

		default:
			if s := suggest.Closest(key.Value, configKeys); s != "" {
				return nil, positioned(path, line, "unknown key %q: did you mean %q?", key.Value, s)
			}
			return nil, positioned(path, line, "unknown key %q", key.Value)
		}
	}

	if !haveTitle {
		return nil, positioned(path, nodeLine(mapping, 1), `missing required key "title"`)
	}
	if !haveTheme {
		if _, err := themes.Lookup(cfg.Theme); err != nil {
			var unknown *theme.UnknownThemeError
			if errors.As(err, &unknown) {
				return nil, unknown.WithPosition(path, nodeLine(mapping, 1))
			}
			return nil, positioned(path, nodeLine(mapping, 1), "theme %q: %v", cfg.Theme, err)
		}
	}
	return cfg, nil
}

// TemplatesPath returns the configured top-level `templates:` path from a
// deck's Config exactly as it was written in kalide.yaml, or "" when the key
// is absent or empty (external-template-library).
//
// It is the deck-side accessor for the template loader's single root
// resolution point; it does not resolve, clean or validate the path — a
// relative path is resolved against the deck root by the loader
// (template.ResolveLibraryRoot), not here.
func TemplatesPath(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.Templates
}

// parseProperties parses the value of the top-level `properties` key: a mapping
// of arbitrary non-empty author keys to simple typed YAML scalars (string,
// number, boolean or date). A mapping or sequence value — or a null value — is
// a config error naming the offending property key. The returned map is always
// non-nil, empty when the value is absent (null) or an empty mapping.
//
// Property keys are not checked against the top-level whitelist: any non-empty
// string key is accepted.
func parseProperties(path string, key, val *yaml.Node) (map[string]any, error) {
	props := map[string]any{}
	if val == nil || isNull(val) {
		return props, nil
	}
	if val.Kind != yaml.MappingNode {
		return nil, positioned(path, nodeLine(val, nodeLine(key, 1)),
			"key %q: expected a mapping, got %s", key.Value, kindWord(val))
	}
	for i := 0; i+1 < len(val.Content); i += 2 {
		name := val.Content[i]
		value := val.Content[i+1]
		line := nodeLine(name, nodeLine(val, nodeLine(key, 1)))

		if !isString(name) || name.Value == "" {
			return nil, positioned(path, line, "property keys must be non-empty strings")
		}
		if !isScalar(value) || isNull(value) {
			return nil, positioned(path, line,
				"key %q: expected a string, number, boolean or date, got %s",
				name.Value, kindWord(value))
		}
		v, err := decodeProperty(value)
		if err != nil {
			return nil, positioned(path, line, "key %q: %v", name.Value, err)
		}
		props[name.Value] = v
	}
	return props, nil
}

// decodeProperty resolves one property scalar to its typed Go value: a string
// stays a string, a number resolves to int or float64, a boolean to bool, and a
// date written YYYY-MM-DD to time.Time. It mirrors how frontmatter field values
// are decoded.
func decodeProperty(value *yaml.Node) (any, error) {
	if value.ShortTag() == "!!timestamp" {
		t, err := time.Parse(dateLayout, value.Value)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid date, expected YYYY-MM-DD", value.Value)
		}
		return t, nil
	}
	var v any
	if err := value.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// documentMapping returns the root mapping of a parsed YAML document, or nil
// when the document is empty. A document always wraps its content, but this
// tolerates a bare node too.
func documentMapping(doc *yaml.Node) *yaml.Node {
	if doc == nil || doc.Kind == 0 {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	return doc
}

// isNull reports whether n is a YAML null (an omitted value such as `key:`).
func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

// isScalar reports whether n is any scalar node.
func isScalar(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode
}

// isString reports whether n is a scalar carrying the string tag. A plain
// scalar that resolves to another type (number, boolean, date) is not a string,
// so `title: 42` is a type error rather than a titled "42".
func isString(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str"
}

// kindWord names a node's type for a human-readable type error.
func kindWord(n *yaml.Node) string {
	switch n.ShortTag() {
	case "!!str":
		return "a string"
	case "!!int", "!!float":
		return "a number"
	case "!!bool":
		return "a boolean"
	case "!!null":
		return "nothing (null)"
	case "!!timestamp":
		return "a date"
	case "!!seq":
		return "a list"
	case "!!map":
		return "a mapping"
	default:
		return n.ShortTag()
	}
}

// typeError reports key as having the wrong type.
func typeError(path string, line int, key, val *yaml.Node) error {
	return positioned(path, line, "key %q: expected a string, got %s", key.Value, kindWord(val))
}

// nodeLine returns n's 1-based line, or fallback when n has none (the parser
// does not always set it for synthetic or empty nodes).
func nodeLine(n *yaml.Node, fallback int) int {
	if n != nil && n.Line > 0 {
		return n.Line
	}
	return fallback
}

// positioned formats an error as file:line: message, omitting the line when it
// is unknown.
func positioned(file string, line int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if line > 0 {
		return fmt.Errorf("%s:%d: %s", file, line, msg)
	}
	return fmt.Errorf("%s: %s", file, msg)
}
