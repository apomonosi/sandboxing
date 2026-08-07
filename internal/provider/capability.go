package provider

// Status describes how well a Feature is supported by a given Provider.
type Status int

const (
	// Supported means the provider implements this feature natively.
	Supported Status = iota
	// NotAvailable means the provider's platform has no way to do this and
	// no workaround is known. agentctl says so honestly instead of failing
	// with a raw error.
	NotAvailable
	// UnderDevelopment means agentctl knows how to implement this but
	// hasn't yet. A roadmap item, not a platform limitation.
	UnderDevelopment
	// ManualWorkaround means the provider's platform can achieve the same
	// effect, but not through agentctl automation yet. The Capability's
	// Plan field carries a concrete, copyable procedure.
	ManualWorkaround
)

func (s Status) String() string {
	switch s {
	case Supported:
		return "supported"
	case NotAvailable:
		return "not available"
	case UnderDevelopment:
		return "under development"
	case ManualWorkaround:
		return "manual workaround available"
	default:
		return "unknown"
	}
}

// Feature identifies one capability-checkable unit of CLI behavior. Kept as
// a closed set of constants (not a free-form string) so providers and
// internal/cli share one vocabulary and typos are caught at compile time.
type Feature string

const (
	FeatureCreate          Feature = "create"
	FeatureStart           Feature = "start"
	FeatureStop            Feature = "stop"
	FeatureDelete          Feature = "delete"
	FeatureList            Feature = "list"
	FeatureStatus          Feature = "status"
	FeatureExec            Feature = "exec"
	FeatureShell           Feature = "shell"
	FeatureView            Feature = "view"
	FeatureSnapshotCreate  Feature = "snapshot.create"
	FeatureSnapshotList    Feature = "snapshot.list"
	FeatureSnapshotRestore Feature = "snapshot.restore"
	FeatureSnapshotDelete  Feature = "snapshot.delete"
	FeatureNetworkACL      Feature = "network.acl"          // --allow / --allow-file egress allowlist
	FeatureDenyLAN         Feature = "network.deny-lan"     // --deny-lan / --allow-lan
	FeaturePortPublish     Feature = "network.port-publish" // --port host:guest
	FeatureMount           Feature = "fs.mount"             // --mount host directory shares
	// FeatureMountReadOnly is deliberately separate from FeatureMount: a
	// backend that mounts but silently ignores read-only gives the sandbox
	// write access to host files while appearing to comply, which the user
	// should hit the capability gate over rather than discover later. Same
	// reasoning that makes network.deny-lan its own feature.
	FeatureMountReadOnly Feature = "fs.mount.readonly"
	FeatureImagePull     Feature = "image.pull"
	FeatureImageBuild    Feature = "image.build"
	FeatureLogsNetwork   Feature = "logs.network"
	FeatureLogsExec      Feature = "logs.exec"
)

// AllFeatures is the closed set of every Feature a provider must have an
// entry for. Used by the capability table-completeness test so a new
// Feature constant can't be added without also updating every provider's
// table (including stubs).
var AllFeatures = []Feature{
	FeatureCreate, FeatureStart, FeatureStop, FeatureDelete, FeatureList, FeatureStatus,
	FeatureExec, FeatureShell, FeatureView,
	FeatureSnapshotCreate, FeatureSnapshotList, FeatureSnapshotRestore, FeatureSnapshotDelete,
	FeatureNetworkACL, FeatureDenyLAN, FeaturePortPublish,
	FeatureMount, FeatureMountReadOnly,
	FeatureImagePull, FeatureImageBuild,
	FeatureLogsNetwork, FeatureLogsExec,
}

// Capability describes one (provider, feature) cell in the support matrix.
type Capability struct {
	Feature Feature
	Status  Status
	// Message is a short, human-facing one-liner always shown to the user.
	Message string
	// Plan is populated when Status is ManualWorkaround (optionally also
	// UnderDevelopment, to preview a design): a concrete, copy-pasteable
	// procedure the user/admin can run by hand right now. Empty otherwise.
	Plan string
	// TrackingURL optionally points at a roadmap issue for
	// UnderDevelopment items.
	TrackingURL string
}

// Table is the full capability matrix for one provider, keyed by Feature.
// Providers build this once at construction time and return it from
// Provider.Capabilities(). It should have an entry for every Feature in
// AllFeatures; a missing entry is treated as NotAvailable rather than
// panicking or silently allowing the operation.
type Table map[Feature]Capability

// Get returns the capability for f, failing safe to NotAvailable if the
// provider's table has no entry for it.
func (t Table) Get(f Feature) Capability {
	if c, ok := t[f]; ok {
		return c
	}
	return Capability{
		Feature: f,
		Status:  NotAvailable,
		Message: "unknown feature: no capability entry declared for this provider",
	}
}
