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

Requirements: podman, libkrun, and Go 1.26+ to build. Linux needs KVM
(`/dev/kvm`); see [macOS](#macos) for what that means there.

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
isolated: host 6.18.33.2, guest 6.12.91
```

If they match, you got a plain container and bluebox refuses the sandbox.

### macOS

bluebox builds and runs on macOS, but the isolation story is different and
worth understanding before relying on it.

Every container on macOS already runs inside the **podman machine** VM, so
there is no `/dev/kvm` on the host and the `krun` runtime lives inside the
machine rather than beside your shell. `bluebox` checks that a machine is
running instead, and leaves the real question to `bluebox verify`.

Nesting a microVM *inside* that VM needs nested virtualisation, which on Apple
Silicon means an **M3 or later running macOS 15+**, with crun built against
libkrun available inside the machine image. Where that holds, sandboxes behave
as they do on Linux. Where it does not, `bluebox verify` fails and says the
sandbox shares a kernel, rather than pretending.

That check is deliberately not a comparison against the host's own `uname`: on
macOS the host runs Darwin, so any Linux container kernel differs from it
whether or not a microVM is involved, and comparing against the host would
report isolation that is not there. bluebox compares the sandbox against the
kernel a plain container sees instead, which is the host kernel on Linux and
the podman machine's kernel on macOS.

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

### blueprint

For cloud-init-style provisioning — users, files and commands:

```yaml
blueprint:
  users:
    - name: admin
      shell: /bin/bash
      sudo: true          # passwordless; put sudo in packages
  write_files:
    - path: /etc/motd
      content: |
        Welcome.
      mode: "0644"        # optional
  runcmd:
    - echo provisioned > /etc/stamp
```

Unlike cloud-init this is applied at **build** time, not first boot. Every run
is a fresh VM, so boot-time provisioning would repeat on every command.

`write_files` contents are copied into the image rather than echoed through a
shell, so quotes, newlines and `$variables` survive exactly as written. User
creation adapts to the base: `useradd` on apt/dnf images, `adduser` on Alpine,
and the sudo group is `sudo` or `wheel` as that distro expects.

## Commands

| Command | What it does |
|---|---|
| `bluebox new <name>` | scaffold a Bluefile |
| `bluebox build <name>` | generate the Containerfile, build, verify isolation |
| `bluebox run <name> -- <cmd>` | run one command in a fresh microVM |
| `bluebox shell <name>` | interactive session in one microVM |
| `bluebox verify <name>` | re-check that the sandbox has its own kernel |
| `bluebox ls` | list sandboxes |
| `bluebox env <name>` | print effective settings as `KEY=VALUE` |
| `bluebox logs <name> [-n]` | show recent runs (default 200 lines) |
| `bluebox reset <name>` | empty `/data`, keeping the sandbox |
| `bluebox snapshot <name> [-l]` | archive `/data`; `-l` lists archives |
| `bluebox rename <old> <new>` | rename, keeping data, logs and the built image |
| `bluebox destroy <name> [--data]` | remove a sandbox; `--data` also deletes `/data` |
| `bluebox nuke [--no-data]` | remove every sandbox; `--no-data` keeps data |

`bluebox env` is shell-consumable: `eval $(bluebox env devbox)`.

Anything that deletes data asks first, and `-y` skips the prompt. `destroy`
keeps `/data` unless you pass `--data`; `nuke` deletes it unless you pass
`--no-data`. Without a terminal they refuse rather than assume yes, so a script
cannot wipe your work by accident.

Snapshots are plain tarballs under `~/.bluebox/snapshots/<name>/`. To restore:

```sh
tar -xzf <archive> -C "$(bluebox env <name> | grep BLUEBOX_DATA | cut -d= -f2)"
```

`bluebox run` behaves like any subprocess: stdout, stderr and exit codes pass
straight through, so it scripts and automates cleanly. A run stopped by
`timeout_seconds` exits `124`, matching `timeout(1)`.

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
  snapshots/<name>/                /data archives
  logs/<name>.log                  run history
  data/<name>/                     mounted at /data -- the only persistent part
```

Override the root with `BLUEBOX_HOME`.

## Notes

`readonly` and `seccomp` apply on the host side. The workload inside the guest
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
internal/cli/       cobra commands (root.go, commands.go)
```

`spike.sh` validates the runtime assumptions on a new host.
