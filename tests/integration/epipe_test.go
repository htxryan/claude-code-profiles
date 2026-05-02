package integration_test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// IV translation of TS epipe.test.ts (claude-code-profiles-qga). Read-only
// commands can be piped into short-circuiting consumers like `head -1`.
// When the consumer closes the read end before c3p has finished writing,
// EPIPE can fire on the underlying socket. With proper handling, the
// pipeline exits cleanly under `set -o pipefail`.

// shellSingleQuote wraps s in single quotes safe for POSIX sh, escaping
// embedded single quotes as '\''. Naive `"'" + s + "'"` wrapping breaks
// on any single-quote-bearing argument and would be a shell-injection
// hazard if a future caller ever passes user-controlled or fixture-derived
// strings into runPipeline.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runPipeline runs `c3p <args> | head -1` via bash with pipefail. With
// pipefail set, the pipeline's exit code is the first non-zero stage's
// exit code, so 0 means c3p AND head both exited cleanly. If c3p crashed
// on EPIPE we'd see exit != 0 plus a Go traceback on stderr.
func runPipeline(t *testing.T, args []string) (exitCode int, stdout, stderr string) {
	t.Helper()
	bin := helpers.BinPath(t)

	argString := make([]string, len(args))
	for i, a := range args {
		argString[i] = shellSingleQuote(a)
	}
	body := fmt.Sprintf("set -o pipefail; %s %s | head -1", shellSingleQuote(bin), strings.Join(argString, " "))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", body)

	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("pipeline timed out after 10s (stderr=%q)", se.String())
	}

	stdout = so.String()
	stderr = se.String()
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
		return
	}
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	return
}

// TestEpipe_HelpPipedToHeadExitsCleanly — `c3p --help | head -1` under
// pipefail must exit 0 with no EPIPE/traceback on stderr. We loop a
// handful of times because the underlying race is timing-dependent.
func TestEpipe_HelpPipedToHeadExitsCleanly(t *testing.T) {
	if isWindows() {
		t.Skip("windows pipe semantics differ; bash + head not reliably on PATH")
	}
	if !shellAvailable("bash") {
		t.Skip("bash not on PATH")
	}
	if !shellAvailable("head") {
		t.Skip("head not on PATH")
	}
	helpers.EnsureBuilt(t)
	for i := 0; i < 5; i++ {
		t.Run(fmt.Sprintf("run-%d", i+1), func(t *testing.T) {
			code, stdout, stderr := runPipeline(t, []string{"--help"})
			if code != 0 {
				t.Fatalf("pipeline exit: want 0, got %d (stderr=%q)", code, stderr)
			}
			for _, bad := range []string{"EPIPE", "panic", "goroutine ", "Error: write"} {
				if strings.Contains(stderr, bad) {
					t.Errorf("stderr contains %q (regression?): %q", bad, stderr)
				}
			}
			if len(stdout) == 0 {
				t.Errorf("stdout empty; head -1 should have read at least one line")
			}
		})
	}
}

// TestEpipe_VersionPipedToHeadExitsCleanly — `c3p --version | head -1` is
// a short-output stress case: the writer may finish before head closes.
func TestEpipe_VersionPipedToHeadExitsCleanly(t *testing.T) {
	if isWindows() {
		t.Skip("windows pipe semantics differ; bash + head not reliably on PATH")
	}
	if !shellAvailable("bash") {
		t.Skip("bash not on PATH")
	}
	if !shellAvailable("head") {
		t.Skip("head not on PATH")
	}
	helpers.EnsureBuilt(t)
	code, _, stderr := runPipeline(t, []string{"--version"})
	if code != 0 {
		t.Fatalf("pipeline exit: want 0, got %d (stderr=%q)", code, stderr)
	}
	for _, bad := range []string{"EPIPE", "panic", "goroutine ", "Error: write"} {
		if strings.Contains(stderr, bad) {
			t.Errorf("stderr contains %q (regression?): %q", bad, stderr)
		}
	}
}
