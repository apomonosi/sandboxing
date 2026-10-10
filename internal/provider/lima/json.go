package lima

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// limaInstanceJSON is a deliberately lenient, partial view of `limactl
// list --json` output: only the fields agentctl actually needs are
// declared, and every conversion in toInstance is defensive
// (missing/malformed fields degrade to zero values rather than erroring)
// — same posture as internal/provider/incus/json.go's instanceJSON.
//
// NOTE: the network-confinement fields (vmType through config) were
// checked against the JSON tags of Lima v2.2's limatype.Instance source;
// name, status, dir, vmType, sshLocalPort, and config's propagateProxyEnv,
// env, mounts and ssh also against `limactl list --json` from a Lima v2.2.1
// build (on Linux, not macOS, and only for a stopped instance with no
// additional networks). That same source shows the output has no guest IP
// address field at all (so Instance.IPs stays unpopulated on this
// backend), and no creation time either: CreatedAt below always comes back
// empty and degrades to a zero time.
type limaInstanceJSON struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	// Dir is the instance's directory, $LIMA_HOME/<name>, which holds its
	// own lima.yaml.
	Dir string `json:"dir"`

	// VMType, Networks (Lima's additional NICs, after merging
	// ~/.lima/_config/default.yaml and override.yaml), and SSHLocalPort
	// are what network.go's checkConfinable verifies before every start.
	VMType       string            `json:"vmType"`
	Networks     []json.RawMessage `json:"network"`
	SSHLocalPort int               `json:"sshLocalPort"`
	// HostAgentPID is the running hostagent's PID (0 when stopped);
	// AutoStartedIdentifier is non-empty when launchd started it.
	HostAgentPID          int    `json:"hostAgentPID"`
	AutoStartedIdentifier string `json:"autoStartedIdentifier"`
	// Config is the instance's effective configuration: its own lima.yaml
	// merged with $LIMA_HOME/_config/default.yaml and override.yaml, the
	// way Lima loads it for every start. Nil if Lima couldn't load it.
	Config *limaConfigJSON `json:"config"`
}

// configDir returns $LIMA_HOME/_config, which holds the default.yaml and
// override.yaml Lima merges into every instance: Dir's parent is
// $LIMA_HOME. It's spelled "$LIMA_HOME/_config" if limactl reported no
// Dir.
func (j limaInstanceJSON) configDir() string {
	if j.Dir == "" {
		return filepath.Join("$LIMA_HOME", "_config")
	}
	return filepath.Join(filepath.Dir(j.Dir), "_config")
}

// limaConfigJSON is the part of an instance's effective lima.yaml that
// Start checks: what network confinement depends on (checkConfinable),
// what the guest gets mounted (checkMounts), and what Lima forwards into it
// from the host or copies out of it (checkForwarding, checkHostChannels).
type limaConfigJSON struct {
	PropagateProxyEnv *bool                 `json:"propagateProxyEnv"`
	Env               map[string]string     `json:"env"`
	Mounts            []limaMountJSON       `json:"mounts"`
	SSH               limaSSHJSON           `json:"ssh"`
	PortForwards      []limaPortForwardJSON `json:"portForwards"`
	CopyToHost        []limaCopyToHostJSON  `json:"copyToHost"`
}

// limaPortForwardJSON is the part of a portForwards entry that says
// whether it forwards a host socket into the guest. Lima has already
// expanded the socket paths' templates.
type limaPortForwardJSON struct {
	GuestSocket string `json:"guestSocket"`
	HostSocket  string `json:"hostSocket"`
	Reverse     bool   `json:"reverse"`
}

// limaCopyToHostJSON is a copyToHost entry: a guest file Lima copies to a
// host path once the guest is up, both with templates already expanded.
type limaCopyToHostJSON struct {
	GuestFile string `json:"guest"`
	HostFile  string `json:"host"`
}

// limaSSHJSON is the part of the effective configuration's ssh section
// that says what Lima forwards from the host into the guest. Lima fills
// both fields in (default: false) before reporting them, so only a Lima
// without the setting leaves one out — and that Lima can't forward it.
type limaSSHJSON struct {
	ForwardAgent bool `json:"forwardAgent"`
	ForwardX11   bool `json:"forwardX11"`
}

// limaMountJSON is one host directory shared into the guest, as Lima
// reports it in `limactl list --json` — and, through the yaml tags, as it
// writes it in an instance's own lima.yaml. In the effective configuration,
// Lima has already expanded Location ("~", templates) and filled in
// MountPoint (default: Location) and Writable (default: false); in a
// lima.yaml any of that can still be left to Lima.
type limaMountJSON struct {
	Location   string  `json:"location" yaml:"location"`
	MountPoint *string `json:"mountPoint" yaml:"mountPoint"`
	Writable   *bool   `json:"writable" yaml:"writable"`
}

func toInstance(j limaInstanceJSON) provider.Instance {
	inst := provider.Instance{
		Name:     j.Name,
		Provider: "lima",
		Status:   toInstanceStatus(j.Status),
	}
	if t, err := time.Parse(time.RFC3339, j.CreatedAt); err == nil {
		inst.CreatedAt = t
	}
	return inst
}

func toInstanceStatus(s string) provider.InstanceStatus {
	switch strings.ToLower(s) {
	case "running":
		return provider.StatusRunning
	case "stopped":
		return provider.StatusStopped
	default:
		return provider.StatusUnknown
	}
}

// parseLimaList parses `limactl list --json` output, hedging a real open
// question about its exact shape: whether limactl emits one top-level
// JSON array, or JSON Lines (one object per line, a convention several
// other CLIs use for --json output). It tries the array shape first and
// falls back to line-by-line parsing, so it's correct regardless of which
// shape is real — verify against a live install and simplify once
// confirmed.
func parseLimaList(stdout []byte) ([]limaInstanceJSON, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var arr []limaInstanceJSON
	if err := json.Unmarshal(trimmed, &arr); err == nil {
		return arr, nil
	}

	var out []limaInstanceJSON
	for _, line := range bytes.Split(trimmed, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var j limaInstanceJSON
		if err := json.Unmarshal(line, &j); err != nil {
			return nil, fmt.Errorf("parsing limactl list output: %w", err)
		}
		out = append(out, j)
	}
	return out, nil
}
