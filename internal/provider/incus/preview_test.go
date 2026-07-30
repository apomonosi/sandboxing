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
		"config device add demo mount0 disk source=/host/ws path=/workspace",
		"config device add demo port0 proxy listen=tcp:0.0.0.0:8080 connect=tcp:127.0.0.1:80",
		"network acl rule add agentctl-demo egress action=reject destination=10.0.0.0/8",
		"config device override demo eth0 security.acls=agentctl-demo",
	} {
		if !strings.Contains(joinedAll, want) {
			t.Errorf("expected preview output to contain %q, got:\n%s", want, joinedAll)
		}
	}
}

func TestPreviewCreate_MatchesRealCreateCommandSequence(t *testing.T) {
	// The real Create() method issues init, then (optionally) disk
	// resize, mounts, ports, then ApplyNetworkPolicy, then a trailing
	// Status() query. PreviewCreate should match every step except that
	// trailing query.
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
	if len(fr.calls) < len(preview) {
		t.Fatalf("real Create issued %d commands, fewer than preview's %d", len(fr.calls), len(preview))
	}
	for i, want := range preview {
		got := fr.calls[i]
		wantArgs := append([]string{binary}, want.Args...)
		if !reflect.DeepEqual(got, wantArgs) {
			t.Errorf("call %d = %v, want %v", i, got, wantArgs)
		}
	}
}
