# Authoring Profiles & Policies

A **profile** is a named, reusable bundle of network policy, resource limits,
and mount policy — author it once, reference it by name from `create
--profile=<name>`. See [Profile Schema](../reference/profile-schema.md) for
the full field reference; this page walks through the important parts.

## Network policy

```yaml
spec:
  network:
    denyLAN: true
    allow:
      - domain: "pypi.org"
        ports: [443]
    ports:
      - host: 8080
        guest: 8080
        protocol: tcp
```

- **`denyLAN: true`** (the default) blocks egress to RFC1918 and link-local
  ranges, so the sandbox can't reach other devices on your network — printers,
  NAS boxes, internal admin panels, coworkers' laptops. Only set this to
  `false` (or pass `--allow-lan`) if you specifically need the sandbox to
  reach something on your LAN, and understand the tradeoff.
- **`allow`** is an egress allowlist; everything not matched is denied.
  Domains are resolved to IP addresses when the policy is applied — this is a
  snapshot of the domain's current IPs, not dynamic DNS-aware filtering. If a
  target rotates IPs (common behind a CDN), re-apply the policy to refresh it.
- **`ports`** are Docker-style host:guest port publishes, for when you need to
  reach something the sandbox is running (e.g. a web UI) from your host.

## Resources and mounts

```yaml
spec:
  resources:
    cpuCores: 2
    memory: "4GiB"
    diskSize: "20GiB"
  mounts:
    - hostPath: "~/agentctl/workspaces/{{.Name}}"
      guestPath: "/workspace"
      writable: true
  mountPolicy:
    createMissing: true
```

`hostPath` supports `~` expansion and a `{{.Name}}` template substituted with
the instance name — the built-in profiles use this so each instance gets its
own workspace directory automatically.

## Host filesystem access

**A sandbox sees nothing of your filesystem except the directories you name.**
Out of the box that is one directory: the per-instance workspace above.
`writable` defaults to `false`, so a directory is read-only unless you say
otherwise, and `$HOME` itself can never be mounted.

Name directories with `--mount`, on `create` or on `start`:

```console
$ agentctl create demo --image=… --mount ~/src/project:/workspace:w
$ agentctl start demo --mount ~/src/other:/workspace    # read-only
$ agentctl start demo --mount-none                       # no host access at all
```

The syntax is `hostPath[:guestPath][:w|:ro]`. Omit the guest path and it
defaults to the host path; omit the suffix and the mount is read-only. This
mirrors Lima's own `--mount`, `--mount-only`, `--mount-none` and
`--mount-writable` flags.

### One sandbox, many projects

Mounts apply per boot, so a single sandbox can be rebound to a different
project instead of building one VM per project:

```console
$ agentctl create work --image=…                          # no project bound to it
$ agentctl start work --mount ~/src/alpha:/workspace:w
$ agentctl stop work
$ agentctl start work --mount ~/src/beta:/workspace:w     # same VM, new project
```

The guest path stays `/workspace`, so tooling inside the guest doesn't need to
know which project it's looking at. Mounts are **sticky**: a bare `agentctl
start work` reuses whatever was applied last. Because of that, `start` and
`status` always print the directories currently exposed — a sticky mount you've
forgotten about is exactly the failure worth making visible.

### Constraining what may be mounted (for admins)

`mountPolicy` bounds which host directories anyone may name. No CLI flag can
widen it, and layering profiles can only ever tighten it:

```yaml
spec:
  mountPolicy:
    allowedRoots: ["~/src", "~/agentctl/workspaces"]
    denyPaths: ["~/src/production-secrets"]
    createMissing: true
```

Credentials directories (`~/.ssh`, `~/.aws`, `~/.kube`, …) and the backends'
own state directories are refused regardless of configuration — see the
[Profile Schema](../reference/profile-schema.md#mount-policy) for the full
resolution order.

## Default non-root user

`agentctl create` provisions a non-root user inside every new instance and
makes it the default for `shell`/`exec` — not root. This is defense-in-depth:
the VM boundary is still the primary protection against host escape, but if
the agent *process itself* is compromised (a malicious dependency, a
prompt-injected shell command), it shouldn't automatically get root inside
the guest.

- The user has passwordless `sudo`, so anything that genuinely needs root
  (`apt install`, service management, ...) still works — this is hygiene and
  an audit signal (a `sudo` invocation is a distinct, loggable event), **not**
  a hard security boundary against a truly malicious agent.
- The username is `agent` by default, or matches the agent name if you used
  `--agent=<name>` (see [Agent Provisioning](agent-provisioning.md)) — so
  `--agent=claude` provisions and runs as a `claude` user.
- Root is always an explicit, visible opt-out, never removed:
  `agentctl shell <name> --root` / `agentctl exec --root <name> -- <cmd>`.
- This only affects `shell`/`exec` (the agent-mediated command channel).
  `agentctl view`'s console access is unaffected — it doesn't go through a
  guest user context at all.

## Tools in the guest: `packages:` and the `terminal`/`gui` tiers

Base images are deliberately minimal — `images:ubuntu/24.04` has no `git`,
no compiler, no editor. A profile's `packages:` list (and the matching
repeatable `--package` flag) installs tools into the guest during `create`,
before any `--agent` install runs:

```console
$ agentctl create demo --image=images:fedora/44 --package=git --package=ripgrep
```

Names are **neutral**, not distro package names: agentctl asks the guest
which package manager it has and maps each one onto that distribution's
real package, so the same profile works on Ubuntu, Fedora and Alpine.
`agentctl profile packages` prints the table.

Two built-in profiles bundle sensible sets:

| Profile | What it installs |
|---|---|
| `terminal` | `git`, `curl`, `ca-certificates`, `openssh-client`, `ripgrep`, `jq`, `unzip`, `tar`, `less`, `vim`, `procps`, `make`, `gcc`, `python3`, `shellcheck` |
| `gui` | everything in `terminal`, plus `xorg`, `xinit`, `openbox`, `xterm` |

```console
$ agentctl create demo --image=images:fedora/44 --profile=terminal --agent=claude
```

Both carry exactly the same network, resource and mount policy as
`default` — they differ only in what they install, and neither relaxes
`denyLAN`. Neither inherits from the other: each is a complete policy you
can read top to bottom.

Language-specific linters are deliberately absent from `terminal`.
`shellcheck` is there because shell is the one language every guest already
runs; `eslint`, `ruff`, `golangci-lint` and friends belong to their own
ecosystem's package manager, and pinning distro versions of them would be
worse than letting a project install what it needs.

!!! note "`packages:` is not a security control"
    It is the one field in a profile that *adds* capability rather than
    restricting it. A short list doesn't stop a sandbox installing
    software — the network policy does. See the
    [schema reference](../reference/profile-schema.md#packages) for how
    requesting packages changes when the network ACL is attached.

### What `gui` gives you

A display that can be driven, not a desktop that comes up on its own.
There is no display manager and no autologin, so nothing starts X for you:

```console
$ agentctl view demo      # opens the console
# ...log in, then:
$ startx
```

A display manager is a much larger surface to add to a sandbox, and adding
one implicitly wasn't worth it. If you're pointing this at a GUI coding
agent, check first that the agent actually ships a Linux build — several
of the desktop agents are macOS/Windows only, with Linux packaging done by
third parties.

## Combining a profile with ad-hoc flags

Flags on `create` always win over a profile's settings, and list-valued
fields (`allow`, `ports`, `mounts`) accumulate rather than replace:

```console
$ agentctl create demo --profile=default --allow=github.com:443 --port=9000:9000
```

This applies the `default` profile's full allowlist plus `github.com:443`,
and publishes port 9000 in addition to anything the profile already defined.

## Where profiles live

`agentctl profile list`/`show`/`set` search, in order:

1. `./.agentctl/profiles/<name>.yaml` (project-local)
2. `~/.config/agentctl/profiles/<name>.yaml` (personal)
3. the configured `profileDir` (org-distributed — see
   [Distributing Profiles Org-Wide](../admin/distributing-profiles.md))
4. embedded built-ins (`default`, `strict`, `terminal`, `gui`)

## Strict decoding

Profile YAML is parsed with unknown fields rejected as an error — a typo like
`denyLan` instead of `denyLAN` fails to load rather than silently being
ignored. This is a security policy file; failing loudly on a typo is the
correct default.
