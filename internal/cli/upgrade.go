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
//
// runUpgrade is the skeleton created in phase-04 task-4; phase-04 task-5
// implements its argument validation, the scaffold.Upgrade call and the report
// rendering.

import (
	"fmt"
	"io"
)

// runUpgrade implements `kalide upgrade`. It returns the process status code:
// 0 when the project is refreshed and validated (even when author-edited files
// are skipped); non-zero when the current directory is not a project, the
// library format is unsupported, or the upgraded project fails end-to-end
// validation; 2 for any argument, which is a usage error.
//
// The implementation is completed by phase-04 task-5; this skeleton keeps the
// package compiling between tasks.
func runUpgrade(args []string, stdout, stderr io.Writer) int {
	_ = args
	_ = stdout
	fmt.Fprintln(stderr, "kalide upgrade: not implemented yet")
	return 1
}
