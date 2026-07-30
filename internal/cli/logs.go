package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newLogsCmd() *cobra.Command {
	var network, execLogs, follow bool

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show audit-relevant logs for a sandbox instance",
		Long: `Shows network and/or exec activity for an instance, using whatever the
active provider natively exposes. Structured, correlated audit tooling
(eBPF/ETW-based observability) is a separate, larger effort and isn't
built yet — see docs/admin/security-model.md.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			fp := forcePartial(cmd)
			w := cmd.OutOrStdout()
			if network || (!network && !execLogs) {
				if err := gateOrBlock(w, providerName, p.Capabilities().Get(provider.FeatureLogsNetwork), fp); err != nil {
					return err
				}
			}
			if execLogs {
				if err := gateOrBlock(w, providerName, p.Capabilities().Get(provider.FeatureLogsExec), fp); err != nil {
					return err
				}
			}
			reader, err := p.Logs(cmd.Context(), args[0], provider.LogOptions{Network: network, Exec: execLogs, Follow: follow})
			if err != nil {
				return err
			}
			defer reader.Close()
			_, err = io.Copy(w, reader)
			return err
		},
	}
	cmd.Flags().BoolVar(&network, "network", false, "show network egress attempts (default when no filter is given)")
	cmd.Flags().BoolVar(&execLogs, "exec", false, "show commands executed inside the instance")
	cmd.Flags().BoolVar(&follow, "follow", false, "stream new log entries as they occur")
	return cmd
}
