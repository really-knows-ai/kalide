// Command kalide authors and presents Markdown slide decks.
//
// The command is a thin shim: it delegates os.Args to internal/cli.Run and
// exits with the returned status code. All real work lives in internal/cli.
package main

import (
	"os"

	"github.com/really-knows-ai/kalide/internal/cli"
)

// version is the release version stamped in at build time:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// `make release` injects $(VERSION) this way; unbuilt/source runs report the
// default. It is intentionally a plain variable with no user-facing surface —
// the release workflow is the only setter.
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args, os.Stdout, os.Stderr))
}
