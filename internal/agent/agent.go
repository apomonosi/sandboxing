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
//   - gemini:   https://github.com/google-gemini/gemini-cli
//   - cursor:   https://cursor.com/docs/cli/installation
//
// opencode and pi are explicitly multi-provider: their AllowDomains only
// guarantee the install domain (opencode) or the install domain plus the
// default-provider runtime domain (pi, which defaults to Anthropic but
// supports others) — a user on a different provider extends the
// allowlist the same way they would for anything else, via --allow.
//
// Two things about the entries below are worth knowing before relying on
// them, because they differ from the curl-installer shape the first four
// share:
//
//   - gemini is the only agent with no curl-based installer. Google
//     documents npm/npx/Homebrew only, so InstallScript is the npm
//     one-liner and the guest needs Node.js >= 20 already present —
//     which the base images used throughout this project's docs
//     (images:ubuntu/24.04, images:alpine/edge, template://ubuntu-lts)
//     do not ship. On a bare image `--agent=gemini` fails with
//     "npm: command not found" rather than installing anything. Left
//     verbatim rather than silently bootstrapping Node, since that
//     would be agentctl inventing an install path the agent's own docs
//     don't describe; see docs/user/agent-provisioning.md.
//   - cursor's runtime traffic is spread across more domains than any
//     other entry here. Cursor's own network-configuration docs
//     recommend *.cursor.sh, *.cursorapi.com and *.cursor-cdn.com, and
//     name api2.cursor.sh as carrying most API requests, with
//     api3/api4/api5, repo42 and authentication subdomains needed in
//     some setups. The entry covers install plus the primary API path;
//     anything beyond that is a --allow extension, same as the
//     multi-provider agents above.
//
// PREFER CONCRETE HOSTNAMES OVER WILDCARDS HERE. A "*.example.com" entry
// does not do what it looks like: the Incus backend resolves allow-rules
// to IP addresses, and it does that by stripping the "*." and resolving
// the *apex* (see resolveAllowRules in internal/provider/incus). So
// "*.anthropic.com" produced an allow rule for anthropic.com — the
// marketing site — while api.anthropic.com, which is where Claude Code
// actually sends every request, resolves elsewhere and stayed blocked.
// That was a real failure on a live host, not a theoretical one. A
// wildcard is only worth using when the apex is itself a destination.
var Registry = map[string]Spec{
	"claude": {
		Name:          "claude",
		InstallScript: "curl -fsSL https://claude.ai/install.sh | bash",
		EnvVar:        "ANTHROPIC_API_KEY",
		// Anthropic publishes this list; see
		// https://code.claude.com/docs/en/network-config. Two more it
		// names are left out deliberately: raw.githubusercontent.com is
		// a very broad grant for what it buys, and *.sentry.io is
		// optional error reporting. Add either with --allow if you need
		// them.
		AllowDomains: []profile.AllowRule{
			{Domain: "claude.ai", Ports: []int{443}},
			{Domain: "downloads.claude.ai", Ports: []int{443}},
			{Domain: "platform.claude.com", Ports: []int{443}},
			{Domain: "api.anthropic.com", Ports: []int{443}},
			{Domain: "statsig.anthropic.com", Ports: []int{443}},
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
			// Concrete host, not *.anthropic.com: see the wildcard note
			// above. pi defaults to Anthropic, and this is the endpoint
			// that default actually talks to.
			{Domain: "api.anthropic.com", Ports: []int{443}},
		},
	},
	"gemini": {
		Name: "gemini",
		// npm, not curl — see the Registry comment above. The guest must
		// already have Node.js >= 20; this does not install it.
		InstallScript: "npm install -g @google/gemini-cli",
		// Gemini CLI documents three auth paths (GEMINI_API_KEY,
		// GOOGLE_API_KEY + GOOGLE_GENAI_USE_VERTEXAI for Vertex AI, and
		// GOOGLE_CLOUD_PROJECT for Code Assist). GEMINI_API_KEY is the
		// one obvious default, which is what this field is for.
		EnvVar: "GEMINI_API_KEY",
		AllowDomains: []profile.AllowRule{
			{Domain: "registry.npmjs.org", Ports: []int{443}},
			{Domain: "generativelanguage.googleapis.com", Ports: []int{443}},
			{Domain: "accounts.google.com", Ports: []int{443}},
			{Domain: "oauth2.googleapis.com", Ports: []int{443}},
		},
	},
	"cursor": {
		Name: "cursor",
		// Cursor's own documented form: flags after the URL, and no -L.
		// Kept verbatim rather than normalized to match the other curl
		// entries above.
		InstallScript: "curl https://cursor.com/install -fsS | bash",
		EnvVar:        "CURSOR_API_KEY",
		AllowDomains: []profile.AllowRule{
			{Domain: "cursor.com", Ports: []int{443}},
			{Domain: "api2.cursor.sh", Ports: []int{443}},
			// Cursor's docs also recommend *.cursorapi.com, but a
			// wildcard here would only allow the apex (see the note
			// above), which nothing contacts — coverage in appearance
			// only. The one concrete host they name under it,
			// marketplace.cursorapi.com, serves the IDE extension
			// marketplace rather than the CLI, so it is left out until
			// something is shown to need it.
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
