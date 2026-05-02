package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LockHandle is a held lock on .claude-profiles/.meta/lock. Returned from
// AcquireLock. The caller MUST call Release (or use WithLock) — the
// idempotent contract is enforced internally so signal handlers and finally-
// equivalent defer blocks can both release safely.
//
// Mirrors src/state/types.ts:LockHandle. PID + AcquiredAt are recorded on
// disk so a stale-lock holder can be diagnosed without external tooling.
type LockHandle struct {
	// Path is the absolute path of the lock file we hold.
	Path string
	// PID is the OS PID we wrote into the lock file.
	PID int
	// AcquiredAt is the ISO 8601 timestamp written when the lock was acquired.
	AcquiredAt string

	mu            sync.Mutex
	released      bool
	releaseFn     func() error
	signalCleanup func()
}

// Release relinquishes the lock. Idempotent and safe to call from signal
// handlers and from defer blocks. First call performs the work; subsequent
// calls return nil.
func (h *LockHandle) Release() error {
	h.mu.Lock()
	if h.released {
		h.mu.Unlock()
		return nil
	}
	h.released = true
	cleanup := h.signalCleanup
	releaseFn := h.releaseFn
	h.signalCleanup = nil
	h.releaseFn = nil
	h.mu.Unlock()

	if cleanup != nil {
		cleanup()
	}
	if releaseFn != nil {
		return releaseFn()
	}
	return nil
}

// LockHeldError is raised when the lock file exists with a live PID (R41a).
// CLI dispatch (E5) renders this as a non-zero exit naming the holder.
type LockHeldError struct {
	LockPath        string
	HolderPID       int
	HolderTimestamp string
}

func (e *LockHeldError) Error() string {
	return fmt.Sprintf(
		"lock at %q is held by PID %d (acquired at %s) — wait for the other process to finish, or remove %q if the PID is dead",
		e.LockPath, e.HolderPID, e.HolderTimestamp, e.LockPath,
	)
}

// LockCorruptError is raised when the lock file content is unparseable. The
// stale-recovery path replaces it, but we surface the detail for diagnostics.
type LockCorruptError struct {
	LockPath string
	Detail   string
}

func (e *LockCorruptError) Error() string {
	return fmt.Sprintf("Lock file at %q is corrupt: %s", e.LockPath, e.Detail)
}

// acquireMaxRetries bounds the recovery loop for transient races (e.g. a
// concurrent release that unlinked the lock file between our open and our
// read). Three attempts is enough to clear any single transient race; more
// would mask a real bug.
const acquireMaxRetries = 3

// testPollHook fires on every iteration of acquireLockWithWait's loop. Tests
// install it via SetTestPollHook (export_test.go) to count poll attempts and
// catch regressions in backoff arithmetic. Production code never sets this.
var testPollHook func()

// AcquireOptions configures lock acquisition. Zero value is safe (no signal
// handlers, fail-fast on conflict) — callers wanting wait/poll behaviour
// pass a non-nil Wait pointer.
type AcquireOptions struct {
	// SignalHandlers, when true, registers SIGINT/SIGTERM handlers that
	// release the lock and exit (R41c). Tests pass false so they can sync on
	// release without process-level handlers (closes the vitest-can't-host-
	// signals issue carried forward from the TS code).
	SignalHandlers bool

	// Wait, when non-nil, polls the lock with exponential backoff instead of
	// failing fast on first conflict. Off by default.
	Wait *WaitOptions
}

// WaitOptions configures lock-acquisition polling. Mirrors the TS
// LockWaitOptions surface.
type WaitOptions struct {
	// TotalMs is the total wall time the caller is willing to wait, in ms.
	TotalMs int64
	// InitialBackoffMs is the initial delay between polls (default 250ms,
	// floored at 50ms).
	InitialBackoffMs int64
	// MaxBackoffMs caps the backoff delay (default 2000ms).
	MaxBackoffMs int64
	// OnWait is fired ONCE when the wait begins so the user sees a "waiting"
	// line; subsequent polls are silent until success or timeout.
	OnWait func(holderPID int, holderTimestamp string)
}

// AcquireLock claims the project lock. Returns a LockHandle whose Release
// method is idempotent and signal-safe.
//
// Acquisition strategy combines two layers:
//  1. OS-level advisory file lock (flock on POSIX, LockFileEx on Windows) —
//     gives us correct cross-process exclusion even when the lock file
//     persists between runs.
//  2. PID + timestamp written into the lock file — gives us a human-readable
//     "who's holding it?" error and a stale-detection path for crashed
//     processes that left the file behind without releasing the OS lock.
//
// Reads (R43) bypass the lock entirely; AcquireLock is only called before
// mutating ops.
func AcquireLock(paths StatePaths, opts AcquireOptions) (*LockHandle, error) {
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		return nil, err
	}

	if opts.Wait != nil {
		return acquireLockWithWait(paths, opts)
	}
	return acquireLockOnce(paths, opts.SignalHandlers)
}

func acquireLockWithWait(paths StatePaths, opts AcquireOptions) (*LockHandle, error) {
	wait := opts.Wait
	totalMs := wait.TotalMs
	if totalMs < 0 {
		totalMs = 0
	}
	// Mirror the TS reference's Math.max(50, initialBackoffMs ?? 250):
	// unset (≤0) takes the 250ms default; any caller-supplied positive value
	// is floored at 50ms but otherwise honoured (10ms means "I want fast
	// polling, just not absurdly fast").
	initial := wait.InitialBackoffMs
	if initial <= 0 {
		initial = 250
	} else if initial < 50 {
		initial = 50
	}
	maxBackoff := wait.MaxBackoffMs
	if maxBackoff <= 0 {
		maxBackoff = 2000
	}
	if maxBackoff < initial {
		maxBackoff = initial
	}
	startedAt := time.Now()
	backoff := initial
	noticeSent := false

	for {
		if testPollHook != nil {
			testPollHook()
		}
		handle, err := acquireLockOnce(paths, opts.SignalHandlers)
		if err == nil {
			return handle, nil
		}
		var held *LockHeldError
		if !errors.As(err, &held) {
			// Non-conflict failure (FS error, corrupt-after-retries) propagates.
			return nil, err
		}
		elapsed := time.Since(startedAt).Milliseconds()
		if elapsed >= totalMs {
			return nil, err
		}
		if !noticeSent && wait.OnWait != nil {
			wait.OnWait(held.HolderPID, held.HolderTimestamp)
			noticeSent = true
		}
		remaining := totalMs - elapsed
		sleepMs := backoff
		if sleepMs > remaining {
			sleepMs = remaining
		}
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func acquireLockOnce(paths StatePaths, signalHandlers bool) (*LockHandle, error) {
	pid := os.Getpid()
	for attempt := 0; attempt < acquireMaxRetries; attempt++ {
		// Open the lock file with O_CREAT (no O_EXCL — the OS-level advisory
		// lock provides exclusion, and the file may legitimately persist
		// across runs on Windows where we unlink only on normal release).
		f, err := os.OpenFile(paths.LockFile, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return nil, err
		}

		// Try to take the OS-level advisory lock. Non-blocking: if another
		// process holds it, we get back the platform-specific "would-block"
		// error and inspect the file contents to decide live-vs-stale.
		locked, lockErr := tryAdvisoryLock(f)
		if lockErr != nil {
			_ = f.Close()
			return nil, lockErr
		}
		if !locked {
			// Lock is held by another process. Read the file to surface a
			// useful error. ENOENT is impossible immediately after our open,
			// but if it happens (file removed by signal handler) we retry.
			// On Windows, the LockFileEx region covers byte 0, so the read
			// itself fails with ERROR_LOCK_VIOLATION — that's still "held by
			// another process", just without diagnostic detail.
			raw, readErr := os.ReadFile(paths.LockFile)
			if readErr != nil {
				_ = f.Close()
				if errors.Is(readErr, os.ErrNotExist) {
					continue
				}
				if isLockReadConflict(readErr) {
					return nil, &LockHeldError{
						LockPath:        paths.LockFile,
						HolderPID:       0,
						HolderTimestamp: "(locked)",
					}
				}
				return nil, readErr
			}
			parsed, parsedOK := parseLockContents(string(raw))
			_ = f.Close()
			if parsedOK {
				return nil, &LockHeldError{
					LockPath:        paths.LockFile,
					HolderPID:       parsed.pid,
					HolderTimestamp: parsed.timestamp,
				}
			}
			// Held but contents unparseable — lockfile is corrupt AND the OS
			// lock is taken. We can't safely recover without forcing the
			// holder to release. Surface a corrupt-with-held error.
			return nil, &LockHeldError{
				LockPath:        paths.LockFile,
				HolderPID:       0,
				HolderTimestamp: "(corrupt)",
			}
		}

		// We hold the OS-level advisory lock — exclusion is enforced by the
		// kernel, so the stamp in the file is purely diagnostic. Whatever
		// PID/timestamp was last written either belongs to a previous holder
		// (now released, since we got the OS lock) or to a never-completed
		// stamp from a crashed acquirer. Either way, overwrite unconditionally.
		//
		// We don't inspect "is the recorded PID alive?" here: once we hold the
		// OS lock, that PID's liveness is irrelevant. The PID-alive check is
		// for the OTHER branch (we failed to get the OS lock), which already
		// reports LockHeldError using the file's stamp.
		timestamp := time.Now().UTC().Format(time.RFC3339Nano)
		contents := fmt.Sprintf("%d %s\n", pid, timestamp)
		if err := writeLockContents(f, contents); err != nil {
			_ = unlockAdvisoryAndClose(f)
			return nil, err
		}
		FsyncDir(paths.LockFile)

		handle := &LockHandle{
			Path:       paths.LockFile,
			PID:        pid,
			AcquiredAt: timestamp,
		}
		handle.releaseFn = makeReleaseFn(paths.LockFile, f)
		if signalHandlers {
			handle.signalCleanup = registerSignalRelease(handle)
		}
		return handle, nil
	}
	return nil, &LockCorruptError{LockPath: paths.LockFile, Detail: "acquire retries exhausted"}
}

// makeReleaseFn returns the platform-aware release closure: unlock the
// advisory lock, close the file, then on Windows unlink the lock file (PR26)
// so a fresh acquirer doesn't see lingering stamp contents from us.
//
// On POSIX we leave the file in place — flock on the inode is naturally
// released when the fd closes, and unlinking would make the file racy with
// concurrent acquirers (a competitor opening between our unlink and a hand-
// off would see ENOENT and re-create, bypassing the OS lock briefly).
func makeReleaseFn(lockPath string, f *os.File) func() error {
	return func() error {
		err := unlockAdvisoryAndClose(f)
		if releaseUnlinksLockFile {
			if removeErr := os.Remove(lockPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				if err == nil {
					err = removeErr
				}
			}
		}
		return err
	}
}

func unlockAdvisoryAndClose(f *os.File) error {
	unlockErr := unlockAdvisory(f)
	closeErr := f.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

// WithLock acquires the project lock, runs fn, and always releases (even on
// panic via defer). Mirrors src/state/lock.ts:withLock.
//
// ctx is honored only at entry: ctx.Err() is checked once before AcquireLock
// runs. The wait/retry loop inside AcquireLock does NOT inspect ctx, so a
// caller cancelling mid-wait will block until WaitOptions.TotalMs expires.
// Tests pass a pre-cancelled context to surface ctx.Err() before any FS work.
func WithLock(ctx context.Context, paths StatePaths, opts AcquireOptions, fn func(*LockHandle) error) (err error) {
	if ctx != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
	}
	handle, err := AcquireLock(paths, opts)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := handle.Release(); releaseErr != nil && err == nil {
			err = releaseErr
		}
	}()
	return fn(handle)
}

// parsedLock is the in-memory shape of a lock file's contents (PID +
// human-readable timestamp). Used only on the contention path to populate
// LockHeldError; the timestamp is kept as a string because we never need to
// arithmetic on it from this layer.
type parsedLock struct {
	pid       int
	timestamp string
}

func parseLockContents(raw string) (parsedLock, bool) {
	// strings.Fields splits on any unicode whitespace run, so we tolerate
	// canonical "pid ts\n", a tab variant, or any future stamp format that
	// keeps PID and timestamp as the first two whitespace-separated tokens.
	// RFC3339Nano timestamps don't contain whitespace, so taking field[1] as
	// the timestamp is safe.
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return parsedLock{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return parsedLock{}, false
	}
	return parsedLock{pid: pid, timestamp: fields[1]}, true
}

// writeLockContents truncates f and writes contents, fsyncing before return.
// f must be opened RDWR/CREATE; we seek to 0 and ftruncate so a previous
// stale stamp is fully overwritten (not appended).
func writeLockContents(f *os.File, contents string) error {
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteString(contents); err != nil {
		return err
	}
	return f.Sync()
}

// registerSignalRelease wires SIGINT/SIGTERM handlers that release the lock
// and exit with the conventional 128+signo code (R41c). Returns a cleanup
// closure the caller invokes during normal release so we don't leak handlers
// across multiple acquire cycles in one process.
//
// The signal handler does NOT call handle.Release() — that would deadlock
// because Release() calls this very function's cleanup closure, which waits
// for the signal goroutine to exit. Instead we do the bare minimum directly:
//   - On Windows, unlink the lock file (PR26).
//   - Exit the process; the kernel auto-releases the OS-level advisory lock
//     when our file handle closes during normal teardown.
func registerSignalRelease(handle *LockHandle) func() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	stop := make(chan struct{})
	done := make(chan struct{})
	lockPath := handle.Path
	go func() {
		defer close(done)
		select {
		case sig := <-c:
			if releaseUnlinksLockFile {
				_ = os.Remove(lockPath)
			}
			signo := 1
			switch sig {
			case syscall.SIGINT:
				signo = 2
			case syscall.SIGTERM:
				signo = 15
			}
			os.Exit(128 + signo)
		case <-stop:
			signal.Stop(c)
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
