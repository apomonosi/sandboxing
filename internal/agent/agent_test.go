package agent

import "testing"

func TestLookup_KnownAgents(t *testing.T) {
	for _, name := range []string{"claude", "codex", "opencode", "pi"} {
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
	want := []string{"claude", "codex", "opencode", "pi"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q (must be sorted)", i, got[i], want[i])
		}
	}
}
