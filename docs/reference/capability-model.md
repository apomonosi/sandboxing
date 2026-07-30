# Capability Model (internals)

Source of truth: `internal/provider/capability.go`. This is the mechanism
behind the "not available / under development / manual workaround" messages
described in [Troubleshooting](../user/troubleshooting.md) and the
[Capability Matrix](../admin/capability-matrix.md).

## Types

```go
type Status int
const (
    Supported Status = iota
    NotAvailable
    UnderDevelopment
    ManualWorkaround
)

type Feature string // e.g. FeatureCreate, FeatureNetworkACL, FeaturePortPublish

type Capability struct {
    Feature     Feature
    Status      Status
    Message     string // always shown, one-liner
    Plan        string // populated only for ManualWorkaround: a concrete,
                        // copyable procedure
    TrackingURL string // populated for UnderDevelopment: a roadmap link
}

type Table map[Feature]Capability
func (t Table) Get(f Feature) Capability // missing entry -> NotAvailable, never panics
```

`AllFeatures` is the closed set every provider must cover; a table-
completeness test (`internal/provider/capability_completeness_test.go`) runs
against every provider package — including the Lima/Hyper-V stubs — and
fails if any `Feature` is missing an entry, so a newly added feature can't
silently degrade to "unknown feature: not available" without someone
noticing.

## The two kinds of gap

It matters which of these a `Capability` represents:

- **`UnderDevelopment`**: the backend supports this fine; `agentctl`'s own
  integration just isn't built yet. This is the overwhelming majority of the
  Lima and Hyper-V stub tables today — see the
  [capability matrix](../admin/capability-matrix.md) for exactly which cells
  these are.
- **`NotAvailable` / `ManualWorkaround`**: the backend's platform genuinely
  lacks the primitive (Lima has no ACL object; Hyper-V has no port-publish
  primitive) or the relevant upstream API is too unstable to build on yet
  (`limactl snapshot`). `ManualWorkaround` additionally means a concrete
  procedure exists — the `Plan` field carries it.

Conflating these would either overstate a genuine platform limitation as
"just not implemented yet" or understate real, working functionality as a
permanent gap — both are misleading to someone deciding whether to trust a
given backend for a given task.

## Dispatch flow

Every leaf command in `internal/cli`:

1. Resolves the active `Provider` (`--provider` flag, falling back to
   persisted config).
2. Looks up `provider.Capabilities().Get(feature)` for the feature(s) it's
   about to touch — some conditionally, based on which flags were actually
   passed (e.g. `create` only checks `FeatureNetworkACL` if `--allow` was
   given, but always checks `FeatureDenyLAN` since that's on by default).
3. Calls `CapabilityGate` (`internal/cli/capability_gate.go`), a pure
   function (`io.Writer` in, no globals) that prints the standard message
   for the status and returns whether to proceed, plus an exit-code hint.
4. Only on `Supported` (or `ManualWorkaround` + `--force-partial`) does the
   command actually call the `Provider` method.

`CapabilityGate` is the single place all four statuses' user-facing copy and
exit codes are defined, so every command's behavior is consistent and the
whole thing is testable without Cobra or a real provider — see
`internal/cli/capability_gate_test.go`.

## Extending it

Adding a new provider is: implement the `Provider` interface, build a
complete `capability.Table` covering every `Feature` in `AllFeatures`
(honestly categorizing each gap per the two kinds above), and register a
`Factory` for it in `cmd/agentctl/main.go`. Nothing in `internal/cli` needs
to change.
