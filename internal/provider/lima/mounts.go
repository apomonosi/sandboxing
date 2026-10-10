package lima

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mounts Lima adds behind agentctl's back
//
// A sandbox should see exactly the host directories agentctl mounted into
// it — named at create or `start --mount` time and checked against the
// profile's mountPolicy (see internal/profile.ResolveMounts) — and nothing
// else. buildMountExpressions makes the instance's own lima.yaml say
// exactly that, but Lima also merges the mounts in
// $LIMA_HOME/_config/default.yaml and override.yaml into every instance,
// each time it loads one, and no limactl flag opts an instance out. Those
// files can add any host directory, past every mountPolicy check — the
// home directory, ~/.ssh, ~/.lima — or, since Lima merges mounts by guest
// mount point, swap the host directory behind one of the instance's own
// mounts, or make it writable. (Verified against Lima v2.2.1: FillDefault
// in pkg/limayaml/defaults.go, and `limactl list --json` from a v2.2.1
// build.)
//
// So before every start, Start compares what Lima will actually mount —
// the effective configuration `limactl list --json` reports — with the
// instance's own lima.yaml, and won't boot an instance that would see
// more. It refuses rather than warns: a warning scrolls past, and the
// agent can read the directory as soon as the guest is up.

// checkMounts returns an error describing the first mount in j's
// effective configuration that the instance's own lima.yaml doesn't
// declare — matched by guest mount point and host directory, and writable
// only if declared writable — or nil if there's none. A declared mount
// that's missing from the effective configuration, or read-only there,
// isn't an error: that shares less, not more.
func checkMounts(j limaInstanceJSON) error {
	if j.Config == nil {
		return fmt.Errorf("could not read Lima's effective configuration for %q to check which host directories it would mount", j.Name)
	}
	if len(j.Config.Mounts) == 0 {
		return nil
	}
	own, err := instanceMounts(j.Dir)
	if err != nil {
		return fmt.Errorf("reading Lima instance %q's own mounts, to check what it would mount: %w", j.Name, err)
	}
	for _, m := range j.Config.Mounts {
		location, mountPoint, writable := resolveMount(m)
		if declared(location, mountPoint, writable, own) {
			continue
		}
		access := "read-only"
		if writable {
			access = "writable"
		}
		cfgDir := j.configDir()
		return fmt.Errorf("Lima would mount %s at %s (%s) in instance %q, beyond what the instance's own configuration mounts, so agentctl won't start it. "+
			"Lima merges the mounts in %s and %s into every instance it starts: remove the entry there. "+
			"To give this sandbox a directory, use `agentctl start %s --mount` instead",
			location, mountPoint, access, j.Name, filepath.Join(cfgDir, "default.yaml"), filepath.Join(cfgDir, "override.yaml"), j.Name)
	}
	return nil
}

func declared(location, mountPoint string, writable bool, own []limaMountJSON) bool {
	for _, o := range own {
		ownLocation, ownMountPoint, ownWritable := resolveMount(o)
		if ownMountPoint == mountPoint && ownLocation == location && (ownWritable || !writable) {
			return true
		}
	}
	return false
}

// instanceMounts returns the mounts in the instance's own lima.yaml, in
// dir — what create and ApplyMountPolicy wrote, before Lima merges in
// default.yaml and override.yaml.
func instanceMounts(dir string) ([]limaMountJSON, error) {
	if dir == "" {
		return nil, errors.New("limactl list reported no instance directory")
	}
	path := filepath.Join(dir, "lima.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var y struct {
		Mounts []limaMountJSON `yaml:"mounts"`
	}
	if err := yaml.Unmarshal(data, &y); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return y.Mounts, nil
}

// resolveMount returns m's host directory, guest mount point and
// writability the way Lima resolves them: a leading "~" expanded and the
// path cleaned, the mount point defaulting to the host directory, and
// read-only unless writable (see FillDefault and localpathutil.Expand in
// Lima v2.2.1). The effective configuration comes already resolved; a
// lima.yaml can still say "~", as the stock templates' home-directory
// mount does. Lima's {{...}} path templates aren't expanded: agentctl
// writes resolved paths, and one written by hand that uses them just
// fails the comparison, which refuses the start rather than allowing it.
func resolveMount(m limaMountJSON) (location, mountPoint string, writable bool) {
	location = filepath.Clean(m.Location)
	if location == "~" || strings.HasPrefix(location, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			location = filepath.Join(home, location[1:])
		}
	}
	mountPoint = location
	if m.MountPoint != nil {
		mountPoint = *m.MountPoint
	}
	return location, mountPoint, m.Writable != nil && *m.Writable
}
