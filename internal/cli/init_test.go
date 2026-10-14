package cli

// This file unit-tests the `kalide init` command surface (phase-8 task 5;
// phase-03 task 4): requirements.requirement.cli-init,
// requirements.requirement.cli-init-refuse-existing and
// requirements.requirement.init-library-resolution.
//
// It exercises Run's `init` route through the runCLI helper and asserts the
// process contract, not scaffold's internals: parseInitArgs' accepted forms
// (`init`, `init <path>`, `init --local`) and its rejected ones (an unknown
// option, two paths, `--local` combined with a path); the three-way library
// resolution first match wins (explicit path as given -> the configured
// absolute default -> the built-in seed, which `--local` forces even when a
// default is configured); the created-path messages (initCreatedMessage for the
// seed, initExternalCreatedMessage naming the stored path for the two external
// branches); a non-zero exit with scaffold's refusal message naming the
// blocking path(s), and no overwrite, when a deck path is already present; a
// per-user config file that cannot be read or parsed making init exit 1 without
// falling back to the seed; and a usage error (exit 2) writing nothing, because
// there is still no --force.
//
// Every case first points the per-user config store at fresh temp dirs
// (isolateConfig: HOME, XDG_CONFIG_HOME, AppData and USERPROFILE) so the
// developer's real config is never read, then runs in a t.TempDir via t.Chdir,
// so the package directory is never written to and the suite is repeatable.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/config"
	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/displaypath"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/validate"
	"gopkg.in/yaml.v3"
)

// initCreatedPaths are the entries `kalide init` advertises and must actually
// create on an empty directory. Each is a forward-slash, deck-relative identity
// spelling rendered through displaypath.Display, so the literal a case matches
// against the created report is the host operating system's native spelling
// (templates\library.yaml on Windows, templates/library.yaml elsewhere). The
// empty-directory entry is appended in its printed "assets/" form where the
// report is checked.
var initCreatedPaths = []string{
	"kalide.yaml",
	displaypath.Display("", "slides/1-hello.md"),
	displaypath.Display("", "templates/library.yaml"),
	displaypath.Display("", "templates/slides/hello/template.yaml"),
	displaypath.Display("", "templates/slides/hello/layout.html.tmpl"),
	displaypath.Display("", "templates/slides/hello/example.md"),
	displaypath.Display("", "templates/themes/default/theme.css"),
}

// assertNoMixedSeparators fails when a displayed path carries both the
// forward-slash identity spelling and the backslash native spelling: a correct
// display path uses exactly one separator character, the host operating
// system's.
func assertNoMixedSeparators(t *testing.T, path string) {
	t.Helper()
	if strings.Contains(path, "/") && strings.Contains(path, `\`) {
		t.Errorf("display path %q mixes / and \\ separators, want a single native spelling", path)
	}
}

// createdReportEntries returns the path entries of a created report — every
// non-empty line after the header, with the two-space indent removed — so a
// test can assert each entry on its own (for example that none mixes
// separators).
func createdReportEntries(report string) []string {
	lines := strings.Split(report, "\n")
	entries := make([]string, 0, len(lines))
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		entries = append(entries, strings.TrimPrefix(line, "  "))
	}
	return entries
}

// TestInit covers `kalide init` end to end from the command surface: argument
// parsing, library resolution, success, each blocking path, the rejected extra
// argument (no --force), and a repeated run.
func TestInit(t *testing.T) {
	// Every subtest inherits a per-user config store isolated from the
	// developer's real one; the cases that write config re-isolate themselves.
	isolateConfig(t)

	t.Run("parseInitArgs accepts the documented forms", func(t *testing.T) {
		cases := []struct {
			name      string
			args      []string
			wantPath  string
			wantLocal bool
		}{
			{name: "no arguments", args: nil, wantPath: "", wantLocal: false},
			{name: "external library path", args: []string{"../shared-lib"}, wantPath: "../shared-lib", wantLocal: false},
			{name: "--local", args: []string{"--local"}, wantPath: "", wantLocal: true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				path, local, err := parseInitArgs(tc.args)
				if err != nil {
					t.Fatalf("parseInitArgs(%q) error = %v, want nil", tc.args, err)
				}
				if path != tc.wantPath || local != tc.wantLocal {
					t.Errorf("parseInitArgs(%q) = (%q, %v), want (%q, %v)", tc.args, path, local, tc.wantPath, tc.wantLocal)
				}
			})
		}
	})

	t.Run("parseInitArgs rejects malformed arguments", func(t *testing.T) {
		cases := []struct {
			name string
			args []string
			want string
		}{
			{name: "retired --force", args: []string{"--force"}, want: `unexpected option "--force"`},
			{name: "unknown option", args: []string{"-x"}, want: "unexpected option"},
			{name: "two paths", args: []string{"a", "b"}, want: "at most one"},
			{name: "path then --local", args: []string{"a", "--local"}, want: "--local cannot be combined"},
			{name: "--local then path", args: []string{"--local", "a"}, want: "--local cannot be combined"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				path, local, err := parseInitArgs(tc.args)
				if err == nil {
					t.Fatalf("parseInitArgs(%q) error = nil, want a usage error", tc.args)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("parseInitArgs(%q) error = %q, want it to contain %q", tc.args, err, tc.want)
				}
				if path != "" || local {
					t.Errorf("parseInitArgs(%q) = (%q, %v) on error, want (\"\", false)", tc.args, path, local)
				}
			})
		}
	})

	t.Run("usage errors write nothing", func(t *testing.T) {
		cases := []struct {
			name string
			args []string
		}{
			{name: "path and --local", args: []string{"init", "lib", "--local"}},
			{name: "--local and path", args: []string{"init", "--local", "lib"}},
			{name: "two paths", args: []string{"init", "a", "b"}},
			{name: "unknown option", args: []string{"init", "--force"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				t.Chdir(dir)

				code, stdout, stderr := runCLI(tc.args...)
				if code != 2 {
					t.Fatalf("Run(%q) exit = %d, want 2 (stderr = %q)", tc.args, code, stderr)
				}
				if stdout != "" {
					t.Errorf("Run(%q) stdout = %q, want empty", tc.args, stdout)
				}
				if !strings.Contains(stderr, "Usage:") {
					t.Errorf("Run(%q) stderr = %q, want usage text", tc.args, stderr)
				}
				assertInitDirEmpty(t, dir)
			})
		}
	})

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
		if stdout != initCreatedMessage {
			t.Errorf("Run(init) stdout = %q, want initCreatedMessage %q", stdout, initCreatedMessage)
		}

		// The success text lists every created path, spelled deck-relative
		// with the host's native separators ("assets/" becomes "assets\" on
		// Windows).
		for _, want := range append(append([]string{}, initCreatedPaths...), displaypath.Display("", "assets/")) {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(init) stdout = %q, want created path %q", stdout, want)
			}
		}
		// Every reported entry uses one separator spelling only: it must not
		// mix the identity forward slash with the native backslash.
		for _, entry := range createdReportEntries(stdout) {
			assertNoMixedSeparators(t, entry)
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
		{name: "slides dir exists", path: "slides", isDir: true, display: displaypath.Display("", "slides/")},
		{name: "templates dir exists", path: "templates", isDir: true, display: displaypath.Display("", "templates/")},
		{name: "assets dir exists", path: "assets", isDir: true, display: displaypath.Display("", "assets/")},
		{name: "kalide.yaml file exists", path: "kalide.yaml", isDir: false, display: displaypath.Display("", "kalide.yaml")},
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
			// The refusal spells every blocking entry natively, never mixing
			// the forward-slash identity form with the native backslash.
			assertNoMixedSeparators(t, stderr)

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
		for _, want := range []string{displaypath.Display("", "slides/"), displaypath.Display("", "assets/"), displaypath.Display("", "kalide.yaml")} {
			if !strings.Contains(stderr, want) {
				t.Errorf("Run(init) stderr = %q, want all blocking paths named, missing %q", stderr, want)
			}
		}
		assertNoMixedSeparators(t, stderr)
	})

	t.Run("extra argument is rejected because there is no --force", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "--force")
		if code != 2 {
			t.Fatalf("Run(init --force) exit = %d, want 2 (stderr = %q)", code, stderr)
		}
		if want := `unexpected option "--force"`; !strings.Contains(stderr, want) {
			t.Errorf("Run(init --force) stderr = %q, want %q", stderr, want)
		}
		if !strings.Contains(stderr, "at most one library path and the optional --local") {
			t.Errorf("Run(init --force) stderr = %q, want the expected-options hint", stderr)
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
		for _, want := range []string{displaypath.Display("", "slides/"), displaypath.Display("", "templates/"), displaypath.Display("", "assets/"), displaypath.Display("", "kalide.yaml"), "never overwrites"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("second Run(init) stderr = %q, want refusal mentioning %q", stderr, want)
			}
		}
		assertNoMixedSeparators(t, stderr)
	})

	t.Run("--local forces the seed even when a configured default is present", func(t *testing.T) {
		isolateConfig(t)
		_, libDir := newInitExternalLibrary(t)
		if err := config.Set(configKeyDefaultTemplateDir, libDir); err != nil {
			t.Fatalf("config.Set(%q, %q): %v", configKeyDefaultTemplateDir, libDir, err)
		}
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "--local")
		if code != 0 {
			t.Fatalf("Run(init --local) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init --local) stderr = %q, want empty", stderr)
		}
		if stdout != initCreatedMessage {
			t.Errorf("Run(init --local) stdout = %q, want initCreatedMessage %q", stdout, initCreatedMessage)
		}
		// The seed's local templates/ library is written, not the configured
		// external default.
		for _, created := range []string{displaypath.Display("", "templates/library.yaml"), displaypath.Display("", "slides/1-hello.md")} {
			if _, err := os.Stat(filepath.Join(dir, created)); err != nil {
				t.Errorf("stat %s after init --local: %v, want the seed written", created, err)
			}
		}
		if _, templates := initConfigTemplates(t, dir); templates != "" {
			t.Errorf("init --local kalide.yaml templates = %q, want the empty seed value", templates)
		}
	})

	t.Run("explicit path is passed as given and prints the external message", func(t *testing.T) {
		isolateConfig(t)
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
		if want := fmt.Sprintf(initExternalCreatedMessage, "../shared-lib"); stdout != want {
			t.Errorf("Run(init ../shared-lib) stdout = %q, want %q", stdout, want)
		}
		// The path is stored exactly as given, not resolved to an absolute path.
		if _, templates := initConfigTemplates(t, dir); templates != "../shared-lib" {
			t.Errorf("kalide.yaml templates = %q, want %q (as given)", templates, "../shared-lib")
		}
	})

	t.Run("configured default is used as the absolute path and prints the external message", func(t *testing.T) {
		isolateConfig(t)
		parent, libDir := newInitExternalLibrary(t)
		if err := config.Set(configKeyDefaultTemplateDir, libDir); err != nil {
			t.Fatalf("config.Set(%q, %q): %v", configKeyDefaultTemplateDir, libDir, err)
		}
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code != 0 {
			t.Fatalf("Run(init with a configured default) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init with a configured default) stderr = %q, want empty", stderr)
		}
		if want := fmt.Sprintf(initExternalCreatedMessage, libDir); stdout != want {
			t.Errorf("Run(init with a configured default) stdout = %q, want %q", stdout, want)
		}
		// The resolved default is stored as the absolute path.
		if _, templates := initConfigTemplates(t, dir); templates != libDir {
			t.Errorf("kalide.yaml templates = %q, want the absolute default %q", templates, libDir)
		}
		// No local templates/ or starter slide: this is the external branch.
		if _, err := os.Stat(filepath.Join(dir, "templates")); err == nil {
			t.Errorf("init with a configured default created a local templates/, want none")
		}
		if _, err := os.Stat(filepath.Join(dir, "slides", "1-hello.md")); err == nil {
			t.Errorf("init with a configured default wrote the starter slide, want none")
		}
	})

	t.Run("unparseable config file exits 1 without falling back to the seed", func(t *testing.T) {
		isolateConfig(t)
		path := userConfigFile(t)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("this line has no equals sign\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code != 1 {
			t.Fatalf("Run(init) exit = %d, want 1 for an unparseable config (stderr = %q)", code, stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(init) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "config") {
			t.Errorf("Run(init) stderr = %q, want it to name the config file error", stderr)
		}
		// It must not silently fall back to the seed deck.
		assertInitDirEmpty(t, dir)
	})

	t.Run("unreadable config file exits 1 without falling back to the seed", func(t *testing.T) {
		isolateConfig(t)
		// A directory where the config file is expected cannot be read as the
		// key=value file, so config.Load fails rather than returning empty.
		if err := os.MkdirAll(userConfigFile(t), 0o755); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code != 1 {
			t.Fatalf("Run(init) exit = %d, want 1 for an unreadable config (stderr = %q)", code, stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(init) stdout = %q, want empty", stdout)
		}
		if stderr == "" {
			t.Errorf("Run(init) stderr = %q, want an error naming the unreadable config", stderr)
		}
		assertInitDirEmpty(t, dir)
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
// (phase-03 task-8; phase-03 task-5): requirements.requirement.cli-init,
// requirements.requirement.cli-init-refuse-existing and
// requirements.requirement.init-library-resolution.
//
// It exercises Run's `init` route through runCLI and asserts the process
// contract: the optional external path is parsed and scaffolds a deck that
// references it (stdout lists the created paths, exit 0); extra arguments are a
// usage error (exit 2, nothing written); an invalid or missing external
// library, or a library that does not resolve the deck's default theme, exits
// non-zero with nothing written; a pre-existing local templates/ is exempt and
// left in place as the deck's local layer, so a slide name it shares with the
// external library is not detected at init but rejected when `kalide start`
// loads the merged library; an existing local templates/ still blocks only the
// seed form; and the no-arg seed path is unchanged.
//
// Its configured-default integration cases drive a REAL per-user config store
// (isolateConfig) and a REAL external library on disk: `kalide init` (no path)
// writes a deck whose kalide.yaml carries `templates: <absolute configured
// path>`, empty slides/ and assets/, no local templates/ and no starter slide,
// and the deck loads and whole-deck-validates through the same loader path
// `kalide start` uses; an explicit `kalide init <path>` overrides the
// configured default and stores the relative path as given; and a configured
// default that is missing from disk, is not a valid library, or does not
// resolve the deck's theme fails before writing anything. Those on-disk cases
// are skipped under -short.
//
// Every case runs in a t.TempDir with t.Chdir, so the package directory is
// never written to.
func TestInitExternalLibrary(t *testing.T) {
	// Isolate the per-user config store: the no-arg case below must see an
	// empty store, never the developer's real default-template-dir.
	isolateConfig(t)

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
			displaypath.Display("", "slides/"),
			displaypath.Display("", "assets/"),
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(init ../shared-lib) stdout = %q, want created path %q", stdout, want)
			}
		}
		// Every reported entry is a single native spelling, with no mixed
		// separators (the library path in the header is left as given).
		for _, entry := range createdReportEntries(stdout) {
			assertNoMixedSeparators(t, entry)
		}
		if strings.Contains(stdout, displaypath.Display("", "templates/library.yaml")) || strings.Contains(stdout, displaypath.Display("", "slides/1-hello.md")) {
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

	// The cases below drive the configured-default resolution over a REAL
	// per-user config store (isolateConfig) and a REAL external library on
	// disk, and validate the written deck through the same loader path `kalide
	// start` uses. They are the file's on-disk integration cases, so each is
	// skipped under -short.

	t.Run("configured default writes a validating external deck", func(t *testing.T) {
		if testing.Short() {
			t.Skip("integration test: writes a real per-user config store and external library to disk")
		}
		isolateConfig(t)
		parent, libDir := newInitExternalLibrary(t)
		if err := config.Set(configKeyDefaultTemplateDir, libDir); err != nil {
			t.Fatalf("config.Set(%q, %q): %v", configKeyDefaultTemplateDir, libDir, err)
		}
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code != 0 {
			t.Fatalf("Run(init with a configured default) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init with a configured default) stderr = %q, want empty", stderr)
		}
		if want := fmt.Sprintf(initExternalCreatedMessage, libDir); stdout != want {
			t.Errorf("Run(init with a configured default) stdout = %q, want %q", stdout, want)
		}

		// kalide.yaml carries the configured default as an absolute path.
		_, templates := initConfigTemplates(t, dir)
		if templates != libDir {
			t.Errorf("kalide.yaml templates = %q, want the absolute configured default %q", templates, libDir)
		}
		if !filepath.IsAbs(templates) {
			t.Errorf("kalide.yaml templates = %q, want an absolute path", templates)
		}

		// Empty slides/ and assets/, no local templates/ and no starter slide.
		for _, name := range []string{"slides", "assets"} {
			entries, err := os.ReadDir(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("ReadDir(%s): %v", name, err)
			}
			if len(entries) != 0 {
				t.Errorf("%s/ contains %d entries, want an empty directory", name, len(entries))
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "templates")); err == nil {
			t.Errorf("local templates/ was created by the configured-default init, want none")
		}
		if _, err := os.Stat(filepath.Join(dir, "slides", "1-hello.md")); err == nil {
			t.Errorf("starter slide slides/1-hello.md was written, want none")
		}

		// The deck loads and whole-deck-validates exactly as `kalide start`
		// validates it before serving.
		assertInitDeckLoads(t, dir)
	})

	t.Run("explicit path overrides the configured default and stays relative", func(t *testing.T) {
		if testing.Short() {
			t.Skip("integration test: writes a real per-user config store and external libraries to disk")
		}
		isolateConfig(t)
		// A valid configured default that must NOT be used...
		_, defaultLib := newInitExternalLibrary(t)
		if err := config.Set(configKeyDefaultTemplateDir, defaultLib); err != nil {
			t.Fatalf("config.Set(%q, %q): %v", configKeyDefaultTemplateDir, defaultLib, err)
		}
		// ...and a different valid library the explicit path resolves to.
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
		if want := fmt.Sprintf(initExternalCreatedMessage, "../shared-lib"); stdout != want {
			t.Errorf("Run(init ../shared-lib) stdout = %q, want %q", stdout, want)
		}

		// The path is stored exactly as given, not the configured absolute
		// default.
		_, templates := initConfigTemplates(t, dir)
		if templates != "../shared-lib" {
			t.Errorf("kalide.yaml templates = %q, want %q (the explicit path as given)", templates, "../shared-lib")
		}
		if templates == defaultLib {
			t.Errorf("kalide.yaml templates = %q, want the explicit path, not the configured default", templates)
		}
		assertInitDeckLoads(t, dir)
	})

	t.Run("missing configured default fails before writing", func(t *testing.T) {
		if testing.Short() {
			t.Skip("integration test: writes a real per-user config store to disk")
		}
		isolateConfig(t)
		parent := t.TempDir()
		missing := filepath.Join(parent, "nope")
		if err := config.Set(configKeyDefaultTemplateDir, missing); err != nil {
			t.Fatalf("config.Set(%q, %q): %v", configKeyDefaultTemplateDir, missing, err)
		}
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code == 0 {
			t.Fatalf("Run(init) exit = 0, want non-zero for a missing configured default")
		}
		if stdout != "" {
			t.Fatalf("Run(init) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, missing) {
			t.Errorf("Run(init) stderr = %q, want it to name the missing configured path %q", stderr, missing)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("invalid configured default fails before writing", func(t *testing.T) {
		if testing.Short() {
			t.Skip("integration test: writes a real per-user config store and external library to disk")
		}
		isolateConfig(t)
		parent, libDir := newInitExternalLibrary(t)
		writeExternalFile(t, filepath.Join(libDir, "library.yaml"), "name: Bad_Name\nformat: 1\n")
		if err := config.Set(configKeyDefaultTemplateDir, libDir); err != nil {
			t.Fatalf("config.Set(%q, %q): %v", configKeyDefaultTemplateDir, libDir, err)
		}
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code == 0 {
			t.Fatalf("Run(init) exit = 0, want non-zero for an invalid configured default")
		}
		if stdout != "" {
			t.Fatalf("Run(init) stdout = %q, want empty", stdout)
		}
		if want := filepath.Join(libDir, "library.yaml"); !strings.Contains(stderr, want) {
			t.Errorf("Run(init) stderr = %q, want it to name %q", stderr, want)
		}
		if !strings.Contains(stderr, "not a valid name") {
			t.Errorf("Run(init) stderr = %q, want the library.yaml name error", stderr)
		}
		assertInitDirEmpty(t, dir)
	})

	t.Run("configured default without the deck's theme fails before writing", func(t *testing.T) {
		if testing.Short() {
			t.Skip("integration test: writes a real per-user config store and external library to disk")
		}
		isolateConfig(t)
		parent, libDir := newInitExternalLibrary(t)
		if err := os.RemoveAll(filepath.Join(libDir, "themes", "default")); err != nil {
			t.Fatalf("remove themes/default: %v", err)
		}
		writeExternalFile(t, filepath.Join(libDir, "themes", "plain", "theme.css"), "body {}\n")
		if err := config.Set(configKeyDefaultTemplateDir, libDir); err != nil {
			t.Fatalf("config.Set(%q, %q): %v", configKeyDefaultTemplateDir, libDir, err)
		}
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init")
		if code == 0 {
			t.Fatalf("Run(init) exit = 0, want non-zero when the configured default does not resolve the deck's theme")
		}
		if stdout != "" {
			t.Fatalf("Run(init) stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "default") {
			t.Errorf("Run(init) stderr = %q, want it to name the unresolvable default theme", stderr)
		}
		assertInitDirEmpty(t, dir)
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

	// A two-layer deck whose theme lives ONLY in the external `templates:`
	// library loads and validates: assertInitDeckLoads builds the theme registry
	// from EVERY merged library layer through template.LoadThemeRegistry, so the
	// external-only theme resolves (layered-template-library, theme-selection).
	// theme.LoadDir on the merged Library's primary (local) root would find no
	// themes/ and fail here.
	t.Run("a two-layer deck whose theme lives only in the external layer loads and validates", func(t *testing.T) {
		parent, _ := newInitExternalLibrary(t)
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// A deck-root templates/ layer contributes a slide but carries no
		// themes/, so the deck's theme (`default`) lives ONLY in the external
		// library.
		writeTestLibrary(t, filepath.Join(dir, template.TemplatesDir), "local-library", []string{"local-slide"}, nil, "")
		t.Chdir(dir)

		code, stdout, stderr := runCLI("init", "../shared-lib")
		if code != 0 {
			t.Fatalf("Run(init ../shared-lib) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init ../shared-lib) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "../shared-lib") {
			t.Errorf("Run(init ../shared-lib) stdout = %q, want the external library path", stdout)
		}

		// The deck loads AND validates: the external library's default theme
		// resolves through the per-layer theme registry even though the local
		// layer carries none.
		assertInitDeckLoads(t, dir)
	})

	// The external branch validates the external library ALONE, so a
	// pre-existing local templates/ whose slide name collides with the external
	// library's is left in place by init and the collision surfaces only when
	// `kalide start` loads the merged library (library-layer-uniqueness).
	t.Run("colliding local layer is left in place and rejected at start's load", func(t *testing.T) {
		if testing.Short() {
			t.Skip("integration test: writes a real deck and two on-disk libraries")
		}
		isolateConfig(t)
		parent, libDir := newInitExternalLibrary(t)
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		// A pre-existing local templates/ that is itself a valid library and
		// carries the SAME slide name ("hello") the external library carries.
		localTemplates := filepath.Join(dir, "templates")
		writeExternalCLILibrary(t, localTemplates, "local-lib", "local hello description",
			"<section><h1>{{.title}}</h1></section>\n")
		localManifest := filepath.Join(localTemplates, "slides", "hello", "template.yaml")
		before, err := os.ReadFile(localManifest)
		if err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		// `init ../shared-lib` reads the external library alone: the colliding
		// local templates/ is not read, so init succeeds with nothing refused.
		code, stdout, stderr := runCLI("init", "../shared-lib")
		if code != 0 {
			t.Fatalf("Run(init ../shared-lib) exit = %d, want 0 with a colliding local templates/ (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init ../shared-lib) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "../shared-lib") {
			t.Errorf("Run(init ../shared-lib) stdout = %q, want the external library path", stdout)
		}

		// The local layer participates as the deck's local library layer and is
		// left byte-for-byte in place; init writes none of its own.
		if after, err := os.ReadFile(localManifest); err != nil || string(after) != string(before) {
			t.Errorf("local templates/slides/hello/template.yaml changed across init (err = %v), want it untouched", err)
		}
		_, configured := initConfigTemplates(t, dir)
		if configured != "../shared-lib" {
			t.Errorf("kalide.yaml templates = %q, want %q", configured, "../shared-lib")
		}

		// The collision is NOT detected at init: it surfaces when `kalide start`
		// loads the merged library through the same loader runStart calls with
		// the same raw `templates:` value. The duplicate slide name is reported
		// naming the name and both carrying paths, one per layer.
		if _, err := template.LoadDeckLibrary(dir, configured); err == nil {
			t.Fatalf("template.LoadDeckLibrary(%s, %q) error = nil, want the duplicate slide-name error", dir, configured)
		} else {
			for _, want := range []string{
				"declared in both",
				`"hello"`,
				filepath.Join(libDir, "slides", "hello", "template.yaml"),
				localManifest,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("merged load error = %q, want it to name %q", err, want)
				}
			}
		}
	})

	// An existing local templates/ still blocks the built-in starter-deck
	// branch, which writes a local templates/ of its own, while the external
	// branch leaves it exempt: the block applies to the seed form only
	// (cli-init-refuse-existing).
	t.Run("existing local templates still blocks only the seed form", func(t *testing.T) {
		parent, _ := newInitExternalLibrary(t)
		dir := filepath.Join(parent, "deck")
		if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
			t.Fatal(err)
		}
		const sentinel = "seed-blocking local templates content\n"
		if err := os.WriteFile(filepath.Join(dir, "templates", "keep.txt"), []byte(sentinel), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)

		// The seed form (no path, no configured default) refuses to clobber it.
		code, stdout, stderr := runCLI("init")
		if code == 0 {
			t.Fatalf("Run(init) exit = 0 with a pre-existing local templates/, want the seed refusal")
		}
		if stdout != "" {
			t.Fatalf("Run(init) stdout = %q, want empty on refusal", stdout)
		}
		for _, want := range []string{"init:", displaypath.Display("", "templates/"), "already present", "never overwrites"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("Run(init) stderr = %q, want the seed refusal mentioning %q", stderr, want)
			}
		}
		assertNoMixedSeparators(t, stderr)
		if got, err := os.ReadFile(filepath.Join(dir, "templates", "keep.txt")); err != nil || string(got) != sentinel {
			t.Errorf("templates/keep.txt = %q, err = %v; want the pre-existing content unchanged", got, err)
		}
		for _, other := range []string{"kalide.yaml", "slides", "assets"} {
			if _, err := os.Lstat(filepath.Join(dir, other)); err == nil {
				t.Errorf("%s was created despite the seed refusal, want nothing written", other)
			}
		}

		// The same pre-existing local templates/ does not block the external
		// form: `init ../shared-lib` proceeds and leaves it in place.
		code, stdout, stderr = runCLI("init", "../shared-lib")
		if code != 0 {
			t.Fatalf("Run(init ../shared-lib) exit = %d with the same pre-existing templates/, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Fatalf("Run(init ../shared-lib) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, "../shared-lib") {
			t.Errorf("Run(init ../shared-lib) stdout = %q, want the external library path", stdout)
		}
		if got, err := os.ReadFile(filepath.Join(dir, "templates", "keep.txt")); err != nil || string(got) != sentinel {
			t.Errorf("templates/keep.txt = %q, err = %v; want it left in place by the external init", got, err)
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
		for _, want := range []string{displaypath.Display("", "templates/library.yaml"), displaypath.Display("", "slides/1-hello.md"), displaypath.Display("", "templates/themes/default/theme.css")} {
			if !strings.Contains(stdout, want) {
				t.Errorf("Run(init) stdout = %q, want the seed created path %q", stdout, want)
			}
		}
		for _, created := range []string{displaypath.Display("", "templates/library.yaml"), displaypath.Display("", "slides/1-hello.md"), displaypath.Display("", "templates/themes/default/theme.css")} {
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

// assertInitDeckLoads validates dir exactly as `kalide start` validates a deck
// before serving: it resolves and loads the deck's template library (honouring
// the configured `templates:` path), builds the theme registry from EVERY
// merged library layer through template.LoadThemeRegistry (so a theme living only
// in an external `templates:` layer resolves) and the template registry, and
// runs the whole-deck validator, failing the test on the first error. It proves
// a deck written by `kalide init` is loadable and would serve.
func assertInitDeckLoads(t *testing.T, dir string) {
	t.Helper()
	deckFS := os.DirFS(dir)
	configured := deck.ConfiguredTemplates(deckFS)
	library, err := template.LoadDeckLibrary(dir, configured)
	if err != nil {
		t.Fatalf("init deck %s: template library does not load: %v", dir, err)
	}
	themes, err := template.LoadThemeRegistry(library)
	if err != nil {
		t.Fatalf("init deck %s: theme registry: %v", dir, err)
	}
	registry, err := template.NewRegistryFromLibrary(library)
	if err != nil {
		t.Fatalf("init deck %s: template registry: %v", dir, err)
	}
	if verr, invalid := validate.Validate(deckFS, registry, themes); invalid {
		t.Fatalf("init deck %s does not validate: %s", dir, validate.Format(verr))
	}
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
