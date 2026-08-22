# Examples

Ready-to-use Bluefiles. Each directory is one sandbox.

To try one, scaffold a sandbox by that name and drop the example in:

```sh
name=python-dev
bluebox new "$name"
cp examples/"$name"/Bluefile ~/.bluebox/sandboxes/"$name"/Bluefile
bluebox build "$name"
bluebox run "$name" -- python3 --version
```

`bluebox new` creates the sandbox and its `/data` directory; copying the
Bluefile over the scaffold gives it this configuration; `build` generates the
Containerfile and verifies isolation.

### Development

| Example | What it shows |
|---|---|
| [`python-dev`](python-dev/Bluefile) | apt packages, a pip build step, env vars |
| [`node-dev`](node-dev/Bluefile) | a global npm install as a build step |
| [`go-build`](go-build/Bluefile) | `pkgmgr` override, read-only root |
| [`ai-agent`](ai-agent/Bluefile) | blueprint user + sudo, read-only root, per-run timeout |
| [`offline`](offline/Bluefile) | `network: none` — no egress at all |
| [`os-lab`](os-lab/Bluefile) | a real kernel: `mount`, `sysctl`, `modprobe` work |

### Certification prep

| Example | For | What it shows |
|---|---|---|
| [`cert-rhcsa`](cert-rhcsa/Bluefile) | RHCSA (EX200) | AlmaLinux 9, storage/SELinux/podman toolset |
| [`cert-rhce`](cert-rhce/Bluefile) | RHCE 9 (EX294) | Ansible automation environment |
| [`cert-cka`](cert-cka/Bluefile) | CKA | a real k3s cluster you start with `start-cluster` |
| [`cert-cks`](cert-cks/Bluefile) | CKS | k3s + trivy/kube-bench, AppArmor & seccomp on a real kernel |

### Labs

| Example | What it shows |
|---|---|
| [`lab-aws`](lab-aws/) | AWS CLI + Terraform against [Floci](https://floci.io/), a local AWS emulator; see [`main.tf`](lab-aws/main.tf) |

Three things about the cert sandboxes worth knowing, because they are honest
limits rather than bugs:

- **State is ephemeral.** Each `bluebox run` is a fresh VM. Start a cluster or
  a service inside `bluebox shell`, and keep anything you want to keep in
  `/data`. A k3s cluster's own state resets with the VM.
- **bluebox runs a command, not a full boot.** systemd is not PID 1, so live
  `systemctl start/enable` on services is limited. The many topics that do not
  need a running init — users, permissions, LVM/storage, SELinux contexts,
  networking config, containers, kernel features — all work, and work *because*
  the sandbox has its own kernel.
- **Kubernetes runs, with a networking caveat.** `cert-cka`/`cert-cks` boot a
  real k3s cluster (verified: node Ready, pods scheduled and serving). The
  minimal guest kernel has no VXLAN or nf_conntrack, so overlay CNI and
  Services do not work; `start-cluster` uses host-gw with kube-proxy off, and
  pods should run with `hostNetwork: true`. The API, scheduling, RBAC, kubectl,
  running containers, and the security tooling all work — which covers most of
  the exam objectives. Full overlay networking would need a fuller guest kernel
  (a possible future via libkrun's external-kernel support).

For `lab-aws`, run Floci separately (it serves AWS APIs on port 4566), then
point the sandbox's endpoint vars at it. If Floci is on the host, use the
host's bridge address rather than `localhost`, since inside the sandbox
`localhost` is the sandbox.

## Notes

- **`ai-agent`** is the one to look at for running a coding agent. Point it at
  `bluebox run agent -- <command>`: the agent works in a fresh microVM each
  time, `/data` carries the project between commands, and `timeout_seconds`
  caps any single command.
- **`go-build`** shows `pkgmgr: apt`. The package manager is normally inferred
  from the base image, but `golang:...` is not a name the tool recognises, so
  it is set explicitly.
- **`os-lab`** is the case a container cannot cover: it has its own kernel, so
  kernel-level commands act on the sandbox and reset with it.
- **`offline`** has no route out, so every tool must be listed under
  `packages` — there is no installing anything at run time.
