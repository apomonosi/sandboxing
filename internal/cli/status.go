package cli

import (
	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <name>",
		Short: "Show detailed status for a sandbox instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureStatus), forcePartial(cmd)); err != nil {
				return err
			}
			inst, err := p.Status(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return RenderInstance(cmd.OutOrStdout(), inst, jsonOutput(cmd))
		},
	}
}
