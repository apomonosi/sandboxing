// Package audit defines the record types and LogSource interface behind
// `agentctl logs`. Every provider's Logs() method currently returns
// provider.ErrUnderDevelopment (see each provider's capability table for
// FeatureLogsNetwork/FeatureLogsExec) — structured collection from
// provider-native audit trails (e.g. Incus ACL deny logs) is explicitly
// out of scope for this milestone. This package exists so that future
// work has a stable type to implement against without reshaping
// internal/cli/logs.go when it lands.
package audit

import "time"

// Kind distinguishes the two categories `agentctl logs` filters on
// (--network / --exec).
type Kind string

const (
	KindNetwork Kind = "network"
	KindExec    Kind = "exec"
)

// Event is one audit-relevant record: a network egress attempt (allowed or
// blocked) or a command execution inside a sandbox.
type Event struct {
	Kind      Kind
	Timestamp time.Time
	Instance  string
	Summary   string // e.g. "blocked egress to 93.184.216.34:443" or "exec: curl -s https://example.com"
	Allowed   bool   // meaningful for KindNetwork only
}

// Source produces Events for one instance. A future provider-native
// implementation (e.g. parsing Incus ACL deny logs) will implement this;
// none exists yet this milestone.
type Source interface {
	Events(instance string, kinds []Kind) ([]Event, error)
}
