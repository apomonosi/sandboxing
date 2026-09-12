package incus

import (
	"reflect"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func TestBuildInitArgs(t *testing.T) {
	spec := provider.InstanceSpec{
		Name:     "demo",
		Image:    "images:ubuntu/24.04",
		Profiles: []string{"default", "extra"},
		Resources: provider.ResourceLimits{
			CPUCores: 2,
			Memory:   "4GiB",
		},
	}
	got := buildInitArgs(spec)
	want := []string{
		"images:ubuntu/24.04", "demo", "--vm",
		"-c", "security.secureboot=false",
		"-p", "default", "-p", "extra",
		"-c", "limits.cpu=2",
		"-c", "limits.memory=4GiB",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildInitArgs() = %v, want %v", got, want)
	}
}

// TestBuildInitArgs_SecureBootDisabledByDefault is a regression test for a
// real-world failure on Incus 6.23 (Fedora 44): "The image used by this
// instance is incompatible with secureboot." Most public/community images
// agentctl targets aren't Secure Boot signed, so it must be disabled by
// default for the VM to boot at all.
func TestBuildInitArgs_SecureBootDisabledByDefault(t *testing.T) {
	args := buildInitArgs(provider.InstanceSpec{Name: "demo", Image: "images:alpine/edge"})
	for _, a := range args {
		if a == "security.secureboot=false" {
			return
		}
	}
	t.Errorf("buildInitArgs() = %v, want it to include \"security.secureboot=false\"", args)
}

func TestBuildRootDiskResizeArgs(t *testing.T) {
	if got := buildRootDiskResizeArgs("demo", ""); got != nil {
		t.Errorf("expected nil for empty diskSize, got %v", got)
	}
	got := buildRootDiskResizeArgs("demo", "20GiB")
	want := []string{"config", "device", "override", "demo", "root", "size=20GiB"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildRootDiskResizeArgs() = %v, want %v", got, want)
	}
}

func TestBuildMountDeviceArgs(t *testing.T) {
	m := provider.Mount{HostPath: "/host/ws", GuestPath: "/workspace"}
	got := buildMountDeviceArgs("demo", m)
	want := []string{"config", "device", "add", "demo", mountDeviceName("/workspace"), "disk",
		"source=/host/ws", "path=/workspace", "readonly=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildMountDeviceArgs() = %v, want %v", got, want)
	}
}

// TestBuildMountDeviceArgs_Writable covers the one place agentctl's
// writable polarity is negated into Incus's readonly spelling: a writable
// mount must emit no readonly option at all, not readonly=false.
func TestBuildMountDeviceArgs_Writable(t *testing.T) {
	m := provider.Mount{HostPath: "/host/ws", GuestPath: "/workspace", Writable: true}
	got := buildMountDeviceArgs("demo", m)
	for _, arg := range got {
		if strings.HasPrefix(arg, "readonly=") {
			t.Errorf("buildMountDeviceArgs() = %v, want no readonly option for a writable mount", got)
		}
	}
}

// TestMountDeviceName_StableAndOwned pins the two properties
// ApplyMountPolicy depends on: the same guest path always yields the same
// device name (so re-applying an unchanged mount set is a no-op rather
// than a rename), and every generated name carries the ownership prefix
// that keeps a reconfigure from deleting a hand-added device.
func TestMountDeviceName_StableAndOwned(t *testing.T) {
	first := mountDeviceName("/workspace")
	if second := mountDeviceName("/workspace"); first != second {
		t.Errorf("mountDeviceName() = %q then %q, want a stable name", first, second)
	}
	if !strings.HasPrefix(first, mountDevicePrefix) {
		t.Errorf("mountDeviceName() = %q, want the %q ownership prefix", first, mountDevicePrefix)
	}
	if other := mountDeviceName("/other"); other == first {
		t.Errorf("mountDeviceName() collided: %q for both /workspace and /other", first)
	}
}

func TestBuildPortProxyDeviceArgs(t *testing.T) {
	pp := provider.PortPublish{HostPort: 8080, GuestPort: 80, Protocol: "tcp"}
	got := buildPortProxyDeviceArgs("demo", "port0", pp)
	want := []string{"config", "device", "add", "demo", "port0", "proxy",
		"listen=tcp:0.0.0.0:8080", "connect=tcp:127.0.0.1:80"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPortProxyDeviceArgs() = %v, want %v", got, want)
	}
}

func TestNetworkACLCommands_DenyLANAndAllow(t *testing.T) {
	policy := provider.NetworkPolicy{
		DenyLAN: true,
		Allow:   []provider.AllowRule{{Domain: "example.com", Ports: []int{443}}},
	}
	resolved := map[string][]string{"example.com": {"93.184.216.34"}}
	cmds := networkACLCommands("demo", policy, resolved)

	if len(cmds) == 0 {
		t.Fatal("expected at least one command")
	}
	if cmds[0][0] != "network" || cmds[0][3] != "agentctl-demo" {
		t.Errorf("first command should create the ACL, got %v", cmds[0])
	}

	foundAllow := false
	foundACLAttach := false
	for _, c := range cmds {
		if contains(c, "destination=93.184.216.34") && contains(c, "action=allow") {
			foundAllow = true
		}
		if contains(c, "security.acls=agentctl-demo") {
			foundACLAttach = true
		}
	}
	if !foundAllow {
		t.Error("expected an allow rule for the resolved example.com IP")
	}
	if !foundACLAttach {
		t.Error("expected the ACL to be attached to eth0 via security.acls")
	}
}

// TestNetworkACLCommands_NeverEmitsReject is the load-bearing invariant of
// this whole file, and the regression test for a real lockout on Incus
// 6.x / Fedora 44: every instance agentctl created had no working DNS.
//
// Incus re-sorts ACL rules by action — all rejects first, then allows,
// first match wins — so a reject rule shadows every allow regardless of
// the order they were added in. agentctl used to emit reject rules for
// the RFC1918 ranges under --deny-lan, which covered the bridge's own
// dnsmasq resolver and blackholed name resolution with no way for an
// allow rule to rescue it (upstream: lxc/incus#1919).
//
// Default-deny already rejects everything unmatched, so deny-lan is
// enforced by withholding allows instead. If a reject rule ever comes
// back, DNS breaks again.
func TestNetworkACLCommands_NeverEmitsReject(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy provider.NetworkPolicy
	}{
		{"deny-lan", provider.NetworkPolicy{DenyLAN: true}},
		{"allow-lan", provider.NetworkPolicy{DenyLAN: false}},
		{"deny-lan with allows", provider.NetworkPolicy{
			DenyLAN: true,
			Allow:   []provider.AllowRule{{Domain: "example.com", Ports: []int{443}}},
		}},
		{"dns disabled", provider.NetworkPolicy{DenyLAN: true, DNS: provider.DNSPolicy{Disabled: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved := map[string][]string{"example.com": {"93.184.216.34"}}
			for _, c := range networkACLCommands("demo", tc.policy, resolved) {
				for _, arg := range c {
					if arg == "action=reject" || arg == "action=drop" {
						t.Errorf("emitted a reject/drop rule, which shadows every allow: %v", c)
					}
				}
			}
		})
	}
}

// DNS has to be allowed or the allowlist cannot work at all: the guest
// must resolve a domain before it can reach the address the allowlist
// permits. Both protocols, because a response over 512 bytes falls back
// to TCP.
func TestNetworkACLCommands_AllowsDNSByDefault(t *testing.T) {
	cmds := networkACLCommands("demo", provider.NetworkPolicy{DenyLAN: true}, nil)
	var sawUDP, sawTCP bool
	for _, c := range cmds {
		if !contains(c, "destination_port=53") || !contains(c, "action=allow") {
			continue
		}
		if contains(c, "protocol=udp") {
			sawUDP = true
		}
		if contains(c, "protocol=tcp") {
			sawTCP = true
		}
	}
	if !sawUDP || !sawTCP {
		t.Errorf("want DNS allow rules for both udp and tcp, got udp=%v tcp=%v in %v", sawUDP, sawTCP, cmds)
	}
}

func TestNetworkACLCommands_DNSServersNarrowAndDisableRemoves(t *testing.T) {
	narrowed := networkACLCommands("demo", provider.NetworkPolicy{
		DenyLAN: true,
		DNS:     provider.DNSPolicy{Servers: []string{"10.0.0.1"}},
	}, nil)
	var sawNarrow, sawBroad bool
	for _, c := range narrowed {
		if !contains(c, "destination_port=53") {
			continue
		}
		if contains(c, "destination=10.0.0.1") {
			sawNarrow = true
		} else {
			sawBroad = true
		}
	}
	if !sawNarrow {
		t.Errorf("want a DNS rule scoped to the named server, got %v", narrowed)
	}
	if sawBroad {
		t.Errorf("naming a DNS server must not also leave the any-destination rule in place, got %v", narrowed)
	}

	disabled := networkACLCommands("demo", provider.NetworkPolicy{
		DenyLAN: true,
		DNS:     provider.DNSPolicy{Disabled: true},
	}, nil)
	for _, c := range disabled {
		if contains(c, "destination_port=53") {
			t.Errorf("dns.disabled must emit no DNS rule, got %v", c)
		}
	}
}

// --allow-lan has to grant the private ranges explicitly. Removing a
// reject rule would leave them just as unreachable, since default-deny
// catches everything unmatched — which is what --allow-lan used to do:
// nothing at all.
func TestNetworkACLCommands_AllowLANGrantsPrivateRangesBothFamilies(t *testing.T) {
	cmds := networkACLCommands("demo", provider.NetworkPolicy{DenyLAN: false}, nil)
	for _, want := range []string{"10.0.0.0/8", "192.168.0.0/16", "fc00::/7", "fe80::/10"} {
		found := false
		for _, c := range cmds {
			if contains(c, "destination="+want) && contains(c, "action=allow") {
				found = true
			}
		}
		if !found {
			t.Errorf("--allow-lan should emit an allow for %s, got %v", want, cmds)
		}
	}
}

// Incus cannot express "allow this domain, except on the LAN", so the
// conflict is resolved here: with deny-lan on, an allow-domain that
// resolves to a private address is dropped rather than emitted.
func TestNetworkACLCommands_DenyLANDropsPrivateResolvedIPs(t *testing.T) {
	policy := provider.NetworkPolicy{
		DenyLAN: true,
		Allow: []provider.AllowRule{
			{Domain: "internal.example", Ports: []int{443}},
			{Domain: "public.example", Ports: []int{443}},
		},
	}
	resolved := map[string][]string{
		"internal.example": {"192.168.1.5", "fd00::5"},
		"public.example":   {"93.184.216.34"},
	}
	cmds := networkACLCommands("demo", policy, resolved)
	for _, c := range cmds {
		if contains(c, "destination=192.168.1.5") || contains(c, "destination=fd00::5") {
			t.Errorf("deny-lan must drop an allow-domain that resolves to a LAN address, got %v", c)
		}
	}
	found := false
	for _, c := range cmds {
		if contains(c, "destination=93.184.216.34") {
			found = true
		}
	}
	if !found {
		t.Error("the public address should still be allowed")
	}
}

func TestIsPrivateAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"10.1.2.3":       true,
		"192.168.1.5":    true,
		"172.16.0.1":     true,
		"169.254.1.1":    true,
		"127.0.0.1":      true,
		"fd42::1":        true,
		"fe80::1":        true,
		"93.184.216.34":  false,
		"2606:4700::1":   false,
		"not-an-address": true, // fail closed
	} {
		if got := isPrivateAddr(addr); got != want {
			t.Errorf("isPrivateAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

// TestNetworkACLCommands_DedupesIdenticalRules is a regression test for a
// real-world failure reported against `create --agent=claude`: claude.ai
// and *.anthropic.com (both merged into the allowlist by --agent) resolved
// to the same IP behind shared CDN infrastructure, producing two
// byte-identical `acl rule add ... destination=<ip> ... destination_port=443`
// invocations. Incus rejects the second with "Duplicate of egress rule N",
// failing the whole apply. Two rules for the same IP but different ports
// must still both survive — only exact duplicates are dropped.
func TestNetworkACLCommands_DedupesIdenticalRules(t *testing.T) {
	policy := provider.NetworkPolicy{
		Allow: []provider.AllowRule{
			{Domain: "claude.ai", Ports: []int{443}},
			{Domain: "*.anthropic.com", Ports: []int{443}},
			{Domain: "extra.example.com", Ports: []int{8443}},
		},
	}
	resolved := map[string][]string{
		"claude.ai":         {"160.79.104.10"},
		"*.anthropic.com":   {"160.79.104.10"},
		"extra.example.com": {"160.79.104.10"},
	}
	cmds := networkACLCommands("demo", policy, resolved)

	var allowRuleCount int
	for _, c := range cmds {
		if contains(c, "destination=160.79.104.10") && contains(c, "action=allow") {
			allowRuleCount++
		}
	}
	if allowRuleCount != 2 {
		t.Errorf("expected 2 distinct allow rules for 160.79.104.10 (443 once, 8443 once), got %d in %v", allowRuleCount, cmds)
	}
}

// TestNetworkACLCommands_NoInvalidEgressActionKey is a regression test for
// a real-world failure on Incus 6.23 (Fedora 44): an earlier version of
// networkACLCommands issued `incus network acl set <acl> egress.action=reject`,
// which fails with "Invalid config option \"egress.action\"" — there's no
// such ACL-level config key. Default-deny for unmatched egress traffic
// needs no explicit command at all: Incus already defaults
// security.acls.default.egress.action to "reject" once an ACL is attached
// to a NIC.
func TestNetworkACLCommands_NoInvalidEgressActionKey(t *testing.T) {
	policy := provider.NetworkPolicy{DenyLAN: true}
	cmds := networkACLCommands("demo", policy, nil)
	for _, c := range cmds {
		if len(c) >= 4 && c[0] == "network" && c[1] == "acl" && c[2] == "set" {
			t.Errorf("networkACLCommands must not emit an ACL-level `network acl set` command (no such config key exists), got: %v", c)
		}
		for _, arg := range c {
			if arg == "egress.action=reject" {
				t.Errorf("networkACLCommands must not emit the invalid egress.action ACL config key, got: %v", c)
			}
		}
	}
}

func TestExecUserArgs_NilIsRoot(t *testing.T) {
	if args := execUserArgs(nil); args != nil {
		t.Errorf("execUserArgs(nil) = %v, want nil (root, matching Incus's own default)", args)
	}
}

// testUser is the identity resolveExecUser would produce from the config
// value "claude:1500:/home/claude".
func testUser() *execUser {
	return &execUser{name: "claude", uid: "1500", home: "/home/claude"}
}

// wantUserArgs is the full env block a non-root exec must carry. Spelled
// out once here rather than rebuilt from loginPath(), so a change to
// either is a visible test diff instead of a tautology.
var wantUserArgs = []string{
	"--user", "1500", "--group", "1500", "--cwd", "/home/claude",
	"--env", "HOME=/home/claude",
	"--env", "USER=claude",
	"--env", "LOGNAME=claude",
	"--env", "PATH=/home/claude/.local/bin:/home/claude/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
}

func TestExecUserArgs_NonRoot(t *testing.T) {
	got := execUserArgs(testUser())
	if !reflect.DeepEqual(got, wantUserArgs) {
		t.Errorf("execUserArgs() = %v, want %v", got, wantUserArgs)
	}
}

// TestExecUserArgs_PathCarriesUserLocalBin pins the specific regression
// this PATH exists for: `incus exec` is not a login shell, so nothing
// sources the profile snippet that puts ~/.local/bin on PATH — which is
// where claude.ai/install.sh and most other agent installers put their
// binary. Without this, the install succeeds and the binary is still
// "command not found" on the very next exec.
func TestExecUserArgs_PathCarriesUserLocalBin(t *testing.T) {
	var path string
	args := execUserArgs(testUser())
	for i, a := range args {
		if a == "--env" && i+1 < len(args) && strings.HasPrefix(args[i+1], "PATH=") {
			path = strings.TrimPrefix(args[i+1], "PATH=")
		}
	}
	if path == "" {
		t.Fatal("execUserArgs emitted no PATH; agent binaries under ~/.local/bin will not be found")
	}
	first, _, _ := strings.Cut(path, ":")
	if first != "/home/claude/.local/bin" {
		t.Errorf("PATH starts with %q, want the user's ~/.local/bin first so it shadows a system copy", first)
	}
}

func TestBuildExecArgs_RootVsNonRoot(t *testing.T) {
	root := buildExecArgs("demo", []string{"echo", "hi"}, nil)
	wantRoot := []string{"exec", "demo", "--", "echo", "hi"}
	if !reflect.DeepEqual(root, wantRoot) {
		t.Errorf("buildExecArgs(nil) = %v, want %v", root, wantRoot)
	}

	nonRoot := buildExecArgs("demo", []string{"echo", "hi"}, testUser())
	wantNonRoot := append(append([]string{"exec", "demo"}, wantUserArgs...), "--", "echo", "hi")
	if !reflect.DeepEqual(nonRoot, wantNonRoot) {
		t.Errorf("buildExecArgs(execUser) = %v, want %v", nonRoot, wantNonRoot)
	}
}

// Both shells must be login shells (-l): a bare `incus exec -- /bin/bash`
// is interactive but not a login, so /etc/profile, /etc/profile.d/* and
// ~/.bash_profile are never sourced and the shell comes up without the
// distro's PATH, prompt or locale.
func TestBuildShellArgs_RootVsNonRoot(t *testing.T) {
	root := buildShellArgs("demo", nil)
	wantRoot := []string{"exec", "demo", "--", "/bin/bash", "-l"}
	if !reflect.DeepEqual(root, wantRoot) {
		t.Errorf("buildShellArgs(nil) = %v, want %v", root, wantRoot)
	}

	nonRoot := buildShellArgs("demo", testUser())
	wantNonRoot := append(append([]string{"exec", "demo"}, wantUserArgs...), "--", "/bin/bash", "-l")
	if !reflect.DeepEqual(nonRoot, wantNonRoot) {
		t.Errorf("buildShellArgs(execUser) = %v, want %v", nonRoot, wantNonRoot)
	}
}

func TestBootstrapUserCommand(t *testing.T) {
	got := bootstrapUserCommand("claude")
	want := []string{"sh", "-s", "--", "claude"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bootstrapUserCommand(\"claude\") = %v, want %v", got, want)
	}
}

func TestUserConfigArgs(t *testing.T) {
	setArgs := buildSetUserConfigArgs("demo", "claude", "1500", "/home/claude")
	wantSet := []string{"config", "set", "demo", "user.agentctl-shell-user=claude:1500:/home/claude"}
	if !reflect.DeepEqual(setArgs, wantSet) {
		t.Errorf("buildSetUserConfigArgs() = %v, want %v", setArgs, wantSet)
	}

	getArgs := buildGetUserConfigArgs("demo")
	wantGet := []string{"config", "get", "demo", "user.agentctl-shell-user"}
	if !reflect.DeepEqual(getArgs, wantGet) {
		t.Errorf("buildGetUserConfigArgs() = %v, want %v", getArgs, wantGet)
	}
}

func TestAgentConfigArgs(t *testing.T) {
	setArgs := buildSetConfigArgs("demo", agentConfigKey, "claude")
	wantSet := []string{"config", "set", "demo", "user.agentctl-agent=claude"}
	if !reflect.DeepEqual(setArgs, wantSet) {
		t.Errorf("buildSetConfigArgs(agentConfigKey) = %v, want %v", setArgs, wantSet)
	}

	getArgs := buildGetConfigArgs("demo", agentConfigKey)
	wantGet := []string{"config", "get", "demo", "user.agentctl-agent"}
	if !reflect.DeepEqual(getArgs, wantGet) {
		t.Errorf("buildGetConfigArgs(agentConfigKey) = %v, want %v", getArgs, wantGet)
	}

	installedArgs := buildSetConfigArgs("demo", agentInstalledConfigKey, "true")
	wantInstalled := []string{"config", "set", "demo", "user.agentctl-agent-installed=true"}
	if !reflect.DeepEqual(installedArgs, wantInstalled) {
		t.Errorf("buildSetConfigArgs(agentInstalledConfigKey) = %v, want %v", installedArgs, wantInstalled)
	}
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
