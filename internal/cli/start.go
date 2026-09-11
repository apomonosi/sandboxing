package cli

import (
	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/profile"
	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/spec"
)

func newStartCmd() *cobra.Command {
	var (
		profiles []string
		mounts   mountFlags
	)

	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start a stopped sandbox instance",
		Long: `Start a stopped sandbox instance.

Passing any --mount flag rebinds which host directories the sandbox can
see, for this boot and every later one, without recreating the instance:

  agentctl start work --mount ~/src/alpha:/workspace:w
  agentctl stop work
  agentctl start work --mount ~/src/beta:/workspace:w

Mounts are sticky, so a bare "agentctl start" reuses whatever was applied
last — it never silently drops or widens them. The active set is printed
on every start.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			p, providerName, err := resolveProvider(cmd)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fp := forcePartial(cmd)
			if err := gateOrBlock(w, providerName, p.Capabilities().Get(provider.FeatureStart), fp); err != nil {
				return err
			}

			// Only touch the mount set when the user actually asked to.
			// Mounts persist in the backend's own state (Lima's instance
			// YAML, Incus's device list), so re-applying a freshly
			// resolved set on every bare start would silently reset an
			// instance to its profile's defaults.
			if mounts.changed(cmd) {
				resolved, err := resolveStartMounts(cmd, name, profiles, mounts, previewRequested(cmd))
				if err != nil {
					return err
				}
				if err := gateMounts(w, providerName, p, resolved, fp); err != nil {
					return err
				}
				// Applying the mount set is a real mutation, so it is
				// skipped under --preview along with the start itself.
				if !previewRequested(cmd) {
					if err := p.ApplyMountPolicy(cmd.Context(), name, resolved); err != nil {
						return err
					}
				}
			}

			if handled, err := tryPreview(cmd, providerName, p, func(pv provider.CommandPreviewer) []provider.Command {
				return pv.PreviewStart(name)
			}); handled {
				return err
			}
			if err := p.Start(cmd.Context(), name); err != nil {
				return err
			}
			if inst, err := p.Status(cmd.Context(), name); err == nil {
				renderMounts(w, inst.Mounts, jsonOutput(cmd))
			}
			return retryPendingAgentInstall(cmd.Context(), w, p, name)
		},
	}

	cmd.Flags().StringSliceVar(&profiles, "profile", nil,
		"named security profile(s) whose mountPolicy constrains --mount (repeatable)")
	mounts.register(cmd)
	return cmd
}

// resolveStartMounts resolves the mount set a --mount-carrying `start`
// should apply. It deliberately ignores the profile's own `mounts` list
// and keeps only its `mountPolicy`: the profile's declared mounts belong
// to `create`, whereas `start` is naming directories for this boot, and
// silently re-adding a profile's workspace mount alongside them would make
// --mount-only and --mount-none meaningless here.
//
// The constraint half of the profile still applies in full, so allowed
// roots and deny paths are re-checked on every start — a profile tightened
// after the instance was created takes effect at the next boot rather than
// being frozen at create time.
func resolveStartMounts(cmd *cobra.Command, name string, profiles []string, flags mountFlags, preview bool) ([]provider.Mount, error) {
	policy, err := spec.ResolvePolicy(resolveProfileNames(cmd, profiles), profileDirFlag(cmd), profile.Policy{})
	if err != nil {
		return nil, err
	}
	policy.Mounts = nil

	policy, err = flags.apply(policy)
	if err != nil {
		return nil, err
	}
	if preview {
		return profile.ResolveMountsPreview(policy.Mounts, name, policy.MountPolicy)
	}
	return profile.ResolveMounts(policy.Mounts, name, policy.MountPolicy)
}
