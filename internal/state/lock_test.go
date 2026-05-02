package state_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

// readLockStampOK reports whether the test process can read the lockfile
// while it holds the OS-level advisory lock. POSIX flock binds the inode,
// not the byte range, so reads from a different fd succeed. Windows
// LockFileEx covers byte 0 — reads from any other handle (even our own
// process's os.ReadFile) hit ERROR_LOCK_VIOLATION. Tests that want to
// verify the on-disk PID stamp gate that assertion behind this helper.
func readLockStampOK() bool {
	return runtime.GOOS != "windows"
}

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

	// Lock file exists with our PID stamped in. Skip the read-back assertion
	// on Windows: LockFileEx blocks any read of the locked region (byte 0)
	// while we hold the lock, even from our own process.
	if readLockStampOK() {
		contents, err := os.ReadFile(paths.LockFile)
		if err != nil {
			t.Fatalf("read lock: %v", err)
		}
		if !strings.HasPrefix(string(contents), strconv.Itoa(os.Getpid())+" ") {
			t.Errorf("lock contents %q do not start with our PID", contents)
		}
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
	if held.LockPath != paths.LockFile {
		t.Errorf("LockPath = %q, want %q", held.LockPath, paths.LockFile)
	}
	if readLockStampOK() {
		// Stamp readable: assertions on PID + error message that names PID.
		if held.HolderPID != os.Getpid() {
			t.Errorf("HolderPID = %d, want %d", held.HolderPID, os.Getpid())
		}
		if !strings.Contains(held.Error(), strconv.Itoa(os.Getpid())) {
			t.Errorf("error message %q missing PID", held.Error())
		}
	} else {
		// Windows: LockFileEx blocks the read of the PID/timestamp stamp,
		// so the held error degrades to PID=0, ts="(locked)". The contract
		// surfaced here is "we returned a *LockHeldError naming this lock
		// path"; identity-of-holder is best-effort.
		if held.HolderPID != 0 {
			t.Errorf("Windows: HolderPID = %d, want 0 (stamp-unreadable fallback)", held.HolderPID)
		}
		if held.HolderTimestamp != "(locked)" {
			t.Errorf("Windows: HolderTimestamp = %q, want \"(locked)\"", held.HolderTimestamp)
		}
	}
}

// TestAcquireLock_RecoversFromOrphanedStamp covers R41a/R41b: a lock file
// whose PID/timestamp stamp is left behind without a held OS-level advisory
// lock is reclaimed on the next acquire. The implementation does NOT check
// PID liveness — recovery hinges entirely on tryAdvisoryLock returning
// (true, nil) because no process holds the OS lock — so this test seeds a
// stamp via plain os.WriteFile (no flock) and verifies the next acquirer
// overwrites it.
func TestAcquireLock_RecoversFromOrphanedStamp(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}

	// Any PID stamp will do — a real-but-unrelated PID, a never-existed PID,
	// or one we just made up. The acquire path doesn't read it back unless
	// it fails to take the OS lock, which won't happen here because os.WriteFile
	// doesn't touch flock.
	const orphanedPID = 99999
	timestamp := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	contents := fmt.Sprintf("%d %s\n", orphanedPID, timestamp)
	if err := os.WriteFile(paths.LockFile, []byte(contents), 0o644); err != nil {
		t.Fatalf("seed orphaned stamp: %v", err)
	}

	handle, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("AcquireLock after orphaned stamp: %v", err)
	}
	defer handle.Release()

	if handle.PID != os.Getpid() {
		t.Errorf("recovered handle PID = %d, want %d", handle.PID, os.Getpid())
	}
	// Skip the post-recovery stamp read on Windows (LockFileEx blocks reads
	// of byte 0 while we hold the lock).
	if readLockStampOK() {
		got, err := os.ReadFile(paths.LockFile)
		if err != nil {
			t.Fatalf("read post-recovery: %v", err)
		}
		if !strings.HasPrefix(string(got), strconv.Itoa(os.Getpid())+" ") {
			t.Errorf("post-recovery lock %q not stamped with our PID", got)
		}
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
// not silently replaced with the 250ms default. The naive elapsed-time check
// can't distinguish fix-vs-bug because the wait loop clamps the final sleep
// to TotalMs-elapsed; both versions return at ~TotalMs. We instead count
// poll attempts via SetTestPollHook:
//
//	Fixed   (initial floor 10→50, max=50): polls every 50ms over 200ms → ≥4 polls.
//	Buggy   (10→250 default, max bumped to 250): one sleep clamped to 200ms → 2 polls.
//
// Asserting pollCount ≥ 3 catches the regression definitively.
func TestAcquireLock_WaitHonoursSmallBackoff(t *testing.T) {
	// Cannot t.Parallel(): SetTestPollHook touches package-global state.
	paths := state.BuildStatePaths(t.TempDir())
	holder, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	defer holder.Release()

	var pollCount int32
	restore := state.SetTestPollHook(func() { atomic.AddInt32(&pollCount, 1) })
	defer restore()

	wait := &state.WaitOptions{
		TotalMs:          200,
		InitialBackoffMs: 10, // sub-floor; expect 50ms (not 250ms) from the fix
		MaxBackoffMs:     50,
	}
	_, err = state.AcquireLock(paths, state.AcquireOptions{Wait: wait})
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	got := atomic.LoadInt32(&pollCount)
	if got < 3 {
		t.Errorf("only %d poll(s) — initial backoff likely defaulted to 250ms instead of being floored at 50ms", got)
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

// Silence unused import check on platforms that don't need filepath here.
var _ = filepath.Join
