package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func shellRunE(cmd *cobra.Command, args []string) error {
	p, providerName, err := resolveProvider(cmd)
	if err != nil {
		return err
	}
	if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureShell), forcePartial(cmd)); err != nil {
		return err
	}
	root, _ := cmd.Flags().GetBool("root")
	shellOpts := provider.ShellOptions{
		Stdin:  cmd.InOrStdin(),
		Stdout: cmd.OutOrStdout(),
		Stderr: cmd.ErrOrStderr(),
		Root:   root,
	}
	if handled, err := tryPreviewCtx(cmd, providerName, p, func(ctx context.Context, pv provider.CommandPreviewer) ([]provider.Command, error) {
		return pv.PreviewShell(ctx, args[0], shellOpts)
	}); handled {
		return err
	}
	return p.Shell(cmd.Context(), args[0], shellOpts)
}

func addRootFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("root", false, "run as root instead of the instance's provisioned non-root default user")
}

func newShellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell <name>",
		Short: "Open an interactive shell inside a sandbox instance",
		Args:  cobra.ExactArgs(1),
		RunE:  shellRunE,
	}
	addRootFlag(cmd)
	return cmd
}

// newLoginAliasCmd registers "login" as a hidden alias of "shell": early
// design used that verb, but it reads as credential auth against a
// remote account (docker login, gh auth login) rather than "open a shell
// in an existing sandbox", so it's kept working but out of --help.
func newLoginAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "login <name>",
		Short:  "Alias of \"shell\"",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE:   shellRunE,
	}
	addRootFlag(cmd)
	return cmd
}
