// Package runtime drives podman + the krun runtime. It is the only place that
// knows how a Bluefile spec becomes VM isolation, so swapping the backend later
// means touching this file alone.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"bluebox/internal/bluefile"
	"bluebox/internal/sandbox"
)

// ExitTimeout matches timeout(1) so a harness can tell "killed on time" from a
// normal non-zero exit.
const ExitTimeout = 124

// ErrTimeout is returned by Run when the wall-clock limit is hit.
var ErrTimeout = errors.New("timed out")

// Preflight fails loudly rather than letting podman silently fall back to a
// plain container, which would look identical but share the host kernel.
func Preflight() error {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("/dev/kvm not available -- microVMs need KVM on this host")
	}
	if _, err := exec.LookPath("krun"); err != nil {
		return fmt.Errorf("no 'krun' on PATH. It is a symlink to crun:\n" +
			"  sudo ln -sf $(command -v crun) /usr/local/bin/krun\n" +
			"and libkrun must be installed (Fedora: sudo dnf install libkrun)")
	}
	return nil
}

// vmArgs builds the podman invocation. All isolation lives in these flags, so
// they are constructed in exactly one place.
func vmArgs(name string, s bluefile.Spec, interactive bool) ([]string, error) {
	data, err := sandbox.DataDir(name)
	if err != nil {
		return nil, err
	}
	args := []string{
		"run", "--rm",
		"--runtime", "krun",
		"--network=" + s.Network,
		"--annotation", "krun.cpus=" + strconv.Itoa(s.CPUs),
		"--annotation", "krun.ram_mib=" + strconv.Itoa(s.RAMMiB),
		"-v", data + ":/data",
	}
	if s.ReadOnlyRootfs {
		args = append(args, "--read-only")
	}
	if s.Seccomp != "" {
		args = append(args, "--security-opt", "seccomp="+s.Seccomp)
	}
	if interactive {
		args = append(args, "-i")
		// Only request a TTY when we have one; -t with piped stdin hangs.
		if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			args = append(args, "-t")
		}
	}
	return args, nil
}

// Build renders the Containerfile from the spec, writes it, and builds the image.
func Build(name string, s bluefile.Spec) error {
	cfPath, err := sandbox.ContainerfilePath(name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfPath, []byte(s.Containerfile()), 0o644); err != nil {
		return err
	}
	dir, err := sandbox.Dir(name)
	if err != nil {
		return err
	}
	return stream(exec.Command("podman", "build", "-t", sandbox.ImageTag(name), dir))
}

// Run executes argv in a fresh microVM. A timed-out run is reaped explicitly:
// killing the podman CLI does not stop the VM it started, so --rm never fires.
func Run(name string, s bluefile.Spec, argv []string) error {
	base, err := vmArgs(name, s, false)
	if err != nil {
		return err
	}
	runName := fmt.Sprintf("bluebox-%s-%d", name, os.Getpid())
	args := append(base, "--name", runName, sandbox.ImageTag(name))
	args = append(args, argv...)

	ctx := context.Background()
	if s.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(s.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	err = streamCtx(ctx, exec.CommandContext(ctx, "podman", args...))
	if ctx.Err() == context.DeadlineExceeded {
		exec.Command("podman", "rm", "-f", runName).Run()
		return ErrTimeout
	}
	return err
}

// Shell opens an interactive session in one microVM. No timeout: the user is it.
func Shell(name string, s bluefile.Spec) error {
	base, err := vmArgs(name, s, true)
	if err != nil {
		return err
	}
	args := append(base, sandbox.ImageTag(name), "/bin/sh", "-l")
	cmd := exec.Command("podman", args...)
	cmd.Stdin = os.Stdin
	return stream(cmd)
}

// GuestKernel returns the kernel reported from inside the microVM.
func GuestKernel(name string, s bluefile.Spec) (string, error) {
	base, err := vmArgs(name, s, false)
	if err != nil {
		return "", err
	}
	args := append(base, sandbox.ImageTag(name), "uname", "-r")
	out, err := exec.Command("podman", args...).Output()
	return strings.TrimSpace(string(out)), err
}

func HostKernel() (string, error) {
	out, err := exec.Command("uname", "-r").Output()
	return strings.TrimSpace(string(out)), err
}

func stream(cmd *exec.Cmd) error { return streamCtx(context.Background(), cmd) }

func streamCtx(_ context.Context, cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExitCode extracts a child process exit code from a Run/Shell error, or -1.
func ExitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
