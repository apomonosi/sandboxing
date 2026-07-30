package profile

import "testing"

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
