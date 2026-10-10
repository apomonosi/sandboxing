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
install time, so a flaky network mid-install, or an install script that
starts fetching from a host agentctl's registry doesn't list yet (each
agent's allowlist covers the hosts its installer used when it was last
checked — see [Agent Provisioning](agent-provisioning.md#built-in-registry)),
can leave `create` reporting a failure with the instance left running.

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

## `apt`, `pip`, `npm` or `git clone` can't reach its server

Egress is default-deny, so package managers and git only work against
hosts on the instance's allowlist — and they usually need more than one
host each (pip downloads from `files.pythonhosted.org`, not `pypi.org`;
GitHub release downloads come from `release-assets.githubusercontent.com`).
Create the instance with the matching
[allow presets](profiles-and-policies.md#allowing-package-sources-and-git-hosts),
e.g. `--allow-preset=apt,pypi,github`, and add anything else with `--allow`.
The tool's own error usually names the host it couldn't reach (`apt-get
update` lists each failed URL); on Lima, the instance's egress proxy log
(see [below](#lima-a-request-from-the-sandbox-gets-403-denied-by-agentctl-network-policy))
names every refused host:port. The allowlist is set when the instance is
created, so recreate it with the extra entries. (Tools requested with
`--package`/`packages:` are different: on Incus they're installed during
`create`, before the network policy is attached — see
[Packages](../reference/profile-schema.md#ordering-and-what-it-means-for-the-network-policy) —
so they don't need a preset.)

## An `--allow` entry stopped working after a while

On Incus, domain-based allow rules are resolved to IP addresses when the
policy is applied — a snapshot, not dynamic DNS-aware filtering. If the
target rotates IPs (common behind a CDN), re-apply the policy (re-run
`create`'s network setup, or recreate the instance) to refresh it. (On
Lima this can't happen: its egress proxy resolves the hostname on every
connection.)

## Lima: a request from the sandbox gets `403` / "denied by agentctl network policy"

On Lima, the sandbox's traffic goes through a per-instance egress proxy
(see [Lima setup](../admin/providers/lima-setup.md#network-policy-enforcement)),
which answers a refused destination with `403 Forbidden` and the reason —
curl shows it as `CONNECT tunnel failed, response 403` for HTTPS. Every
decision is logged on the Mac in
`~/.config/agentctl/lima/<name>/egress-proxy.log`, so that's the quickest
way to see exactly which host:port was refused and why:

```
2026-10-07T19:08:50Z deny CONNECT downloads.example.com:443: not in the allowlist
```

Allow rules match the hostname the client asked for, so a site that
redirects to another hostname (a download CDN, say) needs that hostname
allowlisted too. Add it with `--allow` and recreate the instance — and the
same reasons as on Incus apply to "deny-LAN is on" refusals.

## Lima: a tool can't connect at all ("Connection refused"), even to an allowlisted host

That tool isn't using the instance's proxy. On Lima, the sandbox's only way
out is the egress proxy its `http_proxy`/`https_proxy` variables point at;
a connection that doesn't go through it is refused by design, allowlisted
destination or not (that's what stops a compromised agent from simply
ignoring the proxy). Configure the tool to use an HTTP proxy — the
`https_proxy` value in the sandbox's environment — or use a tool that
honors those variables. Some common cases: Node's built-in `fetch` and
`http`/`https` modules only honor them with `NODE_USE_ENV_PROXY=1` (or
`--use-env-proxy`; Node 22.21+/24.5+); `git` over SSH needs an ssh
`ProxyCommand` (or use the HTTPS remote); anything UDP-based has no way out.

## Lima: `agentctl start` refuses an instance that's "already running, but agentctl didn't start it under its network sandbox"

The instance was started some other way — a plain `limactl start`, for
instance — so its egress is unrestricted, and agentctl won't pretend
otherwise by adopting it. Stop it and start it with agentctl:

```console
$ agentctl stop <name>
$ agentctl start <name>
```

## Lima: `agentctl start` refuses an instance "registered with launchd to start automatically"

`limactl autostart enable` (formerly `start-at-login`) has launchd start the
instance at login — outside agentctl's sandbox, with unrestricted egress.
Unregister it with `limactl autostart disable <name>` and start it with
agentctl instead.

## Lima: `agentctl start` refuses because of the instance's configuration

agentctl checks Lima's effective configuration for the instance before
every start and refuses anything that would undermine its network policy:

- **"has additional networks configured"** — a vzNAT or socket_vmnet
  network, whose traffic doesn't go through the sandboxed Lima hostagent.
  agentctl removes these from the instance itself, so this means one is
  coming from `~/.lima/_config/default.yaml` or `override.yaml`; remove it
  there.
- **"ssh.localPort ...", "propagateProxyEnv ...", or an `http_proxy`/
  `https_proxy`/`no_proxy` mismatch** — the same files overriding
  settings agentctl pins for the instance; remove the override.
- **"uses vmType ..."** — the instance doesn't use Lima's `vz` VM type
  (the macOS default), and agentctl's enforcement is only verified for
  `vz`. Recreate it from a template that doesn't force another VM type.
- **"mounts ... writable, which overlaps agentctl's state directory"** —
  a writable mount (typically of your whole home directory) would let the
  guest edit its own egress policy in `~/.config/agentctl`. Make that
  mount read-only, or share a narrower directory instead.
- **"beyond what the instance's own configuration mounts"** — a mount that
  `~/.lima/_config/default.yaml` or `override.yaml` adds; see
  [below](#lima-start-fails-with-beyond-what-the-instances-own-configuration-mounts).
- **"would forward the host's SSH agent" (or "X11 display")** — Lima would
  forward one of them into the guest; see
  [below](#lima-start-fails-with-would-forward-the-hosts-ssh-agent-or-x11-display).

## Lima: `start` fails with "beyond what the instance's own configuration mounts"

```
agentctl: Lima would mount /Users/you at /Users/you (read-only) in instance "demo", beyond what the instance's own configuration mounts, so agentctl won't start it. ...
```

Lima merges the `mounts` in `~/.lima/_config/default.yaml` and
`override.yaml` (the error names the exact files) into every instance it
starts, agentctl's included. A mount you set up there for your other Lima VMs
would reach this sandbox too, without passing any of agentctl's mount checks,
so `start` checks what Lima would actually mount and stops when that's more
than the instance itself declares (see
[Lima setup](../admin/providers/lima-setup.md#mounts)).

Remove the entry from that file. If this sandbox should see the directory,
name it to agentctl instead:

```console
$ agentctl start demo --mount ~/src/project:/workspace:w
```

The check runs on every `start`, so editing those files can stop a sandbox
that started fine before.

## Lima: `start` fails with "would forward the host's SSH agent" (or "X11 display")

```
agentctl: Lima would forward the host's SSH agent into instance "demo", so agentctl won't start it: anything in the guest could use it to authenticate as you. ...
```

Lima forwards your SSH agent into an instance when its `ssh.forwardAgent` is
on, and your X11 display when its `ssh.forwardX11` is. The agent would let
anything in the sandbox authenticate as you, and X11 would give it your
display, so agentctl creates sandboxes with both off, and `start` stops if Lima
would turn one back on (see
[Lima setup](../admin/providers/lima-setup.md#ssh-agent-and-x11-forwarding)).
That happens one of two ways:

- **`~/.lima/_config/override.yaml` turns it on** (the error names the exact
  file). Lima applies `override.yaml` over every instance's own settings, so
  remove it there. If you want agent forwarding for your other Lima VMs, set it
  in `default.yaml` instead: an agentctl sandbox turns it off itself, and
  `default.yaml` doesn't override that.
- **The instance doesn't turn it off itself**, because an older agentctl
  created it, or `limactl` did, so `default.yaml` or the template still reaches
  it. Turn it off for this instance, then `start` it again:

    ```console
    $ limactl edit --set '.ssh.forwardAgent = false' --start=false demo
    ```

    For X11, the same with `.ssh.forwardX11`.

The check runs on every `start`, so editing `override.yaml` can stop a sandbox
that started fine before.

## Lima: "network confinement check failed"

After every start, agentctl tries a direct connection out from inside the
guest, bypassing the proxy, to confirm it's refused. This error means it
got through — so Lima's processes weren't running under agentctl's macOS
sandbox as expected — and agentctl stopped the instance rather than leave
it running unprotected. Please report it, with your macOS version and
`limactl --version`.

## Lima: an instance created by an older agentctl has no network access

Instances created before Lima network enforcement existed have no recorded
egress policy, so their proxy denies everything (failing closed, as on
Incus with an empty allowlist). Recreate the instance with the `--allow`
flags or profile it should have.

## Exit code reference

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Generic runtime error |
| `2` | Capability gap (not available / under development) |
| `3` | Manual workaround available but not applied/forced |
