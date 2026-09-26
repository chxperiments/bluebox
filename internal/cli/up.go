package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"bluebox/internal/agent"
	"bluebox/internal/runtime"
	"bluebox/internal/sandbox"
	"bluebox/internal/server"
)

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use: "up <name>", Short: "boot once, keep running", GroupID: groupRun,
		Long: "Boots the sandbox's microVM and leaves it running, so bluebox exec\n" +
			"reaches it in milliseconds instead of booting a VM per command.\n" +
			"State outside /data lasts until bluebox down.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeName,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			s, err := loadSpec(name)
			if err != nil {
				return err
			}
			took, err := runtime.UpChecked(name, s, notice)
			if err != nil {
				return err
			}
			fmt.Printf("up %s (ready in %.2fs)\n", name, took.Seconds())
			return nil
		},
	}
}

func execCmd() *cobra.Command {
	var interactive bool
	c := &cobra.Command{
		Use: "exec <name> [command...]", Short: "run in the running VM", GroupID: groupRun,
		Long: "Runs one command in a sandbox brought up with bluebox up. Nothing is\n" +
			"booted, so it starts in milliseconds and sees what earlier commands\n" +
			"left behind. Exit codes pass through; timeout_seconds applies per\n" +
			"command and exits 124.\n\n" +
			"Piped stdin is forwarded. A terminal is not, unless -i is given.",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeRunArgs,
		Example: "  bluebox up devbox\n" +
			"  bluebox exec devbox -- pip install requests\n" +
			"  echo 'print(1)' | bluebox exec devbox -- python3 -",
		RunE: func(_ *cobra.Command, args []string) error {
			name, argv := args[0], args[1:]
			s, err := loadSpec(name)
			if err != nil {
				return err
			}
			// Forwarding a terminal would swallow what the user types next
			// for a command that never reads it, so a tty needs -i.
			streams := runtime.Terminal()
			if fi, err := os.Stdin.Stat(); interactive || (err == nil && fi.Mode()&os.ModeCharDevice == 0) {
				streams.Stdin = os.Stdin
			}
			err = runtime.Exec(name, s, argv, streams)
			switch {
			case err == nil:
				return nil
			case errors.Is(err, runtime.ErrNotUp):
				return fmt.Errorf("%s is not up; start it: bluebox up %s", name, name)
			case err == runtime.ErrTimeout:
				exitCode = runtime.ExitTimeout
				return fmt.Errorf("killed after %ds (timeout_seconds)", s.TimeoutSeconds)
			default:
				if code := runtime.ExitCode(err); code >= 0 {
					exitCode = code
					return nil
				}
				return err
			}
		},
	}
	c.Flags().BoolVarP(&interactive, "interactive", "i", false, "forward stdin even from a terminal")
	return c
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use: "down <name>", Short: "stop the running VM", GroupID: groupRun,
		Long: "Stops a sandbox brought up with bluebox up. Everything outside /data\n" +
			"is discarded with the VM.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeName,
		RunE: func(_ *cobra.Command, args []string) error {
			if !sandbox.Exists(args[0]) {
				return fmt.Errorf("no sandbox %q", args[0])
			}
			if err := runtime.Down(args[0]); err != nil {
				return err
			}
			fmt.Printf("down %s\n", args[0])
			return nil
		},
	}
}

// agentCmd is the guest side of up/exec. It is hidden: it only makes sense as
// the main process of a microVM that bluebox up started.
func agentCmd() *cobra.Command {
	return &cobra.Command{
		Use: "__agent", Hidden: true,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return agent.Serve() },
	}
}

func serveCmd() *cobra.Command {
	var sock string
	var idle time.Duration
	c := &cobra.Command{
		Use: "serve", Short: "local API for the SDKs", GroupID: groupRun,
		Long: "Serves the bluebox API on a Unix socket only you can open, for the\n" +
			"Python and Go SDKs. The SDKs start it themselves when it is not\n" +
			"running, with an idle limit so it exits once unused.",
		Args:              cobra.NoArgs,
		ValidArgsFunction: completeNothing,
		RunE: func(*cobra.Command, []string) error {
			if sock == "" {
				p, err := sandbox.SocketPath()
				if err != nil {
					return err
				}
				sock = p
			}
			return server.Serve(sock, Version, idle)
		},
	}
	c.Flags().StringVar(&sock, "socket", "", "socket path (default ~/.bluebox/bluebox.sock)")
	c.Flags().DurationVar(&idle, "idle", 0, "exit after this long without a request (0 = never)")
	return c
}

// tendCmd tops up a sandbox's warm pool. It is started detached by run and
// build, never by hand, and reports failures to the sandbox's log.
func tendCmd() *cobra.Command {
	return &cobra.Command{
		Use: "__tend <name>", Hidden: true,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNothing,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := runtime.Tend(args[0]); err != nil {
				runtime.TendLog(args[0], err)
				return err
			}
			return nil
		},
	}
}

// refuseWhileUp guards commands that swap /data wholesale: a running VM holds
// the old directory mounted and would carry on writing to it unseen.
func refuseWhileUp(name, what string) error {
	if sandbox.IsUp(name) {
		return fmt.Errorf("%s is up; bring it down before you %s: bluebox down %s", name, what, name)
	}
	return nil
}
