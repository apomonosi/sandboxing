# Host Filesystem Access — Named Directories Only (Plan, Not Yet Implemented)

**Status:** Design plan only. No code has been written for any of this. The
goal: a sandbox sees **nothing** of the host filesystem except directories
explicitly named at create time, with an admin-controllable allowlist of which
host directories may be named at all. The CLI/YAML surface should read like
Lima's and Incus's own, not like a third dialect.

## What exists today

Mount policy already flows end to end, so this is an extension of an existing
path, not a new subsystem:

| Layer | File | What it does today |
|---|---|---|
| Profile YAML | `internal/profile/schema.go` | `Mount{HostPath, GuestPath, ReadOnly}`, list field `spec.mounts` |
| Merge | `internal/profile/merge.go` | `Mounts` is an **additive union** (base then override) |
| Validation | `internal/profile/validate.go` | `hostPath` non-empty, `guestPath` absolute — that is all |
| Spec → provider | `internal/spec/spec.go` | `profile.ToProviderMounts` → `provider.InstanceSpec.Mounts` |
| Incus | `internal/provider/incus/translate.go` | `incus config device add <inst> mount<N> disk source=… path=… [readonly=true]` |
| Lima | `internal/provider/lima/translate.go` | `--set '.mounts += [{"location":…,"mountPoint":…,"writable":…}]'` |

Five concrete gaps, in rough order of severity:

1. **Lima inherits its template's mounts.** `buildSetExpressions` only ever
   *appends* (`.mounts += [...]`). `spec.Image` is a Lima template name, and
   Lima templates commonly declare their own mounts (historically `~`
   read-only and `/tmp/lima` writable). agentctl never resets them, so on
   Lima a sandbox can plausibly see the whole host home directory today —
   directly contrary to what `docs/admin/security-model.md` claims the tool is
   for. Incus has no equivalent problem: a VM gets no host share unless a disk
   device is added.
2. **`~` expansion and `{{.Name}}` templating are documented but not
   implemented.** `schema.go`'s comment, `docs/reference/profile-schema.md`,
   and `docs/user/profiles-and-policies.md` all promise both. A repo-wide grep
   finds no expansion code anywhere. Both built-in profiles ship
   `hostPath: "~/agentctl/workspaces/{{.Name}}"`, so Incus is currently handed
   `source=~/agentctl/workspaces/{{.Name}}` **as a literal string** — visible
   in `docs/user/preview-mode.md`'s own sample output. The mechanism meant to
   keep each instance confined to its own directory does not run.
3. **No way to name a directory at create time.** There is no `--mount` flag.
   Mounts can only come from a profile or a `--spec` file, and because Merge is
   additive, a user can only ever *widen* what a profile grants, never narrow
   it.
4. **No containment checks.** Nothing verifies the host path is absolute,
   exists, is a directory, is not a symlink pointing somewhere else, is not
   `/` or `$HOME` or `~/.ssh`, and nothing detects two mounts overlapping.
5. **No capability entry.** `provider.AllFeatures` has no mount feature, so
   there is no gate, no capability-matrix row, and no way for a backend to say
   "I cannot enforce read-only" — Hyper-V would silently accept mount policy it
   cannot implement at all.

## Upstream API surface being matched

Lima (`cmd/limactl/editflags`, verbatim usage strings):

```
--mount <dir>[:w]      "Directories to mount, suffix ':w' for writable
                        (Do not specify directories that overlap with the existing mounts)"
--mount-only <dir>[:w] "Similar to --mount, but overrides the existing mounts"
--mount-none           "Remove all mounts"                  → .mounts = null
--mount-writable       "Make all mounts writable"           → .mounts[].writable = true
--mount-type <t>       "Mount type (reverse-sshfs, 9p, virtiofs)"
--mount-inotify        "Enable inotify for mounts"          (experimental upstream)
```

```yaml
mounts:
  - location: "~"          # host path
    mountPoint: null       # builtin default: value of location
    writable: null         # builtin default: false
```

Incus:

```
incus config device add <instance> <device> disk source=<host-path> path=<guest-path> \
    [readonly=true] [required=true] [recursive=…] [propagation=…] [raw.mount.options=…]
incus config device list|show|remove <instance> …
incus init/launch … -d|--device <device>,<key>=<value>
```

Two things worth stealing from each: Lima's compact `<dir>[:w]` flag form and
its explicit `--mount-none` / `--mount-only` narrowing verbs; Incus's
`source=`/`path=`/`readonly=` triple, which agentctl's `Mount` struct already
mirrors field for field.

## Proposed CLI surface

```console
$ agentctl create demo --image=… \
    --mount ~/src/project \                 # read-only, guest path = host path
    --mount ~/src/project:w \               # writable (Lima's ':w')
    --mount ~/src/project:/workspace \      # explicit guest path (Incus's path=)
    --mount ~/src/project:/workspace:w      # both
$ agentctl create demo --image=… --mount-none          # zero host filesystem access
$ agentctl create demo --image=… --mount-only ~/scratch:w   # ignore profile mounts
$ agentctl create demo --image=… --mount ~/src --mount-writable
```

Parsing rules for `--mount` (new `parseMountFlag` in `internal/cli/flags.go`,
same shape as the existing `parsePortFlag`/`parseAllowFlag`):

- Split on `:`. A trailing `w` or `ro` token sets writability; everything
  before is `hostPath[:guestPath]`.
- One path only → `guestPath` defaults to `hostPath` **after expansion**,
  matching Lima's documented "mountPoint builtin default: value of location".
- No suffix → **read-only**, matching Lima's `writable` builtin default of
  `false` and the safer default for a sandbox. `:w` is always explicit.
- Repeatable; accumulates on top of profile mounts, exactly like `--allow` and
  `--port` do today.

Flag semantics map onto the existing merge model:

| Flag | Effect |
|---|---|
| `--mount` | appended to the profile's `mounts` (Merge's additive rule, unchanged) |
| `--mount-only` | replaces the profile's `mounts` entirely (Lima's own verb for this) |
| `--mount-none` | resolved mount list is empty; no host path is exposed at all |
| `--mount-writable` | sets `readOnly: false` on every resolved mount; prints a warning line |

`--mount-type` and `--mount-inotify` are deliberately **out of scope**: they are
Lima-only performance/tuning knobs with no Incus analogue, and neither changes
what the guest can reach. If they are wanted later, the natural shape is a
per-mount `options` passthrough map rather than top-level flags.

## Proposed profile schema

`spec.mounts` keeps its current shape (it already matches Incus's triple and is
one rename away from Lima's). The new part is an admin-authored constraint
block — this is the piece that actually delivers "restrict host file access to
named directories," because profiles are the org-distributed artifact
(`docs/admin/distributing-profiles.md`), and there is deliberately **no CLI flag
that widens it**:

```yaml
spec:
  mounts:
    - hostPath: "~/agentctl/workspaces/{{.Name}}"
      guestPath: /workspace
      readOnly: false
  mountPolicy:
    allowedRoots:                  # a mount must resolve under one of these
      - "~/src"
      - "~/agentctl/workspaces"
    denyPaths:                     # never mountable, even under an allowed root
      - "~/.ssh"
      - "~/.aws"
      - "~/.gnupg"
      - "~/.kube"
      - "~/.config/agentctl"
    allowHome: false               # refuse to mount $HOME, /, or system dirs
    defaultReadOnly: true
    createMissing: true            # mkdir 0700 a missing hostPath
```

Merge rule for `mountPolicy`, to be stated explicitly in
`docs/reference/profile-schema.md`: **overrides may narrow, never widen.**
`denyPaths` is an additive union; `allowedRoots` takes the later profile's value
only if it is a subset of the earlier one, otherwise the load fails; the booleans
follow the existing "override wins when non-zero" convention. Since `--mount`
produces only `mounts` entries and never a `mountPolicy`, an ad-hoc `create`
cannot escape an org profile's roots.

A `denyPaths` default worth hard-coding in the resolver rather than leaving to
profile authors: the provider's own state directories (`~/.lima`,
`~/.local/share/incus`, `~/.config/incus`) and agentctl's config dir. A sandbox
with write access to those can rewrite the *next* VM's configuration — a
sandbox escape that routes around the VM boundary entirely instead of
attacking it.

## Resolution pipeline

New file `internal/profile/mount.go` (or a small `internal/mount` package if it
grows), with one entry point called from `spec.ToInstanceSpec`:

```go
func ResolveMounts(mounts []Mount, instanceName string, policy MountPolicy) ([]provider.Mount, error)
```

Steps, aggregating every failure via `errors.Join` the way `Validate` already
does so an author sees the full list in one pass:

1. **Expand** `~` (via `os.UserHomeDir`) and `{{.Name}}` (via `text/template`
   with the instance name) — finally implementing what `schema.go` and the docs
   already promise. This is gap #2 and is independently worth landing.
2. **Normalize**: `filepath.Abs` + `filepath.Clean`; reject a `hostPath` still
   relative after expansion (today's validation does not even require absolute).
3. **Canonicalize**: `filepath.EvalSymlinks`, and run every subsequent check
   against the canonical path — otherwise `~/src/link -> /` walks straight past
   `allowedRoots`. Fail closed if the path cannot be resolved.
4. **Existence**: if missing and `createMissing`, `os.MkdirAll(path, 0o700)`;
   otherwise a clear agentctl error rather than the backend's (Incus's disk
   device defaults to `required=true`, so it would fail at device-add anyway,
   with a worse message).
5. **Deny checks**: `/`, `$HOME` itself unless `allowHome`, anything under
   `denyPaths`, plus the hard-coded provider-state paths above.
6. **Containment**: `filepath.Rel(root, path)` must not start with `..`, with an
   explicit separator-boundary check so `/home/u/srcevil` does not match root
   `/home/u/src`. Empty `allowedRoots` means unconstrained — preserving today's
   behavior for anyone not using the new block.
7. **Guest path**: default to the host path (Lima parity); must be absolute;
   reject `/` and refuse to shadow `/etc`, `/usr`, `/bin`, `/sbin`, `/boot`.
8. **Overlap detection**: duplicate or nested `guestPath`s, and nested
   `hostPath`s — Lima's own `--mount` usage string warns against overlapping
   mounts, so catching it in agentctl is strictly better than passing it down.
9. **Stable device naming**: Incus device names are index-derived (`mount0`,
   `mount1`), so reordering mounts renames devices on re-apply. Sort resolved
   mounts by guest path before assigning names, or derive the name from a
   sanitized/hashed guest path.

## Provider layer

**Two new capability features** in `internal/provider/capability.go`, added to
`AllFeatures` (the existing `capability_completeness_test.go` then forces every
provider table — including the Hyper-V stub — to be updated, which is exactly
the guard rail wanted here):

```go
FeatureMount         Feature = "fs.mount"           // host directory shares at all
FeatureMountReadOnly Feature = "fs.mount.readonly"  // read-only enforcement specifically
```

Splitting read-only out is the same reasoning that made `network.deny-lan` its
own feature: a backend that mounts but silently ignores `readonly` gives the
sandbox write access to host files while appearing to comply, and the user
should hit the existing `--force-partial` gate rather than find out later.

Per-provider entries:

- **Incus** — `fs.mount`: Supported (`config device add … disk`), with a
  capability `Message` noting the VM guest needs `incus-agent` running to
  actually mount the share (VMs get directories via 9p/virtiofs, mounted
  guest-side by the agent). `fs.mount.readonly`: the disk device's `readonly`
  option is documented without a container-only condition, but whether it is
  enforced on a **VM** virtiofs/9p share has not been verified against a live
  daemon — same caveat `translate.go`'s package NOTE already carries for every
  other `incus` flag in this repo. Verify in the gated integration test before
  claiming `Supported`; if it is not enforced, `ManualWorkaround` with a
  "mount the source read-only on the host first" plan is the honest entry.
- **Lima** — both Supported (`writable: false` is Lima's own default and is
  enforced by the mount backend). Worth recording that upstream discourages
  `writable: true` with `reverse-sshfs`; Lima ≥1.0 resolves the default mount
  type to 9p (QEMU) or virtiofs (vz), so this is mostly a non-issue.
- **Hyper-V** — `NotAvailable`/`ManualWorkaround` with a `Plan` string (host SMB
  share + guest credential mapping); there is no virtiofs-equivalent primitive
  to wire up, so this is a genuine platform gap, not unfinished wiring.

**Gate in `internal/cli/create.go`**, alongside the existing DenyLAN/ACL/Port
gates:

```go
if len(policy.Mounts) > 0            { gateOrBlock(…, FeatureMount, fp) }
if anyReadOnly(policy.Mounts)        { gateOrBlock(…, FeatureMountReadOnly, fp) }
```

**Lima translate change — the actual isolation fix (gap #1).**
`buildSetExpressions` must emit a reset expression **unconditionally**, before
any `+=`, so the base template's own mounts never survive:

```go
exprs = append(exprs, ".mounts = []")            // Lima's own --mount-none is `.mounts = null`
for _, m := range spec.Mounts { /* .mounts += [...] as today */ }
```

That makes agentctl's behavior identical to `limactl … --mount-only`, which is
the correct default for a sandbox: the mount list is agentctl's policy, not the
template author's. A translate test should assert the reset is emitted even when
`spec.Mounts` is empty — that is the `--mount-none` case and the one most worth
regression-proofing.

**Incus translate change:** none structurally — `buildMountDeviceArgs` already
emits `source=`/`path=`/`readonly=true`. Optionally pass `required=true`
explicitly for self-documenting preview output. Do not emit `shift=` (a no-op
for VMs) or `recursive=`.

## Introspection

- Add `Mounts []Mount` to `provider.Instance` and populate it in `Status`:
  Incus via `incus config device show <name>` (or the existing `incus query`
  path), Lima by reading the instance's own `lima.yaml`. `agentctl status`
  then answers "what host directories can this sandbox see?" without the user
  reconstructing it from profiles plus flags.
- `--preview` needs no new code — it already renders the device-add / `--set`
  commands from the same builders — but its output changes once expansion works,
  so `docs/user/preview-mode.md`'s sample (which currently shows the literal
  `{{.Name}}`) must be updated in the same change.

## Docs to update

`docs/reference/profile-schema.md` (the `mountPolicy` block, resolution and
merge rules), `docs/user/profiles-and-policies.md` (a "Host filesystem access"
section: nothing is exposed but what you name), `docs/user/cli-reference.md`
(the four new flags), `docs/admin/capability-matrix.md` (two new rows),
`docs/admin/security-model.md` (a new section stating plainly that a writable
mount is a two-way channel out of the sandbox and that read-only enforcement is
per-provider), and `docs/user/preview-mode.md` (expanded paths).

## Testing

- Table-driven unit tests for `parseMountFlag`, mirroring the existing
  `parsePortFlag` tests: bare path, `:w`, `:ro`, explicit guest path, both,
  and the malformed cases.
- `ResolveMounts` tests against a `t.TempDir()`: template/`~` expansion, symlink
  escape, `..` traversal, allowed-root boundary (`/src` vs `/srcevil`), deny
  paths, `$HOME` and `/`, missing-directory creation with mode 0700, guest-path
  collision, host and guest overlap.
- Merge tests for `mountPolicy`'s narrow-only rule, including the failure case
  where a later profile tries to widen `allowedRoots`.
- Lima translate test: `.mounts = []` always emitted, and emitted first.
- Incus translate test: `readonly=true` present/absent per `ReadOnly`.
- `capability_completeness_test.go` covers the new features for free.
- Integration tests (`//go:build integration`): on Incus, create with one
  read-only and one writable mount, assert the file is visible in the guest and
  that a write to the read-only share fails — this is what settles the
  `fs.mount.readonly` capability entry. On Lima, assert `~` is **not** visible
  in the guest after create.

## Suggested phasing

**Phase 0 — close the documented-vs-actual gap (independently shippable).**
Implement `~`/`{{.Name}}` expansion; emit Lima's `.mounts = []` reset. These two
alone stop the built-in profiles from passing literal template strings to Incus
and stop Lima sandboxes from inheriting the host home directory. No new API.

**Phase 1 — the create-time flags.** `--mount`, `--mount-none`, `--mount-only`,
`--mount-writable`, plus `ResolveMounts` containment checks.

**Phase 2 — admin constraints.** `mountPolicy` in the profile schema, the two
capability features, and the `create` gate.

**Phase 3 — visibility.** `provider.Instance.Mounts`, `status` output, doc
updates, integration tests.

## Open questions to resolve before implementation

1. **Read-only default vs. the existing field.** Lima's default is
   `writable: false`; agentctl's `Mount.ReadOnly bool` zero value means
   *writable*, so a YAML author who omits the field gets the opposite of Lima's
   default. Either (a) keep `readOnly` and change only the CLI flag's default
   (non-breaking, but YAML and flag disagree), or (b) rename the YAML field to
   `writable` for exact Lima parity — a breaking schema change, and profiles are
   parsed with `KnownFields(true)`, so every existing profile fails to load
   loudly rather than silently. (a) is the safer default; (b) is the cleaner
   API. Needs a call.
2. **Windows path parsing.** `--mount C:\src:/workspace:w` collides with `:` as
   the delimiter. Options: parse from the right (like `parsePortFlag` handles
   `/proto`), require the profile YAML form on Windows, or accept a `=`
   separator variant. Hyper-V is a stub today, so this can be deferred — but the
   flag grammar should not have to change later.
3. **Should `--mount` be accepted on `start` too?** The request framed this as
   "defined when starting the vm." Incus can hot-attach a disk device to a
   running instance; Lima cannot (it needs stop → edit → start). Recommendation:
   mounts stay a create-time property for v1, with a later `agentctl mount
   add|list|remove` mirroring `incus config device add|list|remove` if live
   reconfiguration is genuinely wanted.
4. **Default when nothing is specified.** Keep the built-ins' per-instance
   `~/agentctl/workspaces/{{.Name}}` → `/workspace` mount (ergonomic, already
   instance-scoped), with `--mount-none` as the zero-access switch? Or default
   to no mounts at all and make the workspace opt-in? Recommendation: keep it,
   since it is per-instance and is what makes the sandbox useful.
5. **Guest-side ownership.** A virtiofs/9p share lands with some uid/gid; the
   provisioned non-root `agent` user must be able to read (and for writable
   mounts, write) it. Whether that needs a mount option, a `chown` in the
   bootstrap script, or is handled by the backend differs per provider and is
   unverified — settle it in the Phase 3 integration tests rather than guessing
   in the design.
6. **Does Incus enforce `readonly=true` on VM shares?** Blocks the
   `fs.mount.readonly` capability entry for Incus (see above). Verify against a
   live daemon.
