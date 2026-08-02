package lima

import (
	"bytes"
	"encoding/json"
	"fmt"
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
// NOTE: field names/casing below have not been confirmed against a real
// `limactl list --json` invocation in this environment (verify before
// relying on this). IPAddress in particular is a best-effort guess: the
// only confirmed-real evidence in this repo is capability.go's existing
// ManualWorkaround Plan text, which uses `limactl list --format
// '{{.Name}} {{.IPAddress}}'` — that confirms an .IPAddress Go-template
// field exists, but --format template field names and --json key names
// aren't guaranteed to share spelling/casing, so IPs are left unpopulated
// here rather than guessed; wire this up once confirmed live.
type limaInstanceJSON struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
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
