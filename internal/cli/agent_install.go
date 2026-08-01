package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/apomonosi/sandboxing/internal/agent"
	"github.com/apomonosi/sandboxing/internal/provider"
)

// installAgent runs spec's install script inside name via Exec (which
// already waits for the in-guest agent to be ready), then marks it done
// via Provider.MarkAgentInstalled. Used both by create.go's initial
// install and start.go's retry-on-pending path, so the two can't drift.
func installAgent(ctx context.Context, w io.Writer, p provider.Provider, name string, spec agent.Spec) error {
	fmt.Fprintf(w, "agentctl: installing %s...\n", spec.Name)
	code, err := p.Exec(ctx, name, provider.ExecOptions{
		Command: []string{"sh", "-c", spec.InstallScript},
		Stdout:  w,
		Stderr:  w,
	})
	if err != nil {
		return fmt.Errorf("installing agent %q: %w", spec.Name, err)
	}
	if code != 0 {
		return fmt.Errorf("installing agent %q: install script exited %d", spec.Name, code)
	}
	return p.MarkAgentInstalled(ctx, name)
}

// retryPendingAgentInstall checks whether a prior `create --agent=...`
// left an install requested-but-not-finished (e.g. a transient network
// failure mid-install) and retries it via installAgent. A no-op for any
// instance that never had --agent, or whose install already succeeded.
func retryPendingAgentInstall(ctx context.Context, w io.Writer, p provider.Provider, name string) error {
	pending, err := p.PendingAgentInstall(ctx, name)
	if err != nil {
		return fmt.Errorf("checking pending agent install: %w", err)
	}
	if pending == "" {
		return nil
	}
	spec, ok := agent.Lookup(pending)
	if !ok {
		// The marker refers to an agent no longer in the registry (e.g.
		// an older agentctl build); nothing sane to retry.
		return nil
	}
	fmt.Fprintf(w, "agentctl: retrying incomplete install of %s...\n", spec.Name)
	return installAgent(ctx, w, p, name, spec)
}
