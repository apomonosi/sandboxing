# Lima pf Firewall Assistance — Design Notes (Not Yet Implemented)

**Status:** Brainstorm/design discussion only. Nothing described here has
been implemented, no code has been written, and no decision has been made
on the open questions at the bottom. This file exists purely to preserve
the discussion so it can be picked up again later without re-deriving it.

## The question

`network.acl`/`network.deny-lan` on Lima are `ManualWorkaround`
(`internal/provider/lima/capability.go`): agentctl prints a static pf-anchor
procedure and the user runs it by hand, because Lima has no native ACL
primitive the way Incus does. Could agentctl do more — run pre/post tasks
around the Lima instance lifecycle to manage the pf rules itself — even
though `pfctl` needs root? The instinct proposed: yes, and printing the
exact commands agentctl is about to (or would) run is the transparent way
to do it, rather than hiding `sudo` calls inside the tool.

This extends the same principle `CommandPreviewer`/`--preview` already
commits to elsewhere in this codebase (preview output must never lie about
what actually runs) into the one place that needs it most, since this
would be the first operation in the whole codebase that crosses from
"runs as the invoking user" into "needs root."

## Where the pre/post hooks naturally land

The IP a pf anchor needs to reference doesn't exist until the instance is
actually running (`limactl list --format '{{.Name}} {{.IPAddress}}'`, per
the current `Plan` text), and Lima's `Create()` deliberately never starts
the instance (see `internal/provider/lima/lima.go`'s `Create` doc comment).
So the natural hook points are:

- **Apply on `Start()`** — once the instance has a real IP.
- **Tear down on `Stop()`/`Delete()`** — so a stale anchor never silently
  applies to a different instance that later reuses the same
  DHCP-assigned IP.

Hooking this to `Start()` would also turn the existing `Plan` text's
caveat ("must be redone on every instance restart") from something the
user has to remember into something agentctl handles automatically.

## Concrete blocker: IP extraction isn't implemented yet

This depends on parsing the instance's IP out of `limactl list --json`,
which the Lima backend deliberately left unpopulated —
`internal/provider/lima/json.go`'s `toInstance` doesn't set `Instance.IPs`
at all, specifically because the real JSON field name/casing is
unverified (see that file's NOTE comment). This can't move forward until
that's resolved against a real `limactl` install, or falls back to
parsing the `--format` Go-template output the `Plan` text already
references (a field name that's at least referenced in currently-shipped
docs, even if not yet parsed in code).

## Two tiers, worth scoping separately rather than as one feature

### Tier 1 — no privilege escalation in agentctl at all

Keep the `Plan` text's *content* dynamic instead of static: resolve the
real instance IP and the real `--allow` domain IPs (reusing whatever
domain-resolution logic Incus's `resolveAllowRules` already has — see
`internal/provider/incus/incus.go` — rather than duplicating it; likely
worth extracting into something shared across providers), generate the
actual pf anchor text and the actual `sudo pfctl ...` command line, and
print it at `start`/`stop` time. Zero sudo calls from agentctl itself,
zero new trust boundary — just replaces "copy this template and fill in
the blanks yourself" with "here's the exact, correct command for *this*
instance, right now." This alone removes most of the realistic failure
mode (hand-typed pf syntax errors, stale/wrong IPs).

### Tier 2 — agentctl actually runs it, opt-in

Same generated command, but behind an explicit flag (something like
`--apply-firewall`, in the same family as `--force-partial`/`--root`/
`--allow-lan` — visible, opt-in, never silent), with the exact command
printed *before* it runs, not just logged after. Meaningfully more
complex than Tier 1:

- `pfctl` needs root every time, so either every `start`/`stop` prompts
  for a sudo password (disruptive for scripted/CI use), or the user does
  a one-time setup step granting narrowly-scoped passwordless sudo for
  `pfctl -a agentctl/*` specifically. This is the same idiom
  `bootstrap-user.sh` already uses *inside* the guest for the non-root
  user's sudoers entry (`internal/provider/incus/embedded/bootstrap-user.sh`),
  but this time on the host — a bigger blast radius than a sandboxed
  guest, and would need its own careful docs section, not a one-liner.
- Cleanup matters more once apply is automatic: a stale anchor from a
  deleted instance must not silently apply to a different instance that
  later gets the same IP. Teardown on `Stop()`/`Delete()` stops being
  optional the moment apply is automatic.

## Capability-model question worth deciding explicitly

Neither tier makes `network.acl`/`network.deny-lan` `Supported` — it's
still not automatic-by-default the way Incus's ACL is, and Tier 2 still
needs an opt-in flag plus (likely) one-time host setup. It stays
`ManualWorkaround`; the question is whether "static instructions" vs.
"agentctl-generated-and-optionally-applied" deserves surfacing in the
`Status` enum itself (`internal/provider/capability.go`) or just in the
`Message`/`Plan` text. This project has been deliberate about each
`Status` value mapping to exactly one claim — leaning toward *not* growing
the enum and instead letting `Plan`/`--preview`-style output carry the
distinction, but this is an explicit open question, not a decision.

## Relationship to other in-flight brainstorms

- **`proxy.plan.md`** (Mode B): if a proxy narrows the ACL to a single
  "allow only proxy IP:port" rule, the pf anchor this feature would
  generate becomes much simpler and more static — no DNS-resolved-domain
  churn to track, since there's exactly one destination. Worth
  cross-referencing if both features move forward; the pf-anchor
  generation logic this file describes should probably be written
  generically enough to serve both the today's per-domain-allowlist case
  and a future proxy-only case.

## Open questions to resolve before this becomes an implementation plan

1. Tier 1 only, or Tier 1 + Tier 2 in scope together?
2. If Tier 2: how is the one-time host-level passwordless-sudo setup
   presented to the user — a documented manual step, or an
   `agentctl`-driven setup command that itself needs to explain what
   it's granting and why?
3. Does `Status` gain a new value, or does this stay `ManualWorkaround`
   with richer `Message`/`Plan` content?
4. Should pf-anchor generation be written now in a way that's reusable for
   the proxy-Mode-B case from `proxy.plan.md`, or kept narrowly scoped to
   today's per-domain allowlist until proxy support actually lands?
5. Resolved instance IPs are a snapshot, not dynamic — if Lima's DHCP/
   vzNAT allocation isn't sticky across restarts (unverified), does
   Tier 1's guidance need to say "IP may change on every start," and does
   Tier 2's automatic re-apply-on-`Start()` fully absorb that, or is there
   a window (e.g. mid-session IP renewal) neither tier covers?

## Where this fits once resolved

Once the open questions above are answered, this should go through the
same plan-mode workflow the Lima backend and non-root-user/JIT-agent
features did: explore existing patterns (`internal/provider/lima/json.go`,
`internal/provider/incus/incus.go`'s `resolveAllowRules`,
`internal/provider/lima/capability.go`, the `CommandPreviewer` interface),
design a concrete file-by-file plan, and get it reviewed before any code
is written.
