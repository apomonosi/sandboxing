//go:build integration

package incus

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// These tests exercise a real `incus` daemon and are excluded from a plain
// `go test ./...` by the integration build tag above. They additionally
// check AGENTCTL_INTEGRATION_INCUS at runtime as a second, belt-and-
// suspenders gate — this protects a `-tags=integration` run on a machine
// that has the `incus` binary but no initialized daemon/permissions.
// Run via: AGENTCTL_INTEGRATION_INCUS=1 go test -tags=integration ./internal/provider/incus/...
func skipUnlessIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTCTL_INTEGRATION_INCUS") != "1" {
		t.Skip("set AGENTCTL_INTEGRATION_INCUS=1 to run against a real incus daemon")
	}
}

func TestIntegration_CreateStartExecStatusStopDelete(t *testing.T) {
	skipUnlessIntegration(t)

	p := NewWithRunner(provider.ExecRunner{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	name := "agentctl-it-lifecycle"
	t.Cleanup(func() { _ = p.Delete(context.Background(), name, true) })

	if _, err := p.Create(ctx, provider.InstanceSpec{Name: name, Image: "images:alpine/edge"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Start(ctx, name); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if code, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"true"}}); err != nil || code != 0 {
		t.Fatalf("Exec(true): code=%d err=%v", code, err)
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

// TestIntegration_ApplyNetworkPolicy_BlocksLAN is the single test most
// directly tied to agentctl's actual security goal: it asserts that an
// instance with the default network policy genuinely cannot reach another
// address on the local network, not just that the ACL commands succeeded.
func TestIntegration_ApplyNetworkPolicy_BlocksLAN(t *testing.T) {
	skipUnlessIntegration(t)

	p := NewWithRunner(provider.ExecRunner{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	name := "agentctl-it-lan-block"
	t.Cleanup(func() { _ = p.Delete(context.Background(), name, true) })

	if _, err := p.Create(ctx, provider.InstanceSpec{
		Name:      name,
		Image:     "images:alpine/edge",
		Overrides: provider.NetworkPolicy{DenyLAN: true},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Start(ctx, name); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// A LAN gateway address is about as representative a "nearby network"
	// target as this test can pick without depending on the CI runner's
	// specific topology; a curl connect-timeout failure is what
	// deny-lan is supposed to cause.
	code, err := p.Exec(ctx, name, provider.ExecOptions{
		Command: []string{"sh", "-c", "curl -s --connect-timeout 3 http://192.168.1.1/ ; echo exit=$?"},
	})
	if err != nil {
		t.Fatalf("Exec(curl): %v", err)
	}
	if code == 0 {
		t.Error("expected the LAN request to fail (deny-lan should block it), but curl reported success")
	}
}

// TestIntegration_MountReadOnlyIsEnforced is what settles Incus's
// fs.mount.readonly capability entry.
//
// The disk device's `readonly` option is documented without a
// container-only condition, but a VM's share goes through virtiofs/9p
// rather than a bind mount, and that path has not been confirmed to honor
// it. agentctl claims Supported on the strength of the documentation; this
// test is the check. If a write to a read-only share succeeds here, the
// capability entry in capability.go is wrong and must become
// ManualWorkaround ("mount the source read-only on the host first")
// rather than staying a claim agentctl cannot back.
func TestIntegration_MountReadOnlyIsEnforced(t *testing.T) {
	skipUnlessIntegration(t)

	p := NewWithRunner(provider.ExecRunner{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	name := "agentctl-it-mount-ro"
	t.Cleanup(func() { _ = p.Delete(context.Background(), name, true) })

	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "marker.txt"), []byte("from-host\n"), 0o600); err != nil {
		t.Fatalf("seeding host directory: %v", err)
	}

	if _, err := p.Create(ctx, provider.InstanceSpec{
		Name:   name,
		Image:  "images:alpine/edge",
		Mounts: []provider.Mount{{HostPath: hostDir, GuestPath: "/ro"}},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Start(ctx, name); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var stdout bytes.Buffer
	code, err := p.Exec(ctx, name, provider.ExecOptions{
		Command: []string{"cat", "/ro/marker.txt"},
		Stdout:  &stdout,
	})
	if err != nil || code != 0 {
		t.Fatalf("reading the mounted file: code=%d err=%v", code, err)
	}
	if !strings.Contains(stdout.String(), "from-host") {
		t.Errorf("mounted file contents = %q, want the host's content", stdout.String())
	}

	code, err = p.Exec(ctx, name, provider.ExecOptions{
		Command: []string{"sh", "-c", "echo mutated > /ro/marker.txt"},
	})
	if err != nil {
		t.Fatalf("Exec(write): %v", err)
	}
	if code == 0 {
		t.Error("wrote to a read-only mount successfully; fs.mount.readonly is not enforced for VM shares and the Incus capability entry must be corrected")
	}
}

// TestIntegration_ApplyMountPolicy_RebindsAcrossBoots covers the reuse
// cycle `start --mount` exists for: one sandbox pointed at project A, then
// at project B, with A genuinely gone from the guest afterwards. A mount
// surviving a reconfigure would quietly break the whole model.
func TestIntegration_ApplyMountPolicy_RebindsAcrossBoots(t *testing.T) {
	skipUnlessIntegration(t)

	p := NewWithRunner(provider.ExecRunner{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
		Image:  "images:alpine/edge",
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
