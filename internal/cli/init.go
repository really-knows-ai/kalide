package cli

// This file implements `kalide init [path] [--local]` (phase-8 task 3;
// phase-3 tasks 1-2): requirements.requirement.cli-init,
// requirements.requirement.cli-init-refuse-existing and
// requirements.requirement.init-library-resolution.
//
// `kalide init` scaffolds a deck into the current working directory. The
// filesystem work lives in internal/scaffold: with no path Init writes the
// minimal starter deck and its local templates/ library; with an external
// library path InitExternal writes a deck referencing that library (no local
// templates/, no starter slide) after validating it. This handler parses the
// optional path and --local flag, resolves which template library to use, and
// turns scaffold's outcome into the process exit code and user-facing text:
//
//   - the template library is resolved first match wins: an explicit [path]
//     (an external library, the path stored as given), else — unless --local —
//     the configured user-level default-template-dir read from the per-user
//     config store (an external library, its absolute stored value), else the
//     built-in starter deck, which --local forces even when a default is
//     configured;
//   - the external branches validate the library and the deck's default theme
//     fail-fast before writing anything; the seed branch writes the seed and
//     its local templates/ block applies;
//   - success prints the paths that were created and exits 0 — the external
//     branches print initExternalCreatedMessage naming the stored library path
//     and the seed branch prints initCreatedMessage;
//   - a refusal — slides/, assets/ or kalide.yaml already present (with or
//     without a path; a local templates/ blocks only the seed form) — or an
//     invalid/missing external library, or a library that does not resolve the
//     deck's default theme, prints scaffold's message to stderr and exits
//     non-zero, having written nothing;
//   - a per-user config file that exists but cannot be read or parsed is an
//     error: this handler prints it to stderr and exits 1, having written
//     nothing, and never falls back to the seed;
//   - a malformed argument list (an unknown/option-looking token, more than one
//     path, or --local combined with a path) is a usage error: the message,
//     then usage, to stderr and exit 2.
//
// Only `default-template-dir` is read, and only here: `kalide start` and
// `kalide templates` never consult the user config. All output goes to the
// supplied writers, so the command is testable without a real terminal.

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/really-knows-ai/kalide/internal/config"
	"github.com/really-knows-ai/kalide/internal/scaffold"
)

// initDefaultTemplateDirKey is the user-config key whose value runInit uses as
// the template library when no path argument is given. It mirrors the sole key
// of internal/config's RecognisedKeys set.
const initDefaultTemplateDirKey = "default-template-dir"

// initCreatedMessage is the success text for `kalide init`. It lists the
// top-level entries scaffold.Init creates from the embedded hello seed,
// spelled as deck-relative, slash-separated paths: the deck-root agent guide,
// the deck config, the starter slide, the hello slide template and default
// theme, and the empty assets/ directory for the author's images.
const initCreatedMessage = `Created a starter deck:
  AGENTS.md
  kalide.yaml
  slides/1-hello.md
  templates/library.yaml
  templates/slides/hello/template.yaml
  templates/slides/hello/layout.html.tmpl
  templates/slides/hello/example.md
  templates/themes/default/theme.css
  assets/
`

// initExternalCreatedMessage is the success text for the external-library
// branches of `kalide init`: an explicit path argument, or the configured
// default-template-dir. It names the external library the deck references and
// lists the top-level entries InitExternal creates: the deck-root agent guide,
// the deck config and the empty slides/ and assets/ directories (an empty
// directory is not a file, so it is listed with its trailing slash). There is
// deliberately no local templates/ entry.
const initExternalCreatedMessage = `Created a deck referencing the external template library %s:
  AGENTS.md
  kalide.yaml
  slides/
  assets/
`

// parseInitArgs parses the arguments after `kalide init`: an optional
// external-library path plus the optional --local flag, in any order, and
// nothing else (there is still no --force). It returns the path (empty when
// absent) and whether --local was given. Any malformed argument list is a usage
// error reported by the returned error's message:
//
//   - an unknown option-looking token (any argument beginning with "-" other
//     than --local, such as the retired --force) is rejected;
//   - more than one path argument is rejected;
//   - --local combined with a path is rejected, since --local forces the
//     built-in starter deck and therefore contradicts an external library.
//
// It performs no filesystem work and writes nothing; the caller turns a
// non-nil error into usage text on stderr and exit 2.
func parseInitArgs(args []string) (path string, local bool, err error) {
	havePath := false
	for _, arg := range args {
		switch {
		case arg == "--local":
			local = true
		case strings.HasPrefix(arg, "-"):
			// There are no other init options. A token that looks like a flag
			// (the retired `--force`, say) is a usage error, not a path.
			return "", false, fmt.Errorf("kalide init: unexpected option %q (expected at most one library path and the optional --local)", arg)
		default:
			if havePath {
				return "", false, fmt.Errorf("kalide init: unexpected argument %q (expected at most one library path)", arg)
			}
			path = arg
			havePath = true
		}
	}
	if local && havePath {
		return "", false, fmt.Errorf("kalide init: --local cannot be combined with a library path")
	}
	return path, local, nil
}

// runInit implements `kalide init [path] [--local]`. It returns the process
// status code: 0 when the deck is written; 1 when the current directory cannot
// be resolved, the per-user config cannot be read, or scaffold refuses (a
// blocking path already exists, the external library is invalid/missing, or it
// does not resolve the deck's default theme); 2 for malformed arguments.
//
// It resolves the template library first match wins — an explicit path, else
// the configured default-template-dir, else the built-in starter deck — then
// hands the chosen path to scaffold.Init, which writes nothing on refusal or
// invalid input. An empty external path selects the no-arg hello-seed form.
func runInit(args []string, stdout, stderr io.Writer) int {
	path, local, err := parseInitArgs(args)
	if err != nil {
		// parseInitArgs' error message is the usage first line; print it, a
		// blank line and the shared usage text, as every usage error does.
		fmt.Fprintf(stderr, "%v\n\n", err)
		fmt.Fprint(stderr, usage)
		return 2
	}

	// Branch 1: an explicit external-library path, stored as given. Branch 2:
	// the configured user-level default-template-dir, whose stored value is
	// already absolute. --local forces branch 3, the built-in starter deck, so
	// the config store is not consulted at all.
	external := path
	if external == "" && !local {
		values, err := config.Load()
		if err != nil {
			// A config file that exists but cannot be read or parsed is an
			// error; it never silently falls back to the seed deck.
			fmt.Fprintf(stderr, "kalide init: %v\n", err)
			return 1
		}
		external = values[initDefaultTemplateDirKey]
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "kalide init: %v\n", err)
		return 1
	}

	// scaffold.Init writes nothing on refusal or invalid input and returns a
	// message that already names the command and the blocking path(s) or the
	// offending library path, so it is printed verbatim rather than
	// re-prefixed. An empty external path selects the no-arg hello-seed form.
	if err := scaffold.Init(cwd, external); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if external != "" {
		fmt.Fprintf(stdout, initExternalCreatedMessage, external)
		return 0
	}
	fmt.Fprint(stdout, initCreatedMessage)
	return 0
}
