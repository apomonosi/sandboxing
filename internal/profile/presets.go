package profile

import "slices"

// Preset is a named, built-in group of egress allow rules for a common
// package source, code host or coding agent. Getting e.g. pip to work
// takes knowing that it downloads packages from a different host than
// the index it queries; a preset captures that once, and lets a profile
// or `create` say "pypi" instead (allowPresets, --allow-preset).
type Preset struct {
	Name        string
	Description string
	Allow       []AllowRule
}

// presets is the built-in table, sorted by name. Each lists the hosts its
// tools connect to in their default configuration, including the hosts
// downloads get redirected to (for GitHub and GitLab, checked by following
// real downloads; for the agents, see below).
//
// NO WILDCARDS. Every host is listed by name, and TestPresets_WellFormed
// enforces it. A "*.example.com" rule means different things per backend:
// Lima's egress proxy matches it against each requested hostname, but the
// Incus backend resolves allow rules to IP addresses, stripping the "*."
// and resolving only the apex (see resolveAllowRules in
// internal/provider/incus). So "*.anthropic.com" allowed anthropic.com's
// marketing site on Incus while api.anthropic.com stayed blocked — a real
// failure on a live host. Built-in policy should mean the same thing on
// every backend, so presets (like the agent registry and the built-in
// profiles) name concrete hosts; a user who wants Lima's broader wildcard
// matching can still write one with --allow.
//
// The coding agents' presets (claude, codex, cursor, gemini, opencode, pi)
// are also what `create --agent=<name>` allows (see internal/agent), so
// they cover each agent's installer as well as its sign-in and default
// model provider. The hosts were checked against each agent's own docs or
// installer and client sources, because an installer that redirects to or
// downloads from a host missing here fails under default-deny egress:
//   - claude: Claude Code's documented network requirements
//     (https://code.claude.com/docs/en/network-config). install.sh 302s to
//     downloads.claude.ai, which serves the native build and its
//     auto-updates; sign-in (claude.ai or Console) exchanges and refreshes
//     its OAuth token with platform.claude.com. Two more hosts that page
//     has listed are left out deliberately: raw.githubusercontent.com is a
//     very broad grant for what it buys (/release-notes), and *.sentry.io
//     is optional error reporting. Add either with --allow if you need
//     them.
//   - codex: the installer in github.com/openai/codex
//     (scripts/install/install.sh) downloads from releases.openai.com
//     (GitHub Releases only as a fallback); ChatGPT sign-in uses
//     auth.openai.com (codex-rs/login/src/server.rs), and ChatGPT-plan
//     usage goes to chatgpt.com.
//   - cursor: Cursor's network-configuration docs recommend *.cursor.sh,
//     *.cursorapi.com and *.cursor-cdn.com, and name api2.cursor.sh as
//     carrying most API requests, with api3/api4/api5, repo42 and
//     authentication subdomains needed in some setups. The preset covers
//     install plus the primary API path. *.cursorapi.com is left out: as a
//     wildcard it would only allow the apex on Incus, which nothing
//     contacts, and the one concrete host Cursor names under it,
//     marketplace.cursorapi.com, serves the IDE extension marketplace
//     rather than the CLI.
//   - gemini: Google documents installing Gemini CLI with npm (hence
//     registry.npmjs.org); generativelanguage.googleapis.com is the
//     Gemini API, and accounts.google.com plus oauth2.googleapis.com its
//     Google sign-in.
//   - opencode: its install script (github.com/anomalyco/opencode,
//     "install") looks the release up on api.github.com and downloads it
//     from github.com, which redirects to release-assets.githubusercontent.com;
//     at startup it fetches its model catalog from models.opencode.ai
//     (packages/core/src/models-dev.ts), falling back to a bundled copy.
//   - pi: per its README (github.com/earendil-works/pi), the pi.dev
//     installer installs pinned npm packages, and installs Node.js 22.19+
//     if it's missing (true of stock Ubuntu images). nodejs.org — the
//     official Node.js distribution host — is the one entry here not read
//     out of the installer itself.
//
// cursor, opencode and pi reach beyond their presets in some setups:
// opencode and pi are multi-provider (their presets cover installing them
// plus, for pi, its default provider, Anthropic), and cursor spreads its
// traffic over many hosts. Anything else they need is an --allow entry.
var presets = []Preset{
	{
		Name:        "apk",
		Description: "Alpine Linux packages (apk), from Alpine's default CDN",
		Allow:       hostsOn([]int{80, 443}, "dl-cdn.alpinelinux.org"),
	},
	{
		// Ubuntu images use archive.ubuntu.com and security.ubuntu.com
		// on amd64, and ports.ubuntu.com on arm64 (e.g. on Apple
		// silicon); Debian's default is deb.debian.org (whose
		// debian-security path newer releases use) or, on older ones,
		// security.debian.org. apt still speaks plain HTTP by default.
		Name:        "apt",
		Description: "Ubuntu and Debian packages (apt), from their default mirrors",
		Allow: hostsOn([]int{80, 443},
			"archive.ubuntu.com", "security.ubuntu.com", "ports.ubuntu.com",
			"deb.debian.org", "security.debian.org"),
	},
	{
		Name:        "claude",
		Description: "Claude Code: installer and updates, sign-in, the Claude API (what --agent=claude allows)",
		Allow: hostsOn([]int{443},
			"claude.ai",             // install.sh; claude.ai account sign-in
			"downloads.claude.ai",   // native installer and auto-updater
			"platform.claude.com",   // OAuth token exchange/refresh (every sign-in)
			"api.anthropic.com",     // the Claude API
			"statsig.anthropic.com", // Statsig (telemetry, feature flags)
		),
	},
	{
		Name:        "codex",
		Description: "OpenAI Codex CLI: installer, ChatGPT sign-in, the OpenAI API (what --agent=codex allows)",
		Allow: hostsOn([]int{443},
			"chatgpt.com",         // install.sh; ChatGPT-plan usage
			"releases.openai.com", // installer downloads
			"auth.openai.com",     // ChatGPT sign-in
			"api.openai.com",      // the OpenAI API
		),
	},
	{
		Name:        "cursor",
		Description: "Cursor CLI: installer and its primary API host (what --agent=cursor allows)",
		Allow: hostsOn([]int{443},
			"cursor.com",     // install script
			"api2.cursor.sh", // most API requests
		),
	},
	{
		Name:        "gemini",
		Description: "Gemini CLI: its npm package, the Gemini API, Google sign-in (what --agent=gemini allows)",
		Allow: hostsOn([]int{443},
			"registry.npmjs.org",                // npm install -g @google/gemini-cli
			"generativelanguage.googleapis.com", // the Gemini API
			"accounts.google.com",               // Google sign-in
			"oauth2.googleapis.com",             // Google sign-in token exchange
		),
	},
	{
		// git over HTTPS and the API (gh) use github.com and
		// api.github.com; source archives redirect to codeload.github.com
		// and release downloads to release-assets.githubusercontent.com;
		// raw.githubusercontent.com serves raw files, which plenty of
		// install scripts are fetched from.
		Name:        "github",
		Description: "GitHub over HTTPS: git, the API and gh, raw files, archives, release downloads",
		Allow: hostsOn([]int{443},
			"github.com", "api.github.com", "codeload.github.com",
			"raw.githubusercontent.com", "release-assets.githubusercontent.com"),
	},
	{
		// gitlab.com serves git over HTTPS, the API, raw files, archives
		// and release downloads from the one host.
		Name:        "gitlab",
		Description: "GitLab.com over HTTPS: git, the API, raw files, archives, release downloads",
		Allow:       hostsOn([]int{443}, "gitlab.com"),
	},
	{
		Name:        "npm",
		Description: "npm packages (npm, pnpm, yarn), from the public registry",
		Allow:       hostsOn([]int{443}, "registry.npmjs.org", "registry.yarnpkg.com"),
	},
	{
		Name:        "opencode",
		Description: "opencode: installer (GitHub releases) and model catalog (what --agent=opencode allows)",
		Allow: hostsOn([]int{443},
			"opencode.ai",                          // install script
			"github.com",                           // release download
			"api.github.com",                       // latest-release lookup
			"release-assets.githubusercontent.com", // where github.com redirects the download
			"models.opencode.ai",                   // model catalog
		),
	},
	{
		Name:        "pi",
		Description: "pi: installer, its npm packages, Node.js, Anthropic as default provider (what --agent=pi allows)",
		Allow: hostsOn([]int{443},
			"pi.dev",             // install script
			"registry.npmjs.org", // pi itself and its dependencies
			"nodejs.org",         // Node.js, when the installer has to install it
			"api.anthropic.com",  // default provider
		),
	},
	{
		// pip and uv query the index on pypi.org and download from
		// files.pythonhosted.org.
		Name:        "pypi",
		Description: "Python packages from PyPI (pip, uv, poetry)",
		Allow:       hostsOn([]int{443}, "pypi.org", "files.pythonhosted.org"),
	},
}

// hostsOn builds one allow rule per host, each for ports.
func hostsOn(ports []int, hosts ...string) []AllowRule {
	rules := make([]AllowRule, len(hosts))
	for i, h := range hosts {
		rules[i] = AllowRule{Domain: h, Ports: slices.Clone(ports)}
	}
	return rules
}

// Presets returns every built-in preset, sorted by name.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	for i, p := range presets {
		out[i] = p.clone()
	}
	return out
}

// LookupPreset returns the preset called name and whether there is one.
func LookupPreset(name string) (Preset, bool) {
	for _, p := range presets {
		if p.Name == name {
			return p.clone(), true
		}
	}
	return Preset{}, false
}

// PresetNames returns the names of every built-in preset, sorted.
func PresetNames() []string {
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return names
}

// clone deep-copies p, so callers can't modify the built-in table through
// a returned rule's Ports.
func (p Preset) clone() Preset {
	p.Allow = slices.Clone(p.Allow)
	for i := range p.Allow {
		p.Allow[i].Ports = slices.Clone(p.Allow[i].Ports)
	}
	return p
}
