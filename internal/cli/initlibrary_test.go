package cli

// This file unit-tests the `kalide init-library` command surface (phase-04
// task-8): requirements.requirement.init-library.
//
// It exercises Run's `init-library` route through the runCLI helper and asserts
// the process contract, not scaffold's internals: a missing <path> is a usage
// error (usage to stderr, exit 2, nothing written); a valid path scaffolds the
// library and exits 0 listing every created path; extra arguments and an
// option-looking token are usage errors; a refusal (a pre-existing library.yaml
// or layout entry) and a non-directory target exit non-zero with a clear
// message and nothing overwritten; and Run dispatches init-library to
// runInitLibrary (its created-library message, never an unknown-command error).
// Every case runs in a t.TempDir with t.Chdir, so the package directory is
// never written to.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/theme"
)

// initLibraryCreatedPaths are the entries `kalide init-library` advertises and
// must actually create, spelled as the handler prints them (the empty
// directories with a trailing slash). The library root itself is the target.
var initLibraryCreatedPaths = []string{
	"library.yaml",
	"slides/",
	"sections/",
	"media/",
	"themes/default/theme.css",
}

// TestInitLibraryCommand covers `kalide init-library` from the command surface:
// the argument-count and option usage errors, success, the refusal matrix and
// the non-directory target, and Run's dispatch.
func TestInitLibraryCommand(t *testing.T) {
	t.Run("no path is a usage error writing nothing", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init-library")
		if code != 2 {
			t.Fatalf("Run(init-library) exit = %d, want 2 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stderr, "expected exactly one library path") {
			t.Errorf("Run(init-library) stderr = %q, want an argument-count error", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(init-library) stderr = %q, want usage text", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(init-library) stdout = %q, want empty", stdout)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("valid path scaffolds the library and lists the created paths", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init-library", "my-lib")
		if code != 0 {
			t.Fatalf("Run(init-library my-lib) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init-library my-lib) stderr = %q, want empty", stderr)
		}

		// The success text names the target and lists every created path.
		for _, want := range append([]string{"my-lib"}, initLibraryCreatedPaths...) {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(init-library my-lib) stdout = %q, want created path %q", stdout, want)
			}
		}

		target := filepath.Join(dir, "my-lib")
		if fi, err := os.Stat(filepath.Join(target, "library.yaml")); err != nil {
			t.Errorf("stat library.yaml after init-library: %v, want created", err)
		} else if fi.IsDir() {
			t.Errorf("library.yaml is a directory, want a file")
		}
		for _, d := range []string{"slides", "sections", "media"} {
			assertInitLibraryDirEmpty(t, filepath.Join(target, d))
		}
		style := filepath.Join(target, template.ThemesDir, theme.DefaultName, template.ThemeStylesheet)
		if fi, err := os.Stat(style); err != nil {
			t.Errorf("stat themes/default/theme.css after init-library: %v, want created", err)
		} else if fi.IsDir() {
			t.Errorf("themes/default/theme.css is a directory, want a file")
		}
	})

	t.Run("extra arguments are a usage error", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init-library", "a", "b")
		if code != 2 {
			t.Fatalf("Run(init-library a b) exit = %d, want 2 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stderr, "expected exactly one library path") {
			t.Errorf("Run(init-library a b) stderr = %q, want an argument-count error", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(init-library a b) stderr = %q, want usage text", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(init-library a b) stdout = %q, want empty", stdout)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("option-looking argument is a usage error", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init-library", "--force")
		if code != 2 {
			t.Fatalf("Run(init-library --force) exit = %d, want 2 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stderr, "unexpected option") {
			t.Errorf("Run(init-library --force) stderr = %q, want an unexpected-option error", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(init-library --force) stderr = %q, want usage text", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(init-library --force) stdout = %q, want empty", stdout)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("existing library.yaml refuses non-zero and overwrites nothing", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		target := filepath.Join(dir, "my-lib")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		const sentinel = "name: existing\nformat: 1\n"
		if err := os.WriteFile(filepath.Join(target, "library.yaml"), []byte(sentinel), 0o644); err != nil {
			t.Fatal(err)
		}

		code, stdout, stderr := runCLI("init-library", "my-lib")
		if code == 0 {
			t.Fatalf("Run(init-library my-lib) exit = 0 with library.yaml present, want non-zero")
		}
		if stdout != "" {
			t.Fatalf("Run(init-library my-lib) stdout = %q, want empty on refusal", stdout)
		}
		for _, want := range []string{"init-library:", "library.yaml", "already present", "never overwrites"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("Run(init-library my-lib) stderr = %q, want refusal mentioning %q", stderr, want)
			}
		}
		if got, err := os.ReadFile(filepath.Join(target, "library.yaml")); err != nil || string(got) != sentinel {
			t.Errorf("library.yaml changed: content = %q, err = %v; want the pre-existing content", got, err)
		}
		for _, other := range []string{"slides", "sections", "media", "themes"} {
			if _, err := os.Lstat(filepath.Join(target, other)); err == nil {
				t.Errorf("%s was created despite the refusal, want nothing written", other)
			}
		}
	})

	t.Run("existing layout entry refuses non-zero and overwrites nothing", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		target := filepath.Join(dir, "my-lib")
		if err := os.MkdirAll(filepath.Join(target, "slides"), 0o755); err != nil {
			t.Fatal(err)
		}
		const sentinel = "keep me\n"
		if err := os.WriteFile(filepath.Join(target, "slides", "keep.txt"), []byte(sentinel), 0o644); err != nil {
			t.Fatal(err)
		}

		code, stdout, stderr := runCLI("init-library", "my-lib")
		if code == 0 {
			t.Fatalf("Run(init-library my-lib) exit = 0 with slides/ present, want non-zero")
		}
		if stdout != "" {
			t.Fatalf("Run(init-library my-lib) stdout = %q, want empty on refusal", stdout)
		}
		for _, want := range []string{"init-library:", "slides/", "already present", "never overwrites"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("Run(init-library my-lib) stderr = %q, want refusal mentioning %q", stderr, want)
			}
		}
		if got, err := os.ReadFile(filepath.Join(target, "slides", "keep.txt")); err != nil || string(got) != sentinel {
			t.Errorf("slides/keep.txt changed: content = %q, err = %v; want the pre-existing content", got, err)
		}
		if _, err := os.Lstat(filepath.Join(target, "library.yaml")); err == nil {
			t.Errorf("library.yaml was created despite the refusal, want nothing written")
		}
	})

	t.Run("non-directory target exits non-zero and is left untouched", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		const sentinel = "i am a file, not a library directory\n"
		if err := os.WriteFile(filepath.Join(dir, "my-lib"), []byte(sentinel), 0o644); err != nil {
			t.Fatal(err)
		}

		code, stdout, stderr := runCLI("init-library", "my-lib")
		if code == 0 {
			t.Fatalf("Run(init-library my-lib) exit = 0 on a non-directory target, want non-zero")
		}
		if stdout != "" {
			t.Fatalf("Run(init-library my-lib) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "is not a directory") {
			t.Errorf("Run(init-library my-lib) stderr = %q, want a non-directory error", stderr)
		}
		if got, err := os.ReadFile(filepath.Join(dir, "my-lib")); err != nil || string(got) != sentinel {
			t.Errorf("target file = %q, err = %v; want it unchanged", got, err)
		}
	})

	t.Run("Run dispatches init-library to runInitLibrary", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init-library", "dispatch-lib")
		if code != 0 {
			t.Fatalf("Run(init-library dispatch-lib) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stdout, "Created a template library at") {
			t.Errorf("Run(init-library dispatch-lib) stdout = %q, want runInitLibrary's created-library message", stdout)
		}
		if strings.Contains(stderr, "unknown command") {
			t.Errorf("Run(init-library dispatch-lib) stderr = %q, want init-library dispatched, not unknown", stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, "dispatch-lib", "library.yaml")); err != nil {
			t.Errorf("stat dispatch-lib/library.yaml after Run: %v, want created", err)
		}
	})
}

// assertInitLibraryDirEmpty asserts dir exists, is a directory, and contains no
// entries, so a scaffolded library's content directories are really empty.
func assertInitLibraryDirEmpty(t *testing.T, dir string) {
	t.Helper()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v, want a directory", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("%s contains %v, want an empty directory", dir, entries)
	}
}
