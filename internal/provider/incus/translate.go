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

// buildStartArgs, buildStopArgs, buildDeleteArgs, buildExecArgs,
// buildShellArgs, buildViewArgs, and buildImagePullArgs mirror
// buildInitArgs below: each is the single source of truth for one
// operation's `incus` invocation, called by both the real Provider method
// (incus.go) and its CommandPreviewer counterpart (preview.go) so preview
// output can never drift from what actually executes.

func buildStartArgs(name string) []string {
	return []string{"start", name}
}

func buildStopArgs(name string, opts provider.StopOptions) []string {
	args := []string{"stop", name}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", int(opts.Timeout.Seconds())))
	}
	return args
}

func buildDeleteArgs(name string, force bool) []string {
	args := []string{"delete", name}
	if force {
		args = append(args, "--force")
	}
	return args
}

func buildExecArgs(name string, command []string) []string {
	return append([]string{"exec", name, "--"}, command...)
}

func buildShellArgs(name string) []string {
	return []string{"exec", name, "--", "/bin/bash"}
}

// buildViewArgs returns the args for Incus's native SPICE console —
// never the guest's X server, see docs/user/view-and-console.md.
func buildViewArgs(name string) []string {
	return []string{"console", name, "--type=vga"}
}

func buildImagePullArgs(ref string) []string {
	return []string{"image", "copy", ref, "local:"}
}

// buildACLDeleteArgs returns the best-effort cleanup command run before
// (re-)applying a network policy; its errors are ignored by the real
// ApplyNetworkPolicy since the ACL may not exist yet on a first apply.
func buildACLDeleteArgs(instanceName string) []string {
	return []string{"network", "acl", "delete", aclName(instanceName)}
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
// Default-deny for unmatched egress traffic needs no explicit command:
// Incus's own default for security.acls.default.egress.action is "reject"
// the moment one or more ACLs are attached to a NIC via security.acls
// (see the final "config device override" call below) — there is no
// ACL-level config key for this (an earlier version of this code tried
// `incus network acl set <acl> egress.action=reject`, which is invalid:
// "egress.action" isn't a real ACL config option, only a NIC/network
// setting, and an unnecessary one at that since reject is already the
// default).
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
