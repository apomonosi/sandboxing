package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <name> -- <command...>",
		Short: "Run a one-shot command inside a sandbox instance",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			command := args[1:]
			if command[0] == "--" {
				command = command[1:]
			}
			if len(command) == 0 {
				return fmt.Errorf("exec requires a command after --")
			}

			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureExec), forcePartial(cmd)); err != nil {
				return err
			}

			exitCode, err := p.Exec(cmd.Context(), name, provider.ExecOptions{
				Command: command,
				Stdin:   cmd.InOrStdin(),
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			if exitCode != 0 {
				return &ExitError{Code: exitCode, Err: fmt.Errorf("command exited with status %d", exitCode)}
			}
			return nil
		},
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}
