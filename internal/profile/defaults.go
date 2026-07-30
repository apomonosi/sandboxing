package profile

import "embed"

//go:embed embedded/*.yaml
var embeddedFS embed.FS

// builtinProfiles maps a profile name to its raw embedded YAML. Populated
// once at init from embedded/*.yaml so `agentctl profile list` always has
// at least "default" and "strict" available with no external files.
var builtinProfiles = mustLoadBuiltins()

func mustLoadBuiltins() map[string][]byte {
	names := map[string]string{
		"default": "embedded/default.yaml",
		"strict":  "embedded/strict.yaml",
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
func BuiltinNames() []string {
	names := make([]string, 0, len(builtinProfiles))
	for n := range builtinProfiles {
		names = append(names, n)
	}
	return names
}
