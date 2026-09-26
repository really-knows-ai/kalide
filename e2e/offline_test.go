package e2e

// Tests for the phase-9 offline enforcement helpers (offline.go and the
// build-tagged samplers). They are unit tests: the parsers are exercised with
// synthetic lsof/netstat output, and the OS sampler is exercised against this
// test process itself holding a UDP socket to a documentation address (no
// traffic is sent). They do not build kalide, so they run under -short too.
//
// The parser tests are split by build tag: parseLsofConnections is defined only
// on Unix (offline_unix_test.go) and parseNetstatConnections only on Windows
// (offline_windows_test.go), so each is referenced where it exists.

import (
	"net"
	"strings"
	"testing"
)

// fakeT is a T that records failures instead of aborting the test, so the
// offline helpers' pass/fail behaviour can be asserted directly.
type fakeT struct {
	fatals []string
	errors []string
}

func (f *fakeT) Helper()                        {}
func (f *fakeT) Cleanup(func())                 {}
func (f *fakeT) TempDir() string                { return "" }
func (f *fakeT) Fatalf(format string, a ...any) { f.fatals = append(f.fatals, format) }
func (f *fakeT) Errorf(format string, a ...any) { f.errors = append(f.errors, format) }

// TestOfflineForeignEndpoints pins the endpoint classifier: loopback and
// wildcard endpoints are not outbound; anything else is.
func TestOfflineForeignEndpoints(t *testing.T) {
	cases := []struct {
		spec string
		want []string
	}{
		{"127.0.0.1:8080", nil},
		{"127.0.0.1:8080 (LISTEN)", nil},
		{"*:8080", nil},
		{"*:*", nil},
		{"0.0.0.0:0", nil},
		{"[::]:0", nil},
		{"[::1]:8080", nil},
		{"localhost:8080", nil},
		{"127.0.0.1:51234->127.0.0.1:8080", nil},
		{"93.184.216.34:443", []string{"93.184.216.34:443"}},
		{"93.184.216.34:443 (ESTABLISHED)", []string{"93.184.216.34:443"}},
		{"127.0.0.1:51234->93.184.216.34:443", []string{"93.184.216.34:443"}},
		{"[2606:2800:220:1:248:1893:25c8:1946]:443", []string{"[2606:2800:220:1:248:1893:25c8:1946]:443"}},
	}
	for _, tc := range cases {
		got := offlineForeignEndpoints(tc.spec)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("offlineForeignEndpoints(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}

// TestAssertOfflineRejectsInvalidPID pins the guard: a non-positive pid fails
// immediately instead of silently sampling nothing.
func TestAssertOfflineRejectsInvalidPID(t *testing.T) {
	ft := &fakeT{}
	o := assertOffline(ft, 0)
	o.Stop()
	if len(ft.errors) == 0 {
		t.Errorf("assertOffline(0) recorded no error")
	}
}

// TestOfflineProxyEnv pins the belt-and-braces environment: every proxy variable
// points at a closed loopback port and loopback is excluded from the proxy.
func TestOfflineProxyEnv(t *testing.T) {
	env := offlineProxyEnv()
	joined := strings.Join(env, "\n")
	for _, key := range []string{
		"HTTP_PROXY=", "http_proxy=", "HTTPS_PROXY=", "https_proxy=",
		"ALL_PROXY=", "all_proxy=", "NO_PROXY=", "no_proxy=",
	} {
		if !strings.Contains(joined, key) {
			t.Errorf("offlineProxyEnv is missing %s:\n%s", key, joined)
		}
	}
	if !strings.Contains(joined, "127.0.0.1:") {
		t.Errorf("offlineProxyEnv does not point at a loopback port:\n%s", joined)
	}
}

// containsEndpoint reports whether any sampled endpoint equals want or carries
// want as its remote half.
func containsEndpoint(conns []string, want string) bool {
	for _, c := range conns {
		if c == want || strings.HasSuffix(c, "->"+want) {
			return true
		}
	}
	return false
}

// portOf returns the port of a net.Addr as a string.
func portOf(t *testing.T, addr net.Addr) string {
	t.Helper()
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	return port
}
