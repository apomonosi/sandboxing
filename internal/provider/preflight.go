package provider

import "context"

// Preflighter is an optional interface a Provider may implement to report
// host-configuration problems that would otherwise surface much later as
// a confusing failure inside the guest.
//
// Same shape as CommandPreviewer: internal/cli type-asserts for it and
// skips the step entirely for backends that don't implement it, so the
// Provider interface itself stays free of things only one backend needs.
//
// The motivating case is firewalld on Fedora/RHEL leaving the Incus
// bridge unzoned, which blocks DHCP and produces a guest with an IPv6
// address, no IPv4, and no working DNS — a symptom that looks like an
// agentctl network-policy bug and isn't. Diagnosing that by hand takes a
// dozen commands; one line at create time saves all of it.
type Preflighter interface {
	// Preflight returns human-readable warnings about the host, each a
	// complete sentence naming the fix.
	//
	// It returns no error, deliberately. A check that cannot run — the
	// tool isn't installed, the query needs root, the host isn't Linux —
	// has learned nothing, and must stay silent rather than guess. A
	// preflight that cries wolf gets ignored, which costs more than
	// having none at all.
	Preflight(ctx context.Context) []string
}
