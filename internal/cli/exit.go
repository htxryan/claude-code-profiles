// Exit-code policy. Mirrors src/cli/exit.ts. The matrix is frozen — new
// error variants must route through one of the four codes via ExitCodeFor.
package cli

import (
	"errors"

	"github.com/htxryan/claude-code-config-profiles/internal/cli/commands"
	"github.com/htxryan/claude-code-config-profiles/internal/cli/service"
	pipelineerrors "github.com/htxryan/claude-code-config-profiles/internal/errors"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

// Exit codes are the public CLI surface (E5 fitness function).
const (
	ExitOK       = 0
	ExitUser     = 1
	ExitSystem   = 2
	ExitConflict = 3
)

// CliUserError is a marker thrown by command handlers to signal a user-error
// exit without a specific error subclass (drift abort, validation fail).
type CliUserError struct {
	Message  string
	ExitCode int
}

func (e *CliUserError) Error() string { return e.Message }

// NewUserError builds a *CliUserError at the user-error code (1).
func NewUserError(msg string) *CliUserError {
	return &CliUserError{Message: msg, ExitCode: ExitUser}
}

// NewConflictError builds a *CliUserError at the conflict code (3). Used
// when the failure is structural rather than typo-class.
func NewConflictError(msg string) *CliUserError {
	return &CliUserError{Message: msg, ExitCode: ExitConflict}
}

// CliNotImplementedError marks "not yet implemented in this epic" stubs.
// Maps to exit 2 (system error class) — distinct from bad user input.
type CliNotImplementedError struct {
	Verb  string
	Owner string
}

func (e *CliNotImplementedError) Error() string {
	return e.Verb + " is not yet implemented (owned by " + e.Owner + ")"
}

// ExitCodeFor maps any error returned by the command pipeline to a CLI
// exit code. The mapping mirrors src/cli/exit.ts.
func ExitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	// CliUserError carries its own code (lets callers force exit 1 vs 3).
	var cue *CliUserError
	if errors.As(err, &cue) {
		return cue.ExitCode
	}
	// Swap-orchestrator abort signal — mirrors TS CliUserError default.
	if service.IsSwapAbort(err) {
		return ExitUser
	}
	// Per-command user error (commands package can't import cli/).
	var ue *commands.UserError
	if errors.As(err, &ue) {
		if ue.ExitCode != 0 {
			return ue.ExitCode
		}
		return ExitUser
	}
	var nie *CliNotImplementedError
	if errors.As(err, &nie) {
		return ExitSystem
	}
	// Lock-held: a peer process holds the project, conceptually "no slot".
	var lhe *state.LockHeldError
	if errors.As(err, &lhe) {
		return ExitConflict
	}
	// Schema-too-new: refuse to operate. Conflict class — the user must
	// upgrade c3p, not edit input.
	var stne *state.SchemaTooNewError
	if errors.As(err, &stne) {
		return ExitConflict
	}
	// Pipeline errors: split by code so the matrix matches TS exit.ts.
	if pe := pipelineerrors.AsPipelineError(err); pe != nil {
		switch pe.ErrorCode() {
		case pipelineerrors.CodeMissingProfile:
			// Top-level CLI typo (referencedBy=="") → 1; structural manifest
			// failure → 3. Inspect the concrete error to discriminate.
			var mpe *pipelineerrors.MissingProfileError
			if errors.As(err, &mpe) {
				if mpe.ReferencedBy == "" {
					return ExitUser
				}
				return ExitConflict
			}
			return ExitConflict
		case pipelineerrors.CodeCycle,
			pipelineerrors.CodeMissingInclude,
			pipelineerrors.CodeConflict,
			pipelineerrors.CodePathTraversal:
			return ExitConflict
		case pipelineerrors.CodeInvalidManifest:
			// Structural manifest fault — user remediation, but exits 1 to
			// match TS validate semantics (a single bad profile.json doesn't
			// shut down the whole pipeline at conflict-class).
			return ExitUser
		case pipelineerrors.CodeRootMarkers:
			// Materialize-phase markers-missing — user runs `c3p init` to fix,
			// so it's user-error class (TS exit.ts MaterializeError → 1).
			return ExitUser
		case pipelineerrors.CodeInvalidSettings,
			pipelineerrors.CodeMergeReadFailed:
			// Merge-phase faults — runtime drift, not config typo.
			return ExitSystem
		default:
			return ExitSystem
		}
	}
	return ExitSystem
}
