// Command bluebox runs isolated, persistent sandboxes as microVMs.
package main

import (
	"os"

	"bluebox/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
