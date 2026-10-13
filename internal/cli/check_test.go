package cli

// Tests for `kalide check [--all] [--json]` (cli-check): the non-serving
// whole-deck validator. runCheck loads the deck's resolved library and theme
// and runs the whole-deck validator over the deck without opening a server,
// binding a port or reaching the network.
//
// The cases pin the contract the command documents: exit 0 for a valid deck,
// exit 1 for a validation error (the fail-fast first error, or every error in
// the fixed order with --all) and exit 2 for a usage error; the summary on
// stdout with the errors on stderr in text mode; and the machine-readable
// `--json` report on stdout — a valid boolean, a summary and each error's
// file/line/path/message, with the errors array always present.
//
// The deck fixtures copy the phase-3 fixture library (internal/template/
// testdata/library/templates) into a fresh temp directory as templates/, write
// the deck's kalide.yaml and slides alongside it, and chdir into the deck root,
// matching the neighbours in cli_test.go/start_templates_test.go.

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// chdirFixtureDeck copies the phase-3 fixture templates/ library into a fresh
// temp directory (chdirFixtureLibrary), writes each deck-relative file beneath
// it, and leaves the test chdir'd into the deck root. Keys are deck-relative
// slash paths (for example "slides/1-hello.md").
func chdirFixtureDeck(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := chdirFixtureLibrary(t)
	for name, data := range files {
		writeCLIFile(t, filepath.Join(dir, filepath.FromSlash(name)), []byte(data))
	}
	return dir
}

// validCheckDeck is a deck that passes whole-deck validation against the
// fixture library: a valid kalide.yaml and one hello slide carrying its
// required title.
func validCheckDeck() map[string]string {
	return map[string]string{
		"kalide.yaml":       "title: Check Deck\ntheme: plain\n",
		"slides/1-hello.md": "---\ntemplate: hello\ntitle: Hi\n---\n",
	}
}

// twoErrorCheckDeck is a deck with two deterministic, independently
// detectable whole-deck errors in slide order: two hello slides each missing
// the required `title` field. The fail-fast walk reports only the first; --all
// reports both, in filename order.
func twoErrorCheckDeck() map[string]string {
	return map[string]string{
		"kalide.yaml":     "title: Check Deck\ntheme: plain\n",
		"slides/1-one.md": "---\ntemplate: hello\n---\n",
		"slides/2-two.md": "---\ntemplate: hello\n---\n",
	}
}

// checkJSONError mirrors one error in the `--json` report (checkReportError).
type checkJSONError struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Path    []string `json:"path"`
	Message string   `json:"message"`
}

// checkJSONReport mirrors the machine-readable `--json` report (checkReport).
type checkJSONReport struct {
	Valid   bool             `json:"valid"`
	Summary string           `json:"summary"`
	Errors  []checkJSONError `json:"errors"`
}

// decodeCheckReport parses a `--json` report and pins its top-level shape: it
// carries exactly the valid/summary/errors keys, so a stray key such as a
// suggestion would fail here.
func decodeCheckReport(t *testing.T, stdout string) checkJSONReport {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &keys); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout, err)
	}
	if len(keys) != 3 {
		t.Errorf("report keys = %v, want exactly valid/summary/errors", keys)
	}
	for _, want := range []string{"valid", "summary", "errors"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("report keys = %v, want key %q", keys, want)
		}
	}
	var report checkJSONReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout, err)
	}
	return report
}

// nonEmptyLines splits output into its non-blank lines.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestRunCheck(t *testing.T) {
	t.Run("valid deck exits 0 with the valid summary on stdout", func(t *testing.T) {
		chdirFixtureDeck(t, validCheckDeck())

		code, stdout, stderr := runCLI("check")
		if code != 0 {
			t.Fatalf("Run(check) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if want := "kalide check: valid\n"; stdout != want {
			t.Errorf("Run(check) stdout = %q, want %q", stdout, want)
		}
		if stderr != "" {
			t.Errorf("Run(check) stderr = %q, want empty", stderr)
		}
	})

	t.Run("invalid deck exits 1 reporting only the first error to stderr", func(t *testing.T) {
		chdirFixtureDeck(t, twoErrorCheckDeck())

		code, stdout, stderr := runCLI("check")
		if code != 1 {
			t.Fatalf("Run(check) exit = %d, want 1 (stdout = %q)", code, stdout)
		}
		if want := "kalide check: 1 error\n"; stdout != want {
			t.Errorf("Run(check) stdout = %q, want %q", stdout, want)
		}
		lines := nonEmptyLines(stderr)
		if len(lines) != 1 {
			t.Fatalf("Run(check) stderr = %q, want exactly one error line", stderr)
		}
		if !strings.Contains(lines[0], "slides/1-one.md") {
			t.Errorf("Run(check) stderr = %q, want the first slide's error", stderr)
		}
		if !strings.Contains(lines[0], `field "title" is required but missing`) {
			t.Errorf("Run(check) stderr = %q, want the missing-title error", stderr)
		}
		if strings.Contains(stderr, "slides/2-two.md") {
			t.Errorf("Run(check) stderr = %q, want only the fail-fast first error", stderr)
		}
	})

	t.Run("--all collects every error in slide order", func(t *testing.T) {
		chdirFixtureDeck(t, twoErrorCheckDeck())

		code, stdout, stderr := runCLI("check", "--all")
		if code != 1 {
			t.Fatalf("Run(check --all) exit = %d, want 1 (stdout = %q)", code, stdout)
		}
		if want := "kalide check: 2 errors\n"; stdout != want {
			t.Errorf("Run(check --all) stdout = %q, want %q", stdout, want)
		}
		lines := nonEmptyLines(stderr)
		if len(lines) != 2 {
			t.Fatalf("Run(check --all) stderr = %q, want two error lines", stderr)
		}
		if !strings.Contains(lines[0], "slides/1-one.md") {
			t.Errorf("Run(check --all) stderr[0] = %q, want the first slide", lines[0])
		}
		if !strings.Contains(lines[1], "slides/2-two.md") {
			t.Errorf("Run(check --all) stderr[1] = %q, want the second slide", lines[1])
		}
	})

	t.Run("--json on a valid deck emits a report with an empty errors array", func(t *testing.T) {
		chdirFixtureDeck(t, validCheckDeck())

		code, stdout, stderr := runCLI("check", "--json")
		if code != 0 {
			t.Fatalf("Run(check --json) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("Run(check --json) stderr = %q, want empty", stderr)
		}
		if !strings.Contains(stdout, `"errors":[]`) {
			t.Errorf("Run(check --json) stdout = %q, want an empty errors array", stdout)
		}
		report := decodeCheckReport(t, stdout)
		if !report.Valid {
			t.Errorf("report.Valid = false, want true for a valid deck")
		}
		if report.Summary != "valid" {
			t.Errorf("report.Summary = %q, want %q", report.Summary, "valid")
		}
		if len(report.Errors) != 0 {
			t.Errorf("report.Errors = %+v, want none", report.Errors)
		}
	})

	t.Run("--json on an invalid deck carries one error with file/line/path/message", func(t *testing.T) {
		chdirFixtureDeck(t, twoErrorCheckDeck())

		code, stdout, stderr := runCLI("check", "--json")
		if code != 1 {
			t.Fatalf("Run(check --json) exit = %d, want 1 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("Run(check --json) stderr = %q, want empty", stderr)
		}
		report := decodeCheckReport(t, stdout)
		if report.Valid {
			t.Errorf("report.Valid = true, want false for an invalid deck")
		}
		if report.Summary != "1 error" {
			t.Errorf("report.Summary = %q, want %q", report.Summary, "1 error")
		}
		if len(report.Errors) != 1 {
			t.Fatalf("report.Errors = %+v, want exactly one (fail-fast)", report.Errors)
		}
		e := report.Errors[0]
		if e.File != "slides/1-one.md" {
			t.Errorf("error.File = %q, want %q", e.File, "slides/1-one.md")
		}
		if e.Line != 0 {
			t.Errorf("error.Line = %d, want 0 (a field error carries no line)", e.Line)
		}
		if !reflect.DeepEqual(e.Path, []string{"title"}) {
			t.Errorf("error.Path = %v, want [title]", e.Path)
		}
		want := `required: field "title" is required but missing — add a title: value`
		if e.Message != want {
			t.Errorf("error.Message = %q, want %q", e.Message, want)
		}
	})

	t.Run("--json with --all carries every error", func(t *testing.T) {
		chdirFixtureDeck(t, twoErrorCheckDeck())

		code, stdout, stderr := runCLI("check", "--all", "--json")
		if code != 1 {
			t.Fatalf("Run(check --all --json) exit = %d, want 1 (stderr = %q)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("Run(check --all --json) stderr = %q, want empty", stderr)
		}
		report := decodeCheckReport(t, stdout)
		if report.Summary != "2 errors" {
			t.Errorf("report.Summary = %q, want %q", report.Summary, "2 errors")
		}
		if len(report.Errors) != 2 {
			t.Fatalf("report.Errors = %+v, want two errors", report.Errors)
		}
		if report.Errors[0].File != "slides/1-one.md" || report.Errors[1].File != "slides/2-two.md" {
			t.Errorf("report error files = %q, %q, want slides/1-one.md, slides/2-two.md",
				report.Errors[0].File, report.Errors[1].File)
		}
	})

	t.Run("a usage error is plain stderr with no report even with --json", func(t *testing.T) {
		chdirFixtureDeck(t, validCheckDeck())

		code, stdout, stderr := runCLI("check", "--json", "--bogus")
		if code != 2 {
			t.Fatalf("Run(check --json --bogus) exit = %d, want 2", code)
		}
		if stdout != "" {
			t.Errorf("Run(check --json --bogus) stdout = %q, want no report", stdout)
		}
		if !strings.Contains(stderr, `unexpected argument "--bogus"`) {
			t.Errorf("Run(check --json --bogus) stderr = %q, want the offending argument", stderr)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("Run(check --json --bogus) stderr = %q, want usage text", stderr)
		}
	})

	t.Run("a library load error is the first check error and ends the report with --all", func(t *testing.T) {
		// No templates/ directory and no kalide.yaml: the library cannot be
		// loaded at all, so the report holds exactly the one load error. With
		// --all it still ends there — no registry exists to collect further
		// errors from.
		t.Chdir(t.TempDir())

		code, stdout, stderr := runCLI("check")
		if code != 1 {
			t.Fatalf("Run(check) exit = %d, want 1 (stdout = %q)", code, stdout)
		}
		if want := "kalide check: 1 error\n"; stdout != want {
			t.Errorf("Run(check) stdout = %q, want %q", stdout, want)
		}
		if !strings.Contains(stderr, "templates") {
			t.Errorf("Run(check) stderr = %q, want the load error naming templates", stderr)
		}

		code, stdout, _ = runCLI("check", "--all")
		if code != 1 {
			t.Fatalf("Run(check --all) exit = %d, want 1", code)
		}
		if want := "kalide check: 1 error\n"; stdout != want {
			t.Errorf("Run(check --all) stdout = %q, want the single load error to end the report", stdout)
		}

		code, stdout, _ = runCLI("check", "--json")
		if code != 1 {
			t.Fatalf("Run(check --json) exit = %d, want 1", code)
		}
		report := decodeCheckReport(t, stdout)
		if len(report.Errors) != 1 {
			t.Fatalf("report.Errors = %+v, want exactly the one load error", report.Errors)
		}
		e := report.Errors[0]
		if e.File != "templates" {
			t.Errorf("error.File = %q, want %q", e.File, "templates")
		}
		if e.Line != 0 {
			t.Errorf("error.Line = %d, want 0", e.Line)
		}
		if len(e.Path) != 0 {
			t.Errorf("error.Path = %v, want empty", e.Path)
		}
		if e.Message == "" {
			t.Error("error.Message = \"\", want the loader's message")
		}
	})
}
