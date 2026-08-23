# bluebox

Isolated, persistent sandboxes built on podman and the `krun` runtime (libkrun). 

## Why

A container shares your host kernel. bluebox gives each sandbox its **own**
kernel, so `mount`, `sysctl`, `modprobe` and `rm -rf /` act on a machine that
is rebuilt on the next run.

## Install

Requirements: podman, libkrun, and Go 1.26+ to build. Linux needs KVM
(`/dev/kvm`).

**0. Or just take the binary**

```sh
curl -fsSL https://chxperiments.github.io/bluebox/install.sh | sh
```

That fetches the right build for your platform into `~/.local/bin`. You still
need the runtime pieces below.

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

Every `run` and `shell` re-checks this boundary, but cheaply: the result is
cached against a fingerprint of the runtime (the `krun` symlink, the `crun` it
resolves to, and the podman and libkrun versions). While that fingerprint is
unchanged the check is a fast lookup; if it changes — a re-pointed symlink, an
upgraded `crun`, libkrun support dropped — the full kernel comparison runs
again before the command does, and a sandbox that has quietly become a plain
container is refused rather than run.

## Quick start

```sh
bluebox new devbox                        # scaffold a Bluefile
$EDITOR ~/.bluebox/sandboxes/devbox/Bluefile
bluebox build devbox                      # build the image, verify isolation
bluebox run devbox -- python3 script.py   # one command in a fresh microVM
bluebox shell devbox                      # interactive session
```

Ready-made Bluefiles for Python, Node, Go, an AI-agent sandbox, an offline one
and a system-experiments one live in [`examples/`](examples/).

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

Field constraints, enforced at parse time so a bad value fails loudly instead
of leaking into the generated Containerfile:

- `base` must be an image reference without whitespace.
- `env` keys are identifiers (`LANG`, `CGO_ENABLED`); values cannot contain
  newlines — put multi-step builds in `run` instead.
- `packages` entries must be plain package names (no spaces or `; | & $ \``).
- blueprint user names are lowercase identifiers, `shell` an absolute path,
  and file `mode`s octal (e.g. `"0755"`).

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

Sandbox names use letters, digits, `.`, `_` and `-` (max 64 characters,
starting with a letter or digit). A name is a single path component by
construction, so nothing a sandbox does with its name can reach outside
`~/.bluebox`.

| Command | What it does |
|---|---|
| `bluebox new <name>` | scaffold a Bluefile |
| `bluebox edit <name> [-b\|-c]` | open the Bluefile or Containerfile in `$EDITOR` |
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

`bluebox edit` opens `$VISUAL`, `$EDITOR`, or `vi`. With no flag it asks which
file; `-b` and `-c` skip the prompt. Editing the Bluefile re-parses it on save,
so a mistake surfaces immediately rather than at the next build. The
Containerfile is generated, so edits to it are replaced by the next build —
bluebox says so before opening it.

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

## License

MIT
