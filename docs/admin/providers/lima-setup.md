# Lima Setup (macOS)

!!! note
    The Lima backend is a capability-table-driven **stub** this milestone —
    every lifecycle command currently responds `UnderDevelopment`. This page
    describes the target setup and current gaps so macOS users know what to
    expect; see the [capability matrix](../capability-matrix.md) for the
    full picture.

## Install (once the backend lands)

```console
$ brew install lima
```

Current stable is Lima v2.2, which added Windows guest support alongside the
macOS/FreeBSD guest support introduced in v2.1.

## Known gaps versus Incus

| Gap | Detail |
|---|---|
| No native network ACL | Lima has no first-class allowlist/ACL object. `agentctl` will report `ManualWorkaround` for `--allow`/`--deny-lan` and print a concrete `pf`-based procedure until this is built natively. |
| Snapshot support experimental | `limactl snapshot` is explicitly marked experimental/unstable upstream; `agentctl` won't build on it until it stabilizes (`NotAvailable`, not `UnderDevelopment`). |
| No first-class GUI console | Lima has historically targeted headless Linux VM use cases. `agentctl` plans a dedicated VNC bridge for `view`, not X11 forwarding — see [Viewing a Sandbox](../../user/view-and-console.md). |

## Worth reading before building against Lima

Lima's v2.0 release was titled "expanding the focus to hardening AI" at
FOSDEM 2026 — a strong signal upstream is already working on problems
adjacent to this project. Before implementing agentctl's own macOS hardening
layer, check what Lima itself now offers; it may cover part of the gap above
natively.

## What agentctl will assume exists (once implemented)

- A working `limactl` on `$PATH`.
- vzNAT or socket_vmnet networking configured, per Lima's own docs, depending
  on whether IP-addressable guest access is needed.
