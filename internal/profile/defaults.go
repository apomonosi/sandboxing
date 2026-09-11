package profile

import (
	"embed"
	"sort"
)

//go:embed embedded/*.yaml
var embeddedFS embed.FS

// Default is the profile applied when a command is given no --profile and
// no defaultProfile is configured. Named rather than spelled "default" at
// each call site so the fallback is greppable and can't drift.
const Default = "default"

// builtinProfiles maps a profile name to its raw embedded YAML. Populated
// once at init from embedded/*.yaml so `agentctl profile list` always has
// the base profiles ("default", "strict") and the ecosystem profiles
// available with no external files.
//
// The ecosystem profiles (python, node, go, rust) carry egress entries
// only — no resources, mounts or mountPolicy — so they layer on top of a
// base profile instead of competing with it. Two constraints make that
// work, and both are easy to break when adding another one:
//
//   - They must declare no mounts. Merge unions mount lists, so an
//     ecosystem profile that also mounted /workspace would collide with
//     the base profile's workspace mount and ResolveMounts would reject
//     the pair as overlapping.
//   - They must set denyLAN explicitly. Merge takes the override's
//     denyLAN as-is, so one that omitted it would resolve to false and
//     silently unblock the LAN when layered last.
var builtinProfiles = mustLoadBuiltins()

func mustLoadBuiltins() map[string][]byte {
	names := map[string]string{
		Default:  "embedded/default.yaml",
		"strict": "embedded/strict.yaml",
		"python": "embedded/python.yaml",
		"node":   "embedded/node.yaml",
		"go":     "embedded/go.yaml",
		"rust":   "embedded/rust.yaml",
	}
	out := make(map[string][]byte, len(names))
	for name, path := range names {
		data, err := embeddedFS.ReadFile(path)
		if err != nil {
			// Embedded assets are compiled into the binary; a missing file
			// here means the build itself is broken, not a runtime
			// condition callers can recover from.
			panic("profile: missing embedded builtin " + path + ": " + err.Error())
		}
		out[name] = data
	}
	return out
}

// BuiltinNames lists the names of embedded built-in profiles, e.g. for
// `agentctl profile list` to show alongside any file-based profiles.
// Sorted, so the listing is stable rather than following Go's randomized
// map iteration order.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtinProfiles))
	for n := range builtinProfiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
