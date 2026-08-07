//go:build integration

package lima

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// These tests exercise a real `limactl` install and are excluded from a
// plain `go test ./...` by the integration build tag above. They
// additionally check AGENTCTL_INTEGRATION_LIMA at runtime as a second,
// belt-and-suspenders gate. This is also where every "unverified against
// a live install" NOTE in translate.go/lima.go/json.go gets empirically
// resolved — required reading before this backend ships, not optional
// polish.
// Run via: AGENTCTL_INTEGRATION_LIMA=1 go test -tags=integration ./internal/provider/lima/...
func skipUnlessIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTCTL_INTEGRATION_LIMA") != "1" {
		t.Skip("set AGENTCTL_INTEGRATION_LIMA=1 to run against a real limactl install")
	}
}

func TestIntegration_CreateStartExecStatusStopDelete(t *testing.T) {
	skipUnlessIntegration(t)

	p := NewWithRunner(provider.ExecRunner{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := "agentctl-it-lifecycle"
	t.Cleanup(func() { _ = p.Delete(context.Background(), name, true) })

	if _, err := p.Create(ctx, provider.InstanceSpec{Name: name, Image: "template://ubuntu-lts"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Start(ctx, name); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if code, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"true"}}); err != nil || code != 0 {
		t.Fatalf("Exec(true): code=%d err=%v", code, err)
	}
	if code, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"whoami"}}); err != nil || code != 0 {
		t.Fatalf("Exec(whoami) as default non-root user: code=%d err=%v", code, err)
	}
	if code, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"whoami"}, Root: true}); err != nil || code != 0 {
		t.Fatalf("Exec(whoami, --root): code=%d err=%v", code, err)
	}
	inst, err := p.Status(ctx, name)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if inst.Status != provider.StatusRunning {
		t.Errorf("Status = %q, want running", inst.Status)
	}
	if err := p.Stop(ctx, name, provider.StopOptions{Force: true}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := p.Delete(ctx, name, false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// TestIntegration_TemplateMountsDoNotLeakIn is the empirical check on the
// isolation bug this backend shipped with: buildSetExpressions used to
// only append to `.mounts`, and Lima's stock templates declare their own
// (historically the host home directory read-only, plus /tmp/lima
// writable). A sandbox could therefore see all of $HOME while agentctl's
// docs promised otherwise.
//
// Create with no mounts at all, then assert the guest cannot see the
// host's home directory. This is the one Lima test most directly tied to
// agentctl's stated purpose, the mount-side counterpart of Incus's
// TestIntegration_ApplyNetworkPolicy_BlocksLAN.
func TestIntegration_TemplateMountsDoNotLeakIn(t *testing.T) {
	skipUnlessIntegration(t)

	p := NewWithRunner(provider.ExecRunner{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := "agentctl-it-no-template-mounts"
	t.Cleanup(func() { _ = p.Delete(context.Background(), name, true) })

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolving home directory: %v", err)
	}
	marker := filepath.Join(home, ".agentctl-mount-leak-probe")
	if err := os.WriteFile(marker, []byte("probe\n"), 0o600); err != nil {
		t.Fatalf("writing probe file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(marker) })

	if _, err := p.Create(ctx, provider.InstanceSpec{Name: name, Image: "template://ubuntu-lts"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Start(ctx, name); err != nil {
		t.Fatalf("Start: %v", err)
	}

	code, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"test", "-f", marker}})
	if err != nil {
		t.Fatalf("Exec(test): %v", err)
	}
	if code == 0 {
		t.Errorf("the guest can read %s: the template's own mounts leaked in and the host home directory is exposed", marker)
	}
}

// TestIntegration_ApplyMountPolicy_RebindsAcrossBoots is Lima's half of
// the reuse cycle `start --mount` exists for, mirroring the Incus test of
// the same name. It also exercises the assumption that `limactl edit
// --set` accepts the reset-then-append expression sequence on a stopped
// instance.
func TestIntegration_ApplyMountPolicy_RebindsAcrossBoots(t *testing.T) {
	skipUnlessIntegration(t)

	p := NewWithRunner(provider.ExecRunner{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := "agentctl-it-mount-rebind"
	t.Cleanup(func() { _ = p.Delete(context.Background(), name, true) })

	alpha, beta := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(alpha, "alpha.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatalf("seeding alpha: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beta, "beta.txt"), []byte("b\n"), 0o600); err != nil {
		t.Fatalf("seeding beta: %v", err)
	}

	if _, err := p.Create(ctx, provider.InstanceSpec{
		Name:   name,
		Image:  "template://ubuntu-lts",
		Mounts: []provider.Mount{{HostPath: alpha, GuestPath: "/workspace", Writable: true}},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Start(ctx, name); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if code, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"test", "-f", "/workspace/alpha.txt"}}); err != nil || code != 0 {
		t.Fatalf("alpha not visible after create: code=%d err=%v", code, err)
	}
	if err := p.Stop(ctx, name, provider.StopOptions{Force: true}); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := p.ApplyMountPolicy(ctx, name, []provider.Mount{{HostPath: beta, GuestPath: "/workspace", Writable: true}}); err != nil {
		t.Fatalf("ApplyMountPolicy: %v", err)
	}
	if err := p.Start(ctx, name); err != nil {
		t.Fatalf("Start after rebind: %v", err)
	}
	if code, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"test", "-f", "/workspace/beta.txt"}}); err != nil || code != 0 {
		t.Fatalf("beta not visible after rebind: code=%d err=%v", code, err)
	}
	code, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"test", "-f", "/workspace/alpha.txt"}})
	if err != nil {
		t.Fatalf("Exec(test alpha): %v", err)
	}
	if code == 0 {
		t.Error("the previous project's directory is still visible after a rebind; the old mount was not removed")
	}
}
