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

That's `--deny-lan` (on by default) doing its job — the sandbox can't reach
other devices on your network. If you genuinely need that, pass `--allow-lan`
on `create`, or set `denyLAN: false` in a profile, understanding the tradeoff.

Note that `--deny-lan` is enforced by *not allowing* the private ranges rather
than by rejecting them, because an Incus reject rule would shadow every allow
rule including DNS. One consequence worth knowing: if an `--allow` domain
resolves to a LAN address, that rule is silently dropped rather than applied,
since "allow this host, but not the LAN" can't be expressed in a single ACL.
`--preview` shows exactly which rules survive.

## Nothing in the sandbox can reach the network / `curl` fails during an agent install

Run the diagnostic first — it walks every layer below and prints which one is
at fault, changing nothing:

```console
$ scripts/diagnose-incus-network.sh <name>
```

The manual version, in order. The first two are host-side and far more common
than a problem with the policy itself.

**0. Rule agentctl out entirely.** If `--no-network-policy` also has no
network, no ACL was ever created or attached, so the cause is below agentctl —
skip to step 1 and treat it as a plain Incus networking problem. Note that
`incus exec` on a **VM** goes over vsock rather than the network, so exec
working tells you nothing about connectivity.

**1. On Fedora/RHEL, check firewalld.** If the guest has an IPv6 address but no
IPv4 one, firewalld is blocking DHCP on the bridge. See
[Incus setup](../admin/providers/incus-setup.md#fedorarhel-add-the-bridge-to-firewalld-trusted-zone-first).

```console
$ incus exec <name> -- ip -br addr      # IPv6 only? -> firewalld
```

**2. Check DNS specifically**, since everything else depends on it:

```console
$ incus exec <name> -- getent hosts example.com
```

If that fails but the instance otherwise looks healthy, the ACL is the
suspect. Confirm by detaching it:

```console
$ incus config device unset <name> eth0 security.acls
$ incus exec <name> -- getent hosts example.com     # works now? -> the ACL
```

**3. Create a sandbox with no policy at all** to get a working baseline you
can compare against:

```console
$ agentctl create debug --image=<ref> --no-network-policy
```

That skips ACL creation and attachment entirely. It is an explicit, visible
escape hatch for exactly this situation — the sandbox gets unrestricted
egress, so use it to diagnose, not as a habit.

**4. Inspect what agentctl would actually apply**, without applying it:

```console
$ agentctl --preview create demo --image=<ref> --agent=claude
```

Every `incus network acl rule add` line is shown verbatim. Note that with
`--deny-lan` on, an `--allow` domain that resolves to a LAN address is
deliberately *omitted* rather than allowed — so a missing rule there is the
policy working, not a bug.

## `start` fails with "incompatible with secureboot"

```
Error: The image used by this instance is incompatible with secureboot. Please set security.secureboot=false on the instance
```

This shouldn't happen on current `agentctl` — `create` disables Secure Boot
by default precisely because most public/community images aren't signed for
it (see [Incus setup](../admin/providers/incus-setup.md)). If you hit this
on an instance created by an older `agentctl` build, fix it directly:
`incus config set <name> security.secureboot=false`, then `agentctl start`
again.

## `create` fails during non-root user provisioning

`agentctl create` runs a bootstrap script inside the instance (right after
network policy is applied) to provision the non-root default user that
`shell`/`exec` use — see
[Profiles & Policies](profiles-and-policies.md#default-non-root-user). Two
ways this can fail:

- **Neither `useradd` nor `adduser` is present.** Some minimal/custom images
  ship neither tool. The bootstrap script fails with a clear error instead of
  silently leaving the instance root-only. There's no workaround short of
  using an image that includes one of them (any mainstream Debian/Ubuntu or
  Alpine-family image does).
- **`sudo` can't be installed.** The script tries `apt-get install -y sudo` or
  `apk add --no-cache sudo` if `sudo` is missing, which needs working network
  egress at create time. If your allowlist is unusually restrictive before
  the instance is fully up, this step can fail — check the instance's egress
  policy, or use an image that already includes `sudo` preinstalled.

Re-running the bootstrap step is safe: it's idempotent and a no-op if the
user already exists, so retrying `agentctl create` (after deleting the failed
instance) or a future retry mechanism won't double-provision anything.

## `shell`/`exec` say "Error: VM agent isn't currently running"

`agentctl shell`/`agentctl exec` need the in-guest `incus-agent` to have
connected, which normally happens within a few seconds of `start` but can
take longer depending on the image and hardware. `shell`/`exec` already
wait up to 30 seconds for it, printing "waiting for the VM agent... to
start" to stderr if it takes a moment — you shouldn't need to do anything
but wait.

If it still fails after 30 seconds, either the guest is taking unusually
long to boot (just retry `agentctl shell`/`exec` again), or the image
doesn't include incus-agent support at all (some minimal or custom images
don't) — in which case `exec`/`shell` won't work on that image, but
`agentctl view` (the console) still will, since it doesn't need the agent.

## `applying network policy` fails with "Duplicate of egress rule N"

```
Error: incus network acl rule add agentctl-<name> egress action=allow destination=<ip> protocol=tcp destination_port=443: exit status 1: Error: Duplicate of egress rule 4
```

This shouldn't happen on current `agentctl` — it was a real bug where two
different allow-domains that happened to resolve to the same IP (common
behind shared CDN infrastructure; e.g. `--agent=claude`'s `claude.ai` and
`*.anthropic.com` allow-domains can share an IP) produced two
byte-identical ACL rules, which Incus rejects on the second one. Fixed by
deduplicating identical rule commands before applying the policy. If you
still hit this on an older `agentctl` build, no workaround is needed beyond
upgrading — there's nothing to fix in your own profile or `--allow` flags.

## `provisioning default user: user bootstrap script exited 1: Error: Instance is not running`

This shouldn't happen on current `agentctl` — it was a real bug where
`create` tried to run the non-root user bootstrap step (which needs `incus
exec`, and `incus exec` requires a running instance) against a freshly
`init`'d instance, which is stopped by design. Fixed by having `create`
briefly start the instance around the bootstrap step and stop it again
afterward — see [Incus setup](../admin/providers/incus-setup.md) for what
that means for `create`'s timing. If you still hit this on an older
`agentctl` build, upgrade; there's nothing to change in your own command or
profile.

## Agent install fails or never finishes

`create --agent=<name>` runs that agent's install command (e.g. `curl ... |
bash`) as soon as the instance is up — see
[Agent Provisioning](agent-provisioning.md). This needs working egress at
install time, so a flaky network mid-install, or an unusually restrictive
`--allow`/profile combination that's missing a domain the install script
itself redirects through, can leave `create` reporting a failure with the
instance left running.

You don't need to redo anything by hand: the very next `agentctl start
<name>` — even a completely ordinary one, with no `--agent` flag — detects
the incomplete install and retries it automatically before doing anything
else. If it keeps failing, the error `create`/`start` printed is the actual
install script's output; check that against whatever domain it's trying to
reach and extend `--allow` if it's egress being blocked, same as any other
blocked-domain issue.

An already-successful install is never repeated: `create --agent=<name>`
records success once the install script exits `0`, and every subsequent
`start` is a no-op with respect to the agent.

## An agent installed successfully but `claude`/`codex`/... is "command not found"

Almost always a `PATH` problem rather than a failed install, and the fix is
already in agentctl — this section is here because instances created by an
older build still carry the symptom.

Agent installers put their binary in `~/.local/bin`, and every distribution
puts that directory on `PATH` from a *login* shell profile
(`/etc/profile.d/` on Fedora, the skel `~/.profile` on Debian). `incus exec`
runs neither a login nor an interactive shell, so none of those were ever
sourced: the install genuinely succeeded and the binary genuinely wasn't
findable.

agentctl now sets `PATH` (and `HOME`/`USER`/`LOGNAME`) explicitly on every
`exec`, and `agentctl shell` opens a real login shell. Check what you're
getting:

```console
$ agentctl exec demo -- sh -c 'echo $PATH'
/home/claude/.local/bin:/home/claude/bin:/usr/local/sbin:...
```

If `~/.local/bin` isn't first, the instance predates the fix. Either
recreate it, or call the binary by its full path
(`agentctl exec demo -- ~/.local/bin/claude`).

The same non-login behavior is why a sandbox's shell could come up with no
prompt, no colors and no locale: `~/.bashrc` was sourced but `/etc/profile`
was not.

## `agentctl shell` has no dotfiles / an empty home directory

`useradd -m` copies `/etc/skel` into a new home directory, and busybox
`adduser` usually does too — "usually" being the problem, since it depends
on how busybox was compiled. agentctl's bootstrap script now copies any
missing `/etc/skel` entry explicitly, and never overwrites one that already
exists.

That runs on every provisioning pass, including the idempotent one for an
account that already exists, so an instance created by an older build is
repaired the next time its user is provisioned rather than staying broken.
`agentctl start <name>` on an instance with a pending agent install is the
usual trigger; otherwise recreate it.

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
