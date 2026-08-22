# bluebox

Disposable microVM sandboxes for AI harnesses. Describe a sandbox in one
**Bluefile** — base image, RAM, CPUs, network, and the tools to install — then
run commands inside a real KVM microVM that boots in about a second. Only
`/data` survives between runs.

Built on podman + the `krun` runtime (libkrun). bluebox is a thin wrapper: podman
does the image building, caching, volumes and networking. You never write a
Containerfile — bluebox generates it from the Bluefile.

## Project layout

```
cmd/bluebox/main.go        entrypoint (just calls internal/cli)
internal/bluefile/         Bluefile parser + Containerfile generator (unit-tested)
internal/sandbox/          on-disk layout (~/.bluebox paths)
internal/runtime/          the podman + krun driver -- the ONLY backend-aware code
internal/cli/              subcommand wiring
```

Swapping podman for another backend (Firecracker, Cloud Hypervisor) means
touching `internal/runtime` alone.

## Why every command gets a fresh VM

This is the one design decision worth understanding, because it is not how
Docker behaves.

`podman exec` **does not work with krun**. crun's krun handler refuses it
outright — `the handler does not support exec`. That is unavoidable rather than
a bug: a microVM has its own kernel, so there is no host-side namespace for
`exec` to step into. (Historically this was worse: [crun#1098](https://github.com/containers/crun/issues/1098)
reported `exec` silently running on the *host* kernel while appearing isolated.
It now fails closed.)

So bluebox does not keep VMs running to exec into. Each `bluebox run` boots a
fresh microVM, runs the command, and discards it. Isolation is correct by
construction — there is no code path that can accidentally land on the host.

The trade-off: **no state carries between commands except `/data`**. No working
directory, no environment, no background processes. A netcat listener started in
one `run` is gone by the next. If you need a long-lived session, use
`bluebox shell`, which gives you one VM for the whole session.

## Requirements

- KVM (`/dev/kvm`)
- podman
- libkrun — Fedora: `sudo dnf install libkrun`
- a `krun` symlink, which is how crun is told to use libkrun:

```sh
sudo ln -sf $(command -v crun) /usr/local/bin/krun
```

Build it:

```sh
go build -o bluebox ./cmd/bluebox
go test ./...          # unit tests for the Bluefile parser/generator
```

## Use

```sh
bluebox new kali-ctf                     # scaffold a Bluefile
$EDITOR ~/.bluebox/sandboxes/kali-ctf/Bluefile
bluebox build kali-ctf                   # generate Containerfile, build, verify
bluebox run kali-ctf -- nmap -sV target  # one command, fresh VM
bluebox shell kali-ctf                   # interactive session
bluebox verify kali-ctf                  # prove it has its own kernel
bluebox ls
```

Point your AI harness at `bluebox run <name> -- <cmd>`. It behaves like any
subprocess: stdout, stderr and exit code pass straight through.

## The Bluefile

One file defines the whole sandbox. Line-oriented `KEY value`; `#` comments and
blank lines ignored; `PACKAGE`, `RUN` and `ENV` may repeat.

```
BASE     docker.io/kalilinux/kali-rolling
CPUS     4
RAM      4096          # MiB
NETWORK  bridge        # bridge = internet access, none = airgapped
READONLY true          # read-only guest rootfs (/tmp and /data stay writable)
TIMEOUT  300           # seconds per run, 0 = unlimited
SECCOMP  /home/me/.bluebox/seccomp/tight.json   # optional, filters the VMM
PACKAGE  nmap python3 python3-pip gdb
PACKAGE  binutils file
RUN      pip3 install --break-system-packages pwntools
ENV      LANG=C.UTF-8
```

`bluebox build` turns that into a Containerfile and builds it. The package
manager is picked from `BASE`: Alpine → `apk`, Debian/Ubuntu/Kali → `apt`.
`CPUS` maxes at 16 (krun's limit); `RAM` is MiB. See the runtime-restrictions
section for what `READONLY`, `TIMEOUT` and `SECCOMP` actually enforce.

## Layout

```
~/.bluebox/
  sandboxes/<name>/Bluefile        the spec you edit
  sandboxes/<name>/Containerfile   generated from the Bluefile on build
  data/<name>/                     mounted at /data -- the ONLY persistent part
```

Override the root with `BLUEBOX_HOME`.

## Restriction directives

`NETWORK bridge` gives outbound access (needed for `apt`, `pip`, CTF targets);
`NETWORK none` cuts it off entirely — use it when detonating untrusted binaries
that should not phone home.

`READONLY true` makes the guest root filesystem read-only; podman still provides
a writable `/tmp`, and `/data` stays writable. `TIMEOUT` caps wall-clock time
per `bluebox run` — the VM is killed and reaped, and the exit code is `124`,
matching `timeout(1)` so a harness can distinguish it from an ordinary failure.
`0` disables it. `bluebox shell` is never timed out.

## Runtime restrictions: where the boundary actually is

Worth understanding before reaching for seccomp or eBPF, because a microVM
moves the trust boundary and container instincts mislead here.

**Inside the guest, the workload is unconfined.** It runs as root with full
capabilities and can even mount filesystems. `--cap-drop=ALL` does *not* reach
it — `CapEff` still reads `000001ffffffffff` with the flag set. That is fine:
those syscalls hit the **guest** kernel, which is disposable and rebuilt on the
next run. Confining them buys little.

**The real boundary is the VMM process on the host.** The guest root filesystem
is `virtiofs`, served by the VMM, so anything the guest does that touches host
resources becomes a syscall made by the VMM. That is what host-side controls
filter, and it is exactly the blast radius of a VM escape.

This produces a result that surprises people. A seccomp profile blocking `mkdir`
*does* stop `mkdir` inside the guest — not because the guest is filtered
(`/proc/self/status` shows `Seccomp: 0`, no filter loaded) but because the
directory creation is ultimately performed by the VMM on the host. Meanwhile a
profile blocking `uname` does **not** stop `uname` in the guest, because the
guest kernel answers it without ever involving the host.

So: **host-served operations are filterable, guest-only operations are not.**

Point `seccomp` at a profile path to filter the VMM. Podman already applies its
default profile, so custom profiles are for tightening further — and a profile
that is too aggressive will break virtiofs or networking rather than fail
loudly. Test with `bluebox verify` and a real workload before trusting one.

**On eBPF:** it is not where the value is here. In-guest eBPF would confine a
throwaway kernel. Host-side eBPF would confine the VMM, which seccomp already
does more simply. If you want stronger runtime restriction, the highest-value
addition is **egress allow-listing** (nftables rules against the sandbox's
network, rather than the current all-or-nothing `bridge`/`none`), not eBPF.

## Verifying isolation

`bluebox build` verifies automatically, and `bluebox verify` re-checks on demand.
It compares the guest kernel against the host's and refuses the sandbox if they
match:

```
  host kernel:  6.18.33.2-microsoft-standard-WSL2
  guest kernel: 6.12.91
  OK: separate kernel, genuine microVM.
```

Matching kernels mean you got a plain container — same commands, same output,
no kernel boundary. Worth checking after changing anything about the runtime setup.

## A Kali CTF sandbox

The whole thing is one Bluefile:

```
BASE     docker.io/kalilinux/kali-rolling
CPUS     4
RAM      4096
NETWORK  bridge
READONLY true
TIMEOUT  600
PACKAGE  nmap netcat-traditional python3 python3-pip gdb ltrace strace
PACKAGE  binutils file curl wget git
RUN      pip3 install --break-system-packages pwntools
```

Tools go in the Bluefile; work products go in `/data`. A broken or compromised
sandbox is fixed by rebuilding, and nothing is lost.

## Not implemented

- **Persistent VMs with `exec`.** Needs sshd in the image plus port forwarding.
  Host-to-guest forwarding is verified working, so this is viable — it is just
  more moving parts than v1 needs.
- **GUI.** libkrun supports virtio-gpu and Wayland forwarding via `krun.gpu_flags`
  and sommelier. Both punch holes through the VM boundary (host compositor
  socket, host GPU driver), so for untrusted code prefer a VNC server inside the
  guest: only pixels cross. Fine to use GPU passthrough for trusted dev sandboxes.
- **bootc.** Not needed here — libkrun supplies its own kernel, so a bootc
  image's kernel would be dead weight. It becomes the right tool if bluebox ever
  moves to Firecracker or Cloud Hypervisor, which need a real bootable disk
  image. Note `bootc-image-builder` was archived in June 2026; use
  [osbuild/image-builder](https://github.com/osbuild/image-builder) with `--bootc-ref`.

## spike.sh

The throwaway script used to validate this design against the real runtime
before any Go was written. Run it on a new host to confirm the six assumptions
bluebox rests on. Test 2 is the one that decides the architecture.
