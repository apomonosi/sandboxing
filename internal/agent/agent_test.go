package agent

import (
	"strings"
	"testing"
)

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
