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

	// Hyper-V is the one backend with no host-side share primitive at all:
	// no virtiofs, no 9p, and no VMware-Tools-style shared folder.
	// Enhanced Session Mode's drive redirection is RDP-based and aimed at
	// Windows guests, not something agentctl can drive for a Linux guest.
	// So this is a genuine platform gap like network.port-publish below,
	// not unfinished wiring, and it stays a documented manual procedure
	// until a native path is built.
	//
	// The native path being designed is a host-side SFTP server serving
	// only the named directories, which is what Lima does by default
	// (reverse-sshfs) — see filesystem-access.plan.md. It needs agentctl
	// to gain a host-side daemon, which it has never had, so it is
	// deliberately not started here.
	//
	// Note the cross-feature consequence of every option in this space:
	// on Hyper-V a mount is network traffic, so it interacts with
	// --deny-lan, which blocks the RFC1918 ranges the Default Switch hands
	// out. Whatever lands must open that hole narrowly and visibly rather
	// than silently widening the network policy.
	mountPlan := `Manual workaround (Windows host, elevated PowerShell):
1. Share the directory on the host:
     New-SmbShare -Name "agentctl-<name>" -Path <hostDir> -FullAccess <account>
2. Allow the guest to reach the host's vSwitch address on TCP 445 only —
   this is a deliberate exception to --deny-lan, so keep it to that one
   address and port.
3. In the guest:
     mount -t cifs //<host-vswitch-ip>/agentctl-<name> <guestPath> \
       -o username=<account>,vers=3.1.1
4. For a read-only mount, enforce it with the share ACL (-ReadAccess
   instead of -FullAccess) and add ",ro" to the mount options.
Note the guest then holds host SMB credentials, which is itself a
lateral-movement risk; prefer a dedicated low-privilege local account.`

	t[provider.FeatureMount] = provider.Capability{
		Feature: provider.FeatureMount,
		Status:  provider.ManualWorkaround,
		Message: "Hyper-V has no host-directory share primitive (no virtiofs, no 9p); sharing a folder with a Linux guest goes over SMB on the guest network.",
		Plan:    mountPlan,
	}
	t[provider.FeatureMountReadOnly] = provider.Capability{
		Feature: provider.FeatureMountReadOnly,
		Status:  provider.ManualWorkaround,
		Message: "Read-only comes from the SMB share ACL rather than a mount flag agentctl can set.",
		Plan:    mountPlan,
	}

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
