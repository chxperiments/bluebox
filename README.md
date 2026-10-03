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
bluebox doctor
bluebox new demo
bluebox build demo
```

`doctor` checks every piece above at once and prints the fix for whatever is
missing.

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
isolation: strict      # VMM runs as a UID that is not yours (see Security model)
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
in about 20-45ms instead of about 1.4s, and every run still gets a fresh VM
(see [bench/RESULTS.md](bench/RESULTS.md)):

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

Mounts cannot nest inside anything the guest can write. A mount whose host
path is inside an `rw` mount (`~/proj` rw plus `~/proj/config` ro), inside any
sandbox's `/data` under `~/.bluebox/data`, or an `rw` mount that contains this
sandbox's `/data`, is refused at every boot. Otherwise the guest could replace
part of that path with a symlink, and the next run would mount whatever it
points at — `~/.ssh`, say — in the declared place.

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
| `bluebox doctor` | check the host setup, with a fix for each failure |
| `bluebox ls` | list sandboxes |
| `bluebox env <name>` | print effective settings as `KEY=VALUE` |
| `bluebox logs <name> [-n]` | show recent runs (default 200 lines) |
| `bluebox reset <name>` | empty `/data`, keeping the sandbox |
| `bluebox snapshot <name> [label]` | archive `/data` under a name; `-l` lists archives |
| `bluebox restore <name> [snap]` | replace `/data` from a snapshot (newest by default) |
| `bluebox fork <name> <fork>` | branch a sandbox: same image, `/data` overlaid |
| `bluebox diff <fork>` | list what a fork changed in `/data` |
| `bluebox apply <fork>` | merge a fork's changes into its parent |
| `bluebox discard <fork>` | drop a fork's changes |
| `bluebox data import\|export <name> <dir>` | copy files into or out of `/data` |
| `bluebox rename <old> <new>` | rename, keeping data, logs, snapshots and the built image |
| `bluebox destroy <name> [--data]` | remove a sandbox; `--data` also deletes `/data` |
| `bluebox nuke [--no-data]` | remove every sandbox; `--no-data` keeps data |

`bluebox edit` opens `$VISUAL`, `$EDITOR`, or `vi`. With no flag it asks which
file; `-b` and `-c` skip the prompt. Editing the Bluefile re-parses it on save,
so a mistake surfaces immediately rather than at the next build. The
Containerfile is generated, so edits to it are replaced by the next build —
bluebox says so before opening it.

`bluebox env` is shell-consumable: `eval "$(bluebox env devbox)"`. Every value
is single-quoted, and the Bluefile's own `env` entries are printed as
`BLUEBOX_ENV_<KEY>` (e.g. `BLUEBOX_ENV_LANG`), so evaluating the output of a
Bluefile you did not write only ever assigns `BLUEBOX_*` variables — it cannot
run a command or replace `PATH` in your shell.

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

### fork, diff, apply

A fork is a sandbox whose `/data` is an overlay on another's: the parent's
`/data` underneath, read-only, and the fork's own changes in a layer of their
own. Running a fork never touches the parent, so you can try several things
side by side and keep one:

```sh
bluebox fork devbox try-a               # same Bluefile and image, branched /data
bluebox fork devbox try-b
bluebox run try-a -- make test
bluebox run try-b -- make test
bluebox diff try-a                      # A added, M modified, D deleted
bluebox apply try-a                     # merge into devbox's /data, asks first
bluebox discard try-b                   # or throw the changes away
```

This is how an agent's work reaches your real `/data` only after you have
seen it: point the agent at a fork, `diff`, then `apply` or `discard`. It is
also cheap: a fork is created in milliseconds, holds only what it changes,
and boots nothing to diff.

Only `/data` is branched; everything else resets per run anyway, so a fork
cannot carry a running process or installed packages from its parent. The
overlay is an unprivileged overlayfs mounted inside rootless podman's own
user namespace, so it needs no root and is invisible on the host: the
layers live under `~/.bluebox/forks/<name>/`, and `merge/` there looks empty
from outside. Forks work under both isolation modes, and a fork may use the
warm pool and `up`/`exec` like any sandbox. Forks need a Linux host.

`apply` treats the parent's `/data` as guest-written, which it is. A sandbox
can plant a symlink there pointing anywhere on the host, exactly where a
fork then writes a file; `apply` never follows one, replaces it with what the
fork has, and skips device nodes, fifos and sockets. setuid and setgid bits
are dropped. While a sandbox has forks, `reset`, `restore`, `rename` and
`destroy` on it are refused, since a fork's lower layer is its `/data`; a
fork cannot be snapshotted or forked again (apply it first). A fork's
`isolation` must match its parent's, as the two share files.

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

## Backends

What boots a sandbox's VM is a per-sandbox choice, `backend:` in the Bluefile
(or `BLUEBOX_BACKEND` to try one without editing). Images are always built by
podman; the warm pool, `up`/`exec`, forks, the isolation check and the VMM's
confinement are the same on every backend.

| `backend:` | How it boots | Fresh-VM `run` (bridge / none) | `up` | Status |
|---|---|---|---|---|
| `podman` (default) | `podman run --runtime krun` (libkrun) | ~1.5 s / ~1.3 s | ~1.6 s | all features |
| `krun` | crun's libkrun handler driven directly, no podman in the path | ~1.1 s / ~0.75 s | ~1.0 s | `isolation: standard` only, for now |
| `firecracker` | Firecracker, restoring a snapshot per run | — / **~0.3 s** | ~0.3 s | `network: none`, no host mounts, no forks yet |

With `warm:`, a podman or krun run takes ~20–45 ms from the pool; Firecracker
needs no pool, because restoring a snapshot is itself the fast path.

### The firecracker backend

Each image is booted once, at its first use, and snapshotted with its agent
ready; every run restores that snapshot instead of booting a kernel. A
restored copy is made distinct before anything runs in it: the agent token
baked into the snapshot is rotated, the guest mixes in fresh randomness
(Firecracker also exposes a VM generation ID) and takes the host's clock,
and only then is `/data` mounted.

- **Confinement.** The VMM is the only process of a crun container with
  **no capabilities at all**, `no_new_privs`, podman's default seccomp filter
  (under which Firecracker installs its own per-thread filters), its own pid,
  mount, network and IPC namespaces, pids and memory limits, and a read-only
  root holding nothing but its binary, kernel, image, VM directory, `/data`
  disk and `/dev/kvm`. Under `isolation: strict` it runs as your subordinate
  UID. This is what Firecracker's jailer provides, without the root it needs.
- **No network needed for the agent.** `up`/`exec` talk to it over vsock,
  whose host side is a Unix socket in the VM's owner-only directory.
- **`/data` is a disk** per sandbox (`~/.bluebox/disks/<name>.ext4`, sparse,
  8 GiB ceiling), mounted by one VM at a time. Move files with
  `bluebox data import` / `bluebox data export`.
- **Pinned and verified.** Firecracker v1.17.0 and its 6.1 guest kernel are
  downloaded on first use and checked against fixed SHA-256 hashes before
  they run.
- **Costs.** Each image keeps a memory snapshot the size of its `ram_mib`
  on disk. x86_64 Linux only.
- **Not yet:** networking (`network: bridge`), host `mounts:`, forks, and a
  TTY for `bluebox shell` (it gets a plain stdin/stdout session).

### Moving data: `bluebox data`

```sh
bluebox data import devbox ./inputs     # copy a directory's contents into /data
bluebox data export devbox ./results    # copy /data out to a new, empty directory
```

Works on every backend. Exported files were written by the guest, so they
are treated as hostile: symlinks are kept as symlinks and never followed,
nothing can land outside the target directory, devices and fifos are
skipped, and setuid bits are dropped.

The `krun` backend exports the image once to `~/.bluebox/rootfs/`, overlays
it per VM, and writes the OCI spec itself with the same confinement the
podman backend asks podman for: no new privileges, the same 6 capabilities,
podman's default seccomp filter, pids and memory limits, an empty network
namespace that a `pasta` hook connects with the host gateway unmapped, and
the agent's port on `127.0.0.1` only. It passes the same escape suite
(`security/escape-test.sh ./bluebox standard krun`). It refuses
`isolation: strict` rather than fall back to running the VMM as you: libkrun
does not start inside the nested user namespace that strict needs here, which
is the next thing to fix. Warm runs and `exec` are milliseconds on both
backends; the difference is cold boots and how fast the pool refills.

## SDKs

Drive sandboxes from code: [Python](sdk/python/), [TypeScript](sdk/typescript/)
and [Go](sdk/go/), all without dependencies.

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
  forks/<name>/upper               a fork's changes to its parent's /data
  logs/<name>.log                  run history
  data/<name>/                     mounted at /data -- the only persistent part
  run/<name>.json                  agent address + token while a sandbox is up
  pool/<name>/                     warm VMs waiting for a run (owner-only)
  bluebox.sock                     the SDK server's socket (owner-only)
  agent/bluebox                    the agent binary running sandboxes mount
```

Override the root with `BLUEBOX_HOME`.

## Security model

A sandbox has two layers, and both matter.

**1. The microVM.** The guest runs on its own kernel under KVM, so a guest
kernel exploit takes over the guest, not your host.

**2. The confinement around the VMM.** libkrun's own documentation says the
guest and its VMM "pertain to the same security context": the VMM, a process
on your host, does the guest's file I/O (virtiofs) and opens its network
connections. Anything the VMM may do, a guest that has broken out of libkrun
may do too. So bluebox confines the VMM:

| | every sandbox | `isolation: strict` |
|---|---|---|
| own network namespace (no host loopback) | yes | yes |
| seccomp filter | podman's default | podman's default |
| `no_new_privs` | yes | yes |
| capabilities | 6 of podman's 11: what virtiofs and low ports need | same |
| pids limit / memory limit | 512 / `ram_mib` + 256 MiB | same |
| host UID of the VMM | **yours** | **a subordinate UID, not yours** |
| writable host mounts | allowed | refused |

Under standard isolation the VMM runs as you, so escaping both the VM and
libkrun lands in your account. **Use `isolation: strict` for agents and any
untrusted code.** The VMM then runs as the first UID of your subordinate range
(see `/etc/subuid`), which owns nothing of yours. The agent examples use
strict.

What strict changes:

- `/data` is owned by that UID. You can read it from your account. bluebox's
  own `snapshot`, `restore`, `reset` and `destroy` handle it through
  `podman unshare`. Switching a sandbox between modes re-owns `/data` once.
- Mounts must be read-only, and readable by other users (`o+rx`), because the
  sandbox is no longer you. Exchange files through `/data` or the SDK's
  `write_file`.
- Every strict sandbox shares one subordinate UID, so strict separates
  sandboxes from you, not from each other.

The full threat model, including what the boundary is not, is in
[SECURITY.md](SECURITY.md). `security/escape-test.sh` runs what a hostile agent would try from inside a
sandbox (reaching services on host loopback, reading host files, symlink and
`..` traversal out of shared directories, writing through read-only mounts,
reading the agent token, a fork bomb) and checks the VMM's confinement:

```sh
security/escape-test.sh "$(command -v bluebox)" strict
```

Limits worth knowing:

- The per-command kernel check catches a runtime that fell back to a plain
  container. It cannot catch a guest that lies about its kernel.
- A sandbox can plant symlinks in `/data` that point anywhere. They are
  harmless inside the guest, but host-side tools must not follow them; bluebox's
  own `restore` checks archive entries before unpacking.
- `seccomp:` in the Bluefile filters the VMM process, not the guest: the guest
  runs unconfined against its own kernel, which is the point.
- For hostile multi-tenant workloads, a Firecracker backend with its jailer is
  the planned next step (see the ROADMAP).

## Development

```sh
CGO_ENABLED=0 go build -o bluebox ./cmd/bluebox   # static, so `up` can use it
go test ./...                 # unit tests; no VM needed
security/escape-test.sh ./bluebox strict   # escape attempts, needs KVM
security/escape-test.sh ./bluebox standard krun   # the same, krun backend
security/escape-test.sh ./bluebox strict firecracker   # and firecracker
test/fork-e2e.sh ./bluebox strict          # fork lifecycle, needs KVM
```

```
cmd/bluebox/      entrypoint
internal/bluefile/  Bluefile parser + Containerfile generator
internal/sandbox/   on-disk layout
internal/runtime/   podman + krun driver -- the only backend-aware code
internal/agent/     in-guest agent and its wire protocol (up/exec)
internal/server/    `bluebox serve`, the local API the SDKs use
sdk/{python,typescript,go}  client SDKs
examples/           example Bluefiles, embedded for `new --from`
internal/cli/       cobra commands (root.go, commands.go)
```

## License

MIT
