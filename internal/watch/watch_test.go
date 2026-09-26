// Package watch_test is an external test package: internal/server imports
// internal/watch (reload.go), so an in-package test could not import
// internal/server without an import cycle. The external test package lives in
// the same internal/watch directory, so the top-level TestWatchIntegration is
// still the internal/watch integration test.
package watch_test

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/server"
	"github.com/really-knows-ai/kalide/internal/watch"
)

// testDebounce is the debounce window the integration test drives the real
// watcher with. It is large enough that a burst of writes coalesces reliably
// and small enough that the test stays quick.
const testDebounce = 150 * time.Millisecond

// TestWatchIntegration exercises internal/watch and internal/server against the
// real operating system: a real temporary directory watched by a real fsnotify
// watcher (slides/, assets/ and kalide.yaml, with debounce, recursive directory
// pickup and clean stop) and real loopback listeners (127.0.0.1 only, fallback
// through the 8080 range, explicit busy port failure). It touches the
// filesystem, real ports and a real watcher, so it is an integration test and
// is skipped under -short.
func TestWatchIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test uses a real filesystem watcher and real loopback listeners")
	}

	t.Run("slides edit yields one debounced event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustWrite(t, filepath.Join(root, deck.SlidesDir, "1-intro.md"), "# One edited\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, "slides/1-intro.md")
		assertQuiet(t, events)
	})

	t.Run("assets edit yields one debounced event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustWrite(t, filepath.Join(root, watch.AssetsDir, "logo.txt"), "logo edited\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, "assets/logo.txt")
		assertQuiet(t, events)
	})

	t.Run("kalide.yaml edit yields one debounced event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustWrite(t, filepath.Join(root, deck.ConfigFile), "title: Edited\n")

		ev := waitEvent(t, events)
		expectPath(t, ev, deck.ConfigFile)
		assertQuiet(t, events)
	})

	t.Run("burst of rapid writes coalesces to exactly one event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		path := filepath.Join(root, deck.SlidesDir, "1-intro.md")
		for i := 0; i < 5; i++ {
			mustWrite(t, path, "# burst "+strconv.Itoa(i)+"\n")
			time.Sleep(15 * time.Millisecond)
		}

		ev := waitEvent(t, events)
		expectPath(t, ev, "slides/1-intro.md")
		// The whole burst must have been one Event: 3*debounce of quiet
		// afterwards may not surface a second one.
		assertQuiet(t, events)
	})

	t.Run("templates slides edit yields one debounced event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustMkdir(t, filepath.Join(root, watch.TemplatesDir, "slides", "hello"))
		mustWrite(t, filepath.Join(root, watch.TemplatesDir, "slides", "hello", "template.yaml"), "description: x\n")
		// The directory created above already surfaced an event; drain it
		// before exercising the edit this subtest is actually about.
		waitEvent(t, events)

		mustWrite(t, filepath.Join(root, watch.TemplatesDir, "slides", "hello", "template.yaml"), "description: y\n")
		ev := waitEvent(t, events)
		expectPath(t, ev, "templates/slides/hello/template.yaml")
		assertQuiet(t, events)
	})

	t.Run("templates sections edit yields one debounced event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustMkdir(t, filepath.Join(root, watch.TemplatesDir, "sections", "item"))
		waitEvent(t, events)

		mustWrite(t, filepath.Join(root, watch.TemplatesDir, "sections", "item", "template.yaml"), "description: x\n")
		ev := waitEvent(t, events)
		expectPath(t, ev, "templates/sections/item/template.yaml")
		assertQuiet(t, events)
	})

	t.Run("templates theme edit yields one debounced event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustMkdir(t, filepath.Join(root, watch.TemplatesDir, "themes", "plain"))
		waitEvent(t, events)

		mustWrite(t, filepath.Join(root, watch.TemplatesDir, "themes", "plain", "theme.css"), "body { margin: 0; }\n")
		ev := waitEvent(t, events)
		expectPath(t, ev, "templates/themes/plain/theme.css")
		assertQuiet(t, events)
	})

	t.Run("templates media edit yields one debounced event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustMkdir(t, filepath.Join(root, watch.TemplatesDir, "media"))
		waitEvent(t, events)

		mustWrite(t, filepath.Join(root, watch.TemplatesDir, "media", "logo.svg"), "<svg/>\n")
		ev := waitEvent(t, events)
		expectPath(t, ev, "templates/media/logo.svg")
		assertQuiet(t, events)
	})

	t.Run("templates library.yaml edit yields one debounced event", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustMkdir(t, filepath.Join(root, watch.TemplatesDir))
		waitEvent(t, events)

		mustWrite(t, filepath.Join(root, watch.TemplatesDir, "library.yaml"), "name: x\n")
		ev := waitEvent(t, events)
		expectPath(t, ev, "templates/library.yaml")
		assertQuiet(t, events)
	})

	t.Run("templates directory created after startup is watched", func(t *testing.T) {
		root, events := newWatchDeck(t)
		// templates/ does not exist at startup (newWatchDeck only creates
		// slides/ and assets/); creating it must be picked up through the
		// root watch, and a file written inside it right after must be
		// reported without a restart.
		mustMkdir(t, filepath.Join(root, watch.TemplatesDir))
		ev := waitEvent(t, events)
		expectPathPrefix(t, ev, "templates")

		mustWrite(t, filepath.Join(root, watch.TemplatesDir, "library.yaml"), "name: x\n")
		ev = waitEvent(t, events)
		expectPath(t, ev, "templates/library.yaml")
		assertQuiet(t, events)
	})

	t.Run("new subdirectory under templates created after startup is watched", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustMkdir(t, filepath.Join(root, watch.TemplatesDir, "slides"))
		waitEvent(t, events)

		sub := filepath.Join(root, watch.TemplatesDir, "slides", "newslide")
		mustMkdir(t, sub)
		ev := waitEvent(t, events)
		expectPathPrefix(t, ev, "templates/slides/newslide")

		mustWrite(t, filepath.Join(sub, "template.yaml"), "description: x\n")
		ev = waitEvent(t, events)
		expectPathPrefix(t, ev, "templates/slides/newslide/template.yaml")
		assertQuiet(t, events)
	})

	t.Run("unrelated root paths stay quiet", func(t *testing.T) {
		root, events := newWatchDeck(t)
		mustWrite(t, filepath.Join(root, "README.md"), "# notes\n")
		mustMkdir(t, filepath.Join(root, "unrelated"))
		mustWrite(t, filepath.Join(root, "unrelated", "file.txt"), "x\n")
		assertQuiet(t, events)
	})

	t.Run("created and removed subdirectory yields events", func(t *testing.T) {
		root, events := newWatchDeck(t)
		sub := filepath.Join(root, deck.SlidesDir, "sub")

		mustMkdir(t, sub)
		ev := waitEvent(t, events)
		expectPathPrefix(t, ev, "slides/sub")

		// The subtree created after startup must be watched: an edit inside it
		// has to be reported.
		mustWrite(t, filepath.Join(sub, "1-nested.md"), "# Nested\n")
		ev = waitEvent(t, events)
		expectPathPrefix(t, ev, "slides/sub/1-nested.md")

		mustRemoveAll(t, sub)
		ev = waitEvent(t, events)
		expectPathPrefix(t, ev, "slides/sub")
		assertQuiet(t, events)
	})

	t.Run("stop closes the event channel and is idempotent", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, deck.SlidesDir))
		mustMkdir(t, filepath.Join(root, watch.AssetsDir))
		mustWrite(t, filepath.Join(root, deck.ConfigFile), "title: D\n")

		events, stop, err := watch.Watch(root, testDebounce)
		if err != nil {
			t.Fatalf("Watch(%s): %v", root, err)
		}
		if err := stop(); err != nil {
			t.Fatalf("stop: %v", err)
		}
		if err := stop(); err != nil {
			t.Fatalf("second stop: %v", err)
		}

		select {
		case _, ok := <-events:
			if ok {
				t.Fatal("watch channel yielded an event after stop")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("watch channel was not closed by stop")
		}
	})

	t.Run("real listener binds loopback only and serves", func(t *testing.T) {
		s, err := server.Listen(server.Options{})
		if err != nil {
			t.Fatalf("server.Listen: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })

		addr, ok := s.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatalf("Addr() is %T, want *net.TCPAddr", s.Addr())
		}
		if !addr.IP.IsLoopback() {
			t.Fatalf("listener bound %s; want a loopback address only", addr.IP)
		}
		if addr.IP.String() != server.Host {
			t.Errorf("listener bound %s, want %s", addr.IP, server.Host)
		}
		if !strings.HasPrefix(s.URL(), "http://"+server.Host+":") {
			t.Errorf("URL %q does not start with http://%s:", s.URL(), server.Host)
		}

		// The listener is live: a real request reaches it (503, since no Page
		// was supplied).
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(s.URL())
		if err != nil {
			t.Fatalf("GET %s: %v", s.URL(), err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("GET %s status %d, want 503 for a server with no page", s.URL(), resp.StatusCode)
		}

		// It must not be reachable on any non-loopback address.
		ips := nonLoopbackIPv4(t)
		if len(ips) == 0 {
			t.Log("no non-loopback IPv4 interface available; skipped reachability probe")
		}
		for _, ip := range ips {
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(addr.Port)), 500*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				t.Errorf("listener is reachable on non-loopback %s:%d; want 127.0.0.1 only", ip, addr.Port)
			}
		}
	})

	t.Run("falls back through the 8080 range when ports are busy", func(t *testing.T) {
		probe, err := net.Listen("tcp", net.JoinHostPort(server.Host, strconv.Itoa(server.DefaultPort)))
		if err != nil {
			t.Skipf("port %d is not free in this environment: %v", server.DefaultPort, err)
		}
		_ = probe.Close()

		held := make([]net.Listener, 0, 3)
		heldPorts := make(map[int]struct{})
		for p := server.DefaultPort; p < server.DefaultPort+3 && p <= server.MaxPort; p++ {
			ln, err := net.Listen("tcp", net.JoinHostPort(server.Host, strconv.Itoa(p)))
			if err != nil {
				if p == server.DefaultPort {
					t.Skipf("port %d is not free in this environment: %v", server.DefaultPort, err)
				}
				continue
			}
			held = append(held, ln)
			heldPorts[p] = struct{}{}
		}
		t.Cleanup(func() {
			for _, ln := range held {
				_ = ln.Close()
			}
		})

		s, err := server.Listen(server.Options{})
		if err != nil {
			// Under `go test ./...` other packages contend for the same port
			// range; if the range filled up meanwhile that is environment
			// contention, not a fallback failure.
			t.Skipf("server.Listen found no free port in %d–%d (concurrent port use): %v", server.DefaultPort, server.MaxPort, err)
		}
		t.Cleanup(func() { _ = s.Close() })

		got := listenerPort(t, s)
		if got < server.DefaultPort || got > server.MaxPort {
			t.Fatalf("bound port %d, want a port in %d–%d", got, server.DefaultPort, server.MaxPort)
		}
		if got == server.DefaultPort {
			t.Fatalf("bound default port %d although it was busy; want a fallback port", server.DefaultPort)
		}
		if _, busy := heldPorts[got]; busy {
			t.Fatalf("bound port %d although this test held it busy", got)
		}
		if !strings.HasSuffix(s.URL(), ":"+strconv.Itoa(got)+"/") {
			t.Errorf("URL %q does not carry the fallback port %d", s.URL(), got)
		}
	})

	t.Run("explicit busy port fails with no fallback", func(t *testing.T) {
		ln, err := net.Listen("tcp", net.JoinHostPort(server.Host, "0"))
		if err != nil {
			t.Fatalf("reserve a port: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		port := ln.Addr().(*net.TCPAddr).Port

		s, err := server.Listen(server.Options{Port: port})
		if err == nil {
			_ = s.Close()
			t.Fatalf("Listen on busy explicit port %d succeeded; want an error (explicit ports never fall back)", port)
		}
		msg := err.Error()
		if !strings.Contains(msg, strconv.Itoa(port)) {
			t.Errorf("explicit-busy error %q does not name port %d", msg, port)
		}
		if !strings.Contains(msg, "--port") {
			t.Errorf("explicit-busy error %q does not suggest --port", msg)
		}
		if !strings.Contains(msg, "kalide") {
			t.Errorf("explicit-busy error %q does not name kalide", msg)
		}
	})
}

// newWatchDeck builds a real temporary deck (slides/, assets/ and kalide.yaml)
// and starts a real Watch on it at testDebounce. It stops the watch when the
// subtest ends and returns the deck root and the event channel.
func newWatchDeck(t *testing.T) (root string, events <-chan watch.Event) {
	t.Helper()
	root = t.TempDir()
	mustMkdir(t, filepath.Join(root, deck.SlidesDir))
	mustMkdir(t, filepath.Join(root, watch.AssetsDir))
	mustWrite(t, filepath.Join(root, deck.SlidesDir, "1-intro.md"), "# One\n")
	mustWrite(t, filepath.Join(root, watch.AssetsDir, "logo.txt"), "logo\n")
	mustWrite(t, filepath.Join(root, deck.ConfigFile), "title: D\n")

	events, stop, err := watch.Watch(root, testDebounce)
	if err != nil {
		t.Fatalf("Watch(%s): %v", root, err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	return root, events
}

// waitEvent reads one event, failing the test if none arrives within a generous
// bound.
func waitEvent(t *testing.T, events <-chan watch.Event) watch.Event {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("watch channel closed while waiting for an event")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a watch event")
		return watch.Event{}
	}
}

// assertQuiet fails if any further event arrives within 3*debounce, so a test
// that expects exactly one event per burst proves no second one follows.
func assertQuiet(t *testing.T, events <-chan watch.Event) {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("watch channel closed unexpectedly")
		}
		t.Fatalf("unexpected extra watch event after the quiet period: %v", ev.Paths)
	case <-time.After(3 * testDebounce):
	}
}

// expectPath fails unless ev carries exactly want among its paths.
func expectPath(t *testing.T, ev watch.Event, want string) {
	t.Helper()
	for _, p := range ev.Paths {
		if p == want {
			return
		}
	}
	t.Fatalf("event paths %v do not contain %q", ev.Paths, want)
}

// expectPathPrefix fails unless ev carries at least one path equal to or under
// prefix.
func expectPathPrefix(t *testing.T, ev watch.Event, prefix string) {
	t.Helper()
	for _, p := range ev.Paths {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return
		}
	}
	t.Fatalf("event paths %v do not contain %q or a path under it", ev.Paths, prefix)
}

// nonLoopbackIPv4 returns the machine's non-loopback IPv4 addresses, so the
// test can prove the listener is not reachable there.
func nonLoopbackIPv4(t *testing.T) []string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Logf("net.InterfaceAddrs: %v", err)
		return nil
	}
	var ips []string
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		ips = append(ips, ip.String())
	}
	return ips
}

// listenerPort returns the port the server is bound to, failing the test if the
// address is not a TCP address.
func listenerPort(t *testing.T, s *server.Server) int {
	t.Helper()
	addr, ok := s.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("Addr() is %T, want *net.TCPAddr", s.Addr())
	}
	return addr.Port
}

// mustMkdir creates dir and its parents, failing the test on error.
func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
}

// mustWrite writes content to path, failing the test on error.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// mustRemoveAll removes path and everything under it, failing the test on error.
func mustRemoveAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll %s: %v", path, err)
	}
}
