//go:build windows

package e2e

// Windows-only tests: the netstat parser.

import (
	"strings"
	"testing"
)

// TestParseNetstatConnections feeds the Windows sampler synthetic netstat -ano
// output: rows for the kalide PID (loopback listener and a foreign connection),
// rows for other PIDs (including a foreign one) that must be ignored, and an
// IPv6 loopback listener.
func TestParseNetstatConnections(t *testing.T) {
	out := strings.Join([]string{
		"Proto  Local Address          Foreign Address        State           PID",
		"TCP    127.0.0.1:8080         0.0.0.0:0              LISTENING       4321",
		"TCP    127.0.0.1:51234        93.184.216.34:443      ESTABLISHED     4321",
		"TCP    127.0.0.1:6000         203.0.113.5:80         ESTABLISHED     9999",
		"UDP    [::1]:5353             *:*                                    4321",
		"UDP    0.0.0.0:1234           203.0.113.7:53                        4321",
		"",
	}, "\n")

	got := parseNetstatConnections(out, 4321)
	want := []string{"93.184.216.34:443", "203.0.113.7:53"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("parseNetstatConnections = %v, want %v", got, want)
	}
}
