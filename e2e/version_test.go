package e2e

// This file is the phase-01 task-5 end-to-end test for the build-stamped
// version surface (cli.Run's `version`, `--version` and `-v`).
//
// Harness.build compiles cmd/kalide with a plain `go build`, no -ldflags, so
// the harness-built binary reports the compiled-in default "dev". The test:
//
//   - runs `version` on that unstamped harness build and asserts exactly
//     "kalide dev" on stdout and exit 0;
//   - compiles cmd/kalide a second time itself, CGO_ENABLED=0 with
//     -ldflags "-X main.version=<sentinel>" into a t.TempDir(), then runs
//     `version`, `--version` and `-v` against it and asserts each prints
//     exactly "kalide <sentinel>" and exits 0 — proving the -ldflags stamp
//     reaches the user-facing surface regardless of spelling.
//
// When KALIDE_BINARY is set, native CI has supplied an already-built binary
// whose version was stamped by the release build (or left as "dev"), so there
// is no local sentinel to build or compare against: the test runs the three
// version spellings against that prebuilt binary and asserts the stable
// "kalide " prefix instead, and the unstamped "kalide dev" subcase is skipped.
//
// The test builds a binary (or runs one via KALIDE_BINARY), so it is skipped
// under -short.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// versionSentinel is a version string distinctive enough that observing it in
// a binary's output can only come from -ldflags -X main.version.
const versionSentinel = "e2e-version-sentinel"

// versionSpellings are the three command-line aliases cli.Run accepts for the
// version surface.
var versionSpellings = []string{"version", "--version", "-v"}

// TestVersionStampedBuild is the build-stamping end-to-end test described at
// the top of this file.
func TestVersionStampedBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds the real kalide binary")
	}

	h := NewHarness(t)

	if os.Getenv(prebuiltBinaryEnv) != "" {
		// The prebuilt binary's version is whatever CI stamped (or the
		// compiled-in default), so only the stable prefix is assertable.
		for _, arg := range versionSpellings {
			assertVersionPrefix(t, h, arg, "kalide ")
		}
		return
	}

	// The harness build carried no ldflags: its version is the default "dev".
	assertVersionOutput(t, h, "version", "kalide dev\n")

	// Point the harness at a freshly stamped build and drive the same surface
	// with the sentinel; Run keeps the clean working directory and scrubbed
	// environment.
	h.binPath = buildStamped(t, h, versionSentinel)
	for _, arg := range versionSpellings {
		assertVersionOutput(t, h, arg, "kalide "+versionSentinel+"\n")
	}
}

// buildStamped compiles cmd/kalide CGO-free with the given version stamped via
// -ldflags -X main.version into a fresh temporary directory and returns the
// binary path. It mirrors Harness.build but adds the stamp the harness build
// deliberately omits.
func buildStamped(t *testing.T, h *Harness, version string) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), binaryName())
	ldflags := "-X main.version=" + version

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, goCommand(), "build", "-o", bin,
		"-ldflags", ldflags, "./cmd/kalide")
	cmd.Dir = h.moduleRoot
	cmd.Env = envWith(os.Environ(), "CGO_ENABLED", "0")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("e2e: build stamped kalide (CGO_ENABLED=0 -ldflags %q): %v\n%s",
			ldflags, err, out)
	}
	return bin
}

// assertVersionOutput runs `kalide arg` through the harness and asserts the
// exact stdout line, an empty stderr and exit 0.
func assertVersionOutput(t *testing.T, h *Harness, arg, want string) {
	t.Helper()

	stdout, stderr, code := h.Run(arg)
	if code != 0 {
		t.Errorf("kalide %s: exit = %d, want 0 (stderr = %q)", arg, code, stderr)
	}
	if stdout != want {
		t.Errorf("kalide %s: stdout = %q, want %q", arg, stdout, want)
	}
	if stderr != "" {
		t.Errorf("kalide %s: stderr = %q, want empty", arg, stderr)
	}
}

// assertVersionPrefix runs `kalide arg` through the harness and asserts stdout
// begins with prefix, stderr is empty and the exit code is 0. It is the
// prebuilt-binary variant, where the exact version is not known locally.
func assertVersionPrefix(t *testing.T, h *Harness, arg, prefix string) {
	t.Helper()

	stdout, stderr, code := h.Run(arg)
	if code != 0 {
		t.Errorf("kalide %s: exit = %d, want 0 (stderr = %q)", arg, code, stderr)
	}
	if !strings.HasPrefix(stdout, prefix) {
		t.Errorf("kalide %s: stdout = %q, want prefix %q", arg, stdout, prefix)
	}
	if stderr != "" {
		t.Errorf("kalide %s: stderr = %q, want empty", arg, stderr)
	}
}
