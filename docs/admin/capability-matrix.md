# Capability Matrix

This mirrors the `provider.Table` each backend actually returns from
`Capabilities()` (see `internal/provider/{incus,lima,hyperv}/capability.go`).
**Incus and Lima are both real implementations**; Hyper-V is still a
capability-table-driven stub, so almost everything on that row is a "not
wired up yet," not a platform limitation — see the notes below the table
for which cells are genuine platform gaps.

| Feature | Incus | Lima | Hyper-V |
|---|---|---|---|
| create / start / stop / delete / list / status | Supported | Supported | Under development |
| exec / shell | Supported | Supported | Under development |
| view (console) | Supported | Under development | Under development |
| snapshot create/list/restore/delete | Supported | **Not available** | Under development |
| network.acl (`--allow`) | Supported | **Manual workaround** | Under development |
| network.deny-lan (`--deny-lan`) | Supported | **Manual workaround** | Under development |
| network.port-publish (`--port`) | Supported | Supported | **Manual workaround** |
| fs.mount (`--mount`) | Supported | Supported | **Manual workaround** |
| fs.mount.readonly | Supported | Supported | **Manual workaround** |
| image.pull | Supported | **Not available** | Under development |
| image.build | Supported | Under development | Under development |
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
- **Lima: image.pull → Not available.** Lima has no local named image store
  to pull into ahead of `create`; a template's base image resolves lazily,
  per-instance, inside `create`/`start` itself — there's no daemon-side
  store to wire up to, unlike Incus's `incus image copy`.
- **Lima: logs.network → Not available.** There's no egress log source to
  read from without the network.acl workaround's `pf` anchor in place first.
- **Hyper-V: network.port-publish → Manual workaround.** Hyper-V has no
  built-in "publish a port" primitive; agentctl prints a NAT-switch +
  `netsh interface portproxy` procedure.
- **Hyper-V: fs.mount / fs.mount.readonly → Manual workaround.** Hyper-V has no
  host-directory share primitive at all: no virtiofs, no 9p, and no
  VMware-Tools-style shared folder. Enhanced Session Mode's drive redirection
  is RDP-based and aimed at Windows guests. Sharing a folder with a Linux guest
  therefore goes over SMB on the guest network, which agentctl prints as a
  procedure. Two consequences worth understanding before relying on it: the
  guest ends up holding host SMB credentials (a lateral-movement risk in its
  own right — use a dedicated low-privilege account), and because the mount is
  network traffic it needs a deliberate hole in `--deny-lan` for the host's
  vSwitch address. A native path — a host-side SFTP server serving only the
  named directories, which is what Lima does by default — is designed in
  `filesystem-access.plan.md` but not built: it needs agentctl to gain a
  host-side daemon, and the Hyper-V backend's own lifecycle is still a stub.

Every other `Under development` cell reflects a backend that supports the
feature natively (New-VM/Start-VM, Extended Port ACLs, Standard/Production
checkpoints, VMConnect for Hyper-V) — agentctl just hasn't wired up the
integration yet. On Lima specifically, `view` stays `Under development` by
deliberate choice this milestone: a real GUI console needs a VNC bridge
that doesn't exist yet (Lima's default `vz` backend has no display concept
to attach to), which is a separately-scoped piece of work, not something
silently dropped.

## `--preview`/`--dry-run` availability

`--preview` shows the backend command(s) an operation would run, built from
the exact same code path the real dispatch uses (see
[Capability Model](../reference/capability-model.md#preview-and-drift)).
It only ever has something to show for an operation the active provider
actually supports — the capability gate above always runs first. Today
that means:

| Provider | `--preview` |
|---|---|
| Incus | Live — every command in the table above that's `Supported` |
| Lima | Live — every command in the table above that's `Supported` |
| Hyper-V | Nothing to preview yet (no operations are `Supported` yet) |

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
