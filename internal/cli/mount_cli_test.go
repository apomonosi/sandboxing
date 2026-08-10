package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

// useIsolatedHome points $HOME at a temporary directory so mount
// resolution — which touches the real filesystem — never reads or creates
// anything under the developer's own home.
func useIsolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func mustMkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
	return path
}

func TestCLI_CreateWithMount(t *testing.T) {
	useIsolatedConfig(t)
	home := useIsolatedHome(t)
	src := mustMkdir(t, filepath.Join(home, "src", "project"))
	p := fake.New()

	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--mount", src+":/workspace:w")
	if err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
	if !strings.Contains(out, src+" -> /workspace (writable)") {
		t.Errorf("create output %q should report the mount it applied", out)
	}

	inst, err := p.Status(t.Context(), "demo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(inst.Mounts) != 1 || inst.Mounts[0].GuestPath != "/workspace" || !inst.Mounts[0].Writable {
		t.Errorf("instance mounts = %+v, want one writable /workspace mount", inst.Mounts)
	}
}

// A bare create still gets the built-in profile's per-instance workspace,
// and nothing else — decision 4.
func TestCLI_CreateDefaultsToWorkspaceMountOnly(t *testing.T) {
	useIsolatedConfig(t)
	home := useIsolatedHome(t)
	p := fake.New()

	if out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--profile=default"); err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
	inst, err := p.Status(t.Context(), "demo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(inst.Mounts) != 1 {
		t.Fatalf("instance mounts = %+v, want exactly the workspace mount", inst.Mounts)
	}
	if inst.Mounts[0].GuestPath != "/workspace" {
		t.Errorf("guest path = %q, want /workspace", inst.Mounts[0].GuestPath)
	}
	// The {{.Name}} template must have been expanded — a literal
	// "{{.Name}}" reaching a backend is the bug this replaced.
	if !strings.Contains(inst.Mounts[0].HostPath, "demo") || strings.Contains(inst.Mounts[0].HostPath, "{{") {
		t.Errorf("host path = %q, want the instance name substituted in", inst.Mounts[0].HostPath)
	}
	if !strings.HasPrefix(inst.Mounts[0].HostPath, home) {
		t.Errorf("host path = %q, want it under the (temporary) home %q", inst.Mounts[0].HostPath, home)
	}
}

func TestCLI_CreateWithMountNone(t *testing.T) {
	useIsolatedConfig(t)
	useIsolatedHome(t)
	p := fake.New()

	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--profile=default", "--mount-none")
	if err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
	if !strings.Contains(out, "no host directories") {
		t.Errorf("create output %q should say no directories are exposed", out)
	}
	inst, err := p.Status(t.Context(), "demo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(inst.Mounts) != 0 {
		t.Errorf("instance mounts = %+v, want none after --mount-none", inst.Mounts)
	}
}

// The reuse cycle this whole feature exists for: one sandbox, rebound to a
// different project between boots, with the previous project's directory
// gone rather than accumulating.
func TestCLI_StartRebindsMountsAcrossBoots(t *testing.T) {
	useIsolatedConfig(t)
	home := useIsolatedHome(t)
	alpha := mustMkdir(t, filepath.Join(home, "src", "alpha"))
	beta := mustMkdir(t, filepath.Join(home, "src", "beta"))
	p := fake.New()

	if out, err := execute(t, p, "create", "work", "--image=ubuntu", "--mount-none"); err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
	if out, err := execute(t, p, "start", "work", "--mount", alpha+":/workspace:w"); err != nil {
		t.Fatalf("start alpha: %v (%s)", err, out)
	}
	assertSingleMount(t, p, alpha)

	if _, err := execute(t, p, "stop", "work"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if out, err := execute(t, p, "start", "work", "--mount", beta+":/workspace:w"); err != nil {
		t.Fatalf("start beta: %v (%s)", err, out)
	}
	assertSingleMount(t, p, beta)
}

// Mounts are sticky: a bare start must reuse whatever was last applied
// rather than resetting the instance to its profile's defaults.
func TestCLI_StartWithoutMountFlagsKeepsExistingMounts(t *testing.T) {
	useIsolatedConfig(t)
	home := useIsolatedHome(t)
	alpha := mustMkdir(t, filepath.Join(home, "src", "alpha"))
	p := fake.New()

	if _, err := execute(t, p, "create", "work", "--image=ubuntu", "--mount-none"); err != nil {
		t.Fatal("create failed")
	}
	if _, err := execute(t, p, "start", "work", "--mount", alpha+":/workspace:w"); err != nil {
		t.Fatal("start with mount failed")
	}
	if _, err := execute(t, p, "stop", "work"); err != nil {
		t.Fatal("stop failed")
	}

	out, err := execute(t, p, "start", "work")
	if err != nil {
		t.Fatalf("bare start: %v (%s)", err, out)
	}
	assertSingleMount(t, p, alpha)
	// Sticky mounts are only safe if they're visible, so a bare start
	// still prints what the sandbox can see.
	if !strings.Contains(out, alpha) {
		t.Errorf("bare start output %q should still report the active mount set", out)
	}
}

// A mount outside the profile's allowedRoots must be refused at start,
// not just at create: the constraint is re-checked on every boot.
func TestCLI_StartRejectsMountOutsideAllowedRoots(t *testing.T) {
	useIsolatedConfig(t)
	home := useIsolatedHome(t)
	outside := mustMkdir(t, filepath.Join(home, "elsewhere"))
	p := fake.New()

	if _, err := execute(t, p, "create", "work", "--image=ubuntu", "--mount-none"); err != nil {
		t.Fatal("create failed")
	}
	out, err := execute(t, p, "start", "work", "--profile=strict", "--mount", outside+":/workspace")
	if err == nil {
		t.Fatalf("start = nil error for a mount outside allowedRoots, want a failure (%s)", out)
	}
}

// A backend that can't mount at all must block rather than silently
// creating a sandbox without the directory the user asked for.
func TestCLI_CreateBlocksWhenMountUnsupported(t *testing.T) {
	useIsolatedConfig(t)
	home := useIsolatedHome(t)
	src := mustMkdir(t, filepath.Join(home, "src"))
	p := fake.New().WithCapability(provider.Capability{
		Feature: provider.FeatureMount,
		Status:  provider.NotAvailable,
		Message: "no host-directory share primitive",
	})

	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--mount", src+":/workspace")
	if err == nil {
		t.Fatalf("create = nil error on a backend without fs.mount, want a capability block (%s)", out)
	}
	if !strings.Contains(out, "fs.mount") {
		t.Errorf("output %q should name the blocked feature", out)
	}
}

// --preview promises that nothing happens. mountPolicy.createMissing is
// the one part of mount resolution that writes to disk, so previewing a
// create must not honor it — otherwise preview silently creates the
// workspace directory it is only supposed to describe.
func TestCLI_PreviewDoesNotCreateHostDirectories(t *testing.T) {
	useIsolatedConfig(t)
	home := useIsolatedHome(t)
	p := fake.New()

	out, err := execute(t, p, "--preview", "create", "demo", "--image=ubuntu", "--profile=default")
	if err != nil {
		t.Fatalf("preview create: %v (%s)", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "agentctl", "workspaces", "demo")); !os.IsNotExist(err) {
		t.Errorf("--preview created the workspace directory on disk (stat err = %v), want nothing written", err)
	}
	if _, err := p.Status(t.Context(), "demo"); err == nil {
		t.Error("--preview created an instance, want nothing created")
	}
}

func assertSingleMount(t *testing.T, p *fake.Provider, wantHostPath string) {
	t.Helper()
	inst, err := p.Status(t.Context(), "work")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(inst.Mounts) != 1 {
		t.Fatalf("instance mounts = %+v, want exactly one", inst.Mounts)
	}
	if inst.Mounts[0].HostPath != wantHostPath {
		t.Errorf("mounted %q, want %q", inst.Mounts[0].HostPath, wantHostPath)
	}
}
