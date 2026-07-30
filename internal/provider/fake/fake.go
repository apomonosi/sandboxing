// Package fake provides an in-memory provider.Provider implementation used
// by unit tests across internal/cli, internal/provider, and anywhere else
// that needs a working backend without a real Incus/Lima/Hyper-V install.
// Its capability table defaults to Supported for every feature, but every
// entry can be overridden per test (e.g. to simulate "pretend this is
// Lima" and assert the CLI's capability-gate behavior) without compiling
// against any real backend package.
package fake

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// Provider is an in-memory, concurrency-safe fake implementing
// provider.Provider.
type Provider struct {
	mu           sync.Mutex
	instances    map[string]*provider.Instance
	snapshots    map[string][]provider.Snapshot
	capabilities provider.Table
	now          func() time.Time
}

// New builds a fake Provider with a fully Supported capability table.
// Use WithCapability to override individual entries.
func New() *Provider {
	table := make(provider.Table, len(provider.AllFeatures))
	for _, f := range provider.AllFeatures {
		table[f] = provider.Capability{Feature: f, Status: provider.Supported}
	}
	return &Provider{
		instances:    make(map[string]*provider.Instance),
		snapshots:    make(map[string][]provider.Snapshot),
		capabilities: table,
		now:          time.Now,
	}
}

// WithCapability overrides a single capability entry and returns the
// receiver for chaining.
func (p *Provider) WithCapability(c provider.Capability) *Provider {
	p.capabilities[c.Feature] = c
	return p
}

// WithClock overrides the time source (for deterministic tests).
func (p *Provider) WithClock(now func() time.Time) *Provider {
	p.now = now
	return p
}

func (p *Provider) Name() string { return "fake" }

func (p *Provider) Capabilities() provider.Table { return p.capabilities }

func (p *Provider) Create(ctx context.Context, spec provider.InstanceSpec) (*provider.Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.instances[spec.Name]; exists {
		return nil, provider.ErrAlreadyExists
	}
	inst := &provider.Instance{
		Name:      spec.Name,
		Provider:  p.Name(),
		Status:    provider.StatusStopped,
		Image:     spec.Image,
		Profiles:  append([]string(nil), spec.Profiles...),
		CreatedAt: p.now(),
	}
	p.instances[spec.Name] = inst
	cp := *inst
	return &cp, nil
}

func (p *Provider) lookup(name string) (*provider.Instance, error) {
	inst, ok := p.instances[name]
	if !ok {
		return nil, provider.ErrNotFound
	}
	return inst, nil
}

func (p *Provider) Start(ctx context.Context, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	inst, err := p.lookup(name)
	if err != nil {
		return err
	}
	inst.Status = provider.StatusRunning
	inst.IPs = []string{"10.0.0.2"}
	return nil
}

func (p *Provider) Stop(ctx context.Context, name string, opts provider.StopOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	inst, err := p.lookup(name)
	if err != nil {
		return err
	}
	inst.Status = provider.StatusStopped
	inst.IPs = nil
	return nil
}

func (p *Provider) Delete(ctx context.Context, name string, force bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	inst, err := p.lookup(name)
	if err != nil {
		return err
	}
	if inst.Status == provider.StatusRunning && !force {
		return fmt.Errorf("instance %q is running, use --force to delete anyway", name)
	}
	delete(p.instances, name)
	delete(p.snapshots, name)
	return nil
}

func (p *Provider) List(ctx context.Context) ([]provider.Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]provider.Instance, 0, len(p.instances))
	for _, inst := range p.instances {
		out = append(out, *inst)
	}
	return out, nil
}

func (p *Provider) Status(ctx context.Context, name string) (*provider.Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	inst, err := p.lookup(name)
	if err != nil {
		return nil, err
	}
	cp := *inst
	return &cp, nil
}

func (p *Provider) Exec(ctx context.Context, name string, opts provider.ExecOptions) (int, error) {
	p.mu.Lock()
	inst, err := p.lookup(name)
	p.mu.Unlock()
	if err != nil {
		return -1, err
	}
	if inst.Status != provider.StatusRunning {
		return -1, fmt.Errorf("instance %q is not running", name)
	}
	if opts.Stdout != nil {
		fmt.Fprintf(opts.Stdout, "fake-exec: %s\n", strings.Join(opts.Command, " "))
	}
	return 0, nil
}

func (p *Provider) Shell(ctx context.Context, name string, opts provider.ShellOptions) error {
	_, err := p.Exec(ctx, name, provider.ExecOptions{Command: []string{"/bin/sh"}, Stdout: opts.Stdout})
	return err
}

func (p *Provider) View(ctx context.Context, name string, opts provider.ViewOptions) error {
	p.mu.Lock()
	_, err := p.lookup(name)
	p.mu.Unlock()
	return err
}

func (p *Provider) SnapshotCreate(ctx context.Context, name, snapshotName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.lookup(name); err != nil {
		return err
	}
	p.snapshots[name] = append(p.snapshots[name], provider.Snapshot{Name: snapshotName, CreatedAt: p.now()})
	return nil
}

func (p *Provider) SnapshotList(ctx context.Context, name string) ([]provider.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.lookup(name); err != nil {
		return nil, err
	}
	return append([]provider.Snapshot(nil), p.snapshots[name]...), nil
}

func (p *Provider) SnapshotRestore(ctx context.Context, name, snapshotName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.lookup(name); err != nil {
		return err
	}
	for _, s := range p.snapshots[name] {
		if s.Name == snapshotName {
			return nil
		}
	}
	return fmt.Errorf("snapshot %q not found for instance %q", snapshotName, name)
}

func (p *Provider) SnapshotDelete(ctx context.Context, name, snapshotName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	snaps := p.snapshots[name]
	for i, s := range snaps {
		if s.Name == snapshotName {
			p.snapshots[name] = append(snaps[:i], snaps[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("snapshot %q not found for instance %q", snapshotName, name)
}

func (p *Provider) ApplyNetworkPolicy(ctx context.Context, name string, policy provider.NetworkPolicy) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.lookup(name)
	return err
}

func (p *Provider) ImagePull(ctx context.Context, ref string) error { return nil }

func (p *Provider) ImageBuild(ctx context.Context, spec provider.ImageBuildSpec) (string, error) {
	return spec.OutputName, nil
}

func (p *Provider) Logs(ctx context.Context, name string, opts provider.LogOptions) (io.ReadCloser, error) {
	p.mu.Lock()
	_, err := p.lookup(name)
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader("")), nil
}
