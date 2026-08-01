package incus

import (
	"reflect"
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
	m := provider.Mount{HostPath: "/host/ws", GuestPath: "/workspace", ReadOnly: true}
	got := buildMountDeviceArgs("demo", "mount0", m)
	want := []string{"config", "device", "add", "demo", "mount0", "disk",
		"source=/host/ws", "path=/workspace", "readonly=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildMountDeviceArgs() = %v, want %v", got, want)
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

	foundLANBlock := false
	foundAllow := false
	foundACLAttach := false
	for _, c := range cmds {
		joined := ""
		for _, a := range c {
			joined += a + " "
		}
		if contains(c, "destination=10.0.0.0/8") {
			foundLANBlock = true
		}
		if contains(c, "destination=93.184.216.34") && contains(c, "action=allow") {
			foundAllow = true
		}
		if contains(c, "security.acls=agentctl-demo") {
			foundACLAttach = true
		}
		_ = joined
	}
	if !foundLANBlock {
		t.Error("expected an RFC1918 block rule for 10.0.0.0/8")
	}
	if !foundAllow {
		t.Error("expected an allow rule for the resolved example.com IP")
	}
	if !foundACLAttach {
		t.Error("expected the ACL to be attached to eth0 via security.acls")
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

func TestExecUserArgs_NonRoot(t *testing.T) {
	got := execUserArgs(&execUser{uid: "1500", home: "/home/claude"})
	want := []string{"--user", "1500", "--group", "1500", "--cwd", "/home/claude", "--env", "HOME=/home/claude"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("execUserArgs() = %v, want %v", got, want)
	}
}

func TestBuildExecArgs_RootVsNonRoot(t *testing.T) {
	root := buildExecArgs("demo", []string{"echo", "hi"}, nil)
	wantRoot := []string{"exec", "demo", "--", "echo", "hi"}
	if !reflect.DeepEqual(root, wantRoot) {
		t.Errorf("buildExecArgs(nil) = %v, want %v", root, wantRoot)
	}

	nonRoot := buildExecArgs("demo", []string{"echo", "hi"}, &execUser{uid: "1500", home: "/home/claude"})
	wantNonRoot := []string{"exec", "demo", "--user", "1500", "--group", "1500", "--cwd", "/home/claude",
		"--env", "HOME=/home/claude", "--", "echo", "hi"}
	if !reflect.DeepEqual(nonRoot, wantNonRoot) {
		t.Errorf("buildExecArgs(execUser) = %v, want %v", nonRoot, wantNonRoot)
	}
}

func TestBuildShellArgs_RootVsNonRoot(t *testing.T) {
	root := buildShellArgs("demo", nil)
	wantRoot := []string{"exec", "demo", "--", "/bin/bash"}
	if !reflect.DeepEqual(root, wantRoot) {
		t.Errorf("buildShellArgs(nil) = %v, want %v", root, wantRoot)
	}

	nonRoot := buildShellArgs("demo", &execUser{uid: "1500", home: "/home/claude"})
	wantNonRoot := []string{"exec", "demo", "--user", "1500", "--group", "1500", "--cwd", "/home/claude",
		"--env", "HOME=/home/claude", "--", "/bin/bash"}
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
