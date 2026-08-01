package cli

import (
	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a stopped sandbox instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureStart), forcePartial(cmd)); err != nil {
				return err
			}
			if handled, err := tryPreview(cmd, providerName, p, func(pv provider.CommandPreviewer) []provider.Command {
				return pv.PreviewStart(args[0])
			}); handled {
				return err
			}
			if err := p.Start(cmd.Context(), args[0]); err != nil {
				return err
			}
			return retryPendingAgentInstall(cmd.Context(), cmd.OutOrStdout(), p, args[0])
		},
	}
}
