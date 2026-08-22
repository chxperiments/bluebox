// bluebox: disposable microVM sandboxes for AI harnesses.
//
// Each sandbox is an OCI image run as a real microVM via podman + the krun
// runtime (libkrun). Every command gets a fresh VM; only /data survives.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	CPUs      int    `json:"cpus"`
	MemoryMiB int    `json:"memory_mib"`
	Network   string `json:"network"` // "bridge" for egress, "none" to cut it off

	// Guest rootfs read-only. podman still provides a writable /tmp.
	ReadOnlyRootfs bool `json:"readonly_rootfs"`
	// Wall-clock limit per run; 0 disables. Runaway commands exit 124.
	TimeoutSeconds int `json:"timeout_seconds"`
	// Path to a seccomp profile. NOTE: this filters the VMM process on the
	// host, not the workload in the guest -- see README.
	Seccomp string `json:"seccomp"`
}

var defaultConfig = Config{CPUs: 2, MemoryMiB: 2048, Network: "bridge"}

// exitTimeout matches timeout(1) so a harness can tell "killed on time" from
// an ordinary non-zero exit.
const exitTimeout = 124

const containerfileTemplate = `FROM docker.io/library/alpine:latest

# Tools this sandbox needs. Everything here is baked into the image and is
# reset on every run -- only /data persists.
RUN apk add --no-cache bash curl

WORKDIR /data
`

func home() string {
	if h := os.Getenv("BLUEBOX_HOME"); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err != nil {
		die("cannot determine home directory: %v", err)
	}
	return filepath.Join(h, ".bluebox")
}

func sandboxDir(name string) string { return filepath.Join(home(), "sandboxes", name) }
func dataDir(name string) string    { return filepath.Join(home(), "data", name) }
func imageTag(name string) string   { return "bluebox/" + name + ":latest" }

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "bluebox: "+format+"\n", a...)
	os.Exit(1)
}

func loadConfig(name string) Config {
	path := filepath.Join(sandboxDir(name), "config.json")
	b, err := os.ReadFile(path)
	if err != nil {
		die("no sandbox %q (looked for %s). Run: bluebox new %s", name, path, name)
	}
	cfg := defaultConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		die("bad config %s: %v", path, err)
	}
	if cfg.CPUs < 1 || cfg.CPUs > 16 {
		die("cpus must be 1-16 (krun's limit), got %d", cfg.CPUs)
	}
	if cfg.Seccomp != "" {
		if _, err := os.Stat(cfg.Seccomp); err != nil {
			die("seccomp profile %s: %v", cfg.Seccomp, err)
		}
	}
	return cfg
}

// preflight fails loudly rather than silently falling back to a plain
// container, which would look identical but share the host kernel.
func preflight() {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		die("/dev/kvm not available -- microVMs need KVM on this host")
	}
	if _, err := exec.LookPath("krun"); err != nil {
		die("no 'krun' on PATH. It is a symlink to crun:\n" +
			"  sudo ln -sf $(command -v crun) /usr/local/bin/krun\n" +
			"and libkrun must be installed (Fedora: sudo dnf install libkrun)")
	}
}

// vmArgs builds the podman invocation. Isolation lives entirely in these
// flags, so they are constructed in one place only.
func vmArgs(name string, cfg Config, interactive bool) []string {
	args := []string{
		"run", "--rm",
		"--runtime", "krun",
		"--network=" + cfg.Network,
		"--annotation", "krun.cpus=" + strconv.Itoa(cfg.CPUs),
		"--annotation", "krun.ram_mib=" + strconv.Itoa(cfg.MemoryMiB),
		"-v", dataDir(name) + ":/data",
	}
	if cfg.ReadOnlyRootfs {
		args = append(args, "--read-only")
	}
	if cfg.Seccomp != "" {
		args = append(args, "--security-opt", "seccomp="+cfg.Seccomp)
	}
	if interactive {
		args = append(args, "-i")
		// Only ask podman for a TTY when we actually have one; passing -t
		// with piped stdin hangs waiting on a terminal that never arrives.
		if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			args = append(args, "-t")
		}
	}
	return args
}

var errTimeout = errors.New("timed out")

func podman(args []string, stdin bool, timeout time.Duration) error {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if stdin {
		cmd.Stdin = os.Stdin
	}
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return errTimeout
	}
	return err
}

// guestKernel returns the kernel reported from inside the microVM.
func guestKernel(name string, cfg Config) (string, error) {
	args := append(vmArgs(name, cfg, false), imageTag(name), "uname", "-r")
	out, err := exec.Command("podman", args...).Output()
	return strings.TrimSpace(string(out)), err
}

func hostKernel() string {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		die("cannot read host kernel: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func cmdNew(name string) {
	dir := sandboxDir(name)
	if _, err := os.Stat(dir); err == nil {
		die("sandbox %q already exists at %s", name, dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		die("%v", err)
	}
	if err := os.MkdirAll(dataDir(name), 0o755); err != nil {
		die("%v", err)
	}
	cf := filepath.Join(dir, "Containerfile")
	if err := os.WriteFile(cf, []byte(containerfileTemplate), 0o644); err != nil {
		die("%v", err)
	}
	b, _ := json.MarshalIndent(defaultConfig, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), append(b, '\n'), 0o644); err != nil {
		die("%v", err)
	}
	fmt.Printf("created %s\n  edit %s, then: bluebox build %s\n", dir, cf, name)
}

func cmdBuild(name string) {
	dir := sandboxDir(name)
	if _, err := os.Stat(filepath.Join(dir, "Containerfile")); err != nil {
		die("no Containerfile in %s. Run: bluebox new %s", dir, name)
	}
	if err := podman([]string{"build", "-t", imageTag(name), dir}, false, 0); err != nil {
		die("build failed: %v", err)
	}
	// Verify once here rather than on every run: a sandbox that is not
	// actually a VM should never reach first use.
	fmt.Println("\nverifying microVM isolation...")
	cmdVerify(name)
}

func cmdVerify(name string) {
	preflight()
	cfg := loadConfig(name)
	host := hostKernel()
	guest, err := guestKernel(name, cfg)
	if err != nil || guest == "" {
		die("could not read guest kernel (is the image built?): %v", err)
	}
	fmt.Printf("  host kernel:  %s\n  guest kernel: %s\n", host, guest)
	if guest == host {
		die("NOT ISOLATED: guest and host share a kernel. This is a container,\n" +
			"not a microVM -- do not run untrusted code in it.")
	}
	fmt.Println("  OK: separate kernel, genuine microVM.")
}

func cmdRun(name string, argv []string) {
	preflight()
	cfg := loadConfig(name)
	// Name the container so a timed-out run can be reaped. Killing the podman
	// CLI does not stop the VM it started, so --rm never fires.
	runName := fmt.Sprintf("bluebox-%s-%d", name, os.Getpid())
	args := append(vmArgs(name, cfg, false), "--name", runName, imageTag(name))
	args = append(args, argv...)
	err := podman(args, false, time.Duration(cfg.TimeoutSeconds)*time.Second)
	if errors.Is(err, errTimeout) {
		exec.Command("podman", "rm", "-f", runName).Run()
		fmt.Fprintf(os.Stderr, "bluebox: killed after %ds (timeout_seconds)\n", cfg.TimeoutSeconds)
		os.Exit(exitTimeout)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		die("%v", err)
	}
}

func cmdShell(name string) {
	preflight()
	cfg := loadConfig(name)
	// No timeout on an interactive session; the user is the timeout.
	args := append(vmArgs(name, cfg, true), imageTag(name), "/bin/sh", "-l")
	if err := podman(args, true, 0); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		die("%v", err)
	}
}

func cmdList() {
	entries, err := os.ReadDir(filepath.Join(home(), "sandboxes"))
	if err != nil {
		fmt.Println("no sandboxes yet. Create one: bluebox new <name>")
		return
	}
	fmt.Printf("%-14s %-5s %-7s %-7s %-6s %-8s %s\n",
		"NAME", "CPUS", "MEM", "NET", "RO", "TIMEOUT", "DATA")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cfg := loadConfig(e.Name())
		timeout := "-"
		if cfg.TimeoutSeconds > 0 {
			timeout = strconv.Itoa(cfg.TimeoutSeconds) + "s"
		}
		fmt.Printf("%-14s %-5d %-7s %-7s %-6t %-8s %s\n", e.Name(), cfg.CPUs,
			strconv.Itoa(cfg.MemoryMiB)+"M", cfg.Network, cfg.ReadOnlyRootfs,
			timeout, dataDir(e.Name()))
	}
}

const usage = `bluebox -- disposable microVM sandboxes

  bluebox new <name>          scaffold a sandbox (Containerfile + config.json)
  bluebox build <name>        build its image, then verify isolation
  bluebox run <name> [cmd...] run a command in a fresh microVM
  bluebox shell <name>        interactive shell in a fresh microVM
  bluebox verify <name>       prove the sandbox has its own kernel
  bluebox ls                  list sandboxes

Every run is a NEW microVM. Only /data persists between runs.`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	need := func() string {
		if len(args) < 1 {
			die("%s needs a sandbox name", cmd)
		}
		return args[0]
	}

	switch cmd {
	case "new":
		cmdNew(need())
	case "build":
		cmdBuild(need())
	case "verify":
		cmdVerify(need())
	case "run":
		name := need()
		rest := args[1:]
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			die("run needs a command, e.g. bluebox run %s -- nmap -sV target", name)
		}
		cmdRun(name, rest)
	case "shell":
		cmdShell(need())
	case "ls":
		cmdList()
	default:
		fmt.Println(usage)
		os.Exit(2)
	}
}
