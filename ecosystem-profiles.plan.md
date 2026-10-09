# Built-in `default` Fallback & Ecosystem Profiles — Preserved Implementation

**Status:** Unlike the other `*.plan.md` files in this repo, this is **not** a
brainstorm. The code described here is *written, documented and tested* — it
exists as commit `1f0c2ea` on the branch
`claude/safe-ai-network-brainstorm-uwolel` (15 files, +394/−31) and was never
merged. `main` has since moved 12 commits past it, and the two now conflict
in a way that is partly a genuine design disagreement rather than a textual
clash.

This document exists so the branch can be deleted without losing either the
reasoning or the researched domain lists, and so the open decisions can be
settled before the code is re-applied. It was requested after the conflicts
were diagnosed; it is a record, not a proposal.

## Why it isn't just rebased

A dry-run rebase of `1f0c2ea` onto `main` (`f2bd284`) produces five
conflicts:

```
docs/user/cli-reference.md
docs/user/profiles-and-policies.md
internal/cli/mount_cli_test.go
internal/profile/defaults.go
internal/profile/embedded/default.yaml
```

Two are mechanical:

- **`defaults.go`** — both sides add entries to the same `builtinProfiles`
  map. `main` added `terminal` and `gui`; this commit adds `python`, `node`,
  `go` and `rust`. The union is eight built-ins.
- **`mount_cli_test.go`** — both sides changed the same tests for the same
  reason (the mount-overlap error), independently and similarly.

One is not:

- **`default.yaml`** — the two commits make *opposite* decisions about what
  the baseline profile means. See "The disagreement" below.

The remaining two are documentation following from `default.yaml`.

---

## What the commit contains

Four separable changes.

### 1. A built-in `default` fallback

New in `internal/cli/root.go`, replacing duplicated logic in `create.go` and
`start.go`:

```go
// resolveProfileNames returns the profiles a command should apply, in
// precedence order: explicit --profile flags, else the configured
// defaultProfile, else the built-in "default".
//
// That last fallback is the important one. Without it, a create with no
// --profile and no configured defaultProfile resolved against an empty
// policy: no resource limits, and — since the built-in profiles are where
// the per-instance workspace mount is declared — no host directory at
// all. An agent installed via --agent would come up with nowhere to work.
// Falling back to "default" is not a widening: it is default-deny egress
// with LAN blocked, so it constrains an instance that previously had no
// policy attached.
//
// A file-based profile named "default" in the search paths still wins
// over the built-in, since this only chooses a name and LoadNamed
// resolves it (see profile.SearchPaths).
func resolveProfileNames(cmd *cobra.Command, explicit []string) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if def := configFromContext(cmd.Context()).DefaultProfile; def != "" {
		return []string{def}
	}
	return []string{profile.Default}
}
```

This is the behavior gap observed independently on `main` while testing
`--package`: a bare `agentctl create demo --image=... --package=git` resolves
against an empty policy, which is why `agentctl: no host directories are
exposed to this sandbox` appears on every bare create.

Note precisely what it does and does not fix. Combined with change (2) below
— which empties `default`'s allowlist — a bare create would gain the
`/workspace` mount and resource limits but **still have no egress
allowlist**. That is probably correct, but it should be a deliberate choice
rather than an emergent one.

Also adds `profile.Default = "default"` as a named constant, so the fallback
is greppable, and sorts `BuiltinNames()` so `agentctl profile list` stops
following Go's randomized map order.

### 2. De-Claude-ifying `default`

`default.yaml`'s allowlist becomes empty, with the reasoning in the file:

```yaml
  network:
    denyLAN: true
    # Empty by design. This profile used to pre-approve *.anthropic.com
    # plus PyPI, which made the baseline both agent- and language-flavored:
    # a --agent=gemini user inherited an unused Anthropic grant, and a Go
    # user inherited Python registries. Those now live where they vary —
    # the agent registry (internal/agent) and the python profile.
    allow: []
```

### 3. Four ecosystem profiles

`python`, `node`, `go`, `rust`. Each carries **egress entries only** — no
resources, mounts or `mountPolicy` — so they layer on top of a base profile
rather than competing with it:

```console
$ agentctl create demo --image=images:fedora/44 --profile=default --profile=python
```

Two invariants make that work, both recorded in `defaults.go` because both
are easy to break when adding a fifth:

- **They must declare no mounts.** `Merge` unions mount lists, so an
  ecosystem profile that also mounted `/workspace` would collide with the
  base profile's workspace mount and `ResolveMounts` would reject the pair
  as overlapping.
- **They must set `denyLAN` explicitly.** `Merge` takes the override's
  `denyLAN` as-is, so one that omitted it would resolve to `false` and
  silently unblock the LAN when layered last.

The domain lists are the part most worth preserving — each non-obvious entry
was researched rather than guessed:

| Profile | Domains | Why the non-obvious ones |
|---|---|---|
| `python` | `pypi.org`, `files.pythonhosted.org` | Package *files* are served from a separate host; installs fail with only `pypi.org` allowed. |
| `node` | `registry.npmjs.org` | — |
| `go` | `proxy.golang.org`, `sum.golang.org`, `index.golang.org`, `storage.googleapis.com` | `sum.golang.org` is consulted by default, so a build fails without it. `proxy.golang.org` serves some archives by 302-redirecting to GCS, so downloads fail *intermittently* without `storage.googleapis.com` — a deliberately broad grant (every GCS bucket shares that host), droppable with a private `GOPROXY`. |
| `rust` | `index.crates.io`, `crates.io`, `static.crates.io` | `index.crates.io` is the sparse index, Cargo's default since Rust 1.70. Archives come from `static.crates.io`, so builds fail with only the index allowed. |

Every one of these is a concrete hostname. None is a wildcard — see
`TestRegistry_NoWildcardDomains` and `TestBuiltins_NoWildcardDomains` on
`main` for why that matters (a `*.` rule resolves the apex only).

### 4. `--mount-only` guidance in the overlap error

`internal/profile/mount.go`, in `checkOverlap`. **Absent from `main`** —
verified:

```go
// The common way to hit this is a --mount at the same guest
// path as the active profile's own workspace mount, since
// --mount accumulates on top of a profile rather than
// replacing it. Name the escape hatch here: the error is
// otherwise accurate but gives no hint what to do about it.
errs = append(errs, fmt.Errorf(
	"mounts %s and %s both mount at guestPath %s (use --mount-only to replace the profile's mounts instead of adding to them)",
	a.HostPath, b.HostPath, a.GuestPath))
```

This one is independent of everything else here and conflicts with nothing.
It could be lifted on its own at any time.

### Tests the commit adds

Worth re-creating alongside whatever is re-applied:

- `TestCLI_BareCreateAppliesBuiltinDefault` — the behavior the fallback
  exists for.
- `TestCLI_ConfiguredDefaultProfileWinsOverBuiltin` — the fallback is a last
  resort, not an override.
- `TestBuiltins_AllLoadAndValidate` — guards *every* embedded profile, so a
  malformed one fails the suite instead of the first user who names it.
  (`main` has since grown its own `TestBuiltins_AreValid`, which overlaps.)
- `TestEcosystemProfiles_ComposeSafely` — pins the no-mounts and
  explicit-`denyLAN` invariants above. Both are silent failures if broken.
- `TestEcosystemProfile_LayersOntoDefault` — the composition the docs
  instruct users to run actually behaves: base resources and workspace
  survive, ecosystem domains are added rather than replacing.

---

## The disagreement

`default.yaml` conflicts because the two commits answer the same question
differently:

| | `main` (PR #19) | this commit |
|---|---|---|
| `default`'s allowlist | `api.anthropic.com`, `pypi.org`, `files.pythonhosted.org` | `allow: []` |
| Anthropic egress lives in | `default` | the agent registry (`--agent=claude`) |
| PyPI egress lives in | `default` | a composable `python` profile |

`main`'s version is not a considered position. It only changed
`*.anthropic.com` to `api.anthropic.com` — a wildcard-apex bug fix — while
editing that file for an unrelated reason, and left the list's *membership*
as it found it. This commit's position is the reasoned one and is the earlier
explicit decision: a `--agent=gemini` user should not inherit an unused
Anthropic grant, and a Go user should not inherit Python registries.

**Recommendation: take this commit's `default.yaml`.**

## The overlap `main` introduced

`main` now has `packages:` (`internal/packages`, PR #19), which this commit
predates. That creates a shape problem the commit could not have
anticipated: there are now **two kinds of built-in profile on two unstated
axes**.

| | What it does | Carries |
|---|---|---|
| `terminal`, `gui` | *installs* a toolchain | `packages:`, same policy as `default` |
| `python`, `node`, `go`, `rust` | *allows* a registry | egress only, no packages |

Eight built-ins split across two axes nobody has named is confusing. And
`python.yaml` allowing PyPI while not installing `python3` — which it now
could, in one line, via the catalog — reads as an omission rather than a
choice.

## Open questions to resolve before re-applying

1. **Does `default` keep a starter allowlist, or go empty?** The central
   question. Recommendation above, but it is a user-visible behavior change
   either way and should be signed off, not inferred.
2. **Do the ecosystem profiles gain `packages:`?** That would turn them from
   "allow language X's registries" into "set up for language X" — e.g.
   `python` installing `python3` and allowing PyPI. More useful, and it
   collapses the two axes into one.
3. **Is `terminal`/`gui` × `python`/`node`/`go`/`rust` the right shape at
   all**, or does one axis fold into the other? Answering (2) mostly answers
   this.
4. **Should the fallback also give a bare create *some* egress?** With (1)
   resolved as recommended, a bare create gets a workspace and resource
   limits but no allowlist. Defensible — but say so on purpose.
5. **Does the `--mount-only` error-message fix go in now, separately?** It
   conflicts with nothing and improves an error users hit routinely.

## How to pick this up

The branch is the authoritative source for the exact code — read it with:

```
git show 1f0c2ea
git diff $(git merge-base origin/main claude/safe-ai-network-brainstorm-uwolel) \
         claude/safe-ai-network-brainstorm-uwolel
```

Once the questions above are answered, re-apply rather than rebase: the
commit is small, three of its five conflicts are in files `main` has
restructured since, and the `packages:` overlap means parts of it want to be
rewritten rather than replayed. Changes (1) and (4) apply almost as-is;
changes (2) and (3) should wait on questions 1–3.
