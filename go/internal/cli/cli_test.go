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
	if code != ExitUsageError {
		t.Fatalf("unknown command: want %d, got %d", ExitUsageError, code)
	}
}

func TestRunStubVerbReturnsInternalError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hello"}, "0.0.0", &stdout, &stderr)
	if code != ExitInternalError {
		t.Fatalf("stub verb: want %d, got %d (stderr=%q)", ExitInternalError, code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not implemented") {
		t.Fatalf("stderr missing not-implemented marker: %q", stderr.String())
	}
}

func TestExitCodeForPipelineError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"cycle", pipelineerrors.NewCycleError([]string{"a", "a"}), ExitUserError},
		{"conflict", pipelineerrors.NewConflictError("x", []string{"a", "b"}), ExitUserError},
		{"missingProfile", pipelineerrors.NewMissingProfileError("x", "", nil), ExitUserError},
		{"pathTraversal", pipelineerrors.NewPathTraversalError("x", "y", "z"), ExitUserError},
		{"plainError", stderrors.New("boom"), ExitInternalError},
		{"cobraUnknownFlag", stderrors.New("unknown flag: --noplz"), ExitUsageError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Fatalf("want %d, got %d", tc.want, got)
			}
		})
	}
}
