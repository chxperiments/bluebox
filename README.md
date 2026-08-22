# bluebox

Isolated, persistent sandboxes.

Describe a sandbox in one **Bluefile** — base image, CPUs, RAM, network, and
the tools you want — and bluebox runs it as a real KVM microVM that boots in
about a second. Your work in `/data` persists. Everything else resets on every
run, so a sandbox you break is fixed by running it again.

Built on podman and the `krun` runtime (libkrun). You never write a
Containerfile; bluebox generates one from the Bluefile.

## Why

A container shares your host kernel. bluebox gives each sandbox its **own**
kernel, so it can install anything, change anything, and never touch your
machine — while the files you care about survive in one directory you can read
from the host.

## Install

Requirements: Linux with KVM (`/dev/kvm`), podman, libkrun, and Go 1.26+ to build.

**1. Install the runtime pieces**

```sh
# Fedora / RHEL
sudo dnf install -y podman libkrun golang
```

On other distros, podman and Go are packaged everywhere; libkrun may need
building from [libkrun/libkrun](https://github.com/libkrun/libkrun).

**2. Check that crun has libkrun support**

```sh
crun --version | grep -o '+LIBKRUN'
```

It must print `+LIBKRUN`. Without it, crun cannot start microVMs.

**3. Create the `krun` symlink**

This is how crun is told to use libkrun. It is required, not optional.

```sh
sudo ln -sf $(command -v crun) /usr/local/bin/krun
```

**4. Build and install**

```sh
git clone https://github.com/chxperiments/bluebox
cd bluebox
go build -o bluebox ./cmd/bluebox
install -Dm755 bluebox ~/.local/bin/bluebox
```

Make sure `~/.local/bin` is on your `PATH`.

**5. Verify**

```sh
bluebox new demo
bluebox build demo
```

The build ends by comparing kernels. Two different versions means real
isolation:

```
  host kernel:  6.18.33.2
  guest kernel: 6.12.91
  OK: separate kernel, genuine microVM.
```

If they match, you got a plain container and bluebox refuses the sandbox.

## Quick start

```sh
bluebox new devbox                        # scaffold a Bluefile
$EDITOR ~/.bluebox/sandboxes/devbox/Bluefile
bluebox build devbox                      # build the image, verify isolation
bluebox run devbox -- python3 script.py   # one command in a fresh microVM
bluebox shell devbox                      # interactive session
```

## The Bluefile

One YAML file per sandbox. Unknown keys are rejected, so typos fail loudly.

```yaml
base: docker.io/library/debian:bookworm-slim
cpus: 4
ram_mib: 4096
network: bridge        # bridge = internet access, none = offline
readonly: true         # read-only root (/tmp and /data stay writable)
timeout_seconds: 600   # per-run wall clock limit, 0 = unlimited

packages:
  - python3
  - git
run:
  - pip3 install --break-system-packages requests
env:
  LANG: C.UTF-8
```

The package manager is chosen from `base`: Alpine uses `apk`, Debian and Ubuntu
use `apt`, Fedora and RHEL-likes use `dnf`. For any other base, set `pkgmgr`
explicitly to `apk`, `apt` or `dnf`.

`cpus` maxes at 16 (a krun limit) and `ram_mib` is in MiB.

## Commands

| Command | What it does |
|---|---|
| `bluebox new <name>` | scaffold a Bluefile |
| `bluebox build <name>` | generate the Containerfile, build, verify isolation |
| `bluebox run <name> -- <cmd>` | run one command in a fresh microVM |
| `bluebox shell <name>` | interactive session in one microVM |
| `bluebox verify <name>` | re-check that the sandbox has its own kernel |
| `bluebox ls` | list sandboxes |

`bluebox run` behaves like any subprocess: stdout, stderr and exit codes pass
straight through, so it scripts and automates cleanly. A run stopped by
`TIMEOUT` exits `124`, matching `timeout(1)`.

## What persists

Only `/data`, which lives at `~/.bluebox/data/<name>/` and is a normal
directory you can open from the host.

Everything else is discarded. **Each `bluebox run` is a new microVM**, so no
working directory, environment change, or background process carries from one
command to the next. Use `bluebox shell` when you need a session that holds
state, and keep anything worth saving in `/data`.

This is a consequence of the design rather than a limitation to work around:
`podman exec` does not work with krun (`the handler does not support exec`),
because a microVM has its own kernel and there is no host-side namespace to
step into. Booting per command means there is no path that can quietly land
back on your host.

## Where things live

```
~/.bluebox/
  sandboxes/<name>/Bluefile        the spec you edit
  sandboxes/<name>/Containerfile   generated on build
  data/<name>/                     mounted at /data -- the only persistent part
```

Override the root with `BLUEBOX_HOME`.

## Notes

`READONLY` and `SECCOMP` apply on the host side. The workload inside the guest
runs unconfined against its own throwaway kernel, which is the point — but it
means a seccomp profile filters the VMM process, not the guest. Operations the
VMM performs on the host (file I/O, which goes through virtiofs) are filterable;
operations the guest kernel answers alone are not.

## Development

```sh
go build -o bluebox ./cmd/bluebox
go test ./...        # covers the Bluefile parser and Containerfile generator
```

```
cmd/bluebox/      entrypoint
internal/bluefile/  Bluefile parser + Containerfile generator
internal/sandbox/   on-disk layout
internal/runtime/   podman + krun driver -- the only backend-aware code
internal/cli/       subcommand wiring
```

`spike.sh` validates the runtime assumptions on a new host.
