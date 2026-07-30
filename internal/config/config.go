// Package config manages agentctl's persistent, per-user configuration:
// which provider backend to use and where to find org-distributed
// profiles. This is deliberately small — provider selection is meant to
// be pinned once (by the user or by whoever rolls agentctl out org-wide)
// and rarely touched afterward.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is agentctl's persistent configuration file.
type Config struct {
	// Provider is the backend to dispatch to: "incus", "lima", or
	// "hyperv". Empty until `agentctl config set provider=...` is run.
	Provider string `yaml:"provider"`
	// ProfileDir is an optional org-distributed directory of shared
	// profile YAML files, checked after the user's own profile
	// directories and before embedded built-ins (see
	// internal/profile.SearchPaths).
	ProfileDir string `yaml:"profileDir"`
	// DefaultProfile is applied by `create` when no --profile or --spec
	// profile list is given at all. Set via `agentctl profile set <name>`.
	DefaultProfile string `yaml:"defaultProfile"`
}

// Path returns the config file path: $AGENTCTL_CONFIG if set, otherwise
// ~/.config/agentctl/config.yaml.
func Path() (string, error) {
	if p := os.Getenv("AGENTCTL_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "agentctl", "config.yaml"), nil
}

// Load reads the config file. A missing file is not an error: it returns a
// zero-value Config, since agentctl runs (mostly command --help, config
// set) without one.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return &c, nil
}

// Save writes the config file, creating its parent directory if needed.
func Save(c *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}
