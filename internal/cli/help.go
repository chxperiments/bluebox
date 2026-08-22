package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// banner is drawn on `bluebox` and `bluebox --help`.
const banner = `     ┌──────────────┐
    ╱              ╱│
   ┌──────────────┐ │
   │   bluebox    │ │
   │              │ ╱
   └──────────────┘`

// blue wraps s in a true-blue SGR sequence when the terminal will render it.
// Piped output and NO_COLOR get plain text, so logs stay clean.
func blue(s string) string {
	if !colorOK() {
		return s
	}
	return "\x1b[38;5;33m" + s + "\x1b[0m"
}

func colorOK() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// helpGroups is the order groups appear, with the short label shown once per
// group instead of a heading line per section.
var helpGroups = []struct{ id, label string }{
	{groupSandbox, "define"},
	{groupRun, "run"},
	{groupState, "state"},
	{groupInspect, "inspect"},
	{groupRemove, "remove"},
}

// rootHelp prints a compact overview: the box, one usage line, and one line
// per command. Detail lives behind `bluebox <command> -h`.
func rootHelp(c *cobra.Command) {
	var b strings.Builder
	b.WriteString(blue(banner))
	b.WriteString("\n\n   usage: bluebox <command> [flags]\n\n")

	for _, g := range helpGroups {
		label := g.label
		for _, sub := range c.Commands() {
			if sub.GroupID != g.id || sub.Hidden || !sub.IsAvailableCommand() {
				continue
			}
			fmt.Fprintf(&b, "   %-8s %-9s %s\n", label, sub.Name(), sub.Short)
			label = "" // the group is named once, then indented under it
		}
	}
	b.WriteString("\n   bluebox <command> -h   flags and detail\n")
	fmt.Print(b.String())
}

// installHelp gives the root a compact overview while leaving subcommands with
// cobra's standard layout, which is already the right shape for one command.
func installHelp(root *cobra.Command) {
	root.SetHelpFunc(func(c *cobra.Command, _ []string) {
		if !c.HasParent() {
			rootHelp(c)
			return
		}
		if c.Long != "" {
			fmt.Println(c.Long)
		} else if c.Short != "" {
			fmt.Println(c.Short)
		}
		fmt.Print("\n", c.UsageString())
	})
}
