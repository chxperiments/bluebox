// Package cli wires the subcommands to the sandbox, bluefile, and runtime
// packages. main() only calls Run.
package cli

import (
	"fmt"
	"os"
	"strconv"

	"bluebox/internal/bluefile"
	"bluebox/internal/runtime"
	"bluebox/internal/sandbox"
)

const usage = `bluebox -- disposable microVM sandboxes

  bluebox new <name>          scaffold a sandbox (writes a Bluefile)
  bluebox build <name>        generate the Containerfile, build, verify isolation
  bluebox run <name> [cmd...] run a command in a fresh microVM
  bluebox shell <name>        interactive shell in a fresh microVM
  bluebox verify <name>       prove the sandbox has its own kernel
  bluebox ls                  list sandboxes

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
	if cmd != "new" && cmd != "build" && cmd != "verify" && cmd != "run" && cmd != "shell" {
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
			"container, not a microVM -- do not run untrusted code in it.")
	}
	fmt.Println("  OK: separate kernel, genuine microVM.")
	return 0
}

func cmdRun(name string, argv []string) int {
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return fail("run needs a command, e.g. bluebox run %s -- nmap -sV target", name)
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
