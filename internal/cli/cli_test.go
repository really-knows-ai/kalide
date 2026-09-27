package cli

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureLibraryDir is the phase-3 fixture library's project root, relative
// to this package, holding templates/slides/hello, templates/sections/item,
// templates/themes/plain and templates/media/logo.svg.
const fixtureLibraryDir = "../template/testdata/library"

// chdirFixtureLibrary copies the phase-3 fixture templates/ library into a
// fresh temp directory and chdirs the test into it (t.Chdir, restored when
// the test ends), so `kalide templates` has a project library to read.
func chdirFixtureLibrary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(fixtureLibraryDir, "templates")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dir, "templates", rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture templates/ into %s: %v", dir, err)
	}
	t.Chdir(dir)
	return dir
}

// testVersion is the build-stamped version injected into Run by runCLI. It
// stands in for the `main.version` value cmd/kalide sets via -ldflags -X.
const testVersion = "dev"

// runCLI invokes Run with a program name prepended, exactly as cmd/kalide does
// with os.Args, and returns the status code plus captured output. The version
// argument is testVersion; version-surface cases call Run directly to vary it.
func runCLI(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"kalide"}, args...), testVersion, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunNoArgsPrintsUsage(t *testing.T) {
	code, stdout, stderr := runCLI()
	if code != 0 {
		t.Fatalf("Run() exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Fatalf("Run() stdout = %q, want usage text", stdout)
	}
	if strings.Contains(strings.ToLower(stdout), "eypres") {
		t.Errorf("Run() stdout contains 'eypres': %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("Run() stderr = %q, want empty", stderr)
	}
}

func TestRunHelpPrintsUsage(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			code, stdout, stderr := runCLI(arg)
			if code != 0 {
				t.Fatalf("Run(%q) exit = %d, want 0", arg, code)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Fatalf("Run(%q) stdout = %q, want usage text", arg, stdout)
			}
			if strings.Contains(strings.ToLower(stdout), "eypres") {
				t.Errorf("Run(%q) stdout contains 'eypres': %q", arg, stdout)
			}
			if stderr != "" {
				t.Fatalf("Run(%q) stderr = %q, want empty", arg, stderr)
			}
		})
	}
}

// TestRunVersion pins the version surface: each of `version`, `--version` and
// `-v` prints exactly one line `kalide <version>\n` to stdout and returns 0,
// with nothing on stderr. The version string is injected in-process, standing
// in for the -ldflags -X stamp cmd/kalide applies to main.version.
func TestRunVersion(t *testing.T) {
	const version = "0.5.0"

	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"kalide", arg}, version, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("Run(%q) exit = %d, want 0", arg, code)
			}
			if got, want := stdout.String(), "kalide "+version+"\n"; got != want {
				t.Errorf("Run(%q) stdout = %q, want %q", arg, got, want)
			}
			if stderr.String() != "" {
				t.Errorf("Run(%q) stderr = %q, want empty", arg, stderr.String())
			}
		})
	}
}

// TestRunRoutesCommands asserts each recognised command reaches its handler.
//
// All three commands are real handlers now, so there is no stub table left:
// `init` routes to runInit (phase-8 task 3), `start` to runStart (phase-7
// task 6) and `templates` to runTemplates (phase-7 task 7). The start case
// validates the deck before it can serve and exits non-zero in a directory
// with no kalide.yaml; templates lists the built-ins (exit 0), documents one
// by name (exit 0) and rejects an unknown name (non-zero). The init case
// scaffolds a starter deck, so it is run in a throwaway temp directory and
// never in the package directory. Full per-command coverage lives in the
// phase-7 task 9 and phase-8 task 5 tests; here we only pin the routes.
func TestRunRoutesCommands(t *testing.T) {
	t.Run("init scaffolds and refuses a second run", func(t *testing.T) {
		// runInit resolves the current working directory with os.Getwd and
		// scaffold.Init writes slides/, assets/ and kalide.yaml there, so the
		// test must never run in the package directory. t.Chdir gives each
		// subtest its own temp dir and restores the original afterwards, which
		// also keeps the suite safe to run repeatedly (no stray deck dirs).
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code != 0 {
			t.Fatalf("Run(init) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init) stderr = %q, want empty", stderr)
		}
		for _, want := range []string{
			"kalide.yaml",
			"slides/1-hello.md",
			"templates/library.yaml",
			"templates/slides/hello/template.yaml",
			"templates/themes/default/theme.css",
			"assets/",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(init) stdout = %q, want created path %q", stdout, want)
			}
		}

		// The advertised paths must actually exist in the temp dir.
		for _, created := range []string{
			"kalide.yaml",
			"slides/1-hello.md",
			"templates/library.yaml",
			"templates/slides/hello/template.yaml",
			"templates/themes/default/theme.css",
			"assets",
		} {
			if _, err := os.Stat(filepath.Join(dir, created)); err != nil {
				t.Errorf("stat %s after init: %v, want the path created", created, err)
			}
		}

		// A second init in the same directory must refuse, name the blocking
		// path and leave the first deck untouched.
		code, stdout, stderr = runCLI("init")
		if code == 0 {
			t.Fatal("Run(init) second run exit = 0, want non-zero refusal")
		}
		if stdout != "" {
			t.Fatalf("Run(init) second run stdout = %q, want empty", stdout)
		}
		for _, want := range []string{"kalide.yaml", "slides/", "templates/", "assets/", "never overwrites"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("Run(init) second run stderr = %q, want refusal mentioning %q", stderr, want)
			}
		}
	})

	t.Run("start validates before serving", func(t *testing.T) {
		// A project with the fixture templates/ library but no kalide.yaml:
		// the library loads, then whole-deck validation fails
		// deterministically before any port is bound. This pins the route
		// (not the stub) and the phase-5 single-error format; the full start
		// behaviour is phase-7 task 9.
		chdirFixtureLibrary(t)

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatal("Run(start) exit = 0, want non-zero for a deck with no kalide.yaml")
		}
		if strings.Contains(stderr, "not implemented") {
			t.Fatalf("Run(start) stderr = %q, want the real runStart handler, not the phase-1 stub", stderr)
		}
		if !strings.Contains(stderr, "kalide.yaml") {
			t.Fatalf("Run(start) stderr = %q, want the first validation error naming kalide.yaml", stderr)
		}
		if strings.Contains(stdout, "Serving slides at") {
			t.Fatalf("Run(start) stdout = %q, want no server for an invalid deck", stdout)
		}
	})

	t.Run("start with valid eypres.yaml still fails naming kalide.yaml", func(t *testing.T) {
		dir := chdirFixtureLibrary(t)
		// Write a valid eypres.yaml - it must be ignored by kalide
		if err := os.WriteFile(filepath.Join(dir, "eypres.yaml"), []byte("title: Old Eypres Deck\ntheme: plain\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatal("Run(start) exit = 0 with only eypres.yaml, want non-zero failure")
		}
		if !strings.Contains(stderr, "kalide.yaml") {
			t.Fatalf("Run(start) stderr = %q, want error naming kalide.yaml", stderr)
		}
		if strings.Contains(stdout, "Serving slides at") {
			t.Fatalf("Run(start) stdout = %q, want no server started", stdout)
		}
	})

	t.Run("start fails cleanly without templates/", func(t *testing.T) {
		// No templates/ directory at all (no-built-in-fallback): the project
		// library fails to load before any deck validation is attempted.
		t.Chdir(t.TempDir())

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatal("Run(start) exit = 0, want non-zero with no templates/ directory")
		}
		if !strings.Contains(stderr, "templates") {
			t.Fatalf("Run(start) stderr = %q, want it to name the missing templates/ directory", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(start) stdout = %q, want empty", stdout)
		}
	})

	t.Run("templates list", func(t *testing.T) {
		chdirFixtureLibrary(t)
		code, stdout, stderr := runCLI("templates")
		if code != 0 {
			t.Fatalf("Run(templates) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stdout == "" {
			t.Fatal("Run(templates) stdout = \"\", want the project template list")
		}
		if !strings.Contains(stdout, "hello") {
			t.Errorf("Run(templates) stdout = %q, want the fixture %q template", stdout, "hello")
		}
		if stderr != "" {
			t.Fatalf("Run(templates) stderr = %q, want empty", stderr)
		}
	})

	t.Run("templates show", func(t *testing.T) {
		chdirFixtureLibrary(t)
		code, stdout, stderr := runCLI("templates", "hello")
		if code != 0 {
			t.Fatalf("Run(templates hello) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stdout == "" {
			t.Fatal("Run(templates hello) stdout = \"\", want the hello template documented")
		}
		if stderr != "" {
			t.Fatalf("Run(templates hello) stderr = %q, want empty", stderr)
		}
	})

	t.Run("templates unknown", func(t *testing.T) {
		chdirFixtureLibrary(t)
		code, stdout, stderr := runCLI("templates", "no-such-template")
		if code == 0 {
			t.Fatal("Run(templates no-such-template) exit = 0, want non-zero")
		}
		if !strings.Contains(stderr, "unknown template") {
			t.Fatalf("Run(templates no-such-template) stderr = %q, want an unknown-template error", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(templates no-such-template) stdout = %q, want empty", stdout)
		}
	})
}

func TestRunTemplatesRejectsExtraArgs(t *testing.T) {
	code, stdout, stderr := runCLI("templates", "a", "b")
	if code != 2 {
		t.Fatalf("Run(templates a b) exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "at most one") {
		t.Fatalf("Run(templates a b) stderr = %q, want an argument-count error", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("Run(templates a b) stderr = %q, want usage text", stderr)
	}
	if stdout != "" {
		t.Fatalf("Run(templates a b) stdout = %q, want empty", stdout)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	for _, args := range [][]string{{"frobnicate"}, {"frobnicate", "extra"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := runCLI(args...)
			if code == 0 {
				t.Fatalf("Run(%q) exit = 0, want non-zero", args)
			}
			if code != 2 {
				t.Fatalf("Run(%q) exit = %d, want 2", args, code)
			}
			if !strings.Contains(stderr, "unknown command") {
				t.Fatalf("Run(%q) stderr = %q, want an unknown-command error", args, stderr)
			}
			if !strings.Contains(stderr, "frobnicate") {
				t.Fatalf("Run(%q) stderr = %q, want the offending command named", args, stderr)
			}
			if !strings.Contains(stderr, "Usage:") {
				t.Fatalf("Run(%q) stderr = %q, want usage text", args, stderr)
			}
			if stdout != "" {
				t.Fatalf("Run(%q) stdout = %q, want empty", args, stdout)
			}
		})
	}
}
