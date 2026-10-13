package cli

// This file implements `kalide check [--all] [--json]` (phase-10 task 1):
// requirements.requirement.cli-check and
// solution.component.cli-check.
//
// check is the non-serving validation entry point: it loads the deck's
// resolved template library layers and its theme exactly as `kalide start`
// does before serving (template.LoadDeckLibrary +
// cli.loadLibraryThemeRegistry + template.NewRegistryFromLibrary), then runs
// the whole-deck validator over the deck without opening a server, binding a
// port or reaching the network.
//
// By default it is fail-fast: it reports the single first error in the fixed
// whole-deck-validation order (validate.Validate), the same string terminal
// and browser show (error-reporting, byte-identical to what `kalide start`
// prints). With `--all` it collects every error in that same order
// (validate.ValidateAll). `--json` emits a machine-readable report on stdout —
// a valid boolean, a summary and each error's file/line/path/message —
// following the same collection mode; without `--json` the summary goes to
// stdout and the errors to stderr. A usage error is plain text on stderr even
// with `--json` and produces no report.
//
// Exit codes are 0 when the deck is valid, 1 when a validation error is found
// (or the library/theme cannot be loaded, which is reported as the first
// check error), and 2 for a usage error.
//
// runCheck is called from Run with the arguments after the `check` command
// word. All output goes to the supplied writers, so the command is testable
// without a real terminal.

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/displaypath"
	"github.com/really-knows-ai/kalide/internal/template"
	"github.com/really-knows-ai/kalide/internal/validate"
)

// checkOptions are the parsed `kalide check` flags.
type checkOptions struct {
	// all collects every error (validate.ValidateAll) instead of stopping at
	// the first (validate.Validate).
	all bool

	// json emits the machine-readable report on stdout instead of the text
	// summary.
	json bool
}

// checkReport is the machine-readable `--json` report. Valid is the deck's
// outcome, Summary is the same one-line summary the text mode prints, and
// Errors carries the collected errors in the fixed whole-deck-validation
// order (at most one without --all).
type checkReport struct {
	Valid   bool               `json:"valid"`
	Summary string             `json:"summary"`
	Errors  []checkReportError `json:"errors"`
}

// checkReportError is one error in the machine-readable report. It carries the
// error-reporting shape (error-reporting): the deck-relative File, the Line
// when known (0 otherwise), the Path segments and the Message — the "what is
// wrong" and "how to fix it" text joined exactly as validate.Format renders it.
type checkReportError struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Path    []string `json:"path"`
	Message string   `json:"message"`
}

// runCheck implements `kalide check [--all] [--json]`. It returns the process
// status code: 0 when the deck is valid, 1 when a validation error is found or
// the library/theme cannot be loaded, and 2 for a usage error.
func runCheck(args []string, stdout, stderr io.Writer) int {
	opts, err := parseCheckArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "kalide check: %v\n\n", err)
		fmt.Fprint(stderr, usage)
		return 2
	}

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "kalide check: %v\n", err)
		return 1
	}
	// Canonicalise the deck root once, before the loader sees it, so an error
	// naming a resolved layer or manifest is shown in the native spelling of
	// the root it was resolved against (matching runStart/runTemplates).
	root = displaypath.CanonicalRoot(root)

	// Resolve the deck's template library — every layer it draws from: the
	// external `templates:` library and the deck-root templates/ — exactly as
	// `kalide start` does before serving. The raw `templates:` value is read
	// directly here because deck.LoadConfig needs a theme registry, which comes
	// from the not-yet-resolved library; an unreadable config yields "" and the
	// loader then looks for the local templates/ directory.
	deckFS := os.DirFS(root)
	configured := ""
	if data, readErr := fs.ReadFile(deckFS, deck.ConfigFile); readErr == nil {
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) == nil {
			configured, _ = raw["templates"].(string)
		}
	}

	// A library or theme load failure is reported as the first check error: the
	// report holds exactly this one error, and with --all it still ends there,
	// because no registry exists to collect further errors from.
	library, err := template.LoadDeckLibrary(root, configured)
	if err != nil {
		return reportCheck(opts, stdout, stderr, []validate.ValidationError{templateLoadError(err)})
	}
	themeReg, err := loadLibraryThemeRegistry(library)
	if err != nil {
		return reportCheck(opts, stdout, stderr, []validate.ValidationError{templateLoadError(err)})
	}
	registry, err := template.NewRegistryFromLibrary(library)
	if err != nil {
		return reportCheck(opts, stdout, stderr, []validate.ValidationError{templateLoadError(err)})
	}

	// The whole-deck validator, fail-fast or collect-all.
	var errs []validate.ValidationError
	if opts.all {
		errs = validate.ValidateAll(deckFS, registry, themeReg)
	} else if verr, invalid := validate.Validate(deckFS, registry, themeReg); invalid {
		errs = []validate.ValidationError{verr}
	}
	return reportCheck(opts, stdout, stderr, errs)
}

// parseCheckArgs parses the arguments after the `check` command word:
// `--all` and `--json` in any order. Anything else is a usage error.
func parseCheckArgs(args []string) (checkOptions, error) {
	var opts checkOptions
	for _, arg := range args {
		switch arg {
		case "--all":
			opts.all = true
		case "--json":
			opts.json = true
		default:
			return checkOptions{}, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return opts, nil
}

// templateLoadError adapts a library or theme load failure into the single
// ValidationError the check report carries, matching how the reload pipeline
// reports the same failure (server.renderDefault). The offending path and
// reason are already in the loader's message, so it becomes the What with the
// `templates` file marker.
func templateLoadError(err error) validate.ValidationError {
	return validate.New("templates", 0, nil, err.Error(), "")
}

// reportCheck emits the outcome for the given errors and returns the process
// status code: 0 when there are none, 1 otherwise.
func reportCheck(opts checkOptions, stdout, stderr io.Writer, errs []validate.ValidationError) int {
	valid := len(errs) == 0
	summary := checkSummary(valid, len(errs))
	if opts.json {
		writeCheckJSON(stdout, valid, summary, errs)
	} else {
		fmt.Fprintf(stdout, "kalide check: %s\n", summary)
		for _, e := range errs {
			fmt.Fprintln(stderr, validate.Format(e))
		}
	}
	if valid {
		return 0
	}
	return 1
}

// writeCheckJSON writes the machine-readable report to stdout. The errors array
// is always present (empty when the deck is valid) so the report's shape is
// stable.
func writeCheckJSON(stdout io.Writer, valid bool, summary string, errs []validate.ValidationError) {
	report := checkReport{
		Valid:   valid,
		Summary: summary,
		Errors:  make([]checkReportError, 0, len(errs)),
	}
	for _, e := range errs {
		report.Errors = append(report.Errors, checkReportError{
			File:    e.File,
			Line:    e.Line,
			Path:    append([]string(nil), e.Path...),
			Message: checkErrorMessage(e),
		})
	}
	enc := json.NewEncoder(stdout)
	// The report is machine-readable and may embed a `>` in a path; escaping it
	// as \u003e would obscure the values for no HTML-safety gain.
	enc.SetEscapeHTML(false)
	_ = enc.Encode(report)
}

// checkSummary is the one-line outcome summary shared by the text output and
// the JSON report.
func checkSummary(valid bool, n int) string {
	if valid {
		return "valid"
	}
	if n == 1 {
		return "1 error"
	}
	return fmt.Sprintf("%d errors", n)
}

// checkErrorMessage joins an error's "what is wrong" and "how to fix it" text
// exactly as validate.Format's ` — ` clause does, so the JSON report's message
// matches the trailing part of the text error.
func checkErrorMessage(e validate.ValidationError) string {
	if e.Fix == "" {
		return e.What
	}
	return e.What + " — " + e.Fix
}
