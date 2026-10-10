package lima

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckHostChannels(t *testing.T) {
	limaHome := t.TempDir()
	defaultYAML := filepath.Join(limaHome, "_config", "default.yaml")
	overrideYAML := filepath.Join(limaHome, "_config", "override.yaml")

	cases := []struct {
		name    string
		config  limaConfigJSON
		wantErr []string // substrings; nil means no error
	}{
		{name: "neither"},
		{
			// What `agentctl create --port 8080:80` asks for, and a
			// template's guest socket shared with the host (Lima's docker
			// template does this): both reach into the guest, not out of it.
			name: "ordinary port and socket forwards",
			config: limaConfigJSON{PortForwards: []limaPortForwardJSON{
				{},
				{GuestSocket: "/run/user/1000/docker.sock", HostSocket: filepath.Join(limaHome, "demo", "sock", "docker.sock")},
			}},
		},
		{
			name: "a reverse socket forward",
			config: limaConfigJSON{PortForwards: []limaPortForwardJSON{
				{},
				{GuestSocket: "/run/user/1000/docker.sock", HostSocket: "/var/run/docker.sock", Reverse: true},
			}},
			wantErr: []string{
				`would forward the host socket /var/run/docker.sock into instance "demo" (at /run/user/1000/docker.sock)`,
				defaultYAML, overrideYAML,
				"`limactl edit --set 'del(.portForwards[] | select(.reverse == true))' --start=false demo`",
			},
		},
		{
			name: "a copyToHost entry",
			config: limaConfigJSON{CopyToHost: []limaCopyToHostJSON{
				{GuestFile: "/etc/rancher/k3s/k3s.yaml", HostFile: "/home/you/.kube/config"},
			}},
			wantErr: []string{
				`would copy /etc/rancher/k3s/k3s.yaml from instance "demo" to /home/you/.kube/config on the host`,
				defaultYAML, overrideYAML,
				"`limactl edit --set '.copyToHost = []' --start=false demo`",
			},
		},
		{
			name: "both",
			config: limaConfigJSON{
				PortForwards: []limaPortForwardJSON{{GuestSocket: "/g.sock", HostSocket: "/h.sock", Reverse: true}},
				CopyToHost:   []limaCopyToHostJSON{{GuestFile: "/etc/hostname", HostFile: "/tmp/hostname"}},
			},
			wantErr: []string{"would forward the host socket /h.sock"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := tc.config
			j := limaInstanceJSON{Name: "demo", Dir: filepath.Join(limaHome, "demo"), Config: &config}
			err := checkHostChannels(j)
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("checkHostChannels() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("checkHostChannels() = nil, want an error")
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("checkHostChannels() = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestCheckHostChannels_CannotVerify: without Lima's effective
// configuration, agentctl can't tell what Lima would open to the host, so
// it refuses the start.
func TestCheckHostChannels_CannotVerify(t *testing.T) {
	if err := checkHostChannels(limaInstanceJSON{Name: "demo"}); err == nil {
		t.Error("checkHostChannels() = nil, want an error")
	}
}
