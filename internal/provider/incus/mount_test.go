package incus

import (
	"context"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// instanceWithDevicesJSON is an `incus list <name> --format json` response
// carrying a device map: one mount agentctl added on a previous boot, and
// one disk device a user attached by hand.
const instanceWithDevicesJSON = `[{
  "name": "demo",
  "status": "Stopped",
  "devices": {
    "` + "agentctl-mount-" + `deadbeef": {"type": "disk", "source": "/home/me/alpha", "path": "/workspace", "readonly": "true"},
    "my-own-disk": {"type": "disk", "source": "/home/me/keep", "path": "/keep"}
  }
}]`

// TestApplyMountPolicy_ReplacesOnlyAgentctlDevices is the property that
// makes `start --mount` safe to run against an existing instance: the
// previous agentctl mount is removed and the new set added, while a disk
// device the user attached themselves is left completely alone.
func TestApplyMountPolicy_ReplacesOnlyAgentctlDevices(t *testing.T) {
	fr := &fakeRunner{listJSON: instanceWithDevicesJSON}
	p := NewWithRunner(fr).(*Provider)

	err := p.ApplyMountPolicy(context.Background(), "demo", []provider.Mount{
		{HostPath: "/home/me/beta", GuestPath: "/workspace", Writable: true},
	})
	if err != nil {
		t.Fatalf("ApplyMountPolicy: %v", err)
	}

	var removed, added []string
	for _, call := range fr.calls {
		joined := strings.Join(call, " ")
		switch {
		case strings.Contains(joined, "device remove"):
			removed = append(removed, joined)
		case strings.Contains(joined, "device add"):
			added = append(added, joined)
		}
	}

	if len(removed) != 1 {
		t.Fatalf("removed %d devices, want exactly 1: %v", len(removed), removed)
	}
	if !strings.Contains(removed[0], mountDevicePrefix) {
		t.Errorf("removed %q, want a device carrying the %q prefix", removed[0], mountDevicePrefix)
	}
	if strings.Contains(removed[0], "my-own-disk") {
		t.Errorf("removed the user's own disk device: %q", removed[0])
	}

	if len(added) != 1 {
		t.Fatalf("added %d devices, want exactly 1: %v", len(added), added)
	}
	if !strings.Contains(added[0], "source=/home/me/beta") {
		t.Errorf("added %q, want the new host path", added[0])
	}
	if strings.Contains(added[0], "readonly=") {
		t.Errorf("added %q, want no readonly option for a writable mount", added[0])
	}
}

// A --mount-none start resolves to an empty mount set, which must clear
// the previous mounts rather than silently leaving them attached.
func TestApplyMountPolicy_EmptySetClearsMounts(t *testing.T) {
	fr := &fakeRunner{listJSON: instanceWithDevicesJSON}
	p := NewWithRunner(fr).(*Provider)

	if err := p.ApplyMountPolicy(context.Background(), "demo", nil); err != nil {
		t.Fatalf("ApplyMountPolicy: %v", err)
	}

	var sawRemove bool
	for _, call := range fr.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "device remove") {
			sawRemove = true
		}
		if strings.Contains(joined, "device add") {
			t.Errorf("added a device for an empty mount set: %q", joined)
		}
	}
	if !sawRemove {
		t.Error("ApplyMountPolicy(nil) removed nothing, want the previous mount cleared")
	}
}

// Status reports what the sandbox can currently see by reading Incus's own
// device map — mounts are sticky across boots, so the daemon is the only
// honest source. Devices agentctl doesn't manage aren't reported.
func TestStatus_ReportsAgentctlMountsOnly(t *testing.T) {
	fr := &fakeRunner{listJSON: instanceWithDevicesJSON}
	p := NewWithRunner(fr).(*Provider)

	inst, err := p.Status(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(inst.Mounts) != 1 {
		t.Fatalf("Status reported %d mounts, want 1: %+v", len(inst.Mounts), inst.Mounts)
	}
	got := inst.Mounts[0]
	if got.HostPath != "/home/me/alpha" || got.GuestPath != "/workspace" {
		t.Errorf("Status mount = %+v, want the agentctl-managed device", got)
	}
	if got.Writable {
		t.Error("Status reported a readonly=true device as writable")
	}
}

// TestCreate_UnrestrictedSkipsACL covers the --no-network-policy escape
// hatch: no ACL is created, and crucially none is attached, so the
// instance keeps the backend's own default connectivity. This exists
// because a sandbox whose policy is wrong is otherwise undebuggable from
// the inside — no DNS, no egress, nothing to read a log from.
func TestCreate_UnrestrictedSkipsACL(t *testing.T) {
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)

	if _, err := p.Create(context.Background(), provider.InstanceSpec{
		Name:      "demo",
		Image:     "images:ubuntu/24.04",
		Overrides: provider.NetworkPolicy{DenyLAN: true, Unrestricted: true},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, c := range fr.calls {
		for _, arg := range c {
			if strings.HasPrefix(arg, "security.acls=") {
				t.Errorf("--no-network-policy must not attach an ACL, got %v", c)
			}
		}
		if len(c) >= 3 && c[1] == "network" && c[2] == "acl" && contains(c, "create") {
			t.Errorf("--no-network-policy must not create an ACL, got %v", c)
		}
	}
}

// The default path must still attach one, so the escape hatch above can't
// silently become the norm.
func TestCreate_AppliesACLByDefault(t *testing.T) {
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)

	if _, err := p.Create(context.Background(), provider.InstanceSpec{
		Name:      "demo",
		Image:     "images:ubuntu/24.04",
		Overrides: provider.NetworkPolicy{DenyLAN: true},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var attached bool
	for _, c := range fr.calls {
		for _, arg := range c {
			if arg == "security.acls=agentctl-demo" {
				attached = true
			}
		}
	}
	if !attached {
		t.Error("a default create should attach the per-instance ACL")
	}
}
