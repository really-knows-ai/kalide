package e2e

// This file is the public-install end-to-end test for anonymous package
// manager installation now that github.com/really-knows-ai/kalide is public.
//
// TestAnonymousPackageInstall installs kalide through the platform package
// manager — Homebrew on macOS, Scoop on Windows — from the public repository
// (https://github.com/really-knows-ai/kalide, used as both the Homebrew tap and
// the Scoop bucket) with every GitHub token/credential variable stripped from
// the subprocess environment (anonymousEnv). The macOS flow taps the public
// repository, trusts the tap (`brew trust really-knows-ai/kalide`, Homebrew
// 6.0.0+ tap-level trust), then installs kalide; the Windows flow adds the
// bucket, then installs kalide. It then runs the installed kalide's `version`
// (installedVersion) and asserts it reports the expected release version from
// KALIDE_RELEASE_VERSION.
//
// Linux has no package-manager route (manual download is the Linux install),
// so the test skips there. It also skips under -short, when brew/scoop is not
// on PATH, or when KALIDE_RELEASE_VERSION is unset. Cleanup uninstalls the
// package and removes the tap/bucket, untrusting the Homebrew tap first.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// publicRepoURL is the public kalide repository, which is both the Homebrew
// tap and the Scoop bucket (HTTPS, no .git suffix).
const publicRepoURL = "https://github.com/really-knows-ai/kalide"

// releaseVersionEnv names the variable carrying the expected release version.
const releaseVersionEnv = "KALIDE_RELEASE_VERSION"

// tokenEnvVars are the credential variables that must not reach the install.
var tokenEnvVars = []string{
	"HOMEBREW_GITHUB_API_TOKEN",
	"KALIDE_GITHUB_TOKEN",
	"GITHUB_TOKEN",
	"GH_TOKEN",
}

// TestAnonymousPackageInstall is the anonymous install test described at the
// top of this file.
func TestAnonymousPackageInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e install test runs a real package manager install")
	}
	want := os.Getenv(releaseVersionEnv)
	if want == "" {
		t.Skipf("%s unset: no expected release version", releaseVersionEnv)
	}

	var mgr string
	var setup, teardown [][]string
	switch runtime.GOOS {
	case "darwin":
		mgr = "brew"
		setup = [][]string{
			{"brew", "tap", "really-knows-ai/kalide", publicRepoURL},
			{"brew", "trust", "really-knows-ai/kalide"},
			{"brew", "install", "kalide"},
		}
		teardown = [][]string{
			{"brew", "uninstall", "kalide"},
			{"brew", "untrust", "really-knows-ai/kalide"},
			{"brew", "untap", "really-knows-ai/kalide"},
		}
	case "windows":
		mgr = "scoop"
		setup = [][]string{
			{"scoop", "bucket", "add", "kalide", publicRepoURL},
			{"scoop", "install", "kalide"},
		}
		teardown = [][]string{
			{"scoop", "uninstall", "kalide"},
			{"scoop", "bucket", "rm", "kalide"},
		}
	default:
		t.Skipf("no package-manager install route on %s", runtime.GOOS)
	}
	if _, err := exec.LookPath(mgr); err != nil {
		t.Skipf("%s not on PATH: %v", mgr, err)
	}

	env := anonymousEnv()
	run := func(args []string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, v := range tokenEnvVars {
		for _, kv := range env {
			if strings.HasPrefix(strings.ToUpper(kv), v+"=") {
				t.Fatalf("anonymousEnv leaked %s", v)
			}
		}
	}

	t.Cleanup(func() {
		for _, args := range teardown {
			if out, err := run(args); err != nil {
				t.Logf("cleanup %q: %v\n%s", args, err, out)
			}
		}
	})
	for _, args := range setup {
		if out, err := run(args); err != nil {
			t.Fatalf("anonymous %q failed: %v\n%s", args, err, out)
		}
	}

	got, err := installedVersion(env)
	if err != nil {
		t.Fatalf("installed kalide version: %v", err)
	}
	if strings.TrimPrefix(got, "v") != strings.TrimPrefix(want, "v") {
		t.Errorf("installed kalide version = %q, want %q", got, want)
	}
}

// anonymousEnv returns the current process environment with every GitHub
// token/credential variable removed, so installs run unauthenticated.
func anonymousEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, v := range tokenEnvVars {
			if strings.EqualFold(name, v) {
				drop = true
				break
			}
		}
		if !drop {
			env = append(env, kv)
		}
	}
	return env
}

// installedVersion runs the package-manager-installed kalide (resolved on
// PATH) with `version` under env and returns the version string after
// "kalide ".
func installedVersion(env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kalide", "version")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kalide version: %v: %s", err, out)
	}
	line := strings.TrimSpace(string(out))
	if !strings.HasPrefix(line, "kalide ") {
		return "", fmt.Errorf("unexpected kalide version output %q", out)
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "kalide ")), nil
}
