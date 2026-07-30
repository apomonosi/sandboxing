package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage agentctl's persistent configuration",
	}
	cmd.AddCommand(newConfigSetCmd(), newConfigGetCmd())
	return cmd
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key>=<value>",
		Short: "Set a configuration value (provider, profileDir, defaultProfile)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value, ok := strings.Cut(args[0], "=")
			if !ok {
				return fmt.Errorf("expected <key>=<value>, got %q", args[0])
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			switch key {
			case "provider":
				if value != "incus" && value != "lima" && value != "hyperv" {
					return fmt.Errorf("unknown provider %q (want incus, lima, or hyperv)", value)
				}
				cfg.Provider = value
			case "profileDir":
				cfg.ProfileDir = value
			case "defaultProfile":
				cfg.DefaultProfile = value
			default:
				return fmt.Errorf("unknown config key %q (want provider, profileDir, or defaultProfile)", key)
			}
			return config.Save(cfg)
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [key]",
		Short: "Print configuration values, or one key",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(args) == 0 {
				fmt.Fprintf(w, "provider=%s\n", cfg.Provider)
				fmt.Fprintf(w, "profileDir=%s\n", cfg.ProfileDir)
				fmt.Fprintf(w, "defaultProfile=%s\n", cfg.DefaultProfile)
				return nil
			}
			switch args[0] {
			case "provider":
				fmt.Fprintln(w, cfg.Provider)
			case "profileDir":
				fmt.Fprintln(w, cfg.ProfileDir)
			case "defaultProfile":
				fmt.Fprintln(w, cfg.DefaultProfile)
			default:
				return fmt.Errorf("unknown config key %q", args[0])
			}
			return nil
		},
	}
}
