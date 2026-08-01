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

type Mount struct {
	HostPath  string
	GuestPath string
	ReadOnly  bool
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
