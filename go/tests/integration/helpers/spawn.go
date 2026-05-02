package helpers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// SpawnResult captures the observable surface of a CLI invocation: stdout,
// stderr, exit code, and the signal (if any) that terminated the process.
// Tests that need a clean-exit invariant should assert Signal == "" alongside
// ExitCode == 0; reporting 0 when a signal killed the child would silently
// let signal-killed losers count as winners in race tests (matches TS sentinel
// behaviour where ExitCode = -1 on signal-kill).
type SpawnResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Signal   string
}

// SpawnOptions is the input to RunCli. Fields mirror the TS SpawnOptions
// surface field-for-field (PR4 parity audit pins this).
type SpawnOptions struct {
	Cwd       string
	Env       map[string]string
	Args      []string
	Stdin     string
	TimeoutMs int
}

// defaultTimeoutMs matches TS spawn.ts (15s). Bumped from 10s upstream so a
// slow CI runner doesn't flake on first slow spawn.
const defaultTimeoutMs = 15000

var (
	binMu       sync.Mutex
	binPath     string
	binBuilt    bool
	binBuildErr error
)

// BinPath returns the resolved path to the c3p binary, building it on first
// call. Subsequent calls return the cached path. Tests that need a custom
// build (different ldflags, different OS target) should not use this — they
// should `go build` directly.
func BinPath(t *testing.T) string {
	t.Helper()
	binMu.Lock()
	defer binMu.Unlock()
	if binBuilt {
		if binBuildErr != nil {
			t.Fatalf("binary build previously failed: %v", binBuildErr)
		}
		return binPath
	}
	binBuilt = true
	path, err := buildBin()
	binPath = path
	binBuildErr = err
	if err != nil {
		t.Fatalf("build c3p: %v", err)
	}
	return binPath
}

// EnsureBuilt is the F1 fitness function: every tests/integration/*_test.go
// must call helpers.EnsureBuilt(t) (per the epic's explicit contract). It
// triggers BinPath() so the build cost amortizes across the whole test
// binary. Returns the bin path for convenience.
func EnsureBuilt(t *testing.T) string {
	t.Helper()
	return BinPath(t)
}

// CleanupBuiltBin removes the cached test binary, if one was built. Call
// from TestMain after m.Run() so the binary doesn't accumulate in
// os.TempDir() across `go test ./...` invocations.
func CleanupBuiltBin() {
	binMu.Lock()
	defer binMu.Unlock()
	if binBuilt && binPath != "" {
		_ = os.Remove(binPath)
	}
	binBuilt = false
	binPath = ""
	binBuildErr = nil
}

// buildBin compiles cmd/c3p into a temp file. We don't reuse a workspace
// build because integration tests must run against the source tree, not a
// stale `go install` artifact in $GOBIN.
func buildBin() (string, error) {
	module, err := findModuleRoot()
	if err != nil {
		return "", err
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("c3p-test-%d%s", os.Getpid(), exeSuffix()))
	cmd := exec.Command("go", "build", "-o", out, "./cmd/c3p")
	cmd.Dir = module
	// CGO_ENABLED=0 matches PR20 (release builds). Tests must exercise the
	// same build mode CI/release uses; otherwise a CGO-vs-pure difference
	// would only surface in production.
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build: %w (stderr: %s)", err, stderr.String())
	}
	return out, nil
}

// findModuleRoot walks up from the test source file's directory until it
// finds a go.mod. Tests can run from any cwd, so os.Getwd() is unreliable.
func findModuleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod found walking up from " + filepath.Dir(file))
		}
		dir = parent
	}
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// RunCli spawns the c3p binary with opts and returns a SpawnResult. Errors
// are returned only for harness-level failures (binary missing, timeout
// exceeded). A nonzero exit from c3p is reported as SpawnResult.ExitCode
// and is NOT a Go error — matches the TS surface where the promise resolves
// with the result on close, regardless of code.
func RunCli(ctx context.Context, opts SpawnOptions, t *testing.T) (SpawnResult, error) {
	t.Helper()
	bin := BinPath(t)

	timeout := time.Duration(opts.TimeoutMs) * time.Millisecond
	if opts.TimeoutMs == 0 {
		timeout = time.Duration(defaultTimeoutMs) * time.Millisecond
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, opts.Args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	if opts.Env != nil {
		// TS uses `{...process.env, ...opts.env}` — caller wins on
		// collision. exec.Cmd.Env replaces wholesale, so we splice
		// the inherited env in first.
		env := os.Environ()
		for k, v := range opts.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}
	if opts.Stdin != "" {
		cmd.Stdin = bytes.NewBufferString(opts.Stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := SpawnResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if runCtx.Err() == context.DeadlineExceeded {
		return res, fmt.Errorf("spawn timed out after %dms", int(timeout/time.Millisecond))
	}

	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
		res.Signal = signalName(cmd.ProcessState)
	}

	// Swallow the "exit status N" wrapping; harness-level errors (binary
	// missing, permission denied) still bubble up so tests don't silently
	// degrade to "exit 0".
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return res, err
	}
	return res, nil
}
