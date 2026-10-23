package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

// stopCountingFake counts Stop calls and, like limactl, fails a stop of an
// instance that isn't running.
type stopCountingFake struct {
	*fake.Provider
	stops int
}

func (p *stopCountingFake) Stop(ctx context.Context, name string, opts provider.StopOptions) error {
	p.stops++
	if inst, err := p.Provider.Status(ctx, name); err == nil && inst.Status != provider.StatusRunning {
		return errors.New("expected status `Running`, got `Stopped`")
	}
	return p.Provider.Stop(ctx, name, opts)
}

// Stopping a stopped instance says so and succeeds, without asking the
// backend to stop it; --force still goes to the backend, as before.
func TestCLI_Stop_AlreadyStopped(t *testing.T) {
	useIsolatedConfig(t)
	p := &stopCountingFake{Provider: fake.New()}
	if _, err := executeProvider(t, p, "create", "pibox", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := executeProvider(t, p, "stop", "pibox")
	if err != nil {
		t.Fatalf("stop of a stopped instance: %v (%s)", err, out)
	}
	if out != "agentctl: pibox is already stopped\n" {
		t.Errorf("stop output = %q, want the already-stopped line", out)
	}
	if p.stops != 0 {
		t.Errorf("backend Stop called %d times, want 0", p.stops)
	}

	if _, err := executeProvider(t, p, "stop", "pibox", "--force"); err == nil || p.stops != 1 {
		t.Errorf("stop --force: err %v, backend Stop calls %d; want the backend called and its error returned", err, p.stops)
	}
}

func TestCLI_Stop_RunningInstanceStops(t *testing.T) {
	useIsolatedConfig(t)
	p := &stopCountingFake{Provider: fake.New()}
	if _, err := executeProvider(t, p, "create", "pibox", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := executeProvider(t, p, "start", "pibox"); err != nil {
		t.Fatalf("start: %v", err)
	}

	out, err := executeProvider(t, p, "stop", "pibox")
	if err != nil {
		t.Fatalf("stop: %v (%s)", err, out)
	}
	if strings.Contains(out, "already stopped") {
		t.Errorf("stop output %q claims a running instance was already stopped", out)
	}
	inst, err := p.Status(t.Context(), "pibox")
	if err != nil || inst.Status != provider.StatusStopped || p.stops != 1 {
		t.Errorf("after stop: status %v (err %v), backend Stop calls %d; want stopped, 1", inst, err, p.stops)
	}
}
