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
		"-p", "default", "-p", "extra",
		"-c", "limits.cpu=2",
		"-c", "limits.memory=4GiB",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildInitArgs() = %v, want %v", got, want)
	}
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

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
