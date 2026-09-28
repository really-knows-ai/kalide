package cli

// This file implements `kalide init-library <path>` (phase-04 tasks 4 and 5):
// requirements.requirement.init-library.
//
// `kalide init-library` scaffolds a new, deck-ready template library at the
// required <path>. The filesystem work lives in internal/scaffold.InitLibrary;
// this handler only validates the argument count and turns scaffold's outcome
// into the process exit code and user-facing text:
//
//   - success prints the paths that were created and exits 0;
//   - a refusal (a library.yaml or a standard layout entry already present), an
//     invalid target base name, or a target that exists as a non-directory
//     prints scaffold's message to stderr and exits 1, having written nothing;
//   - omitting <path>, passing extra arguments, or passing an option-looking
//     token is a usage error: usage to stderr and exit 2, nothing written.
//
// All output goes to the supplied writers, so the command is testable without
// a real terminal.

import (
	"fmt"
	"io"
	"strings"

	"github.com/really-knows-ai/kalide/internal/scaffold"
)

// initLibraryCreatedMessage is the success text for `kalide init-library`. It
// names the library's target path and lists the root entries InitLibrary
// creates; the empty directories are spelled with a trailing slash.
const initLibraryCreatedMessage = `Created a template library at %s:
  library.yaml
  AGENTS.md
  slides/
  sections/
  media/
  themes/default/theme.css
`

// runInitLibrary implements `kalide init-library <path>`. It returns the
// process status code: 0 when the library is written; 1 when scaffold refuses
// (a blocking path already exists), the target base name is not a valid library
// name, or the target exists as a non-directory; 2 for a missing or extra
// argument (usage to stderr, nothing written).
func runInitLibrary(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintf(stderr, "kalide init-library: expected exactly one library path\n\n")
		fmt.Fprint(stderr, usage)
		return 2
	}
	if strings.HasPrefix(args[0], "-") {
		// There are no init-library options. A token that looks like a flag
		// (the retired `--force`, say) is a usage error, not a path; no valid
		// library path begins with `-`.
		fmt.Fprintf(stderr, "kalide init-library: unexpected option %q (expected a library path)\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}

	// scaffold.InitLibrary writes nothing on refusal or invalid input and
	// returns a message that already names the command and the offending
	// path or blocking path(s), so it is printed verbatim rather than
	// re-prefixed.
	if err := scaffold.InitLibrary(args[0]); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintf(stdout, initLibraryCreatedMessage, args[0])
	return 0
}
