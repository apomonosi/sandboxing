package profile

// Merge layers override on top of base and returns the result. It is used
// to combine a loaded --profile with ad-hoc create-time flags
// (--allow, --port, --deny-lan/--allow-lan, ...): the CLI layer builds
// override from whichever flags the user actually passed (defaulting
// zero-value fields to base's values before calling Merge, since Go's
// plain bool/int/string fields can't distinguish "flag not passed" from
// "flag passed as the zero value") and Merge is then a straightforward,
// deterministic combination:
//
//   - Network.Allow, Network.Ports, Mounts: additive union (override
//     entries are appended after base's, duplicates are the caller's
//     problem to avoid via Validate)
//   - Network.DenyLAN: override's value is taken as-is (the CLI layer is
//     responsible for resolving --deny-lan/--allow-lan against the
//     profile's value before constructing override)
//   - Resources, Console.Viewer: override's value wins whenever it is
//     non-zero/non-empty, otherwise base's value is kept
func Merge(base, override Policy) Policy {
	out := base

	out.Network.DenyLAN = override.Network.DenyLAN
	out.Network.Allow = append(append([]AllowRule(nil), base.Network.Allow...), override.Network.Allow...)
	out.Network.Ports = append(append([]PortPublish(nil), base.Network.Ports...), override.Network.Ports...)
	if override.Network.AllowFile != "" {
		out.Network.AllowFile = override.Network.AllowFile
	}

	if override.Resources.CPUCores != 0 {
		out.Resources.CPUCores = override.Resources.CPUCores
	}
	if override.Resources.Memory != "" {
		out.Resources.Memory = override.Resources.Memory
	}
	if override.Resources.DiskSize != "" {
		out.Resources.DiskSize = override.Resources.DiskSize
	}

	out.Mounts = append(append([]Mount(nil), base.Mounts...), override.Mounts...)

	if override.Console.Viewer != "" {
		out.Console.Viewer = override.Console.Viewer
	}

	return out
}
