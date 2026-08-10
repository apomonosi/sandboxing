package profile

import (
	"reflect"
	"testing"
)

func TestMerge_AdditiveListsAndOverridingScalars(t *testing.T) {
	base := Policy{
		Network: NetworkPolicy{
			DenyLAN: true,
			Allow:   []AllowRule{{Domain: "pypi.org", Ports: []int{443}}},
		},
		Resources: Resources{CPUCores: 2, Memory: "4GiB"},
		Mounts:    []Mount{{HostPath: "/base", GuestPath: "/workspace"}},
		Console:   Console{Viewer: "spice"},
	}
	override := Policy{
		Network: NetworkPolicy{
			DenyLAN: false, // simulates --allow-lan
			Allow:   []AllowRule{{Domain: "example.com"}},
			Ports:   []PortPublish{{Host: 8080, Guest: 80, Protocol: "tcp"}},
		},
		Resources: Resources{CPUCores: 4}, // Memory left zero-value: base's should survive
	}

	merged := Merge(base, override)

	if merged.Network.DenyLAN {
		t.Error("DenyLAN: override should win (false), got true")
	}
	if len(merged.Network.Allow) != 2 {
		t.Fatalf("Allow: want 2 entries (additive union), got %d: %+v", len(merged.Network.Allow), merged.Network.Allow)
	}
	if merged.Network.Allow[0].Domain != "pypi.org" || merged.Network.Allow[1].Domain != "example.com" {
		t.Errorf("Allow order/contents wrong: %+v", merged.Network.Allow)
	}
	if len(merged.Network.Ports) != 1 || merged.Network.Ports[0].Host != 8080 {
		t.Errorf("Ports: want override's port publish, got %+v", merged.Network.Ports)
	}
	if merged.Resources.CPUCores != 4 {
		t.Errorf("CPUCores: want override's 4, got %d", merged.Resources.CPUCores)
	}
	if merged.Resources.Memory != "4GiB" {
		t.Errorf("Memory: want base's 4GiB to survive (override left it zero-value), got %q", merged.Resources.Memory)
	}
	if len(merged.Mounts) != 1 {
		t.Errorf("Mounts: want base's mount to survive, got %+v", merged.Mounts)
	}
	if merged.Console.Viewer != "spice" {
		t.Errorf("Console.Viewer: want base's spice to survive, got %q", merged.Console.Viewer)
	}
}

// MountPolicy is the one part of a Policy that must only ever narrow when
// layered. Everything else in Merge lets a later layer win outright; here
// that would let a personal profile unlock a directory an org profile
// ruled out.
func TestMergeMountPolicy_NarrowsOnly(t *testing.T) {
	t.Run("allowedRoots intersect", func(t *testing.T) {
		got := mergeMountPolicy(
			MountPolicy{AllowedRoots: []string{"~/src", "~/work"}},
			MountPolicy{AllowedRoots: []string{"~/work", "~/elsewhere"}},
		)
		if want := []string{"~/work"}; !reflect.DeepEqual(got.AllowedRoots, want) {
			t.Errorf("AllowedRoots = %v, want %v (intersection, not union)", got.AllowedRoots, want)
		}
	})

	t.Run("an unconstrained override keeps the base's roots", func(t *testing.T) {
		got := mergeMountPolicy(MountPolicy{AllowedRoots: []string{"~/src"}}, MountPolicy{})
		if want := []string{"~/src"}; !reflect.DeepEqual(got.AllowedRoots, want) {
			t.Errorf("AllowedRoots = %v, want %v: an empty override must not clear the constraint", got.AllowedRoots, want)
		}
	})

	t.Run("denyPaths union and dedupe", func(t *testing.T) {
		got := mergeMountPolicy(
			MountPolicy{DenyPaths: []string{"~/.ssh", "~/secrets"}},
			MountPolicy{DenyPaths: []string{"~/secrets", "~/keys"}},
		)
		if want := []string{"~/.ssh", "~/secrets", "~/keys"}; !reflect.DeepEqual(got.DenyPaths, want) {
			t.Errorf("DenyPaths = %v, want %v", got.DenyPaths, want)
		}
	})
}

func TestMergeMountPolicy_AllowHome(t *testing.T) {
	tests := []struct {
		name           string
		base, override *bool
		want           bool
	}{
		{"unset stays denied", nil, nil, false},
		{"explicit true survives an unset override", allow(true), nil, true},
		{"an explicit false beats a later true", allow(false), allow(true), false},
		{"an explicit false wins from the override side too", allow(true), allow(false), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeMountPolicy(MountPolicy{AllowHome: tc.base}, MountPolicy{AllowHome: tc.override})
			if got.HomeAllowed() != tc.want {
				t.Errorf("HomeAllowed() = %v, want %v", got.HomeAllowed(), tc.want)
			}
		})
	}
}

// Every create layers a zero-valued override (the ad-hoc flags) on top of
// the resolved profile. With a plain bool that layer would silently reset
// an org profile's explicit allowHome, which is why the field is a
// pointer.
func TestMerge_ZeroOverrideDoesNotResetMountPolicy(t *testing.T) {
	base := Policy{MountPolicy: MountPolicy{
		AllowedRoots:  []string{"~/src"},
		AllowHome:     allow(true),
		CreateMissing: true,
	}}
	merged := Merge(base, Policy{})

	if !reflect.DeepEqual(merged.MountPolicy.AllowedRoots, []string{"~/src"}) {
		t.Errorf("AllowedRoots = %v, want the base's to survive", merged.MountPolicy.AllowedRoots)
	}
	if !merged.MountPolicy.HomeAllowed() {
		t.Error("HomeAllowed() = false, want the base's explicit true to survive an empty override")
	}
	if !merged.MountPolicy.CreateMissing {
		t.Error("CreateMissing = false, want the base's true to survive")
	}
}
