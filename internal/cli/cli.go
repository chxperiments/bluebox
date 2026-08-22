// Package cli wires the subcommands to the sandbox, bluefile, and runtime
// packages. main() only calls Run.
package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"bluebox/internal/bluefile"
	"bluebox/internal/runtime"
	"bluebox/internal/sandbox"
)

const usage = `bluebox -- disposable microVM sandboxes

  bluebox new <name>            scaffold a sandbox (writes a Bluefile)
  bluebox build <name>          generate the Containerfile, build, verify isolation
  bluebox run <name> [cmd...]   run a command in a fresh microVM
  bluebox shell <name>          interactive shell in a fresh microVM
  bluebox verify <name>         prove the sandbox has its own kernel
  bluebox ls                    list sandboxes
  bluebox env <name>            print the sandbox's effective settings as KEY=VALUE
  bluebox logs <name> [lines]   show recent runs (default 200 lines)
  bluebox rename <old> <new>    rename a sandbox, keeping its data
  bluebox destroy <name> [--data]  remove a sandbox; --data also deletes /data

A sandbox is defined by one Bluefile: base image, cpus, ram, network, and the
tools to install. Every run is a NEW microVM; only /data persists.`

// Run dispatches argv (excluding the program name) and returns a process exit code.
func Run(argv []string) int {
	if len(argv) < 1 {
		fmt.Println(usage)
		return 2
	}
	cmd, rest := argv[0], argv[1:]

	if cmd == "ls" {
		return cmdList()
	}
	// Every other command needs a sandbox name as its first argument.
	switch cmd {
	case "new", "build", "verify", "run", "shell", "env", "logs", "rename", "destroy":
	default:
		fmt.Println(usage)
		return 2
	}
	if len(rest) < 1 {
		fmt.Fprintf(os.Stderr, "bluebox: %s needs a sandbox name\n", cmd)
		return 2
	}
	name := rest[0]

	switch cmd {
	case "new":
		return cmdNew(name)
	case "build":
		return cmdBuild(name)
	case "verify":
		return cmdVerify(name)
	case "run":
		return cmdRun(name, rest[1:])
	case "shell":
		return cmdShell(name)
	case "env":
		return cmdEnv(name)
	case "logs":
		return cmdLogs(name, rest[1:])
	case "rename":
		return cmdRename(name, rest[1:])
	case "destroy":
		return cmdDestroy(name, rest[1:])
	}
	return 2 // unreachable: cmd is guarded above
}

func fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "bluebox: "+format+"\n", a...)
	return 1
}

func loadSpec(name string) (bluefile.Spec, bool) {
	if !sandbox.Exists(name) {
		fail("no sandbox %q. Create it: bluebox new %s", name, name)
		return bluefile.Spec{}, false
	}
	path, err := sandbox.BluefilePath(name)
	if err != nil {
		fail("%v", err)
		return bluefile.Spec{}, false
	}
	s, err := bluefile.Parse(path)
	if err != nil {
		fail("%v", err)
		return bluefile.Spec{}, false
	}
	return s, true
}

func cmdNew(name string) int {
	dir, err := sandbox.Create(name)
	if err != nil {
		return fail("%v", err)
	}
	path, _ := sandbox.BluefilePath(name)
	if err := os.WriteFile(path, []byte(bluefile.Template), 0o644); err != nil {
		return fail("%v", err)
	}
	fmt.Printf("created %s\n  edit %s, then: bluebox build %s\n", dir, path, name)
	return 0
}

func cmdBuild(name string) int {
	s, ok := loadSpec(name)
	if !ok {
		return 1
	}
	if err := runtime.Preflight(); err != nil {
		return fail("%v", err)
	}
	if err := runtime.Build(name, s); err != nil {
		return fail("build failed: %v", err)
	}
	// Verify once at build so a sandbox that is not really a VM never reaches
	// first use.
	fmt.Println("\nverifying microVM isolation...")
	return verify(name, s)
}

func cmdVerify(name string) int {
	s, ok := loadSpec(name)
	if !ok {
		return 1
	}
	if err := runtime.Preflight(); err != nil {
		return fail("%v", err)
	}
	return verify(name, s)
}

func verify(name string, s bluefile.Spec) int {
	host, err := runtime.HostKernel()
	if err != nil {
		return fail("cannot read host kernel: %v", err)
	}
	guest, err := runtime.GuestKernel(name, s)
	if err != nil || guest == "" {
		return fail("could not read guest kernel (is the image built?): %v", err)
	}
	fmt.Printf("  host kernel:  %s\n  guest kernel: %s\n", host, guest)
	if guest == host {
		return fail("NOT ISOLATED: guest and host share a kernel. This is a\n" +
			"container, not a microVM -- the isolation you expect is not there.")
	}
	fmt.Println("  OK: separate kernel, genuine microVM.")
	return 0
}

func cmdRun(name string, argv []string) int {
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return fail("run needs a command, e.g. bluebox run %s -- python3 script.py", name)
	}
	s, ok := loadSpec(name)
	if !ok {
		return 1
	}
	if err := runtime.Preflight(); err != nil {
		return fail("%v", err)
	}
	err := runtime.Run(name, s, argv)
	switch {
	case err == nil:
		return 0
	case err == runtime.ErrTimeout:
		fmt.Fprintf(os.Stderr, "bluebox: killed after %ds (TIMEOUT)\n", s.TimeoutSeconds)
		return runtime.ExitTimeout
	default:
		if code := runtime.ExitCode(err); code >= 0 {
			return code
		}
		return fail("%v", err)
	}
}

func cmdShell(name string) int {
	s, ok := loadSpec(name)
	if !ok {
		return 1
	}
	if err := runtime.Preflight(); err != nil {
		return fail("%v", err)
	}
	if err := runtime.Shell(name, s); err != nil {
		if code := runtime.ExitCode(err); code >= 0 {
			return code
		}
		return fail("%v", err)
	}
	return 0
}

// cmdEnv prints the effective settings as KEY=VALUE, so it can be consumed
// with `eval $(bluebox env <name>)`.
func cmdEnv(name string) int {
	s, ok := loadSpec(name)
	if !ok {
		return 1
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
	// The guest's own env, from the Bluefile, sorted for stable output.
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s=%s\n", k, s.Env[k])
	}
	return 0
}

func cmdLogs(name string, args []string) int {
	if !sandbox.Exists(name) {
		return fail("no sandbox %q", name)
	}
	lines := 200
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return fail("logs: line count must be a positive number, got %q", args[0])
		}
		lines = n
	}
	p, err := sandbox.LogPath(name)
	if err != nil {
		return fail("%v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		fmt.Printf("no runs logged yet for %q\n", name)
		return 0
	}
	all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	fmt.Println(strings.Join(all, "\n"))
	return 0
}

func cmdRename(from string, args []string) int {
	if len(args) < 1 {
		return fail("rename needs a new name, e.g. bluebox rename %s newname", from)
	}
	to := args[0]
	if err := sandbox.Rename(from, to); err != nil {
		return fail("%v", err)
	}
	// Retag rather than rebuild, so the image survives the rename.
	runtime.RetagImage(from, to)
	fmt.Printf("renamed %s -> %s (data and logs moved)\n", from, to)
	return 0
}

func cmdDestroy(name string, args []string) int {
	if !sandbox.Exists(name) {
		return fail("no sandbox %q", name)
	}
	withData := false
	for _, a := range args {
		if a != "--data" {
			return fail("destroy: unknown option %q (only --data)", a)
		}
		withData = true
	}
	data, _ := sandbox.DataDir(name)
	runtime.RemoveImage(name)
	if err := sandbox.Remove(name, withData); err != nil {
		return fail("%v", err)
	}
	if withData {
		fmt.Printf("destroyed %s, including %s\n", name, data)
	} else {
		fmt.Printf("destroyed %s. Data kept at %s (use --data to delete it too)\n", name, data)
	}
	return 0
}

func cmdList() int {
	names, err := sandbox.List()
	if err != nil || len(names) == 0 {
		fmt.Println("no sandboxes yet. Create one: bluebox new <name>")
		return 0
	}
	fmt.Printf("%-14s %-5s %-7s %-7s %-6s %-8s %s\n",
		"NAME", "CPUS", "RAM", "NET", "RO", "TIMEOUT", "BASE")
	for _, name := range names {
		path, _ := sandbox.BluefilePath(name)
		s, err := bluefile.Parse(path)
		if err != nil {
			fmt.Printf("%-14s <invalid Bluefile: %v>\n", name, err)
			continue
		}
		timeout := "-"
		if s.TimeoutSeconds > 0 {
			timeout = strconv.Itoa(s.TimeoutSeconds) + "s"
		}
		fmt.Printf("%-14s %-5d %-7s %-7s %-6t %-8s %s\n", name, s.CPUs,
			strconv.Itoa(s.RAMMiB)+"M", s.Network, s.ReadOnlyRootfs, timeout, s.Base)
	}
	return 0
}
