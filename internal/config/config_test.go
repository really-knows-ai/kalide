package config

// This file unit-tests kalide's per-user settings store (internal/config): the
// flat <os.UserConfigDir()>/kalide/config key=value file backing `kalide config`
// and `kalide init`'s default-template-directory resolution (phase-01 task-8).
//
// Every case isolates the store from the developer's real per-user config by
// pointing HOME — and, for portability, XDG_CONFIG_HOME (Linux), AppData and
// USERPROFILE (Windows) — at a fresh t.TempDir, and chdirs into a temp directory
// (t.Chdir) so relative-path normalisation is deterministic and no package file
// is ever written. The tests therefore never read or write the live config and
// never touch the network.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// isolateConfig points every directory the store consults at fresh temp dirs so
// a test can never touch the developer's real per-user config. HOME drives
// os.UserConfigDir on macOS and os.UserHomeDir everywhere; XDG_CONFIG_HOME
// drives os.UserConfigDir on Linux; AppData drives it on Windows; USERPROFILE
// drives os.UserHomeDir on Windows. It returns the temp root and is restored by
// t.Setenv when the test (or subtest) ends.
func isolateConfig(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("AppData", filepath.Join(root, "appdata"))
	t.Setenv("USERPROFILE", root)
	return root
}

// userConfigFile is the config file path the store must use under the isolated
// environment: <os.UserConfigDir()>/kalide/config. It is derived from
// os.UserConfigDir so it stays correct on every platform.
func userConfigFile(t *testing.T) string {
	t.Helper()

	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error = %v", err)
	}
	return filepath.Join(dir, "kalide", "config")
}

// writeConfig writes content to the store's config file, creating the parent
// directory, so Load can be driven with hand-written file text.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := userConfigFile(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file %s: %v", path, err)
	}
	return path
}

// assertStoreAbsent asserts that neither the config file nor its parent
// directory exists, i.e. a refused operation wrote nothing.
func assertStoreAbsent(t *testing.T) {
	t.Helper()

	path := userConfigFile(t)
	if _, err := os.Stat(path); err == nil {
		t.Errorf("config file %s exists, want nothing written", path)
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Errorf("config directory %s exists, want nothing written", filepath.Dir(path))
	}
}

// TestSetNormalisesPaths covers AC (a): Set normalises the path value to an
// absolute path — a leading `~` expands to the home directory, a relative path
// resolves against the process cwd — and the stored value is absolute and
// independent of the working directory of any later command.
func TestSetNormalisesPaths(t *testing.T) {
	t.Run("leading tilde expands to the home directory", func(t *testing.T) {
		isolateConfig(t)
		t.Chdir(t.TempDir())

		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("os.UserHomeDir() error = %v", err)
		}

		if err := Set(defaultTemplateDirKey, "~/templates"); err != nil {
			t.Fatalf("Set(%q, %q) error = %v", defaultTemplateDirKey, "~/templates", err)
		}
		got, err := Get(defaultTemplateDirKey)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", defaultTemplateDirKey, err)
		}
		want := filepath.Join(home, "templates")
		if got != want {
			t.Errorf("Get(%q) = %q, want %q", defaultTemplateDirKey, got, want)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("Get(%q) = %q, want an absolute path", defaultTemplateDirKey, got)
		}
	})

	t.Run("bare tilde expands to the home directory", func(t *testing.T) {
		isolateConfig(t)
		t.Chdir(t.TempDir())

		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("os.UserHomeDir() error = %v", err)
		}

		if err := Set(defaultTemplateDirKey, "~"); err != nil {
			t.Fatalf("Set(%q, %q) error = %v", defaultTemplateDirKey, "~", err)
		}
		got, err := Get(defaultTemplateDirKey)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", defaultTemplateDirKey, err)
		}
		if got != home {
			t.Errorf("Get(%q) = %q, want %q", defaultTemplateDirKey, got, home)
		}
	})

	t.Run("relative path resolves against the current working directory", func(t *testing.T) {
		isolateConfig(t)

		cwd := t.TempDir()
		t.Chdir(cwd)
		// os.Getwd canonicalises the path (e.g. macOS /var -> /private/var), and
		// filepath.Abs resolves the relative value against exactly that path.
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd() error = %v", err)
		}

		if err := Set(defaultTemplateDirKey, "templates"); err != nil {
			t.Fatalf("Set(%q, %q) error = %v", defaultTemplateDirKey, "templates", err)
		}
		got, err := Get(defaultTemplateDirKey)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", defaultTemplateDirKey, err)
		}
		want := filepath.Join(wd, "templates")
		if got != want {
			t.Errorf("Get(%q) = %q, want %q", defaultTemplateDirKey, got, want)
		}
	})

	t.Run("absolute path is stored unchanged", func(t *testing.T) {
		isolateConfig(t)
		t.Chdir(t.TempDir())

		abs := t.TempDir()
		if err := Set(defaultTemplateDirKey, abs); err != nil {
			t.Fatalf("Set(%q, %q) error = %v", defaultTemplateDirKey, abs, err)
		}
		got, err := Get(defaultTemplateDirKey)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", defaultTemplateDirKey, err)
		}
		if got != abs {
			t.Errorf("Get(%q) = %q, want %q", defaultTemplateDirKey, got, abs)
		}
	})

	t.Run("stored value stays absolute across a later command cwd", func(t *testing.T) {
		isolateConfig(t)

		first := t.TempDir()
		t.Chdir(first)
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd() error = %v", err)
		}
		if err := Set(defaultTemplateDirKey, "rel/deck"); err != nil {
			t.Fatalf("Set(%q, %q) error = %v", defaultTemplateDirKey, "rel/deck", err)
		}
		want := filepath.Join(wd, "rel", "deck")

		// A later command runs in a different directory; the stored value must
		// not change.
		t.Chdir(t.TempDir())
		got, err := Get(defaultTemplateDirKey)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", defaultTemplateDirKey, err)
		}
		if got != want {
			t.Errorf("Get(%q) = %q, want %q (independent of cwd)", defaultTemplateDirKey, got, want)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("Get(%q) = %q, want an absolute path", defaultTemplateDirKey, got)
		}
	})
}

// TestMissingConfigYieldsEmptyStore covers AC (b): a missing config file (or
// its directory) is not an error — Load returns an empty, non-nil map — while
// Get of a recognised-but-unset key is an error naming the key.
func TestMissingConfigYieldsEmptyStore(t *testing.T) {
	isolateConfig(t)

	values, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for a missing config file", err)
	}
	if values == nil {
		t.Fatalf("Load() = nil, want a non-nil empty map")
	}
	if len(values) != 0 {
		t.Errorf("Load() = %v, want an empty map", values)
	}

	got, err := Get(defaultTemplateDirKey)
	if err == nil {
		t.Fatalf("Get(%q) = %q, nil; want an error for an unset key", defaultTemplateDirKey, got)
	}
	if !strings.Contains(err.Error(), defaultTemplateDirKey) {
		t.Errorf("Get(%q) error = %q, want it to name %q", defaultTemplateDirKey, err, defaultTemplateDirKey)
	}

	// Reading a missing store writes nothing.
	assertStoreAbsent(t)
}

// TestUnrecognisedKeysRefused covers AC (c): get/set/unset each refuse a key
// outside the recognised set {default-template-dir} and write nothing.
func TestUnrecognisedKeysRefused(t *testing.T) {
	const foreign = "template-dir"

	if !RecognisedKeys[defaultTemplateDirKey] {
		t.Errorf("RecognisedKeys[%q] = false, want true", defaultTemplateDirKey)
	}
	if RecognisedKeys[foreign] {
		t.Errorf("RecognisedKeys[%q] = true, want false", foreign)
	}

	t.Run("Set", func(t *testing.T) {
		isolateConfig(t)

		err := Set(foreign, t.TempDir())
		assertUnrecognised(t, err, foreign)
		assertStoreAbsent(t)
	})

	t.Run("Get", func(t *testing.T) {
		isolateConfig(t)

		_, err := Get(foreign)
		assertUnrecognised(t, err, foreign)
		assertStoreAbsent(t)
	})

	t.Run("Unset", func(t *testing.T) {
		isolateConfig(t)

		err := Unset(foreign)
		assertUnrecognised(t, err, foreign)
		assertStoreAbsent(t)
	})
}

// assertUnrecognised asserts err refuses key as a usage error.
func assertUnrecognised(t *testing.T, err error, key string) {
	t.Helper()

	if err == nil {
		t.Fatalf("operation on %q = nil error, want an unrecognised-key error", key)
	}
	if !strings.Contains(err.Error(), "unrecognised") {
		t.Errorf("error = %q, want an unrecognised-key error", err)
	}
	if !strings.Contains(err.Error(), key) {
		t.Errorf("error = %q, want it to name %q", err, key)
	}
}

// TestGetSetUnsetListRoundTrip covers AC (d): Get/Set/Unset/List round-trip
// over the same config file, with List emitting one key=value entry.
func TestGetSetUnsetListRoundTrip(t *testing.T) {
	isolateConfig(t)
	t.Chdir(t.TempDir())

	deckDir := t.TempDir()

	if err := Set(defaultTemplateDirKey, deckDir); err != nil {
		t.Fatalf("Set(%q, %q) error = %v", defaultTemplateDirKey, deckDir, err)
	}
	got, err := Get(defaultTemplateDirKey)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", defaultTemplateDirKey, err)
	}
	if got != deckDir {
		t.Errorf("Get(%q) = %q, want %q", defaultTemplateDirKey, got, deckDir)
	}

	entries, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []string{defaultTemplateDirKey + "=" + deckDir}
	if !slices.Equal(entries, want) {
		t.Errorf("List() = %v, want %v", entries, want)
	}

	// The first write created the file and its parent directory.
	path := userConfigFile(t)
	if fi, err := os.Stat(path); err != nil {
		t.Fatalf("stat config file %s after Set: %v", path, err)
	} else if fi.IsDir() {
		t.Fatalf("config file %s is a directory, want a file", path)
	}

	// Unset removes the key: a following Get errors naming the key and List is
	// empty.
	if err := Unset(defaultTemplateDirKey); err != nil {
		t.Fatalf("Unset(%q) error = %v", defaultTemplateDirKey, err)
	}
	if _, err := Get(defaultTemplateDirKey); err == nil {
		t.Errorf("Get(%q) = nil error after Unset, want an unset-key error", defaultTemplateDirKey)
	}
	if entries, err := List(); err != nil {
		t.Fatalf("List() error = %v", err)
	} else if len(entries) != 0 {
		t.Errorf("List() = %v, want empty after Unset", entries)
	}

	// Unsetting an already-absent recognised key is a no-op success.
	if err := Unset(defaultTemplateDirKey); err != nil {
		t.Errorf("Unset(%q) on an absent key error = %v, want nil", defaultTemplateDirKey, err)
	}
}

// TestConfigFileUnderUserConfigDir covers AC (e): the config file is located
// under os.UserConfigDir()/kalide.
func TestConfigFileUnderUserConfigDir(t *testing.T) {
	isolateConfig(t)
	t.Chdir(t.TempDir())

	deckDir := t.TempDir()
	if err := Set(defaultTemplateDirKey, deckDir); err != nil {
		t.Fatalf("Set(%q, %q) error = %v", defaultTemplateDirKey, deckDir, err)
	}

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error = %v", err)
	}
	want := filepath.Join(userDir, "kalide", "config")

	got, err := configFile()
	if err != nil {
		t.Fatalf("configFile() error = %v", err)
	}
	if got != want {
		t.Errorf("configFile() = %q, want %q", got, want)
	}
	if base := filepath.Base(filepath.Dir(want)); base != "kalide" {
		t.Errorf("config file parent directory = %q, want %q", base, "kalide")
	}

	fi, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat config file %s: %v", want, err)
	}
	if fi.IsDir() {
		t.Fatalf("config file %s is a directory, want a file", want)
	}

	content, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read config file %s: %v", want, err)
	}
	if !strings.Contains(string(content), defaultTemplateDirKey+"="+deckDir) {
		t.Errorf("config file content = %q, want a %s=%s line", content, defaultTemplateDirKey, deckDir)
	}
}

// TestLoadParsesKeyValueText covers the file-format contract Load relies on:
// blank lines, comments and unrecognised keys are skipped, while a non-comment
// line with no '=' is a parse error.
func TestLoadParsesKeyValueText(t *testing.T) {
	t.Run("blank lines, comments and unrecognised keys are skipped", func(t *testing.T) {
		isolateConfig(t)

		writeConfig(t, "# a comment\n\nother-key=ignored\ndefault-template-dir=/deck/dir\n")

		values, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(values) != 1 || values[defaultTemplateDirKey] != "/deck/dir" {
			t.Errorf("Load() = %v, want only %s=/deck/dir", values, defaultTemplateDirKey)
		}
	})

	t.Run("a non-comment line with no '=' is a parse error", func(t *testing.T) {
		isolateConfig(t)

		writeConfig(t, "malformed line without separator\n")

		if _, err := Load(); err == nil {
			t.Fatalf("Load() = nil error, want a parse error")
		}
	})
}
