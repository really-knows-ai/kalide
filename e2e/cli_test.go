package e2e

// This file is the plan.phase-10 tasks 13–15 end-to-end test for the
// non-serving CLI commands `kalide check`, `kalide render` and `kalide
// templates` (requirements.requirement.cli-check, cli-render,
// cli-templates-list, cli-templates-show). It drives the actual kalide binary
// the harness builds (harness.go) through Harness.Run — the one-shot path,
// unlike the signal-blocking `kalide start` the harness starts with Start — so
// it exercises exactly what an author runs from a shell.
//
// Every case uses a real on-disk deck and a two-layer template library (the
// deck-root templates/ local layer plus the external `templates: ../shared-lib`
// layer), so a merge, a validation error and a render are exercised through the
// same loader and registry `kalide start` uses:
//
//   - TestCLICheck (task 13): exit 0 on the valid deck with the
//     `kalide check: valid` summary on stdout; exit 1 with the single first
//     error (fail-fast, validate.Validate) or, with `--all`, every error in the
//     fixed whole-deck-validation order; the `--json` report on stdout
//     ({"valid","summary","errors"} with each error's file/line/path/message and
//     no suggestion key), the text summary on stdout with the errors on stderr,
//     and exit 2 for a usage error (plain stderr, even with --json). It also
//     proves the one-shot run reaches completion printing no
//     `Serving slides at <url>` server marker and spawning no platform
//     browser-opener child, observed with a sentinel-writing stub opener first
//     on the harness's scrubbed PATH.
//
// The fixture's `main` slide template carries a required heading, a
// list-of-record field with recursively nested fields and a nullable nested
// field, an enum field and a declared section; one slide uses a bad enum value
// and omits the required heading so `check` has two ordered errors to report.
//
// The tests build a binary, so they are skipped under -short.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cliFixtureDeckConfig is the fixture deck's kalide.yaml: it draws from BOTH
// resolved library layers — the deck-root templates/ local layer and the
// external `templates: ../shared-lib` layer (layered-template-library) — and
// selects the `plain` theme the local layer carries.
const cliFixtureDeckConfig = `title: CLI Fixture Deck
author: Ada Lovelace
theme: plain
templates: ../shared-lib
`

// cliFixtureMainManifest is the fixture's `main` slide template. Its field
// schema is deliberately rich so `kalide templates main` has a list-of-record
// field with recursively nested fields (including a nullable nested field) and
// an enum field with effective variants to document, plus a declared section
// and a body rule.
const cliFixtureMainManifest = `description: the main fixture slide
fields:
  - name: heading
    type: text
    required: true
  - name: authors
    type: list
    item:
      type: record
      fields:
        - name: name
          type: text
          required: true
        - name: note
          type: text
          nullable: true
        - name: handles
          type: list
          item:
            type: record
            fields:
              - name: handle
                type: text
  - name: status
    type: enum
    variants: [draft, published]
sections:
  - name: note
    accepted: [note]
    max: 2
body:
  mode: optional
`

// cliFixtureMainLayout emits the slide's heading and status, so a render can be
// checked for the slide's own markup and its label anchor id.
const cliFixtureMainLayout = `<section class="main"><h1 class="main-heading">{{.heading}}</h1><span class="main-status">{{.status}}</span></section>` + "\n"

// cliFixtureMainExample is the valid example `main` must carry (the loader
// validates every template's example), filling the record list and enum.
const cliFixtureMainExample = `---
template: main
heading: Example
status: draft
authors:
  - name: Ada
    note: first
    handles:
      - handle: ada
---

Example body.
`

// cliFixtureNoteManifest is the fixture's `note` section template, declared by
// `main` so its section-documentation line is exercised.
const cliFixtureNoteManifest = `description: a note section
fields:
  - name: label
    type: text
    required: true
body:
  mode: optional
`

const cliFixtureNoteLayout = `<aside class="note">{{.label}}</aside>` + "\n"

const cliFixtureNoteExample = "```\nlabel: Example note\n```\n"

// cliFixtureHiddenManifest is the fixture's `internal: true` slide template:
// resolvable and usable, but omitted from the default `kalide templates`
// listing and shown with `--all` (internal-templates).
const cliFixtureHiddenManifest = `description: an internal helper slide
internal: true
body:
  mode: optional
`

const cliFixtureHiddenLayout = `<section class="hidden">internal</section>` + "\n"

const cliFixtureHiddenExample = "---\ntemplate: hidden\n---\n"

// cliFixtureExtCardManifest is a section template contributed by the EXTERNAL
// layer only, so the merged `kalide templates` listing proves both layers are
// listed.
const cliFixtureExtCardManifest = `description: an external card section
fields:
  - name: title
    type: text
    required: true
body:
  mode: disallowed
`

const cliFixtureExtCardLayout = `<div class="extcard">{{.title}}</div>` + "\n"

const cliFixtureExtCardExample = "```\ntitle: External\n```\n"

// The three valid slides: two horizontal slides and one vertical (letter) slide
// under number 2, so `render -o` writes `1-intro.html`, `2-detail.html` and
// `2a-extra.html` in deck order and the label/position-label selectors differ.
const cliFixtureIntroSlide = `---
template: main
heading: Intro
status: draft
authors:
  - name: Ada
    note: first
---

Welcome.
`

const cliFixtureDetailSlide = `---
template: main
heading: Detail
---

Detail body.
`

const cliFixtureExtraSlide = `---
template: main
heading: Extra
---

Extra body.
`

// cliFixtureBrokenSlide uses `main` but omits the required `heading` and gives
// `status` a value outside its variants, so the whole-deck validator reports
// exactly two errors in the fixed field order (heading, then status).
const cliFixtureBrokenSlide = `---
template: main
status: bogus
---
`

// writeCLIDeckFixture lays down the two-layer valid fixture deck and library
// every CLI test starts from: kalide.yaml, the deck-root templates/ local layer
// (library.yaml, the plain theme, the rich `main` slide, the `note` section and
// the `internal: true` hidden slide), the external `../shared-lib` layer (its
// own library.yaml and the `extcard` section), and the three valid slides.
func writeCLIDeckFixture(t *testing.T, h *Harness) {
	t.Helper()

	h.WriteFile("kalide.yaml", []byte(cliFixtureDeckConfig))

	// The deck-root templates/ local layer.
	h.WriteFile("templates/library.yaml",
		[]byte("name: cli-fixture-lib\ndescription: the CLI fixture library\nformat: 1\n"))
	h.WriteFile("templates/themes/plain/theme.css", []byte("/* plain */\nbody { margin: 0; }\n"))
	h.WriteFile("templates/slides/main/template.yaml", []byte(cliFixtureMainManifest))
	h.WriteFile("templates/slides/main/layout.html.tmpl", []byte(cliFixtureMainLayout))
	h.WriteFile("templates/slides/main/example.md", []byte(cliFixtureMainExample))
	h.WriteFile("templates/sections/note/template.yaml", []byte(cliFixtureNoteManifest))
	h.WriteFile("templates/sections/note/layout.html.tmpl", []byte(cliFixtureNoteLayout))
	h.WriteFile("templates/sections/note/example.md", []byte(cliFixtureNoteExample))
	h.WriteFile("templates/slides/hidden/template.yaml", []byte(cliFixtureHiddenManifest))
	h.WriteFile("templates/slides/hidden/layout.html.tmpl", []byte(cliFixtureHiddenLayout))
	h.WriteFile("templates/slides/hidden/example.md", []byte(cliFixtureHiddenExample))

	// The external `templates: ../shared-lib` layer.
	h.WriteFile("../shared-lib/library.yaml",
		[]byte("name: cli-external-lib\ndescription: the CLI external layer\nformat: 1\n"))
	h.WriteFile("../shared-lib/sections/extcard/template.yaml", []byte(cliFixtureExtCardManifest))
	h.WriteFile("../shared-lib/sections/extcard/layout.html.tmpl", []byte(cliFixtureExtCardLayout))
	h.WriteFile("../shared-lib/sections/extcard/example.md", []byte(cliFixtureExtCardExample))

	// The valid slides.
	h.WriteFile("slides/1-intro.md", []byte(cliFixtureIntroSlide))
	h.WriteFile("slides/2-detail.md", []byte(cliFixtureDetailSlide))
	h.WriteFile("slides/2a-extra.md", []byte(cliFixtureExtraSlide))
}

// writeCLIBrokenDeck replaces the fixture's slides with the single broken slide
// (the required heading missing, the enum value out of range), leaving the
// library in place so the failure is a deck validation error, not a load error.
func writeCLIBrokenDeck(t *testing.T, h *Harness) {
	t.Helper()

	if err := os.RemoveAll(h.Path("slides")); err != nil {
		t.Fatalf("remove slides/: %v", err)
	}
	h.WriteFile("slides/1-broken.md", []byte(cliFixtureBrokenSlide))
}

// TestCLICheck is the plan.phase-10 task-13 end-to-end test described at the top
// of this file: the valid deck's `kalide check: valid` on stdout and exit 0, the
// broken deck's first error (fail-fast) versus every error (`--all`) in the
// fixed whole-deck-validation order, the `--json` report on stdout with its
// error shape and no suggestion key, the text summary/errors writer split, and
// the usage error's exit 2 with plain stderr even under --json. Every one-shot
// run prints no `Serving slides at <url>` marker, and a sentinel-writing stub
// opener first on PATH proves no browser-opener child was spawned.
func TestCLICheck(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	// The offline belt: any HTTP(S) client in kalide that honours the proxy
	// environment fails its connection instead of reaching the network. check
	// never constructs one.
	h.EnableOfflineProxy()
	writeCLIDeckFixture(t, h)

	// --- The valid deck: exit 0, the summary on stdout, nothing on stderr. ---
	stdout, stderr, code := h.Run("check")
	if code != 0 {
		t.Fatalf("kalide check (valid deck) exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	const validSummary = "kalide check: valid\n"
	if stdout != validSummary {
		t.Errorf("kalide check (valid deck) stdout = %q, want %q", stdout, validSummary)
	}
	if stderr != "" {
		t.Errorf("kalide check (valid deck) stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, stdout, stderr)

	// The one-shot run reaches completion through the harness and spawns no
	// browser-opener child: with the platform opener command names shadowed by
	// sentinel-writing stubs first on PATH, no sentinel file appears.
	guardedOut, guardedErr, guardedCode := runWithOpenerGuard(t, h, "check")
	if guardedCode != 0 {
		t.Fatalf("kalide check (valid deck, opener guard) exit = %d, want 0 (stderr = %q)", guardedCode, guardedErr)
	}
	if guardedOut != validSummary {
		t.Errorf("kalide check (valid deck, opener guard) stdout = %q, want %q", guardedOut, validSummary)
	}
	assertNoServingLine(t, guardedOut, guardedErr)

	// --- The --json report on the valid deck: valid, "valid", no errors. ---
	stdout, stderr, code = h.Run("check", "--json")
	if code != 0 {
		t.Fatalf("kalide check --json (valid deck) exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide check --json (valid deck) stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, stdout, stderr)
	report, raw := decodeCheckReport(t, stdout)
	assertOnlyKeys(t, "the --json report", raw, "valid", "summary", "errors")
	if !report.Valid {
		t.Errorf("kalide check --json (valid deck) valid = false, want true (report = %s)", stdout)
	}
	if report.Summary != "valid" {
		t.Errorf("kalide check --json (valid deck) summary = %q, want %q", report.Summary, "valid")
	}
	if len(report.Errors) != 0 {
		t.Errorf("kalide check --json (valid deck) errors = %v, want none", report.Errors)
	}

	// --- The broken deck: fail-fast reports the single first error. ---
	writeCLIBrokenDeck(t, h)

	stdout, stderr, code = h.Run("check")
	if code != 1 {
		t.Fatalf("kalide check (broken deck) exit = %d, want 1 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "kalide check: 1 error\n" {
		t.Errorf("kalide check (broken deck) stdout = %q, want %q", stdout, "kalide check: 1 error\n")
	}
	assertNoServingLine(t, stdout, stderr)
	firstLine := singleLine(t, stderr)
	if want := cliHeadingErrorText(); !strings.Contains(firstLine, want) {
		t.Errorf("kalide check (broken deck) stderr = %q, want the first error to carry %q", firstLine, want)
	}
	if wantPrefix := filepath.FromSlash("slides/1-broken.md") + " › heading: "; !strings.HasPrefix(firstLine, wantPrefix) {
		t.Errorf("kalide check (broken deck) stderr = %q, want the first error positioned at %q", firstLine, wantPrefix)
	}

	// --- --all collects every error in the fixed whole-deck-validation order:
	// the slide frontmatter's fields in declaration order (heading, then
	// status). The first error is byte-for-byte the fail-fast one. ---
	stdout, stderr, code = h.Run("check", "--all")
	if code != 1 {
		t.Fatalf("kalide check --all (broken deck) exit = %d, want 1 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "kalide check: 2 errors\n" {
		t.Errorf("kalide check --all (broken deck) stdout = %q, want %q", stdout, "kalide check: 2 errors\n")
	}
	assertNoServingLine(t, stdout, stderr)
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("kalide check --all (broken deck) stderr = %q, want exactly 2 errors", stderr)
	}
	if lines[0] != firstLine {
		t.Errorf("kalide check --all first error = %q, want the fail-fast error %q", lines[0], firstLine)
	}
	if want := cliStatusErrorText(); !strings.Contains(lines[1], want) {
		t.Errorf("kalide check --all second error = %q, want it to carry %q", lines[1], want)
	}
	if wantPrefix := filepath.FromSlash("slides/1-broken.md") + " › status: "; !strings.HasPrefix(lines[1], wantPrefix) {
		t.Errorf("kalide check --all second error = %q, want it positioned at %q", lines[1], wantPrefix)
	}

	// --- The --json report on the broken deck: fail-fast keeps one error,
	// --all every error, each in the same order and shape. ---
	stdout, stderr, code = h.Run("check", "--json")
	if code != 1 {
		t.Fatalf("kalide check --json (broken deck) exit = %d, want 1 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide check --json (broken deck) stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, stdout, stderr)
	report, raw = decodeCheckReport(t, stdout)
	assertOnlyKeys(t, "the --json report", raw, "valid", "summary", "errors")
	assertOnlyKeys(t, "the first --json error", firstReportError(t, raw), "file", "line", "path", "message")
	if report.Valid {
		t.Errorf("kalide check --json (broken deck) valid = true, want false (report = %s)", stdout)
	}
	if report.Summary != "1 error" {
		t.Errorf("kalide check --json (broken deck) summary = %q, want %q", report.Summary, "1 error")
	}
	if len(report.Errors) != 1 {
		t.Fatalf("kalide check --json (broken deck) errors = %v, want exactly one", report.Errors)
	}
	if got := report.Errors[0].File; got != "slides/1-broken.md" {
		t.Errorf("kalide check --json (broken deck) error file = %q, want %q", got, "slides/1-broken.md")
	}
	if got := report.Errors[0].Path; len(got) != 1 || got[0] != "heading" {
		t.Errorf("kalide check --json (broken deck) error path = %v, want [heading]", got)
	}
	if got, want := report.Errors[0].Message, cliHeadingErrorMessage(); got != want {
		t.Errorf("kalide check --json (broken deck) error message = %q, want %q", got, want)
	}
	// The text error is exactly the JSON error positioned in the slide and
	// prefixed by its path, so the two report modes cannot drift.
	if want := firstLine; !strings.HasSuffix(want, " › heading: "+report.Errors[0].Message) {
		t.Errorf("kalide check text error %q and --json message %q disagree", want, report.Errors[0].Message)
	}

	stdout, stderr, code = h.Run("check", "--all", "--json")
	if code != 1 {
		t.Fatalf("kalide check --all --json (broken deck) exit = %d, want 1 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide check --all --json (broken deck) stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, stdout, stderr)
	report, raw = decodeCheckReport(t, stdout)
	if report.Valid {
		t.Errorf("kalide check --all --json (broken deck) valid = true, want false (report = %s)", stdout)
	}
	if report.Summary != "2 errors" {
		t.Errorf("kalide check --all --json (broken deck) summary = %q, want %q", report.Summary, "2 errors")
	}
	if len(report.Errors) != 2 {
		t.Fatalf("kalide check --all --json (broken deck) errors = %v, want two", report.Errors)
	}
	for i, wantPath := range [][]string{{"heading"}, {"status"}} {
		got := report.Errors[i].Path
		if len(got) != 1 || got[0] != wantPath[0] {
			t.Errorf("kalide check --all --json error %d path = %v, want %v", i, got, wantPath)
		}
		if got := report.Errors[i].File; got != "slides/1-broken.md" {
			t.Errorf("kalide check --all --json error %d file = %q, want %q", i, got, "slides/1-broken.md")
		}
	}
	if got, want := report.Errors[1].Message, cliStatusErrorMessage(); got != want {
		t.Errorf("kalide check --all --json second error message = %q, want %q", got, want)
	}
	for i, e := range reportErrors(t, raw) {
		assertOnlyKeys(t, fmt.Sprintf("the --all --json error %d", i), e, "file", "line", "path", "message")
	}

	// --- A usage error is exit 2 with plain stderr and empty stdout, even
	// under --json (no report is emitted). ---
	stdout, stderr, code = h.Run("check", "--bogus")
	if code != 2 {
		t.Fatalf("kalide check --bogus exit = %d, want 2 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("kalide check --bogus stdout = %q, want empty", stdout)
	}
	for _, want := range []string{`kalide check: unexpected argument "--bogus"`, "Usage:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("kalide check --bogus stderr = %q, want it to mention %q", stderr, want)
		}
	}
	assertNoServingLine(t, stdout, stderr)

	stdout, stderr, code = h.Run("check", "--json", "--bogus")
	if code != 2 {
		t.Fatalf("kalide check --json --bogus exit = %d, want 2 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("kalide check --json --bogus stdout = %q, want no report on a usage error", stdout)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("kalide check --json --bogus stderr = %q, want the plain usage text", stderr)
	}
	assertNoServingLine(t, stdout, stderr)
}

// TestCLIRender is the plan.phase-10 task-14 end-to-end test: `kalide render`
// selecting a slide by position label and by label to stdout, `--fragment`
// yielding only the slide's `<section>` markup against the default full page,
// `-o` writing one `<number>[letter]-<label>.html` per slide in deck order, the
// usage errors (no slide and no -o; `--full` with `--fragment`) exiting 2, a
// broken deck exiting 1, and byte-identical output across two runs with each
// slide's anchor id equal to its label. Every run prints no
// `Serving slides at <url>` marker and spawns no browser-opener child, and the
// offline proxy environment is armed, so render starts no server, binds no port
// and makes no network connection.
func TestCLIRender(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds and drives the real kalide binary")
	}

	h := NewHarness(t)
	h.EnableOfflineProxy()
	writeCLIDeckFixture(t, h)

	// --- render <position-label> to stdout: the default full page. ---
	full, stderr, code := h.Run("render", "1")
	if code != 0 {
		t.Fatalf("kalide render 1 exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide render 1 stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, full, stderr)
	assertFullPage(t, full, "intro", "Intro")

	// --- render <label> to stdout selects the same single slide. ---
	byLabel, stderr, code := h.Run("render", "detail")
	if code != 0 {
		t.Fatalf("kalide render detail exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide render detail stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, byLabel, stderr)
	assertFullPage(t, byLabel, "detail", "Detail")

	// A vertical slide is selectable by its position label (2a) and by its
	// deck-unique label (extra); both name the same slide.
	vertical, stderr, code := h.Run("render", "2a")
	if code != 0 {
		t.Fatalf("kalide render 2a exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide render 2a stderr = %q, want empty", stderr)
	}
	assertFullPage(t, vertical, "extra", "Extra")
	byVerticalLabel, stderr, code := h.Run("render", "extra")
	if code != 0 {
		t.Fatalf("kalide render extra exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if byVerticalLabel != vertical {
		t.Errorf("kalide render extra differs from kalide render 2a; the label and position label must select the same slide")
	}

	// --- --fragment yields only the slide's <section> markup. ---
	fragment, stderr, code := h.Run("render", "1", "--fragment")
	if code != 0 {
		t.Fatalf("kalide render 1 --fragment exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide render 1 --fragment stderr = %q, want empty", stderr)
	}
	assertNoServingLine(t, fragment, stderr)
	assertFragment(t, fragment, "intro", "Intro")

	// --- byte-identical output across two runs. ---
	again, stderr, code := h.Run("render", "1")
	if code != 0 {
		t.Fatalf("kalide render 1 (second run) exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if again != full {
		t.Errorf("kalide render 1 is not byte-identical across two runs:\nfirst = %q\nsecond = %q", full, again)
	}

	// --- The opener guard: render never spawns a platform browser opener. ---
	guarded, guardedErr, guardedCode := runWithOpenerGuard(t, h, "render", "1", "--fragment")
	if guardedCode != 0 {
		t.Fatalf("kalide render 1 --fragment (opener guard) exit = %d, want 0 (stderr = %q)", guardedCode, guardedErr)
	}
	if guarded != fragment {
		t.Errorf("kalide render 1 --fragment with the opener guard on PATH differs from the plain run")
	}
	assertNoServingLine(t, guarded, guardedErr)

	// --- -o writes one file per slide in deck order, each named
	// <number>[letter]-<label>.html and carrying its label as the anchor id. ---
	out1 := h.Path("out1")
	_, stderr, code = h.Run("render", "-o", out1)
	if code != 0 {
		t.Fatalf("kalide render -o out1 exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("kalide render -o out1 stderr = %q, want empty", stderr)
	}
	written := []struct{ name, label, heading string }{
		{"1-intro.html", "intro", "Intro"},
		{"2-detail.html", "detail", "Detail"},
		{"2a-extra.html", "extra", "Extra"},
	}
	entries, err := os.ReadDir(out1)
	if err != nil {
		t.Fatalf("read the -o directory %s: %v", out1, err)
	}
	if len(entries) != len(written) {
		t.Errorf("render -o wrote %d files, want %d: %v", len(entries), len(written), entryNames(entries))
	}
	for _, w := range written {
		data, err := os.ReadFile(filepath.Join(out1, w.name))
		if err != nil {
			t.Errorf("render -o did not write %s: %v", w.name, err)
			continue
		}
		assertFullPage(t, string(data), w.label, w.heading)
	}

	// The -o output is byte-identical across two runs too.
	out2 := h.Path("out2")
	_, stderr, code = h.Run("render", "-o", out2)
	if code != 0 {
		t.Fatalf("kalide render -o out2 exit = %d, want 0 (stderr = %q)", code, stderr)
	}
	for _, w := range written {
		first, err := os.ReadFile(filepath.Join(out1, w.name))
		if err != nil {
			t.Fatalf("read %s: %v", w.name, err)
		}
		second, err := os.ReadFile(filepath.Join(out2, w.name))
		if err != nil {
			t.Errorf("the second render -o did not write %s: %v", w.name, err)
			continue
		}
		if !bytes.Equal(first, second) {
			t.Errorf("render -o %s is not byte-identical across two runs", w.name)
		}
	}

	// --- Usage errors: no slide and no -o; --full with --fragment. ---
	stdout, stderr, code := h.Run("render")
	if code != 2 {
		t.Fatalf("kalide render (no slide, no -o) exit = %d, want 2 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("kalide render (no slide, no -o) stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"kalide render:", "Usage:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("kalide render (no slide, no -o) stderr = %q, want it to mention %q", stderr, want)
		}
	}
	assertNoServingLine(t, stdout, stderr)

	stdout, stderr, code = h.Run("render", "1", "--full", "--fragment")
	if code != 2 {
		t.Fatalf("kalide render 1 --full --fragment exit = %d, want 2 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("kalide render 1 --full --fragment stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"--full and --fragment", "Usage:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("kalide render 1 --full --fragment stderr = %q, want it to mention %q", stderr, want)
		}
	}
	assertNoServingLine(t, stdout, stderr)

	// --- A broken deck exits 1 with the first error, no server started. ---
	writeCLIBrokenDeck(t, h)
	stdout, stderr, code = h.Run("render", "1")
	if code != 1 {
		t.Fatalf("kalide render 1 (broken deck) exit = %d, want 1 (stdout = %q, stderr = %q)", code, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("kalide render 1 (broken deck) stdout = %q, want empty", stdout)
	}
	if want := cliHeadingErrorText(); !strings.Contains(stderr, want) {
		t.Errorf("kalide render 1 (broken deck) stderr = %q, want it to carry %q", stderr, want)
	}
	assertNoServingLine(t, stdout, stderr)
}

// assertFullPage asserts html is the standalone deck page holding the slide
// with the given label and heading: the page shell, the reveal.js initialiser
// and the slide's anchored <section> markup.
func assertFullPage(t *testing.T, html, label, heading string) {
	t.Helper()

	for _, want := range []string{
		"<!DOCTYPE html>",
		"Reveal.initialize",
		`<section id="` + label + `"`,
		`<h1 class="main-heading">` + heading + `</h1>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("full page for label %q is missing %q:\n%s", label, want, html)
		}
	}
}

// assertFragment asserts html is only the slide's `<section>` markup — the
// fragment the deck page embeds — anchored on the slide's label and carrying no
// page-shell markup.
func assertFragment(t *testing.T, html, label, heading string) {
	t.Helper()

	if !strings.HasPrefix(html, `<section id="`+label+`"`) {
		t.Errorf("fragment for label %q does not begin with its anchored <section>:\n%s", label, html)
	}
	if want := `<h1 class="main-heading">` + heading + `</h1>`; !strings.Contains(html, want) {
		t.Errorf("fragment for label %q is missing %q:\n%s", label, want, html)
	}
	for _, forbidden := range []string{"<!DOCTYPE html>", "Reveal.initialize", "<html"} {
		if strings.Contains(html, forbidden) {
			t.Errorf("fragment for label %q carries page-shell markup %q:\n%s", label, forbidden, html)
		}
	}
}

// entryNames returns the names of dir entries, for a failure message.
func entryNames(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// cliHeadingErrorMessage is the JSON message for the broken slide's missing
// required `heading` field: validate.checkErrorMessage joins the field's
// "what is wrong" and "how to fix it" text the way Format renders them.
func cliHeadingErrorMessage() string {
	return `required: field "heading" is required but missing — add a heading: value`
}

// cliHeadingErrorText is the trailing part of the formatted first error.
func cliHeadingErrorText() string {
	return "heading: " + cliHeadingErrorMessage()
}

// cliStatusErrorMessage is the JSON message for the broken slide's out-of-range
// `status` enum value.
func cliStatusErrorMessage() string {
	return `enum: "bogus" is not one of the allowed variants (draft, published) — use one of: draft, published`
}

// cliStatusErrorText is the trailing part of the formatted second error.
func cliStatusErrorText() string {
	return "status: " + cliStatusErrorMessage()
}

// cliCheckReport mirrors the machine-readable `kalide check --json` report
// (internal/cli's checkReport). Every error carries the error-reporting shape —
// the deck-relative file, the line (0 when unknown), the path segments and the
// joined message — and the report deliberately has no suggestion key.
type cliCheckReport struct {
	Valid   bool                  `json:"valid"`
	Summary string                `json:"summary"`
	Errors  []cliCheckReportError `json:"errors"`
}

// cliCheckReportError is one error in the report.
type cliCheckReportError struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Path    []string `json:"path"`
	Message string   `json:"message"`
}

// decodeCheckReport parses stdout as the `kalide check --json` report, both into
// the typed report and into a raw map so the exact key set (and the absence of a
// suggestion key) can be asserted. It fails the test when stdout is not a JSON
// report.
func decodeCheckReport(t *testing.T, stdout string) (cliCheckReport, map[string]any) {
	t.Helper()

	var report cliCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("kalide check --json stdout is not a JSON report: %v\nstdout:\n%s", err, stdout)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("kalide check --json stdout is not a JSON object: %v\nstdout:\n%s", err, stdout)
	}
	return report, raw
}

// firstReportError returns the raw first error object of a decoded report.
func firstReportError(t *testing.T, raw map[string]any) map[string]any {
	t.Helper()

	errs := reportErrors(t, raw)
	if len(errs) == 0 {
		t.Fatalf("the report carries no errors, want at least one")
	}
	return errs[0]
}

// reportErrors returns the report's raw error objects.
func reportErrors(t *testing.T, raw map[string]any) []map[string]any {
	t.Helper()

	items, ok := raw["errors"].([]any)
	if !ok {
		t.Fatalf("the report errors is %T, want a JSON array", raw["errors"])
	}
	out := make([]map[string]any, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("the report error %d is %T, want a JSON object", i, item)
		}
		out = append(out, m)
	}
	return out
}

// assertOnlyKeys fails the test unless m carries exactly the want keys.
func assertOnlyKeys(t *testing.T, what string, m map[string]any, want ...string) {
	t.Helper()

	seen := make(map[string]bool, len(want))
	for _, k := range want {
		seen[k] = true
	}
	for k := range m {
		if !seen[k] {
			t.Errorf("%s carries an unexpected key %q (want only %v)", what, k, want)
		}
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("%s is missing the key %q", what, k)
		}
	}
}

// singleLine returns s with its single trailing newline removed, failing the
// test when s is not exactly one line.
func singleLine(t *testing.T, s string) string {
	t.Helper()

	body := strings.TrimSuffix(s, "\n")
	if strings.Contains(body, "\n") {
		t.Fatalf("want exactly one line, got:\n%s", s)
	}
	if body == "" {
		t.Fatalf("want one non-empty line, got an empty string")
	}
	return body
}

// assertNoServingLine fails the test when either output stream carries the
// `Serving slides at <url>` marker the harness's servingPrefix/findServingURL
// parse from `kalide start`, proving a one-shot command started no server.
func assertNoServingLine(t *testing.T, stdout, stderr string) {
	t.Helper()

	for _, out := range []string{stdout, stderr} {
		if strings.Contains(out, servingPrefix) {
			t.Errorf("output carries the server marker %q, want no server started:\n%s", servingPrefix, out)
		}
	}
}

// openerGuard shadows the platform browser-opener command names with stub
// executables that record any invocation in a sentinel file, so a one-shot
// command that must not open a browser can be proven never to run one.
type openerGuard struct {
	dir      string
	sentinel string
}

// newOpenerGuard writes a sentinel-writing stub for every browser-opener name
// kalide's systemOpenURL may launch (`open`, `rundll32`, `xdg-open`) into a new
// temp directory and returns a guard whose dir must be placed first on PATH.
func newOpenerGuard(t *testing.T) *openerGuard {
	t.Helper()

	dir := t.TempDir()
	g := &openerGuard{dir: dir, sentinel: filepath.Join(dir, "opener-invoked")}
	for _, name := range []string{"open", "rundll32", "xdg-open"} {
		writeOpenerStub(t, dir, name, g.sentinel)
	}
	return g
}

// assertUnused fails the test when any stub opener ran (its sentinel exists).
func (g *openerGuard) assertUnused(t *testing.T) {
	t.Helper()

	if _, err := os.Stat(g.sentinel); err == nil {
		t.Errorf("a platform browser opener was spawned; %s exists", g.sentinel)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat opener sentinel %s: %v", g.sentinel, err)
	}
}

// writeOpenerStub writes an executable named name into dir that appends to
// sentinel when run. On Windows it is a .bat (found through PATHEXT); elsewhere
// a POSIX shell script with the executable bit set.
func writeOpenerStub(t *testing.T, dir, name, sentinel string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		body := "@echo off\r\necho invoked> \"" + sentinel + "\"\r\n"
		p := filepath.Join(dir, name+".bat")
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatalf("write opener stub %s: %v", p, err)
		}
		return
	}
	body := "#!/bin/sh\n: > " + shQuote(sentinel) + "\n"
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("write opener stub %s: %v", p, err)
	}
}

// shQuote single-quotes s for a POSIX shell, escaping embedded single quotes.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runWithOpenerGuard runs `kalide args...` like Harness.Run but with the opener
// guard's stub directory first on the child's PATH, then fails the test when any
// stub opener was invoked. It is how a one-shot command's "spawns no browser"
// claim is observed on the real binary.
func runWithOpenerGuard(t *testing.T, h *Harness, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	g := newOpenerGuard(t)
	cmd := h.Command(args...)
	cmd.Env = envWith(cmd.Env, "PATH", g.dir+string(os.PathListSeparator)+envValue(cmd.Env, "PATH"))

	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	g.assertUnused(t)
	if err == nil {
		return out.String(), errOut.String(), 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), errOut.String(), ee.ExitCode()
	}
	t.Fatalf("e2e: run kalide %v with opener guard: %v", args, err)
	return "", "", -1
}

// envValue returns the value of key in a key=value environment slice,
// case-insensitively (so it also works on Windows).
func envValue(env []string, key string) string {
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i > 0 && strings.EqualFold(kv[:i], key) {
			return kv[i+1:]
		}
	}
	return ""
}
