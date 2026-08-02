package lima

import "github.com/apomonosi/sandboxing/internal/provider"

// buildCapabilities returns Lima's capability table.
//
// Two different kinds of gap show up here, and it matters which is which:
//
//   - UnderDevelopment: Lima itself supports this fine; agentctl just
//     hasn't wired the `limactl` integration up yet. It's our roadmap,
//     not a platform limit.
//   - ManualWorkaround / NotAvailable: Lima's platform genuinely lacks the
//     primitive (no network ACL object) or the relevant upstream API is
//     too unstable to build on yet (limactl snapshot is explicitly
//     experimental).
//
// create/start/stop/delete/list/status/exec/shell/network.port-publish
// are Supported as of this backend's real implementation (lima.go,
// translate.go). image.pull is NotAvailable, not just unwired: Lima has
// no local named image store to pull into ahead of `create` — a
// template's base image resolves lazily, per-instance, inside
// `create`/`start` itself, so there's no daemon-side store to wire up to.
func buildCapabilities() provider.Table {
	t := make(provider.Table, len(provider.AllFeatures))

	supported := func(f provider.Feature) {
		t[f] = provider.Capability{Feature: f, Status: provider.Supported}
	}
	underDev := func(f provider.Feature, msg string) {
		t[f] = provider.Capability{
			Feature:     f,
			Status:      provider.UnderDevelopment,
			Message:     msg,
			TrackingURL: "https://github.com/apomonosi/sandboxing/issues", // placeholder until real issues exist
		}
	}

	supported(provider.FeatureCreate)
	supported(provider.FeatureStart)
	supported(provider.FeatureStop)
	supported(provider.FeatureDelete)
	supported(provider.FeatureList)
	supported(provider.FeatureStatus)
	supported(provider.FeatureExec)
	supported(provider.FeatureShell)
	supported(provider.FeaturePortPublish)

	underDev(provider.FeatureView, "Lima has no first-class GUI console; agentctl plans a dedicated VNC bridge against the VM's own display, not yet built.")
	underDev(provider.FeatureImageBuild, "agentctl hasn't wired a Lima cloud-init JIT build path up yet.")
	underDev(provider.FeatureLogsExec, "agentctl hasn't wired exec history collection up yet.")

	t[provider.FeatureImagePull] = notAvailable(provider.FeatureImagePull,
		"Lima has no local named image store to pull into ahead of create; a template's base image resolves lazily, per-instance, inside create/start itself.")

	t[provider.FeatureSnapshotCreate] = notAvailable(provider.FeatureSnapshotCreate,
		"limactl snapshot is explicitly experimental/unstable upstream; agentctl won't build on it until it stabilizes.")
	t[provider.FeatureSnapshotList] = notAvailable(provider.FeatureSnapshotList,
		"limactl snapshot is explicitly experimental/unstable upstream; agentctl won't build on it until it stabilizes.")
	t[provider.FeatureSnapshotRestore] = notAvailable(provider.FeatureSnapshotRestore,
		"limactl snapshot is explicitly experimental/unstable upstream; agentctl won't build on it until it stabilizes.")
	t[provider.FeatureSnapshotDelete] = notAvailable(provider.FeatureSnapshotDelete,
		"limactl snapshot is explicitly experimental/unstable upstream; agentctl won't build on it until it stabilizes.")

	t[provider.FeatureNetworkACL] = provider.Capability{
		Feature: provider.FeatureNetworkACL,
		Status:  provider.ManualWorkaround,
		Message: "Lima has no native egress allowlist primitive; agentctl cannot enforce this automatically yet.",
		Plan: `Manual workaround (macOS host, per Lima instance):
1. Identify the instance's vzNAT/socket_vmnet IP:
     limactl list --format '{{.Name}} {{.IPAddress}}'
2. On the host, add a pf anchor restricting that IP's egress to the
   allowed domains' resolved IPs (re-resolve on DNS TTL expiry):
     anchor "agentctl/<name>" {
       pass out quick from <instance-ip> to <allowed-ip> port {80,443}
       block out quick from <instance-ip> to any
     }
3. Load it: pfctl -a agentctl/<name> -f /path/to/anchor.conf
This must be redone on every instance restart until native support lands.`,
	}
	t[provider.FeatureDenyLAN] = provider.Capability{
		Feature: provider.FeatureDenyLAN,
		Status:  provider.ManualWorkaround,
		Message: "Same gap as network.acl: Lima has no native ACL object to block RFC1918/link-local egress.",
		Plan: `Manual workaround (macOS host): extend the pf anchor from the
network.acl workaround with explicit block rules for RFC1918 and
link-local ranges, placed before any "pass" rules:
  block out quick from <instance-ip> to 10.0.0.0/8
  block out quick from <instance-ip> to 172.16.0.0/12
  block out quick from <instance-ip> to 192.168.0.0/16
  block out quick from <instance-ip> to 169.254.0.0/16`,
	}
	t[provider.FeatureLogsNetwork] = notAvailable(provider.FeatureLogsNetwork,
		"No native egress log source exists without the network.acl workaround's pf anchor in place first.")

	return t
}

func notAvailable(f provider.Feature, msg string) provider.Capability {
	return provider.Capability{Feature: f, Status: provider.NotAvailable, Message: msg}
}
