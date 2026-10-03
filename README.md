# bluebox

Just a box for your AI agents.

They can install, delete and break whatever they like inside it; nothing
outside changes. Every command runs in a disposable microVM with its own
kernel, defined in one file and started in milliseconds.

**[Docs](https://chxperiments.github.io/bluebox/docs.html)** ·
[Architecture](https://chxperiments.github.io/bluebox/architecture.html) ·
[Security](SECURITY.md) ·
[Benchmarks](bench/RESULTS.md)

## Install

```sh
curl -fsSL https://chxperiments.github.io/bluebox/install.sh | sh
bluebox doctor    # checks podman, libkrun and KVM, and prints a fix for anything missing
```

Needs Linux with KVM, podman and libkrun.

## Quick start

```sh
bluebox new devbox --from tiny-python     # scaffold a Bluefile
bluebox build devbox                      # build the image, verify isolation
bluebox run devbox -- python3 -c 'print(42)'   # one command, fresh VM

bluebox up devbox                         # keep one VM running
bluebox exec devbox -- pytest             # milliseconds per command
bluebox down devbox
```

## Bluefile

```yaml
base: docker.io/library/alpine:latest
backend: podman        # podman, krun or firecracker
isolation: strict      # the VMM runs as a UID that is not yours
network: bridge        # or none, for no network at all
warm: 2                # keep VMs booted so runs start in ms
packages: [python3, git]
```

Only `/data` persists between runs. Everything else is rebuilt every time.

## Forks

```sh
bluebox fork devbox try-a    # branch /data
bluebox run try-a -- make
bluebox diff try-a           # review what changed
bluebox apply try-a          # or: bluebox discard try-a
```

## From code and agents

- **SDKs:** [Python](sdk/python/), [TypeScript](sdk/typescript/), [Go](sdk/go/), [Rust](sdk/rust/)
- **MCP:** `claude mcp add bluebox -- bluebox mcp`. Agents can fork but not
  apply, unless you start it with `--allow-apply`.

```python
from bluebox import Sandbox

with Sandbox("devbox") as sb:
    print(sb.exec(["python3", "-c", "print(6 * 7)"]).stdout_text)
```

## Security

Each sandbox is a microVM under KVM, and the VMM itself is confined: no new
privileges, seccomp, a reduced capability set, its own network namespace and,
under `isolation: strict`, a UID that is not yours.
`security/escape-test.sh` tries what an agent gone wrong would. See
[SECURITY.md](SECURITY.md) for the threat model.

## Development

```sh
CGO_ENABLED=0 go build -o bluebox ./cmd/bluebox
go test ./...
security/escape-test.sh ./bluebox strict    # needs KVM
```

## License

MIT
