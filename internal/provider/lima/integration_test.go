//go:build integration

package lima

import (
	"context"
	"os"
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
