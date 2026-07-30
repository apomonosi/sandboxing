// Package incus implements agentctl's Linux backend by shelling out to
// the `incus` CLI (not the Incus Go client SDK — see internal/provider.Runner
// for why). This is the milestone's one real, working Provider; Lima and
// Hyper-V ship as capability-table-driven stubs until their own CLIs get
// wired up the same way.
package incus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/apomonosi/sandboxing/internal/provider"
)

const binary = "incus"

type Provider struct {
	runner       provider.Runner
	capabilities provider.Table
}

// New builds the Incus provider using the real ExecRunner. It does not
// fail if the `incus` binary is missing — that surfaces naturally as a
// command-not-found error the first time a method actually runs one,
// which keeps New cheap enough to call during provider registration.
func New() (provider.Provider, error) {
	return NewWithRunner(provider.ExecRunner{}), nil
}

// NewWithRunner builds the Incus provider with an injected Runner, for
// tests that want to fake `incus` invocations without a real daemon.
func NewWithRunner(r provider.Runner) provider.Provider {
	return &Provider{runner: r, capabilities: buildCapabilities()}
}

func (p *Provider) Name() string { return "incus" }

func (p *Provider) Capabilities() provider.Table { return p.capabilities }

func (p *Provider) run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	stdout, stderr, err := p.runner.Run(ctx, binary, args...)
	if err != nil {
		return stdout, stderr, fmt.Errorf("incus %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(stderr)))
	}
	return stdout, stderr, nil
}

func (p *Provider) Create(ctx context.Context, spec provider.InstanceSpec) (*provider.Instance, error) {
	if _, _, err := p.run(ctx, append([]string{"init"}, buildInitArgs(spec)...)...); err != nil {
		return nil, err
	}

	if args := buildRootDiskResizeArgs(spec.Name, spec.Resources.DiskSize); args != nil {
		if _, _, err := p.run(ctx, args...); err != nil {
			return nil, fmt.Errorf("resizing root disk: %w", err)
		}
	}

	for i, m := range spec.Mounts {
		devName := fmt.Sprintf("mount%d", i)
		if _, _, err := p.run(ctx, buildMountDeviceArgs(spec.Name, devName, m)...); err != nil {
			return nil, fmt.Errorf("adding mount %s: %w", m.GuestPath, err)
		}
	}

	for i, pp := range spec.Overrides.Ports {
		devName := fmt.Sprintf("port%d", i)
		if _, _, err := p.run(ctx, buildPortProxyDeviceArgs(spec.Name, devName, pp)...); err != nil {
			return nil, fmt.Errorf("publishing port %d: %w", pp.HostPort, err)
		}
	}

	if err := p.ApplyNetworkPolicy(ctx, spec.Name, spec.Overrides); err != nil {
		return nil, fmt.Errorf("applying network policy: %w", err)
	}

	return p.Status(ctx, spec.Name)
}

func (p *Provider) Start(ctx context.Context, name string) error {
	_, _, err := p.run(ctx, "start", name)
	return err
}

func (p *Provider) Stop(ctx context.Context, name string, opts provider.StopOptions) error {
	args := []string{"stop", name}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", int(opts.Timeout.Seconds())))
	}
	_, _, err := p.run(ctx, args...)
	return err
}

func (p *Provider) Delete(ctx context.Context, name string, force bool) error {
	args := []string{"delete", name}
	if force {
		args = append(args, "--force")
	}
	_, _, err := p.run(ctx, args...)
	return err
}

func (p *Provider) List(ctx context.Context) ([]provider.Instance, error) {
	stdout, _, err := p.run(ctx, "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var raw []instanceJSON
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("parsing incus list output: %w", err)
	}
	out := make([]provider.Instance, len(raw))
	for i, j := range raw {
		out[i] = toInstance(j)
	}
	return out, nil
}

func (p *Provider) Status(ctx context.Context, name string) (*provider.Instance, error) {
	stdout, _, err := p.run(ctx, "list", name, "--format", "json")
	if err != nil {
		return nil, err
	}
	var raw []instanceJSON
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("parsing incus list output: %w", err)
	}
	if len(raw) == 0 {
		return nil, provider.ErrNotFound
	}
	inst := toInstance(raw[0])
	return &inst, nil
}

func (p *Provider) Exec(ctx context.Context, name string, opts provider.ExecOptions) (int, error) {
	args := append([]string{"exec", name, "--"}, opts.Command...)
	return p.runner.RunStream(ctx, binary, args, opts.Stdin, opts.Stdout, opts.Stderr)
}

func (p *Provider) Shell(ctx context.Context, name string, opts provider.ShellOptions) error {
	_, err := p.runner.RunStream(ctx, binary, []string{"exec", name, "--", "/bin/bash"}, opts.Stdin, opts.Stdout, opts.Stderr)
	return err
}

// View spawns Incus's native SPICE console against the instance's own
// display, never the guest's X server — see docs/user/view-and-console.md
// for why raw X11 forwarding is deliberately excluded from agentctl.
func (p *Provider) View(ctx context.Context, name string, opts provider.ViewOptions) error {
	args := []string{"console", name, "--type=vga"}
	var stdout, stderr bytes.Buffer
	_, err := p.runner.RunStream(ctx, binary, args, nil, &stdout, &stderr)
	return err
}

func (p *Provider) SnapshotCreate(ctx context.Context, name, snapshotName string) error {
	_, _, err := p.run(ctx, "snapshot", "create", name, snapshotName)
	return err
}

func (p *Provider) SnapshotList(ctx context.Context, name string) ([]provider.Snapshot, error) {
	stdout, _, err := p.run(ctx, "query", fmt.Sprintf("/1.0/instances/%s/snapshots?recursion=1", name))
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("parsing snapshot list: %w", err)
	}
	out := make([]provider.Snapshot, len(raw))
	for i, s := range raw {
		out[i] = provider.Snapshot{Name: s.Name}
	}
	return out, nil
}

func (p *Provider) SnapshotRestore(ctx context.Context, name, snapshotName string) error {
	_, _, err := p.run(ctx, "snapshot", "restore", name, snapshotName)
	return err
}

func (p *Provider) SnapshotDelete(ctx context.Context, name, snapshotName string) error {
	_, _, err := p.run(ctx, "snapshot", "delete", name, snapshotName)
	return err
}

// ApplyNetworkPolicy resolves domain-based allow rules to IP addresses
// (a best-effort snapshot, not dynamic DNS-aware filtering — see the
// doc comment on networkACLCommands) and applies the resulting Incus
// network ACL, replacing any previous one for this instance.
func (p *Provider) ApplyNetworkPolicy(ctx context.Context, name string, policy provider.NetworkPolicy) error {
	resolved := resolveAllowRules(policy.Allow)
	// Best-effort cleanup of a previous ACL from an earlier apply; ignore
	// errors since it may not exist yet.
	_, _, _ = p.run(ctx, "network", "acl", "delete", aclName(name))
	for _, args := range networkACLCommands(name, policy, resolved) {
		if _, _, err := p.run(ctx, args...); err != nil {
			return err
		}
	}
	return nil
}

// resolveAllowRules resolves each AllowRule's domain to its current IPs
// via normal DNS lookup. A leading "*." wildcard is stripped and only the
// apex domain is resolved — matching the exact set of hosts behind a
// wildcard is not something IP-based ACLs can express, and is called out
// as a known limitation in docs/admin/security-model.md.
func resolveAllowRules(rules []provider.AllowRule) map[string][]string {
	out := make(map[string][]string, len(rules))
	for _, rule := range rules {
		host := strings.TrimPrefix(rule.Domain, "*.")
		ips, err := net.LookupHost(host)
		if err != nil {
			continue // best-effort: an unresolvable domain simply gets no allow rule
		}
		out[rule.Domain] = ips
	}
	return out
}

func (p *Provider) ImagePull(ctx context.Context, ref string) error {
	_, _, err := p.run(ctx, "image", "copy", ref, "local:")
	return err
}

func (p *Provider) ImageBuild(ctx context.Context, spec provider.ImageBuildSpec) (string, error) {
	return "", provider.StatusError(p.capabilities.Get(provider.FeatureImageBuild))
}

func (p *Provider) Logs(ctx context.Context, name string, opts provider.LogOptions) (io.ReadCloser, error) {
	return nil, provider.StatusError(p.capabilities.Get(provider.FeatureLogsNetwork))
}
