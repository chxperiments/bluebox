package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"bluebox/internal/bluefile"
	"bluebox/internal/runtime"
	"bluebox/internal/sandbox"
)

func newCmd() *cobra.Command {
	return &cobra.Command{
		Use: "new <name>", Short: "Create a sandbox", GroupID: groupSandbox,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if _, err := sandbox.Create(args[0]); err != nil {
				return err
			}
			path, err := sandbox.BluefilePath(args[0])
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(bluefile.Template), 0o644); err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}

func buildCmd() *cobra.Command {
	return &cobra.Command{
		Use: "build <name>", Short: "Build the image and verify isolation", GroupID: groupSandbox,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, err := loadSpec(args[0])
			if err != nil {
				return err
			}
			if err := runtime.Preflight(); err != nil {
				return err
			}
			if err := runtime.Build(args[0], s); err != nil {
				return err
			}
			// Verify at build so a sandbox that is not really a VM never
			// reaches first use.
			return verify(args[0], s)
		},
	}
}

func verifyCmd() *cobra.Command {
	return &cobra.Command{
		Use: "verify <name>", Short: "Check the sandbox has its own kernel", GroupID: groupInspect,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, err := loadSpec(args[0])
			if err != nil {
				return err
			}
			if err := runtime.Preflight(); err != nil {
				return err
			}
			return verify(args[0], s)
		},
	}
}

func verify(name string, s bluefile.Spec) error {
	host, err := runtime.HostKernel()
	if err != nil {
		return err
	}
	guest, err := runtime.GuestKernel(name, s)
	if err != nil || guest == "" {
		return fmt.Errorf("could not read the guest kernel; is the image built?")
	}
	if guest == host {
		return fmt.Errorf("not isolated: guest and host both run %s.\n"+
			"This is a container, not a microVM", host)
	}
	fmt.Printf("isolated: host %s, guest %s\n", host, guest)
	return nil
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use: "run <name> [command...]", Short: "Run a command in a fresh microVM", GroupID: groupRun,
		Args:    cobra.MinimumNArgs(2),
		Example: "  bluebox run devbox -- python3 script.py",
		RunE: func(_ *cobra.Command, args []string) error {
			name, argv := args[0], args[1:]
			s, err := loadSpec(name)
			if err != nil {
				return err
			}
			if err := runtime.Preflight(); err != nil {
				return err
			}
			err = runtime.Run(name, s, argv)
			switch {
			case err == nil:
				return nil
			case err == runtime.ErrTimeout:
				exitCode = runtime.ExitTimeout
				return fmt.Errorf("killed after %ds (timeout_seconds)", s.TimeoutSeconds)
			default:
				// A non-zero exit from the sandbox is its result, not our error.
				if code := runtime.ExitCode(err); code >= 0 {
					exitCode = code
					return nil
				}
				return err
			}
		},
	}
}

func shellCmd() *cobra.Command {
	return &cobra.Command{
		Use: "shell <name>", Short: "Open an interactive shell", GroupID: groupRun,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, err := loadSpec(args[0])
			if err != nil {
				return err
			}
			if err := runtime.Preflight(); err != nil {
				return err
			}
			if err := runtime.Shell(args[0], s); err != nil {
				if code := runtime.ExitCode(err); code >= 0 {
					exitCode = code
					return nil
				}
				return err
			}
			return nil
		},
	}
}

func resetCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use: "reset <name>", Short: "Clear a sandbox's /data", GroupID: groupState,
		Long: "Empties /data, returning the sandbox to a clean state.\n" +
			"The sandbox and its image are untouched.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if !sandbox.Exists(name) {
				return fmt.Errorf("no sandbox %q", name)
			}
			// Only ask when there is actually something to lose.
			empty, err := sandbox.DataEmpty(name)
			if err != nil {
				return err
			}
			if !empty {
				if err := confirm(fmt.Sprintf("Delete everything in %s's /data?", name), yes); err != nil {
					return err
				}
			}
			if err := sandbox.ResetData(name); err != nil {
				return err
			}
			fmt.Printf("reset %s\n", name)
			return nil
		},
	}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return c
}

func snapshotCmd() *cobra.Command {
	var list bool
	c := &cobra.Command{
		Use: "snapshot <name>", Short: "Archive a sandbox's /data", GroupID: groupState,
		Long: "Archives /data to ~/.bluebox/snapshots/<name>/<timestamp>.tar.gz.\n" +
			"Restore with: tar -xzf <archive> -C \"$(bluebox env <name> | grep BLUEBOX_DATA | cut -d= -f2)\"",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if !sandbox.Exists(name) {
				return fmt.Errorf("no sandbox %q", name)
			}
			if list {
				snaps, err := sandbox.Snapshots(name)
				if err != nil {
					return err
				}
				for _, s := range snaps {
					fmt.Println(s)
				}
				return nil
			}
			path, err := sandbox.Snapshot(name, time.Now().UTC().Format("20060102T150405Z"))
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
	c.Flags().BoolVarP(&list, "list", "l", false, "list existing snapshots instead")
	return c
}

func envCmd() *cobra.Command {
	return &cobra.Command{
		Use: "env <name>", Short: "Print settings as KEY=VALUE", GroupID: groupInspect,
		Long:    "Shell-consumable settings, for: eval $(bluebox env <name>)",
		Args:    cobra.ExactArgs(1),
		Example: "  eval $(bluebox env devbox)",
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			s, err := loadSpec(name)
			if err != nil {
				return err
			}
			data, _ := sandbox.DataDir(name)
			fmt.Printf("BLUEBOX_NAME=%s\n", name)
			fmt.Printf("BLUEBOX_IMAGE=%s\n", sandbox.ImageTag(name))
			fmt.Printf("BLUEBOX_DATA=%s\n", data)
			fmt.Printf("BLUEBOX_BASE=%s\n", s.Base)
			fmt.Printf("BLUEBOX_CPUS=%d\n", s.CPUs)
			fmt.Printf("BLUEBOX_RAM_MIB=%d\n", s.RAMMiB)
			fmt.Printf("BLUEBOX_NETWORK=%s\n", s.Network)
			fmt.Printf("BLUEBOX_READONLY=%t\n", s.ReadOnlyRootfs)
			fmt.Printf("BLUEBOX_TIMEOUT_SECONDS=%d\n", s.TimeoutSeconds)
			keys := make([]string, 0, len(s.Env))
			for k := range s.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("%s=%s\n", k, s.Env[k])
			}
			return nil
		},
	}
}

func logsCmd() *cobra.Command {
	var lines int
	c := &cobra.Command{
		Use: "logs <name>", Short: "Show recent runs", GroupID: groupInspect,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if !sandbox.Exists(name) {
				return fmt.Errorf("no sandbox %q", name)
			}
			p, err := sandbox.LogPath(name)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil // nothing run yet; nothing to say
			}
			all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
			if len(all) > lines {
				all = all[len(all)-lines:]
			}
			fmt.Println(strings.Join(all, "\n"))
			return nil
		},
	}
	c.Flags().IntVarP(&lines, "lines", "n", 200, "how many lines to show")
	return c
}

func renameCmd() *cobra.Command {
	return &cobra.Command{
		Use: "rename <old> <new>", Short: "Rename a sandbox", GroupID: groupRemove,
		Long: "Moves the definition, data, logs and snapshots, and retags the\n" +
			"built image so no rebuild is needed.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := sandbox.Rename(args[0], args[1]); err != nil {
				return err
			}
			runtime.RetagImage(args[0], args[1])
			fmt.Printf("%s -> %s\n", args[0], args[1])
			return nil
		},
	}
}

func destroyCmd() *cobra.Command {
	var withData, yes bool
	c := &cobra.Command{
		Use: "destroy <name>", Short: "Remove a sandbox", GroupID: groupRemove,
		Long: "Removes the sandbox and its image. /data is kept unless --data,\n" +
			"since it is the only part a rebuild cannot reproduce.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if !sandbox.Exists(name) {
				return fmt.Errorf("no sandbox %q", name)
			}
			data, _ := sandbox.DataDir(name)
			if withData {
				empty, _ := sandbox.DataEmpty(name)
				if !empty {
					if err := confirm(fmt.Sprintf("Delete %s and its data?", name), yes); err != nil {
						return err
					}
				}
			}
			runtime.RemoveImage(name)
			if err := sandbox.Remove(name, withData); err != nil {
				return err
			}
			if withData {
				fmt.Printf("destroyed %s\n", name)
			} else {
				fmt.Printf("destroyed %s (data kept: %s)\n", name, data)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&withData, "data", false, "also delete /data and snapshots")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return c
}

func nukeCmd() *cobra.Command {
	var noData, yes bool
	c := &cobra.Command{
		Use: "nuke", Short: "Remove every sandbox", GroupID: groupRemove,
		Long: "Removes every sandbox, its image and its data.\n" +
			"Pass --no-data to keep the data directories.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			names, err := sandbox.List()
			if err != nil || len(names) == 0 {
				return nil // nothing to do
			}
			what := "and their data"
			if noData {
				what = "keeping their data"
			}
			if err := confirm(fmt.Sprintf("Remove %s (%s), %s?",
				plural(len(names), "sandbox", "sandboxes"),
				strings.Join(names, ", "), what), yes); err != nil {
				return err
			}
			for _, n := range names {
				runtime.RemoveImage(n)
				if err := sandbox.Remove(n, !noData); err != nil {
					return fmt.Errorf("%s: %w", n, err)
				}
			}
			fmt.Printf("removed %s\n", plural(len(names), "sandbox", "sandboxes"))
			return nil
		},
	}
	c.Flags().BoolVar(&noData, "no-data", false, "keep /data and snapshots")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return c
}

func lsCmd() *cobra.Command {
	return &cobra.Command{
		Use: "ls", Short: "List sandboxes", GroupID: groupSandbox,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			names, err := sandbox.List()
			if err != nil || len(names) == 0 {
				return nil
			}
			fmt.Printf("%-14s %-5s %-7s %-7s %-6s %-8s %s\n",
				"NAME", "CPUS", "RAM", "NET", "RO", "TIMEOUT", "BASE")
			for _, name := range names {
				path, _ := sandbox.BluefilePath(name)
				s, err := bluefile.Parse(path)
				if err != nil {
					fmt.Printf("%-14s invalid Bluefile\n", name)
					continue
				}
				timeout := "-"
				if s.TimeoutSeconds > 0 {
					timeout = strconv.Itoa(s.TimeoutSeconds) + "s"
				}
				fmt.Printf("%-14s %-5d %-7s %-7s %-6t %-8s %s\n", name, s.CPUs,
					strconv.Itoa(s.RAMMiB)+"M", s.Network, s.ReadOnlyRootfs, timeout, s.Base)
			}
			return nil
		},
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
