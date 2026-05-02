package cli

import (
	"errors"
	"testing"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/state"
)

// TestExitCodeMatrix is the PR6 + R29 fitness function: every error class
// maps to a stable, documented exit code. The matrix is frozen — adding a
// new error variant means choosing one of the four codes here.
func TestExitCodeMatrix(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		// 0 — success
		{"nil", nil, ExitOK},

		// 1 — user error class
		{"CliUserError default", &CliUserError{Message: "bad", ExitCode: ExitUser}, ExitUser},
		{"missing-profile-typo", pipelineerrors.NewMissingProfileError("foo", "", nil), ExitUser},
		{"invalid-manifest", pipelineerrors.NewInvalidManifestError("p.json", "syntax"), ExitUser},
		{"root-markers-missing", pipelineerrors.NewRootClaudeMdMarkersMissingError("/root"), ExitUser},

		// 2 — system error class
		{"CliNotImplementedError", &CliNotImplementedError{Verb: "x", Owner: "y"}, ExitSystem},
		{"plain error", errors.New("boom"), ExitSystem},
		{"merge-read-failed", pipelineerrors.NewMergeReadFailedError("a", "b", "c", "d"), ExitSystem},
		{"invalid-settings", pipelineerrors.NewInvalidSettingsJsonError("a", "b", "c"), ExitSystem},

		// 3 — conflict class
		{"cycle", pipelineerrors.NewCycleError([]string{"a", "b", "a"}), ExitConflict},
		{"missing-include", pipelineerrors.NewMissingIncludeError("x", "y", "z"), ExitConflict},
		{"path-traversal", pipelineerrors.NewPathTraversalError("x", "y", "z"), ExitConflict},
		{"resolver-conflict", pipelineerrors.NewConflictError("a", []string{"x", "y"}), ExitConflict},
		{"missing-profile-structural", pipelineerrors.NewMissingProfileError("foo", "child", nil), ExitConflict},
		{"lock-held", &state.LockHeldError{LockPath: "/lock", HolderPID: 1, HolderTimestamp: "now"}, ExitConflict},
		{"schema-too-new", &state.SchemaTooNewError{Path: "/s", OnDisk: 2, OnDiskRaw: "2", BinMaxKnown: 1}, ExitConflict},

		// CliUserError with explicit conflict code
		{"CliUserError conflict", &CliUserError{Message: "bad", ExitCode: ExitConflict}, ExitConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCodeFor(tc.err); got != tc.want {
				t.Fatalf("%s: want %d, got %d", tc.name, tc.want, got)
			}
		})
	}
}

// TestExitCodeUserVsConflictForMissingProfile is the discriminator test:
// a top-level CLI typo (referencedBy=="") exits 1; a structural manifest
// failure (referencedBy set) exits 3. Lifted directly from src/cli/exit.ts.
func TestExitCodeUserVsConflictForMissingProfile(t *testing.T) {
	typo := pipelineerrors.NewMissingProfileError("ghst", "", nil)
	if got := ExitCodeFor(typo); got != ExitUser {
		t.Fatalf("typo: want %d (user), got %d", ExitUser, got)
	}
	structural := pipelineerrors.NewMissingProfileError("nope", "child-profile", nil)
	if got := ExitCodeFor(structural); got != ExitConflict {
		t.Fatalf("structural: want %d (conflict), got %d", ExitConflict, got)
	}
}
