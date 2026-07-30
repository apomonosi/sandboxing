package provider

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by Provider methods as a defensive fallback.
// The primary enforcement point for unsupported operations is the
// capability check internal/cli performs before dispatch (see
// internal/cli/capability_gate.go); these exist so that direct callers of
// the Provider interface (including tests) never get a panic or a silent
// no-op if they skip that check.
var (
	ErrNotAvailable     = errors.New("operation not available on this provider")
	ErrUnderDevelopment = errors.New("operation is under development for this provider")
	ErrNotFound         = errors.New("instance not found")
	ErrAlreadyExists    = errors.New("instance already exists")
)

// StatusError converts a non-Supported Capability into the sentinel error
// a stub/partial Provider method should return if called directly (tests,
// or a caller that skipped the capability check internal/cli normally
// performs before dispatch).
func StatusError(c Capability) error {
	switch c.Status {
	case Supported:
		return nil
	case UnderDevelopment:
		return fmt.Errorf("%w: %s", ErrUnderDevelopment, c.Message)
	default: // NotAvailable, ManualWorkaround
		return fmt.Errorf("%w: %s", ErrNotAvailable, c.Message)
	}
}
