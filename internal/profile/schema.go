// Package profile defines the YAML schema for agentctl's reusable,
// named security profiles: bundles of network policy, resource limits,
// and mount policy that admins author once and users reference by name
// (agentctl create --profile=default). A profile is the org-wide,
// distributable policy artifact; internal/spec's InstanceSpec is what one
// specific `create` invocation produces after merging a profile with any
// ad-hoc flags.
package profile

import "github.com/apomonosi/sandboxing/internal/provider"

// APIVersion and Kind are the only accepted values today. Checking them on
// load is a forward-compatibility guard, not current versioning support.
const (
	APIVersion = "agentctl.dev/v1"
	Kind       = "Profile"
)

// Profile is the top-level YAML document.
type Profile struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Policy   `yaml:"spec"`
}

type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Policy is the reusable payload: everything a Profile bundles, and also
// what an ad-hoc `create` invocation's flags produce as an override layer
// (see Merge).
type Policy struct {
	Network     NetworkPolicy `yaml:"network"`
	Resources   Resources     `yaml:"resources"`
	Mounts      []Mount       `yaml:"mounts"`
	MountPolicy MountPolicy   `yaml:"mountPolicy"`
	Console     Console       `yaml:"console"`
	// Packages are neutral tool names (internal/packages) installed into
	// the guest during create, before any agent install runs. Additive on
	// merge, like Network.Allow: a profile chain can only ever add tools,
	// never remove one an earlier layer asked for.
	//
	// Unlike every other field here, this one *adds software* to the
	// sandbox rather than restricting what the sandbox may do — so it is
	// worth being clear that it is not a security control and cannot be
	// used as one. See docs/user/profiles-and-policies.md.
	Packages []string `yaml:"packages"`
}

type NetworkPolicy struct {
	// DenyLAN blocks egress to RFC1918/link-local ranges so a sandbox
	// can't reach other devices on the same network. Defaults to true;
	// profiles/flags must opt out explicitly (--allow-lan).
	DenyLAN bool `yaml:"denyLAN"`
	// Allow is the egress allowlist; everything not matched is denied.
	Allow []AllowRule `yaml:"allow"`
	// AllowFile optionally points at an external policy file merged into
	// Allow at load time (--allow-file).
	AllowFile string `yaml:"allowFile"`
	// Ports are host:guest port publishes, Docker-style.
	Ports []PortPublish `yaml:"ports"`
	// DNS controls name-resolution egress. The zero value allows DNS to
	// any destination, because without it the allowlist below cannot
	// work: a guest has to resolve a domain before it can reach the
	// address the allowlist permits.
	DNS DNSPolicy `yaml:"dns"`
}

// Deliberately absent from this schema: the --no-network-policy escape
// hatch. It lives only on provider.NetworkPolicy, set per invocation by
// the CLI flag. Making it a profile field would let a distributed profile
// silently disable network enforcement for everyone who applies it, which
// is precisely the kind of quiet widening mountPolicy's narrow-only merge
// exists to prevent.

// DNSPolicy narrows or disables name-resolution egress. See
// provider.DNSPolicy for why allowing DNS to any destination is the
// default, and what it costs.
type DNSPolicy struct {
	// Servers, when non-empty, restricts DNS egress to these addresses.
	Servers []string `yaml:"servers"`
	// Disabled removes DNS egress altogether — only useful when a proxy
	// resolves on the sandbox's behalf.
	Disabled bool `yaml:"disabled"`
}

type AllowRule struct {
	Domain string `yaml:"domain"`
	Ports  []int  `yaml:"ports"`
}

type PortPublish struct {
	Host     int    `yaml:"host"`
	Guest    int    `yaml:"guest"`
	Protocol string `yaml:"protocol"` // "tcp" (default) or "udp"
}

type Resources struct {
	CPUCores int    `yaml:"cpuCores"`
	Memory   string `yaml:"memory"`   // e.g. "4GiB"
	DiskSize string `yaml:"diskSize"` // e.g. "20GiB"
}

// Mount is one host directory exposed inside the guest.
//
// The field names are agentctl's own rather than either backend's, because
// the two backends disagree: Lima calls them location/mountPoint, Incus
// calls them source/path. Writable, though, matches Lima exactly — same
// name, same false-means-read-only default — since a sandbox that silently
// gets write access to a host directory is the failure worth defaulting
// against.
type Mount struct {
	// HostPath supports "~" expansion and a "{{.Name}}" template
	// substituted with the instance name; see ResolveMounts, which is the
	// only place either is interpreted.
	HostPath  string `yaml:"hostPath"`
	GuestPath string `yaml:"guestPath"`
	Writable  bool   `yaml:"writable"`
}

// MountPolicy constrains which host directories may be mounted at all. It
// is the admin-facing half of mount policy: profiles are the
// org-distributed artifact (see docs/admin/distributing-profiles.md), and
// no CLI flag can widen any field here — --mount only ever produces Mount
// entries, which are then checked against this.
type MountPolicy struct {
	// AllowedRoots, when non-empty, requires every resolved host path to
	// live under one of these directories. Empty means unconstrained,
	// which is the pre-existing behavior for profiles that don't set it.
	AllowedRoots []string `yaml:"allowedRoots"`
	// DenyPaths are never mountable, even under an AllowedRoots entry.
	// Merged additively across profiles, and unioned with a hard-coded set
	// (see alwaysDenied) that profile authors can't forget.
	DenyPaths []string `yaml:"denyPaths"`
	// AllowHome permits mounting $HOME itself, along with "/" and system
	// directories. Defaults to false: handing an agent the whole home
	// directory is the specific failure this package exists to prevent,
	// and Lima's own default templates do exactly that.
	//
	// A pointer, unlike every other bool in this file, so that "not set"
	// is distinguishable from "set to false". Merge layers a zero-valued
	// override on top of a profile on every create (that's how ad-hoc
	// flags work), and with a plain bool that layer would silently reset
	// an org profile's explicit choice. It also lets an explicit false
	// beat a later true — see mergeMountPolicy.
	AllowHome *bool `yaml:"allowHome"`
	// CreateMissing creates a missing host directory (mode 0700) instead
	// of failing. Defaults to false; the built-in profiles set it, since
	// their per-instance workspace path can't exist before first use.
	CreateMissing bool `yaml:"createMissing"`
}

// HomeAllowed reports whether mounting $HOME itself is permitted, applying
// the false default for an unset AllowHome.
func (p MountPolicy) HomeAllowed() bool {
	return p.AllowHome != nil && *p.AllowHome
}

type Console struct {
	// Viewer is advisory ("spice" | "vnc" | "native"); the provider
	// decides the actual mechanism it's capable of.
	Viewer string `yaml:"viewer"`
}

// ToProviderNetworkPolicy converts the profile's network section into the
// provider-agnostic type Provider.ApplyNetworkPolicy expects.
func (n NetworkPolicy) ToProviderNetworkPolicy() provider.NetworkPolicy {
	allow := make([]provider.AllowRule, len(n.Allow))
	for i, a := range n.Allow {
		allow[i] = provider.AllowRule{Domain: a.Domain, Ports: append([]int(nil), a.Ports...)}
	}
	ports := make([]provider.PortPublish, len(n.Ports))
	for i, p := range n.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		ports[i] = provider.PortPublish{HostPort: p.Host, GuestPort: p.Guest, Protocol: proto}
	}
	return provider.NetworkPolicy{
		DenyLAN: n.DenyLAN,
		Allow:   allow,
		Ports:   ports,
		DNS: provider.DNSPolicy{
			Servers:  append([]string(nil), n.DNS.Servers...),
			Disabled: n.DNS.Disabled,
		},
	}
}

// ToProviderResourceLimits converts Resources into provider.ResourceLimits.
func (r Resources) ToProviderResourceLimits() provider.ResourceLimits {
	return provider.ResourceLimits{CPUCores: r.CPUCores, Memory: r.Memory, DiskSize: r.DiskSize}
}

// Mounts are deliberately not convertible to provider.Mount by a plain
// field copy the way the two types above are: a Mount's HostPath is a
// template ("~", "{{.Name}}") that has to be expanded, canonicalized and
// checked against MountPolicy first. ResolveMounts (mount.go) is the only
// path from profile.Mount to provider.Mount.
