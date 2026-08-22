package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
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
		Use: "new <name>", Short: "write a Bluefile", GroupID: groupSandbox,
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
		Use: "build <name>", Short: "build image, verify isolation", GroupID: groupSandbox,
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
		Use: "verify <name>", Short: "re-check the kernel boundary", GroupID: groupInspect,
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

// verify proves the sandbox got its own kernel, by comparing it against the
// kernel a plain container sees. Comparing against the host's own uname would
// be wrong on macOS, where the host runs Darwin and any Linux container kernel
// differs from it whether or not a microVM is involved.
func verify(name string, s bluefile.Spec) error {
	guest, err := runtime.GuestKernel(name, s)
	if err != nil || guest == "" {
		return fmt.Errorf("could not start the sandbox; is the image built?")
	}
	baseline, err := runtime.BaselineKernel(name, s)
	if err != nil || baseline == "" {
		return fmt.Errorf("could not read the baseline kernel")
	}
	if guest == baseline {
		return fmt.Errorf("not isolated: the sandbox shares kernel %s.\n"+
			"This is a container, not a microVM", baseline)
	}
	fmt.Printf("isolated: sandbox %s, outside %s\n", guest, baseline)
	return nil
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use: "run <name> [command...]", Short: "one command, fresh microVM", GroupID: groupRun,
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
		Use: "shell <name>", Short: "interactive session", GroupID: groupRun,
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
		Use: "reset <name>", Short: "empty /data", GroupID: groupState,
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
		Use: "snapshot <name>", Short: "archive /data", GroupID: groupState,
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
		Use: "env <name>", Short: "settings as KEY=VALUE", GroupID: groupInspect,
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
			fmt.Printf("BLUEBOX_PASST=%t\n", s.Passt)
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
		Use: "logs <name>", Short: "recent runs", GroupID: groupInspect,
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
		Use: "rename <old> <new>", Short: "rename, keeping data", GroupID: groupRemove,
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
		Use: "destroy <name>", Short: "remove one sandbox", GroupID: groupRemove,
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
		Use: "nuke", Short: "remove every sandbox", GroupID: groupRemove,
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
		Use: "ls", Short: "list sandboxes", GroupID: groupSandbox,
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

// editorArgv resolves the user's editor. Fields are split so "code --wait"
// works, not just a bare command name.
func editorArgv() []string {
	for _, k := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return strings.Fields(v)
		}
	}
	return []string{"vi"}
}

func openEditor(path string) error {
	argv := editorArgv()
	cmd := exec.Command(argv[0], append(argv[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	return nil
}

// chooseTarget asks which file to open. Reaching EOF without a line means
// nobody is there to answer -- /dev/null is a character device, so testing for
// a terminal would wrongly accept it -- and guessing between two files that
// behave this differently is worse than refusing.
func chooseTarget() (bluefile bool, err error) {
	fmt.Println("  1  Bluefile       the spec you edit")
	fmt.Println("  2  Containerfile  generated, replaced by the next build")
	fmt.Print("  choose [1]: ")
	line, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
	if readErr != nil && strings.TrimSpace(line) == "" {
		fmt.Println()
		return false, fmt.Errorf("no answer; pass -b for the Bluefile or -c for the Containerfile")
	}
	switch strings.TrimSpace(line) {
	case "", "1", "b", "bluefile":
		return true, nil
	case "2", "c", "containerfile":
		return false, nil
	default:
		return false, fmt.Errorf("choose 1 or 2")
	}
}

func editCmd() *cobra.Command {
	var wantBlue, wantContainer bool
	c := &cobra.Command{
		Use: "edit <name>", Short: "edit the Bluefile or Containerfile", GroupID: groupSandbox,
		Long: "Opens a sandbox's file in $VISUAL, $EDITOR, or vi.\n\n" +
			"The Containerfile is generated from the Bluefile, so edits to it are\n" +
			"replaced by the next build. Change the Bluefile to make them stick.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if !sandbox.Exists(name) {
				return fmt.Errorf("no sandbox %q", name)
			}
			if wantBlue && wantContainer {
				return fmt.Errorf("pass -b or -c, not both")
			}

			editBluefile := wantBlue
			if !wantBlue && !wantContainer {
				chosen, err := chooseTarget()
				if err != nil {
					return err
				}
				editBluefile = chosen
			}

			if editBluefile {
				path, err := sandbox.BluefilePath(name)
				if err != nil {
					return err
				}
				if err := openEditor(path); err != nil {
					return err
				}
				// Catch a broken edit now rather than at the next build.
				if _, err := bluefile.Parse(path); err != nil {
					return fmt.Errorf("saved, but it no longer parses:\n%w", err)
				}
				fmt.Printf("ok — run: bluebox build %s\n", name)
				return nil
			}

			path, err := sandbox.ContainerfilePath(name)
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("no Containerfile yet; it is written by: bluebox build %s", name)
			}
			fmt.Println("note: generated file — the next build overwrites it")
			return openEditor(path)
		},
	}
	c.Flags().BoolVarP(&wantBlue, "bluefile", "b", false, "edit the Bluefile")
	c.Flags().BoolVarP(&wantContainer, "containerfile", "c", false, "edit the generated Containerfile")
	return c
}
