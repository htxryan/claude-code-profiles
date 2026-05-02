//go:build windows

package state

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// releaseUnlinksLockFile is true on Windows (PR26): UnlockFileEx returns the
// region; we then delete the file so a fresh acquirer doesn't read stale
// PID/timestamp bytes from us. The unlink is ENOENT-tolerant (signal handler
// may have raced ahead).
const releaseUnlinksLockFile = true

// lockfileExclusiveLock + lockfileFailImmediately together produce the
// non-blocking exclusive write lock semantics we want from LockFileEx.
const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
)

// isLockReadConflict reports whether err is ERROR_LOCK_VIOLATION — what
// Windows returns when ReadFile (and therefore os.ReadFile) tries to read
// bytes covered by another process's exclusive LockFileEx region. Our lock
// covers byte 0 of the lockfile, so anyone else opening the file to read
// PID/timestamp diagnostics hits this. Caller (acquireLockOnce) treats it
// the same as "held by another, identity unknown" — we already proved
// non-ownership via tryAdvisoryLock returning (false, nil); the read is
// best-effort diagnostics, not a precondition for the held verdict.
func isLockReadConflict(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// isFileInUseError reports whether err is a Windows "the file is open in
// another handle so this delete cannot proceed" error. Used by the lock
// release path: when we unlink the stamp file after releasing the OS-level
// lock, a fresh acquirer in another goroutine/process may already have the
// file open. The OS lock is what enforces exclusion; the unlink is purely
// stamp cleanup, so a "file in use" error here is benign — the new holder
// will overwrite the stamp on its own acquire path. ERROR_SHARING_VIOLATION
// is the canonical case; ERROR_ACCESS_DENIED can also surface during
// concurrent delete-vs-open races on some Windows file systems.
func isFileInUseError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

// tryAdvisoryLock attempts a non-blocking exclusive LockFileEx on the entire
// file. Returns (true, nil) on success, (false, nil) on contention, and
// (false, err) on unexpected failures.
//
// LockFileEx is held against the file handle, so the lock auto-releases when
// the holder process exits — that's how we detect "stale" state from a
// crashed previous holder without needing an explicit liveness probe.
func tryAdvisoryLock(f *os.File) (bool, error) {
	var ol windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		lockfileExclusiveLock|lockfileFailImmediately,
		0,
		1, 0, // 1 byte at offset 0 — sufficient for whole-file mutex semantics
		&ol,
	)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return false, err
}

// unlockAdvisory releases the byte-range lock taken by LockFileEx.
func unlockAdvisory(f *os.File) error {
	var ol windows.Overlapped
	if err := windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,
		1, 0,
		&ol,
	); err != nil {
		return err
	}
	return nil
}
