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
// passed to --agent. Install commands verified directly from each
// project's own docs (not reconstructed from memory):
//   - claude:   https://code.claude.com/docs/en/quickstart
//   - codex:    https://learn.chatgpt.com/docs/codex/cli
//   - opencode: https://opencode.ai/download
//   - pi:       https://pi.dev/
//   - gemini:   https://github.com/google-gemini/gemini-cli
//   - cursor:   https://cursor.com/docs/cli/installation
//
// The hosts each agent needs live in the built-in allow preset of the same
// name (internal/profile/presets.go, which also records where each host
// list was checked and why some documented hosts are left out), so
// `--allow-preset=<name>` allows exactly what `--agent=<name>` does, for
// when the agent gets installed some other way.
//
// Two things about the entries below are worth knowing before relying on
// them, because they differ from the curl-installer shape the others
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
//     other entry here; its preset covers install plus the primary API
//     path, and anything beyond that is a --allow extension.
var Registry = map[string]Spec{
	"claude": {
		Name:          "claude",
		InstallScript: "curl -fsSL https://claude.ai/install.sh | bash",
		EnvVar:        "ANTHROPIC_API_KEY",
		AllowDomains:  presetRules("claude"),
	},
	"codex": {
		Name:          "codex",
		InstallScript: "curl -fsSL https://chatgpt.com/codex/install.sh | sh",
		EnvVar:        "OPENAI_API_KEY",
		AllowDomains:  presetRules("codex"),
	},
	"opencode": {
		Name:          "opencode",
		InstallScript: "curl -fsSL https://opencode.ai/install | bash",
		EnvVar:        "",
		AllowDomains:  presetRules("opencode"),
	},
	"pi": {
		Name:          "pi",
		InstallScript: "curl -fsSL https://pi.dev/install.sh | sh",
		EnvVar:        "ANTHROPIC_API_KEY",
		AllowDomains:  presetRules("pi"),
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
		EnvVar:       "GEMINI_API_KEY",
		AllowDomains: presetRules("gemini"),
	},
	"cursor": {
		Name: "cursor",
		// Cursor's own documented form: flags after the URL, and no -L.
		// Kept verbatim rather than normalized to match the other curl
		// entries above.
		InstallScript: "curl https://cursor.com/install -fsS | bash",
		EnvVar:        "CURSOR_API_KEY",
		AllowDomains:  presetRules("cursor"),
	},
}

// presetRules returns the allow rules of the built-in preset called name.
// The registry and the preset table are both static, so a missing preset
// is a build defect (and TestLookup_KnownAgents catches it), not a runtime
// condition.
func presetRules(name string) []profile.AllowRule {
	p, ok := profile.LookupPreset(name)
	if !ok {
		panic("agent: no built-in allow preset " + name)
	}
	return p.Allow
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
