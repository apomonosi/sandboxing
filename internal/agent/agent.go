// Package agent holds agentctl's static registry of coding agents that
// can be just-in-time installed into a freshly created sandbox via
// `agentctl create --agent=<name>`, instead of hand-typing an install
// command after `shell`-ing in. Shape mirrors internal/image: a small,
// static table, no external registry lookups.
package agent

import (
	_ "embed"
	"sort"

	"github.com/apomonosi/sandboxing/internal/profile"
)

// Spec describes one installable coding agent.
type Spec struct {
	Name string
	// InstallScript is a full shell command line (passed to `sh -c`, not
	// stdin) — each agent's own documented install command. A curl
	// one-liner piped into a shell is run through fetchRun instead, with
	// the agent's own URL, shell and curl flags, so a refused download
	// fails the install rather than running an empty script; pi's is
	// additionally preceded by nodeBootstrap and toolsBootstrap.
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
//   - pi's installer needs Node.js >= 22.19.0 and only offers to install
//     it when it has a terminal, which `create --agent` never gives it,
//     so its one-liner is preceded by nodeBootstrap. gemini could reuse
//     the same bootstrap (its floor, Node 20, is below the v22 line it
//     installs); that is a separate change. pi also downloads ripgrep and
//     fd from github.com at first use, which egress refuses, so
//     toolsBootstrap installs them from the distribution first.
//   - every curl installer is fetched to a file by fetchRun and only run
//     once curl has succeeded, rather than piped into a shell: a pipeline
//     exits with the shell's status, and a shell given nothing exits 0.
//   - every install script ends by putting the agent's binary on PATH
//     (see onPath), since installers commonly leave it where a later
//     `agentctl shell` cannot find it by name.
//   - cursor's runtime traffic is spread across more domains than any
//     other entry here; its preset covers install plus the primary API
//     path, and anything beyond that is a --allow extension.
var Registry = map[string]Spec{
	"claude": {
		Name:          "claude",
		InstallScript: onPath("claude", "claude", fetchRun+"agentctl_fetch_run claude bash https://claude.ai/install.sh -fsSL"),
		EnvVar:        "ANTHROPIC_API_KEY",
		AllowDomains:  presetRules("claude"),
	},
	"codex": {
		Name:          "codex",
		InstallScript: onPath("codex", "codex", fetchRun+"agentctl_fetch_run codex sh https://chatgpt.com/codex/install.sh -fsSL"),
		EnvVar:        "OPENAI_API_KEY",
		AllowDomains:  presetRules("codex"),
	},
	"opencode": {
		Name:          "opencode",
		InstallScript: onPath("opencode", "opencode", fetchRun+"agentctl_fetch_run opencode bash https://opencode.ai/install -fsSL"),
		EnvVar:        "",
		AllowDomains:  presetRules("opencode"),
	},
	"pi": {
		Name:          "pi",
		InstallScript: onPath("pi", "pi", piInstallScript),
		EnvVar:        "ANTHROPIC_API_KEY",
		AllowDomains:  presetRules("pi"),
	},
	"gemini": {
		Name: "gemini",
		// npm, not curl — see the Registry comment above. The guest must
		// already have Node.js >= 20; this does not install it.
		InstallScript: onPath("gemini", "gemini", "npm install -g @google/gemini-cli"),
		// Gemini CLI documents three auth paths (GEMINI_API_KEY,
		// GOOGLE_API_KEY + GOOGLE_GENAI_USE_VERTEXAI for Vertex AI, and
		// GOOGLE_CLOUD_PROJECT for Code Assist). GEMINI_API_KEY is the
		// one obvious default, which is what this field is for.
		EnvVar:       "GEMINI_API_KEY",
		AllowDomains: presetRules("gemini"),
	},
	"cursor": {
		Name: "cursor",
		// Cursor's own documented flags: -fsS, and no -L. Kept rather
		// than normalized to match the other curl entries above. The
		// binary it installs is cursor-agent, not cursor.
		InstallScript: onPath("cursor", "cursor-agent", fetchRun+"agentctl_fetch_run cursor bash https://cursor.com/install -fsS"),
		EnvVar:        "CURSOR_API_KEY",
		AllowDomains:  presetRules("cursor"),
	},
}

// nodeBootstrap defines agentctl_ensure_node, which installs Node.js in
// the guest unless a new enough one is already on PATH. See the script's
// header for what it does on each kind of guest.
//
//go:embed node-bootstrap.sh
var nodeBootstrap string

// toolsBootstrap defines agentctl_ensure_tools, which installs ripgrep and
// fd with the guest's package manager when they are missing, and never
// fails the install.
//
//go:embed tools-bootstrap.sh
var toolsBootstrap string

// piInstallScript is pi's documented one-liner, after installing ripgrep
// and fd where the package source is reachable and making sure Node.js is
// at least the 22.19.0 pi's installer requires. nodeBootstrap needs
// pathAdd ahead of it, which onPath supplies.
var piInstallScript = nodeBootstrap + toolsBootstrap + fetchRun +
	"agentctl_ensure_tools pi\n" +
	"agentctl_ensure_node pi 22.19.0\n" +
	"agentctl_fetch_run pi sh https://pi.dev/install.sh -fsSL"

// pathAdd defines agentctl_path_add and agentctl_agent_on_path, which put
// a directory on PATH now and through ~/.profile. See the script's header.
//
//go:embed path-add.sh
var pathAdd string

// onPath wraps an agent's install script so that, once the script has
// succeeded, agentctl_agent_on_path makes binary runnable by name. The
// script's last command is the installer, so its failure still ends the
// script with the installer's status.
func onPath(agent, binary, script string) string {
	return pathAdd + script + " || exit\n" +
		"agentctl_agent_on_path " + agent + " " + binary
}

// fetchRun defines agentctl_fetch_run, which downloads an agent's
// installer to a temporary file and runs it only if curl succeeded. See
// the script's header for why.
//
//go:embed fetch-run.sh
var fetchRun string

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

// profile checks a profile's agents list against this registry, which it
// cannot import; see profile.KnownAgents.
func init() {
	profile.KnownAgents = Names
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
