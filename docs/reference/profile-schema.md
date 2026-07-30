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
  resources:
    cpuCores: int               # must be >= 0
    memory: string              # size string, e.g. "4GiB", "512MiB", "20GB"
    diskSize: string            # size string, same format as memory
  mounts:
    - hostPath: string           # supports "~" expansion and a "{{.Name}}"
                                  # template substituted with the instance name
      guestPath: string          # must be an absolute path
      readOnly: bool
  console:
    viewer: string               # advisory: "spice" | "vnc" | "native" — the
                                  # provider decides the actual mechanism it's
                                  # capable of; see view-and-console.md
```

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
- Every `mounts[].hostPath` is non-empty; every `mounts[].guestPath` is
  absolute

## Strict decoding

Profiles are decoded with unknown fields rejected — see
[Profiles & Policies](../user/profiles-and-policies.md#strict-decoding) for
why.

## Merging

`internal/profile.Merge(base, override Policy) Policy` combines a loaded
profile with ad-hoc `create` flags:

- `network.allow`, `network.ports`, `mounts`: **additive union** — override
  entries are appended after base's
- `network.denyLAN`: override's value is taken as-is (the CLI resolves
  `--deny-lan`/`--allow-lan` against the profile's value before calling Merge)
- `resources.*`, `console.viewer`: override's value wins whenever it's
  non-zero/non-empty, otherwise base's value survives

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
