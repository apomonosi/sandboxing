package cli

import "errors"

// ExitError carries an explicit process exit code through Cobra's error
// return path. Exit code convention (documented in
// docs/user/cli-reference.md and docs/user/troubleshooting.md):
//
//	0  success
//	1  generic runtime error
//	2  capability gap (NotAvailable / UnderDevelopment)
//	3  manual-workaround-required-but-not-forced
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func newExitError(code int, msg string) *ExitError {
	return &ExitError{Code: code, Err: errors.New(msg)}
}
