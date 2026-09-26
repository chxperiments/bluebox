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
CGO_ENABLED=0 go build -o bluebox ./cmd/bluebox
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

bluebox up devbox                         # boot once, keep it running
bluebox exec devbox -- pytest             # milliseconds per command
bluebox down devbox
```

Ready-made Bluefiles for Python, Node, Go, an AI-agent sandbox, an offline one,
a system-experiments one and tiny images from ~5 MB live in
[`examples/`](examples/). They are built into bluebox:

```sh
bluebox new agent --from tiny-python      # start from an example
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
warm: 2                # VMs kept booted so run starts in ms, 0 = boot per run
warmup:                # run once in each warm VM, before it serves a run
  - python3 -c "import json"

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

### warm

`warm: N` (0-8) keeps N microVMs booted and waiting, so `bluebox run` starts
in about 20-30ms instead of about 1.5s, and every run still gets a fresh VM:

```yaml
warm: 2
```

Each waiting VM serves exactly one run and is then destroyed. After a run, a
detached `bluebox` process removes it and boots a replacement in the
background. If runs arrive faster than the pool refills (about 1.5s per VM),
the extra ones boot cold as before, so a larger `warm` absorbs bursts.

Before a pooled VM serves a run, bluebox runs a no-op in it, followed by any
`warmup:` lines. A VM's first command pays to load the shell and the program
into the guest, so preloading what your runs use (for example, starting
`python3` once) takes that cost off the run itself.

Every waiting VM holds its `ram_mib`, so size the pool to fit. `build`,
`reset`, `restore`, `rename`, `destroy` and `nuke` drain the pool first, since a
waiting VM holds the old image or `/data`, and a VM booted from an older
Bluefile is discarded rather than used. `warm` needs `network: bridge` (see
[up and exec](#running-sandboxes-up-and-exec)). A failed refill is recorded in
`bluebox logs`.

Field constraints, enforced at parse time so a bad value fails loudly instead
of leaking into the generated Containerfile:

- `base` must be an image reference without whitespace.
- `env` keys are identifiers (`LANG`, `CGO_ENABLED`); values cannot contain
  newlines — put multi-step builds in `run` instead.
- `packages` entries must be plain package names (no spaces or `; | & $ \``).
- blueprint user names are lowercase identifiers, `shell` an absolute path,
  and file `mode`s octal (e.g. `"0755"`).
- `write_files` paths are absolute and free of shell metacharacters — the path
  reaches a build `RUN`, so it is checked the same way as a mode.

### mounts

Beyond the implicit `/data` share, a Bluefile can declare exactly which host
directories a sandbox sees:

```yaml
mounts:
  - host: ~/projects/demo
    guest: /work
    mode: ro              # ro (default) or rw
```

`host` must be an absolute path (`~` expands to your home directory) with no
`:`, `guest` an absolute guest path that cannot shadow `/data`, and `mode` is
`ro` or `rw` — defaulting to `ro`, so a mount you forgot to make writable stays
read-only. What a sandbox can touch on the host is now visible in the spec
rather than implicit. Note that a writable mount still exposes that directory
fully: the guest writes through virtiofs with your user's permissions.

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
| `bluebox up <name>` | boot the microVM once and keep it running |
| `bluebox exec <name> -- <cmd>` | run one command in the running microVM |
| `bluebox down <name>` | stop the running microVM |
| `bluebox serve` | local API for the SDKs |
| `bluebox verify <name>` | re-check that the sandbox has its own kernel |
| `bluebox ls` | list sandboxes |
| `bluebox env <name>` | print effective settings as `KEY=VALUE` |
| `bluebox logs <name> [-n]` | show recent runs (default 200 lines) |
| `bluebox reset <name>` | empty `/data`, keeping the sandbox |
| `bluebox snapshot <name> [label]` | archive `/data` under a name; `-l` lists archives |
| `bluebox restore <name> [snap]` | replace `/data` from a snapshot (newest by default) |
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

Snapshots are plain tarballs under `~/.bluebox/snapshots/<name>/`. Give one a
label and you restore it by that label, instead of looking up a timestamp:

```sh
bluebox snapshot devbox before-upgrade    # archive /data as before-upgrade
bluebox restore devbox before-upgrade     # go back to it
```

Without a label the archive is named for the time it was taken:

```sh
bluebox snapshot devbox               # archive /data
bluebox snapshot devbox -l            # list archives, newest last
bluebox restore devbox                # roll back to the most recent
bluebox restore devbox 20260823T150405Z   # or to a specific one
```

Labels are reusable — `bluebox snapshot devbox nightly` replaces the previous
`nightly` rather than piling up archives — so it asks before overwriting, and
`-y` skips the prompt. The new archive is written beside the old one and
renamed into place, so an interrupted snapshot never destroys the one it was
replacing.

`restore` names a snapshot by its label or stamp, or takes a path to an archive
kept elsewhere. Entries are checked before anything is unpacked — an archive
that would write outside `/data` is refused — and the new data is swapped in
only once it is complete, so a failed restore leaves `/data` as it was.

`bluebox run` behaves like any subprocess: stdout, stderr and exit codes pass
straight through, so it scripts and automates cleanly. A run stopped by
`timeout_seconds` exits `124`, matching `timeout(1)`.

### Shell completion

```sh
bluebox completion bash > ~/.local/share/bash-completion/completions/bluebox
bluebox completion zsh  > "${fpath[1]}/_bluebox"
bluebox completion fish > ~/.config/fish/completions/bluebox.fish
```

Sandbox names complete after every command that takes one, and `restore`
completes that sandbox's snapshots by stamp, newest first. Both are read from
disk as you type, so a sandbox created in another terminal completes right
away. Where an argument is invented rather than chosen — `new`, the target of a
`rename`, a command passed to `run` — completion stays quiet instead of
offering host file names, which are never what belongs there.

## What persists

Only `/data`, which lives at `~/.bluebox/data/<name>/` and is a normal
directory you can open from the host.

Everything else is discarded. **Each `bluebox run` is a new microVM**, so no
working directory, environment change, or background process carries from one
command to the next. Use `bluebox shell` when you need a session that holds
state, and keep anything worth saving in `/data`.

`podman exec` does not work with krun (`the handler does not support exec`),
because a microVM has its own kernel and there is no host-side namespace to
step into. Booting per command means there is no path that can quietly land
back on your host.

### Running sandboxes: up and exec

Booting a microVM per command costs a second or more. When you want to run
many commands (an agent loop, a test suite, a build), bring the sandbox up
once and `exec` into it:

```sh
bluebox up devbox                              # ~1s, once
bluebox exec devbox -- pip install requests    # ~20-40ms each
echo 'import requests' | bluebox exec devbox -- python3 -
bluebox down devbox
```

`up` starts a small agent (the bluebox binary itself, mounted read-only at
`/.bluebox`) as the VM's main process, and `exec` asks it to run a command.
The agent is reached through a port published on `127.0.0.1` only, and it
answers only to a random per-VM token kept in `~/.bluebox/run/<name>.json`
(mode 0600). Before every command, the guest reports its kernel, and the
command is refused if it matches the host's.

Unlike `run`, state carries between commands, like a shell session: files
outside `/data`, installed packages and background processes all last until
`down`. Exit codes, stdout and stderr pass through, and `timeout_seconds`
applies per command (exit `124`). Piped stdin is forwarded, and a terminal is
forwarded only with `-i`.

Limits for now:

- Linux hosts only.
- The sandbox needs a network, because the agent is reached through a
  published port, and podman publishes none with `network: none`.
- bluebox must be a static build (`CGO_ENABLED=0`, as release binaries are),
  since it runs inside whatever distro the guest is. `up` says so if it isn't.
- No TTY yet. Use `bluebox shell` for interactive programs.

While a sandbox is up, `reset`, `restore` and `rename` refuse to run, because
the VM holds `/data` mounted. `destroy` and `nuke` bring it down first.

## SDKs

Drive sandboxes from code: [Python](sdk/python/) (standard library only) and
[Go](sdk/go/).

```python
from bluebox import Sandbox

with Sandbox("agent") as sb:                       # up on enter, down on exit
    sb.write_file("/data/task.py", "print(6 * 7)")
    print(sb.exec(["python3", "/data/task.py"]).stdout_text)   # ~10ms

print(Sandbox("agent").run("uname -r").stdout_text)            # fresh VM each call
```

The SDKs talk to `bluebox serve`, a local API on a Unix socket that only your
user can open (`~/.bluebox/bluebox.sock`). They start it when it is not
running. It exits after 15 minutes unused, and steps aside when bluebox is
upgraded.

## Where things live

```
~/.bluebox/
  sandboxes/<name>/Bluefile        the spec you edit
  sandboxes/<name>/Containerfile   generated on build
  snapshots/<name>/                /data archives
  logs/<name>.log                  run history
  data/<name>/                     mounted at /data -- the only persistent part
  run/<name>.json                  agent address + token while a sandbox is up
  pool/<name>/                     warm VMs waiting for a run (owner-only)
  bluebox.sock                     the SDK server's socket (owner-only)
  agent/bluebox                    the agent binary running sandboxes mount
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
CGO_ENABLED=0 go build -o bluebox ./cmd/bluebox   # static, so `up` can use it
go test ./...        # covers the Bluefile parser and Containerfile generator
```

```
cmd/bluebox/      entrypoint
internal/bluefile/  Bluefile parser + Containerfile generator
internal/sandbox/   on-disk layout
internal/runtime/   podman + krun driver -- the only backend-aware code
internal/agent/     in-guest agent and its wire protocol (up/exec)
internal/server/    `bluebox serve`, the local API the SDKs use
sdk/python, sdk/go  client SDKs
examples/           example Bluefiles, embedded for `new --from`
internal/cli/       cobra commands (root.go, commands.go)
```

## License

MIT
