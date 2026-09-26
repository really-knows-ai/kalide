//go:build windows

package e2e

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// This file is the Windows half of the harness's process-group handling. It
// runs on the supported windows/amd64 (and windows/arm64) targets. On Windows
// there is no Setpgid; the equivalent is to create the child with
// CREATE_NEW_PROCESS_GROUP and then send CTRL_BREAK_EVENT to that group, which
// kalide receives as os.Interrupt exactly like Ctrl+Break at the console.
//
// The Win32 entry points are called through the standard library's syscall
// package rather than golang.org/x/sys/windows, so the harness adds no new
// module dependency (go.mod is not owned by this package).
const (
	// createNewProcessGroup is Win32 CREATE_NEW_PROCESS_GROUP (0x00000200): the
	// new process becomes the root of a new console process group, so a console
	// control event can be addressed to it and its descendants.
	createNewProcessGroup = 0x00000200

	// ctrlBreakEvent is Win32 CTRL_BREAK_EVENT (1): a Ctrl+Break console event,
	// which Go delivers to a process as os.Interrupt.
	ctrlBreakEvent = 1

	// th32csSnapProcess is Win32 TH32CS_SNAPPROCESS (0x00000002): snapshot
	// every process in the system.
	th32csSnapProcess = 0x00000002

	// invalidHandleValue is Win32 INVALID_HANDLE_VALUE, as a syscall.Handle.
	invalidHandleValue = syscall.Handle(^uintptr(0))
)

// kernel32 is the module the harness's two non-stdlib Win32 calls live in.
var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// procGenerateConsoleCtrlEvent is Win32 GenerateConsoleCtrlEvent.
var procGenerateConsoleCtrlEvent = kernel32.NewProc("GenerateConsoleCtrlEvent")

// setProcessGroup makes the child the root of a new console process group
// (CREATE_NEW_PROCESS_GROUP), the Windows counterpart of Setpgid.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// signalGroupGracefully sends CTRL_BREAK_EVENT to the harness's process group.
// kalide registers os.Interrupt, which on Windows covers both Ctrl+C and
// Ctrl+Break, so it shuts its server, watcher and reloader down cleanly and
// exits 0.
//
// GenerateConsoleCtrlEvent requires the calling process to share the target's
// console; when the harness runs with no console (a detached CI agent) the call
// fails, and the timeout fallback in Stop then fails the test rather than
// silently pretending the stop was graceful.
func signalGroupGracefully(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	r1, _, err := procGenerateConsoleCtrlEvent.Call(
		uintptr(ctrlBreakEvent),
		uintptr(uint32(cmd.Process.Pid)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

// killGroup forcibly terminates the process group. Go has no group-kill on
// Windows, so this kills the group leader; the OS tears down the rest of the
// group when its root exits. It is the forced-kill timeout fallback only;
// reaching it fails the test.
func killGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// childProcessCount reports how many processes have the harness's kalide
// process as their parent, by walking a system process snapshot. It is how the
// harness asserts that a graceful stop left no child process behind.
func childProcessCount(pid int) (int, error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(th32csSnapProcess, 0)
	if err != nil {
		return 0, err
	}
	if snapshot == invalidHandleValue {
		return 0, syscall.EINVAL
	}
	defer syscall.CloseHandle(snapshot)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return 0, err
	}

	count := 0
	for {
		if int(entry.ParentProcessID) == pid {
			count++
		}
		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			// ERROR_NO_MORE_FILES ends the enumeration.
			break
		}
	}
	return count, nil
}
