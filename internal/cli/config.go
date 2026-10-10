package cli

// This file implements `kalide config` (phase-02 task 1):
// requirements.requirement.cli-config and solution.component.config-command.
//
// `kalide config` manages kalide's user-level settings — the flat
// key-to-string-value map persisted in the per-user config file (see
// internal/config, the storage half of domain.service.load-user-config). It is
// the CLI presentation half: it parses the four subcommands, validates the
// argument list and the key against the store's recognised key set, and drives
// the internal/config store:
//
//   - `kalide config get <key>` prints the value of a set recognised key to
//     stdout and exits 0; a recognised-but-unset key prints an error naming the
//     key to stderr and exits 1;
//   - `kalide config set <key> <value>` writes the value and exits 0;
//   - `kalide config unset <key>` removes the recognised key when present and
//     is a no-op success when it is absent, exiting 0 either way;
//   - `kalide config list` prints one `key=value` line per set recognised key
//     (nothing when none are set) and exits 0.
//
// Every argument-list problem — a missing or unknown subcommand, a missing
// <key>, a `set` missing its <value>, an extra argument, or a key outside the
// recognised set — is a usage error: usage to stderr listing the recognised
// keys, exit 2, and nothing written. All validation happens before the store is
// called, so a usage error never touches the config file. A store error other
// than an unset get (an unreadable or unparsable file, an unwritable path) is
// reported to stderr and exits 1.
//
// The command is local-filesystem only: it never touches the network. All
// output goes to the supplied writers, so it is testable without a real
// terminal.

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/really-knows-ai/kalide/internal/config"
)

// runConfig implements `kalide config get|set|unset|list`. It returns the
// process status code: 0 on success; 1 when the store fails (a recognised key
// that is unset on get, an unreadable/unparsable config file, or a failed
// write); 2 for a usage error (a missing or unknown subcommand, a wrong
// argument count, or a key outside the recognised set), having written nothing.
func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "kalide config: missing subcommand\n\n")
		configUsage(stderr)
		return 2
	}

	sub := args[0]
	if sub != "get" && sub != "set" && sub != "unset" && sub != "list" {
		fmt.Fprintf(stderr, "kalide config: unknown subcommand %q\n\n", sub)
		configUsage(stderr)
		return 2
	}

	// `list` takes no arguments; the other three take a <key> and `set` also a
	// <value>. Validate the whole argument list and the key up front, before
	// any store call, so a usage error writes nothing.
	switch sub {
	case "list":
		if len(args) > 1 {
			fmt.Fprintf(stderr, "kalide config list: unexpected argument %q\n\n", args[1])
			configUsage(stderr)
			return 2
		}
	default:
		want := 2
		if sub == "set" {
			want = 3
		}
		switch {
		case len(args) < want:
			fmt.Fprintf(stderr, "kalide config %s: missing %s\n\n", sub, configArgName(sub, len(args)))
			configUsage(stderr)
			return 2
		case len(args) > want:
			fmt.Fprintf(stderr, "kalide config %s: unexpected argument %q\n\n", sub, args[want])
			configUsage(stderr)
			return 2
		}
		if key := args[1]; !config.RecognisedKeys[key] {
			fmt.Fprintf(stderr, "kalide config %s: unrecognised key %q\n\n", sub, key)
			configUsage(stderr)
			return 2
		}
	}

	// The argument list and key are valid; drive the store. A store error other
	// than the unset-get case is an operational failure (exit 1), not a usage
	// error.
	switch sub {
	case "get":
		value, err := config.Get(args[1])
		if err != nil {
			fmt.Fprintf(stderr, "kalide config get: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, value)
		return 0
	case "set":
		if err := config.Set(args[1], args[2]); err != nil {
			fmt.Fprintf(stderr, "kalide config set: %v\n", err)
			return 1
		}
		return 0
	case "unset":
		if err := config.Unset(args[1]); err != nil {
			fmt.Fprintf(stderr, "kalide config unset: %v\n", err)
			return 1
		}
		return 0
	default: // "list"
		entries, err := config.List()
		if err != nil {
			fmt.Fprintf(stderr, "kalide config list: %v\n", err)
			return 1
		}
		for _, entry := range entries {
			fmt.Fprintln(stdout, entry)
		}
		return 0
	}
}

// configArgName names the argument that a too-short get/set/unset invocation is
// missing. get/unset take <key> as their only argument; set takes <key> then
// <value>, so the first missing token is the value once the key is present.
func configArgName(sub string, have int) string {
	if sub == "set" && have >= 2 {
		return "value"
	}
	return "key"
}

// configUsage prints the config-specific usage — the four subcommands and the
// recognised key set — followed by the shared usage text. It goes to stderr,
// like every usage error.
func configUsage(stderr io.Writer) {
	keys := make([]string, 0, len(config.RecognisedKeys))
	for key := range config.RecognisedKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	fmt.Fprintf(stderr, `kalide config <subcommand>
  kalide config get <key>          Print the value of a set recognised key
  kalide config set <key> <value>  Write a recognised key
  kalide config unset <key>        Remove a recognised key
  kalide config list               List the set recognised keys

recognised keys: %s

`, strings.Join(keys, ", "))
	fmt.Fprint(stderr, usage)
}
