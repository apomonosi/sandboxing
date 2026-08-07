package incus

import (
	"sort"
	"strings"
	"time"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// The following types are a deliberately lenient, partial view of
// `incus list --format json` / `incus list <name> --format json` output:
// only the fields agentctl actually needs are declared, and every
// conversion in toInstance is defensive (missing/malformed fields degrade
// to zero values rather than erroring), since the exact JSON shape should
// be confirmed against a live daemon (see the NOTE in translate.go).
type instanceJSON struct {
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	CreatedAt string            `json:"created_at"`
	Profiles  []string          `json:"profiles"`
	Config    map[string]string `json:"config"`
	State     *instanceState    `json:"state"`
	// Devices is the instance's *own* device map, deliberately not
	// expanded_devices: profile-inherited devices must never be removed by
	// a mount reconfigure, and agentctl only ever adds instance-local
	// ones.
	Devices map[string]map[string]string `json:"devices"`
}

type instanceState struct {
	Network map[string]networkState `json:"network"`
}

type networkState struct {
	Addresses []networkAddress `json:"addresses"`
}

type networkAddress struct {
	Family  string `json:"family"`
	Address string `json:"address"`
	Scope   string `json:"scope"`
}

func toInstance(j instanceJSON) provider.Instance {
	inst := provider.Instance{
		Name:     j.Name,
		Provider: "incus",
		Status:   toInstanceStatus(j.Status),
		Image:    j.Config["image.description"],
		Profiles: append([]string(nil), j.Profiles...),
	}
	if t, err := time.Parse(time.RFC3339, j.CreatedAt); err == nil {
		inst.CreatedAt = t
	}
	if j.State != nil {
		for _, net := range j.State.Network {
			for _, addr := range net.Addresses {
				if addr.Family == "inet" && addr.Scope == "global" {
					inst.IPs = append(inst.IPs, addr.Address)
				}
			}
		}
	}
	inst.Mounts = toMounts(j.Devices)
	return inst
}

// toMounts reports the host directories agentctl has exposed to this
// instance, read back from Incus's own device map rather than from the
// profile that created it — mounts are sticky across boots, so the daemon
// is the only honest source for what a sandbox can currently see.
//
// Only agentctl-managed devices are reported: a disk device the user
// attached by hand isn't agentctl's to describe, and claiming it would
// make `status` look like it manages more than it does.
func toMounts(devices map[string]map[string]string) []provider.Mount {
	var out []provider.Mount
	for name, dev := range devices {
		if !strings.HasPrefix(name, mountDevicePrefix) || dev["type"] != "disk" {
			continue
		}
		out = append(out, provider.Mount{
			HostPath:  dev["source"],
			GuestPath: dev["path"],
			Writable:  dev["readonly"] != "true",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GuestPath < out[j].GuestPath })
	return out
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
