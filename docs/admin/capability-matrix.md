# Capability Matrix

This mirrors the `provider.Table` each backend actually returns from
`Capabilities()` (see `internal/provider/{incus,lima,hyperv}/capability.go`).
It reflects this milestone: **Incus is the one real implementation**; Lima
and Hyper-V are capability-table-driven stubs, so almost everything on those
two rows is a "not wired up yet," not a platform limitation — see the notes
below the table for which cells are genuine platform gaps.

| Feature | Incus | Lima | Hyper-V |
|---|---|---|---|
| create / start / stop / delete / list / status | Supported | Under development | Under development |
| exec / shell | Supported | Under development | Under development |
| view (console) | Supported | Under development | Under development |
| snapshot create/list/restore/delete | Supported | **Not available** | Under development |
| network.acl (`--allow`) | Supported | **Manual workaround** | Under development |
| network.deny-lan (`--deny-lan`) | Supported | **Manual workaround** | Under development |
| network.port-publish (`--port`) | Supported | Under development | **Manual workaround** |
| image.pull / image.build | Supported | Under development | Under development |
| logs.network / logs.exec | Under development | **Not available** (network only) / Under development | Under development |

## Reading the "genuine platform gap" cells

These are the only entries where the gap is in the *platform*, not just in
agentctl's own wiring:

- **Lima: network.acl / network.deny-lan → Manual workaround.** Lima has no
  native egress-allowlist/ACL object. `agentctl` prints a `pf`-anchor-based
  procedure to achieve the same effect by hand until this is built natively
  (possibly on top of whatever Lima's own "hardening AI" initiative ships —
  see [Lima setup](providers/lima-setup.md)).
- **Lima: snapshot.\* → Not available.** `limactl snapshot` is explicitly
  experimental/unstable upstream; agentctl won't build on it until it
  stabilizes.
- **Lima: logs.network → Not available.** There's no egress log source to
  read from without the network.acl workaround's `pf` anchor in place first.
- **Hyper-V: network.port-publish → Manual workaround.** Hyper-V has no
  built-in "publish a port" primitive; agentctl prints a NAT-switch +
  `netsh interface portproxy` procedure.

Every other `Under development` cell reflects a backend that supports the
feature natively (New-VM/Start-VM, Extended Port ACLs, Standard/Production
checkpoints, VMConnect for Hyper-V; create/start/stop and port forwarding for
Lima) — agentctl just hasn't wired up the integration yet.

## Where this comes from

Run any command against an unsupported feature and agentctl prints the same
information live, plus (for manual-workaround cells) the actual copyable
procedure:

```console
$ agentctl --provider=lima create demo --image=foo --allow=example.com
agentctl: network.acl has no native support on provider "lima".
Lima has no native egress allowlist primitive; agentctl cannot enforce this automatically yet.

Manual workaround (macOS host, per Lima instance):
...
```
