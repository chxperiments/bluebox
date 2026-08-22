# bluebox

Disposable microVM sandboxes for AI harnesses. Define a sandbox with a
Containerfile, then run commands inside a real KVM microVM that boots in about
a second. Only `/data` survives between runs.

Built on podman + the `krun` runtime (libkrun). bluebox is a thin wrapper: podman
does the image building, caching, volumes and networking.

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
go build -o bluebox .
```

## Use

```sh
bluebox new kali-ctf                     # scaffold
$EDITOR ~/.bluebox/sandboxes/kali-ctf/Containerfile
bluebox build kali-ctf                   # build + verify isolation
bluebox run kali-ctf -- nmap -sV target  # one command, fresh VM
bluebox shell kali-ctf                   # interactive session
bluebox verify kali-ctf                  # prove it has its own kernel
bluebox ls
```

Point your AI harness at `bluebox run <name> -- <cmd>`. It behaves like any
subprocess: stdout, stderr and exit code pass straight through.

## Layout

```
~/.bluebox/
  sandboxes/<name>/Containerfile   what is installed (rebuilt, never persists)
  sandboxes/<name>/config.json     cpus, memory_mib, network
  data/<name>/                     mounted at /data -- the ONLY persistent part
```

Override the root with `BLUEBOX_HOME`.

`config.json`:

```json
{
  "cpus": 2,
  "memory_mib": 2048,
  "network": "bridge"
}
```

`network: "bridge"` gives outbound access (needed for `apt`, `pip`, CTF targets).
`network: "none"` cuts it off entirely — use it when detonating untrusted
binaries that should not phone home. `cpus` maxes out at 16, which is krun's limit.

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

```dockerfile
FROM docker.io/kalilinux/kali-rolling

RUN apt-get update && apt-get install -y --no-install-recommends \
      nmap netcat-traditional python3 python3-pip gdb ltrace strace \
      binutils file curl wget git \
 && rm -rf /var/lib/apt/lists/*
RUN pip3 install --break-system-packages pwntools

WORKDIR /data
```

Keep tools in the Containerfile and work products in `/data`. Then a broken or
compromised sandbox is fixed by rebuilding, and nothing is lost.

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
