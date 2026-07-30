package cli

import (
	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newViewCmd() *cobra.Command {
	var readOnly bool

	cmd := &cobra.Command{
		Use:   "view <name>",
		Short: "Open a console/display viewer against a sandbox instance",
		Long: `Opens a viewer against the sandbox's own display (SPICE on Incus,
VMConnect on Hyper-V). agentctl deliberately never forwards raw X11: an
X server has no isolation between clients, so a compromised agent could
keylog or screenshot anything else running on the same X server. See
docs/user/view-and-console.md for the full rationale.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			if err := gateOrBlock(cmd.OutOrStdout(), providerName, p.Capabilities().Get(provider.FeatureView), forcePartial(cmd)); err != nil {
				return err
			}
			return p.View(cmd.Context(), args[0], provider.ViewOptions{ReadOnly: readOnly})
		},
	}
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "open the viewer in read-only mode if the provider supports it")
	return cmd
}
