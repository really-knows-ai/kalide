package cli

// This file implements `eypres start` (phase-7 task 6):
// requirements.requirement.cli-start.
//
// `eypres start [--port N] [--no-open]` validates the whole deck FIRST, in the
// same fixed deterministic order internal/validate uses (`eypres start` must
// never serve a broken deck). A validation failure prints exactly the single
// first error, formatted by validate.Format — byte for byte the message the
// browser error page shows — and exits non-zero without binding a port.
//
// When the deck is valid the command starts the three collaborating halves of
// live reload:
//
//   - internal/server.Listen binds 127.0.0.1 only and serves the deck page, the
//     embedded assets and the /templates gallery; the actual bound port (8080,
//     a fallback in 8081..8099, or the explicit --port) is what gets printed;
//   - internal/watch.Watch reports debounced changes to slides/, assets/ and
//     eypres.yaml;
//   - internal/server.NewReloader ties the two together: it registers the SSE
//     endpoint, publishes the first page, and re-validates + re-renders on every
//     watch event, swapping in the deck or the full-page error.
//
// The link is printed with the actual port and, unless --no-open was passed,
// the default browser is opened for it. openURL is the injection seam tests
// replace: production uses the platform's own opener (`open` on macOS,
// `rundll32` on Windows, `xdg-open` elsewhere).
//
// The command blocks until it is asked to stop — os.Interrupt (Ctrl+C/SIGINT)
// or SIGTERM. On Windows both Ctrl+C and Ctrl+Break reach the process as
// os.Interrupt, so the same set covers the platform's two console interrupts.
// It then shuts down cleanly: the reloader releases its event streams, the
// server is gracefully shut down and its port released, the watcher is stopped
// and no child process is left behind. It exits 0.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/really-knows-ai/ey-present/internal/server"
	"github.com/really-knows-ai/ey-present/internal/template"
	"github.com/really-knows-ai/ey-present/internal/theme"
	"github.com/really-knows-ai/ey-present/internal/validate"
	"github.com/really-knows-ai/ey-present/internal/watch"
)

const (
	// startDebounce is how long the watcher waits for a burst of edits to go
	// quiet before it emits one change event. It is short enough that an edit
	// reloads the browser near-instantly and long enough that a single save
	// (which often produces several filesystem events) coalesces into one
	// reload.
	startDebounce = 200 * time.Millisecond

	// startShutdownTimeout bounds the graceful server shutdown, so a stuck
	// client connection cannot keep `eypres start` from releasing the port on
	// Ctrl+C forever.
	startShutdownTimeout = 5 * time.Second
)

// startOptions are the parsed `eypres start` flags.
type startOptions struct {
	// port is the explicit --port value, or 0 when none was given (the server
	// then tries its default range).
	port int

	// noOpen suppresses opening the browser, for headless use and the e2e
	// harness.
	noOpen bool
}

// openURL opens url in the user's default browser. It is a package variable so
// a test can replace it with a stub and observe the URL that would have been
// opened without launching a real browser; production installs systemOpenURL.
var openURL = systemOpenURL

// systemOpenURL launches the platform's default-browser opener for url. The
// command is Run to completion, not merely Started, so the opener process is
// reaped and `eypres start` leaves no child behind. The supported targets are
// macOS (`open`) and Windows (`rundll32`); anything else falls back to
// `xdg-open`.
func systemOpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Run()
}

// runStart implements `eypres start [--port N] [--no-open]`. It returns the
// process status code: 0 after a clean, signal-triggered shutdown; 1 when the
// deck does not validate, the registry cannot be built, a port cannot be bound
// or the server cannot start; 2 for malformed arguments.
func runStart(args []string, stdout, stderr io.Writer) int {
	opts, err := parseStartArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "eypres start: %v\n\n", err)
		fmt.Fprint(stderr, usage)
		return 2
	}

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "eypres start: %v\n", err)
		return 1
	}

	// Load the project's templates/ library and its themes registry before
	// anything else: a missing or invalid templates/ directory is reported
	// the same way a broken deck is, and never falls back to any compiled-in
	// content (no-built-in-fallback).
	deckFS := os.DirFS(root)
	library, err := template.LoadLibrary(deckFS, template.TemplatesDir)
	if err != nil {
		fmt.Fprintf(stderr, "eypres start: %v\n", err)
		return 1
	}

	themeReg, err := theme.LoadDir(deckFS, path.Join(template.TemplatesDir, template.ThemesDir))
	if err != nil {
		fmt.Fprintf(stderr, "eypres start: %v\n", err)
		return 1
	}

	registry, err := template.NewRegistryFromLibrary(library)
	if err != nil {
		fmt.Fprintf(stderr, "eypres start: %v\n", err)
		return 1
	}

	// Validate the WHOLE deck before anything is served. A broken deck is an
	// author error, not a crash: print the single first error, exactly as the
	// browser error page would render it, and exit without binding a port.
	if verr, invalid := validate.Validate(deckFS, registry, themeReg); invalid {
		fmt.Fprintln(stderr, validate.Format(verr))
		return 1
	}

	events, stopWatch, err := watch.Watch(root, startDebounce)
	if err != nil {
		fmt.Fprintf(stderr, "eypres start: %v\n", err)
		return 1
	}

	srv, err := server.Listen(server.Options{Port: opts.port, Root: root, Library: library, Themes: themeReg})
	if err != nil {
		_ = stopWatch()
		fmt.Fprintf(stderr, "eypres start: %v\n", err)
		return 1
	}

	reloader, err := server.NewReloader(server.ReloadOptions{
		Server: srv,
		Root:   root,
		Events: events,
		Log:    stderr,
	})
	if err != nil {
		_ = srv.Shutdown(context.Background())
		_ = stopWatch()
		fmt.Fprintf(stderr, "eypres start: %v\n", err)
		return 1
	}

	// Start the reloader before printing the URL, so the very first request a
	// browser makes already carries the live-reload client script and the
	// rendered deck.
	if err := reloader.Start(); err != nil {
		_ = srv.Shutdown(context.Background())
		_ = stopWatch()
		fmt.Fprintf(stderr, "eypres start: %v\n", err)
		return 1
	}

	url := srv.URL()
	fmt.Fprintf(stdout, "Serving slides at %s\n", url)
	fmt.Fprintln(stdout, "Press Ctrl+C to stop.")
	if !opts.noOpen {
		if err := openURL(url); err != nil {
			fmt.Fprintf(stderr, "eypres start: could not open a browser: %v\n", err)
		}
	}

	// Block until a shutdown signal arrives, or until the reloader's event loop
	// returns on its own (the watch channel closed). Ctrl+C/Ctrl+Break arrive as
	// os.Interrupt (and SIGBREAK on Windows); SIGTERM is the non-interactive
	// equivalent.
	ctx, stopSignals := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stopSignals()

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	runDone := make(chan error, 1)
	go func() { runDone <- reloader.Run(runCtx) }()

	select {
	case <-ctx.Done():
	case err := <-runDone:
		if err != nil {
			fmt.Fprintf(stderr, "eypres start: %v\n", err)
		}
	}

	// Clean shutdown. Stop the reloader first so its long-lived SSE streams end
	// and the graceful server shutdown is not held open by them; then release
	// the port; then stop the watcher.
	cancelRun()
	reloader.Stop()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), startShutdownTimeout)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx)
	_ = stopWatch()
	return 0
}

// parseStartArgs parses the arguments after the `start` command word:
// `[--port N] [--no-open]` in any order, with `--port=N` accepted as the
// single-token form. Anything else is a usage error.
func parseStartArgs(args []string) (startOptions, error) {
	var opts startOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-open":
			opts.noOpen = true
		case arg == "--port":
			if i+1 >= len(args) {
				return startOptions{}, errors.New("option --port needs a number")
			}
			i++
			port, err := parsePort(args[i])
			if err != nil {
				return startOptions{}, err
			}
			opts.port = port
		case strings.HasPrefix(arg, "--port="):
			port, err := parsePort(strings.TrimPrefix(arg, "--port="))
			if err != nil {
				return startOptions{}, err
			}
			opts.port = port
		default:
			return startOptions{}, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return opts, nil
}

// parsePort validates an explicit --port value. Port 0 is not accepted: the
// documented way to ask eypres to choose a free port is to omit --port.
func parsePort(s string) (int, error) {
	port, err := strconv.Atoi(s)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q: choose a port between 1 and 65535", s)
	}
	return port, nil
}

// shutdownSignals returns the signals that trigger a clean shutdown:
// os.Interrupt (Ctrl+C, SIGINT) and SIGTERM on every platform. On Windows both
// Ctrl+C and Ctrl+Break are delivered as os.Interrupt, so no extra signal is
// needed there.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
