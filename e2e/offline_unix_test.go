//go:build !windows

package e2e

// Unix-only tests: the lsof parser and the real lsof sampler.

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestParseLsofConnections feeds the Unix sampler synthetic lsof -F output: a
// loopback listener and a local->foreign IPv4 connection, plus an IPv6 socket
// and a non-network file whose name must be ignored.
func TestParseLsofConnections(t *testing.T) {
	out := strings.Join([]string{
		"p1234",
		"cgo",
		"tIPv4",
		"n127.0.0.1:8080",
		"tIPv4",
		"n127.0.0.1:51234->93.184.216.34:443",
		"tIPv6",
		"n[::1]:9090",
		"tREG",
		"n/some/file.txt",
		"",
	}, "\n")

	got := parseLsofConnections(out)
	want := []string{"93.184.216.34:443"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("parseLsofConnections = %v, want %v", got, want)
	}
}

// TestOfflineForeignConnectionsSampler exercises the real OS sampler against
// this test process. It holds a UDP socket connected to 192.0.2.1 (TEST-NET-1,
// reserved and unroutable; connecting a UDP socket sends no packets) and asserts
// the sampler reports that foreign endpoint, while a loopback listener is not
// reported.
func TestOfflineForeignConnectionsSampler(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		if _, err := os.Stat("/usr/bin/lsof"); err != nil {
			t.Skip("lsof not available")
		}
	}

	foreignConn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9})
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer foreignConn.Close()

	loop, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer loop.Close()

	// The sockets are local to this process, so sample our own pid; allow the
	// OS a moment to publish them.
	pid := os.Getpid()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conns, err := offlineForeignConnections(t, pid)
		if err != nil {
			t.Fatalf("offlineForeignConnections: %v", err)
		}
		if containsEndpoint(conns, "192.0.2.1:9") {
			if containsEndpoint(conns, net.JoinHostPort("127.0.0.1", portOf(t, loop.Addr()))) {
				t.Errorf("sampler reported the loopback listener as foreign: %v", conns)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sampler did not report the foreign UDP endpoint 192.0.2.1:9; got %v", conns)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
