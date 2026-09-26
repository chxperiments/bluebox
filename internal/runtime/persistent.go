package runtime

import (
	"bytes"
	"crypto/rand"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
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

	"bluebox/internal/agent"
	"bluebox/internal/bluefile"
	"bluebox/internal/sandbox"
)

// ErrNotUp is returned by Exec for a sandbox with no running VM.
var ErrNotUp = errors.New("not up")

// errNotIsolated marks a guest that answered with the host's kernel.
var errNotIsolated = errors.New("not isolated")

// ExitStatus is a command's non-zero exit from a running sandbox. It is the
// sandbox's result rather than bluebox failing, so ExitCode unwraps it.
type ExitStatus struct{ Code int }

func (e *ExitStatus) Error() string { return "exit status " + strconv.Itoa(e.Code) }

// agentMount is where the agent directory appears inside the guest.
const agentMount = "/.bluebox"

// upState is what `up` leaves behind for `exec` and `down`. It holds a
// secret, so it is written owner-only.
type upState struct {
	Container string `json:"container"`
	Addr      string `json:"addr"`
	Token     string `json:"token"`
	Kernel    string `json:"kernel"`
	Started   string `json:"started"`
}

func upContainer(name string) string { return "bluebox-up-" + name }

func loadUp(name string) (upState, error) {
	p, err := sandbox.RunStatePath(name)
	if err != nil {
		return upState{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return upState{}, ErrNotUp
	}
	var st upState
	if err := json.Unmarshal(b, &st); err != nil {
		return upState{}, fmt.Errorf("unreadable run state %s: %w", p, err)
	}
	return st, nil
}

func saveUp(name string, st upState) error {
	p, err := sandbox.RunStatePath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// installAgent copies this very binary to where running sandboxes mount it.
// The guest may be any distro, so the binary must not depend on the host's
// libc: a dynamically linked build is refused with the command that fixes it
// rather than failing obscurely inside the VM.
func installAgent() (string, error) {
	if goruntime.GOOS != "linux" {
		// The guest needs a Linux binary; on macOS this one is Darwin.
		return "", fmt.Errorf("bluebox up is Linux-only for now; use bluebox run")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	f, err := elf.Open(exe)
	if err != nil {
		return "", fmt.Errorf("cannot inspect %s: %w", exe, err)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			f.Close()
			return "", fmt.Errorf("this bluebox is dynamically linked, so it cannot run inside the guest.\n" +
				"Rebuild it static (release binaries already are):\n" +
				"  CGO_ENABLED=0 go build -o bluebox ./cmd/bluebox")
		}
	}
	f.Close()

	src, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	dir, err := sandbox.AgentDir()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "bluebox")
	// Running VMs have the old copy mounted; leave it alone when it is
	// already this binary, and otherwise swap it by rename so they keep the
	// file they opened.
	if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, src) {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := dst + ".partial"
	if err := os.WriteFile(tmp, src, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dir, nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// kernelGate refuses a guest that reports the host's kernel: that is a plain
// container, whatever it was asked to be. It is the same boundary check
// `run` relies on, made against the live VM on every command.
func kernelGate(host string) func(string) error {
	return func(guest string) error {
		if guest == "" || guest == host {
			return fmt.Errorf("%w: the sandbox reports kernel %q, the host's own.\n"+
				"This is a container, not a microVM; refusing to run", errNotIsolated, guest)
		}
		return nil
	}
}

// Up boots a sandbox once and leaves it running with the agent as its main
// process, so later commands cost a connection rather than a boot. It returns
// how long the VM took to answer.
func Up(name string, s bluefile.Spec) (time.Duration, error) {
	if s.Network == "none" {
		// The agent is reached through a published port, and podman will not
		// publish one on a sandbox with no network.
		return 0, fmt.Errorf("bluebox up needs a network to reach its agent; %s has network: none.\n"+
			"Use bluebox run for offline sandboxes", name)
	}
	if st, err := loadUp(name); err == nil {
		if ping(st) == nil {
			return 0, fmt.Errorf("%s is already up", name)
		}
		Down(name) // stale: the VM died or the host rebooted
	}
	started := time.Now()
	st, err := boot(name, s, upContainer(name))
	if err != nil {
		return 0, err
	}
	if err := saveUp(name, st); err != nil {
		removeContainer(st.Container)
		return 0, err
	}
	return time.Since(started), nil
}

func removeContainer(ctr string) {
	exec.Command("podman", "rm", "-f", "-t", "0", ctr).Run()
}

// boot starts a VM named ctr with the agent as its main process and waits
// until the agent answers from a kernel that is not the host's. A VM that
// never answers, or answers from the host kernel, is torn down rather than
// left running.
func boot(name string, s bluefile.Spec, ctr string, extra ...string) (upState, error) {
	agentDir, err := installAgent()
	if err != nil {
		return upState{}, err
	}
	token, err := newToken()
	if err != nil {
		return upState{}, err
	}
	host, err := HostKernel()
	if err != nil {
		return upState{}, err
	}
	removeContainer(ctr)

	base, err := vmArgs(name, s, false, true)
	if err != nil {
		return upState{}, err
	}
	args := append(base, extra...)
	args = append(args,
		"-d", "--name", ctr,
		"-p", fmt.Sprintf("127.0.0.1::%d", agent.Port),
		"-v", agentDir+":"+agentMount+":ro",
		// Name only: podman copies the value from its own environment, so
		// the token never appears in an argv that any host user can read
		// from ps.
		"-e", agent.TokenEnv,
		"--entrypoint", agentMount+"/bluebox",
		sandbox.ImageTag(name), "__agent",
	)
	started := time.Now()
	cmd := exec.Command("podman", args...)
	cmd.Env = append(os.Environ(), agent.TokenEnv+"="+token)
	if out, err := cmd.CombinedOutput(); err != nil {
		return upState{}, fmt.Errorf("podman: %s", strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("podman", "port", ctr, fmt.Sprintf("%d/tcp", agent.Port)).Output()
	if err != nil {
		removeContainer(ctr)
		return upState{}, fmt.Errorf("could not find the agent's port: %w", err)
	}
	st := upState{
		Container: ctr,
		Addr:      strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]),
		Token:     token,
		Started:   started.UTC().Format(time.RFC3339),
	}

	deadline := started.Add(30 * time.Second)
	for {
		res, err := agent.Exec(st.Addr, agent.Request{Token: token}, kernelGate(host), nil, io.Discard, io.Discard)
		if err == nil {
			st.Kernel = res.Kernel
			return st, nil
		}
		if errors.Is(err, errNotIsolated) {
			removeContainer(ctr)
			return upState{}, err
		}
		if time.Now().After(deadline) {
			logs, _ := exec.Command("podman", "logs", ctr).CombinedOutput()
			removeContainer(ctr)
			return upState{}, fmt.Errorf("the agent did not answer within 30s: %v\n%s", err,
				strings.TrimSpace(string(logs)))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func ping(st upState) error {
	_, err := agent.Exec(st.Addr, agent.Request{Token: st.Token}, nil, nil, io.Discard, io.Discard)
	return err
}

// Exec runs argv in the sandbox's running VM. Unlike Run nothing is booted, so
// state -- files outside /data, background processes -- carries from one
// command to the next, exactly as in a shell session.
func Exec(name string, s bluefile.Spec, argv []string, streams Streams) error {
	st, err := loadUp(name)
	if err != nil {
		return err
	}
	err = execOn(name, st, s, "exec", argv, streams)
	if errors.Is(err, errLost) {
		return fmt.Errorf("%s is not responding (%v); restart it: bluebox down %s && bluebox up %s",
			name, err, name, name)
	}
	return err
}

// errLost marks an agent that could not be reached or dropped the session.
var errLost = errors.New("agent unreachable")

// execOn runs argv through the agent of a running VM, with output streamed
// and logged the way Run does, and every command gated on the guest kernel.
func execOn(name string, st upState, s bluefile.Spec, verb string, argv []string, streams Streams) error {
	host, err := HostKernel()
	if err != nil {
		return err
	}
	log := openLog(name)
	if log != nil {
		defer log.Close()
		fmt.Fprintf(log, "\n=== %s %s: %s\n",
			time.Now().UTC().Format(time.RFC3339), verb, strings.Join(argv, " "))
	}
	out, errw := tee(streams, log)

	started := time.Now()
	res, err := agent.Exec(st.Addr, agent.Request{
		Token:          st.Token,
		Argv:           argv,
		TimeoutSeconds: s.TimeoutSeconds,
	}, kernelGate(host), streams.Stdin, out, errw)
	if log != nil {
		code := res.Code
		if err != nil {
			code = -1
		}
		fmt.Fprintf(log, "=== exit %d (%.3fs)\n", code, time.Since(started).Seconds())
	}
	switch {
	case errors.Is(err, errNotIsolated):
		return err
	case err != nil:
		return fmt.Errorf("%w: %v", errLost, err)
	case res.TimedOut:
		return ErrTimeout
	case res.Code != 0:
		return &ExitStatus{Code: res.Code}
	}
	return nil
}

// Down stops a sandbox's running VM and forgets it. Stopping one that is not
// running is not an error: the goal is that it is down.
func Down(name string) error {
	p, err := sandbox.RunStatePath(name)
	if err != nil {
		return err
	}
	removeContainer(upContainer(name))
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
