package lima

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingStateFile_ReturnsZeroValue(t *testing.T) {
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	s, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if len(s.Instances) != 0 {
		t.Errorf("Instances = %v, want empty", s.Instances)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "nested", "config.yaml"))

	want := &stateFile{Instances: map[string]instanceState{
		"demo": {AgentRequested: "claude", AgentInstalled: true},
	}}
	if err := saveState(want); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	got, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if got.Instances["demo"] != want.Instances["demo"] {
		t.Errorf("loadState() = %+v, want %+v", got.Instances["demo"], want.Instances["demo"])
	}
}

func TestPendingAgentInstall_Lifecycle(t *testing.T) {
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	t.Run("nothing requested", func(t *testing.T) {
		got, err := pendingAgentInstall("demo")
		if err != nil {
			t.Fatalf("pendingAgentInstall: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want \"\" when no agent was ever requested", got)
		}
	})

	t.Run("requested but not installed", func(t *testing.T) {
		if err := setAgentRequested("demo", "claude"); err != nil {
			t.Fatalf("setAgentRequested: %v", err)
		}
		got, err := pendingAgentInstall("demo")
		if err != nil {
			t.Fatalf("pendingAgentInstall: %v", err)
		}
		if got != "claude" {
			t.Errorf("got %q, want \"claude\"", got)
		}
	})

	t.Run("requested and installed", func(t *testing.T) {
		if err := markAgentInstalled("demo"); err != nil {
			t.Fatalf("markAgentInstalled: %v", err)
		}
		got, err := pendingAgentInstall("demo")
		if err != nil {
			t.Fatalf("pendingAgentInstall: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want \"\" once installed", got)
		}
	})
}

func TestPruneInstanceState_RemovesEntry(t *testing.T) {
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	if err := setAgentRequested("demo", "claude"); err != nil {
		t.Fatalf("setAgentRequested: %v", err)
	}
	pruneInstanceState("demo")

	got, err := pendingAgentInstall("demo")
	if err != nil {
		t.Fatalf("pendingAgentInstall: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want \"\" after pruneInstanceState", got)
	}
	s, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if _, ok := s.Instances["demo"]; ok {
		t.Error("expected the \"demo\" entry to be removed entirely, not just zeroed")
	}
}

func TestPruneInstanceState_NoopOnUnknownInstance(t *testing.T) {
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	pruneInstanceState("never-existed") // must not panic or error
}
