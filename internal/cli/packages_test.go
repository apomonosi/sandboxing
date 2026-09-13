package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

func TestCLI_Create_PackageFlag_ReachesTheProvider(t *testing.T) {
	useIsolatedConfig(t)
	p := &capturingProvider{Provider: fake.New()}

	if _, err := executeWith(t, p, "create", "demo", "--image=ubuntu",
		"--package=git", "--package=ripgrep"); err != nil {
		t.Fatalf("create --package: %v", err)
	}

	got := p.lastCreateSpec.Packages
	for _, want := range []string{"git", "ripgrep"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("InstanceSpec.Packages = %v, missing %q", got, want)
		}
	}
}

// A name that isn't shaped like a package name is rejected before
// anything is created — the alternative is provisioning a VM, booting it,
// and only then failing on a typo.
func TestCLI_Create_RejectsMalformedPackageName(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--package=git; rm -rf /")
	if err == nil {
		t.Fatalf("expected an error for a malformed --package value (output: %s)", out)
	}
	if !strings.Contains(err.Error(), "--package") {
		t.Errorf("error %q should name the flag at fault", err)
	}
	if _, statusErr := p.Status(context.Background(), "demo"); statusErr == nil {
		t.Error("instance should not have been created when --package validation fails")
	}
}

// Packages are their own capability, so a backend that can't provision
// software says so instead of silently creating a sandbox without the
// tools that were asked for.
func TestCLI_Create_PackagesAreCapabilityGated(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	p = p.WithCapability(provider.Capability{
		Feature: provider.FeaturePackages,
		Status:  provider.UnderDevelopment,
		Message: "not wired up on this backend yet",
	})

	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--package=git")
	if err == nil {
		t.Fatalf("expected the capability gate to block the create (output: %s)", out)
	}
	if !strings.Contains(out+err.Error(), "provision.packages") {
		t.Errorf("gate output %q / error %q should name the feature", out, err)
	}
}

// A create with no packages must not trip the gate at all, so an
// unsupported backend stays fully usable for everything else.
func TestCLI_Create_NoPackages_SkipsTheGate(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	p = p.WithCapability(provider.Capability{
		Feature: provider.FeaturePackages,
		Status:  provider.NotAvailable,
		Message: "no way to provision software on this backend",
	})

	if out, err := execute(t, p, "create", "demo", "--image=ubuntu"); err != nil {
		t.Fatalf("plain create should be unaffected by the packages capability: %v (%s)", err, out)
	}
}

func TestCLI_ProfilePackages_ListsTheCatalog(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	out, err := execute(t, p, "profile", "packages")
	if err != nil {
		t.Fatalf("profile packages: %v (%s)", err, out)
	}
	// The per-manager columns are the point: they are what makes the
	// portability claim checkable rather than asserted.
	for _, want := range []string{"TOOL", "APT", "DNF", "APK", "openssh-clients", "ShellCheck"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q, got:\n%s", want, out)
		}
	}
}
