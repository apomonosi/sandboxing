package agent

import (
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/profile"
)

// allowsExactly reports whether rules has an entry naming host itself (not
// via a wildcard: on Incus "*.example.com" only resolves the apex) with
// port 443 among its ports.
func allowsExactly(rules []profile.AllowRule, host string) bool {
	for _, r := range rules {
		if r.Domain == host && slices.Contains(r.Ports, 443) {
			return true
		}
	}
	return false
}

// TestRegistry_InstallHostAllowlisted guards the failure mode where an
// agent's own installer can't be fetched under default-deny egress: the
// host in each InstallScript's URL must be on that agent's allowlist (and
// for an npm install, the npm registry).
func TestRegistry_InstallHostAllowlisted(t *testing.T) {
	urlRE := regexp.MustCompile(`https://[^\s|]+`)
	for name, spec := range Registry {
		if strings.HasPrefix(spec.InstallScript, "npm ") {
			if !allowsExactly(spec.AllowDomains, "registry.npmjs.org") {
				t.Errorf("%s: npm installer, but registry.npmjs.org:443 is not in AllowDomains %+v", name, spec.AllowDomains)
			}
			continue
		}
		raw := urlRE.FindString(spec.InstallScript)
		if raw == "" {
			t.Errorf("%s: no https:// URL in InstallScript %q", name, spec.InstallScript)
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Errorf("%s: parsing %q: %v", name, raw, err)
			continue
		}
		if !allowsExactly(spec.AllowDomains, u.Hostname()) {
			t.Errorf("%s: install host %s:443 is not in AllowDomains %+v", name, u.Hostname(), spec.AllowDomains)
		}
	}
}

// TestRegistry_RequiredHosts pins the hosts each agent's installer
// downloads from or its sign-in depends on (see Registry's doc comment for
// where each comes from) — every one of them was missing once, which made
// `create --agent` fail with egress enforced.
func TestRegistry_RequiredHosts(t *testing.T) {
	required := map[string][]string{
		"claude":   {"claude.ai", "downloads.claude.ai", "platform.claude.com", "api.anthropic.com"},
		"codex":    {"chatgpt.com", "releases.openai.com", "auth.openai.com", "api.openai.com"},
		"cursor":   {"cursor.com", "api2.cursor.sh"},
		"gemini":   {"registry.npmjs.org", "generativelanguage.googleapis.com"},
		"opencode": {"opencode.ai", "github.com", "api.github.com", "release-assets.githubusercontent.com"},
		"pi":       {"pi.dev", "registry.npmjs.org", "nodejs.org", "api.anthropic.com"},
	}
	for name, hosts := range required {
		spec, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q): not found", name)
		}
		for _, h := range hosts {
			if !allowsExactly(spec.AllowDomains, h) {
				t.Errorf("%s: %s:443 is not in AllowDomains %+v", name, h, spec.AllowDomains)
			}
		}
	}
}

func TestLookup_KnownAgents(t *testing.T) {
	for _, name := range []string{"claude", "codex", "cursor", "gemini", "opencode", "pi"} {
		spec, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q): expected ok=true", name)
		}
		if spec.Name != name {
			t.Errorf("Lookup(%q).Name = %q", name, spec.Name)
		}
		if spec.InstallScript == "" {
			t.Errorf("Lookup(%q).InstallScript is empty", name)
		}
		if len(spec.AllowDomains) == 0 {
			t.Errorf("Lookup(%q).AllowDomains is empty", name)
		}
		for _, rule := range spec.AllowDomains {
			if rule.Domain == "" {
				t.Errorf("Lookup(%q): AllowRule with empty domain", name)
			}
			if len(rule.Ports) == 0 {
				t.Errorf("Lookup(%q): AllowRule %q has no ports", name, rule.Domain)
			}
		}
	}
}

func TestLookup_Unknown(t *testing.T) {
	if _, ok := Lookup("nonexistent"); ok {
		t.Error("Lookup(\"nonexistent\") should return ok=false")
	}
}

func TestNames_SortedAndComplete(t *testing.T) {
	got := Names()
	want := []string{"claude", "codex", "cursor", "gemini", "opencode", "pi"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q (must be sorted)", i, got[i], want[i])
		}
	}
}

// TestRegistry_NoWildcardDomains pins a rule learned the hard way on a
// live Fedora 44 host. The Incus backend resolves allow-rules to IP
// addresses by stripping a leading "*." and resolving the *apex*, so
// "*.anthropic.com" allowed the marketing site's address while
// api.anthropic.com — where Claude Code sends every request — stayed
// blocked. The policy looked correct and the agent could not reach its
// own API.
//
// A wildcard is only meaningful here when the apex is itself a
// destination, which is not the case for any entry in this registry. Name
// concrete hosts instead.
func TestRegistry_NoWildcardDomains(t *testing.T) {
	for name, spec := range Registry {
		for _, rule := range spec.AllowDomains {
			if strings.HasPrefix(rule.Domain, "*.") {
				t.Errorf("agent %q allows %q: a wildcard resolves to the apex only, "+
					"so subdomains stay blocked — name the concrete hosts", name, rule.Domain)
			}
		}
	}
}

// The two hosts Claude Code cannot work without: the install script's
// origin and the API endpoint every request goes to.
func TestRegistry_ClaudeAllowsInstallAndAPI(t *testing.T) {
	spec, ok := Lookup("claude")
	if !ok {
		t.Fatal("claude missing from the registry")
	}
	for _, want := range []string{"claude.ai", "api.anthropic.com"} {
		found := false
		for _, rule := range spec.AllowDomains {
			if rule.Domain == want {
				found = true
			}
		}
		if !found {
			t.Errorf("claude should allow %q, got %+v", want, spec.AllowDomains)
		}
	}
}

// TestRegistry_EveryAgentHasAPreset pins the relationship the docs state as
// a fact: `--allow-preset=<name>` allows exactly what `--agent=<name>`
// does, for when the agent is installed some other way.
//
// presetRules panics on a missing preset, so a new agent without one fails
// loudly at init rather than here. What this catches is the quieter
// direction — an agent whose AllowDomains stopped coming from its preset,
// which would make the two flags diverge silently. It also pairs with the
// docs: two pages list the agent presets by name, and that list went stale
// when gemini and cursor were added.
func TestRegistry_EveryAgentHasAPreset(t *testing.T) {
	for name, spec := range Registry {
		preset, ok := profile.LookupPreset(name)
		if !ok {
			t.Errorf("agent %q has no built-in allow preset of the same name; "+
				"--allow-preset=%s should allow exactly what --agent=%s does", name, name, name)
			continue
		}
		if !reflect.DeepEqual(spec.AllowDomains, preset.Allow) {
			t.Errorf("agent %q allows %+v but preset %q allows %+v — the two must stay identical",
				name, spec.AllowDomains, name, preset.Allow)
		}
	}
}
