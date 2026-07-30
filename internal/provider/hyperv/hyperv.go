// Package hyperv will hold agentctl's Windows backend, driving Hyper-V
// via its PowerShell cmdlets. For this milestone it is a capability-table-
// driven stub: every method returns the sentinel error matching its
// Capabilities() entry, so the CLI already behaves correctly (and
// demoably) on Windows before real PowerShell integration exists.
package hyperv

import (
	"context"
	"io"

	"github.com/apomonosi/sandboxing/internal/provider"
)

type Provider struct {
	capabilities provider.Table
}

// New builds the Hyper-V stub provider. It intentionally does not check
// for the Hyper-V feature/PowerShell availability yet, since no method
// does real work.
func New() (provider.Provider, error) {
	return &Provider{capabilities: buildCapabilities()}, nil
}

func (p *Provider) Name() string { return "hyperv" }

func (p *Provider) Capabilities() provider.Table { return p.capabilities }

func (p *Provider) err(f provider.Feature) error {
	return provider.StatusError(p.capabilities.Get(f))
}

func (p *Provider) Create(ctx context.Context, spec provider.InstanceSpec) (*provider.Instance, error) {
	return nil, p.err(provider.FeatureCreate)
}

func (p *Provider) Start(ctx context.Context, name string) error {
	return p.err(provider.FeatureStart)
}

func (p *Provider) Stop(ctx context.Context, name string, opts provider.StopOptions) error {
	return p.err(provider.FeatureStop)
}

func (p *Provider) Delete(ctx context.Context, name string, force bool) error {
	return p.err(provider.FeatureDelete)
}

func (p *Provider) List(ctx context.Context) ([]provider.Instance, error) {
	return nil, p.err(provider.FeatureList)
}

func (p *Provider) Status(ctx context.Context, name string) (*provider.Instance, error) {
	return nil, p.err(provider.FeatureStatus)
}

func (p *Provider) Exec(ctx context.Context, name string, opts provider.ExecOptions) (int, error) {
	return -1, p.err(provider.FeatureExec)
}

func (p *Provider) Shell(ctx context.Context, name string, opts provider.ShellOptions) error {
	return p.err(provider.FeatureShell)
}

func (p *Provider) View(ctx context.Context, name string, opts provider.ViewOptions) error {
	return p.err(provider.FeatureView)
}

func (p *Provider) SnapshotCreate(ctx context.Context, name, snapshotName string) error {
	return p.err(provider.FeatureSnapshotCreate)
}

func (p *Provider) SnapshotList(ctx context.Context, name string) ([]provider.Snapshot, error) {
	return nil, p.err(provider.FeatureSnapshotList)
}

func (p *Provider) SnapshotRestore(ctx context.Context, name, snapshotName string) error {
	return p.err(provider.FeatureSnapshotRestore)
}

func (p *Provider) SnapshotDelete(ctx context.Context, name, snapshotName string) error {
	return p.err(provider.FeatureSnapshotDelete)
}

func (p *Provider) ApplyNetworkPolicy(ctx context.Context, name string, policy provider.NetworkPolicy) error {
	return p.err(provider.FeatureNetworkACL)
}

func (p *Provider) ImagePull(ctx context.Context, ref string) error {
	return p.err(provider.FeatureImagePull)
}

func (p *Provider) ImageBuild(ctx context.Context, spec provider.ImageBuildSpec) (string, error) {
	return "", p.err(provider.FeatureImageBuild)
}

func (p *Provider) Logs(ctx context.Context, name string, opts provider.LogOptions) (io.ReadCloser, error) {
	return nil, p.err(provider.FeatureLogsNetwork)
}
