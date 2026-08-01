package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

// previewingFake wraps fake.Provider and implements provider.CommandPreviewer,
// so CLI-level preview behavior can be tested without depending on the real
// Incus backend.
type previewingFake struct {
	*fake.Provider
}

func (previewingFake) PreviewCreate(spec provider.InstanceSpec) []provider.Command {
	return []provider.Command{{Binary: "fakectl", Args: []string{"init", spec.Image, spec.Name}}}
}
func (previewingFake) PreviewStart(name string) []provider.Command {
	return []provider.Command{{Binary: "fakectl", Args: []string{"start", name}}}
}
func (previewingFake) PreviewStop(name string, opts provider.StopOptions) []provider.Command {
	return []provider.Command{{Binary: "fakectl", Args: []string{"stop", name}}}
}
func (previewingFake) PreviewDelete(name string, force bool) []provider.Command {
	return []provider.Command{{Binary: "fakectl", Args: []string{"delete", name}}}
}
func (previewingFake) PreviewExec(ctx context.Context, name string, opts provider.ExecOptions) ([]provider.Command, error) {
	args := append([]string{"exec", name}, rootFlagArg(opts.Root)...)
	return []provider.Command{{Binary: "fakectl", Args: append(append(args, "--"), opts.Command...)}}, nil
}
func (previewingFake) PreviewShell(ctx context.Context, name string, opts provider.ShellOptions) ([]provider.Command, error) {
	args := append([]string{"shell", name}, rootFlagArg(opts.Root)...)
	return []provider.Command{{Binary: "fakectl", Args: args}}, nil
}

// rootFlagArg lets tests observe whether --root actually threaded
// through to ExecOptions.Root/ShellOptions.Root.
func rootFlagArg(root bool) []string {
	if root {
		return []string{"--as-root"}
	}
	return nil
}
func (previewingFake) PreviewView(name string, opts provider.ViewOptions) []provider.Command {
	return []provider.Command{{Binary: "fakectl", Args: []string{"view", name}}}
}
func (previewingFake) PreviewImagePull(ref string) []provider.Command {
	return []provider.Command{{Binary: "fakectl", Args: []string{"pull", ref}}}
}

func executeWith(t *testing.T, p provider.Provider, args ...string) (string, error) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register("test", func() (provider.Provider, error) { return p, nil })
	root := NewRootCmd(reg)
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--provider", "test"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestCLI_Preview_PrintsCommandsWithoutSideEffects(t *testing.T) {
	useIsolatedConfig(t)
	p := previewingFake{fake.New()}

	out, err := executeWith(t, p, "--preview", "create", "demo", "--image=ubuntu")
	if err != nil {
		t.Fatalf("create --preview: %v (%s)", err, out)
	}
	if strings.TrimSpace(out) != "fakectl init ubuntu demo" {
		t.Errorf("output = %q, want the previewed command only", out)
	}

	// No side effects: the instance was never actually created, so a real
	// start should fail with "not found".
	if _, err := executeWith(t, p, "start", "demo"); err == nil {
		t.Fatal("expected start to fail: --preview must not have actually created the instance")
	}
}

func TestCLI_Preview_DryRunAliasWorks(t *testing.T) {
	useIsolatedConfig(t)
	p := previewingFake{fake.New()}

	out, err := executeWith(t, p, "--dry-run", "create", "demo", "--image=ubuntu")
	if err != nil {
		t.Fatalf("create --dry-run: %v (%s)", err, out)
	}
	if !strings.Contains(out, "fakectl init ubuntu demo") {
		t.Errorf("output %q missing the previewed command", out)
	}
}

func TestCLI_Preview_AllWiredCommands(t *testing.T) {
	useIsolatedConfig(t)
	p := previewingFake{fake.New()}

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"start", "demo"}, "fakectl start demo"},
		{[]string{"stop", "demo"}, "fakectl stop demo"},
		{[]string{"delete", "demo"}, "fakectl delete demo"},
		{[]string{"shell", "demo"}, "fakectl shell demo"},
		{[]string{"view", "demo"}, "fakectl view demo"},
		{[]string{"image", "pull", "ubuntu"}, "fakectl pull ubuntu"},
		{[]string{"exec", "demo", "--", "echo", "hi"}, "fakectl exec demo -- echo hi"},
	}
	for _, c := range cases {
		out, err := executeWith(t, p, append([]string{"--preview"}, c.args...)...)
		if err != nil {
			t.Errorf("%v --preview: %v (%s)", c.args, err, out)
			continue
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("%v --preview: output %q missing %q", c.args, out, c.want)
		}
	}
}

func TestCLI_Root_ThreadsThroughToPreview(t *testing.T) {
	useIsolatedConfig(t)
	p := previewingFake{fake.New()}

	shellOut, err := executeWith(t, p, "--preview", "shell", "demo", "--root")
	if err != nil {
		t.Fatalf("shell --preview --root: %v (%s)", err, shellOut)
	}
	if !strings.Contains(shellOut, "--as-root") {
		t.Errorf("shell --root output %q should reflect Root=true reaching the provider", shellOut)
	}

	// --root must precede the instance name for exec: SetInterspersed(false)
	// (see exec.go) passes everything from the first positional arg onward
	// straight through as the inner command, by design, so it isn't
	// mistaken for a flag meant for the inner command.
	execOut, err := executeWith(t, p, "--preview", "exec", "--root", "demo", "--", "whoami")
	if err != nil {
		t.Fatalf("exec --preview --root: %v (%s)", err, execOut)
	}
	if !strings.Contains(execOut, "--as-root") {
		t.Errorf("exec --root output %q should reflect Root=true reaching the provider", execOut)
	}

	// Without --root, no trace of it.
	noRootOut, err := executeWith(t, p, "--preview", "shell", "demo")
	if err != nil {
		t.Fatalf("shell --preview: %v (%s)", err, noRootOut)
	}
	if strings.Contains(noRootOut, "--as-root") {
		t.Errorf("shell without --root should not reflect Root=true, got %q", noRootOut)
	}
}

func TestCLI_Preview_UnsupportedProviderPrintsHonestNotice(t *testing.T) {
	useIsolatedConfig(t)
	// A plain fake.Provider does not implement provider.CommandPreviewer.
	p := fake.New()

	out, err := executeWith(t, p, "--preview", "create", "demo", "--image=ubuntu")
	if err != nil {
		t.Fatalf("create --preview: %v (%s)", err, out)
	}
	if !strings.Contains(out, "not yet supported for provider") {
		t.Errorf("output %q should explain preview isn't supported, not silently do nothing", out)
	}
}

func TestCLI_Preview_CapabilityGateStillAppliesFirst(t *testing.T) {
	useIsolatedConfig(t)
	p := previewingFake{fake.New().WithCapability(provider.Capability{
		Feature: provider.FeatureCreate,
		Status:  provider.UnderDevelopment,
		Message: "pretend this is Lima",
	})}

	out, err := executeWith(t, p, "--preview", "create", "demo", "--image=ubuntu")
	if err == nil {
		t.Fatal("expected the capability gate to block before preview is ever consulted")
	}
	exitErr, ok := err.(*ExitError)
	if !ok || exitErr.Code != 2 {
		t.Fatalf("expected ExitError code 2, got %v", err)
	}
	if strings.Contains(out, "fakectl") {
		t.Errorf("output %q should not contain a previewed command when the gate blocks", out)
	}
}
