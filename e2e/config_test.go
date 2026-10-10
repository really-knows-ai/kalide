package e2e

// This file is the phase-02 task-6 end-to-end test for `kalide config`
// (requirements.requirement.cli-config, solution.component.config-command and
// domain.service.load-user-config). It drives the real kalide binary the
// harness builds (harness.go) and asserts the command surface an author sees
// over a real per-user config file, not internal/config's or runConfig's
// internals:
//
//   - `kalide config set default-template-dir <relative>` and
//     `... default-template-dir <~/...>` store an absolute path: the relative
//     value resolves against the process's working directory and the `~/` value
//     against the user's home directory, so the store holds an absolute path
//     either way;
//   - `kalide config get default-template-dir` prints exactly that absolute
//     path and exits 0; `kalide config list` prints
//     `default-template-dir=<absolute path>`;
//   - `kalide config unset default-template-dir` removes the key and a
//     following get exits non-zero naming the key, while list is empty again;
//   - a key outside the recognised set {default-template-dir} is a usage error
//     (exit 2, usage on stderr listing default-template-dir) that writes
//     nothing for each of set, get and unset;
//   - on a fresh config, get exits non-zero naming the key, list is empty and
//     unsetting the recognised-but-absent key is a no-op success.
//
// The per-user config is hermetic: every child process is run with HOME,
// XDG_CONFIG_HOME, AppData and USERPROFILE pointed at a fresh temp directory,
// so os.UserConfigDir() and os.UserHomeDir() resolve inside the test on every
// platform and the developer's real config is never read or written. "Writes
// nothing" is asserted by the strongest cross-platform fact available: the
// private home directory is still empty, so no config file (which always lives
// under it) was created.
//
// The command is local-filesystem only (no network). It builds a binary, so it
// is skipped under -short.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// configKeyDefaultTemplateDir is the sole recognised user-config key
// (internal/config's unexported defaultTemplateDirKey). It is spelled here so
// the test asserts the exact key the store and the CLI must agree on.
const configKeyDefaultTemplateDir = "default-template-dir"

// otherConfigKey is a key outside the recognised key set.
const otherConfigKey = "other-key"

// TestConfigRoundTrip covers the get/set/unset/list round-trip over a real
// per-user config: set normalises a relative and a `~/` value to an absolute
// path, get prints it and exits 0, list prints `key=value`, and unset removes
// it so a following get exits non-zero.
func TestConfigRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	home := t.TempDir()

	// The relative value resolves against the process's working directory.
	// getcwd canonicalises symlinks (macOS reports /var/folders as
	// /private/var/...), so resolve the harness workdir before comparing.
	workDir, err := filepath.EvalSymlinks(h.WorkDir())
	if err != nil {
		t.Fatalf("resolve symlinks in working directory %s: %v", h.WorkDir(), err)
	}
	relative := filepath.Join("decks", "starter")

	// `set <relative>` stores the value absolute: getcwd joined with the
	// relative path.
	stdout, stderr, code := runKalideWithHome(t, h, home, "config", "set", configKeyDefaultTemplateDir, relative)
	if code != 0 {
		t.Fatalf("kalide config set %s %s exit = %d, want 0 (stderr = %q)",
			configKeyDefaultTemplateDir, relative, code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("kalide config set %s %s stdout = %q, stderr = %q, want both empty",
			configKeyDefaultTemplateDir, relative, stdout, stderr)
	}
	assertConfigValue(t, h, home, filepath.Join(workDir, relative))

	// `set <~/...>` stores the value absolute too: the user's home directory
	// joined with the path after `~/`.
	tilded := "templates"
	stdout, stderr, code = runKalideWithHome(t, h, home, "config", "set", configKeyDefaultTemplateDir, "~/"+tilded)
	if code != 0 {
		t.Fatalf("kalide config set %s ~/%s exit = %d, want 0 (stderr = %q)",
			configKeyDefaultTemplateDir, tilded, code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("kalide config set %s ~/%s stdout = %q, stderr = %q, want both empty",
			configKeyDefaultTemplateDir, tilded, stdout, stderr)
	}
	assertConfigValue(t, h, home, filepath.Join(home, tilded))

	// unset removes the key and exits 0 with no output.
	stdout, stderr, code = runKalideWithHome(t, h, home, "config", "unset", configKeyDefaultTemplateDir)
	if code != 0 {
		t.Fatalf("kalide config unset %s exit = %d, want 0 (stderr = %q)",
			configKeyDefaultTemplateDir, code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("kalide config unset %s stdout = %q, stderr = %q, want both empty",
			configKeyDefaultTemplateDir, stdout, stderr)
	}

	// A following get exits non-zero naming the key, and list is empty again.
	stdout, stderr, code = runKalideWithHome(t, h, home, "config", "get", configKeyDefaultTemplateDir)
	if code == 0 {
		t.Fatalf("kalide config get after unset exit = 0, want non-zero")
	}
	if stdout != "" {
		t.Errorf("kalide config get after unset stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, configKeyDefaultTemplateDir) {
		t.Errorf("kalide config get after unset stderr = %q, want it to name %q",
			stderr, configKeyDefaultTemplateDir)
	}

	stdout, stderr, code = runKalideWithHome(t, h, home, "config", "list")
	if code != 0 {
		t.Fatalf("kalide config list after unset exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("kalide config list after unset stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("kalide config list after unset stderr = %q, want empty", stderr)
	}
}

// TestConfigFreshAndUsageErrors covers the fresh-config and usage-error
// contract: a fresh get exits non-zero naming the key; a fresh list is empty;
// unsetting the recognised-but-absent key is a no-op success; and a key outside
// the recognised set is a usage error for set, get and unset that writes
// nothing.
func TestConfigFreshAndUsageErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)

	// A fresh config: get exits non-zero naming the key and writes nothing.
	t.Run("fresh get", func(t *testing.T) {
		home := t.TempDir()

		stdout, stderr, code := runKalideWithHome(t, h, home, "config", "get", configKeyDefaultTemplateDir)
		if code == 0 {
			t.Fatalf("fresh config get exit = 0, want non-zero")
		}
		if stdout != "" {
			t.Errorf("fresh config get stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, configKeyDefaultTemplateDir) {
			t.Errorf("fresh config get stderr = %q, want it to name %q", stderr, configKeyDefaultTemplateDir)
		}
		assertDirEmpty(t, home)
	})

	// A fresh list exits 0 with no output and writes nothing.
	t.Run("fresh list", func(t *testing.T) {
		home := t.TempDir()

		stdout, stderr, code := runKalideWithHome(t, h, home, "config", "list")
		if code != 0 {
			t.Fatalf("fresh config list exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stdout != "" || stderr != "" {
			t.Errorf("fresh config list stdout = %q, stderr = %q, want both empty", stdout, stderr)
		}
		assertDirEmpty(t, home)
	})

	// Unsetting a recognised-but-absent key is a no-op success that writes
	// nothing.
	t.Run("unset absent", func(t *testing.T) {
		home := t.TempDir()

		stdout, stderr, code := runKalideWithHome(t, h, home, "config", "unset", configKeyDefaultTemplateDir)
		if code != 0 {
			t.Fatalf("config unset (absent key) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stdout != "" || stderr != "" {
			t.Errorf("config unset (absent key) stdout = %q, stderr = %q, want both empty", stdout, stderr)
		}
		assertDirEmpty(t, home)
	})

	// An unknown key is a usage error for set, get and unset: exit 2, usage on
	// stderr listing default-template-dir, and nothing written.
	cases := []struct {
		sub  string
		args []string
	}{
		{sub: "set", args: []string{"config", "set", otherConfigKey, "x"}},
		{sub: "get", args: []string{"config", "get", otherConfigKey}},
		{sub: "unset", args: []string{"config", "unset", otherConfigKey}},
	}
	for _, tc := range cases {
		t.Run("unknown key "+tc.sub, func(t *testing.T) {
			home := t.TempDir()

			stdout, stderr, code := runKalideWithHome(t, h, home, tc.args...)
			if code != 2 {
				t.Fatalf("config %s %s exit = %d, want 2 (stderr = %q)", tc.sub, otherConfigKey, code, stderr)
			}
			if stdout != "" {
				t.Errorf("config %s %s stdout = %q, want empty", tc.sub, otherConfigKey, stdout)
			}
			for _, want := range []string{"unrecognised", otherConfigKey, configKeyDefaultTemplateDir, "Usage:"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("config %s %s stderr = %q, want it to mention %q", tc.sub, otherConfigKey, stderr, want)
				}
			}
			assertDirEmpty(t, home)
		})
	}
}

// runKalideWithHome runs `kalide args...` to completion with the per-user
// config environment pointed at home: HOME and USERPROFILE locate the home
// directory (os.UserHomeDir), XDG_CONFIG_HOME the config directory on Linux and
// AppData on Windows (os.UserConfigDir). It mirrors Harness.Run for one-shot
// commands but with the hermetic config environment, so the developer's real
// config is never read or written.
func runKalideWithHome(t *testing.T, h *Harness, home string, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	cmd := h.Command(args...)
	cmd.Env = envWith(cmd.Env, "HOME", home)
	cmd.Env = envWith(cmd.Env, "XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))
	cmd.Env = envWith(cmd.Env, "AppData", filepath.Join(home, "appdata"))
	cmd.Env = envWith(cmd.Env, "USERPROFILE", home)

	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	if err == nil {
		return out.String(), errOut.String(), 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), errOut.String(), ee.ExitCode()
	}
	t.Fatalf("e2e: run kalide %v: %v", args, err)
	return "", "", -1
}

// assertConfigValue asserts that `config get default-template-dir` prints an
// absolute path naming want and exits 0, and that `config list` prints exactly
// `default-template-dir=<that value>`, both with empty stderr.
func assertConfigValue(t *testing.T, h *Harness, home, want string) {
	t.Helper()

	stdout, stderr, code := runKalideWithHome(t, h, home, "config", "get", configKeyDefaultTemplateDir)
	if code != 0 {
		t.Fatalf("kalide config get exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	got := strings.TrimSuffix(stdout, "\n")
	if !filepath.IsAbs(got) {
		t.Errorf("kalide config get printed %q, want an absolute path", got)
	}
	if resolvedPath(t, got) != resolvedPath(t, want) {
		t.Errorf("kalide config get printed %q, want %q", got, want)
	}
	if stderr != "" {
		t.Errorf("kalide config get stderr = %q, want empty", stderr)
	}

	stdout, stderr, code = runKalideWithHome(t, h, home, "config", "list")
	if code != 0 {
		t.Fatalf("kalide config list exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if wantLine := configKeyDefaultTemplateDir + "=" + got + "\n"; stdout != wantLine {
		t.Errorf("kalide config list stdout = %q, want %q", stdout, wantLine)
	}
	if stderr != "" {
		t.Errorf("kalide config list stderr = %q, want empty", stderr)
	}
}

// resolvedPath returns p with the symlinks in its deepest existing ancestor
// resolved and the non-existent remainder appended, so two paths naming the
// same location compare equal even when they differ only in a symlinked prefix
// (the harness's t.TempDir hands out /var/... while the child's getcwd reports
// /private/var/... on macOS).
func resolvedPath(t *testing.T, p string) string {
	t.Helper()

	cur := filepath.Clean(p)
	rest := ""
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(resolved, rest)
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("resolve symlinks in %s: %v", p, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(p)
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
