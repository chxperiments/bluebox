// Command bluebox runs disposable microVM sandboxes for AI harnesses.
// All logic lives in internal/cli; this is just the entrypoint.
package main

import (
	"os"

	"bluebox/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
