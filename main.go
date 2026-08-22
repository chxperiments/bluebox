// bluebox: disposable microVM sandboxes for AI harnesses.
//
// Each sandbox is an OCI image run as a real microVM via podman + the krun
// runtime (libkrun). Every command gets a fresh VM; only /data survives.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	CPUs      int    `json:"cpus"`
	MemoryMiB int    `json:"memory_mib"`
	Network   string `json:"network"` // "bridge" for egress, "none" to cut it off
}

var defaultConfig = Config{CPUs: 2, MemoryMiB: 2048, Network: "bridge"}

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

func podman(args []string, stdin bool) error {
	cmd := exec.Command("podman", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if stdin {
		cmd.Stdin = os.Stdin
	}
	return cmd.Run()
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
	if err := podman([]string{"build", "-t", imageTag(name), dir}, false); err != nil {
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
	args := append(vmArgs(name, cfg, false), imageTag(name))
	args = append(args, argv...)
	if err := podman(args, false); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		die("%v", err)
	}
}

func cmdShell(name string) {
	preflight()
	cfg := loadConfig(name)
	args := append(vmArgs(name, cfg, true), imageTag(name), "/bin/sh", "-l")
	if err := podman(args, true); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
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
	fmt.Printf("%-16s %-6s %-8s %-8s %s\n", "NAME", "CPUS", "MEM", "NET", "DATA")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cfg := loadConfig(e.Name())
		fmt.Printf("%-16s %-6d %-8s %-8s %s\n", e.Name(), cfg.CPUs,
			strconv.Itoa(cfg.MemoryMiB)+"M", cfg.Network, dataDir(e.Name()))
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
