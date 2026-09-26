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
//
// `init` is still the phase-1 stub, so its routing is observable through the
// handler name in the "not implemented" error (exit 1). `start` now routes to
// the real runStart handler (phase-7 task 6): in a directory with no
// eypres.yaml it validates the deck first and prints exactly the single first
// error (naming eypres.yaml) before it can serve, exiting non-zero. `templates`
// is also a real handler (phase-7 task 7): it reaches runTemplates, which lists
// the built-ins (exit 0), documents one by name (exit 0) and rejects an unknown
// name (non-zero). The full start/templates assertions belong to phase-7 task 9;
// here we only pin down that the routes reach the real handlers.
func TestRunRoutesCommands(t *testing.T) {
	stubs := []struct {
		name    string
		args    []string
		handler string
	}{
		{name: "init", args: []string{"init"}, handler: "init"},
	}

	for _, tt := range stubs {
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

	t.Run("start validates before serving", func(t *testing.T) {
		// An empty temp dir has no eypres.yaml, so runStart's whole-deck
		// validation fails deterministically before any port is bound. This
		// pins the route (not the stub) and the phase-5 single-error format;
		// the full start behaviour is phase-7 task 9.
		t.Chdir(t.TempDir())

		code, stdout, stderr := runCLI("start")
		if code == 0 {
			t.Fatal("Run(start) exit = 0, want non-zero for a deck with no eypres.yaml")
		}
		if strings.Contains(stderr, "not implemented") {
			t.Fatalf("Run(start) stderr = %q, want the real runStart handler, not the phase-1 stub", stderr)
		}
		if !strings.Contains(stderr, "eypres.yaml") {
			t.Fatalf("Run(start) stderr = %q, want the first validation error naming eypres.yaml", stderr)
		}
		if strings.Contains(stdout, "Serving slides at") {
			t.Fatalf("Run(start) stdout = %q, want no server for an invalid deck", stdout)
		}
	})

	t.Run("templates list", func(t *testing.T) {
		code, stdout, stderr := runCLI("templates")
		if code != 0 {
			t.Fatalf("Run(templates) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stdout == "" {
			t.Fatal("Run(templates) stdout = \"\", want the built-in template list")
		}
		if stderr != "" {
			t.Fatalf("Run(templates) stderr = %q, want empty", stderr)
		}
	})

	t.Run("templates show", func(t *testing.T) {
		code, stdout, stderr := runCLI("templates", "title")
		if code != 0 {
			t.Fatalf("Run(templates title) exit = %d, want 0 (stderr = %q)", code, stderr)
		}
		if stdout == "" {
			t.Fatal("Run(templates title) stdout = \"\", want the title template documented")
		}
		if stderr != "" {
			t.Fatalf("Run(templates title) stderr = %q, want empty", stderr)
		}
	})

	t.Run("templates unknown", func(t *testing.T) {
		code, stdout, stderr := runCLI("templates", "no-such-template")
		if code == 0 {
			t.Fatal("Run(templates no-such-template) exit = 0, want non-zero")
		}
		if !strings.Contains(stderr, "unknown template") {
			t.Fatalf("Run(templates no-such-template) stderr = %q, want an unknown-template error", stderr)
		}
		if stdout != "" {
			t.Fatalf("Run(templates no-such-template) stdout = %q, want empty", stdout)
		}
	})
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
