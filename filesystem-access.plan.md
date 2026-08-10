# Host Filesystem Access — Named Directories Only (Plan, Not Yet Implemented)

**Status: implemented for Incus and Lima.** Phases 0–3 below have landed:
`--mount`/`--mount-only`/`--mount-none`/`--mount-writable` on `create` and
`start`, `ResolveMounts`, `mountPolicy`, the `fs.mount`/`fs.mount.readonly`
capability features, and mount reporting in `status`. **Hyper-V remains
documented-only** — it ships the `ManualWorkaround` SMB procedure described
below, and the native reverse-SFTP design is deliberately not built, since the
Hyper-V backend's own lifecycle is still a stub. This file is kept as the
rationale record; the code is the source of truth for behavior.

The goal: a sandbox sees **nothing** of the host filesystem except directories
explicitly named when the VM is created **or started**, with an
admin-controllable allowlist of which host directories may be named at all. The
CLI/YAML surface should read like Lima's and Incus's own, not like a third
dialect.

Mounts are a **per-boot** property, not a per-instance-lifetime one. That is
what Lima itself does (`limactl start <existing-instance> --mount …` applies to
an instance created earlier), and it is what makes one long-lived sandbox
reusable across projects instead of forcing a VM per project.

See `image-workflow.plan.md` for the layer above this one: the durable,
reusable artifact is meant to be the **image** (toolchains, runtimes, the agent
CLI — built once, shared across projects), with instances disposable and the
mount naming the project directory for one boot. Mount policy is what makes a
disposable instance useful without also making it a hole in the host.

## Decisions taken

Settled; the rest of this document reflects them rather than presenting them as
options.

1. **`writable`, not `readOnly`.** The YAML field is renamed for exact Lima
   parity, and the Go field follows (`Mount.Writable`). Default `false` =
   read-only, matching Lima's builtin default. This is a breaking schema change
   and profiles are strict-decoded, so every existing profile fails to load
   loudly — acceptable in early development, and the loud failure is the point.
   The `hostPath`/`guestPath` names stay: Lima calls them `location`/
   `mountPoint`, Incus calls them `source`/`path`, so there is no single parity
   to match, and the existing names say which side is which.
2. **`--mount` parses from the right**, so Windows drive letters survive. See
   the grammar below.
3. **Start-time mounts are sticky**, following `limactl start --mount`. The
   stored set is rewritten and reused by a later bare `start`; `start` and
   `status` always print the active mount set so a stale mount is never
   invisible.
4. **The per-instance workspace stays the default; `$HOME` is never mounted.**
   `~/agentctl/workspaces/{{.Name}}` → `/workspace` remains in the built-in
   profiles, and `$HOME`, `/`, and system directories are refused by the
   resolver by default rather than only when a profile opts out. This is
   specifically what Lima does *not* do today (gap #1).
5. **Guest-side ownership** is an implementation detail to settle empirically in
   Phase 1's integration tests, not a design question.
6. **Incus `readonly=true` on VM shares** is likewise a test assertion, not a
   blocker: the integration test asserts a write to a read-only share fails, and
   the capability entry is written to match whatever it finds.

## What exists today

Mount policy already flows end to end, so this is an extension of an existing
path, not a new subsystem:

| Layer | File | What it does today |
|---|---|---|
| Profile YAML | `internal/profile/schema.go` | `Mount{HostPath, GuestPath, ReadOnly}`, list field `spec.mounts` — `ReadOnly` becomes `Writable` (decision 1) |
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

Grammar for `--mount` (new `parseMountFlag` in `internal/cli/flags.go`, same
shape as the existing `parsePortFlag`/`parseAllowFlag`), **parsed right to
left** so Windows drive letters survive (decision 2):

1. If the last `:`-delimited token is exactly `w` or `ro`, it sets writability
   and is stripped. Anything else is part of a path.
2. Of what remains, if there is a `:` whose right-hand side **starts with `/`**,
   that is the `guestPath` and everything to its left is the `hostPath`. Guest
   paths are always absolute Linux paths — agentctl guests are Linux on every
   backend — so a leading `/` is an unambiguous marker that a Windows host path
   can never produce.
3. Otherwise the whole remainder is the `hostPath`, and `guestPath` defaults to
   the host path **after expansion**, matching Lima's documented "mountPoint
   builtin default: value of location".

That makes all of these parse correctly:

```
~/src/project                      host=~/src/project      guest=(same)     ro
~/src/project:w                    host=~/src/project      guest=(same)     rw
~/src/project:/workspace           host=~/src/project      guest=/workspace ro
C:\Users\me\src:/workspace:w       host=C:\Users\me\src    guest=/workspace rw
C:\Users\me\src                    host=C:\Users\me\src    guest=(same)     ro
```

No suffix means **read-only**, matching Lima's `writable` builtin default of
`false` and the safer default for a sandbox; `:w` is always explicit.
Repeatable, and accumulates on top of profile mounts exactly like `--allow` and
`--port` do today.

Flag semantics map onto the existing merge model:

| Flag | Effect |
|---|---|
| `--mount` | appended to the profile's `mounts` (Merge's additive rule, unchanged) |
| `--mount-only` | replaces the profile's `mounts` entirely (Lima's own verb for this) |
| `--mount-none` | resolved mount list is empty; no host path is exposed at all |
| `--mount-writable` | sets `writable: true` on every resolved mount; prints a warning line |

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

Mounts are **sticky** (decision 3): each `start --mount …` rewrites the stored
set, and a later bare `agentctl start work` reuses whatever was last applied —
the same behavior `limactl start --mount` has, and it needs no agentctl-side
state because both backends already persist the mount set (Lima's instance
YAML, Incus's device list). The safety cost of stickiness is that a bare
`start` months later silently re-exposes a directory the user has forgotten
about, so it is paid down by making the set impossible to miss: `start` prints
the mount set it applied, and `status` always lists it.

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
      writable: true               # renamed from readOnly; default false
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
    allowHome: false               # default; true is the deliberate escape hatch
    createMissing: true            # mkdir 0700 a missing hostPath
```

Note `defaultReadOnly` is gone — with the polarity flip there is exactly one
default (`writable: false`) and it lives in the field's zero value, so a second
knob restating it would only create a way for the two to disagree.

`allowHome: false` is the **built-in default**, not merely the recommended
setting (decision 4): `$HOME`, `/`, and system directories are refused by the
resolver unless a profile explicitly sets `allowHome: true`. This is precisely
the behavior Lima does not have today, and gap #1 is the proof that leaving it
to configuration is not enough.

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
5. **Deny checks**: `/` and `$HOME` itself — refused by default, allowed only
   when a profile sets `allowHome: true` — plus anything under `denyPaths` and
   the hard-coded provider-state paths above. The per-instance workspace
   (`~/agentctl/workspaces/<name>`) is *under* `$HOME` and stays perfectly
   legal: the rule denies mounting the home directory itself, not everything
   inside it.
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
- **Hyper-V** — `ManualWorkaround` with an SMB `Plan` string for now; see the
  dedicated section below, because this is the one backend with no host-side
  share primitive at all and it is the platform a large share of users will be
  on.

**Gate in `internal/cli/create.go` and `internal/cli/start.go`**, alongside the
existing DenyLAN/ACL/Port gates:

```go
if len(policy.Mounts) > 0            { gateOrBlock(…, FeatureMount, fp) }
if anyNonWritable(policy.Mounts)     { gateOrBlock(…, FeatureMountReadOnly, fp) }
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

## Hyper-V: no host-side share primitive at all

Incus and Lima both hand the guest a filesystem over a hypervisor channel
(virtiofs/9p, or Lima's reverse-sshfs). **Hyper-V has no equivalent.** There is
no virtiofs, no 9p, and no VMware-Tools-style shared-folder feature; Enhanced
Session Mode's drive redirection is RDP-based and aimed at Windows guests, not
something agentctl can drive for a Linux guest. For a Linux guest the options
reduce to three:

- **SMB/CIFS over the guest network** — the standard, documented answer.
- **`Copy-VMFile`** over the Guest Service Interface — one-way host → guest, a
  copy rather than a mount, and it needs the Hyper-V guest daemons present in
  the image.
- **Attach a VHDX** as a block device — real storage, but not a host directory.

### The cross-feature consequence: mounts become network traffic

This is the part worth deciding deliberately rather than discovering during
implementation. On Hyper-V, every option that produces a *live* mount requires
the guest to reach a service **on the host**, which runs straight into
`--deny-lan`, on by default. The Hyper-V Default Switch is a NAT switch handing
out RFC1918 addresses (172.x, with the subnet documented to change across host
reboots), and an Internal switch is typically 192.168.x — both squarely inside
the ranges `denyLAN` blocks.

So on Hyper-V, "mount a directory" and "block the LAN" are in direct tension in
a way they are not on the other two backends. The answer is not to drop either:
it is to make the exception narrow, explicit and visible — a single allow rule
for the host's own vSwitch address on the single port required, emitted as part
of applying mount policy and surfaced in `status` and `--preview` exactly like
any `--allow` entry. What must not happen is a mount silently widening the
network policy. This also means `fs.mount` on Hyper-V has a real dependency on
`network.acl` being implemented there, which it is not yet.

### Three candidate designs

**A. SMB share — ship as the `ManualWorkaround` Plan string now.**
`New-SmbShare -Name agentctl-<inst> -Path <hostdir>`, guest mounts
`mount -t cifs //<host-ip>/agentctl-<inst> /workspace`. Well-trodden and real.
Costs: a credential has to reach the guest, and a sandbox holding host SMB
credentials is itself a lateral-movement primitive (NTLM relay is the classic
abuse) — which is uncomfortably close to the threat `docs/admin/security-model.md`
says the tool exists to prevent. Read-only has to come from the share ACL rather
than a mount flag. Good as a documented manual procedure; poor as an automated
default.

**B. Reverse-SFTP served by agentctl — the recommended native path.**
Exactly what Lima does by default (`reverse-sshfs`): the *host* runs the file
server and the guest mounts it over SSH. agentctl would embed one (Go's
`pkg/sftp` over `x/crypto/ssh`), bound to the vSwitch address only, authenticated
with a per-instance keypair, **serving only the named directories as its
filesystem roots**.

The reason to prefer this is not convenience. It is that the named-directory
restriction stops being a configuration assertion and becomes structural: with
Incus and Lima, "only these directories" is a property of how the hypervisor was
configured, and a different configuration would expose more. With a served root,
the guest cannot *name* a path outside it — the server has no handle to hand
back. Read-only becomes a server-side decision too, which sidesteps the "does
the backend actually honor `readonly`?" question entirely. And it reuses a
design with years of production use behind it in Lima rather than inventing one.

Costs, stated plainly:

- agentctl is a one-shot CLI. This requires a **host-side process living as long
  as the VM**. Lima has `limactl`'s host agent for exactly this; agentctl has no
  daemon concept at all. That is the largest architectural addition anywhere in
  this plan.
- Slower than virtiofs, and inotify/file-watching semantics differ — which
  matters, because watch-mode build tools are exactly what runs in these
  sandboxes.
- Windows filesystem semantics leak through: case-insensitivity, ACLs instead of
  POSIX modes, path-length limits.

**C. VHDX workspace disk.** Attach a `.vhdx`; the guest formats and mounts it.
No network, no credentials, strong isolation — but the host can only read it
while the VM is stopped (`Mount-VHD`), so it cannot support "edit in my IDE on
the host while the agent builds in the guest." Worth keeping in mind as a
persistent *cache/scratch* volume (the build-cache question raised in
`image-workflow.plan.md`), not as a project mount.

### Recommendation and sequencing

Ship **A** as the capability entry's `Plan` string, following the pattern
`network.port-publish` already uses for Hyper-V's other genuine platform gap —
the mechanism and the `Plan` field exist, so this costs almost nothing and stops
agentctl from silently pretending. Design **B** as the native path, in **its own
plan file**: a host-side daemon is a big enough architectural change that
folding it into this plan's phases would be dishonest about the cost, and once
it exists it may well be worth using on Lima and Incus too rather than staying a
Hyper-V special case.

Sequencing is constrained by something outside this plan: the Hyper-V backend is
a stub in *every* respect — `create`/`start`/`stop` are all `UnderDevelopment`.
Mounts cannot be usable there before the lifecycle is. So the capability entry
and Plan string land with Phase 2, and B is gated behind the Hyper-V backend
existing at all.

**Verification note**, matching the convention `incus/translate.go` and
`lima/translate.go` already follow: none of the Hyper-V specifics above have
been exercised against a live Windows host in this environment. The Default
Switch's changing subnet, `Copy-VMFile`'s guest-daemon requirements, and the
exact SMB/cifs invocation all come from documentation and need confirming before
anything is built on them.

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
  `parsePortFlag` tests: bare path, `:w`, `:ro`, explicit guest path, both, the
  malformed cases, and specifically the Windows forms (`C:\Users\me\src`,
  `C:\Users\me\src:/workspace:w`) that motivated right-to-left parsing.
- `ResolveMounts` tests against a `t.TempDir()`: template/`~` expansion, symlink
  escape, `..` traversal, allowed-root boundary (`/src` vs `/srcevil`), deny
  paths, missing-directory creation with mode 0700, guest-path collision, host
  and guest overlap. Two cases carry decision 4 specifically: mounting `$HOME`
  is refused by default and permitted only under `allowHome: true`, and a path
  *inside* `$HOME` (the per-instance workspace) is still allowed.
- Merge tests for `mountPolicy`'s narrow-only rule, including the failure case
  where a later profile tries to widen `allowedRoots`.
- Lima translate test: `.mounts = []` always emitted, and emitted first; the
  same expressions appear on the `start` invocation for an existing instance.
- Incus translate test: `readonly=true` emitted when `Writable` is false and
  omitted when true (the one place the polarity is negated); the remove-then-add
  reconfigure sequence only touches `agentctl-mount-*` devices and leaves a
  hand-added device alone.
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
capability features, the `create`/`start` gates, and Hyper-V's
`ManualWorkaround` entry with its SMB Plan string.

**Phase 3 — visibility.** `provider.Instance.Mounts`, `status` output, doc
updates, integration tests.

## To settle during implementation

Not design questions — things the code will answer, recorded so they are not
forgotten.

1. **Guest-side ownership.** A virtiofs/9p share lands with some uid/gid; the
   provisioned non-root `agent` user must be able to read it, and write it for
   writable mounts. Whether that needs a mount option, a `chown` in
   `bootstrap-user.sh`, or is handled by the backend differs per provider.
   Answer it with the Phase 1 integration tests rather than guessing.
2. **Whether Incus enforces `readonly=true` on VM shares.** The disk device's
   `readonly` option is documented without a container-only condition, but VM
   shares go through virtiofs/9p and this has not been verified live. The
   integration test asserts a write to a read-only share fails; the
   `fs.mount.readonly` capability entry is then written to match — `Supported`
   if it holds, `ManualWorkaround` ("mount the source read-only on the host
   first") if it does not.
3. **Lima's `.mounts` reset syntax.** Lima's own `--mount-none` emits
   `.mounts = null`; `.mounts = []` should be equivalent for the subsequent
   `+=` appends, but which one the bundled yq dialect prefers needs confirming
   against a live `limactl` — the same caveat `lima/translate.go`'s package NOTE
   already carries for the whole `--set` mechanism.

## Remaining open question

**Hyper-V's native path — design B (host-side reverse-SFTP) as its own plan, or
something else?** Everything else here is decided. This one is open because it
introduces a host-side daemon to a tool that has been a one-shot CLI, and
because the answer plausibly changes the other two backends too (a served root
enforces the named-directory restriction structurally, in a way virtiofs
configuration does not). The recommendation is to write it up separately rather
than fold it into this plan's phases — but whether Windows support waits on that
daemon, or ships on the SMB manual workaround for a release or two, is a
scheduling call worth making explicitly.
