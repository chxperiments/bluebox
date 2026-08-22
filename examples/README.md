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

| Example | What it shows |
|---|---|
| [`python-dev`](python-dev/Bluefile) | apt packages, a pip build step, env vars |
| [`node-dev`](node-dev/Bluefile) | a global npm install as a build step |
| [`go-build`](go-build/Bluefile) | `pkgmgr` override, read-only root |
| [`ai-agent`](ai-agent/Bluefile) | blueprint user + sudo, read-only root, per-run timeout |
| [`offline`](offline/Bluefile) | `network: none` — no egress at all |
| [`os-lab`](os-lab/Bluefile) | a real kernel: `mount`, `sysctl`, `modprobe` work |

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
