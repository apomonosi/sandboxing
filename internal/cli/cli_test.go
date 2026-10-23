package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	return executeProvider(t, p, args...)
}

// executeProvider is execute for a Provider that isn't a *fake.Provider —
// a wrapper implementing an optional interface (CommandPreviewer,
// Preflighter) on top of one, say.
func executeProvider(t *testing.T, p provider.Provider, args ...string) (string, error) {
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

func TestCLI_Start_ReportsConfinement(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	if _, err := execute(t, p, "create", "demo", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := execute(t, p, "start", "demo")
	if err != nil {
		t.Fatalf("start: %v (%s)", err, out)
	}
	const verified = "agentctl: network confinement verified: the guest could not reach the internet except through its egress proxy.\n"
	if n := strings.Count(out, verified); n != 1 {
		t.Errorf("start output %q has the verified line %d times, want once", out, n)
	}
	if !strings.Contains(out, "agentctl: no host directories are exposed to this sandbox.\n"+verified) {
		t.Errorf("start output %q: want the confinement line right after the mounts line", out)
	}

	out, err = execute(t, p, "status", "demo")
	if err != nil || !strings.Contains(out, "Confinement:") || !strings.Contains(out, "verified") {
		t.Errorf("status output %q (err %v), want a Confinement: verified line", out, err)
	}
	out, err = execute(t, p, "--output=json", "status", "demo")
	if err != nil || !strings.Contains(out, `"confinement": "verified"`) {
		t.Errorf("status JSON %q (err %v), want \"confinement\": \"verified\"", out, err)
	}
}

func TestCLI_Start_JSONAndPreview_PrintNoConfinementLine(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	if _, err := execute(t, p, "create", "demo", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := executeProvider(t, previewingFake{p}, "--preview", "start", "demo")
	if err != nil {
		t.Fatalf("start --preview: %v (%s)", err, out)
	}
	if strings.Contains(out, "network confinement") {
		t.Errorf("start --preview output %q mentions confinement, want only the previewed commands", out)
	}
	if inst, _ := p.Status(context.Background(), "demo"); inst.Status == provider.StatusRunning {
		t.Error("start --preview started the instance")
	}
	out, err = execute(t, p, "--output=json", "start", "demo")
	if err != nil {
		t.Fatalf("start --output=json: %v (%s)", err, out)
	}
	if strings.Contains(out, "network confinement") {
		t.Errorf("start --output=json output %q has the confinement line, want nothing extra", out)
	}
}

func TestCLI_CreateWithAgent_ReportsConfinement(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--agent=pi")
	if err != nil {
		t.Fatalf("create --agent=pi: %v (%s)", err, out)
	}
	const verified = "agentctl: network confinement verified: the guest could not reach the internet except through its egress proxy.\n"
	if n := strings.Count(out, verified); n != 1 {
		t.Errorf("create --agent output %q has the verified line %d times, want once", out, n)
	}
	if !strings.Contains(out, "agentctl: no host directories are exposed to this sandbox.\n"+verified) {
		t.Errorf("create --agent output %q: want the confinement line right after the mounts line", out)
	}
}

func TestCLI_Create_NoConfinementLineUnlessStarted(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	out, err := execute(t, p, "create", "demo", "--image=ubuntu")
	if err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
	if strings.Contains(out, "network confinement") {
		t.Errorf("create output %q has a confinement line, want none without --agent", out)
	}

	out, err = execute(t, p, "--output=json", "create", "demo2", "--image=ubuntu", "--agent=pi")
	if err != nil {
		t.Fatalf("create --output=json --agent=pi: %v (%s)", err, out)
	}
	if strings.Contains(out, "network confinement") {
		t.Errorf("create --output=json output %q has the confinement line, want nothing extra", out)
	}

	out, err = executeProvider(t, previewingFake{p}, "--preview", "create", "demo3", "--image=ubuntu", "--agent=pi")
	if err != nil {
		t.Fatalf("create --preview: %v (%s)", err, out)
	}
	if strings.Contains(out, "network confinement") {
		t.Errorf("create --preview output %q mentions confinement, want only the previewed commands", out)
	}
	if _, err := p.Status(context.Background(), "demo3"); err == nil {
		t.Error("create --preview created the instance")
	}
}

func TestCLI_Status_NoConfinementLabelWhenEmpty(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	if _, err := execute(t, p, "create", "demo", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := execute(t, p, "status", "demo")
	if err != nil || strings.Contains(out, "Confinement") {
		t.Errorf("status output %q (err %v), want no Confinement label for a stopped instance", out, err)
	}
}

// status lists the mounts the backend reports, one row each, worded as
// start prints them. Under --output json nothing new prints: they are in
// the instance's existing "mounts" field.
func TestCLI_Status_ShowsMounts(t *testing.T) {
	useIsolatedConfig(t)
	home := useIsolatedHome(t)
	pi01 := mustMkdir(t, filepath.Join(home, "pi01"))
	ref := mustMkdir(t, filepath.Join(home, "ref"))
	p := fake.New()
	if _, err := execute(t, p, "create", "pibox", "--image=ubuntu",
		"--mount", pi01+":/home/pi.guest/work01:w", "--mount", ref+":/home/pi.guest/ref"); err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := execute(t, p, "status", "pibox")
	if err != nil {
		t.Fatalf("status: %v (%s)", err, out)
	}
	var rows []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, " -> ") {
			rows = append(rows, line)
		}
	}
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "Mounts:") || strings.Contains(rows[1], "Mounts:") {
		t.Fatalf("status output %q: want two mount rows, the first labelled Mounts:", out)
	}
	for _, want := range []string{
		pi01 + " -> /home/pi.guest/work01 (writable)",
		ref + " -> /home/pi.guest/ref (read-only)",
	} {
		if !strings.HasSuffix(rows[0], " "+want) && !strings.HasSuffix(rows[1], " "+want) {
			t.Errorf("status output %q should have a row ending %q", out, want)
		}
	}

	out, err = execute(t, p, "--output=json", "status", "pibox")
	if err != nil {
		t.Fatalf("status --output=json: %v (%s)", err, out)
	}
	var inst provider.Instance
	if err := json.Unmarshal([]byte(out), &inst); err != nil {
		t.Fatalf("status --output=json is not one JSON document: %v\n%s", err, out)
	}
	if len(inst.Mounts) != 2 {
		t.Errorf("status JSON mounts = %+v, want both", inst.Mounts)
	}
}

func TestCLI_Status_NoMountsLabelWhenEmpty(t *testing.T) {
	useIsolatedConfig(t)
	useIsolatedHome(t)
	p := fake.New()
	if _, err := execute(t, p, "create", "demo", "--image=ubuntu", "--mount-none"); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := execute(t, p, "status", "demo")
	if err != nil || strings.Contains(out, "Mounts") || strings.Contains(out, "->") {
		t.Errorf("status output %q (err %v), want no Mounts label for an instance with none", out, err)
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
