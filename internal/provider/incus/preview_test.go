package incus

import (
	"reflect"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newTestProvider() *Provider {
	return NewWithRunner(&fakeRunner{}).(*Provider)
}

func TestPreviewCreate_NoNetworkAllowRules(t *testing.T) {
	// DenyLAN-only (no Allow entries) so this doesn't depend on real DNS
	// resolution — see TestPreviewCreate_IncludesNetworkPolicy below for
	// that.
	p := newTestProvider()
	spec := provider.InstanceSpec{
		Name:      "demo",
		Image:     "images:ubuntu/24.04",
		Resources: provider.ResourceLimits{CPUCores: 2, Memory: "4GiB", DiskSize: "20GiB"},
		Mounts:    []provider.Mount{{HostPath: "/host/ws", GuestPath: "/workspace"}},
		Overrides: provider.NetworkPolicy{
			DenyLAN: true,
			Ports:   []provider.PortPublish{{HostPort: 8080, GuestPort: 80, Protocol: "tcp"}},
		},
	}

	cmds := p.PreviewCreate(spec)
	if len(cmds) == 0 {
		t.Fatal("expected at least one command")
	}

	// First command is always the init.
	wantInit := append([]string{"init"}, buildInitArgs(spec)...)
	if !reflect.DeepEqual(cmds[0].Args, wantInit) {
		t.Errorf("cmds[0] = %v, want %v", cmds[0].Args, wantInit)
	}
	for _, c := range cmds {
		if c.Binary != binary {
			t.Errorf("command %v has Binary=%q, want %q", c.Args, c.Binary, binary)
		}
	}

	// Every command should also appear, verbatim, as something the real
	// dispatch path would run: a disk resize, a mount device add, a port
	// proxy device add, and the LAN-block ACL rules.
	joinedAll := ""
	for _, c := range cmds {
		joinedAll += c.String() + "\n"
	}
	for _, want := range []string{
		"config device override demo root size=20GiB",
		"config device add demo " + mountDeviceName("/workspace") + " disk source=/host/ws path=/workspace readonly=true",
		"config device add demo port0 proxy listen=tcp:0.0.0.0:8080 connect=tcp:127.0.0.1:80",
		"network acl rule add agentctl-demo egress action=allow protocol=udp destination_port=53",
		"config device override demo eth0 security.acls=agentctl-demo",
	} {
		if !strings.Contains(joinedAll, want) {
			t.Errorf("expected preview output to contain %q, got:\n%s", want, joinedAll)
		}
	}
}

func TestPreviewCreate_MatchesRealCreateCommandSequence(t *testing.T) {
	// The real Create() method issues init, then (optionally) disk
	// resize, mounts, ports, then ApplyNetworkPolicy, then start +
	// (invisible) waitForAgent + bootstrap + stop, then a trailing
	// Status() query. PreviewCreate should match every step except the
	// invisible agent-readiness probe (same category of gap as Exec/
	// Shell's own preview — a retry loop of unknown length, not a fixed
	// command) and the trailing query.
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)
	spec := provider.InstanceSpec{
		Name:  "demo",
		Image: "images:ubuntu/24.04",
		Overrides: provider.NetworkPolicy{
			DenyLAN: true,
		},
	}

	if _, err := p.Create(t.Context(), spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	preview := p.PreviewCreate(spec)
	var realCalls [][]string
	for _, c := range fr.calls {
		if isAgentProbe(c[1:]) {
			continue
		}
		// The config-set call recording the bootstrap script's real
		// resulting UID isn't shown in preview either (see PreviewCreate's
		// doc comment) — it only exists after actually running the
		// mutating script, so it can't be previewed without lying.
		if len(c) > 0 && strings.HasPrefix(c[len(c)-1], userConfigKey+"=") {
			continue
		}
		realCalls = append(realCalls, c)
	}
	if len(realCalls) < len(preview) {
		t.Fatalf("real Create issued %d commands (excluding the agent probe), fewer than preview's %d", len(realCalls), len(preview))
	}
	for i, want := range preview {
		got := realCalls[i]
		wantArgs := append([]string{binary}, want.Args...)
		if !reflect.DeepEqual(got, wantArgs) {
			t.Errorf("call %d = %v, want %v", i, got, wantArgs)
		}
	}
}

// TestPreviewCreate_UnrestrictedShowsNoACL guards the preview/dispatch
// symmetry for --no-network-policy. Create skips the ACL entirely when
// Unrestricted is set, so a preview that still listed the acl commands
// would be describing something that never runs.
func TestPreviewCreate_UnrestrictedShowsNoACL(t *testing.T) {
	p := newTestProvider()
	cmds := p.PreviewCreate(provider.InstanceSpec{
		Name:      "demo",
		Image:     "images:ubuntu/24.04",
		Overrides: provider.NetworkPolicy{DenyLAN: true, Unrestricted: true},
	})
	for _, c := range cmds {
		if strings.Contains(c.String(), "network acl") || strings.Contains(c.String(), "security.acls") {
			t.Errorf("preview must not show ACL commands under --no-network-policy, got %q", c.String())
		}
	}
}
