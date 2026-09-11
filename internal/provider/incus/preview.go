package incus

import (
	"context"
	"fmt"

	"github.com/apomonosi/sandboxing/internal/provider"
)

var _ provider.CommandPreviewer = (*Provider)(nil)

// This file implements provider.CommandPreviewer for --preview/--dry-run.
// Every method here builds its Commands from the exact same
// translate.go helper functions the real Provider methods in incus.go
// call — never a hand-duplicated copy — so preview output can't drift
// from what actually executes.

func cmd(args []string) provider.Command {
	return provider.Command{Binary: binary, Args: args}
}

// PreviewCreate lists every command Create would run, in order: the
// instance init, an optional root-disk resize, one device-add per mount
// and per published port, the network-policy application sequence (see
// previewNetworkPolicy), a start (incus exec requires a running instance),
// the user-bootstrap invocation, and a forceful stop back down — Create()
// starts the instance only long enough to provision the default user, then
// stops it again so `create` keeps its documented "doesn't leave the
// instance running" contract. It deliberately does not include three
// things Create() also does: the invisible waitForAgent retry wait after
// starting (a retry loop of unknown length, not a fixed command, same gap
// as Exec/Shell's own preview), the trailing `incus list` status query (a
// read, not part of "achieving the sandbox goal"), and the `config set`
// call that records the bootstrap script's resulting UID — that value
// only exists after actually running the (mutating) bootstrap script, so
// it can't be shown without lying about it.
func (p *Provider) PreviewCreate(spec provider.InstanceSpec) []provider.Command {
	var cmds []provider.Command
	cmds = append(cmds, cmd(append([]string{"init"}, buildInitArgs(spec)...)))

	if args := buildRootDiskResizeArgs(spec.Name, spec.Resources.DiskSize); args != nil {
		cmds = append(cmds, cmd(args))
	}
	for _, m := range spec.Mounts {
		cmds = append(cmds, cmd(buildMountDeviceArgs(spec.Name, m)))
	}
	for i, pp := range spec.Overrides.Ports {
		cmds = append(cmds, cmd(buildPortProxyDeviceArgs(spec.Name, fmt.Sprintf("port%d", i), pp)))
	}
	cmds = append(cmds, p.previewNetworkPolicy(spec.Name, spec.Overrides)...)
	cmds = append(cmds, cmd(buildStartArgs(spec.Name)))

	username := spec.DefaultUser
	if username == "" {
		username = defaultUsername
	}
	cmds = append(cmds, cmd(buildExecArgs(spec.Name, bootstrapUserCommand(username), nil)))
	cmds = append(cmds, cmd(buildStopArgs(spec.Name, provider.StopOptions{Force: true})))

	return cmds
}

// previewNetworkPolicy mirrors ApplyNetworkPolicy's own command sequence:
// a best-effort ACL delete (ignored if it fails for real, since the ACL
// may not exist yet) followed by the create/rule/attach sequence from
// networkACLCommands. Domain-based allow rules are resolved via a real
// (read-only) DNS lookup, same as the live path, so the preview shows the
// actual IPs that would be allow-listed right now.
func (p *Provider) previewNetworkPolicy(name string, policy provider.NetworkPolicy) []provider.Command {
	// Unrestricted means Create runs none of this, so preview must show
	// none of it either — preview that lies about what executes is worse
	// than no preview at all.
	if policy.Unrestricted {
		return nil
	}
	resolved := resolveAllowRules(policy.Allow)
	cmds := []provider.Command{cmd(buildACLDeleteArgs(name))}
	for _, args := range networkACLCommands(name, policy, resolved) {
		cmds = append(cmds, cmd(args))
	}
	return cmds
}

func (p *Provider) PreviewStart(name string) []provider.Command {
	return []provider.Command{cmd(buildStartArgs(name))}
}

func (p *Provider) PreviewStop(name string, opts provider.StopOptions) []provider.Command {
	return []provider.Command{cmd(buildStopArgs(name, opts))}
}

func (p *Provider) PreviewDelete(name string, force bool) []provider.Command {
	return []provider.Command{cmd(buildDeleteArgs(name, force))}
}

// PreviewExec and PreviewShell show only the terminal command — not the
// bounded, invisible incus-agent readiness wait the real Exec/Shell
// methods do first (see waitForAgent in incus.go). That wait is a retry
// loop of unknown length depending on the guest's current boot state,
// not a fixed command sequence, so there's nothing deterministic to show
// for it here; the command below is what actually runs once the agent
// responds.
//
// Both call resolveExecUser — the same live, read-only config lookup the
// real Exec/Shell use — so the previewed --user/--cwd flags are the ones
// that will actually be used, not a guess. That's also why these two
// (uniquely among CommandPreviewer's methods) take a context and can
// fail: the lookup is a real `incus config get` call.
func (p *Provider) PreviewExec(ctx context.Context, name string, opts provider.ExecOptions) ([]provider.Command, error) {
	u, err := p.resolveExecUser(ctx, name, opts.Root)
	if err != nil {
		return nil, fmt.Errorf("resolving default user: %w", err)
	}
	return []provider.Command{cmd(buildExecArgs(name, opts.Command, u))}, nil
}

func (p *Provider) PreviewShell(ctx context.Context, name string, opts provider.ShellOptions) ([]provider.Command, error) {
	u, err := p.resolveExecUser(ctx, name, opts.Root)
	if err != nil {
		return nil, fmt.Errorf("resolving default user: %w", err)
	}
	return []provider.Command{cmd(buildShellArgs(name, u))}, nil
}

func (p *Provider) PreviewView(name string, opts provider.ViewOptions) []provider.Command {
	return []provider.Command{cmd(buildViewArgs(name))}
}

func (p *Provider) PreviewImagePull(ref string) []provider.Command {
	return []provider.Command{cmd(buildImagePullArgs(ref))}
}
