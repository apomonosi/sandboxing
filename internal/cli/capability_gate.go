package cli

import (
	"fmt"
	"io"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// GateResult is what CapabilityGate decided.
type GateResult int

const (
	GateProceed GateResult = iota
	GateBlocked
)

// CapabilityGate is the single place that formats agentctl's response to
// a non-Supported capability, so every command's messaging, and its exit
// code, is consistent. It is a pure function (io.Writer in, no globals),
// trivially unit-tested for all four Status values without Cobra or a
// real provider involved.
func CapabilityGate(w io.Writer, providerName string, cap provider.Capability, forcePartial bool) (GateResult, int) {
	switch cap.Status {
	case provider.Supported:
		return GateProceed, 0

	case provider.UnderDevelopment:
		fmt.Fprintf(w, "agentctl: %s is under development for provider %q.\n%s\n", cap.Feature, providerName, cap.Message)
		if cap.TrackingURL != "" {
			fmt.Fprintf(w, "Track progress: %s\n", cap.TrackingURL)
		}
		return GateBlocked, 2

	case provider.NotAvailable:
		fmt.Fprintf(w, "agentctl: %s is not available on provider %q: %s\n", cap.Feature, providerName, cap.Message)
		return GateBlocked, 2

	case provider.ManualWorkaround:
		fmt.Fprintf(w, "agentctl: %s has no native support on provider %q.\n%s\n\n%s\n", cap.Feature, providerName, cap.Message, cap.Plan)
		if forcePartial {
			fmt.Fprintln(w, "Continuing without this protection because --force-partial was set.")
			return GateProceed, 0
		}
		fmt.Fprintln(w, "Re-run with --force-partial to proceed without this protection, after applying the manual workaround above.")
		return GateBlocked, 3

	default:
		fmt.Fprintf(w, "agentctl: unknown capability status for %s on provider %q\n", cap.Feature, providerName)
		return GateBlocked, 1
	}
}

// gateOrBlock is the standard call site: check cap, print via
// CapabilityGate, and return a ready-to-propagate *ExitError if blocked.
func gateOrBlock(w io.Writer, providerName string, cap provider.Capability, forcePartial bool) error {
	result, code := CapabilityGate(w, providerName, cap, forcePartial)
	if result == GateBlocked {
		return &ExitError{Code: code, Err: fmt.Errorf("%s blocked on provider %q", cap.Feature, providerName)}
	}
	return nil
}
