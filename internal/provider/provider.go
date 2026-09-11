// Package provider defines the backend-agnostic interface agentctl
// dispatches through, plus the shared types every backend (Incus, Lima,
// Hyper-V) speaks. Commands in internal/cli never call a specific backend
// directly — they resolve a Provider from the registry and call this
// interface, checking Capabilities() first so unsupported operations are
// reported predictably instead of failing with a raw error.
package provider

import (
	"context"
	"io"
	"time"
)

// Provider is implemented once per OS-native backend. Every method must be
// safe to call even when the underlying feature isn't supported: callers
// are expected to consult Capabilities() before calling, but methods still
// return ErrNotAvailable/ErrUnderDevelopment if called anyway, so the
// interface itself never panics or silently no-ops.
type Provider interface {
	// Name identifies the backend, e.g. "incus", "lima", "hyperv".
	Name() string

	// Lifecycle
	Create(ctx context.Context, spec InstanceSpec) (*Instance, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string, opts StopOptions) error
	Delete(ctx context.Context, name string, force bool) error

	// Introspection
	List(ctx context.Context) ([]Instance, error)
	Status(ctx context.Context, name string) (*Instance, error)

	// Interactive / execution
	Exec(ctx context.Context, name string, opts ExecOptions) (int, error)
	Shell(ctx context.Context, name string, opts ShellOptions) error
	View(ctx context.Context, name string, opts ViewOptions) error

	// Snapshots
	SnapshotCreate(ctx context.Context, name, snapshotName string) error
	SnapshotList(ctx context.Context, name string) ([]Snapshot, error)
	SnapshotRestore(ctx context.Context, name, snapshotName string) error
	SnapshotDelete(ctx context.Context, name, snapshotName string) error

	// ApplyNetworkPolicy enforces a NetworkPolicy against an existing
	// instance. Pulled out as its own method (rather than being buried
	// inside Create) because it is the single most divergent piece of
	// behavior across providers and needs to be capability-gated
	// independently of "can this provider create instances at all".
	ApplyNetworkPolicy(ctx context.Context, name string, policy NetworkPolicy) error

	// ApplyMountPolicy replaces the instance's agentctl-managed mount set,
	// so `start --mount` can rebind a sandbox to a different project
	// without recreating it. Pulled out as its own method for the same
	// reason ApplyNetworkPolicy is: the mechanics diverge sharply per
	// backend (Incus removes and re-adds disk devices; Lima folds the
	// mount expressions into its own `start` invocation), and it needs
	// capability-gating independently of "can this provider create
	// instances at all".
	//
	// The instance is expected to be stopped: both backends bind mounts at
	// boot, so callers apply this immediately before Start. Providers must
	// only touch mounts agentctl itself added — a device or mount entry a
	// user configured by hand is never removed.
	ApplyMountPolicy(ctx context.Context, name string, mounts []Mount) error

	// Images
	ImagePull(ctx context.Context, ref string) error
	ImageBuild(ctx context.Context, spec ImageBuildSpec) (string, error)

	// Logs returns a reader over audit-relevant events for an instance.
	// First-pass implementations wrap whatever the provider natively
	// exposes (e.g. Incus ACL deny logs); see internal/audit.
	Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error)

	// Capabilities returns this provider's full capability table. Must
	// have a non-default entry for every Feature in AllFeatures.
	Capabilities() Table

	// SetAgentRequested records that name was created with agentName
	// requested for JIT install (`create --agent=<name>`), so a later
	// `start` can detect and retry an install that didn't finish. Tracked
	// at the backend level (e.g. Incus instance config), not by agentctl
	// itself, so it survives independently of whether the in-guest agent
	// is currently reachable. Providers that can't persist this (Lima,
	// Hyper-V stubs) may treat it as a no-op — the only consequence is
	// that PendingAgentInstall never has anything to report there.
	SetAgentRequested(ctx context.Context, name, agentName string) error
	// MarkAgentInstalled records that the agent requested via
	// SetAgentRequested finished installing successfully.
	MarkAgentInstalled(ctx context.Context, name string) error
	// PendingAgentInstall returns the agent name requested for name if
	// JIT install hasn't completed yet (e.g. a transient failure during
	// create), or "" if no agent was ever requested, or installation
	// already completed.
	PendingAgentInstall(ctx context.Context, name string) (string, error)
}

// InstanceSpec describes what to create.
type InstanceSpec struct {
	Name      string
	Image     string   // OCI/simplestreams ref, Lima template, or Hyper-V VHDX ref
	Profiles  []string // named profiles to apply, merged in order
	Overrides NetworkPolicy
	Resources ResourceLimits
	Mounts    []Mount
	// DefaultUser is the non-root user Shell/Exec should default to for
	// this instance. Empty means the provider picks its own generic
	// default (e.g. "agent") rather than root — providers that can't
	// provision a non-root user at all are unaffected by this field.
	DefaultUser string
}

// NetworkPolicy is the provider-facing view of profile.Policy's network
// section (internal/provider does not import internal/profile, to keep the
// dependency direction one-way: profile -> spec/cli -> provider).
type NetworkPolicy struct {
	DenyLAN bool
	Allow   []AllowRule
	Ports   []PortPublish
	// DNS controls name-resolution egress, which is infrastructure rather
	// than part of the domain allowlist: without it the allowlist cannot
	// work at all, since the guest has to resolve a domain before it can
	// reach the address the allowlist permits.
	DNS DNSPolicy
	// Unrestricted skips network policy enforcement entirely — no ACL is
	// created and none is attached, so the instance keeps the backend's
	// own default connectivity. An explicit, visible escape hatch for
	// debugging a sandbox that has locked itself out, in the same family
	// as --root and --allow-lan; never the default.
	Unrestricted bool
}

// DNSPolicy describes how much name-resolution egress a sandbox gets.
//
// The zero value allows DNS to any destination on port 53. That is
// deliberately permissive: DNS is a well-known exfiltration channel
// (arbitrary data encoded into subdomain labels of an attacker-controlled
// zone), so a sandbox that can reach any resolver can leak. The
// alternative — resolving only through the bridge's own dnsmasq — needs
// the bridge address, which varies per network and per address family and
// is the exact discovery that is easy to get wrong and leave a sandbox
// with no working DNS at all. Set Servers to narrow it once you know the
// resolver addresses for your setup.
type DNSPolicy struct {
	// Servers, when non-empty, restricts DNS egress to these addresses
	// instead of allowing it to any destination.
	Servers []string
	// Disabled removes DNS egress altogether. Only useful when every
	// destination the sandbox needs is reached through a proxy that
	// resolves on its behalf.
	Disabled bool
}

type AllowRule struct {
	Domain string
	Ports  []int
}

type PortPublish struct {
	HostPort  int
	GuestPort int
	Protocol  string // "tcp" (default) or "udp"
}

type ResourceLimits struct {
	CPUCores int
	Memory   string
	DiskSize string
}

// Mount is one host directory exposed inside the guest. Writable (rather
// than the inverse) is the polarity throughout agentctl, matching Lima's
// own `writable` field and its false-means-read-only default; the Incus
// backend is the single place that negates it, since `incus config device
// add` spells the same thing as `readonly=true`.
type Mount struct {
	HostPath  string `json:"host_path"`
	GuestPath string `json:"guest_path"`
	Writable  bool   `json:"writable"`
}

type ImageBuildSpec struct {
	BaseImage  string
	CloudInit  string // rendered cloud-init user-data
	OutputName string
}

// Instance is the provider-agnostic view of a running or stopped sandbox.
type Instance struct {
	Name      string         `json:"name"`
	Provider  string         `json:"provider"`
	Status    InstanceStatus `json:"status"`
	Image     string         `json:"image"`
	Profiles  []string       `json:"profiles"`
	CreatedAt time.Time      `json:"created_at"`
	IPs       []string       `json:"ips"`
	// Mounts are the host directories currently exposed to this instance,
	// read back from the backend rather than from the profile that created
	// it — mounts are sticky across boots (see ApplyMountPolicy), so the
	// backend is the only honest source for "what can this sandbox see
	// right now".
	Mounts []Mount `json:"mounts,omitempty"`
}

type InstanceStatus string

const (
	StatusRunning InstanceStatus = "running"
	StatusStopped InstanceStatus = "stopped"
	StatusUnknown InstanceStatus = "unknown"
)

type StopOptions struct {
	Force   bool
	Timeout time.Duration
}

type ExecOptions struct {
	Command []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	TTY     bool
	// Root, if true, runs as root instead of the instance's provisioned
	// non-root default user — an explicit, visible opt-out (like
	// --force-partial and --allow-lan), never removed outright.
	Root bool
}

type ShellOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Root, if true, runs as root instead of the instance's provisioned
	// non-root default user.
	Root bool
}

type ViewOptions struct {
	ReadOnly bool
}

type Snapshot struct {
	Name      string
	CreatedAt time.Time
}

type LogOptions struct {
	Network bool
	Exec    bool
	Since   time.Time
	Follow  bool
}
