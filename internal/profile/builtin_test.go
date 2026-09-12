package profile

import (
	"sort"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/packages"
)

func loadBuiltin(t *testing.T, name string) *Profile {
	t.Helper()
	p, err := LoadNamed(name, "")
	if err != nil {
		t.Fatalf("LoadNamed(%q): %v", name, err)
	}
	return p
}

func TestBuiltinNames_Complete(t *testing.T) {
	got := BuiltinNames()
	sort.Strings(got)
	want := []string{"default", "gui", "strict", "terminal"}
	if len(got) != len(want) {
		t.Fatalf("BuiltinNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("BuiltinNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Every built-in must survive its own validator. Cheap, and it is the
// only thing standing between a typo in an embedded YAML file and a
// `--profile=gui` that fails for every user of the release.
func TestBuiltins_AreValid(t *testing.T) {
	for _, name := range BuiltinNames() {
		t.Run(name, func(t *testing.T) {
			p := loadBuiltin(t, name)
			if err := Validate(p); err != nil {
				t.Errorf("built-in profile %q is invalid: %v", name, err)
			}
			if p.Metadata.Name != name {
				t.Errorf("metadata.name = %q, want %q (the name it is loaded by)", p.Metadata.Name, name)
			}
		})
	}
}

// TestBuiltins_NoWildcardDomains is the profile-side twin of
// internal/agent's TestRegistry_NoWildcardDomains, and exists because the
// same bug shipped in both places.
//
// The Incus backend resolves an allow-rule to IP addresses by stripping a
// leading "*." and resolving the *apex*. So "*.anthropic.com" allow-listed
// the marketing site's address while api.anthropic.com — where anything
// actually sends requests — resolved elsewhere and stayed blocked. The
// policy read as more permissive than it was, and the failure looked like
// a network problem rather than a policy one.
func TestBuiltins_NoWildcardDomains(t *testing.T) {
	for _, name := range BuiltinNames() {
		p := loadBuiltin(t, name)
		for _, rule := range p.Spec.Network.Allow {
			if strings.HasPrefix(rule.Domain, "*.") {
				t.Errorf("profile %q allows %q: a wildcard resolves to the apex only, "+
					"so subdomains stay blocked — name the concrete hosts", name, rule.Domain)
			}
		}
	}
}

// Package names in a built-in should be portable across every distro
// agentctl drives — that is the entire point of shipping them. A raw
// distro name would still install, on one distro, which is precisely the
// silent breakage worth catching here rather than on a user's Alpine box.
func TestBuiltins_PackagesAreInTheCatalog(t *testing.T) {
	for _, name := range BuiltinNames() {
		p := loadBuiltin(t, name)
		for _, tool := range p.Spec.Packages {
			if _, ok := packages.Catalog[tool]; !ok {
				t.Errorf("profile %q lists package %q, which is not in the catalog — "+
					"it would be passed through verbatim and only install on some distributions", name, tool)
			}
		}
	}
}

// The two tiers are defined by what they add: terminal brings a
// toolchain, gui brings terminal's toolchain plus a display stack.
func TestBuiltins_TierShape(t *testing.T) {
	plain := loadBuiltin(t, "default")
	if len(plain.Spec.Packages) != 0 {
		t.Errorf("default should install nothing, got %v", plain.Spec.Packages)
	}
	strict := loadBuiltin(t, "strict")
	if len(strict.Spec.Packages) != 0 {
		t.Errorf("strict should install nothing, got %v", strict.Spec.Packages)
	}

	terminal := loadBuiltin(t, "terminal")
	for _, want := range []string{"git", "make", "python3"} {
		if !contains(terminal.Spec.Packages, want) {
			t.Errorf("terminal should install %q, got %v", want, terminal.Spec.Packages)
		}
	}

	gui := loadBuiltin(t, "gui")
	for _, want := range append(terminal.Spec.Packages, "xorg", "openbox") {
		if !contains(gui.Spec.Packages, want) {
			t.Errorf("gui should install %q (everything terminal does, plus the X stack), got %v", want, gui.Spec.Packages)
		}
	}
}

// A built-in that installs software is useless if it drops the sandbox's
// protections to do it. denyLAN in particular must survive the tiers.
func TestBuiltins_ToolingTiersKeepDenyLAN(t *testing.T) {
	for _, name := range []string{"terminal", "gui"} {
		p := loadBuiltin(t, name)
		if !p.Spec.Network.DenyLAN {
			t.Errorf("profile %q has denyLAN=false; a tooling tier must not relax the network policy", name)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
