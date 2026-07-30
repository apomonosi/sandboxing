package incus

import (
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
