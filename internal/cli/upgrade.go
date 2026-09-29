package cli

// This file implements `kalide upgrade` (phase-04 tasks 4 and 5):
// requirements.requirement.deck-library-upgrade, global.constraint.upgrade-offline
// and global.constraint.upgrade-never-clobbers.
//
// `kalide upgrade` brings the project in the current working directory forward
// to the running binary's scaffold content and library format. Unlike
// `kalide init` and `kalide init-library`, it takes NO path argument and has no
// --force: the only accepted invocation is a bare `kalide upgrade`, and it
// always works on the current directory. The filesystem work lives in
// internal/scaffold.Upgrade; this handler validates the argument list and turns
// scaffold's outcome into the process exit code and user-facing text:
//
//   - success prints the UpgradeReport — every refreshed path and every
//     left-alone/skipped path — and exits 0, including when author-edited files
//     were deliberately skipped rather than overwritten;
//   - a directory holding both manifests or neither, an unsupported library
//     format, or a failed end-to-end validation prints scaffold's message to
//     stderr and exits non-zero, having left the project unchanged;
//   - any argument at all — a path or a --force-style flag — is a usage error:
//     usage to stderr and exit 2, nothing written.
//
// All output goes to the supplied writers, so the command is testable without
// a real terminal.

import (
	"fmt"
	"io"
	"os"

	"github.com/really-knows-ai/kalide/internal/scaffold"
)

// runUpgrade implements `kalide upgrade`. It returns the process status code:
// 0 when the project is refreshed and validated (even when author-edited files
// are skipped); non-zero when the current directory is not a project, the
// library format is unsupported, or the upgraded project fails end-to-end
// validation; 2 for any argument, which is a usage error.
//
// There is exactly one accepted invocation — a bare `kalide upgrade`. Unlike
// `kalide init` and `kalide init-library`, it takes no path and has no --force:
// any argument at all, whether a path or a --force-style flag, is a usage error
// (usage to stderr, exit 2, nothing written). The command always works on the
// current working directory, which it resolves with os.Getwd.
//
// On success it prints scaffold.Upgrade's UpgradeReport to stdout — every
// refreshed path and every left-alone/skipped path, including an author-edited
// file that was deliberately skipped — and exits 0. scaffold.Upgrade writes
// nothing on refusal and returns a message that already names the condition (a
// both/neither project, an unsupported library format naming the format found
// and supported, or a failed end-to-end validation), so it is printed verbatim
// to stderr and exits 1 rather than being re-prefixed.
func runUpgrade(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		// There are no upgrade options and no path argument. Any token — a
		// path like `../deck` or a flag like `--force` — is a usage error,
		// not a project to upgrade.
		fmt.Fprintf(stderr, "kalide upgrade: unexpected argument %q (kalide upgrade takes no arguments and no --force)\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "kalide upgrade: %v\n", err)
		return 1
	}

	// scaffold.Upgrade is the single owner of project detection, the refresh
	// and migration halves, and the end-to-end validation; it writes nothing
	// and leaves the project unchanged when it returns an error, which is
	// printed verbatim rather than re-prefixed.
	report, err := scaffold.Upgrade(cwd)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	// String renders the detected project form, the refreshed paths and the
	// left-alone/skipped paths; a trailing newline terminates the last line.
	fmt.Fprintf(stdout, "%s\n", report)
	return 0
}
