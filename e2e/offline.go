// Package e2e offline enforcement (phase-9 task 1).
//
// assertOffline asserts that the real eypres process makes no outbound
// (non-loopback) network connection for the whole run. It is the cross-platform
// half: the per-OS mechanism that reads the process's connections lives in
// build-tagged files (offline_unix.go, offline_windows.go).
//
// Two independent belts are provided, and a test may use either or both:
//
//  1. Connection sampling. assertOffline(t, pid) starts a background sampler
//     and returns an *Offline handle. The sampler repeatedly asks the OS for
//     the connections owned by the eypres process (and, on Unix, its process
//     group) and records any endpoint that is not loopback. Stop reports every
//     violation with t.Errorf, so any non-127.0.0.1 connection fails the test.
//     The usual shape, because the PID is only known after Start:
//
//     h := NewHarness(t)
//     h.EnableOfflineProxy() // optional second belt
//     h.Start()
//     off := assertOffline(t, h.PID())
//     t.Cleanup(off.Stop)
//
//     Stop may also be called explicitly at the end of the run, and Check
//     reports the violations so far without stopping the sampler.
//
//  2. Proxy environment. EnableOfflineProxy points HTTP_PROXY, HTTPS_PROXY and
//     ALL_PROXY at a freshly-closed loopback port, so any HTTP(S) client in
//     eypres that honours the proxy environment fails its connection instead of
//     reaching the network. It must be called before Start, because it changes
//     the environment the process is started with. NO_PROXY keeps loopback
//     requests (the deck itself) out of the proxy.
//
// Like the rest of the harness this file has no dependency on the testing
// package: it talks to the tiny T interface that *testing.T satisfies.
package e2e

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// offlinePollInterval is the pause between connection samples. It is short
	// enough that a connection cannot be opened and closed between two samples
	// in normal use, and cheap enough (one OS process per sample) to run for a
	// whole test.
	offlinePollInterval = 100 * time.Millisecond

	// offlineProxyHost is the loopback host the offline proxy environment points
	// at. Its port is closed, so a proxy-honouring client fails fast.
	offlineProxyHost = "127.0.0.1"
)

// Offline is the handle assertOffline returns. It owns a background sampler that
// records every non-loopback connection the eypres process makes; Stop stops the
// sampler and fails the test if any was seen. Check reports the violations so
// far without stopping the sampler.
type Offline struct {
	t   T
	pid int

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	mu         sync.Mutex
	violations []string
}

// assertOffline starts sampling the network connections of the process pid and
// returns a handle whose Stop fails the test if the process made any outbound
// (non-loopback) connection while it ran.
//
// pid is normally h.PID() after h.Start(). On Unix the process group is sampled
// too, so a connection opened by a child of eypres is caught. A non-positive pid
// fails the test immediately.
//
// The caller must call Stop (commonly via t.Cleanup) to stop the sampler and
// assert the result.
func assertOffline(t T, pid int) *Offline {
	t.Helper()

	o := &Offline{
		t:    t,
		pid:  pid,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	if pid <= 0 {
		t.Errorf("e2e: assertOffline: invalid eypres pid %d", pid)
		close(o.done)
		return o
	}
	go o.sampleLoop()
	return o
}

// sampleLoop polls the OS for the process's connections until Stop, recording
// every non-loopback endpoint. A sampling failure (the OS tool is missing or
// errors) fails the test and ends sampling rather than silently passing.
func (o *Offline) sampleLoop() {
	defer close(o.done)

	for {
		select {
		case <-o.stop:
			return
		default:
		}

		conns, err := offlineForeignConnections(o.t, o.pid)
		if err != nil {
			o.t.Errorf("e2e: offline: sampling eypres pid %d: %v", o.pid, err)
			return
		}
		if len(conns) > 0 {
			o.mu.Lock()
			o.violations = append(o.violations, conns...)
			o.mu.Unlock()
		}

		select {
		case <-o.stop:
			return
		case <-time.After(offlinePollInterval):
		}
	}
}

// Check fails the test if any non-loopback connection has been observed so far.
// It does not stop the sampler.
func (o *Offline) Check() {
	o.t.Helper()

	o.mu.Lock()
	violations := append([]string(nil), o.violations...)
	o.mu.Unlock()

	if len(violations) > 0 {
		o.t.Errorf("e2e: eypres pid %d made %d outbound (non-loopback) connection(s):\n%s",
			o.pid, len(violations), strings.Join(violations, "\n"))
	}
}

// Stop stops the sampler (waiting for it to finish) and asserts that no
// non-loopback connection was made. It is idempotent and safe to call via
// t.Cleanup; the assertion is reported only on the first call.
func (o *Offline) Stop() {
	o.t.Helper()

	first := false
	o.stopOnce.Do(func() {
		first = true
		close(o.stop)
	})
	<-o.done
	if first {
		o.Check()
	}
}

// EnableOfflineProxy points the eypres process's proxy environment at a closed
// loopback port, so an HTTP(S) client in eypres that honours the proxy fails its
// connection rather than reaching the network. It must be called before Start:
// it changes the environment the process is started with.
//
// It is a belt-and-braces companion to assertOffline, not a replacement: the
// sampler observes the OS connection table regardless of how a connection was
// attempted.
func (h *Harness) EnableOfflineProxy() {
	h.t.Helper()

	h.mu.Lock()
	h.extraEnv = offlineProxyEnv()
	h.mu.Unlock()
}

// offlineProxyEnv returns the proxy environment entries that point every proxy
// variable at a closed loopback port. NO_PROXY excludes loopback so the deck the
// process serves is never routed through the (closed) proxy.
func offlineProxyEnv() []string {
	proxyURL := offlineProxyURL()
	return []string{
		"HTTP_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL,
		"HTTPS_PROXY=" + proxyURL,
		"https_proxy=" + proxyURL,
		"ALL_PROXY=" + proxyURL,
		"all_proxy=" + proxyURL,
		"NO_PROXY=" + offlineProxyHost + ",localhost",
		"no_proxy=" + offlineProxyHost + ",localhost",
	}
}

// offlineProxyURL returns "http://127.0.0.1:<port>" for a loopback port that
// was just closed. Any client that connects to it gets connection-refused.
func offlineProxyURL() string {
	ln, err := net.Listen("tcp", net.JoinHostPort(offlineProxyHost, "0"))
	if err != nil {
		return "http://" + net.JoinHostPort(offlineProxyHost, "9")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return "http://" + net.JoinHostPort(offlineProxyHost, strconv.Itoa(port))
}

// offlineForeignEndpoints returns the non-loopback endpoints in a connection
// endpoint spec, such as "127.0.0.1:8080" (listen), "127.0.0.1:51234" (local
// peer) or "127.0.0.1:51234->93.184.216.34:443" (a connected socket). Wildcard
// endpoints ("*:*", "0.0.0.0:0", "[::]:0") denote an unspecified peer or an
// all-interface listener and are not outbound, so they are ignored.
//
// It is shared by the per-OS samplers, which hand it the endpoint spec from
// lsof (macOS/Unix) or netstat (Windows).
func offlineForeignEndpoints(spec string) []string {
	spec = strings.TrimSpace(spec)
	if i := strings.IndexAny(spec, " \t("); i >= 0 {
		spec = spec[:i]
	}

	var out []string
	for _, ep := range strings.Split(spec, "->") {
		ep = strings.TrimSpace(ep)
		if ep == "" || offlineWildcardEndpoint(ep) {
			continue
		}
		if !offlineLoopbackHost(offlineEndpointHost(ep)) {
			out = append(out, ep)
		}
	}
	return out
}

// offlineWildcardEndpoint reports whether ep names no concrete address: an
// unspecified address such as 0.0.0.0:0 or [::]:0, or the "*" wildcard.
func offlineWildcardEndpoint(ep string) bool {
	host := strings.Trim(offlineEndpointHost(ep), "[]")
	if host == "" || host == "*" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// offlineEndpointHost extracts the host from an endpoint spec, handling bracketed
// IPv6 literals such as "[::1]:8080" and bare hosts.
func offlineEndpointHost(ep string) string {
	if strings.HasPrefix(ep, "[") {
		if i := strings.IndexByte(ep, ']'); i >= 0 {
			return ep[1:i]
		}
		return strings.Trim(ep, "[]")
	}
	if i := strings.LastIndexByte(ep, ':'); i >= 0 {
		return ep[:i]
	}
	return ep
}

// offlineLoopbackHost reports whether host is a loopback address: an IP literal
// whose net.IP.IsLoopback is true (127.0.0.0/8, ::1) or the localhost names.
func offlineLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "localhost", "ip6-localhost", "localhost.localdomain":
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// errOfflineToolMissing is returned by the per-OS samplers when their OS tool
// cannot be found, so the failure names the missing mechanism.
var errOfflineToolMissing = errors.New("offline sampling tool not found in PATH")
