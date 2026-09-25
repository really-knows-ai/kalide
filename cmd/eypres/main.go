// Command eypres authors and presents Markdown slide decks.
//
// The command is a thin shim: it delegates os.Args to internal/cli.Run and
// exits with the returned status code. All real work lives in internal/cli.
package main

import (
	"os"

	"github.com/really-knows-ai/ey-present/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args, os.Stdout, os.Stderr))
}
