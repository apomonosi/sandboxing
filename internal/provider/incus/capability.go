package incus

import "github.com/apomonosi/sandboxing/internal/provider"

// buildCapabilities returns Incus's capability table. Incus has native
// primitives for nearly everything agentctl needs (profiles, network
// ACLs with address sets, proxy devices for port publishing, mature
// snapshots, SPICE console), so this milestone marks those Supported.
// The audit/log pipeline is intentionally left UnderDevelopment: Incus
// exposes the raw building blocks (ACL state, exec output) but agentctl's
// own log collection/formatting (internal/audit) isn't wired up yet —
// see the project plan's "explicitly out of scope this milestone" list.
func buildCapabilities() provider.Table {
	t := make(provider.Table, len(provider.AllFeatures))
	supported := func(f provider.Feature) {
		t[f] = provider.Capability{Feature: f, Status: provider.Supported}
	}

	supported(provider.FeatureCreate)
	supported(provider.FeatureStart)
	supported(provider.FeatureStop)
	supported(provider.FeatureDelete)
	supported(provider.FeatureList)
	supported(provider.FeatureStatus)
	supported(provider.FeatureExec)
	supported(provider.FeatureShell)
	supported(provider.FeatureView)
	supported(provider.FeatureSnapshotCreate)
	supported(provider.FeatureSnapshotList)
	supported(provider.FeatureSnapshotRestore)
	supported(provider.FeatureSnapshotDelete)
	supported(provider.FeatureNetworkACL)
	supported(provider.FeatureDenyLAN)
	supported(provider.FeaturePortPublish)
	supported(provider.FeatureMount)
	supported(provider.FeatureImagePull)
	supported(provider.FeatureImageBuild)

	// fs.mount.readonly is marked Supported on the strength of the disk
	// device's documented `readonly` option, which carries no
	// container-only condition. It is called out separately here because
	// it is the one mount behavior that could plausibly differ for VMs
	// (whose shares go through virtiofs/9p rather than a bind mount) and
	// has not been confirmed against a live daemon — the same caveat
	// translate.go's package NOTE carries for every incus flag in this
	// package. TestMountReadOnlyIsEnforced in integration_test.go is what
	// settles it; if a VM share turns out to ignore readonly, this entry
	// becomes ManualWorkaround ("mount the source read-only on the host
	// first") rather than staying a claim agentctl can't back.
	supported(provider.FeatureMountReadOnly)

	underDev := func(f provider.Feature, msg string) {
		t[f] = provider.Capability{
			Feature:     f,
			Status:      provider.UnderDevelopment,
			Message:     msg,
			TrackingURL: "https://github.com/apomonosi/sandboxing/issues",
		}
	}
	underDev(provider.FeatureLogsNetwork, "Incus exposes ACL state and instance events natively, but agentctl's audit log collection/formatting isn't wired up yet.")
	underDev(provider.FeatureLogsExec, "agentctl's exec history collection isn't wired up yet.")

	return t
}
