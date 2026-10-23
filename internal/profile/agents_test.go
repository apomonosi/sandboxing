package profile

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// withKnownAgents stands in for internal/agent's registry, which this
// package cannot import.
func withKnownAgents(t *testing.T, names ...string) {
	t.Helper()
	prev := KnownAgents
	KnownAgents = func() []string { return names }
	t.Cleanup(func() { KnownAgents = prev })
}

const agentsProfileHead = `apiVersion: agentctl.dev/v1
kind: Profile
metadata:
  name: pi-local
spec:
  network:
    denyLAN: true
`

func TestLoadBytes_Agents_RoundTrip(t *testing.T) {
	withKnownAgents(t, "claude", "pi")
	for _, tc := range []struct {
		name string
		yaml string
		want []string
	}{
		{"without the key", agentsProfileHead, nil},
		{"with the key", agentsProfileHead + "  agents: [pi, claude]\n", []string{"pi", "claude"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := LoadBytes([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("LoadBytes: %v", err)
			}
			if !reflect.DeepEqual(p.Spec.Agents, tc.want) {
				t.Errorf("Agents = %#v, want %#v", p.Spec.Agents, tc.want)
			}
			data, err := yaml.Marshal(p)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			again, err := LoadBytes(data)
			if err != nil {
				t.Fatalf("LoadBytes after Marshal: %v\n%s", err, data)
			}
			if len(again.Spec.Agents) != len(tc.want) || (len(tc.want) > 0 && !reflect.DeepEqual(again.Spec.Agents, tc.want)) {
				t.Errorf("Agents after round trip = %#v, want %#v", again.Spec.Agents, tc.want)
			}
		})
	}
}

func TestLoadBytes_Agents_UnknownNameIsAnError(t *testing.T) {
	withKnownAgents(t, "claude", "pi")
	_, err := LoadBytes([]byte(agentsProfileHead + "  agents: [pi, cluade]\n"))
	if err == nil {
		t.Fatal("expected an error for an unknown agent name")
	}
	if want := `agents: unknown agent "cluade" (valid: claude, pi)`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
}

func TestLoadBytes_Agents_NoRegistryFailsClosed(t *testing.T) {
	prev := KnownAgents
	KnownAgents = nil
	t.Cleanup(func() { KnownAgents = prev })
	if _, err := LoadBytes([]byte(agentsProfileHead + "  agents: [pi]\n")); err == nil {
		t.Fatal("a declared agents list must not load unchecked")
	}
	if _, err := LoadBytes([]byte(agentsProfileHead)); err != nil {
		t.Fatalf("a profile without agents needs no registry: %v", err)
	}
}

func TestValidatePolicy_RejectsAgents(t *testing.T) {
	withKnownAgents(t, "pi")
	err := ValidatePolicy(Policy{Agents: []string{"pi"}})
	if err == nil || !strings.Contains(err.Error(), "only a profile") {
		t.Errorf("ValidatePolicy = %v, want an error saying only a profile can declare agents", err)
	}
}

func TestMerge_DoesNotCarryAgents(t *testing.T) {
	got := Merge(Policy{}, Policy{Agents: []string{"pi"}})
	if len(got.Agents) != 0 {
		t.Errorf("Merge carried Agents = %v; each profile's list is checked on its own", got.Agents)
	}
}
