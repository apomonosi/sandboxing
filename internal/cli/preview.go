package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// previewRequested reports whether --preview or its --dry-run alias was
// passed. Both flags do the same thing; --dry-run exists because that's
// the more familiar term for some users.
func previewRequested(cmd *cobra.Command) bool {
	p, _ := cmd.Flags().GetBool("preview")
	d, _ := cmd.Flags().GetBool("dry-run")
	return p || d
}

// tryPreview is the standard call site for every command that shells out
// to the backend: called after the capability gate has already passed
// (so preview only ever runs for an operation the active provider
// actually supports), it checks whether --preview/--dry-run was
// requested and, if so, prints the command(s) buildFn describes instead
// of performing them, then returns handled=true.
//
// If the provider doesn't implement provider.CommandPreviewer (Lima and
// Hyper-V, currently — stubs with nothing real to preview), this prints a
// short, honest notice rather than silently doing nothing or fabricating
// output — consistent with agentctl never pretending a gap isn't there.
func tryPreview(cmd *cobra.Command, providerName string, p provider.Provider, buildFn func(provider.CommandPreviewer) []provider.Command) (handled bool, err error) {
	if !previewRequested(cmd) {
		return false, nil
	}
	w := cmd.OutOrStdout()
	previewer, ok := p.(provider.CommandPreviewer)
	if !ok {
		fmt.Fprintf(w, "agentctl: --preview is not yet supported for provider %q (no command-preview implementation exists yet)\n", providerName)
		return true, nil
	}
	for _, c := range buildFn(previewer) {
		fmt.Fprintln(w, c.String())
	}
	return true, nil
}

// tryPreviewCtx is tryPreview's counterpart for the two
// CommandPreviewer methods that need a live lookup and can therefore
// fail — PreviewExec and PreviewShell, which resolve the instance's
// provisioned non-root user via a real `incus config get` call so the
// previewed --user/--cwd flags match what will actually run. See the
// doc comment on provider.CommandPreviewer for why only these two are
// shaped this way.
func tryPreviewCtx(cmd *cobra.Command, providerName string, p provider.Provider, buildFn func(context.Context, provider.CommandPreviewer) ([]provider.Command, error)) (handled bool, err error) {
	if !previewRequested(cmd) {
		return false, nil
	}
	w := cmd.OutOrStdout()
	previewer, ok := p.(provider.CommandPreviewer)
	if !ok {
		fmt.Fprintf(w, "agentctl: --preview is not yet supported for provider %q (no command-preview implementation exists yet)\n", providerName)
		return true, nil
	}
	cmds, err := buildFn(cmd.Context(), previewer)
	if err != nil {
		return true, err
	}
	for _, c := range cmds {
		fmt.Fprintln(w, c.String())
	}
	return true, nil
}
