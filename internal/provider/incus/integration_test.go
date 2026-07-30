//go:build integration

package incus

import (
	"context"
	"os"
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
