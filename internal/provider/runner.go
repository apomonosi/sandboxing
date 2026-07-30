package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
)

// Runner executes an OS-native CLI command. Every real provider (Incus's
// `incus`, Lima's `limactl`, Hyper-V's PowerShell cmdlets) shells out to
// its backend's own CLI rather than linking a backend-specific SDK — this
// keeps agentctl's dependency footprint small and keeps the three
// providers architecturally consistent with each other. Providers accept a
// Runner in their constructor so tests can inject a fake one instead of
// executing real binaries.
type Runner interface {
	// Run executes a command to completion and captures its output —
	// suitable for one-shot lifecycle/query commands (init, start, stop,
	// list, ...).
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)

	// RunStream executes a command with stdin/stdout/stderr connected
	// live to the given streams, for interactive or long-running use
	// (exec, shell). Returns the process's exit code and any error
	// starting/waiting for it.
	RunStream(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error)
}

// ExecRunner is the production Runner: it actually executes the named
// binary via os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func (ExecRunner) RunStream(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}
