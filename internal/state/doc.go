// Package state provides the on-disk state primitives owned by D4
// (claude-code-profiles-chp): atomic-rename + atomic-write, advisory file
// lock, two-tier fingerprint, copy-tree + write-files, and the canonical
// path bundle used by every D5 verb.
//
// D5 (claude-code-profiles-ryo) adds the orchestration layer on top:
// materialize, reconcile, persist, backup, gitignore management, and the
// state-file reader/writer.
//
// # Design notes (week-1 throwaway-prototype outcomes)
//
// Atomic rename: os.Rename is rename(2) on POSIX (atomic on same FS,
// replaces existing) and MoveFileExW with MOVEFILE_REPLACE_EXISTING on
// Windows (atomic for files; directories require move-prior-aside, which is
// the protocol's job in D5). Cross-FS rename (EXDEV / ERROR_NOT_SAME_DEVICE)
// is mapped to ErrCrossDevice (PR13) so callers never silently re-attempt.
//
// Lock primitive: file-handle-bound advisory lock — flock(LOCK_EX) on POSIX
// via golang.org/x/sys/unix, LockFileEx on Windows via golang.org/x/sys/windows
// (PR14). Process exit auto-releases the OS lock, so stale-PID detection is
// implicit on the OS-lock layer; the PID + ISO-timestamp stamp inside the
// file is purely for human diagnostics (LockHeldError naming the holder).
// On Windows we unlink the lock file after UnlockFileEx (PR26); on POSIX we
// leave the file in place because flock binds the inode and unlinking would
// race with concurrent acquirers.
//
// Fingerprint: two-tier (R18). FastPathHits skip hashing when size+mtime
// match recorded values; SlowPathHits re-hash and compare. CompareMetrics
// surfaces the split so drift detection (D6) can flag "0 fast-path hits" as
// a smell signal for clock-skew or over-eager atomic-write tools.
//
// Copy: tree copy preserves file mode bits and mtime (R39) and dereferences
// symlinks because .claude/ is a copy tree (Windows is copy-only; symlink
// copy on Windows requires elevation).
package state
