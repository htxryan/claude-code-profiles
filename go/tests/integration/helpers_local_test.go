package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// mustRun is a thin wrapper over helpers.RunCli that t.Fatals on
// harness-level failures and returns the SpawnResult on c3p-level errors
// (nonzero exits surface as result.ExitCode, never as an err). Cuts the
// noise floor in tests that don't need to introspect the err.
//
// MUST be called only from the test goroutine (t.Fatalf calls Goexit, which
// only correctly unwinds the test goroutine — see goRun for the worker
// shape that surfaces errors back to the test goroutine via channel).
func mustRun(t *testing.T, opts helpers.SpawnOptions) helpers.SpawnResult {
	t.Helper()
	r, err := helpers.RunCli(context.Background(), opts, t)
	if err != nil {
		t.Fatalf("RunCli: %v", err)
	}
	return r
}

// goRun is the goroutine-safe sibling of mustRun: it returns (result, error)
// instead of t.Fatalf-ing on harness failure, so worker goroutines can
// surface a harness error back to the test goroutine via a channel. The
// test goroutine is then responsible for calling t.Fatal/t.Errorf — Go's
// testing contract requires Fatal/Skip to be invoked only from the
// goroutine running the test (https://pkg.go.dev/testing#T).
func goRun(opts helpers.SpawnOptions, t *testing.T) (helpers.SpawnResult, error) {
	return helpers.RunCli(context.Background(), opts, t)
}

// runBin spawns an arbitrary binary path with args, env, and stdin —
// used for symlink tests where we deliberately want to invoke c3p via a
// path other than helpers.BinPath. Mirrors helpers.RunCli's shape.
func runBin(t *testing.T, bin string, args []string, env map[string]string, stdin string) helpers.SpawnResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	if env != nil {
		e := os.Environ()
		for k, v := range env {
			e = append(e, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = e
	}
	if stdin != "" {
		cmd.Stdin = bytes.NewBufferString(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := helpers.SpawnResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("runBin %q: %v", bin, err)
	}
	return res
}

// shellAvailable returns true if `cmd` resolves on PATH. Mirrors TS's
// `which()` helper. Used to gate shell-completion tests behind shell
// availability rather than failing on minimal CI runners.
func shellAvailable(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// isWindows is a tiny helper so tests don't need to import runtime
// individually for one-off platform gates.
func isWindows() bool { return runtime.GOOS == "windows" }
