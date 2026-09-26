//go:build !windows

package e2e

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// This file is the Unix (macOS and other non-Windows) half of the offline
// enforcement: it reads the connection table the OS attributes to the eypres
// process. The common machinery and the public assertOffline API live in
// offline.go; the Windows counterpart is offline_windows.go.
//
// lsof is the mechanism. It is run twice and the results unioned, because lsof
// ANDs multiple selection options and a single process is only its own process
// group leader when it was started in a new group:
//
//   - `lsof -nP -i -a -p <pid> -F pctn` selects the internet sockets of the
//     eypres process itself;
//   - `lsof -nP -i -a -g <pid> -F pctn` selects the internet sockets of every
//     process in process group <pid>. The harness starts eypres with Setpgid,
//     so its group id is its pid and this catches any child it spawns.
//
// A process that is not a group leader matches only the first query; the group
// query then exits 1 (no matching open files), which is not an error. The -n and
// -P flags suppress DNS and port-name lookups, keeping the sample offline itself
// and the output numeric, and the machine-readable -F output avoids parsing the
// space-aligned columns.
//
// lsof is present on the supported macOS target and on Linux; when it is not in
// PATH the sample fails loudly rather than passing silently.

// offlineForeignConnections samples the TCP/UDP endpoints owned by pid (and its
// process group on Unix) and returns those that are not loopback. It returns an
// error only when the sampling mechanism itself fails; "no connections" is an
// empty slice and a nil error.
func offlineForeignConnections(t T, pid int) ([]string, error) {
	byProcess, err := lsofForeign(pid, false)
	if err != nil {
		return nil, err
	}
	byGroup, err := lsofForeign(pid, true)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(byProcess)+len(byGroup))
	var foreign []string
	for _, ep := range append(byProcess, byGroup...) {
		if _, ok := seen[ep]; ok {
			continue
		}
		seen[ep] = struct{}{}
		foreign = append(foreign, ep)
	}
	return foreign, nil
}

// lsofForeign runs one lsof query — by process (-p) or by process group (-g) —
// and returns its non-loopback endpoints. Exit status 1 means "no matching open
// files" and is not an error.
func lsofForeign(pid int, group bool) ([]string, error) {
	selector := "-p"
	if group {
		selector = "-g"
	}
	args := []string{
		"-nP", "-i", "-a",
		selector, strconv.Itoa(pid),
		"-F", "pctn",
	}
	out, err := exec.Command("lsof", args...).Output()
	if err != nil {
		// lsof exits 1 when it finds no matching open files; that is not a
		// failure. A missing lsof (exec.ErrNotFound) or any other exit status
		// is.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil, nil
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.Join(errOfflineToolMissing, err)
		}
		return nil, err
	}
	return parseLsofConnections(string(out)), nil
}

// parseLsofConnections extracts the non-loopback endpoint specs from lsof's
// -F output. In -F output each record is one field per line: "p<pid>",
// "c<command>", "t<type>" (IPv4/IPv6/…), "n<name>" (the endpoint). A name is
// only a network endpoint when it belongs to an IPv4/IPv6 file, so the parser
// tracks the current file type and ignores names of other file types (paths of
// regular files, pipes and so on).
func parseLsofConnections(out string) []string {
	var foreign []string
	network := false

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			// A new process record: the previous type no longer applies.
			network = false
		case 'c':
			// Command name; nothing to record.
		case 't':
			typ := line[1:]
			network = typ == "IPv4" || typ == "IPv6"
		case 'n':
			if !network {
				continue
			}
			spec := line[1:]
			// Only host:port specs are endpoints; skip anything without one.
			if !strings.Contains(spec, ":") {
				continue
			}
			foreign = append(foreign, offlineForeignEndpoints(spec)...)
		}
	}
	return foreign
}
