package cli

import (
	"context"
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

			root, _ := cmd.Flags().GetBool("root")
			execOpts := provider.ExecOptions{
				Command: command,
				Stdin:   cmd.InOrStdin(),
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
				Root:    root,
			}
			if handled, err := tryPreviewCtx(cmd, providerName, p, func(ctx context.Context, pv provider.CommandPreviewer) ([]provider.Command, error) {
				return pv.PreviewExec(ctx, name, execOpts)
			}); handled {
				return err
			}

			exitCode, err := p.Exec(cmd.Context(), name, execOpts)
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
	addRootFlag(cmd)
	return cmd
}
