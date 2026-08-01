package provider

import (
	"context"
	"strings"
)

// Command is a single OS-native command agentctl would execute — the
// binary plus its arguments, exactly as passed to os/exec. It exists so
// --preview/--dry-run can show users precisely what the wrapper does,
// including people who'd rather not install agentctl on their primary
// desktop at all and just want to copy the underlying incus/limactl/
// PowerShell invocation and run it by hand.
type Command struct {
	Binary string
	Args   []string
}

// String renders the command as a single, copy-pasteable line using
// POSIX shell quoting — correct for Incus's and Lima's own CLIs. Hyper-V's
// PowerShell backend will need its own quoting convention once that
// provider is implemented; this is called out in the doc comment on
// CommandPreviewer below.
func (c Command) String() string {
	parts := make([]string, 0, len(c.Args)+1)
	parts = append(parts, shellQuote(c.Binary))
	for _, a := range c.Args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '=' || r == ',' || r == '*':
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CommandPreviewer is implemented by providers that can describe, without
// executing them, the exact OS-native command(s) a given operation would
// run. It's optional: a provider that doesn't implement it (Lima and
// Hyper-V, currently — they're capability-table-driven stubs with no real
// commands to preview yet) simply has nothing to show. In practice this
// never needs special-casing in internal/cli, because the capability gate
// already blocks Create/Start/etc. on those providers with an
// UnderDevelopment message before --preview would ever be consulted —
// preview only matters for operations a provider actually supports.
//
// Every method here mirrors a Provider method that shells out to a real
// command; implementations build the Commands by calling the exact same
// argument-construction helpers the live method uses (see
// internal/provider/incus/translate.go), so preview output can never drift
// from what actually executes.
type CommandPreviewer interface {
	PreviewCreate(spec InstanceSpec) []Command
	PreviewStart(name string) []Command
	PreviewStop(name string, opts StopOptions) []Command
	PreviewDelete(name string, force bool) []Command
	// PreviewExec and PreviewShell need a live, read-only lookup (which
	// non-root user is provisioned for this instance) to show the
	// --user/--cwd flags that will actually be used, so — unlike every
	// other method here — they take a context and can fail. This is a
	// deliberately asymmetric interface: only these two methods need
	// live data, so only these two pay for it.
	PreviewExec(ctx context.Context, name string, opts ExecOptions) ([]Command, error)
	PreviewShell(ctx context.Context, name string, opts ShellOptions) ([]Command, error)
	PreviewView(name string, opts ViewOptions) []Command
	PreviewImagePull(ref string) []Command
}
