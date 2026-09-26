package cli

// This file implements `eypres init` (phase-8 task 3):
// requirements.requirement.cli-init and
// requirements.requirement.cli-init-refuse-existing.
//
// `eypres init` scaffolds a starter deck into the current working directory.
// The filesystem work lives in internal/scaffold.Init; this handler resolves
// the current directory, rejects any extra argument (there is no --force), and
// turns Init's outcome into the process exit code and user-facing text:
//
//   - success prints the paths that were created and exits 0;
//   - a refusal — one of slides/, assets/ or eypres.yaml already present —
//     prints scaffold's message to stderr and exits non-zero, having written
//     nothing;
//   - an extra argument is a usage error: usage to stderr and exit 2.
//
// All output goes to the supplied writers, so the command is testable without
// a real terminal.

import (
	"fmt"
	"io"
	"os"

	"github.com/really-knows-ai/ey-present/internal/scaffold"
)

// initCreatedMessage is the success text for `eypres init`. It lists the
// top-level entries scaffold.Init creates from the embedded assets.Starter
// tree, spelled as deck-relative, slash-separated paths: the deck config, the
// two starter slides and the empty assets/ directory for the author's images.
const initCreatedMessage = `Created a starter deck:
  eypres.yaml
  slides/1-title.md
  slides/2-content.md
  assets/
`

// runInit implements `eypres init`. It returns the process status code: 0 when
// the starter deck is written; 1 when the current directory cannot be resolved
// or scaffold.Init refuses (a blocking path already exists); 2 for malformed
// arguments.
func runInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "eypres init: expected no arguments\n\n")
		fmt.Fprint(stderr, usage)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "eypres init: %v\n", err)
		return 1
	}

	// scaffold.Init writes nothing on refusal and returns a message that
	// already names the command and the blocking path(s), so it is printed
	// verbatim rather than re-prefixed.
	if err := scaffold.Init(cwd); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprint(stdout, initCreatedMessage)
	return 0
}
