package cli

// This file unit-tests the `kalide init` command surface (phase-8 task 5):
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

	"gopkg.in/yaml.v3"
)

// initCreatedPaths are the entries `kalide init` advertises and must actually
// create on an empty directory. "assets/" is the printed form; "assets" is the
// filesystem form.
var initCreatedPaths = []string{
	"kalide.yaml",
	"slides/1-hello.md",
	"templates/library.yaml",
	"templates/slides/hello/template.yaml",
	"templates/slides/hello/layout.html.tmpl",
	"templates/slides/hello/example.md",
	"templates/themes/default/theme.css",
}

// TestInit covers `kalide init` end to end from the command surface: success,
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
		if fi, err := os.Stat(filepath.Join(dir, "kalide.yaml")); err != nil {
			t.Errorf("stat kalide.yaml after init: %v, want created", err)
		} else if fi.IsDir() {
			t.Errorf("kalide.yaml is a directory, want a file")
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
		if _, err := os.Stat(filepath.Join(dir, "eypres.yaml")); err == nil {
			t.Errorf("init unexpectedly produced eypres.yaml")
		}
		if strings.Contains(strings.ToLower(stdout), "eypres") {
			t.Errorf("Run(init) stdout contains 'eypres': %q", stdout)
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
		{name: "kalide.yaml file exists", path: "kalide.yaml", isDir: false, display: "kalide.yaml"},
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
			for _, other := range []string{"kalide.yaml", "slides", "templates", "assets"} {
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
		for _, p := range []string{"slides", "assets", "kalide.yaml"} {
			if p == "kalide.yaml" {
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
		for _, want := range []string{"slides/", "assets/", "kalide.yaml"} {
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
		for _, p := range []string{"kalide.yaml", "slides", "templates", "assets"} {
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
		for _, want := range []string{"slides/", "templates/", "assets/", "kalide.yaml", "never overwrites"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("second Run(init) stderr = %q, want refusal mentioning %q", stderr, want)
			}
		}
	})

	t.Run("init in temp dirs never produces eypres.yaml and its output contains no eypres", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code != 0 {
			t.Fatalf("Run(init) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if strings.Contains(strings.ToLower(stdout), "eypres") {
			t.Errorf("Run(init) stdout contains 'eypres': %q", stdout)
		}
		if strings.Contains(strings.ToLower(stderr), "eypres") {
			t.Errorf("Run(init) stderr contains 'eypres': %q", stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, "eypres.yaml")); err == nil {
			t.Errorf("init produced eypres.yaml, want only kalide.yaml")
		}
	})
}

// TestInitExternalLibrary covers the `kalide init [path]` command surface
// (phase-03 task-8): requirements.requirement.cli-init and
// requirements.requirement.cli-init-refuse-existing.
//
// It exercises Run's `init` route through runCLI and asserts the process
// contract: the optional external path is parsed and scaffolds a deck that
// references it (stdout lists the created paths, exit 0); extra arguments are a
// usage error (exit 2, nothing written); an invalid or missing external
// library, or a library that does not resolve the deck's default theme, exits
// non-zero with nothing written; a pre-existing local templates/ is exempt;
// and the no-arg seed path is unchanged. Every case runs in a t.TempDir with
// t.Chdir, so the package directory is never written to.
func TestInitExternalLibrary(t *testing.T) {
	t.Run("optional path scaffolds an external deck", func(t *testing.T) {
		parent, _ := newInitExternalLibrary(t)
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "../shared-lib")
		if code != 0 {
			t.Fatalf("Run(init ../shared-lib) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init ../shared-lib) stderr = %q, want empty", stderr)
		}

		// The created-file output names the external library and the three
		// entries InitExternal writes, and never a local templates/ seed.
		for _, want := range []string{
			"../shared-lib",
			"kalide.yaml",
			"slides/",
			"assets/",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(init ../shared-lib) stdout = %q, want created path %q", stdout, want)
			}
		}
		if strings.Contains(stdout, "templates/library.yaml") || strings.Contains(stdout, "slides/1-hello.md") {
			t.Errorf("Run(init ../shared-lib) stdout = %q, want no local templates/ or starter-slide entry", stdout)
		}

		// The deck references the external library and has the minimal empty
		// slides/ and assets/, with no local templates/.
		if _, templates := initConfigTemplates(t, dir); templates != "../shared-lib" {
			t.Errorf("kalide.yaml templates = %q, want %q", templates, "../shared-lib")
		}
		for _, name := range []string{"slides", "assets"} {
			fi, err := os.Stat(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("stat %s after init: %v, want created", name, err)
			}
			if !fi.IsDir() {
				t.Errorf("%s is not a directory, want a directory", name)
			}
			entries, err := os.ReadDir(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("ReadDir(%s): %v", name, err)
			}
			if len(entries) != 0 {
				t.Errorf("%s/ contains %d entries, want an empty directory", name, len(entries))
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "templates")); err == nil {
			t.Errorf("local templates/ was created by `init ../shared-lib`, want none")
		}
		if _, err := os.Stat(filepath.Join(dir, "slides", "1-hello.md")); err == nil {
			t.Errorf("starter slide slides/1-hello.md was written, want none")
		}
	})

	t.Run("extra arguments are a usage error", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "a", "b")
		if code != 2 {
			t.Fatalf("Run(init a b) exit = %d, want 2 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stderr, "at most one") {
			t.Errorf("Run(init a b) stderr = %q, want an at-most-one-path error", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(init a b) stderr = %q, want usage text", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(init a b) stdout = %q, want empty", stdout)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("option-looking argument is a usage error", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "--force")
		if code != 2 {
			t.Fatalf("Run(init --force) exit = %d, want 2 (stderr = %q)", code, stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(init --force) stderr = %q, want usage text", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(init --force) stdout = %q, want empty", stdout)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("missing external library exits non-zero with nothing written", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "../nope")
		if code == 0 {
			t.Fatalf("Run(init ../nope) exit = 0, want non-zero for a missing library")
		}
		if stdout != "" {
			t.Fatalf("Run(init ../nope) stdout = %q, want empty", stdout)
		}
		if want := filepath.Join(parent, "nope"); !strings.Contains(stderr, want) {
			t.Errorf("Run(init ../nope) stderr = %q, want it to name the resolved path %q", stderr, want)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("invalid external library exits non-zero with nothing written", func(t *testing.T) {
		parent, libDir := newInitExternalLibrary(t)
		writeExternalFile(t, filepath.Join(libDir, "library.yaml"), "name: Bad_Name\nformat: 1\n")
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "../shared-lib")
		if code == 0 {
			t.Fatalf("Run(init ../shared-lib) exit = 0, want non-zero for an invalid library")
		}
		if stdout != "" {
			t.Fatalf("Run(init ../shared-lib) stdout = %q, want empty", stdout)
		}
		if want := filepath.Join(libDir, "library.yaml"); !strings.Contains(stderr, want) {
			t.Errorf("Run(init ../shared-lib) stderr = %q, want it to name %q", stderr, want)
		}
		if !strings.Contains(stderr, "not a valid name") {
			t.Errorf("Run(init ../shared-lib) stderr = %q, want the library.yaml name error", stderr)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("unresolvable default theme exits non-zero with nothing written", func(t *testing.T) {
		parent, libDir := newInitExternalLibrary(t)
		if err := os.RemoveAll(filepath.Join(libDir, "themes", "default")); err != nil {
			t.Fatalf("remove themes/default: %v", err)
		}
		writeExternalFile(t, filepath.Join(libDir, "themes", "plain", "theme.css"), "body {}\n")
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "../shared-lib")
		if code == 0 {
			t.Fatalf("Run(init ../shared-lib) exit = 0, want non-zero when the default theme does not resolve")
		}
		if stdout != "" {
			t.Fatalf("Run(init ../shared-lib) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "default") {
			t.Errorf("Run(init ../shared-lib) stderr = %q, want it to name the unresolvable default theme", stderr)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("pre-existing local templates is exempt and left untouched", func(t *testing.T) {
		parent, _ := newInitExternalLibrary(t)
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
			t.Fatal(err)
		}
		const sentinel = "local templates content that init must not touch\n"
		if err := os.WriteFile(filepath.Join(dir, "templates", "keep.txt"), []byte(sentinel), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "../shared-lib")
		if code != 0 {
			t.Fatalf("Run(init ../shared-lib) exit = %d, want 0 with a pre-existing templates/ (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init ../shared-lib) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "../shared-lib") {
			t.Errorf("Run(init ../shared-lib) stdout = %q, want the external library path", stdout)
		}

		got, err := os.ReadFile(filepath.Join(dir, "templates", "keep.txt"))
		if err != nil || string(got) != sentinel {
			t.Errorf("templates/keep.txt = %q, err = %v; want the pre-existing content unchanged", got, err)
		}
		entries, err := os.ReadDir(filepath.Join(dir, "templates"))
		if err != nil {
			t.Fatalf("ReadDir(templates): %v", err)
		}
		if len(entries) != 1 || entries[0].Name() != "keep.txt" {
			t.Errorf("templates/ entries = %v, want only the pre-existing keep.txt", entries)
		}
		_, templates := initConfigTemplates(t, dir)
		if templates != "../shared-lib" {
			t.Errorf("kalide.yaml templates = %q, want %q", templates, "../shared-lib")
		}
	})

	t.Run("no-arg seed path is unchanged", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code != 0 {
			t.Fatalf("Run(init) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init) stderr = %q, want empty", stderr)
		}
		for _, want := range []string{"templates/library.yaml", "slides/1-hello.md", "templates/themes/default/theme.css"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(init) stdout = %q, want the seed created path %q", stdout, want)
			}
		}
		for _, created := range []string{"templates/library.yaml", "slides/1-hello.md", "templates/themes/default/theme.css"} {
			if _, err := os.Stat(filepath.Join(dir, created)); err != nil {
				t.Errorf("stat %s after no-arg init: %v, want created", created, err)
			}
		}
		if _, templates := initConfigTemplates(t, dir); templates != "" {
			t.Errorf("no-arg seed kalide.yaml templates = %q, want empty", templates)
		}
	})
}

// newInitExternalLibrary writes a valid external template library at
// <parent>/shared-lib (reusing start_external_test.go's writeExternalCLILibrary)
// and returns the parent directory and the library directory.
func newInitExternalLibrary(t *testing.T) (parent, libDir string) {
	t.Helper()
	parent = t.TempDir()
	libDir = filepath.Join(parent, "shared-lib")
	writeExternalCLILibrary(t, libDir, "shared-lib", "external hello description",
		"<section><h1>{{.title}}</h1></section>\n")
	return parent, libDir
}

// initConfigTemplates parses kalide.yaml under dir and returns its title and
// templates values, so a test asserts the written configuration rather than its
// exact formatting.
func initConfigTemplates(t *testing.T, dir string) (title, templates string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "kalide.yaml"))
	if err != nil {
		t.Fatalf("read kalide.yaml: %v", err)
	}
	var cfg struct {
		Title     string `yaml:"title"`
		Templates string `yaml:"templates"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse kalide.yaml: %v", err)
	}
	return cfg.Title, cfg.Templates
}

// assertInitDirEmpty asserts dir contains no entries, so a rejected or failed
// init wrote nothing.
func assertInitDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory %s contains %v after a failed init, want nothing written", dir, names)
	}
}
