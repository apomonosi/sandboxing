# Security & Threat Model

## What agentctl defends against

Two distinct threats, both taken seriously:

1. **Host escape.** The agent (or something it's tricked into running)
   breaking out of its sandbox onto the machine running it. This is the
   threat most "run an agent safely" tooling focuses on, and it's why
   `agentctl` uses real hardware-virtualized isolation (Incus VMs — not
   containers, not gVisor-style syscall interception) rather than a weaker
   boundary.
2. **Lateral movement against the local network.** The agent attacking other
   devices on the same network as the machine it's running on — printers,
   NAS boxes, internal admin panels, coworkers' laptops. Multiple sources in
   this space converge on the same point: **isolation alone doesn't stop
   this.** Two sandboxes sharing no kernel is great against kernel exploits,
   but if the sandbox has network reach, a compromised agent can still scan
   or attack other hosts on the LAN unless egress is locked down
   independently of the isolation layer itself.

That second threat is why `agentctl` treats network policy as a first-class,
independently capability-gated concern (`ApplyNetworkPolicy` is its own
`Provider` method, not folded into `Create`), and why the defaults are:

## Default-deny egress, LAN blocked by default

- **`--deny-lan` defaults to on.** Every sandbox blocks egress to
  RFC1918 (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) and link-local
  (`169.254.0.0/16`) ranges unless explicitly opted out (`--allow-lan`).
  Opting out should be a deliberate, reviewed decision — see
  [Distributing Profiles Org-Wide](distributing-profiles.md).
- **Internet egress is default-deny with an explicit allowlist.** A sandbox
  can only reach domains listed in `--allow`/a profile's `allow` entries;
  everything else is rejected.
- **Domain-based allow rules are IP snapshots, not DNS-aware filtering.**
  Incus (and any IP-based ACL mechanism) can only match on resolved IP
  addresses, not domain names. `agentctl` resolves each allowed domain at
  policy-apply time; if the target rotates IPs (common behind a CDN), the
  rule goes stale until the policy is re-applied. A DNS-filtering proxy that
  stays dynamically in sync is a possible future improvement, not built now.

## Why raw X11 forwarding is excluded

`agentctl view` never forwards X11. An X server has no isolation between
clients — any client on a display can generally read the keystrokes and
screen contents of every other client on that display. Forwarding a
potentially-compromised agent's X session onto a shared display would hand
it exactly the cross-boundary access this tool exists to prevent. See
[Viewing a Sandbox](../user/view-and-console.md) for what `agentctl view`
does instead per provider.

## What's honest about current limitations

`agentctl` is explicit, via its [capability model](../reference/capability-model.md),
about what it can and can't yet enforce per backend — see the
[capability matrix](capability-matrix.md). Two backends (Lima, Hyper-V) are
stubs this milestone; treat any claim of protection on those platforms as
not yet real until their capability entries say `Supported`.

## Observability is explicitly postponed

Structured, correlated audit tooling — connecting an agent's high-level
intent (from LLM prompts/tool calls) to its low-level system effects
(network connections, file access, processes spawned) via boundary tracing —
is a substantial, separate effort (the kind of thing eBPF-based tools like
AgentSight do on Linux, with ETW/Sysmon as the natural analog on Windows and
Apple's Endpoint Security Framework on macOS). It is **not built in this
milestone.** `agentctl logs` exists as a command surface today, but every
provider currently reports it `UnderDevelopment`. This is deliberate scoping,
not an oversight — see the project's stated out-of-scope list.

## Auth/RBAC

`agentctl` does not implement its own authentication or access control for
who may operate a shared backend. On Incus, this means whatever OS/Incus
group membership (`incus-admin`) already governs who can run `incus`
commands governs who can run `agentctl` against it.
