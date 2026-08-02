package lima

import (
	"context"

	"github.com/apomonosi/sandboxing/internal/provider"
)

var _ provider.CommandPreviewer = (*Provider)(nil)

// This file implements provider.CommandPreviewer for --preview/--dry-run.
// Every method here builds its Commands from the exact same translate.go
// helper functions the real Provider methods in lima.go call — never a
// hand-duplicated copy — so preview output can't drift from what
// actually executes. Mirrors internal/provider/incus/preview.go's role.

func cmd(args []string) provider.Command {
	return provider.Command{Binary: binary, Args: args}
}

// PreviewCreate shows the single `limactl create` command Create() would
// run. Unlike Incus's PreviewCreate, there's no multi-step start/
// bootstrap/stop sequence to show — a direct consequence of Create()
// never starting the instance (see lima.go's Create doc comment).
func (p *Provider) PreviewCreate(spec provider.InstanceSpec) []provider.Command {
	return []provider.Command{cmd(buildCreateArgs(spec))}
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

// PreviewExec and PreviewShell keep the ctx/error signature
// provider.CommandPreviewer requires, but — unlike Incus, whose preview
// needs a live `config get` to resolve which non-root user to show —
// neither uses ctx or can actually fail: Lima's shell already resolves to
// the provisioned guest user on its own, with no readback step to
// preview. The parameters exist purely to satisfy the shared interface
// shape.
func (p *Provider) PreviewExec(ctx context.Context, name string, opts provider.ExecOptions) ([]provider.Command, error) {
	return []provider.Command{cmd(buildExecArgs(name, opts.Command, opts.Root))}, nil
}

func (p *Provider) PreviewShell(ctx context.Context, name string, opts provider.ShellOptions) ([]provider.Command, error) {
	return []provider.Command{cmd(buildShellArgs(name, opts.Root))}, nil
}

// PreviewView and PreviewImagePull return nil: both are unreachable in
// practice, since internal/cli's capability gate already blocks view/
// image pull before --preview is ever consulted for a feature that isn't
// Supported (see provider.CommandPreviewer's own doc comment). Building
// speculative preview output for a feature that doesn't exist yet would
// violate the same "never fabricate output for a gap" principle the rest
// of the codebase follows.
func (p *Provider) PreviewView(name string, opts provider.ViewOptions) []provider.Command {
	return nil
}

func (p *Provider) PreviewImagePull(ref string) []provider.Command {
	return nil
}
