package lima

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckForwarding(t *testing.T) {
	cases := []struct {
		name    string
		ssh     limaSSHJSON
		wantErr []string // substrings; nil means no error
	}{
		{name: "neither forwarded"},
		{
			name: "the host's SSH agent",
			ssh:  limaSSHJSON{ForwardAgent: true},
			wantErr: []string{
				`would forward the host's SSH agent into instance "demo"`,
				"`limactl edit --set '.ssh.forwardAgent = false' --start=false demo`",
			},
		},
		{
			name: "the host's X11 display",
			ssh:  limaSSHJSON{ForwardX11: true},
			wantErr: []string{
				`would forward the host's X11 display into instance "demo"`,
				"`limactl edit --set '.ssh.forwardX11 = false' --start=false demo`",
			},
		},
		{
			name:    "both",
			ssh:     limaSSHJSON{ForwardAgent: true, ForwardX11: true},
			wantErr: []string{"the host's SSH agent"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := limaInstanceJSON{Name: "demo", Dir: filepath.Join(t.TempDir(), "demo"), Config: &limaConfigJSON{SSH: tc.ssh}}
			err := checkForwarding(j)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("checkForwarding() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("checkForwarding() = nil, want an error")
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("checkForwarding() = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestCheckForwarding_ErrorNamesOverrideYAML: once create turns
// forwarding off in the instance's own configuration, override.yaml is
// the one file left that can turn it back on, so the error names it — in
// the $LIMA_HOME the instance actually lives in.
func TestCheckForwarding_ErrorNamesOverrideYAML(t *testing.T) {
	limaHome := t.TempDir()
	for dir, want := range map[string]string{
		filepath.Join(limaHome, "demo"): filepath.Join(limaHome, "_config", "override.yaml"),
		"":                              filepath.Join("$LIMA_HOME", "_config", "override.yaml"),
	} {
		j := limaInstanceJSON{Name: "demo", Dir: dir, Config: &limaConfigJSON{SSH: limaSSHJSON{ForwardAgent: true}}}
		if err := checkForwarding(j); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("checkForwarding() with dir %q = %v, want it to name %s", dir, err, want)
		}
	}
}

// TestCheckForwarding_CannotVerify: without Lima's effective
// configuration, agentctl can't tell what Lima would forward, so it
// refuses the start.
func TestCheckForwarding_CannotVerify(t *testing.T) {
	if err := checkForwarding(limaInstanceJSON{Name: "demo"}); err == nil {
		t.Error("checkForwarding() = nil, want an error")
	}
}
