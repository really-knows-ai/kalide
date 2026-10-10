// Package config implements kalide's per-user settings store: the flat
// key-to-string-value file under the operating system's user config directory
// (<os.UserConfigDir()>/kalide) that backs the `kalide config` command and
// `kalide init`'s default-template-directory resolution.
//
// The store is local-filesystem only: it never touches the network. The only
// recognised key is "default-template-dir" (see RecognisedKeys); a key outside
// that set is refused as a usage error by the store's mutation and lookup
// helpers.
//
// # File format
//
// The config file is a small line-oriented text format. Blank lines, and lines
// whose first non-space character is '#', are ignored. Every other line is a
// "key=value" pair: the key is the text before the first '=' and the value the
// text after it, each trimmed of surrounding spaces. A non-empty, non-comment
// line with no '=' is a parse error. Only keys in the RecognisedKeys set are
// kept; any other key is ignored on read.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RecognisedKeys is the set of user-config keys kalide understands. It is the
// single source of truth for key validation: a key outside this set is refused
// as a usage error by the store's helpers. The set currently holds one key,
// "default-template-dir" — the directory of the template library `kalide init`
// uses when no path argument is given.
var RecognisedKeys = map[string]bool{
	"default-template-dir": true,
}

// configFile returns the path of kalide's per-user config file:
// <os.UserConfigDir()>/kalide/config. On macOS that is
// ~/Library/Application Support/kalide/config, on Linux
// $XDG_CONFIG_HOME/kalide/config (defaulting to ~/.config/kalide/config) and on
// Windows %AppData%\kalide\config.
func configFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "kalide", "config"), nil
}

// Load reads kalide's per-user config file into a map over the recognised
// keys. A missing config file — or a missing config directory — is not an
// error: Load returns an empty, non-nil map. An unreadable file, or a file
// whose contents are not valid key=value text, is returned as an error.
func Load() (map[string]string, error) {
	path, err := configFile()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	defer f.Close()

	return parse(f, path)
}

// parse reads key=value text into a map over the recognised keys, skipping
// blank lines and comments. path is used only to make parse errors name the
// file they came from.
func parse(r io.Reader, path string) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(r)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return nil, fmt.Errorf("parse config file %s: line %d: expected key=value, got %q", path, line, text)
		}
		key = strings.TrimSpace(key)
		if !RecognisedKeys[key] {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	return values, nil
}
