package cli

import (
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
	return p.Shell(cmd.Context(), args[0], provider.ShellOptions{
		Stdin:  cmd.InOrStdin(),
		Stdout: cmd.OutOrStdout(),
		Stderr: cmd.ErrOrStderr(),
	})
}

func newShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell <name>",
		Short: "Open an interactive shell inside a sandbox instance",
		Args:  cobra.ExactArgs(1),
		RunE:  shellRunE,
	}
}

// newLoginAliasCmd registers "login" as a hidden alias of "shell": early
// design used that verb, but it reads as credential auth against a
// remote account (docker login, gh auth login) rather than "open a shell
// in an existing sandbox", so it's kept working but out of --help.
func newLoginAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "login <name>",
		Short:  "Alias of \"shell\"",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE:   shellRunE,
	}
}
