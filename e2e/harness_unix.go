//go:build !windows

package e2e

import (
	"errors"
	"os/exec"
	"syscall"
)

// This file is the Unix half of the harness's process-group handling. It is
// selected for every non-Windows target, so on the supported macOS target it
// starts `eypres start` in its own process group and stops it gracefully with
// SIGTERM to that group. The Windows counterpart is harness_windows.go.

// setProcessGroup makes the child the leader of a new process group
// (Setpgid on macOS and other Unix targets). The group id equals the child's
// pid, which is how every later graceful-stop call addresses the whole tree.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroupGracefully sends SIGTERM to the harness's process group. eypres
// listens for os.Interrupt/SIGTERM and shuts its server, watcher and reloader
// down cleanly, exiting 0. A process group that is already gone (ESRCH) is not
// an error: the process may have stopped between Wait observing its exit and
// this call.
func signalGroupGracefully(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// killGroup signals the whole process group with SIGKILL. It is the forced-kill
// timeout fallback only; reaching it fails the test.
func killGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// childProcessCount reports how many processes remain in the harness's process
// group after a graceful stop. Because setProcessGroup makes the child a group
// leader whose group id is its pid, a process group can still be signalled
// exactly when a leftover process remains; an empty group answers ESRCH.
//
// The count is deliberately coarse (the group is alive, so at least one process
// remains) because the post-condition under test is "no leftover child
// processes", not their exact number.
func childProcessCount(pid int) (int, error) {
	err := syscall.Kill(-pid, 0)
	switch {
	case err == nil:
		return 1, nil
	case errors.Is(err, syscall.ESRCH):
		return 0, nil
	case errors.Is(err, syscall.EPERM):
		// A process exists but cannot be signalled by this user; it is still
		// a leftover, so report it.
		return 1, nil
	default:
		return 0, err
	}
}
