package incus

import (
	"fmt"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// NOTE: the exact `incus` CLI flag names used below follow Incus 6.x/7.0
// LTS documentation conventions but have not been exercised against a
// live daemon in this development environment (none is installed here).
// Before relying on this in production, verify each command against
// `incus <subcommand> --help` on a real install — the gated integration
// tests (incus_test.go, //go:build integration) are where a mismatch
// should be caught.

// rfc1918AndLinkLocal is blocked by default (--deny-lan) so a sandbox
// can't reach other devices on the same network.
var rfc1918AndLinkLocal = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
}

// aclName returns the deterministic per-instance network ACL name.
func aclName(instanceName string) string {
	return "agentctl-" + instanceName
}

// buildInitArgs returns the argument list for `incus init`, translating
// spec into instance type, profiles, and resource-limit config keys.
// Mounts and network policy are applied as separate follow-up commands
// (see incus.go's Create) rather than folded in here, since ACL creation
// needs the instance to exist first and both are independently useful to
// unit-test in isolation.
func buildInitArgs(spec provider.InstanceSpec) []string {
	args := []string{spec.Image, spec.Name, "--vm"}
	for _, p := range spec.Profiles {
		args = append(args, "-p", p)
	}
	if spec.Resources.CPUCores > 0 {
		args = append(args, "-c", fmt.Sprintf("limits.cpu=%d", spec.Resources.CPUCores))
	}
	if spec.Resources.Memory != "" {
		args = append(args, "-c", fmt.Sprintf("limits.memory=%s", spec.Resources.Memory))
	}
	return args
}

// buildRootDiskResizeArgs returns the `incus config device override` args
// to set the root disk size, if one was requested.
func buildRootDiskResizeArgs(instanceName, diskSize string) []string {
	if diskSize == "" {
		return nil
	}
	return []string{"config", "device", "override", instanceName, "root", "size=" + diskSize}
}

// buildMountDeviceArgs returns the `incus config device add` args to
// bind-mount one host path into the guest.
func buildMountDeviceArgs(instanceName, deviceName string, m provider.Mount) []string {
	args := []string{"config", "device", "add", instanceName, deviceName, "disk",
		"source=" + m.HostPath, "path=" + m.GuestPath}
	if m.ReadOnly {
		args = append(args, "readonly=true")
	}
	return args
}

// buildPortProxyDeviceArgs returns the `incus config device add` args for
// a host:guest port publish using Incus's proxy device type.
func buildPortProxyDeviceArgs(instanceName, deviceName string, pp provider.PortPublish) []string {
	proto := pp.Protocol
	if proto == "" {
		proto = "tcp"
	}
	return []string{"config", "device", "add", instanceName, deviceName, "proxy",
		fmt.Sprintf("listen=%s:0.0.0.0:%d", proto, pp.HostPort),
		fmt.Sprintf("connect=%s:127.0.0.1:%d", proto, pp.GuestPort),
	}
}

// networkACLCommands returns the full sequence of `incus` command
// invocations (each a separate []string of args, run in order) needed to
// create/replace the per-instance network ACL matching policy and attach
// it to the instance's NIC, implementing default-deny egress with an
// explicit allowlist and (if DenyLAN) an RFC1918/link-local block.
//
// Domain-based allow rules are resolved to IP addresses by the caller
// (see incus.go's resolveAllowRules) before this function is called,
// since Incus ACL rules match on IP/CIDR, not DNS names. This is a
// best-effort snapshot of a domain's current IPs, not dynamic DNS-aware
// filtering — an allow rule can go stale if the target rotates IPs (e.g.
// behind a CDN), and re-applying the policy is the way to refresh it.
// Re-resolving automatically on a schedule is future work, not built
// this milestone.
func networkACLCommands(instanceName string, policy provider.NetworkPolicy, resolvedAllowIPs map[string][]string) [][]string {
	acl := aclName(instanceName)
	var cmds [][]string

	cmds = append(cmds, []string{"network", "acl", "create", acl})
	cmds = append(cmds, []string{"network", "acl", "set", acl, "egress.action=reject"})

	if policy.DenyLAN {
		for _, cidr := range rfc1918AndLinkLocal {
			cmds = append(cmds, []string{"network", "acl", "rule", "add", acl, "egress",
				"action=reject", "destination=" + cidr})
		}
	}

	for _, rule := range policy.Allow {
		for _, ip := range resolvedAllowIPs[rule.Domain] {
			ruleArgs := []string{"network", "acl", "rule", "add", acl, "egress",
				"action=allow", "destination=" + ip, "protocol=tcp"}
			if len(rule.Ports) > 0 {
				ruleArgs = append(ruleArgs, "destination_port="+joinPorts(rule.Ports))
			}
			cmds = append(cmds, ruleArgs)
		}
	}

	cmds = append(cmds, []string{"config", "device", "override", instanceName, "eth0", "security.acls=" + acl})
	return cmds
}

func joinPorts(ports []int) string {
	out := ""
	for i, p := range ports {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("%d", p)
	}
	return out
}
