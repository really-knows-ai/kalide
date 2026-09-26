//go:build windows

package e2e

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// This file is the Windows half of the offline enforcement: it reads the
// connection table the OS attributes to the eypres process. The common
// machinery and the public assertOffline API live in offline.go; the Unix
// counterpart is offline_unix.go.
//
// netstat -ano is the mechanism. It is present on every supported Windows
// target and prints a stable, parseable table whose last column is the owning
// PID:
//
//	Proto  Local Address     Foreign Address    State        PID
//	TCP    127.0.0.1:8080    0.0.0.0:0          LISTENING    1234
//	TCP    127.0.0.1:51000   93.184.216.34:443  ESTABLISHED  1234
//
// Sampling is filtered to the eypres PID: the Foreign Address of every TCP row
// owned by that PID is the remote endpoint, and any non-loopback remote is a
// violation. Windows has no process group in the Unix sense, so the eypres PID
// is the whole surface (the harness starts it with CREATE_NEW_PROCESS_GROUP and
// eypres leaves no child behind, which Stop already asserts).

// offlineForeignConnections samples the TCP endpoints owned by pid and returns
// those whose remote address is not loopback. It returns an error only when the
// sampling mechanism itself fails.
func offlineForeignConnections(t T, pid int) ([]string, error) {
	out, err := exec.Command("netstat", "-ano").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// Any non-zero exit is a real sampling failure on Windows; netstat
			// does not use exit status to mean "no rows".
			return nil, err
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.Join(errOfflineToolMissing, err)
		}
		return nil, err
	}
	return parseNetstatConnections(string(out), pid), nil
}

// parseNetstatConnections extracts the non-loopback remote addresses from
// netstat -ano output, keeping only rows whose owning PID is pid. A row is a
// data row when it starts with the TCP or UDP protocol and carries at least
// Proto, Local Address, Foreign Address and PID columns.
func parseNetstatConnections(out string, pid int) []string {
	want := strconv.Itoa(pid)
	var foreign []string

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "TCP", "UDP":
		default:
			continue
		}
		// The owning PID is the last column in both the TCP and UDP layouts.
		if fields[len(fields)-1] != want {
			continue
		}
		foreign = append(foreign, offlineForeignEndpoints(fields[2])...)
	}
	return foreign
}
