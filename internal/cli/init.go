package cli

// This file implements `kalide init [path]` (phase-8 task 3; phase-3 task 6):
// requirements.requirement.cli-init and
// requirements.requirement.cli-init-refuse-existing.
//
// `kalide init` scaffolds a deck into the current working directory. The
// filesystem work lives in internal/scaffold: with no [path], Init writes the
// minimal starter deck and its local templates/ library; with an optional
// [path], InitExternal writes a deck referencing the external template library
// there (no local templates/, no starter slide) after validating it. This
// handler resolves the current directory, parses the optional path, rejects
// more than one argument as a usage error (there is no --force), and turns
// scaffold's outcome into the process exit code and user-facing text:
//
//   - success prints the paths that were created and exits 0;
//   - a refusal — slides/, assets/ or kalide.yaml already present (with or
//     without a path; a local templates/ blocks only the no-path form) — or an
//     invalid/missing external library, or a library that does not resolve the
//     deck's default theme, prints scaffold's message to stderr and exits
//     non-zero, having written nothing;
//   - more than one argument, or an option-looking argument, is a usage error:
//     usage to stderr and exit 2.
//
// All output goes to the supplied writers, so the command is testable without
// a real terminal.

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/really-knows-ai/kalide/internal/scaffold"
)

// initCreatedMessage is the success text for `kalide init`. It lists the
// top-level entries scaffold.Init creates from the embedded hello seed,
// spelled as deck-relative, slash-separated paths: the deck config, the
// starter slide, the hello slide template and default theme, and the empty
// assets/ directory for the author's images.
const initCreatedMessage = `Created a starter deck:
  kalide.yaml
  slides/1-hello.md
  templates/library.yaml
  templates/slides/hello/template.yaml
  templates/slides/hello/layout.html.tmpl
  templates/slides/hello/example.md
  templates/themes/default/theme.css
  assets/
`

// initExternalCreatedMessage is the success text for `kalide init <path>`. It
// names the external library the deck references and lists the top-level
// entries InitExternal creates: the deck config and the empty slides/ and
// assets/ directories (an empty directory is not a file, so it is listed with
// its trailing slash). There is deliberately no local templates/ entry.
const initExternalCreatedMessage = `Created a deck referencing the external template library %s:
  kalide.yaml
  slides/
  assets/
`

// runInit implements `kalide init [path]`. It returns the process status code:
// 0 when the deck is written; 1 when the current directory cannot be resolved
// or scaffold refuses (a blocking path already exists, the external library is
// invalid/missing, or it does not resolve the deck's default theme); 2 for
// malformed arguments.
func runInit(args []string, stdout, stderr io.Writer) int {
	external := ""
	switch {
	case len(args) > 1:
		fmt.Fprintf(stderr, "kalide init: expected at most one library path\n\n")
		fmt.Fprint(stderr, usage)
		return 2
	case len(args) == 1 && strings.HasPrefix(args[0], "-"):
		// There are no init options. A token that looks like a flag (the
		// retired `--force`, say) is a usage error, not an external path.
		fmt.Fprintf(stderr, "kalide init: unexpected option %q (expected no arguments other than an optional library path)\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	case len(args) == 1:
		external = args[0]
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
