package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/apomonosi/sandboxing/internal/profile"
	"github.com/apomonosi/sandboxing/internal/provider"
)

// mountFlags is the shared --mount flag set. Both `create` and `start`
// carry it: mounts are a per-boot property, so rebinding a sandbox to a
// different project is a `start` operation, not a reason to build a new
// instance (see filesystem-access.plan.md).
//
// The four flags mirror Lima's own vocabulary — `limactl` spells them
// --mount / --mount-only / --mount-none / --mount-writable with the same
// meanings — so anyone who knows Lima already knows this surface.
type mountFlags struct {
	mounts   []string
	only     []string
	none     bool
	writable bool
}

func (f *mountFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.mounts, "mount", nil,
		"expose a host directory: \"hostPath[:guestPath][:w|:ro]\", read-only unless :w (repeatable)")
	cmd.Flags().StringArrayVar(&f.only, "mount-only", nil,
		"like --mount, but replaces the profile's mounts instead of adding to them (repeatable)")
	cmd.Flags().BoolVar(&f.none, "mount-none", false, "expose no host directories at all")
	cmd.Flags().BoolVar(&f.writable, "mount-writable", false, "make every mount writable")
}

// changed reports whether the user passed any mount flag. `start` uses
// this to decide whether to touch the instance's mount set at all: mounts
// are sticky, so a bare `start` must reuse whatever was last applied
// rather than resetting it.
//
// StringArray flags are checked via Changed rather than by length so that
// an explicitly empty value is still a signal, matching how Cobra reports
// every other flag on these commands.
func (f *mountFlags) changed(cmd *cobra.Command) bool {
	for _, name := range []string{"mount", "mount-only", "mount-none", "mount-writable"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// apply layers the mount flags onto an already-resolved policy.
//
// Precedence follows Lima's: --mount-none wins outright, --mount-only
// replaces the profile's mounts, and a plain --mount accumulates on top of
// them (the same additive rule --allow and --port already use). Only
// --mount-writable is applied afterwards, to whatever survived.
func (f *mountFlags) apply(policy profile.Policy) (profile.Policy, error) {
	switch {
	case f.none:
		policy.Mounts = nil
	case len(f.only) > 0:
		parsed, err := parseMountFlags(f.only)
		if err != nil {
			return policy, err
		}
		policy.Mounts = parsed
	default:
		parsed, err := parseMountFlags(f.mounts)
		if err != nil {
			return policy, err
		}
		policy.Mounts = append(append([]profile.Mount(nil), policy.Mounts...), parsed...)
	}

	if f.writable {
		for i := range policy.Mounts {
			policy.Mounts[i].Writable = true
		}
	}
	return policy, nil
}

func parseMountFlags(specs []string) ([]profile.Mount, error) {
	var out []profile.Mount
	for _, s := range specs {
		m, err := parseMountFlag(s)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// gateMounts runs the capability gate for a resolved mount set: one check
// for "can this backend mount host directories at all", and a second for
// read-only enforcement specifically, which is only consulted when a
// read-only mount was actually asked for.
func gateMounts(w io.Writer, providerName string, p provider.Provider, mounts []provider.Mount, forcePartial bool) error {
	if len(mounts) == 0 {
		return nil
	}
	if err := gateOrBlock(w, providerName, p.Capabilities().Get(provider.FeatureMount), forcePartial); err != nil {
		return err
	}
	for _, m := range mounts {
		if !m.Writable {
			return gateOrBlock(w, providerName, p.Capabilities().Get(provider.FeatureMountReadOnly), forcePartial)
		}
	}
	return nil
}

// renderMounts prints the host directories a sandbox can see. Mounts are
// sticky across boots, so this is deliberately printed on every start
// rather than only when they change — the failure mode worth designing
// against is a bare `agentctl start` months later silently re-exposing a
// directory the user has forgotten about.
//
// jsonOutput suppresses it entirely rather than reformatting it: this is
// commentary printed alongside a command's real output, and emitting it
// before an --output=json document would leave the caller with something
// that isn't valid JSON. Nothing is lost — provider.Instance carries the
// mount set in its own JSON.
func renderMounts(w io.Writer, mounts []provider.Mount, jsonOutput bool) {
	if jsonOutput {
		return
	}
	if len(mounts) == 0 {
		fmt.Fprintln(w, "agentctl: no host directories are exposed to this sandbox.")
		return
	}
	fmt.Fprintln(w, "agentctl: host directories exposed to this sandbox:")
	for _, m := range mounts {
		access := "read-only"
		if m.Writable {
			access = "writable"
		}
		fmt.Fprintf(w, "  %s -> %s (%s)\n", m.HostPath, m.GuestPath, access)
	}
}

// renderPreflight prints any host-configuration warnings the active
// provider reports. Backends that don't implement provider.Preflighter
// are skipped entirely, the same way tryPreview handles CommandPreviewer.
//
// Warnings go to the command's own output rather than stderr so they
// appear in the same stream as the rest of create's commentary, and are
// prefixed like every other agentctl diagnostic.
func renderPreflight(w io.Writer, p provider.Provider, ctx context.Context) {
	pf, ok := p.(provider.Preflighter)
	if !ok {
		return
	}
	for _, warning := range pf.Preflight(ctx) {
		fmt.Fprintf(w, "agentctl: WARNING %s\n", warning)
	}
}
