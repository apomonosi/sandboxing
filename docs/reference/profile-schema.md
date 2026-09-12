# Profile Schema Reference

Source of truth: `internal/profile/schema.go`. This page mirrors it field by
field; if they drift, the Go source wins.

```yaml
apiVersion: agentctl.dev/v1   # required, must be exactly this value
kind: Profile                 # required, must be exactly this value
metadata:
  name: string                # required
  description: string         # optional
spec:
  network:
    denyLAN: bool              # default true in the embedded built-ins; blocks
                                # RFC1918 (10/8, 172.16/12, 192.168/16) and
                                # link-local (169.254/16) egress
    allow:                      # egress allowlist; everything unmatched is denied
      - domain: string          # hostname, or "*.example.com" wildcard (the
                                 # apex domain is what actually gets resolved
                                 # and matched — see the security model doc)
        ports: [int]            # optional; TCP ports allowed for this domain
    allowFile: string            # optional path to an external allow-rule file,
                                  # merged in at load time (mirrors --allow-file)
    ports:                       # host:guest port publishes, Docker-style
      - host: int
        guest: int
        protocol: string        # "tcp" (default) or "udp"
    dns:                         # name-resolution egress; see "DNS" below
      servers: [string]         # empty (default) = allow DNS to any destination
      disabled: bool            # default false; true = no DNS egress at all
  resources:
    cpuCores: int               # must be >= 0
    memory: string              # size string, e.g. "4GiB", "512MiB", "20GB"
    diskSize: string            # size string, same format as memory
  mounts:
    - hostPath: string           # supports "~" expansion and a "{{.Name}}"
                                  # template substituted with the instance name
      guestPath: string          # absolute; defaults to hostPath if omitted
      writable: bool             # default false (read-only), matching Lima
  mountPolicy:                    # constrains which host directories may be
                                   # mounted at all; see "Mount policy" below
    allowedRoots: [string]        # every mount must resolve under one of these
    denyPaths: [string]           # never mountable, even under an allowed root
    allowHome: bool               # default false; permits mounting $HOME itself
    createMissing: bool           # default false; mkdir 0700 a missing hostPath
  console:
    viewer: string               # advisory: "spice" | "vnc" | "native" — the
                                  # provider decides the actual mechanism it's
                                  # capable of; see view-and-console.md
  packages: [string]              # tools installed into the guest during
                                   # create; see "Packages" below
```

## Packages

`packages:` is the one field here that *adds* something to a sandbox rather
than restricting what it may do. It is not a security control and can't be
used as one — a profile author who wants to stop a sandbox installing
software has to do that through the network policy, not by leaving this
list short.

Entries are **neutral tool names**, not distro package names. agentctl asks
the guest which package manager it has and maps each name onto that
distribution's real package (`internal/packages`), so one profile installs
correctly on Ubuntu, Fedora and Alpine alike:

```yaml
spec:
  packages:
    - git
    - openssh-client   # openssh-clients on Fedora, openssh-client elsewhere
    - shellcheck       # ShellCheck on Fedora
```

Run `agentctl profile packages` for the full table of names and what each
one installs per distribution. A name that isn't in the table is passed to
the guest's package manager verbatim, which works but ties the profile to
one distribution.

Supported package managers are **apt, dnf and apk** — the families the
images used throughout these docs belong to. A guest with anything else
fails the create with a message naming the three, rather than guessing at
package names that were never verified.

### Ordering, and what it means for the network policy

Packages are installed during `create`, in the window where the instance is
already briefly running to provision the non-root user, and **before** any
`--agent` install.

When a create requests packages, the network ACL is attached *after* that
install rather than before it. This is deliberate. Distribution mirrors are
CDN-backed and rotate their addresses; agentctl's allow rules are resolved
host-side at create time and pin the addresses they saw, so a mirror that
hands the guest a different edge address than the host got is simply
unreachable. Rather than ask every profile to allowlist an unpinnable
moving target, provisioning runs against the backend's default
connectivity and the policy is attached as soon as it finishes.

The window is real and worth understanding: during it the guest has
whatever egress the host's bridge allows. Nothing runs in it except the
distribution's own package manager and agentctl's two embedded scripts; no
agent code and no user code has run yet, and the instance is not handed
over until the policy is on. `agentctl create` says so on stdout rather
than reordering silently, and `--preview` shows the reordered sequence.

A create with no packages keeps exactly the previous order — ACL first,
then the first boot.

## Size string format

Parsed by `internal/profile.ParseSize`. Accepts binary units (`KiB`, `MiB`,
`GiB`, `TiB`), decimal units (`KB`, `MB`, `GB`, `TB`), or a plain integer
byte count. Must be positive; zero and negative sizes are rejected.

## Validation

`internal/profile.Validate` aggregates every problem found in one pass
(rather than stopping at the first) and checks:

- `apiVersion`/`kind` match the constants above
- `metadata.name` is non-empty
- Every `allow[].domain` is non-empty and whitespace-free; every
  `allow[].ports` entry is in `1..65535`
- Every `ports[].host`/`ports[].guest` is in `1..65535`; `protocol` is `tcp`
  or `udp`; no two `ports` entries publish the same host port + protocol
- `resources.cpuCores >= 0`; `resources.memory`/`diskSize`, if set, parse via
  `ParseSize`
- Every `mounts[].hostPath` is non-empty; every `mounts[].guestPath`, when set,
  is absolute

Everything else about a mount is checked at create/start time rather than at
load time, because it depends on the instance name and the real filesystem —
see below.

## DNS

`network.dns` controls name-resolution egress, which is infrastructure rather
than part of the allowlist: a guest has to resolve a domain *before* it can
reach the address `allow` permits, so without DNS the allowlist cannot work at
all.

The default — an empty `servers` list — allows DNS to **any** destination on
port 53, over both UDP and TCP. That is deliberately permissive, and the cost
is real: DNS is a well-known exfiltration channel, since data can be encoded
into subdomain labels of an attacker-controlled zone. The alternative, allowing
only the bridge's own resolver, requires discovering that address per network
and per address family — and getting it wrong leaves the sandbox with no
working DNS at all, which is the failure this default exists to avoid.

Narrow it once you know your resolver addresses:

```yaml
spec:
  network:
    dns:
      servers: ["10.115.230.1"]   # only this resolver
```

Or remove DNS egress entirely, which only makes sense when a proxy resolves on
the sandbox's behalf:

```yaml
spec:
  network:
    dns:
      disabled: true
```

Merging is narrow-only, like `mountPolicy`: an override that names servers
replaces a broader base, and `disabled` is OR'd so the most restrictive layer
in the chain wins.

There is deliberately **no profile field for disabling network policy
wholesale**. The `--no-network-policy` escape hatch is a per-invocation CLI
flag only, so a distributed profile can't quietly turn enforcement off for
everyone who applies it.

## Mount policy

`mountPolicy` is the admin-facing half of mount policy. A sandbox sees nothing
of the host filesystem except the directories named in `mounts` or via
`--mount`, and `mountPolicy` bounds what may be named at all. There is
deliberately **no CLI flag that widens it**: `--mount` only ever produces
`mounts` entries, which are then checked against the policy.

`internal/profile.ResolveMounts` is the single place a host path is
interpreted. For each mount, in order:

1. `~` and `{{.Name}}` are expanded (an unknown template field is an error,
   not an empty string).
2. The path is made absolute and cleaned.
3. A missing directory is created with mode `0700` if `createMissing` is set,
   otherwise it is an error.
4. The path is canonicalized with `EvalSymlinks`, and **every check below runs
   against the canonical path** — otherwise a symlink inside an allowed root
   could point anywhere.
5. `/` and `$HOME` itself are refused unless `allowHome` is set. Directories
   *inside* `$HOME` are unaffected — that is where the default workspace lives.
6. A built-in deny list is applied that profile authors cannot forget:
   `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.config/gcloud`, `~/.kube`, `~/.docker`,
   plus the backends' own state directories (`~/.lima`, `~/.local/share/incus`,
   `~/.config/incus`) and `~/.config/agentctl`. Write access to a backend's
   state would let an agent rewrite the *next* sandbox's configuration, which
   escapes the sandbox without ever attacking the VM boundary.
7. `denyPaths` is applied on top.
8. If `allowedRoots` is non-empty, the path must live under one of them.
   Containment compares path segments, so `/home/u/srcevil` is not inside
   `/home/u/src`.
9. `guestPath` defaults to the host path (Lima's own behavior), must be
   absolute, and may not shadow `/`, `/etc`, `/usr`, `/bin`, `/sbin`, `/lib`,
   `/boot`, `/dev`, `/proc`, `/sys`, `/var` or `/root`.
10. Mounts that overlap — same guest path, nested guest paths, or nested host
    paths — are rejected.

Errors aggregate, so one run reports every bad mount rather than one per fix.

## Strict decoding

Profiles are decoded with unknown fields rejected — see
[Profiles & Policies](../user/profiles-and-policies.md#strict-decoding) for
why.

## Merging

`internal/profile.Merge(base, override Policy) Policy` combines a loaded
profile with ad-hoc `create` flags:

- `network.allow`, `network.ports`, `mounts`: **additive union** — override
  entries are appended after base's
- `packages`: **additive union, deduped** — no layer can withdraw a tool an
  earlier one asked for, and two layers both asking for `git` install it
  once. Plain accumulation rather than `mountPolicy`'s narrow-only
  asymmetry, because nothing here is load-bearing for security: packages add
  software, they don't relax a restriction
- `network.denyLAN`: override's value is taken as-is (the CLI resolves
  `--deny-lan`/`--allow-lan` against the profile's value before calling Merge)
- `resources.*`, `console.viewer`: override's value wins whenever it's
  non-zero/non-empty, otherwise base's value survives
- `mountPolicy`: **narrow-only** — layering can tighten what is mountable but
  never loosen it. `allowedRoots` intersect (an empty override leaves the
  base's roots intact rather than clearing them), `denyPaths` union, and for
  `allowHome` an explicit `false` anywhere in the chain wins over a later
  `true`. This asymmetry is the point: a personal profile must not be able to
  unlock a directory an org profile ruled out.

## The Spec document (`create --spec=<file>`)

A separate, smaller document (`internal/spec.Spec`) describes one specific
`create` invocation rather than a reusable profile:

```yaml
apiVersion: agentctl.dev/v1
kind: Spec
metadata:
  name: string          # informational; the CLI's <name> argument always wins
spec:
  image: string          # required
  profiles: [string]     # named profiles to apply, in order
  overrides: Policy       # same Policy shape as a profile's spec, layered on
                          # top of the named profiles via Merge
```
