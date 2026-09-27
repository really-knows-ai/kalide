// Package cli implements the kalide command-line surface.
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

// usage is the plain-language help text shown for `kalide help`, for a bare
// invocation, and after a usage error.
const usage = `kalide — author and present Markdown slide decks

Usage:
  kalide init                Create a starter deck and templates/ library
                             in the current directory
  kalide start [--port N] [--no-open]
                             Validate and serve the deck, then open a browser
  kalide templates [name]    List the project's templates/ library, or show
                             one template by name
  kalide version             Print the build-stamped version (also --version,
                             -v)
  kalide help                Show this help
`

// Run parses args (the full os.Args, so args[0] is the program name) and
// dispatches to a command. It returns the process status code: 0 on success,
// non-zero on error.
//
// version is the build-stamped release version supplied by cmd/kalide (the
// `main.version` variable set via -ldflags -X, or "dev" for unbuilt/source
// runs); it is reported verbatim by the version surface.
//
//   - no arguments and `help` print usage and return 0;
//   - `version`, `--version` and `-v` print a single line `kalide <version>`
//     to stdout and return 0;
//   - `templates [name]` routes to runTemplates, which lists the project's
//     templates/ library or documents one template by name;
//   - `start [--port N] [--no-open]` routes to runStart, which validates the
//     whole deck and then serves it with live reload until a shutdown signal;
//   - `init` routes to runInit, which seeds a minimal templates/ library and a
//     starter deck into the current directory, or refuses non-zero when one of
//     the deck paths already exists;
//   - an unknown command or malformed arguments print usage and return 2.
//
// Both `start` and `templates` operate on decks whose reserved deck-wide
// `.deck`/`.slide` template context is enforced by internal/template's
// reserved-name rejection and injected at render time by internal/render.
//
// single-binary: cli.Run stays offline, self-contained and OS-neutral across
// all six supported targets (darwin/arm64, darwin/amd64, windows/amd64,
// windows/arm64, linux/amd64, linux/arm64) — it needs no runtime, network or
// install step (requirements.requirement.single-binary).
func Run(args []string, version string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch cmd := args[1]; cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "kalide %s\n", version)
		return 0
	case "init":
		return runInit(args[2:], stdout, stderr)
	case "start":
		return runStart(args[2:], stdout, stderr)
	case "templates":
		if len(args) > 3 {
			fmt.Fprintf(stderr, "kalide templates: expected at most one template name\n\n")
			fmt.Fprint(stderr, usage)
			return 2
		}
		return runTemplates(args[2:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "kalide: unknown command %q\n\n", cmd)
		fmt.Fprint(stderr, usage)
		return 2
	}
}
