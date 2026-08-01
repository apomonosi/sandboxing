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
	"time"

	"github.com/apomonosi/sandboxing/internal/provider"
)

const binary = "incus"

// agentNotRunningMsg is the substring Incus includes in its error when
// the in-guest incus-agent hasn't connected yet — most commonly right
// after `start`, before the guest has finished booting and the agent has
// had a chance to come up (reported in practice on Incus 6.23 / Fedora
// 44 immediately following `agentctl start` then `agentctl shell`).
const agentNotRunningMsg = "VM agent isn't currently running"

// defaultUsername is used when InstanceSpec.DefaultUser is empty — i.e.
// no --agent was given, just a plain create.
const defaultUsername = "agent"

// agentReadyTimeout and agentPollInterval bound how long Exec/Shell wait
// for the agent before giving up. Package-level vars (not consts) so
// tests can shrink them instead of waiting out a real 30s timeout.
var (
	agentReadyTimeout = 30 * time.Second
	agentPollInterval = time.Second
)

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

	// provisionDefaultUser needs `incus exec`, which requires a running
	// instance — a freshly `init`'d instance is stopped. Start it just
	// long enough to provision the user, then stop it again so `create`
	// still keeps its documented contract of not leaving the instance
	// running. A forceful stop is fine here: nothing but the bootstrap
	// script has touched the guest at this point, so there's no graceful-
	// shutdown state worth waiting on, and create() shouldn't be able to
	// hang on an image with unreliable ACPI shutdown support.
	if _, _, err := p.run(ctx, buildStartArgs(spec.Name)...); err != nil {
		return nil, fmt.Errorf("starting instance to provision default user: %w", err)
	}
	if err := p.waitForAgent(ctx, spec.Name, nil); err != nil {
		return nil, fmt.Errorf("waiting for the VM agent to provision default user: %w", err)
	}

	username := spec.DefaultUser
	if username == "" {
		username = defaultUsername
	}
	if err := p.provisionDefaultUser(ctx, spec.Name, username); err != nil {
		return nil, fmt.Errorf("provisioning default user: %w", err)
	}

	if _, _, err := p.run(ctx, buildStopArgs(spec.Name, provider.StopOptions{Force: true})...); err != nil {
		return nil, fmt.Errorf("stopping instance after provisioning default user: %w", err)
	}

	return p.Status(ctx, spec.Name)
}

// provisionDefaultUser runs bootstrapUserScript inside the instance (as
// root — creating a user requires it) to create username as a non-root
// account with passwordless sudo, then records the resulting uid/home in
// Incus's own per-instance config (userConfigKey) so Exec/Shell can look
// it up later without agentctl keeping any state of its own. Safe to call
// on an already-provisioned instance — the script is idempotent.
func (p *Provider) provisionDefaultUser(ctx context.Context, name, username string) error {
	var stdout, stderr bytes.Buffer
	exitCode, err := p.runner.RunStream(ctx, binary, buildExecArgs(name, bootstrapUserCommand(username), nil),
		strings.NewReader(bootstrapUserScript), &stdout, &stderr)
	if err != nil {
		return fmt.Errorf("running user bootstrap script: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("user bootstrap script exited %d: %s", exitCode, strings.TrimSpace(stderr.String()))
	}
	uid, err := parseBootstrapUID(stdout.String())
	if err != nil {
		return err
	}
	home := "/home/" + username
	if _, _, err := p.run(ctx, buildSetUserConfigArgs(name, username, uid, home)...); err != nil {
		return fmt.Errorf("recording provisioned user: %w", err)
	}
	return nil
}

func parseBootstrapUID(stdout string) (string, error) {
	for _, line := range strings.Split(stdout, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "AGENTCTL_UID="); ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("user bootstrap script did not report AGENTCTL_UID (output: %q)", stdout)
}

// resolveExecUser looks up the non-root identity provisionDefaultUser
// recorded for name, or returns nil (root) if root is requested
// explicitly (--root) or if no provisioned user is recorded — e.g. an
// instance created by an older agentctl build. Never errors just because
// nothing was recorded; only a malformed recorded value is an error.
func (p *Provider) resolveExecUser(ctx context.Context, name string, root bool) (*execUser, error) {
	if root {
		return nil, nil
	}
	stdout, _, err := p.run(ctx, buildGetUserConfigArgs(name)...)
	if err != nil {
		return nil, err
	}
	val := strings.TrimSpace(string(stdout))
	if val == "" {
		return nil, nil
	}
	parts := strings.SplitN(val, ":", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed %s config value %q on instance %q", userConfigKey, val, name)
	}
	return &execUser{uid: parts[1], home: parts[2]}, nil
}

func (p *Provider) Start(ctx context.Context, name string) error {
	_, _, err := p.run(ctx, buildStartArgs(name)...)
	return err
}

func (p *Provider) Stop(ctx context.Context, name string, opts provider.StopOptions) error {
	_, _, err := p.run(ctx, buildStopArgs(name, opts)...)
	return err
}

func (p *Provider) Delete(ctx context.Context, name string, force bool) error {
	_, _, err := p.run(ctx, buildDeleteArgs(name, force)...)
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
	if err := p.waitForAgent(ctx, name, opts.Stderr); err != nil {
		return -1, err
	}
	u, err := p.resolveExecUser(ctx, name, opts.Root)
	if err != nil {
		return -1, fmt.Errorf("resolving default user: %w", err)
	}
	return p.runner.RunStream(ctx, binary, buildExecArgs(name, opts.Command, u), opts.Stdin, opts.Stdout, opts.Stderr)
}

func (p *Provider) Shell(ctx context.Context, name string, opts provider.ShellOptions) error {
	if err := p.waitForAgent(ctx, name, opts.Stderr); err != nil {
		return err
	}
	u, err := p.resolveExecUser(ctx, name, opts.Root)
	if err != nil {
		return fmt.Errorf("resolving default user: %w", err)
	}
	_, err = p.runner.RunStream(ctx, binary, buildShellArgs(name, u), opts.Stdin, opts.Stdout, opts.Stderr)
	return err
}

// waitForAgent blocks until the guest's incus-agent responds to a
// harmless probe command (`incus exec <name> -- true`), or
// agentReadyTimeout elapses. Only View's SPICE console and the
// daemon-side commands (start/stop/list/...) work without the agent;
// Exec and Shell both need it, since `incus exec` is itself the
// agent-mediated channel.
//
// Any probe error other than "agent not running" is returned
// immediately rather than retried, so a genuinely broken request (wrong
// instance name, instance not running at all, ...) fails fast instead of
// spinning for the full timeout. notify, if non-nil, gets a one-line
// heads-up the first time the agent isn't ready yet, so a multi-second
// wait doesn't look like agentctl hanging.
func (p *Provider) waitForAgent(ctx context.Context, name string, notify io.Writer) error {
	deadline := time.Now().Add(agentReadyTimeout)
	notified := false
	var lastErr error
	for {
		_, _, err := p.run(ctx, "exec", name, "--", "true")
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), agentNotRunningMsg) {
			return err
		}
		lastErr = err
		if !notified && notify != nil {
			fmt.Fprintf(notify, "agentctl: waiting for the VM agent inside %q to start...\n", name)
			notified = true
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("VM agent inside %q did not become ready within %s (the image may not include incus-agent support): %w",
				name, agentReadyTimeout, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(agentPollInterval):
		}
	}
}

// View spawns Incus's native SPICE console against the instance's own
// display, never the guest's X server — see docs/user/view-and-console.md
// for why raw X11 forwarding is deliberately excluded from agentctl.
func (p *Provider) View(ctx context.Context, name string, opts provider.ViewOptions) error {
	var stdout, stderr bytes.Buffer
	_, err := p.runner.RunStream(ctx, binary, buildViewArgs(name), nil, &stdout, &stderr)
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
	_, _, _ = p.run(ctx, buildACLDeleteArgs(name)...)
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

func (p *Provider) SetAgentRequested(ctx context.Context, name, agentName string) error {
	_, _, err := p.run(ctx, buildSetConfigArgs(name, agentConfigKey, agentName)...)
	return err
}

func (p *Provider) MarkAgentInstalled(ctx context.Context, name string) error {
	_, _, err := p.run(ctx, buildSetConfigArgs(name, agentInstalledConfigKey, "true")...)
	return err
}

func (p *Provider) PendingAgentInstall(ctx context.Context, name string) (string, error) {
	stdout, _, err := p.run(ctx, buildGetConfigArgs(name, agentConfigKey)...)
	if err != nil {
		return "", err
	}
	requested := strings.TrimSpace(string(stdout))
	if requested == "" {
		return "", nil
	}
	stdout, _, err = p.run(ctx, buildGetConfigArgs(name, agentInstalledConfigKey)...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return "", nil
	}
	return requested, nil
}

func (p *Provider) ImagePull(ctx context.Context, ref string) error {
	_, _, err := p.run(ctx, buildImagePullArgs(ref)...)
	return err
}

func (p *Provider) ImageBuild(ctx context.Context, spec provider.ImageBuildSpec) (string, error) {
	return "", provider.StatusError(p.capabilities.Get(provider.FeatureImageBuild))
}

func (p *Provider) Logs(ctx context.Context, name string, opts provider.LogOptions) (io.ReadCloser, error) {
	return nil, provider.StatusError(p.capabilities.Get(provider.FeatureLogsNetwork))
}
