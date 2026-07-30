# Troubleshooting

## "not available" / "under development" / manual workaround — what do these mean?

`agentctl` checks whether the active provider actually supports what you
asked for before doing anything, and always tells you one of four things:

- **(nothing printed, command just runs)** — supported, proceeding normally.
- **"is under development for provider ..."** (exit code `2`) — agentctl
  knows how to build this, just hasn't yet. Not a platform limitation.
  Nothing to do but wait for it or use a different provider if one supports it.
- **"is not available on provider ..."** (exit code `2`) — the platform
  genuinely has no way to do this, and no workaround is known.
- **"has no native support on provider ..." followed by a numbered plan**
  (exit code `3`) — a manual procedure exists. Apply it by hand, then either
  re-run with `--force-partial` to proceed anyway (accepting that agentctl
  itself isn't enforcing this), or just run the manual steps and use the
  underlying tool directly for this one operation.

See the full [Capability Matrix](../admin/capability-matrix.md) for what's
supported on each provider today.

## `no provider configured`

Run `agentctl config set provider=<incus|lima|hyperv>`, or pass
`--provider=<name>` for a one-off override.

## `exec: "incus": executable file not found in $PATH`

The Incus backend shells out to the `incus` CLI rather than linking a Go SDK
— install and initialize Incus first. See
[Incus setup](../admin/providers/incus-setup.md).

## A LAN request I expected to succeed is being blocked

That's `--deny-lan` (on by default) doing its job — it blocks RFC1918 and
link-local ranges specifically so a sandbox can't reach other devices on your
network. If you genuinely need that, pass `--allow-lan` on `create`, or set
`denyLAN: false` in a profile, understanding the tradeoff.

## An `--allow` entry stopped working after a while

Domain-based allow rules are resolved to IP addresses when the policy is
applied — a snapshot, not dynamic DNS-aware filtering. If the target rotates
IPs (common behind a CDN), re-apply the policy (re-run `create`'s network
setup, or recreate the instance) to refresh it.

## Exit code reference

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Generic runtime error |
| `2` | Capability gap (not available / under development) |
| `3` | Manual workaround available but not applied/forced |
