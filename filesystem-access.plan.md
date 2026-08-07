# Host Filesystem Access — Named Directories Only (Plan, Not Yet Implemented)

**Status:** Design plan only. No code has been written for any of this. The
goal: a sandbox sees **nothing** of the host filesystem except directories
explicitly named when the VM is created **or started**, with an
admin-controllable allowlist of which host directories may be named at all. The
CLI/YAML surface should read like Lima's and Incus's own, not like a third
dialect.

Mounts are a **per-boot** property, not a per-instance-lifetime one. That is
what Lima itself does (`limactl start <existing-instance> --mount …` applies to
an instance created earlier), and it is what makes one long-lived sandbox
reusable across projects instead of forcing a VM per project.

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

### The same flags on `start`

`agentctl start` takes the identical flag set, and this is the primary
workflow for a sandbox reused across projects:

```console
$ agentctl create work --image=…            # created once, no project bound to it
$ agentctl start work --mount ~/src/alpha:/workspace:w
$ agentctl stop work
$ agentctl start work --mount ~/src/beta:/workspace:w    # same VM, different project
$ agentctl start work --mount-none                        # boot with no host access
```

The guest path stays `/workspace` across projects, so tooling inside the guest
does not have to know which project it is looking at.

This is not agentctl inventing a verb: `limactl start <name> --mount …` on an
already-created instance is exactly Lima's own behavior — `start.go`'s
`loadOrCreateInstance` detects the existing instance, logs `Using the existing
instance …`, and applies the edit flags' yq expressions to it via
`applyYQExpressionToExistingInstance`. Only `limactl edit` refuses on a live
instance (`cannot edit a running instance`), and `agentctl start` operates on a
stopped instance by definition, so that restriction never binds.

Mount policy is re-resolved and re-validated on every start — `allowedRoots`,
`denyPaths` and the symlink canonicalization all run again, so a profile
tightened after the instance was created takes effect at the next boot rather
than being frozen at create time.

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

**Gate in `internal/cli/create.go` and `internal/cli/start.go`**, alongside the
existing DenyLAN/ACL/Port gates:

```go
if len(policy.Mounts) > 0            { gateOrBlock(…, FeatureMount, fp) }
if anyReadOnly(policy.Mounts)        { gateOrBlock(…, FeatureMountReadOnly, fp) }
```

`start.go` today is a thin `p.Start(ctx, name)` wrapper with no flags of its
own, so it also needs the profile-resolution plumbing `create` already has
(`--profile`, `--profile-dir`, `spec.ResolvePolicy`) in order to re-apply
`mountPolicy` on each boot.

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

### Reconfiguring mounts at start

A new `Provider` method, capability-gated like `ApplyNetworkPolicy` and for the
same reason (it is the operation whose mechanics diverge most between
backends):

```go
// ApplyMountPolicy replaces the instance's agentctl-managed mount set.
// Called by `start` when any --mount flag is present; the instance is
// stopped at that point.
ApplyMountPolicy(ctx context.Context, name string, mounts []Mount) error
```

- **Lima**: fold the mount expressions into the `start` invocation itself —
  `limactl start --set '.mounts = []' --set '.mounts += [...]' <name>`, or the
  editflags equivalent (`--mount-only`/`--mount-none`). One command, no separate
  reconfigure step, and it is the same `buildSetExpressions` builder `create`
  already uses. `buildStartArgs(name)` grows a mounts parameter.
- **Incus**: `incus config device remove <name> <dev>` for each
  agentctl-managed mount device, then `config device add` for the new set, then
  `incus start`. This requires **deterministic, agentctl-owned device names** —
  the current index-derived `mount0`/`mount1` scheme cannot distinguish "a
  device agentctl added last boot" from one the user added by hand. Name them
  from a fixed prefix plus a hash of the guest path (e.g.
  `agentctl-mount-<8-hex>`), and only ever remove devices matching that prefix.
  This subsumes the stable-naming point in the resolution pipeline above.
- **Hyper-V**: inherits whatever `fs.mount` says — `NotAvailable` today.

Hot-attaching to an *already running* instance is a separate, later feature
(`agentctl mount add|list|remove`, mirroring `incus config device
add|list|remove`). Incus can plausibly do it — the incus-agent mounts a
hot-plugged share — but it is unverified against a live daemon, and Lima cannot
do it at all without a restart, so it should not gate the start-time design.

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
section: nothing is exposed but what you name, plus a worked
one-sandbox-many-projects example), `docs/user/cli-reference.md` (the four new
flags, on both `create` and `start` — `start`'s entry currently lists no flags
at all), `docs/admin/capability-matrix.md` (two new rows),
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
- Lima translate test: `.mounts = []` always emitted, and emitted first; the
  same expressions appear on the `start` invocation for an existing instance.
- Incus translate test: `readonly=true` present/absent per `ReadOnly`; the
  remove-then-add reconfigure sequence only touches `agentctl-mount-*` devices
  and leaves a hand-added device alone.
- `capability_completeness_test.go` covers the new features for free.
- Integration tests (`//go:build integration`): on Incus, create with one
  read-only and one writable mount, assert the file is visible in the guest and
  that a write to the read-only share fails — this is what settles the
  `fs.mount.readonly` capability entry. On Lima, assert `~` is **not** visible
  in the guest after create. On both, the reuse cycle: start with directory A,
  stop, start with directory B, and assert A is gone from the guest — a mount
  that survives a reconfigure is the failure mode that would quietly break the
  whole "one reusable sandbox" model.

## Suggested phasing

**Phase 0 — close the documented-vs-actual gap (independently shippable).**
Implement `~`/`{{.Name}}` expansion; emit Lima's `.mounts = []` reset. These two
alone stop the built-in profiles from passing literal template strings to Incus
and stop Lima sandboxes from inheriting the host home directory. No new API.

**Phase 1 — the flags, on both `create` and `start`.** `--mount`,
`--mount-none`, `--mount-only`, `--mount-writable`, plus `ResolveMounts`
containment checks, `ApplyMountPolicy`, and agentctl-owned deterministic device
names on Incus. `start` is in this phase, not deferred: without it the tool
forces one VM per project, which is the workflow question this design exists to
answer.

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
3. **Do start-time mounts stick, or last one boot?** Both backends store the
   mount set natively (Lima's instance YAML, Incus's device list), so the
   zero-extra-state option is **sticky**: `start --mount ~/src/alpha` rewrites
   the stored set, and a later bare `agentctl start work` reuses it. That
   matches `limactl start --mount` exactly. The alternative is **per-boot**:
   clear the mount set on `stop`, so a bare `start` exposes nothing and every
   boot must name its directories. Sticky is the better API fit; per-boot is
   the safer default, because a sticky mount means a bare `start` months later
   silently re-exposes a directory the user has forgotten about. A middle
   option: sticky storage, but `status` and `start`'s own output always print
   the mount set, so it is never invisible. Needs a call — this is the one
   decision that changes what a bare `agentctl start` grants.
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
