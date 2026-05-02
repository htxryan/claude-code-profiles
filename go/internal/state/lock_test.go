package state_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/htxryan/c3p/internal/state"
)

func TestAcquireLock_BasicAcquireRelease(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	handle, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if handle.PID != os.Getpid() {
		t.Errorf("handle PID = %d, want %d", handle.PID, os.Getpid())
	}
	if handle.Path != paths.LockFile {
		t.Errorf("handle Path = %q, want %q", handle.Path, paths.LockFile)
	}
	if handle.AcquiredAt == "" {
		t.Errorf("handle AcquiredAt empty")
	}

	// Lock file exists with our PID stamped in.
	contents, err := os.ReadFile(paths.LockFile)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if !strings.HasPrefix(string(contents), strconv.Itoa(os.Getpid())+" ") {
		t.Errorf("lock contents %q do not start with our PID", contents)
	}

	if err := handle.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	// Idempotent.
	if err := handle.Release(); err != nil {
		t.Fatalf("Release (idempotent): %v", err)
	}
}

func TestAcquireLock_RejectsLiveHolder(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	holder, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("AcquireLock holder: %v", err)
	}
	defer holder.Release()

	_, err = state.AcquireLock(paths, state.AcquireOptions{})
	if err == nil {
		t.Fatalf("expected LockHeldError, got nil")
	}
	var held *state.LockHeldError
	if !errors.As(err, &held) {
		t.Fatalf("error %v not a *LockHeldError", err)
	}
	if held.HolderPID != os.Getpid() {
		t.Errorf("HolderPID = %d, want %d", held.HolderPID, os.Getpid())
	}
	if held.LockPath != paths.LockFile {
		t.Errorf("LockPath = %q, want %q", held.LockPath, paths.LockFile)
	}
	if !strings.Contains(held.Error(), strconv.Itoa(os.Getpid())) {
		t.Errorf("error message %q missing PID", held.Error())
	}
}

// TestAcquireLock_StalePIDRecovery covers R41a/R41b: a lock file left behind
// by a crashed process (PID no longer alive) is reclaimed on the next acquire.
//
// We simulate the scenario by writing a lock file with a definitely-dead PID
// (we exit a child process and reuse its now-stale PID). The OS-level advisory
// lock is naturally released when the child exits, so the next acquirer sees
// (locked file present, OS lock available, PID dead) → recover.
func TestAcquireLock_StalePIDRecovery(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}

	// Pick a definitely-dead PID by spawning a process that immediately exits
	// and capturing its PID. On most Unix kernels a recently-exited PID is not
	// reused for a while. On Windows we use the same approach via OpenProcess
	// returning ERROR_INVALID_PARAMETER.
	deadPID := spawnAndExit(t)

	timestamp := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	contents := fmt.Sprintf("%d %s\n", deadPID, timestamp)
	if err := os.WriteFile(paths.LockFile, []byte(contents), 0o644); err != nil {
		t.Fatalf("seed stale lock: %v", err)
	}

	handle, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("AcquireLock after stale: %v", err)
	}
	defer handle.Release()

	if handle.PID != os.Getpid() {
		t.Errorf("recovered handle PID = %d, want %d", handle.PID, os.Getpid())
	}
	got, err := os.ReadFile(paths.LockFile)
	if err != nil {
		t.Fatalf("read post-recovery: %v", err)
	}
	if !strings.HasPrefix(string(got), strconv.Itoa(os.Getpid())+" ") {
		t.Errorf("post-recovery lock %q not stamped with our PID", got)
	}
}

func TestWithLock_RunsFnAndReleases(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	called := false
	err := state.WithLock(context.Background(), paths, state.AcquireOptions{}, func(h *state.LockHandle) error {
		called = true
		if h.PID != os.Getpid() {
			t.Errorf("inner handle PID = %d, want %d", h.PID, os.Getpid())
		}
		// Lock file exists during the closure.
		if _, err := os.Stat(paths.LockFile); err != nil {
			t.Errorf("lock file missing inside WithLock: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock: %v", err)
	}
	if !called {
		t.Errorf("fn not invoked")
	}
	// After return, a fresh acquire must succeed (lock released).
	h2, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("re-acquire after WithLock: %v", err)
	}
	h2.Release()
}

func TestWithLock_PropagatesError(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	want := errors.New("inner")
	err := state.WithLock(context.Background(), paths, state.AcquireOptions{}, func(h *state.LockHandle) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	// Lock released even on inner error.
	h, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("re-acquire after inner error: %v", err)
	}
	h.Release()
}

func TestWithLock_HonoursCancelledContext(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := state.WithLock(ctx, paths, state.AcquireOptions{}, func(h *state.LockHandle) error {
		t.Fatalf("fn invoked despite cancelled ctx")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// TestAcquireLock_WaitHonoursSmallBackoff verifies the bug-fix from code
// review: a caller-supplied InitialBackoffMs of 10 must be floored at 50ms,
// not silently replaced with the 250ms default. We measure the elapsed time
// for a 200ms total budget against a held lock — at 50ms backoff we expect
// ~4 polls, at 250ms backoff we'd expect 0–1 polls. The test fails if the
// actual elapsed time exceeds 250ms (which would indicate the backoff was
// floored at 250 like the buggy version, not 50).
func TestAcquireLock_WaitHonoursSmallBackoff(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	holder, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	defer holder.Release()

	wait := &state.WaitOptions{
		TotalMs:          200,
		InitialBackoffMs: 10, // sub-floor; expect 50ms (not 250ms) from the fix
		MaxBackoffMs:     50,
	}
	start := time.Now()
	_, err = state.AcquireLock(paths, state.AcquireOptions{Wait: wait})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	// Buggy floor would round 10 → 250, single retry, elapsed ≈ 250ms.
	// Fixed floor: 10 → 50, ≥3 retries within 200ms budget, elapsed ≈ 200ms.
	if elapsed > 245*time.Millisecond {
		t.Errorf("elapsed %v exceeds 245ms — backoff likely defaulted to 250ms instead of 50ms floor", elapsed)
	}
}

func TestAcquireLock_WaitTimesOut(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	holder, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	defer holder.Release()

	var noticeFired bool
	var mu sync.Mutex
	wait := &state.WaitOptions{
		TotalMs:          200,
		InitialBackoffMs: 50,
		MaxBackoffMs:     50,
		OnWait: func(pid int, ts string) {
			mu.Lock()
			noticeFired = true
			mu.Unlock()
		},
	}
	start := time.Now()
	_, err = state.AcquireLock(paths, state.AcquireOptions{Wait: wait})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	var held *state.LockHeldError
	if !errors.As(err, &held) {
		t.Fatalf("error %v not LockHeldError", err)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("returned in %v, expected ≥150ms wait", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	if !noticeFired {
		t.Errorf("OnWait callback never fired")
	}
}

func TestAcquireLock_WaitSucceedsWhenHolderReleases(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	holder, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = holder.Release()
	}()

	wait := &state.WaitOptions{
		TotalMs:          1500,
		InitialBackoffMs: 50,
		MaxBackoffMs:     100,
	}
	start := time.Now()
	got, err := state.AcquireLock(paths, state.AcquireOptions{Wait: wait})
	if err != nil {
		t.Fatalf("AcquireLock with Wait: %v", err)
	}
	defer got.Release()
	elapsed := time.Since(start)
	if elapsed < 60*time.Millisecond {
		t.Errorf("returned in %v, expected to wait ≥60ms", elapsed)
	}
	if got.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", got.PID, os.Getpid())
	}
}

func TestAcquireLock_ReleaseFreesLockForNextAcquirer(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	for i := 0; i < 3; i++ {
		h, err := state.AcquireLock(paths, state.AcquireOptions{})
		if err != nil {
			t.Fatalf("iter %d: AcquireLock: %v", i, err)
		}
		if err := h.Release(); err != nil {
			t.Fatalf("iter %d: Release: %v", i, err)
		}
	}
}

// spawnAndExit launches a short-lived child process, waits for it to exit,
// and returns its PID. The kernel doesn't reuse PIDs immediately, so this PID
// is a reliable "definitely dead" stand-in for the stale-recovery test.
func spawnAndExit(t *testing.T) int {
	t.Helper()
	exe := "true"
	if runtime.GOOS == "windows" {
		exe = "cmd"
	}
	args := []string{}
	if runtime.GOOS == "windows" {
		args = []string{"/c", "exit"}
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn helper: %v", err)
	}
	pid := cmd.ProcessState.Pid()
	if pid <= 0 {
		t.Fatalf("invalid spawned PID %d", pid)
	}
	return pid
}

// Silence unused import check on platforms that don't need filepath here.
var _ = filepath.Join
