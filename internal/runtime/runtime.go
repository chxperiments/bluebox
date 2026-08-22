// Package runtime drives podman + the krun runtime. It is the only place that
// knows how a Bluefile spec becomes VM isolation, so swapping the backend later
// means touching this file alone.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
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
// plain container, which would look identical but share a kernel.
func Preflight() error {
	if _, err := exec.LookPath("podman"); err != nil {
		return fmt.Errorf("podman not found on PATH")
	}
	if goruntime.GOOS == "darwin" {
		// On macOS every container already runs inside the podman machine VM,
		// so KVM and the krun runtime live in there, not out here. All we can
		// check from this side is that the machine is up; whether it can
		// actually nest a microVM is settled by Verify.
		out, err := exec.Command("podman", "machine", "list", "--format", "{{.Running}}").Output()
		if err != nil || !strings.Contains(string(out), "true") {
			return fmt.Errorf("no podman machine is running:\n" +
				"  podman machine init && podman machine start")
		}
		return nil
	}
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
func vmArgs(name string, s bluefile.Spec, interactive, useKrun bool) ([]string, error) {
	data, err := sandbox.DataDir(name)
	if err != nil {
		return nil, err
	}
	args := []string{"run", "--rm"}
	if useKrun {
		args = append(args, "--runtime", "krun")
	}
	args = append(args,
		"--network="+s.Network,
		"--annotation", "krun.cpus="+strconv.Itoa(s.CPUs),
		"--annotation", "krun.ram_mib="+strconv.Itoa(s.RAMMiB),
		"-v", data+":/data",
	)
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
	dir, err := sandbox.Dir(name)
	if err != nil {
		return err
	}
	cf, err := s.Render(dir) // materialises any blueprint files first
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfPath, []byte(cf), 0o644); err != nil {
		return err
	}
	return stream(exec.Command("podman", "build", "-t", sandbox.ImageTag(name), dir))
}

// RemoveImage deletes a sandbox's built image, if there is one.
func RemoveImage(name string) {
	exec.Command("podman", "rmi", "-f", sandbox.ImageTag(name)).Run()
}

// RetagImage moves a built image to a new name so a rename does not force a
// rebuild. A missing image is not an error: the sandbox may never have built.
func RetagImage(from, to string) {
	if err := exec.Command("podman", "tag",
		sandbox.ImageTag(from), sandbox.ImageTag(to)).Run(); err == nil {
		exec.Command("podman", "rmi", "-f", sandbox.ImageTag(from)).Run()
	}
}

// Run executes argv in a fresh microVM. A timed-out run is reaped explicitly:
// killing the podman CLI does not stop the VM it started, so --rm never fires.
func Run(name string, s bluefile.Spec, argv []string) error {
	base, err := vmArgs(name, s, false, true)
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

	log := openLog(name)
	if log != nil {
		defer log.Close()
		fmt.Fprintf(log, "\n=== %s run: %s\n",
			time.Now().UTC().Format(time.RFC3339), strings.Join(argv, " "))
	}
	started := time.Now()
	cmd := exec.CommandContext(ctx, "podman", args...)
	err = streamTee(cmd, log)
	if log != nil {
		code := 0
		if err != nil {
			code = ExitCode(err)
		}
		fmt.Fprintf(log, "=== exit %d (%.1fs)\n", code, time.Since(started).Seconds())
	}

	if ctx.Err() == context.DeadlineExceeded {
		exec.Command("podman", "rm", "-f", runName).Run()
		return ErrTimeout
	}
	return err
}

// openLog returns an append handle for the sandbox's run log, or nil if it
// cannot be opened. Logging is best-effort: it must never break a run.
func openLog(name string) *os.File {
	p, err := sandbox.LogPath(name)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	return f
}

// Shell opens an interactive session in one microVM. No timeout: the user is it.
func Shell(name string, s bluefile.Spec) error {
	base, err := vmArgs(name, s, true, true)
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
	return kernelOf(name, s, true)
}

// BaselineKernel returns the kernel a plain container sees: the host kernel on
// Linux, the podman machine's kernel on macOS. Comparing against this rather
// than the host's own uname is what makes the isolation check honest on macOS,
// where the host runs Darwin and every container kernel differs from it
// whether or not a microVM is involved.
func BaselineKernel(name string, s bluefile.Spec) (string, error) {
	return kernelOf(name, s, false)
}

func kernelOf(name string, s bluefile.Spec, useKrun bool) (string, error) {
	base, err := vmArgs(name, s, false, useKrun)
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

// stream runs cmd with its stdout/stderr wired to the process. Any timeout is
// bound into the command via exec.CommandContext before it reaches here.
func stream(cmd *exec.Cmd) error { return streamTee(cmd, nil) }

// streamTee is stream, additionally copying output to log when non-nil.
func streamTee(cmd *exec.Cmd, log io.Writer) error {
	out, errw := io.Writer(os.Stdout), io.Writer(os.Stderr)
	if log != nil {
		out = io.MultiWriter(os.Stdout, log)
		errw = io.MultiWriter(os.Stderr, log)
	}
	cmd.Stdout = out
	cmd.Stderr = errw
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
