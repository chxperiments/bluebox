// Package cli defines the bluebox command line. main() only calls Execute.
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"bluebox/internal/bluefile"
	"bluebox/internal/sandbox"
)

// exitCode lets a command choose the process status without cobra printing an
// error: a sandbox command that exits 3 is not a bluebox failure.
var exitCode = 0

// Command groups, in the order they appear in help.
const (
	groupSandbox = "sandbox"
	groupRun     = "run"
	groupState   = "state"
	groupInspect = "inspect"
	groupRemove  = "remove"
	groupHelp    = "help"
)

func newRoot() *cobra.Command {
	// Keep commands in the order they are added: new before build before ls
	// reads as a workflow, where alphabetical does not.
	cobra.EnableCommandSorting = false

	root := &cobra.Command{
		Use:   "bluebox",
		Short: "Isolated, persistent sandboxes",
		Long: "bluebox runs each sandbox as a real microVM with its own kernel.\n" +
			"Work in /data persists; everything else resets on every run.",
		SilenceUsage:  true, // an error is not a reason to reprint the manual
		SilenceErrors: true, // printed by Execute, without a stack of usage text
		Example: "  bluebox new devbox\n" +
			"  bluebox build devbox\n" +
			"  bluebox run devbox -- python3 script.py",
	}
	root.AddGroup(
		&cobra.Group{ID: groupSandbox, Title: "Defining sandboxes:"},
		&cobra.Group{ID: groupRun, Title: "Running things:"},
		&cobra.Group{ID: groupState, Title: "Managing state:"},
		&cobra.Group{ID: groupInspect, Title: "Inspecting:"},
		&cobra.Group{ID: groupRemove, Title: "Removing:"},
		&cobra.Group{ID: groupHelp, Title: "Shell:"},
	)
	root.SetHelpCommandGroupID(groupHelp)
	root.SetCompletionCommandGroupID(groupHelp)

	root.AddCommand(
		newCmd(), buildCmd(), lsCmd(),
		runCmd(), shellCmd(),
		resetCmd(), snapshotCmd(),
		envCmd(), logsCmd(), verifyCmd(),
		renameCmd(), destroyCmd(), nukeCmd(),
	)
	return root
}

// Execute runs the CLI and returns the process exit status.
func Execute() int {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "bluebox: %v\n", err)
		if exitCode != 0 {
			return exitCode
		}
		return 1
	}
	return exitCode
}

// loadSpec reads a sandbox's Bluefile.
func loadSpec(name string) (bluefile.Spec, error) {
	if !sandbox.Exists(name) {
		return bluefile.Spec{}, fmt.Errorf("no sandbox %q (create it: bluebox new %s)", name, name)
	}
	path, err := sandbox.BluefilePath(name)
	if err != nil {
		return bluefile.Spec{}, err
	}
	return bluefile.Parse(path)
}

// confirm asks before destroying something. Without a terminal it refuses
// rather than assuming yes, so a script cannot delete data by accident.
func confirm(prompt string, assumeYes bool) error {
	if assumeYes {
		return nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("refusing without a terminal; pass --yes to confirm")
	}
	fmt.Printf("%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.ToLower(strings.TrimSpace(line)) != "y" {
		return fmt.Errorf("cancelled")
	}
	return nil
}
