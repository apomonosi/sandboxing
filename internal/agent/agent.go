// Package agent holds agentctl's static registry of coding agents that
// can be just-in-time installed into a freshly created sandbox via
// `agentctl create --agent=<name>`, instead of hand-typing an install
// command after `shell`-ing in. Shape mirrors internal/image: a small,
// static table, no external registry lookups.
package agent

import (
	"sort"

	"github.com/apomonosi/sandboxing/internal/profile"
)

// Spec describes one installable coding agent.
type Spec struct {
	Name string
	// InstallScript is a full shell command line (piped to `sh -c`, not
	// stdin) — each agent's own documented one-liner, verbatim.
	InstallScript string
	// EnvVar is the host API key environment variable this agent expects,
	// where the project has one obvious default. Unused until a future
	// mechanism forwards host env vars into the guest (out of scope for
	// this milestone); empty for agents with no single default provider.
	EnvVar string
	// AllowDomains are the extra egress allowlist entries this agent's
	// install and/or runtime needs, merged into the instance's network
	// policy by `create --agent=<name>` before the ACL is applied.
	AllowDomains []profile.AllowRule
}

// Registry is the static table of installable agents, keyed by the name
// passed to --agent. Values verified directly from each project's own
// docs (not reconstructed from memory):
//   - claude:   https://code.claude.com/docs/en/quickstart
//   - codex:    https://learn.chatgpt.com/docs/codex/cli
//   - opencode: https://opencode.ai/download
//   - pi:       https://pi.dev/
//
// opencode and pi are explicitly multi-provider: their AllowDomains only
// guarantee the install domain (opencode) or the install domain plus the
// default-provider runtime domain (pi, which defaults to Anthropic but
// supports others) — a user on a different provider extends the
// allowlist the same way they would for anything else, via --allow.
var Registry = map[string]Spec{
	"claude": {
		Name:          "claude",
		InstallScript: "curl -fsSL https://claude.ai/install.sh | bash",
		EnvVar:        "ANTHROPIC_API_KEY",
		AllowDomains: []profile.AllowRule{
			{Domain: "claude.ai", Ports: []int{443}},
			{Domain: "*.anthropic.com", Ports: []int{443}},
		},
	},
	"codex": {
		Name:          "codex",
		InstallScript: "curl -fsSL https://chatgpt.com/codex/install.sh | sh",
		EnvVar:        "OPENAI_API_KEY",
		AllowDomains: []profile.AllowRule{
			{Domain: "chatgpt.com", Ports: []int{443}},
			{Domain: "api.openai.com", Ports: []int{443}},
		},
	},
	"opencode": {
		Name:          "opencode",
		InstallScript: "curl -fsSL https://opencode.ai/install | bash",
		EnvVar:        "",
		AllowDomains: []profile.AllowRule{
			{Domain: "opencode.ai", Ports: []int{443}},
		},
	},
	"pi": {
		Name:          "pi",
		InstallScript: "curl -fsSL https://pi.dev/install.sh | sh",
		EnvVar:        "ANTHROPIC_API_KEY",
		AllowDomains: []profile.AllowRule{
			{Domain: "pi.dev", Ports: []int{443}},
			{Domain: "*.anthropic.com", Ports: []int{443}},
		},
	},
}

// Lookup returns the Spec for name and whether it was found.
func Lookup(name string) (Spec, bool) {
	s, ok := Registry[name]
	return s, ok
}

// Names returns every valid --agent value, sorted, for error messages.
func Names() []string {
	names := make([]string, 0, len(Registry))
	for n := range Registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
