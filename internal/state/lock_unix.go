//go:build !windows

package state

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// releaseUnlinksLockFile is the platform switch for PR26: Windows unlinks the
// lock file on normal release; POSIX leaves it (flock binds the inode and
// unlinking would race with concurrent acquirers).
const releaseUnlinksLockFile = false

// isLockReadConflict reports whether err is the platform-specific "another
// process holds an exclusive byte-range lock on bytes we are trying to read"
// error. Always false on POSIX: flock binds the inode, not byte ranges, so
// reads from a different fd are never blocked by an existing flock holder.
func isLockReadConflict(err error) bool {
	return false
}

// tryAdvisoryLock attempts a non-blocking exclusive flock on f. Returns
// (true, nil) on success, (false, nil) if another process holds the lock,
// and (false, err) on unexpected failures.
//
// flock(LOCK_EX) is held against the open file description, so the lock
// auto-releases when the holder process exits — that's how we detect "stale"
// state from a crashed previous holder without needing an explicit kill -0.
func tryAdvisoryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return false, err
}

// unlockAdvisory releases the advisory lock on f. Closing the fd would also
// release flock, but we unlock explicitly so the contract is symmetric and
// observable.
func unlockAdvisory(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		return err
	}
	return nil
}
