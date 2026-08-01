package incus

import (
	"fmt"
	"strings"

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

// execUser carries the resolved non-root identity Exec/Shell should run
// as. nil means root — today's implicit default, and what --root asks
// for explicitly. Incus's exec API takes numeric --user/--group only
// (not a username) and does not derive $HOME/cwd from the UID — both
// default to /root regardless of --user — so both must be set explicitly
// whenever uid is non-root.
type execUser struct {
	uid  string
	home string
}

// execUserArgs returns the --user/--group/--cwd/--env flags for u, or
// nil for root (matching Incus's own default exec behavior exactly, so
// omitting these flags entirely is correct, not just "empty").
func execUserArgs(u *execUser) []string {
	if u == nil {
		return nil
	}
	return []string{"--user", u.uid, "--group", u.uid, "--cwd", u.home, "--env", "HOME=" + u.home}
}

func buildExecArgs(name string, command []string, u *execUser) []string {
	args := append([]string{"exec", name}, execUserArgs(u)...)
	args = append(args, "--")
	return append(args, command...)
}

func buildShellArgs(name string, u *execUser) []string {
	args := append([]string{"exec", name}, execUserArgs(u)...)
	return append(args, "--", "/bin/bash")
}

// bootstrapUserCommand is the command Create() execs (as root — bootstrap
// itself must run unprivileged-user-creation as root) to run
// bootstrapUserScript (embed.go), piped over stdin rather than templated
// into a single argument to avoid quoting hazards. username arrives as
// sh's positional $1 via `-s --`.
func bootstrapUserCommand(username string) []string {
	return []string{"sh", "-s", "--", username}
}

// userConfigKey is the single Incus custom config key (Incus's
// documented user.* namespace for arbitrary instance metadata) used to
// remember the provisioned non-root identity across Exec/Shell
// invocations, so Incus's own daemon stays the sole source of truth —
// agentctl keeps no separate instance registry of its own. One
// colon-delimited value (username:uid:home) rather than three keys, so
// looking it up costs a single `config get` round trip.
const userConfigKey = "user.agentctl-shell-user"

func buildSetUserConfigArgs(instanceName, username, uid, home string) []string {
	return []string{"config", "set", instanceName, userConfigKey + "=" + username + ":" + uid + ":" + home}
}

func buildGetUserConfigArgs(instanceName string) []string {
	return []string{"config", "get", instanceName, userConfigKey}
}

// agentConfigKey/agentInstalledConfigKey are the same user.* convention as
// userConfigKey, tracking `create --agent=<name>`'s JIT install: which
// agent (if any) was requested, and whether it finished installing. Using
// two keys (rather than packing into one, unlike userConfigKey) keeps
// "was anything requested" and "did it finish" independently readable —
// PendingAgentInstall needs both without parsing a combined value.
const (
	agentConfigKey          = "user.agentctl-agent"
	agentInstalledConfigKey = "user.agentctl-agent-installed"
)

func buildSetConfigArgs(instanceName, key, value string) []string {
	return []string{"config", "set", instanceName, key + "=" + value}
}

func buildGetConfigArgs(instanceName, key string) []string {
	return []string{"config", "get", instanceName, key}
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
//
// security.secureboot=false is set unconditionally: Incus VMs default to
// requiring UEFI Secure Boot, but most public/community images used with
// agentctl (Alpine, generic Ubuntu cloud images, ...) aren't Secure Boot
// signed, so the VM simply refuses to start with
// "The image used by this instance is incompatible with secureboot"
// otherwise (reported in practice on Incus 6.23 / Fedora 44). Secure Boot
// protects a guest's own boot chain against tampering with its boot
// media; it isn't part of the threat model agentctl defends against
// (host escape and LAN lateral movement from a legitimately booted
// guest), so disabling it doesn't weaken any of agentctl's actual
// guarantees. An image that *is* Secure Boot signed can have it
// re-enabled by hand afterward: `incus config set <name>
// security.secureboot=true`.
func buildInitArgs(spec provider.InstanceSpec) []string {
	args := []string{spec.Image, spec.Name, "--vm", "-c", "security.secureboot=false"}
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

	// Two distinct allow-domains (e.g. --agent=claude's "claude.ai" and
	// "*.anthropic.com") can resolve to the same IP behind shared CDN/
	// load-balancer infrastructure, which would otherwise produce two
	// byte-identical `acl rule add` invocations — Incus rejects the
	// second with "Duplicate of egress rule N" and the whole apply fails.
	// Track exact rule commands already emitted and skip re-adding one,
	// rather than deduping by IP alone (two rules for the same IP but
	// different ports are legitimately distinct, not duplicates).
	seenRules := make(map[string]bool)
	for _, rule := range policy.Allow {
		for _, ip := range resolvedAllowIPs[rule.Domain] {
			ruleArgs := []string{"network", "acl", "rule", "add", acl, "egress",
				"action=allow", "destination=" + ip, "protocol=tcp"}
			if len(rule.Ports) > 0 {
				ruleArgs = append(ruleArgs, "destination_port="+joinPorts(rule.Ports))
			}
			key := strings.Join(ruleArgs, " ")
			if seenRules[key] {
				continue
			}
			seenRules[key] = true
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
