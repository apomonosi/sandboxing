package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newStopCmd() *cobra.Command {
	var force bool
	var timeoutSeconds int

	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a running sandbox instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureStop), forcePartial(cmd)); err != nil {
				return err
			}
			return p.Stop(cmd.Context(), args[0], provider.StopOptions{
				Force:   force,
				Timeout: time.Duration(timeoutSeconds) * time.Second,
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "force stop without a graceful shutdown")
	cmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "seconds to wait for graceful shutdown before giving up")
	return cmd
}
