package cli

// This file unit-tests the `eypres init` command surface (phase-8 task 5):
// requirements.requirement.cli-init and
// requirements.requirement.cli-init-refuse-existing.
//
// It exercises Run's `init` route through the runCLI helper and asserts the
// process contract, not scaffold's internals: exit 0 and the created-path list
// on an empty directory; a non-zero exit with scaffold's refusal message naming
// the blocking path(s), and no overwrite, when a deck path is already present;
// a usage error (exit 2) for any extra argument, because there is no --force;
// and a refusal on the second run. Every case runs in a t.TempDir with t.Chdir,
// so the package directory is never written to and the suite is repeatable.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initCreatedPaths are the entries `eypres init` advertises and must actually
// create on an empty directory. "assets/" is the printed form; "assets" is the
// filesystem form.
var initCreatedPaths = []string{
	"eypres.yaml",
	"slides/1-hello.md",
	"templates/library.yaml",
	"templates/slides/hello/template.yaml",
	"templates/slides/hello/layout.html.tmpl",
	"templates/slides/hello/example.md",
	"templates/themes/default/theme.css",
}

// TestInit covers `eypres init` end to end from the command surface: success,
// each blocking path, the rejected extra argument (no --force), and a repeated
// run.
func TestInit(t *testing.T) {
	t.Run("empty dir creates the starter deck", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code != 0 {
			t.Fatalf("Run(init) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init) stderr = %q, want empty", stderr)
		}

		// The success text lists every created path, spelled deck-relative.
		for _, want := range append(append([]string{}, initCreatedPaths...), "assets/") {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(init) stdout = %q, want created path %q", stdout, want)
			}
		}

		// And each advertised path is really on disk.
		if fi, err := os.Stat(filepath.Join(dir, "eypres.yaml")); err != nil {
			t.Errorf("stat eypres.yaml after init: %v, want created", err)
		} else if fi.IsDir() {
			t.Errorf("eypres.yaml is a directory, want a file")
		}
		for _, created := range initCreatedPaths[1:] {
			if fi, err := os.Stat(filepath.Join(dir, created)); err != nil {
				t.Errorf("stat %s after init: %v, want created", created, err)
			} else if fi.IsDir() {
				t.Errorf("%s is a directory, want a file", created)
			}
		}
		if fi, err := os.Stat(filepath.Join(dir, "assets")); err != nil {
			t.Errorf("stat assets after init: %v, want created", err)
		} else if !fi.IsDir() {
			t.Errorf("assets is not a directory, want a directory")
		}
	})

	// Each deck path blocks init on its own. Scaffold checks the three in a
	// fixed order; with only one present the refusal must name exactly it. The
	// pre-existing entry carries a sentinel so we can prove init overwrote
	// nothing.
	blockers := []struct {
		name    string // subtest name
		path    string // filesystem path to pre-create
		isDir   bool   // create a directory (with a sentinel inside) vs a file
		display string // how the refusal names it
	}{
		{name: "slides dir exists", path: "slides", isDir: true, display: "slides/"},
		{name: "templates dir exists", path: "templates", isDir: true, display: "templates/"},
		{name: "assets dir exists", path: "assets", isDir: true, display: "assets/"},
		{name: "eypres.yaml file exists", path: "eypres.yaml", isDir: false, display: "eypres.yaml"},
	}
	for _, b := range blockers {
		t.Run(b.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			const sentinel = "sentinel content that init must not touch\n"
			blocking := filepath.Join(dir, b.path)
			if b.isDir {
				if err := os.MkdirAll(blocking, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(blocking, "keep.txt"), []byte(sentinel), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(blocking, []byte(sentinel), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			code, stdout, stderr := runCLI("init")
			if code == 0 {
				t.Fatalf("Run(init) exit = 0 with %s present, want non-zero refusal", b.path)
			}
			if stdout != "" {
				t.Fatalf("Run(init) stdout = %q, want empty on refusal", stdout)
			}
			// scaffold's message names the command and the blocking path and
			// says init never overwrites.
			for _, want := range []string{"init:", b.display, "already present", "never overwrites"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("Run(init) stderr = %q, want refusal mentioning %q", stderr, want)
				}
			}

			// The pre-existing entry is untouched...
			if b.isDir {
				got, err := os.ReadFile(filepath.Join(blocking, "keep.txt"))
				if err != nil || string(got) != sentinel {
					t.Errorf("sentinel in %s changed: content = %q, err = %v; want original", b.path, got, err)
				}
			} else {
				got, err := os.ReadFile(blocking)
				if err != nil || string(got) != sentinel {
					t.Errorf("%s changed: content = %q, err = %v; want original", b.path, got, err)
				}
			}
			// ...and init wrote nothing at all: no other deck path appeared.
			for _, other := range []string{"eypres.yaml", "slides", "templates", "assets"} {
				if other == b.path {
					continue
				}
				if _, err := os.Lstat(filepath.Join(dir, other)); err == nil {
					t.Errorf("%s was created despite the refusal, want nothing written", other)
				}
			}
		})
	}

	t.Run("multiple blocking paths are all named", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		for _, p := range []string{"slides", "assets", "eypres.yaml"} {
			if p == "eypres.yaml" {
				if err := os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
				t.Fatal(err)
			}
		}

		code, _, stderr := runCLI("init")
		if code == 0 {
			t.Fatal("Run(init) exit = 0 with all deck paths present, want non-zero")
		}
		for _, want := range []string{"slides/", "assets/", "eypres.yaml"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("Run(init) stderr = %q, want all blocking paths named, missing %q", stderr, want)
			}
		}
	})

	t.Run("extra argument is rejected because there is no --force", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "--force")
		if code != 2 {
			t.Fatalf("Run(init --force) exit = %d, want 2 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stderr, "expected no arguments") {
			t.Errorf("Run(init --force) stderr = %q, want an argument-count error", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(init --force) stderr = %q, want usage text", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(init --force) stdout = %q, want empty", stdout)
		}
		// No force behaviour: the rejected flag must not have scaffolded
		// anything into the still-empty directory.
		for _, p := range []string{"eypres.yaml", "slides", "templates", "assets"} {
			if _, err := os.Lstat(filepath.Join(dir, p)); err == nil {
				t.Errorf("%s was created by `init --force`, want the argument rejected with no writes", p)
			}
		}
	})

	t.Run("second run refuses", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		if code, _, stderr := runCLI("init"); code != 0 {
			t.Fatalf("first Run(init) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		code, stdout, stderr := runCLI("init")
		if code == 0 {
			t.Fatal("second Run(init) exit = 0, want non-zero refusal")
		}
		if stdout != "" {
			t.Fatalf("second Run(init) stdout = %q, want empty", stdout)
		}
		for _, want := range []string{"slides/", "templates/", "assets/", "eypres.yaml", "never overwrites"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("second Run(init) stderr = %q, want refusal mentioning %q", stderr, want)
			}
		}
	})
}
