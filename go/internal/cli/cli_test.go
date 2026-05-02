package cli

import (
	"bytes"
	stderrors "errors"
	"strings"
	"testing"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version"}, "1.2.3", &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("--version: want %d, got %d (stderr=%q)", ExitOK, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "1.2.3") {
		t.Fatalf("--version stdout missing version: %q", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"nope"}, "0.0.0", &stdout, &stderr)
	// Parse-time errors are user-error class (1) per TS exit.ts. The TS bin
	// surfaces argv-shape failures as exit 1 so scripts can `c3p use ci ||
	// retry-with-fixed-args`. Cobra's exit-2 "usage error" semantics are
	// gone with the hand-rolled parser.
	if code != ExitUser {
		t.Fatalf("unknown command: want %d, got %d (stderr=%q)", ExitUser, code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr missing 'unknown command': %q", stderr.String())
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--help"}, "0.0.0", &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("--help: want %d, got %d (stderr=%q)", ExitOK, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "c3p") {
		t.Fatalf("--help stdout missing banner: %q", stdout.String())
	}
}

func TestRunHelloExitsZero(t *testing.T) {
	// hello is the smoke-test verb. With D7 it actually prints a greeting
	// rather than returning ErrNotImplemented.
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hello"}, "0.0.0", &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("hello: want %d, got %d (stderr=%q)", ExitOK, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Hello") {
		t.Fatalf("hello stdout missing greeting: %q", stdout.String())
	}
}

func TestExitCodeForPipelineError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"cycle", pipelineerrors.NewCycleError([]string{"a", "a"}), ExitConflict},
		{"conflict", pipelineerrors.NewConflictError("x", []string{"a", "b"}), ExitConflict},
		{"missingProfileTopLevel", pipelineerrors.NewMissingProfileError("x", "", nil), ExitUser},
		{"missingProfileStructural", pipelineerrors.NewMissingProfileError("x", "child", nil), ExitConflict},
		{"pathTraversal", pipelineerrors.NewPathTraversalError("x", "y", "z"), ExitConflict},
		{"plainError", stderrors.New("boom"), ExitSystem},
		{"userError", &CliUserError{Message: "bad", ExitCode: ExitUser}, ExitUser},
		{"notImplemented", &CliNotImplementedError{Verb: "x", Owner: "y"}, ExitSystem},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCodeFor(tc.err); got != tc.want {
				t.Fatalf("want %d, got %d", tc.want, got)
			}
		})
	}
}
