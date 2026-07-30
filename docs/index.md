# agentctl

`agentctl` creates isolated, hardened, ephemeral sandboxes for running AI agents
safely — defending against two threats at once:

1. **Host escape** — the agent breaking out of its sandbox onto the machine running it.
2. **Lateral movement** — the agent (or something it's tricked into doing) attacking
   other devices on the same network: printers, NAS boxes, internal admin panels,
   coworkers' laptops.

Most "run an agent safely" tooling focuses only on the first threat. `agentctl`
treats the second as equally important: every sandbox defaults to **denying
egress to the local network** (RFC1918/link-local ranges) and to **default-deny
internet egress** outside an explicit allowlist.

## How it works

`agentctl` is a thin, provider-agnostic wrapper — it doesn't implement its own
hypervisor. It dispatches to whichever OS-native backend is installed and
configured:

| OS | Backend |
|---|---|
| Linux | [Incus](https://linuxcontainers.org/incus/) |
| macOS | [Lima](https://lima-vm.io/) |
| Windows | Hyper-V |

The provider is chosen once, at install/config time
(`agentctl config set provider=...`), and the command surface — `create`,
`start`, `shell`, `view`, network policy flags, and so on — stays identical no
matter which backend answers.

## Not every feature exists everywhere yet

The three backends have genuinely different native capabilities (see the
[capability matrix](admin/capability-matrix.md)). Rather than pretending
otherwise, `agentctl` always tells you plainly when something isn't supported:

- **not available** — the platform has no way to do this, and no workaround is known
- **under development** — agentctl knows how, just hasn't built it yet
- **manual workaround available** — a concrete, copyable procedure you can run by hand today

See [Capability Model](reference/capability-model.md) for how this works
internally, and the [Troubleshooting](user/troubleshooting.md) guide for what
each response looks like in practice.

## Transparent by design

Every command that touches the backend supports `--preview`/`--dry-run`,
which prints the exact `incus`/`limactl`/PowerShell command(s) agentctl
would run instead of running them. This isn't just a debugging aid — it's
meant to build confidence in the tool by letting you verify what it
actually does, and it means you don't strictly need to install agentctl
anywhere you don't want to: preview the commands elsewhere, then copy and
run them directly. See [Preview Mode](user/preview-mode.md).

## Where to start

- New user? Go to [Installation](user/installation.md) then [Quickstart](user/quickstart.md).
- Rolling this out to a team? Start at [Deployment Model](admin/deployment-model.md).
