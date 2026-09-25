package cli

import (
	"bytes"
	"strings"
	"testing"
)

// runCLI invokes Run with a program name prepended, exactly as cmd/eypres does
// with os.Args, and returns the status code plus captured output.
func runCLI(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"eypres"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunNoArgsPrintsUsage(t *testing.T) {
	code, stdout, stderr := runCLI()
	if code != 0 {
		t.Fatalf("Run() exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Fatalf("Run() stdout = %q, want usage text", stdout)
	}
	if stderr != "" {
		t.Fatalf("Run() stderr = %q, want empty", stderr)
	}
}

func TestRunHelpPrintsUsage(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			code, stdout, stderr := runCLI(arg)
			if code != 0 {
				t.Fatalf("Run(%q) exit = %d, want 0", arg, code)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Fatalf("Run(%q) stdout = %q, want usage text", arg, stdout)
			}
			if stderr != "" {
				t.Fatalf("Run(%q) stderr = %q, want empty", arg, stderr)
			}
		})
	}
}

// TestRunRoutesCommands asserts each recognised command reaches its handler.
// The phase-1 handlers are stubs that report "not implemented" with exit 1, so
// routing is observable through the handler name in the error message. The
// templates list and templates show forms both reach the templates handler.
func TestRunRoutesCommands(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		handler string
	}{
		{name: "init", args: []string{"init"}, handler: "init"},
		{name: "start", args: []string{"start"}, handler: "start"},
		{name: "templates list", args: []string{"templates"}, handler: "templates"},
		{name: "templates show", args: []string{"templates", "title"}, handler: "templates"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(tt.args...)
			if code != 1 {
				t.Fatalf("Run(%q) exit = %d, want 1", tt.args, code)
			}
			if !strings.Contains(stderr, `"`+tt.handler+`"`) {
				t.Fatalf("Run(%q) stderr = %q, want mention of handler %q", tt.args, stderr, tt.handler)
			}
			if !strings.Contains(stderr, "not implemented") {
				t.Fatalf("Run(%q) stderr = %q, want a not-implemented error", tt.args, stderr)
			}
			if stdout != "" {
				t.Fatalf("Run(%q) stdout = %q, want empty", tt.args, stdout)
			}
		})
	}
}

func TestRunTemplatesRejectsExtraArgs(t *testing.T) {
	code, stdout, stderr := runCLI("templates", "a", "b")
	if code != 2 {
		t.Fatalf("Run(templates a b) exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "at most one") {
		t.Fatalf("Run(templates a b) stderr = %q, want an argument-count error", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("Run(templates a b) stderr = %q, want usage text", stderr)
	}
	if stdout != "" {
		t.Fatalf("Run(templates a b) stdout = %q, want empty", stdout)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	for _, args := range [][]string{{"frobnicate"}, {"frobnicate", "extra"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := runCLI(args...)
			if code == 0 {
				t.Fatalf("Run(%q) exit = 0, want non-zero", args)
			}
			if code != 2 {
				t.Fatalf("Run(%q) exit = %d, want 2", args, code)
			}
			if !strings.Contains(stderr, "unknown command") {
				t.Fatalf("Run(%q) stderr = %q, want an unknown-command error", args, stderr)
			}
			if !strings.Contains(stderr, "frobnicate") {
				t.Fatalf("Run(%q) stderr = %q, want the offending command named", args, stderr)
			}
			if !strings.Contains(stderr, "Usage:") {
				t.Fatalf("Run(%q) stderr = %q, want usage text", args, stderr)
			}
			if stdout != "" {
				t.Fatalf("Run(%q) stdout = %q, want empty", args, stdout)
			}
		})
	}
}
