package cli

import (
	"fmt"
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
			opts := provider.StopOptions{
				Force:   force,
				Timeout: time.Duration(timeoutSeconds) * time.Second,
			}
			if handled, err := tryPreview(cmd, providerName, p, func(pv provider.CommandPreviewer) []provider.Command {
				return pv.PreviewStop(args[0], opts)
			}); handled {
				return err
			}
			// Stopping a stopped instance is not an error, but both
			// backends' stop commands treat it as one (Lima: "expected
			// status `Running`, got `Stopped`"). --force goes straight to
			// the backend, as before.
			if !force {
				if inst, err := p.Status(cmd.Context(), args[0]); err == nil && inst.Status == provider.StatusStopped {
					if !jsonOutput(cmd) {
						fmt.Fprintf(cmd.OutOrStdout(), "agentctl: %s is already stopped\n", args[0])
					}
					return nil
				}
			}
			return p.Stop(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "force stop without a graceful shutdown")
	cmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "seconds to wait for graceful shutdown before giving up")
	return cmd
}
