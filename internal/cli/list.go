package cli

import (
	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List managed sandbox instances",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureList), forcePartial(cmd)); err != nil {
				return err
			}
			instances, err := p.List(cmd.Context())
			if err != nil {
				return err
			}
			return RenderInstances(cmd.OutOrStdout(), instances, jsonOutput(cmd))
		},
	}
}
