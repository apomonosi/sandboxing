package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

// useIsolatedConfig points AGENTCTL_CONFIG at a fresh file for the
// duration of the test, so config left over from a real run never leaks
// in and successive execute() calls within one test still share the same
// (initially non-existent, then possibly written-to) config file.
func useIsolatedConfig(t *testing.T) {
	t.Helper()
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
}

func execute(t *testing.T, p *fake.Provider, args ...string) (string, error) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register("test", func() (provider.Provider, error) { return p, nil })

	root := NewRootCmd(reg)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--provider", "test"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestCLI_CreateStartStopDelete_HappyPath(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	if _, err := execute(t, p, "create", "demo", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if out, err := execute(t, p, "start", "demo"); err != nil {
		t.Fatalf("start: %v (%s)", err, out)
	}
	out, err := execute(t, p, "status", "demo")
	if err != nil {
		t.Fatalf("status: %v (%s)", err, out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("status output %q should mention running", out)
	}
	if _, err := execute(t, p, "stop", "demo"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := execute(t, p, "delete", "demo"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestCLI_List_JSON(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	if _, err := execute(t, p, "create", "demo", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := execute(t, p, "--output=json", "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, `"name": "demo"`) {
		t.Errorf("expected JSON list output to contain demo, got %s", out)
	}
}

func TestCLI_Create_MissingImage(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	_, err := execute(t, p, "create", "demo")
	if err == nil {
		t.Fatal("expected an error when neither --image nor --spec is given")
	}
}

func TestCLI_NoProviderConfigured(t *testing.T) {
	p := fake.New()
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	reg := provider.NewRegistry()
	reg.Register("test", func() (provider.Provider, error) { return p, nil })
	root := NewRootCmd(reg)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"create", "demo", "--image=ubuntu"}) // no --provider flag
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error when no provider is configured")
	}
}

func TestCLI_CapabilityGate_UnderDevelopment_ExitCode(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New().WithCapability(provider.Capability{
		Feature: provider.FeatureCreate,
		Status:  provider.UnderDevelopment,
		Message: "pretend this is Lima",
	})
	out, err := execute(t, p, "create", "demo", "--image=ubuntu")
	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %v (%T), output=%s", err, err, out)
	}
	if exitErr.Code != 2 {
		t.Errorf("want exit code 2, got %d", exitErr.Code)
	}
	if !strings.Contains(out, "pretend this is Lima") {
		t.Errorf("output %q missing capability message", out)
	}
}

func TestCLI_CapabilityGate_ManualWorkaround_ForcePartial(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New().WithCapability(provider.Capability{
		Feature: provider.FeatureNetworkACL,
		Status:  provider.ManualWorkaround,
		Message: "no native ACL",
		Plan:    "manual pf rule",
	})

	// Without --force-partial: blocked, exit code 3.
	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--allow=example.com")
	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %v, output=%s", err, out)
	}
	if exitErr.Code != 3 {
		t.Errorf("want exit code 3, got %d", exitErr.Code)
	}
	if !strings.Contains(out, "manual pf rule") {
		t.Errorf("output %q missing the plan text", out)
	}

	// With --force-partial: proceeds.
	out, err = execute(t, p, "create", "demo2", "--image=ubuntu", "--allow=example.com", "--force-partial")
	if err != nil {
		t.Fatalf("expected success with --force-partial, got %v (%s)", err, out)
	}
	if !strings.Contains(out, "Continuing without this protection") {
		t.Errorf("output %q missing the warning banner", out)
	}
}

func TestCLI_ProfileListAndShow(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	out, err := execute(t, p, "profile", "list")
	if err != nil {
		t.Fatalf("profile list: %v", err)
	}
	if !strings.Contains(out, "default") || !strings.Contains(out, "strict") {
		t.Errorf("expected built-in profiles in list output, got %q", out)
	}

	out, err = execute(t, p, "profile", "show", "default")
	if err != nil {
		t.Fatalf("profile show: %v", err)
	}
	if !strings.Contains(out, "denyLAN: true") {
		t.Errorf("expected default profile to show denyLAN: true, got %q", out)
	}
}

func TestCLI_ConfigSetGet(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	if _, err := execute(t, p, "config", "set", "provider=incus"); err != nil {
		t.Fatalf("config set: %v", err)
	}
	out, err := execute(t, p, "config", "get", "provider")
	if err != nil {
		t.Fatalf("config get: %v", err)
	}
	if strings.TrimSpace(out) != "incus" {
		t.Errorf("config get provider = %q, want incus", out)
	}
}
