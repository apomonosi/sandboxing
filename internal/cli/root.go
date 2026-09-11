// Package cli builds agentctl's Cobra command tree. It is kept separate
// from cmd/agentctl/main.go so the whole tree is importable and testable
// (via NewRootCmd(registry) with a fake registry) without os.Exit getting
// in the way.
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/config"
	"github.com/apomonosi/sandboxing/internal/profile"
	"github.com/apomonosi/sandboxing/internal/provider"
)

type ctxKey int

const (
	ctxRegistry ctxKey = iota
	ctxConfig
)

// NewRootCmd builds the full agentctl command tree, dispatching through
// reg. Production wiring passes a registry with the real incus/lima/hyperv
// factories (see cmd/agentctl/main.go); tests pass one seeded with
// provider/fake instead.
func NewRootCmd(reg *provider.Registry) *cobra.Command {
	root := &cobra.Command{
		Use:   "agentctl",
		Short: "Create and manage hardened, ephemeral sandboxes for running AI agents safely",
		Long: `agentctl creates isolated, hardened, ephemeral VMs/containers ("sandboxes")
for running AI agents, defending against both host escape and lateral
attacks against other machines on the same network.

It dispatches to an OS-native backend (Incus on Linux, Lima on macOS,
Hyper-V on Windows) selected once via "agentctl config set provider=...".
Not every feature is available on every backend yet; agentctl always says
so explicitly instead of failing with a raw error — see
"agentctl profile --help" and the capability matrix in the docs.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			ctx := context.WithValue(cmd.Context(), ctxRegistry, reg)
			ctx = context.WithValue(ctx, ctxConfig, cfg)
			cmd.SetContext(ctx)
			return nil
		},
	}

	root.PersistentFlags().String("provider", "", "override the configured provider backend (incus|lima|hyperv)")
	root.PersistentFlags().String("output", "table", "output format: table|json")
	root.PersistentFlags().Bool("force-partial", false, "proceed past a manual-workaround capability gap, accepting reduced protection")
	root.PersistentFlags().String("profile-dir", "", "override the configured org-distributed profile directory")
	root.PersistentFlags().Bool("preview", false, "print the backend command(s) this would run instead of running them")
	root.PersistentFlags().Bool("dry-run", false, "alias of --preview")

	root.AddCommand(
		newCreateCmd(),
		newStartCmd(),
		newStopCmd(),
		newDeleteCmd(),
		newListCmd(),
		newStatusCmd(),
		newShellCmd(),
		newLoginAliasCmd(),
		newExecCmd(),
		newViewCmd(),
		newLogsCmd(),
		newImageCmd(),
		newProfileCmd(),
		newConfigCmd(),
	)
	return root
}

func registryFromContext(ctx context.Context) *provider.Registry {
	return ctx.Value(ctxRegistry).(*provider.Registry)
}

func configFromContext(ctx context.Context) *config.Config {
	return ctx.Value(ctxConfig).(*config.Config)
}

// resolveProvider resolves the active provider for cmd: --provider flag,
// falling back to the persisted config. Commands that don't need a
// backend at all (config, profile validate) skip calling this.
func resolveProvider(cmd *cobra.Command) (provider.Provider, string, error) {
	ctx := cmd.Context()
	reg := registryFromContext(ctx)
	cfg := configFromContext(ctx)

	name, _ := cmd.Flags().GetString("provider")
	if name == "" {
		name = cfg.Provider
	}
	if name == "" {
		return nil, "", fmt.Errorf("no provider configured; run `agentctl config set provider=<incus|lima|hyperv>` or pass --provider")
	}
	p, err := reg.Get(name)
	if err != nil {
		return nil, name, err
	}
	return p, name, nil
}

// resolveProfileNames returns the profiles a command should apply, in
// precedence order: explicit --profile flags, else the configured
// defaultProfile, else the built-in "default".
//
// That last fallback is the important one. Without it, a create with no
// --profile and no configured defaultProfile resolved against an empty
// policy: no resource limits, and — since the built-in profiles are where
// the per-instance workspace mount is declared — no host directory at
// all. An agent installed via --agent would come up with nowhere to work.
// Falling back to "default" is not a widening: it is default-deny egress
// with LAN blocked, so it constrains an instance that previously had no
// policy attached.
//
// A file-based profile named "default" in the search paths still wins
// over the built-in, since this only chooses a name and LoadNamed
// resolves it (see profile.SearchPaths).
func resolveProfileNames(cmd *cobra.Command, explicit []string) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if def := configFromContext(cmd.Context()).DefaultProfile; def != "" {
		return []string{def}
	}
	return []string{profile.Default}
}

func jsonOutput(cmd *cobra.Command) bool {
	out, _ := cmd.Flags().GetString("output")
	return out == "json"
}

func forcePartial(cmd *cobra.Command) bool {
	fp, _ := cmd.Flags().GetBool("force-partial")
	return fp
}

func profileDirFlag(cmd *cobra.Command) string {
	dir, _ := cmd.Flags().GetString("profile-dir")
	if dir != "" {
		return dir
	}
	return configFromContext(cmd.Context()).ProfileDir
}
