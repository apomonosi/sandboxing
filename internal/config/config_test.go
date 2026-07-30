package config

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "" {
		t.Errorf("Provider = %q, want empty", cfg.Provider)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	t.Setenv("AGENTCTL_CONFIG", filepath.Join(t.TempDir(), "nested", "config.yaml"))

	want := &Config{Provider: "incus", ProfileDir: "/shared/profiles", DefaultProfile: "strict"}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *got != *want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}
