//go:build !linux

package agent

import (
	"os/exec"
	"strings"
)

// KernelRelease is uname -r. The agent itself only ever runs in a Linux
// guest; this is for the host side on other systems.
func KernelRelease() (string, error) {
	out, err := exec.Command("uname", "-r").Output()
	return strings.TrimSpace(string(out)), err
}
