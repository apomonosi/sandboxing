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
//   - Packages: additive union, deduped
//   - Network.DenyLAN: override's value is taken as-is (the CLI layer is
//     responsible for resolving --deny-lan/--allow-lan against the
//     profile's value before constructing override)
//   - Resources, Console.Viewer: override's value wins whenever it is
//     non-zero/non-empty, otherwise base's value is kept
//   - MountPolicy: narrow-only, see mergeMountPolicy
func Merge(base, override Policy) Policy {
	out := base

	out.Network.DenyLAN = override.Network.DenyLAN
	out.Network.Allow = append(append([]AllowRule(nil), base.Network.Allow...), override.Network.Allow...)
	out.Network.Ports = append(append([]PortPublish(nil), base.Network.Ports...), override.Network.Ports...)
	if override.Network.AllowFile != "" {
		out.Network.AllowFile = override.Network.AllowFile
	}

	// DNS narrows only, like mountPolicy: an override that names servers
	// replaces a broader base (empty means "any destination", so naming
	// any is a tightening), and Disabled is OR'd so the most restrictive
	// layer in the chain wins.
	if len(override.Network.DNS.Servers) > 0 {
		out.Network.DNS.Servers = append([]string(nil), override.Network.DNS.Servers...)
	}
	out.Network.DNS.Disabled = base.Network.DNS.Disabled || override.Network.DNS.Disabled

	if override.Resources.CPUCores != 0 {
		out.Resources.CPUCores = override.Resources.CPUCores
	}
	if override.Resources.Memory != "" {
		out.Resources.Memory = override.Resources.Memory
	}
	if override.Resources.DiskSize != "" {
		out.Resources.DiskSize = override.Resources.DiskSize
	}

	// Additive and deduped, like DenyPaths: two layers both asking for
	// `git` should install it once, and no layer can withdraw a tool an
	// earlier one asked for. (Nothing here is load-bearing for security —
	// packages add software, they don't restrict anything — so plain
	// accumulation is the right semantic rather than mountPolicy's
	// narrow-only asymmetry.)
	out.Packages = dedupe(append(append([]string(nil), base.Packages...), override.Packages...))

	out.Mounts = append(append([]Mount(nil), base.Mounts...), override.Mounts...)
	out.MountPolicy = mergeMountPolicy(base.MountPolicy, override.MountPolicy)

	if override.Console.Viewer != "" {
		out.Console.Viewer = override.Console.Viewer
	}

	return out
}

// mergeMountPolicy combines two mount policies so that layering can only
// ever *narrow* what is mountable, never widen it. That asymmetry is the
// whole point: profiles are the org-distributed artifact, and a personal
// profile or a later --profile in the chain must not be able to unlock a
// directory the org profile ruled out.
//
//   - AllowedRoots: intersection. Once any layer constrains the roots, no
//     later layer can add one back. An empty (unconstrained) override
//     leaves base's roots alone rather than clearing them.
//   - DenyPaths: union — denials only ever accumulate.
//   - AllowHome: an explicit false anywhere in the chain wins; otherwise
//     an explicit true wins; otherwise it stays unset (and defaults to
//     false). So a permissive later profile cannot re-open the home
//     directory once an org profile has ruled it out, while a zero-valued
//     override layer — which every create produces — leaves an explicit
//     choice untouched.
//   - CreateMissing: either layer may enable it; it is a convenience, not
//     a boundary (a created directory is empty, and still has to pass
//     AllowedRoots and DenyPaths like any other).
func mergeMountPolicy(base, override MountPolicy) MountPolicy {
	out := MountPolicy{
		DenyPaths:     dedupe(append(append([]string(nil), base.DenyPaths...), override.DenyPaths...)),
		AllowHome:     mergeAllowHome(base.AllowHome, override.AllowHome),
		CreateMissing: base.CreateMissing || override.CreateMissing,
	}
	switch {
	case len(base.AllowedRoots) == 0:
		out.AllowedRoots = append([]string(nil), override.AllowedRoots...)
	case len(override.AllowedRoots) == 0:
		out.AllowedRoots = append([]string(nil), base.AllowedRoots...)
	default:
		out.AllowedRoots = intersect(base.AllowedRoots, override.AllowedRoots)
	}
	return out
}

func mergeAllowHome(base, override *bool) *bool {
	deny := false
	if (base != nil && !*base) || (override != nil && !*override) {
		return &deny
	}
	if base != nil || override != nil {
		allow := true
		return &allow
	}
	return nil
}

func intersect(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if inB[s] {
			out = append(out, s)
		}
	}
	return out
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
