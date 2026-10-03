// Command bluebox runs isolated, persistent sandboxes as microVMs.
package main

import (
	"os"
	"runtime"

	"bluebox/internal/cli"
	"bluebox/internal/guest"
)

func main() {
	// Inside a Firecracker guest the kernel starts bluebox as init.
	if os.Getpid() == 1 && runtime.GOOS == "linux" {
		guest.Init()
		return
	}
	os.Exit(cli.Execute())
}
