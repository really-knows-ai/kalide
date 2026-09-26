// Package cli implements the eypres command-line surface.
//
// Run is the single entry point: it dispatches os.Args to a command and
// returns the process status code. Handlers write user-facing output to the
// supplied stdout/stderr writers so the package is testable without a real
// terminal.
package cli

import (
	"fmt"
	"io"
)

// usage is the plain-language help text shown for `eypres help`, for a bare
// invocation, and after a usage error.
const usage = `eypres — author and present Markdown slide decks

Usage:
  eypres init                Create a starter deck in the current directory
  eypres start [--port N] [--no-open]
                             Validate and serve the deck, then open a browser
  eypres templates [name]    List built-in templates, or show one by name
  eypres help                Show this help
`

// Run parses args (the full os.Args, so args[0] is the program name) and
// dispatches to a command. It returns the process status code: 0 on success,
// non-zero on error.
//
//   - no arguments and `help` print usage and return 0;
//   - `templates [name]` routes to runTemplates, which lists the built-in
//     templates or documents one by name;
//   - `start [--port N] [--no-open]` routes to runStart, which validates the
//     whole deck and then serves it with live reload until a shutdown signal;
//   - `init` routes to its command, which is not implemented yet and returns a
//     "not implemented" error;
//   - an unknown command or malformed arguments print usage and return 2.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch cmd := args[1]; cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "init":
		return notImplemented("init", stderr)
	case "start":
		return runStart(args[2:], stdout, stderr)
	case "templates":
		if len(args) > 3 {
			fmt.Fprintf(stderr, "eypres templates: expected at most one template name\n\n")
			fmt.Fprint(stderr, usage)
			return 2
		}
		return runTemplates(args[2:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "eypres: unknown command %q\n\n", cmd)
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// notImplemented reports that a recognised command has no implementation yet.
// Later phases replace these stubs with real handlers.
func notImplemented(name string, stderr io.Writer) int {
	fmt.Fprintf(stderr, "eypres: the %q command is not implemented yet\n", name)
	return 1
}
