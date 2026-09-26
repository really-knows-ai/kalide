// Package e2e is the cross-platform end-to-end harness for the real eypres
// binary.
//
// Harness builds cmd/eypres with CGO_ENABLED=0 into a temporary directory (the
// eypres.exe name on Windows), runs it from a clean temporary working directory
// with a scrubbed environment, and starts `eypres start --no-open` in its own
// process group so exactly the tree it started can be stopped again.
//
// Process groups and graceful stop are platform-specific and live in
// build-tagged files (harness_unix.go, harness_windows.go):
//
//   - Unix (macOS): the child is placed in a new process group with Setpgid and
//     SIGTERM is sent to that group;
//   - Windows: the child is created with CREATE_NEW_PROCESS_GROUP and
//     CTRL_BREAK_EVENT is sent to that group.
//
// After the graceful signal, Stop asserts that the process exited 0, that the
// listening port was released and that no child process was left behind. A
// forced kill is only a timeout fallback, and a forced kill always fails the
// test.
//
// By default build compiles cmd/eypres from source. When EYPRES_BINARY is set
// the harness skips the build and runs that prebuilt binary instead, which is
// how native CI feeds the harness an already-built, per-target executable:
// EYPRES_BINARY=/path/to/eypres.exe go test ./e2e
//
// The harness is reused by the phase-8 (init + start) and phase-9 (native
// release, offline) end-to-end tests, so it lives in non-test files and has no
// dependency on the testing package: it talks to a tiny T interface that
// *testing.T satisfies. Offline enforcement builds on the harness in
// offline.go: assertOffline plus Harness.EnableOfflineProxy.
package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// buildTimeout bounds the `go build` that produces the binary.
	buildTimeout = 5 * time.Minute

	// startTimeout bounds how long Start waits for the printed URL.
	startTimeout = 30 * time.Second

	// readyTimeout bounds the HTTP-200 readiness poll once the URL is known.
	readyTimeout = 30 * time.Second

	// stopTimeout bounds the graceful stop before the harness falls back to a
	// forced kill, which fails the test.
	stopTimeout = 15 * time.Second

	// portTimeout bounds the wait for the listening port to be released.
	portTimeout = 5 * time.Second

	// pollInterval is the pause between readiness and port-release probes.
	pollInterval = 25 * time.Millisecond

	// servingPrefix is the exact prefix of the line internal/cli prints once a
	// deck is being served, for example
	// "Serving slides at http://127.0.0.1:8080/". Start parses the URL from it.
	servingPrefix = "Serving slides at "

	// prebuiltBinaryEnv names the environment variable that, when set, points
	// the harness at an already-built eypres binary instead of compiling one.
	// Native CI sets it to the per-target executable it produced.
	prebuiltBinaryEnv = "EYPRES_BINARY"
)

// T is the subset of testing.TB the harness uses. *testing.T and *testing.B
// satisfy it, and so does a test double, which keeps this package free of a
// testing dependency.
type T interface {
	Helper()
	Cleanup(func())
	TempDir() string
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Harness builds and drives the eypres binary. Create one with NewHarness.
//
// It holds the built binary and a clean temporary working directory for the
// deck, and, after Start, the running `eypres start --no-open` process.
type Harness struct {
	t T

	// moduleRoot is the Go module root, from which `go build ./cmd/eypres` runs.
	moduleRoot string

	// binDir holds the built binary and is removed with the test's temp dirs.
	binDir string

	// binPath is the built eypres binary.
	binPath string

	// workDir is the clean temporary working directory the binary runs in.
	workDir string

	// client serves the HTTP fetch helpers. It never uses a proxy so a
	// developer's environment cannot intercept loopback requests.
	client *http.Client

	// extraEnv holds additional key=value entries Command appends to the
	// scrubbed process environment, overriding it. It is set by
	// EnableOfflineProxy and must be configured before Start, because the
	// environment is fixed when the command is executed.
	extraEnv []string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdout  *capture
	stderr  *capture
	exited  chan struct{}
	exitErr error
	url     string
	baseURL string
	port    int

	stopOnce sync.Once
}

// NewHarness builds the eypres binary with CGO_ENABLED=0 into a temporary
// directory and returns a harness whose working directory is a second, empty
// temporary directory. It fails the test if the module root or the binary
// cannot be found.
func NewHarness(t T) *Harness {
	t.Helper()

	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("e2e: %v", err)
	}

	h := &Harness{
		t:          t,
		moduleRoot: root,
		binDir:     t.TempDir(),
		workDir:    t.TempDir(),
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{Proxy: nil},
		},
	}
	h.build()
	return h
}

// build compiles cmd/eypres into the harness's binary directory, CGO-free, so
// the binary is the same static artifact the release and CI e2e runs exercise.
//
// When EYPRES_BINARY is set the build is skipped and that binary is used
// instead: native CI builds the per-target eypres once and points every test at
// it. The path must exist; a missing or non-file path fails the test rather than
// silently building from source.
func (h *Harness) build() {
	h.t.Helper()

	if prebuilt := os.Getenv(prebuiltBinaryEnv); prebuilt != "" {
		info, err := os.Stat(prebuilt)
		if err != nil {
			h.t.Fatalf("e2e: %s=%s: %v", prebuiltBinaryEnv, prebuilt, err)
		}
		if info.IsDir() {
			h.t.Fatalf("e2e: %s=%s is a directory, want the eypres binary", prebuiltBinaryEnv, prebuilt)
		}
		h.binPath = prebuilt
		return
	}

	h.binPath = filepath.Join(h.binDir, binaryName())

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, goCommand(), "build", "-o", h.binPath, "./cmd/eypres")
	cmd.Dir = h.moduleRoot
	cmd.Env = envWith(os.Environ(), "CGO_ENABLED", "0")

	out, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("e2e: build eypres (CGO_ENABLED=0): %v\n%s", err, out)
	}
}

// Command returns an exec.Cmd for `eypres args...` with the harness's clean
// working directory and scrubbed environment. Entries added by
// EnableOfflineProxy (if any) are appended, overriding the scrubbed
// environment. It does not start the command.
func (h *Harness) Command(args ...string) *exec.Cmd {
	cmd := exec.Command(h.binPath, args...)
	cmd.Dir = h.workDir
	h.mu.Lock()
	extra := append([]string(nil), h.extraEnv...)
	h.mu.Unlock()
	cmd.Env = append(scrubbedEnv(), extra...)
	return cmd
}

// Run runs `eypres args...` to completion in the clean working directory and
// returns its stdout, stderr and exit code. It is for one-shot commands such as
// `init` and `templates`; `start` does not exit and must go through Start.
func (h *Harness) Run(args ...string) (stdout, stderr string, code int) {
	h.t.Helper()

	cmd := h.Command(args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	if err == nil {
		return out.String(), errOut.String(), 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), errOut.String(), ee.ExitCode()
	}
	h.t.Fatalf("e2e: run eypres %v: %v", args, err)
	return "", "", -1
}

// Start launches `eypres start --no-open` with any extra arguments (for
// example "--port", "8123"), in its own process group, parses the URL it prints
// and polls until the server answers HTTP 200. It fails the test if the binary
// exits early or the server never becomes ready.
//
// Stop is registered as a test cleanup, so a test that forgets to call it still
// gets a graceful shutdown and a leaked process is reported.
func (h *Harness) Start(extra ...string) {
	h.t.Helper()

	h.mu.Lock()
	if h.cmd != nil {
		h.mu.Unlock()
		h.t.Fatalf("e2e: Start called twice on one Harness")
	}
	h.mu.Unlock()

	args := append([]string{"start", "--no-open"}, extra...)
	cmd := h.Command(args...)
	setProcessGroup(cmd)

	outCap := newCapture()
	errCap := newCapture()
	cmd.Stdout = outCap
	cmd.Stderr = errCap

	if err := cmd.Start(); err != nil {
		h.t.Fatalf("e2e: start eypres: %v", err)
	}

	exited := make(chan struct{})
	h.mu.Lock()
	h.cmd = cmd
	h.stdout = outCap
	h.stderr = errCap
	h.exited = exited
	h.mu.Unlock()

	go func() {
		err := cmd.Wait()
		h.mu.Lock()
		h.exitErr = err
		h.mu.Unlock()
		close(exited)
	}()

	h.t.Cleanup(h.Stop)

	raw := h.waitForURL()

	h.mu.Lock()
	h.url = raw
	h.baseURL = strings.TrimSuffix(raw, "/")
	h.port = urlPort(raw)
	h.mu.Unlock()

	if err := h.waitReady(); err != nil {
		h.t.Fatalf("e2e: eypres did not serve %s: %v\nstdout:\n%s\nstderr:\n%s",
			raw, err, outCap.String(), errCap.String())
	}
}

// waitForURL blocks until `eypres start` prints its "Serving slides at <url>"
// line, the process exits, or startTimeout elapses; the latter two fail the
// test with the captured output.
func (h *Harness) waitForURL() string {
	h.t.Helper()

	urlCh := h.stdoutCapture().urlCh
	exited := h.exitChannel()

	timer := time.NewTimer(startTimeout)
	defer timer.Stop()

	for {
		select {
		case u := <-urlCh:
			return u
		case <-exited:
			if u := h.stdoutCapture().URL(); u != "" {
				return u
			}
			h.t.Fatalf("e2e: eypres start exited before serving a deck\nstdout:\n%s\nstderr:\n%s",
				h.Stdout(), h.Stderr())
		case <-timer.C:
			h.t.Fatalf("e2e: timed out after %s waiting for eypres to print its URL\nstdout:\n%s\nstderr:\n%s",
				startTimeout, h.Stdout(), h.Stderr())
		}
	}
}

// waitReady polls the served root until it answers HTTP 200, the process exits,
// or readyTimeout elapses.
func (h *Harness) waitReady() error {
	h.t.Helper()

	exited := h.exitChannel()
	deadline := time.Now().Add(readyTimeout)

	var last error
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return fmt.Errorf("eypres exited early: %v", h.waitErr())
		default:
		}

		resp, err := h.client.Get(h.BaseURL() + "/")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("GET /: %s", resp.Status)
		} else {
			last = err
		}
		time.Sleep(pollInterval)
	}
	return last
}

// Stop gracefully stops the server and asserts a clean shutdown. On Unix it
// sends SIGTERM to the harness's process group; on Windows it sends
// CTRL_BREAK_EVENT to the group. It then asserts that eypres exited 0, that the
// listening port was released and that no child process was left behind.
//
// Stop is idempotent and is registered as a test cleanup by Start. If the
// process does not stop before stopTimeout it is forcibly killed and the test
// fails.
func (h *Harness) Stop() {
	h.t.Helper()

	first := false
	h.stopOnce.Do(func() { first = true })
	if !first {
		return
	}

	h.mu.Lock()
	cmd := h.cmd
	exited := h.exited
	h.mu.Unlock()
	if cmd == nil {
		return
	}

	select {
	case <-exited:
		// Already gone; still verify the exit status and any leftovers.
	default:
		if err := signalGroupGracefully(cmd); err != nil {
			h.t.Errorf("e2e: graceful stop: %v", err)
		}
		timer := time.NewTimer(stopTimeout)
		select {
		case <-exited:
			timer.Stop()
		case <-timer.C:
			h.t.Errorf("e2e: eypres did not stop within %s; forced kill (the test FAILS)", stopTimeout)
			h.forceKill(cmd)
			<-exited
			return
		}
	}

	h.checkShutdown(cmd)
}

// checkShutdown asserts the post-conditions of a graceful stop: exit code 0,
// the listening port released and no leftover child processes.
func (h *Harness) checkShutdown(cmd *exec.Cmd) {
	h.t.Helper()

	code := 0
	if err := h.waitErr(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			h.t.Errorf("e2e: waiting for eypres: %v", err)
			return
		}
	}
	if code != 0 {
		h.t.Errorf("e2e: eypres exited with code %d, want 0\nstdout:\n%s\nstderr:\n%s",
			code, h.Stdout(), h.Stderr())
	}

	if port := h.Port(); port != 0 && !waitPortReleased(port, portTimeout) {
		h.t.Errorf("e2e: port %d is still listening after eypres stopped", port)
	}

	if n, err := childProcessCount(cmd.Process.Pid); err != nil {
		h.t.Errorf("e2e: checking for leftover child processes: %v", err)
	} else if n > 0 {
		h.t.Errorf("e2e: %d leftover child process(es) after eypres stopped", n)
	}
}

// forceKill is the timeout fallback: it kills the whole process group, then the
// leader directly in case the group has already dissolved. Callers that reach
// it fail the test.
func (h *Harness) forceKill(cmd *exec.Cmd) {
	h.t.Helper()
	if err := killGroup(cmd); err != nil {
		_ = cmd.Process.Kill()
		return
	}
	_ = cmd.Process.Kill()
}

// Get performs a GET request against the running server at path (for example
// "/" or "/assets/theme.css"). The caller closes the response body.
func (h *Harness) Get(path string) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return h.client.Get(h.BaseURL() + path)
}

// GetOK GETs path and returns its body when the status is 200, or an error
// naming the status otherwise.
func (h *Harness) GetOK(path string) ([]byte, error) {
	resp, err := h.Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("GET %s: status %s", path, resp.Status)
	}
	return body, nil
}

// GetString is GetOK as a string.
func (h *Harness) GetString(path string) (string, error) {
	body, err := h.GetOK(path)
	if err != nil {
		return string(body), err
	}
	return string(body), nil
}

// URL returns the URL eypres printed, for example
// "http://127.0.0.1:8080/", or "" before Start.
func (h *Harness) URL() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.url
}

// BaseURL returns URL without its trailing slash, for building request paths.
func (h *Harness) BaseURL() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.baseURL
}

// Port returns the port the server bound, or 0 before Start.
func (h *Harness) Port() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.port
}

// PID returns the eypres process id, or 0 before Start.
func (h *Harness) PID() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// BinaryPath returns the path of the built eypres binary.
func (h *Harness) BinaryPath() string { return h.binPath }

// WorkDir returns the clean temporary working directory the binary runs in.
func (h *Harness) WorkDir() string { return h.workDir }

// Path resolves rel inside the harness's working directory.
func (h *Harness) Path(rel string) string {
	return filepath.Join(h.workDir, filepath.FromSlash(rel))
}

// WriteFile writes data to rel inside the harness's working directory, creating
// parent directories, and returns the absolute path. It fails the test on
// error. It is how a test lays down a fixture deck.
func (h *Harness) WriteFile(rel string, data []byte) string {
	h.t.Helper()

	p := h.Path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatalf("e2e: create directory for %s: %v", rel, err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		h.t.Fatalf("e2e: write %s: %v", rel, err)
	}
	return p
}

// Stdout returns the eypres process's stdout so far.
func (h *Harness) Stdout() string {
	h.mu.Lock()
	c := h.stdout
	h.mu.Unlock()
	if c == nil {
		return ""
	}
	return c.String()
}

// Stderr returns the eypres process's stderr so far.
func (h *Harness) Stderr() string {
	h.mu.Lock()
	c := h.stderr
	h.mu.Unlock()
	if c == nil {
		return ""
	}
	return c.String()
}

func (h *Harness) stdoutCapture() *capture {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stdout
}

func (h *Harness) exitChannel() chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.exited
}

func (h *Harness) waitErr() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.exitErr
}

// capture is an io.Writer that records everything a process writes and
// recognises the URL line as soon as it is complete.
type capture struct {
	mu    sync.Mutex
	b     bytes.Buffer
	url   string
	urlCh chan string
}

func newCapture() *capture {
	return &capture{urlCh: make(chan string, 1)}
}

// Write records p and, when the "Serving slides at <url>" line has been fully
// written, publishes the URL exactly once on urlCh.
func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.b.Write(p)
	found := ""
	if c.url == "" {
		if u := findServingURL(c.b.String()); u != "" {
			c.url = u
			found = u
		}
	}
	c.mu.Unlock()

	if found != "" {
		select {
		case c.urlCh <- found:
		default:
		}
	}
	return len(p), nil
}

func (c *capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}

func (c *capture) URL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.url
}

// findServingURL returns the URL of the first complete "Serving slides at …"
// line in s, or "" when there is none yet. A line without a terminator is
// ignored so a URL split across pipe writes is never parsed half-formed.
func findServingURL(s string) string {
	i := strings.Index(s, servingPrefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(servingPrefix):]
	j := strings.IndexByte(rest, '\n')
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// urlPort returns the port of a parsed URL, or 0 when it has none.
func urlPort(raw string) int {
	u, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return 0
	}
	return port
}

// waitPortReleased reports whether a loopback listener can bind port again
// within timeout, i.e. whether eypres released it. It binds and immediately
// closes on success.
func waitPortReleased(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			_ = ln.Close()
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}

// binaryName is the built binary's file name for the target this harness is
// compiled for: eypres.exe on Windows, eypres elsewhere.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "eypres.exe"
	}
	return "eypres"
}

// goCommand is the go binary used to build eypres. The GO environment variable
// overrides the one on PATH, matching the Makefile.
func goCommand() string {
	if g := os.Getenv("GO"); g != "" {
		return g
	}
	return "go"
}

// findModuleRoot walks up from the current directory until it finds the go.mod
// that owns the e2e package. `go test` runs a package's tests with the package
// directory as the working directory, so this finds the module root reliably.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find module root: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("find module root: no go.mod in any parent directory")
		}
		dir = parent
	}
}

// envWith returns environ with key set to value, replacing any existing entry
// (case-insensitively, so it also works on Windows).
func envWith(environ []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if strings.HasPrefix(strings.ToUpper(kv), prefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, key+"="+value)
}

// scrubAllow is the allow-list of environment variables the eypres process
// receives. Everything else — browser selectors, proxy settings and the rest of
// the developer's shell — is dropped so a run is reproducible and offline by
// default.
var scrubAllow = []string{
	"PATH",
	"HOME",
	"USER",
	"USERNAME",
	"USERPROFILE",
	"TMPDIR",
	"TMP",
	"TEMP",
	"LANG",
	"LC_ALL",
	"SystemRoot",
	"windir",
	"COMSPEC",
	"PATHEXT",
	"ProgramData",
	"ProgramFiles",
	"APPDATA",
	"LOCALAPPDATA",
	"OS",
}

// scrubbedEnv returns the minimal environment the eypres binary runs with.
func scrubbedEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		for _, allowed := range scrubAllow {
			if strings.EqualFold(kv[:i], allowed) {
				out = append(out, kv)
				break
			}
		}
	}
	return out
}
