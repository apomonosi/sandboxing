# Lima Setup (macOS)

This backend is fully implemented for the operations that map onto Lima's
own primitives, and enforces the same network policy as the Incus backend
(default-deny egress allowlist, LAN blocked by default) — see
[Network policy enforcement](#network-policy-enforcement) below for how,
since Lima has no ACL object of its own to do it with. Snapshots remain a
genuine platform gap, and a GUI console (`agentctl view`) is deliberately
not built yet.

## Install

```console
$ brew install lima
```

Current stable is Lima v2.2, which added Windows guest support alongside the
macOS/FreeBSD guest support introduced in v2.1.

## How agentctl customizes instances

`agentctl create` runs one `limactl create --name=<name> --tty=false --set
'<yq-expression>' [--set ...] <image>` invocation, where `<image>` is a Lima
template reference (`--image=template://ubuntu-lts`, a URL, or a local
template path) and each `--set` translates one structured field of the
create request: CPU/memory/disk limits, mounts, and published ports.

> **Not yet exercised against a live `limactl` install** — but checked
> against Lima's source: `create` accepts repeated `--set` flags and
> applies all of them, and agentctl's expressions (including the `+=`
> array appends for mounts/ports, on a template that doesn't declare those
> keys) evaluate correctly in the same yq library version Lima embeds,
> with Lima's restrictions on. Should `--set` misbehave on a real install,
> agentctl falls back to `limactl create` (bare) followed by one `limactl
> edit --set '<expr>' <name>` call per field before the first `start` —
> same overall behavior, just more invocations. Either way, `agentctl
> create` never leaves the instance in a partially-configured state you'd
> need to fix by hand.

agentctl always forces these fields, whatever the template says:

- `.user.name` — the sandbox's non-root default user (`agent` unless
  `--agent=<name>` or an explicit override sets it otherwise), for
  consistency with the Incus backend's own non-root-by-default behavior.
- `.user.sudo = true` — passwordless sudo, so `agentctl exec/shell --root`
  never hangs on an unexpected password prompt.
- `.ssh.forwardAgent = false` and `.ssh.forwardX11 = false` — so Lima doesn't
  forward your SSH agent or X11 display into the sandbox (see
  [SSH agent and X11 forwarding](#ssh-agent-and-x11-forwarding)).

`agentctl exec`/`shell` run as that user via `limactl shell <name> --
<command>`; `--root` runs `sudo -n <command>` (exec) or `sudo -i` (shell)
on top of it. Unlike the Incus backend, there's no separate bootstrap
script or uid/home lookup — Lima's own cloud-init `user:` handling
provisions the non-root account at first boot, and `limactl shell` already
resolves to it.

### Mounts

agentctl sets an instance's mounts itself — at `create`, and on `start
--mount`/`--mount-only`/`--mount-none` — by resetting `.mounts` before adding
the directories you named (see
[Host filesystem access](../../user/profiles-and-policies.md#host-filesystem-access)).
The reset matters: nearly every stock Lima template inherits
`template:_default/mounts`, which shares your entire home directory with the
guest, read-only.

Lima also merges the mounts in `~/.lima/_config/default.yaml` and
`override.yaml` (under `$LIMA_HOME`, if set) into every instance each time it
starts one, and an instance can't opt out. Those never pass through agentctl's
mount checks, so `agentctl start` first reads what Lima will actually mount
(`limactl list <name> --json` — on macOS, as part of the effective-configuration
check under [Network policy enforcement](#network-policy-enforcement)) and
refuses to start the instance if that goes beyond the instance's own
configuration: an extra directory, a different host directory at one of its
mount points, or a read-only mount made writable. Sharing less is fine. If
`start` refuses, see
[Troubleshooting](../../user/troubleshooting.md#lima-start-fails-with-beyond-what-the-instances-own-configuration-mounts).

An instance created before agentctl reset the template's mounts still declares
the home-directory mount in its own configuration, so this check doesn't catch
it; `agentctl start <name> --mount-none` (or `--mount ...`) replaces it. The
check only runs on `agentctl start`; a plain `limactl start` skips it.

### SSH agent and X11 forwarding

Lima can forward your SSH agent (`ssh.forwardAgent`) and your X11 display
(`ssh.forwardX11`) into an instance. Both are off by default, and agentctl
keeps them off. With your agent, anything in the sandbox could authenticate as
you wherever your keys are accepted — push to your GitHub repositories, log in
to your servers — without ever reading a key file. Lima forwards it over its
own SSH connection too, not only during `agentctl shell`/`exec`, and links it
at `/run/host-services/ssh-auth.sock` in the guest for as long as the instance
runs. X11 is excluded for the reasons in the
[security model](../security-model.md#why-raw-x11-forwarding-is-excluded).

Many people turn agent forwarding on for all their Lima VMs in
`~/.lima/_config/default.yaml`. That doesn't reach an agentctl sandbox: Lima
only applies `default.yaml` to fields an instance leaves unset, and `create`
sets both to `false`. `override.yaml` beats an instance's own settings,
though, so `agentctl start` first checks what Lima will actually do
(`limactl list <name> --json`) and refuses to start an instance that would
forward either one. That also catches an instance created before agentctl
turned forwarding off, which `default.yaml` still reaches. If `start` refuses,
see
[Troubleshooting](../../user/troubleshooting.md#lima-start-fails-with-would-forward-the-hosts-ssh-agent-or-x11-display).
Like the mount check, this only runs on `agentctl start`.

## Network policy enforcement

On Incus, network policy is a per-instance network ACL enforced by the host
on the instance's NIC. Lima has no equivalent object, and the obvious
substitute — a macOS `pf` rule on the VM's vzNAT/vmnet interface — doesn't
hold up: Lima *always* also gives the guest a user-mode NIC (`eth0`,
`192.168.5.15`) whose traffic never crosses a host interface as guest
traffic. Lima's hostagent process re-creates each of the guest's
connections as an ordinary connection *from the host*, so `pf` can't tell
it apart from your own traffic, and a guest with root (every agentctl
guest has passwordless `sudo`) could simply route around a vzNAT filter.

That same design makes the hostagent a single, host-side choke point for
all of the guest's traffic, and that's where agentctl enforces policy:

1. **`agentctl start` runs `limactl start` under a macOS sandbox profile**
   (`sandbox-exec`), which the hostagent and everything it spawns inherit.
   The profile denies every outbound connection except to two loopback
   ports: the instance's SSH forward and its egress proxy. When the guest
   tries to reach anything else, the hostagent's connection is refused and
   the guest sees "connection refused" — no matter what the guest does
   with its own routes or firewall, because none of this runs inside the
   guest.
2. **A per-instance egress proxy applies the allowlist.** macOS sandbox
   profiles can only allow "localhost" or "any host", never specific
   addresses, so the allowlist itself lives in a small HTTP/HTTPS proxy
   that agentctl runs in the background for each running instance (it's
   agentctl itself, re-run as a hidden `agentctl egress-proxy` command).
   The guest is pointed at it through `http_proxy`/`https_proxy` in its
   `/etc/environment`, and reaches it at `192.168.5.2` (Lima's address
   for the host). The proxy enforces the same semantics as the Incus
   ACL: everything not on the allowlist is refused; with `--deny-lan`
   (the default) a destination in `10.0.0.0/8`, `172.16.0.0/12`,
   `192.168.0.0/16`, `169.254.0.0/16` (or their IPv6 counterparts) is
   refused *even if* an allow rule matches it; and with `--allow-lan`
   those ranges are reachable on any port whether or not an allow rule
   names them. Destinations on the Mac itself (loopback) are always
   refused.
3. **Every start is checked.** Before booting, agentctl re-applies the
   instance settings this depends on (no extra network interfaces, the
   pinned SSH port, the proxy environment) with `limactl edit`, then reads
   back Lima's *effective* configuration and refuses to start if anything
   — a template, or `~/.lima/_config/default.yaml`/`override.yaml` —
   would add a way around it. After booting, it tries a direct connection
   out from inside the guest; if that succeeds, it stops the instance and
   reports an error rather than leave it running unprotected.

`agentctl --preview start <name>` shows all of it: the `limactl edit`, the
proxy launch, the full sandbox profile on the `sandbox-exec` command line,
and the in-guest check.

### What this means in practice

- **Clients have to use the proxy.** Anything that honors
  `http_proxy`/`https_proxy` — curl and wget (and so the `curl | sh`
  installers `--agent` runs), apt, pip, npm, git over HTTPS, most language
  HTTP clients — works normally; check the proxy support of anything else you
  rely on (Node's built-in `fetch`, for one, ignores these variables unless
  told otherwise). Connections that don't go through the proxy (raw TCP,
  UDP, `git` over SSH, a client that ignores proxy settings) are refused
  even when the destination is allowlisted — never silently allowed. On Incus,
  by contrast, an allowlisted destination is reachable by any TCP client.
  The proxy carries any TCP stream via HTTP `CONNECT`, so a client that
  can be configured to use an HTTP proxy (e.g. ssh's `ProxyCommand`) can
  still reach an allowlisted non-HTTP port.
- **Allow rules match hostnames, at connection time.** Incus resolves
  allowlisted domains to IP addresses once, when the policy is applied;
  the Lima proxy checks the hostname the client asked for and resolves it
  on every connection. So a `*.example.com` rule you write covers every
  subdomain (not just whatever the apex resolved to), CDN address changes
  don't make a rule go stale, and a hostname that shares an IP with an
  allowlisted one isn't let through by accident. A connection by bare IP
  address is only allowed if that IP itself is on the allowlist. (The
  built-in agent lists, presets and profiles never use wildcards, so they
  allow the same hosts here as on Incus.)
- **DNS goes through the Mac's resolver, and only there.** The guest's DNS
  queries are answered by Lima's host resolver, i.e. macOS's own DNS
  settings; the guest can't send DNS to any other server. That's
  narrower than Incus's default (DNS to any server on port 53), and it's
  also why a profile's `network.dns` (`servers` or `disabled`) is
  refused on this backend: agentctl can't narrow or switch off that path,
  so `create` fails with an error rather than quietly ignoring the
  setting.
- **`--no-network-policy` means a plain `limactl start`.** An instance
  created with it boots without the sandbox or the egress proxy —
  unrestricted egress, the counterpart of Incus attaching no ACL. The
  choice is recorded at create time, and `--preview start` shows the
  plain start.
- **The Mac's own services are off-limits.** A stock Lima guest can reach
  anything listening on the Mac's loopback interface (databases, dev
  servers) at `192.168.5.2`; under agentctl it can only reach its own
  proxy there.
- **Start instances with agentctl, not `limactl`.** The sandbox only
  exists when agentctl starts the instance. A plain `limactl start` (or
  launchd, via `limactl autostart`) runs it with unrestricted egress.
  `agentctl start` refuses an instance registered with `limactl
  autostart`, and refuses to adopt a running instance it didn't start
  itself.
- **Each instance's decisions are logged** to
  `~/.config/agentctl/lima/<name>/egress-proxy.log` (one line per allowed
  or denied connection), alongside its policy file. `agentctl logs`
  doesn't read this yet.

### Requirements

- macOS (enforcement is built on `/usr/bin/sandbox-exec`, which every
  macOS release still ships). On a Linux host, the Lima backend reports
  `network.acl`/`network.deny-lan` as not available; use the Incus backend
  there.
- The `vz` VM type — Lima's default on current macOS. agentctl refuses to
  start an instance using another VM type, whose networking it hasn't
  verified runs inside the sandboxed hostagent.
- No additional Lima networks (vzNAT, socket_vmnet) on agentctl
  instances: their traffic doesn't pass through the hostagent. agentctl
  removes them from the instance's own configuration, and refuses to start
  if `~/.lima/_config/default.yaml` or `override.yaml` adds one back.
- No *writable* mount covering agentctl's state directory
  (`~/.config/agentctl`), where each instance's egress policy lives — a
  guest that could write there could rewrite its own allowlist. agentctl
  replaces a template's own mounts with the ones you asked for, and
  refuses to start an instance whose effective mounts (including any
  `~/.lima/_config` adds) would make that directory writable — though any
  mount `~/.lima/_config` adds is refused anyway (see [Mounts](#mounts)).

## Known gaps versus Incus

| Gap | Detail |
|---|---|
| Egress needs proxy-aware clients | See [What this means in practice](#what-this-means-in-practice): traffic that doesn't use the instance's HTTP proxy is refused rather than filtered. |
| No `network.dns` policy | A guest's DNS always goes through Lima's host resolver (the Mac's own DNS settings); `servers` and `disabled` can't be enforced, so a profile that sets them is refused at `create`. |
| Snapshot support experimental | `limactl snapshot` is explicitly marked experimental/unstable upstream; `agentctl` won't build on it until it stabilizes (`NotAvailable`, not `UnderDevelopment`). |
| No local image store | Lima resolves a template's base image lazily, per-instance, inside `create`/`start` itself — there's no separate named store to pull into ahead of time the way `incus image copy` gives you, so `image.pull` is `NotAvailable` rather than `Supported`. |
| No first-class GUI console | Lima has historically targeted headless Linux VM use cases. `agentctl` plans a dedicated VNC bridge for `view`, not X11 forwarding — see [Viewing a Sandbox](../../user/view-and-console.md). |

## Worth reading before extending this further

Lima's v2.0 release was titled "expanding the focus to hardening AI" at
FOSDEM 2026 — a strong signal upstream is already working on problems
adjacent to this project. A native egress filter for Lima's user-mode
network has been proposed upstream
([lima-vm/lima#4326](https://github.com/lima-vm/lima/pull/4326)) but isn't
merged; if something like it ships, it could replace agentctl's
sandbox-plus-proxy arrangement with an enforcement point inside Lima
itself.

## What agentctl assumes exists

- A working `limactl` on `$PATH`.
- Published ports (`--port host:guest`) reach the macOS host via Lima's own
  `portForwards:` mechanism. **Unverified:** whether Lima's default
  `hostIP` binding is loopback-only (LAN-unreachable, unlike Incus's
  `0.0.0.0` proxy-device default) — confirm against a live install before
  relying on LAN-reachability for a published port on this backend.

## Verify

```console
$ agentctl config set provider=lima
$ agentctl create test --image=template://ubuntu-lts --allow=example.com:443
$ agentctl start test
$ agentctl exec test -- whoami        # -> agent (non-root default)
$ agentctl exec --root test -- whoami # -> root
$ agentctl exec test -- curl -fsS -o /dev/null https://example.com      # allowed, via the proxy
$ agentctl exec test -- curl -fsS -o /dev/null https://www.wikipedia.org # refused by the proxy (403)
$ agentctl exec test -- curl --noproxy '*' https://example.com          # refused: no way around the proxy
$ agentctl delete test --force
```

## Testing this backend for real

`internal/provider/lima/integration_test.go` (`//go:build integration`,
gated further at runtime by `AGENTCTL_INTEGRATION_LIMA=1`) exercises the
full create/start/exec/status/stop/delete lifecycle against a real
`limactl` install, plus the network policy from inside a real guest:
allowlisted HTTPS through the proxy, and refusals for everything else —
including a root user flushing the guest's firewall and connecting
directly, UDP straight to a public resolver, and a service on the Mac's
loopback. This is where every "unverified" note above actually gets
resolved. Unlike the Incus integration suite (blocked from standard
GitHub-hosted Linux runners by no nested virtualization), Lima's default
`vz` backend works on standard `macos-latest` GitHub-hosted runners with no
special setup, so `.github/workflows/ci-integration-lima.yml` can run
there directly (currently `workflow_dispatch`-only while timing/flakiness
gets proven out).
