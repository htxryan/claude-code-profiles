// User-error helpers local to commands/. Defines a minimal sentinel that
// the cli package's ExitCodeFor maps to ExitUser=1. We keep this here
// (rather than reusing cli.CliUserError) to avoid a circular import.
package commands

import "fmt"

// UserError is a marker the parent cli package recognises (via errors.As)
// and routes to ExitUser. The presence of an ExitCode field mirrors
// cli.CliUserError so the dispatch can treat them the same way.
type UserError struct {
	Message  string
	ExitCode int
}

func (e *UserError) Error() string { return e.Message }

func userErrorf(format string, args ...interface{}) *UserError {
	return &UserError{Message: fmt.Sprintf(format, args...), ExitCode: 1}
}
