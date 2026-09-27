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
// default (`dev`). main passes it to internal/cli.Run, which exposes it to
// users through `kalide version` (and `--version`/`-v`): the build-time
// stamping is still the only setter, but the variable now has a user-facing
// surface.
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args, version, os.Stdout, os.Stderr))
}
