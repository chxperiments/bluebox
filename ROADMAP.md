# Roadmap

Near-term direction for bluebox. Each release is a [milestone][ms]; the themes
below come from real gaps found while building and testing the tool, not a wish
list. Dates are targets, not promises.

[ms]: https://github.com/chxperiments/bluebox/milestones

## v0.2.1 — Input hardening

Everything that turns a crafted Bluefile or sandbox name into something it
should not be. Host-side only; the microVM boundary is unchanged.

- Merge name and grammar validation ([#1]) — sandbox names stay inside the
  bluebox root; data fields can no longer restructure the generated Containerfile.
- Validate `write_files[].path`, which still reaches a `RUN chmod` line and can
  inject a build-time command.
- Validate sandbox names in `SnapshotsDir` too, so every path builder checks.
- Re-verify isolation per run, not only at build — a degraded runtime should be
  caught before a command runs, via a cached token keyed on the runtime's identity.

## v0.2.3 — Data & lifecycle

Make the persistence model explicit rather than implicit, and reversible.

- Declarative `mounts:` in the Bluefile — name the host paths a sandbox sees,
  instead of `/data` being the one magic directory.
- `bluebox restore <name> <snapshot>` — a first-class inverse of `snapshot`,
  rather than a documented `tar -xzf` one-liner.

## v0.2.5 — Network policy

Egress today is all-or-nothing (`bridge` or `none`). Move to per-sandbox policy.

- Hostname allow-listing via a forced egress proxy — filter on the requested
  host (CONNECT / TLS SNI), so CDN IP rotation can't defeat it and there is no
  route around it. An IP-pinned allow-list was prototyped and rejected: it fails
  open when a CDN rotates.
- An audit trail of what a sandbox reached for — useful for AI-agent use.

## v0.2.7 — Full cluster networking & limits

Close the guest-kernel gaps found running Kubernetes.

- A fuller guest kernel (via libkrun's external-kernel support) with VXLAN and
  `nf_conntrack`, so overlay CNI and Services work and CKA/CKS clusters are
  fully networked rather than host-gw + `hostNetwork` pods.
- Resource limits the VM knob does not cover: pid limits (in-guest) and a disk
  quota for `/data` (host-side).

## Beyond

- A pluggable hypervisor backend (Firecracker / Cloud Hypervisor) behind the
  same CLI — only worth building when a second backend actually lands.
- Confirmed macOS runtime testing on Apple Silicon (M3+, macOS 15+), where
  nesting a microVM needs nested virtualization.

[#1]: https://github.com/chxperiments/bluebox/pull/1
