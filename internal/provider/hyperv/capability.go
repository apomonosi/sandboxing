package hyperv

import "github.com/apomonosi/sandboxing/internal/provider"

// buildCapabilities returns Hyper-V's capability table. Unlike Lima,
// almost every feature here has a strong, mature native primitive
// (Extended Port ACLs, Standard/Production checkpoints, VMConnect) —
// the gaps are almost entirely "agentctl hasn't wired the PowerShell
// integration up yet" (UnderDevelopment), with one genuine platform gap:
// Hyper-V has no "publish a port" primitive (ManualWorkaround, via a NAT
// switch + netsh portproxy).
func buildCapabilities() provider.Table {
	t := make(provider.Table, len(provider.AllFeatures))

	underDev := func(f provider.Feature, msg string) {
		t[f] = provider.Capability{
			Feature:     f,
			Status:      provider.UnderDevelopment,
			Message:     msg,
			TrackingURL: "https://github.com/apomonosi/sandboxing/issues", // placeholder until real issues exist
		}
	}

	underDev(provider.FeatureCreate, "Hyper-V supports this natively via New-VM; agentctl's Hyper-V backend isn't implemented yet.")
	underDev(provider.FeatureStart, "Hyper-V supports this natively via Start-VM; agentctl's Hyper-V backend isn't implemented yet.")
	underDev(provider.FeatureStop, "Hyper-V supports this natively via Stop-VM; agentctl's Hyper-V backend isn't implemented yet.")
	underDev(provider.FeatureDelete, "Hyper-V supports this natively via Remove-VM; agentctl's Hyper-V backend isn't implemented yet.")
	underDev(provider.FeatureList, "Hyper-V supports this natively via Get-VM; agentctl's Hyper-V backend isn't implemented yet.")
	underDev(provider.FeatureStatus, "Hyper-V supports this natively via Get-VM; agentctl's Hyper-V backend isn't implemented yet.")
	underDev(provider.FeatureExec, "Hyper-V supports this natively via PowerShell Direct/Enter-PSSession; agentctl hasn't wired it up yet.")
	underDev(provider.FeatureShell, "Hyper-V supports this natively via PowerShell Direct/Enter-PSSession; agentctl hasn't wired it up yet.")
	underDev(provider.FeatureView, "Hyper-V has a mature native console (VMConnect/Enhanced Session Mode); agentctl hasn't wired it up yet.")
	underDev(provider.FeatureSnapshotCreate, "Hyper-V checkpoints (Standard/Production) are native and mature; agentctl hasn't wired them up yet.")
	underDev(provider.FeatureSnapshotList, "Hyper-V checkpoints (Standard/Production) are native and mature; agentctl hasn't wired them up yet.")
	underDev(provider.FeatureSnapshotRestore, "Hyper-V checkpoints (Standard/Production) are native and mature; agentctl hasn't wired them up yet.")
	underDev(provider.FeatureSnapshotDelete, "Hyper-V checkpoints (Standard/Production) are native and mature; agentctl hasn't wired them up yet.")
	underDev(provider.FeatureNetworkACL, "Hyper-V Extended Port ACLs (Add-VMNetworkAdapterExtendedAcl) are native; agentctl hasn't wired them up yet.")
	underDev(provider.FeatureDenyLAN, "Same native primitive as network.acl (Extended Port ACLs); agentctl hasn't wired it up yet.")
	underDev(provider.FeatureImagePull, "agentctl hasn't wired a signed/checksummed VHDX pull path up yet.")
	underDev(provider.FeatureImageBuild, "agentctl hasn't wired a Hyper-V unattended-install JIT build path up yet.")
	underDev(provider.FeatureLogsNetwork, "Hyper-V Extended ACL logging and port mirroring are native; agentctl hasn't wired log collection up yet.")
	underDev(provider.FeatureLogsExec, "agentctl hasn't wired exec history collection up yet.")

	t[provider.FeaturePortPublish] = provider.Capability{
		Feature: provider.FeaturePortPublish,
		Status:  provider.ManualWorkaround,
		Message: "Hyper-V has no port-publish primitive; it requires a NAT switch plus a portproxy rule.",
		Plan: `Manual workaround (Windows host, elevated PowerShell):
1. Ensure the VM is attached to a NAT-type virtual switch with a known
   guest IP address.
2. netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 ^
     listenport=<hostPort> connectaddress=<guestIP> connectport=<guestPort>
3. New-NetFirewallRule -DisplayName "agentctl-<name>-<hostPort>" ^
     -Direction Inbound -LocalPort <hostPort> -Protocol TCP -Action Allow
4. Remove with:
     netsh interface portproxy delete v4tov4 listenport=<hostPort> listenaddress=0.0.0.0`,
	}

	return t
}
