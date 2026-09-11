package profile

import (
	"strings"
	"testing"
)

const testdataDir = "../../testdata/profiles/"

func TestLoadFile_Valid(t *testing.T) {
	p, err := LoadFile(testdataDir + "valid.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if p.Metadata.Name != "valid" {
		t.Errorf("Name = %q, want %q", p.Metadata.Name, "valid")
	}
	if !p.Spec.Network.DenyLAN {
		t.Error("DenyLAN = false, want true")
	}
	if len(p.Spec.Network.Allow) != 1 || p.Spec.Network.Allow[0].Domain != "example.com" {
		t.Errorf("Allow = %+v, want one rule for example.com", p.Spec.Network.Allow)
	}
}

func TestLoadFile_RejectsUnknownField(t *testing.T) {
	_, err := LoadFile(testdataDir + "invalid-typo-field.yaml")
	if err == nil {
		t.Fatal("expected an error for a typo'd field, got nil")
	}
}

func TestLoadFile_RejectsOutOfRangePort(t *testing.T) {
	_, err := LoadFile(testdataDir + "invalid-port-range.yaml")
	if err == nil {
		t.Fatal("expected an error for an out-of-range port, got nil")
	}
	if !strings.Contains(err.Error(), "out-of-range") {
		t.Errorf("error %q does not mention out-of-range port", err.Error())
	}
}

func TestLoadFile_RejectsPortCollision(t *testing.T) {
	_, err := LoadFile(testdataDir + "invalid-port-collision.yaml")
	if err == nil {
		t.Fatal("expected an error for a duplicate host port, got nil")
	}
	if !strings.Contains(err.Error(), "published more than once") {
		t.Errorf("error %q does not mention the collision", err.Error())
	}
}

func TestLoadFile_RejectsBadResources(t *testing.T) {
	_, err := LoadFile(testdataDir + "invalid-resources.yaml")
	if err == nil {
		t.Fatal("expected errors for negative cpuCores and bad memory size, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cpuCores") {
		t.Errorf("error %q missing cpuCores complaint", msg)
	}
	if !strings.Contains(msg, "memory") {
		t.Errorf("error %q missing memory complaint", msg)
	}
}

func TestLoadNamed_FallsBackToBuiltin(t *testing.T) {
	p, err := LoadNamed("default", "")
	if err != nil {
		t.Fatalf("LoadNamed(default): %v", err)
	}
	if p.Metadata.Name != "default" {
		t.Errorf("Name = %q, want %q", p.Metadata.Name, "default")
	}

	if _, err := LoadNamed("strict", ""); err != nil {
		t.Fatalf("LoadNamed(strict): %v", err)
	}

	if _, err := LoadNamed("does-not-exist", ""); err == nil {
		t.Fatal("expected an error for an unknown profile name")
	}
}

// TestBuiltins_AllLoadAndValidate guards every embedded profile, not just
// the two the rest of these tests name: a malformed or unknown-field YAML
// would otherwise only surface the first time someone asked for it by
// name.
func TestBuiltins_AllLoadAndValidate(t *testing.T) {
	names := BuiltinNames()
	if len(names) == 0 {
		t.Fatal("BuiltinNames() is empty")
	}
	for _, name := range names {
		p, err := LoadNamed(name, "")
		if err != nil {
			t.Errorf("LoadNamed(%q): %v", name, err)
			continue
		}
		if p.Metadata.Name != name {
			t.Errorf("LoadNamed(%q).Metadata.Name = %q, want %q", name, p.Metadata.Name, name)
		}
	}
}

// TestEcosystemProfiles_ComposeSafely pins the two invariants that make
// an ecosystem profile layerable on top of a base one. Both are silent
// failures if broken: a mount would collide with the base profile's
// workspace and be rejected as overlapping, and an omitted denyLAN would
// resolve to false through Merge and quietly unblock the LAN.
func TestEcosystemProfiles_ComposeSafely(t *testing.T) {
	for _, name := range []string{"python", "node", "go", "rust"} {
		p, err := LoadNamed(name, "")
		if err != nil {
			t.Fatalf("LoadNamed(%q): %v", name, err)
		}
		if len(p.Spec.Mounts) != 0 {
			t.Errorf("%s declares mounts %+v; ecosystem profiles must declare none", name, p.Spec.Mounts)
		}
		if !p.Spec.Network.DenyLAN {
			t.Errorf("%s does not set denyLAN: true; layering it last would unblock the LAN", name)
		}
		if len(p.Spec.Network.Allow) == 0 {
			t.Errorf("%s has no allow entries, which is its whole purpose", name)
		}
	}
}

// TestEcosystemProfile_LayersOntoDefault is the composition the docs tell
// users to run: the base profile's resources and workspace survive, and
// the ecosystem profile's domains are added rather than replacing.
func TestEcosystemProfile_LayersOntoDefault(t *testing.T) {
	base, err := LoadNamed(Default, "")
	if err != nil {
		t.Fatalf("LoadNamed(default): %v", err)
	}
	eco, err := LoadNamed("python", "")
	if err != nil {
		t.Fatalf("LoadNamed(python): %v", err)
	}
	merged := Merge(base.Spec, eco.Spec)

	if merged.Resources.CPUCores != base.Spec.Resources.CPUCores {
		t.Errorf("cpuCores = %d, want the base profile's %d to survive", merged.Resources.CPUCores, base.Spec.Resources.CPUCores)
	}
	if len(merged.Mounts) != len(base.Spec.Mounts) {
		t.Errorf("mounts = %+v, want only the base profile's %+v", merged.Mounts, base.Spec.Mounts)
	}
	if !merged.Network.DenyLAN {
		t.Error("denyLAN = false after layering, want it to stay true")
	}
	var sawPyPI bool
	for _, rule := range merged.Network.Allow {
		if rule.Domain == "pypi.org" {
			sawPyPI = true
		}
	}
	if !sawPyPI {
		t.Errorf("allow = %+v, want the ecosystem profile's pypi.org entry", merged.Network.Allow)
	}
}
