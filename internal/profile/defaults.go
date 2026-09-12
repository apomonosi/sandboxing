package profile

import "embed"

//go:embed embedded/*.yaml
var embeddedFS embed.FS

// builtinProfiles maps a profile name to its raw embedded YAML. Populated
// once at init from embedded/*.yaml so `agentctl profile list` always has
// a usable set available with no external files.
//
// The four fall along two axes. default and strict differ only in how much
// they permit (strict pre-approves no egress and confines every mount);
// terminal and gui differ from default only in what they *install* — the
// same policy, plus a toolchain. Nothing here inherits from anything else:
// each file is a complete policy an admin can read top to bottom, which
// matters more for an artifact whose job is to be audited than the
// duplication costs.
var builtinProfiles = mustLoadBuiltins()

func mustLoadBuiltins() map[string][]byte {
	names := map[string]string{
		"default":  "embedded/default.yaml",
		"strict":   "embedded/strict.yaml",
		"terminal": "embedded/terminal.yaml",
		"gui":      "embedded/gui.yaml",
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
