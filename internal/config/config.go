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
	"sort"
	"strings"
)

// defaultTemplateDirKey is the recognised key whose value is a directory path.
// Set normalises it to an absolute path so the stored value does not depend on
// the working directory of any later command.
const defaultTemplateDirKey = "default-template-dir"

// RecognisedKeys is the set of user-config keys kalide understands. It is the
// single source of truth for key validation: a key outside this set is refused
// as a usage error by the store's helpers. The set currently holds one key,
// "default-template-dir" — the directory of the template library `kalide init`
// uses when no path argument is given.
var RecognisedKeys = map[string]bool{
	defaultTemplateDirKey: true,
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

// Get returns the value stored under key in kalide's per-user config file. key
// must be one of the RecognisedKeys; an unrecognised key is refused and no
// value is returned. A recognised key that is not set is an error naming the
// key.
func Get(key string) (string, error) {
	if !RecognisedKeys[key] {
		return "", fmt.Errorf("unrecognised config key %q", key)
	}

	values, err := Load()
	if err != nil {
		return "", err
	}

	value, ok := values[key]
	if !ok {
		return "", fmt.Errorf("config key %q is not set", key)
	}
	return value, nil
}

// Set writes value under key in kalide's per-user config file, creating the
// file and its parent directory on the first write. key must be one of the
// RecognisedKeys; an unrecognised key is refused and nothing is written. Other
// stored recognised keys are preserved.
//
// The value of the path key "default-template-dir" is normalised to an absolute
// path: a leading "~" expands to the user's home directory and a relative path
// resolves against the process's current working directory. The stored value is
// therefore independent of the working directory of any later command.
func Set(key, value string) error {
	if !RecognisedKeys[key] {
		return fmt.Errorf("unrecognised config key %q", key)
	}
	if key == defaultTemplateDirKey {
		abs, err := normalisePath(value)
		if err != nil {
			return err
		}
		value = abs
	}

	values, err := Load()
	if err != nil {
		return err
	}
	values[key] = value
	return writeValues(values)
}

// normalisePath returns value as an absolute, cleaned path. A leading "~"
// expands to the user's home directory; a relative path resolves against the
// process's current working directory.
func normalisePath(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("normalise path %q: expand home directory: %w", value, err)
		}
		if value == "~" {
			value = home
		} else {
			value = filepath.Join(home, value[2:])
		}
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("normalise path %q: %w", value, err)
	}
	return abs, nil
}

// writeValues writes the recognised keys of values to the config file as
// deterministic (sorted) key=value lines, creating the file's parent directory
// as needed.
func writeValues(values map[string]string) error {
	path, err := configFile()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		if RecognisedKeys[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(values[key])
		b.WriteByte('\n')
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}
	return nil
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
