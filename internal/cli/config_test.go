package cli

// This file unit-tests the `kalide config` command handler, runConfig
// (phase-02 task 5): requirements.requirement.cli-config and the presentation
// half (solution.component.config-command) of domain.service.load-user-config.
//
// It drives runConfig directly and asserts the process contract: the
// get/set/unset/list round-trip; `get` of a set recognised key prints the value
// and exits 0; `get` of a recognised-but-unset key exits non-zero naming the
// key; a key outside the recognised set {default-template-dir} is a usage error
// (usage to stderr, exit 2, nothing written) for set/get/unset; a `set` missing
// its value, a missing subcommand, an unknown subcommand and a missing <key>
// are each usage errors (exit 2, usage to stderr listing the recognised key,
// nothing written); `unset` of a recognised-but-absent key exits 0; and `list`
// on a fresh config prints nothing and exits 0. One case drives Run through the
// existing runCLI helper to pin the `config` route.
//
// Every case isolates the store from the developer's real per-user config — and
// from every case in this process — by pointing HOME, and for portability
// XDG_CONFIG_HOME (Linux), AppData and USERPROFILE (Windows), at a fresh
// t.TempDir. It never touches the network and never reads or writes the live
// config.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configKeyDefaultTemplateDir is the sole recognised user-config key
// (internal/config's unexported defaultTemplateDirKey). It is spelled here so
// the tests assert the exact key the store and the CLI must agree on.
const configKeyDefaultTemplateDir = "default-template-dir"

// isolateConfig points every directory internal/config consults at fresh temp
// dirs so a test can never touch the developer's real per-user config. HOME
// drives os.UserConfigDir on macOS and os.UserHomeDir everywhere;
// XDG_CONFIG_HOME drives os.UserConfigDir on Linux; AppData drives it on
// Windows; USERPROFILE drives os.UserHomeDir on Windows. It returns the temp
// root and is restored by t.Setenv when the test (or subtest) ends.
func isolateConfig(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("AppData", filepath.Join(root, "appdata"))
	t.Setenv("USERPROFILE", root)
	return root
}

// userConfigFile is the config file path internal/config uses under the
// isolated environment: <os.UserConfigDir()>/kalide/config. It is derived from
// os.UserConfigDir so it stays correct on every platform.
func userConfigFile(t *testing.T) string {
	t.Helper()

	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error = %v", err)
	}
	return filepath.Join(dir, "kalide", "config")
}

// execConfig invokes runConfig with args (the tokens after `config`) and
// captures stdout and stderr, so a case asserts the handler's status code and
// both output streams.
func execConfig(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := runConfig(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// assertConfigUnwritten asserts that neither the config file nor its parent
// directory exists, i.e. a refused or read-only operation wrote nothing.
func assertConfigUnwritten(t *testing.T) {
	t.Helper()

	path := userConfigFile(t)
	if _, err := os.Stat(path); err == nil {
		t.Errorf("config file %s exists, want nothing written", path)
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Errorf("config directory %s exists, want nothing written", filepath.Dir(path))
	}
}

// TestConfigGetSetUnsetListRoundTrip covers the get/set/unset/list round-trip
// over one config file: `set` writes and exits 0, `get` prints the stored value
// and exits 0, `list` prints one key=value line and exits 0, and `unset`
// removes the key so a following get exits non-zero and list is empty again.
func TestConfigGetSetUnsetListRoundTrip(t *testing.T) {
	isolateConfig(t)

	// An absolute value is stored unchanged (path normalisation is the store's
	// concern; here the round-trip just needs a stable value).
	dir := t.TempDir()

	code, stdout, stderr := execConfig("set", configKeyDefaultTemplateDir, dir)
	if code != 0 {
		t.Fatalf("config set exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("config set stdout = %q, stderr = %q, want both empty", stdout, stderr)
	}

	// The first write creates the config file under the user config dir.
	if fi, err := os.Stat(userConfigFile(t)); err != nil {
		t.Fatalf("stat config file after set: %v, want created", err)
	} else if fi.IsDir() {
		t.Fatalf("config file is a directory, want a file")
	}

	code, stdout, stderr = execConfig("get", configKeyDefaultTemplateDir)
	if code != 0 {
		t.Fatalf("config get exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if want := dir + "\n"; stdout != want {
		t.Errorf("config get stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("config get stderr = %q, want empty", stderr)
	}

	code, stdout, stderr = execConfig("list")
	if code != 0 {
		t.Fatalf("config list exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if want := configKeyDefaultTemplateDir + "=" + dir + "\n"; stdout != want {
		t.Errorf("config list stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("config list stderr = %q, want empty", stderr)
	}

	// unset removes the key and exits 0.
	code, stdout, stderr = execConfig("unset", configKeyDefaultTemplateDir)
	if code != 0 {
		t.Fatalf("config unset exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("config unset stdout = %q, stderr = %q, want both empty", stdout, stderr)
	}

	// A following get exits non-zero naming the key, and list is empty again.
	code, stdout, stderr = execConfig("get", configKeyDefaultTemplateDir)
	if code == 0 {
		t.Fatalf("config get after unset exit = 0, want non-zero")
	}
	if !strings.Contains(stderr, configKeyDefaultTemplateDir) {
		t.Errorf("config get after unset stderr = %q, want it to name %q", stderr, configKeyDefaultTemplateDir)
	}
	if stdout != "" {
		t.Errorf("config get after unset stdout = %q, want empty", stdout)
	}

	code, stdout, stderr = execConfig("list")
	if code != 0 {
		t.Fatalf("config list after unset exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("config list after unset stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("config list after unset stderr = %q, want empty", stderr)
	}
}

// TestConfigGetUnsetKeyExitsNonZero covers the unset-get case: `get` of a
// recognised key that is not set exits non-zero with an error naming the key,
// prints nothing to stdout and writes nothing.
func TestConfigGetUnsetKeyExitsNonZero(t *testing.T) {
	isolateConfig(t)

	code, stdout, stderr := execConfig("get", configKeyDefaultTemplateDir)
	if code == 0 {
		t.Fatalf("config get on an unset key exit = 0, want non-zero")
	}
	if stdout != "" {
		t.Errorf("config get on an unset key stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, configKeyDefaultTemplateDir) {
		t.Errorf("config get on an unset key stderr = %q, want it to name %q", stderr, configKeyDefaultTemplateDir)
	}
	assertConfigUnwritten(t)
}

// TestConfigUnknownKeyExitsTwoWithUsage covers a key outside the recognised set
// for each of set/get/unset: exit 2, usage on stderr listing
// default-template-dir, nothing on stdout and nothing written.
func TestConfigUnknownKeyExitsTwoWithUsage(t *testing.T) {
	const other = "other-key"

	cases := []struct {
		sub  string
		args []string
	}{
		{sub: "set", args: []string{"set", other, "x"}},
		{sub: "get", args: []string{"get", other}},
		{sub: "unset", args: []string{"unset", other}},
	}
	for _, tc := range cases {
		t.Run(tc.sub, func(t *testing.T) {
			isolateConfig(t)

			code, stdout, stderr := execConfig(tc.args...)
			if code != 2 {
				t.Fatalf("config %s %s exit = %d, want 2 (stderr = %q)", tc.sub, other, code, stderr)
			}
			if stdout != "" {
				t.Errorf("config %s %s stdout = %q, want empty", tc.sub, other, stdout)
			}
			for _, want := range []string{"unrecognised", other, configKeyDefaultTemplateDir, "Usage:"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("config %s %s stderr = %q, want it to mention %q", tc.sub, other, stderr, want)
				}
			}
			assertConfigUnwritten(t)
		})
	}
}

// TestConfigSetMissingValueExitsTwoWithUsage covers `set` with a key but no
// value: exit 2, usage on stderr listing default-template-dir, nothing written.
func TestConfigSetMissingValueExitsTwoWithUsage(t *testing.T) {
	isolateConfig(t)

	code, stdout, stderr := execConfig("set", configKeyDefaultTemplateDir)
	if code != 2 {
		t.Fatalf("config set (missing value) exit = %d, want 2 (stderr = %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("config set (missing value) stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"missing value", configKeyDefaultTemplateDir, "Usage:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("config set (missing value) stderr = %q, want it to mention %q", stderr, want)
		}
	}
	assertConfigUnwritten(t)
}

// TestConfigMissingKeyExitsTwo covers a subcommand that takes a <key> invoked
// without one: exit 2, usage on stderr listing default-template-dir, nothing
// written.
func TestConfigMissingKeyExitsTwo(t *testing.T) {
	for _, sub := range []string{"get", "unset", "set"} {
		t.Run(sub, func(t *testing.T) {
			isolateConfig(t)

			code, stdout, stderr := execConfig(sub)
			if code != 2 {
				t.Fatalf("config %s (missing key) exit = %d, want 2 (stderr = %q)", sub, code, stderr)
			}
			if stdout != "" {
				t.Errorf("config %s (missing key) stdout = %q, want empty", sub, stdout)
			}
			for _, want := range []string{"missing key", configKeyDefaultTemplateDir, "Usage:"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("config %s (missing key) stderr = %q, want it to mention %q", sub, stderr, want)
				}
			}
			assertConfigUnwritten(t)
		})
	}
}

// TestConfigMissingSubcommandExitsTwoWithUsage covers a bare `kalide config`:
// exit 2, usage on stderr listing default-template-dir, nothing written.
func TestConfigMissingSubcommandExitsTwoWithUsage(t *testing.T) {
	isolateConfig(t)

	code, stdout, stderr := execConfig()
	if code != 2 {
		t.Fatalf("config (no subcommand) exit = %d, want 2 (stderr = %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("config (no subcommand) stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"missing subcommand", configKeyDefaultTemplateDir, "Usage:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("config (no subcommand) stderr = %q, want it to mention %q", stderr, want)
		}
	}
	assertConfigUnwritten(t)
}

// TestConfigUnknownSubcommandExitsTwoWithUsage covers an unknown subcommand:
// exit 2, usage on stderr naming the offending subcommand and listing
// default-template-dir, nothing written.
func TestConfigUnknownSubcommandExitsTwoWithUsage(t *testing.T) {
	isolateConfig(t)

	const sub = "frobnicate"
	code, stdout, stderr := execConfig(sub)
	if code != 2 {
		t.Fatalf("config %s exit = %d, want 2 (stderr = %q)", sub, code, stderr)
	}
	if stdout != "" {
		t.Errorf("config %s stdout = %q, want empty", sub, stdout)
	}
	for _, want := range []string{"unknown subcommand", sub, configKeyDefaultTemplateDir, "Usage:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("config %s stderr = %q, want it to mention %q", sub, stderr, want)
		}
	}
	assertConfigUnwritten(t)
}

// TestConfigUnsetAbsentKeyExitsZero covers unsetting a recognised-but-absent
// key: a no-op success, exit 0 with no output, that does not create the config
// file.
func TestConfigUnsetAbsentKeyExitsZero(t *testing.T) {
	isolateConfig(t)

	code, stdout, stderr := execConfig("unset", configKeyDefaultTemplateDir)
	if code != 0 {
		t.Fatalf("config unset (absent key) exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("config unset (absent key) stdout = %q, stderr = %q, want both empty", stdout, stderr)
	}
	assertConfigUnwritten(t)
}

// TestConfigListFreshIsEmpty covers `list` on a fresh config: exit 0 with
// nothing on stdout or stderr, and nothing written.
func TestConfigListFreshIsEmpty(t *testing.T) {
	isolateConfig(t)

	code, stdout, stderr := execConfig("list")
	if code != 0 {
		t.Fatalf("config list on a fresh config exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("config list on a fresh config stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("config list on a fresh config stderr = %q, want empty", stderr)
	}
	assertConfigUnwritten(t)
}

// TestRunConfigRoute pins that Run dispatches `kalide config` to runConfig and
// that the command works through the public entry point: a set followed by a
// get round-trips the value.
func TestRunConfigRoute(t *testing.T) {
	isolateConfig(t)

	dir := t.TempDir()

	code, stdout, stderr := runCLI("config", "set", configKeyDefaultTemplateDir, dir)
	if code != 0 {
		t.Fatalf("Run(config set) exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("Run(config set) stdout = %q, stderr = %q, want both empty", stdout, stderr)
	}

	code, stdout, stderr = runCLI("config", "get", configKeyDefaultTemplateDir)
	if code != 0 {
		t.Fatalf("Run(config get) exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if want := dir + "\n"; stdout != want {
		t.Errorf("Run(config get) stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("Run(config get) stderr = %q, want empty", stderr)
	}
}
