package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// ErrCrossDevice is returned by AtomicRename when the source and destination
// live on different filesystems (POSIX EXDEV / Windows ERROR_NOT_SAME_DEVICE).
// The pending/prior protocol places staging dirs as siblings of the target
// inside the same FS, so this error indicates a misconfiguration (e.g.
// .claude-profiles/.meta symlinked to a different mount). Callers must
// surface — never silently retry.
//
// Wraps the underlying syscall error so errors.Is(err, ErrCrossDevice) works
// AND callers can recover the original via errors.Unwrap.
var ErrCrossDevice = errors.New("cross-device rename")

// CrossDeviceError wraps ErrCrossDevice with the source/destination paths so
// the human message names the offending move. Returned from AtomicRename.
type CrossDeviceError struct {
	Src string
	Dst string
	Err error
}

func (e *CrossDeviceError) Error() string {
	return fmt.Sprintf(
		"cross-device rename refused: %q → %q (%v) — staging must live on the same filesystem as its target",
		e.Src, e.Dst, e.Err,
	)
}

// Is reports ErrCrossDevice as the sentinel for this error.
func (e *CrossDeviceError) Is(target error) bool { return target == ErrCrossDevice }

// Unwrap exposes the underlying syscall error so callers can inspect platform
// specifics (e.g. *os.LinkError) when they need them.
func (e *CrossDeviceError) Unwrap() error { return e.Err }

// AtomicRename wraps os.Rename and converts the platform-specific cross-device
// error into ErrCrossDevice (PR13). Other errors propagate verbatim.
//
// The rename itself is atomic on a single filesystem on every supported OS:
//   - POSIX: rename(2)
//   - Windows: MoveFileExW with MOVEFILE_REPLACE_EXISTING
//
// Per-platform fsync of the parent dir (POSIX durability) is deferred to the
// caller via FsyncDir — the rename is already crash-consistent in journaled
// filesystems; fsync just narrows the durability window.
func AtomicRename(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		if isCrossDeviceErr(err) {
			return &CrossDeviceError{Src: src, Dst: dst, Err: err}
		}
		return err
	}
	return nil
}

// FsyncDir best-effort fsyncs the directory containing target. On POSIX this
// makes a preceding rename durable across crashes; on Windows directory fsync
// is not meaningful (NTFS journals the metadata change) so the syscall is
// expected to fail and is swallowed.
//
// Returning a typed error here would be a footgun — every successful rename
// would be paired with a Windows-specific error that callers would either
// ignore or platform-sniff. Best-effort matches src/state/atomic.ts:fsyncDir.
func FsyncDir(target string) {
	dir := filepath.Dir(target)
	f, err := os.Open(dir)
	if err != nil {
		// Windows refuses to open a directory for read; non-POSIX edge cases
		// (network mounts, certain virtualized FSes) similarly. The protocol
		// still gives us crash-recovery via pending/prior; fsync just narrows
		// the window.
		return
	}
	defer f.Close()
	_ = f.Sync()
}

// AtomicWriteFile writes contents to dst atomically: tmp file (in tmpPath),
// fsync the file, rename to dst, fsync the parent dir. Used for state.json
// (R14a) and similar single-file persistence sites.
//
// tmpPath is taken explicitly rather than computed so callers can keep a
// stable cleanup target across retries. If a previous attempt crashed before
// rename, the next call truncates and overwrites the leftover tmp.
func AtomicWriteFile(dst, tmpPath string, contents []byte) error {
	// O_TRUNC ensures a leftover tmp from a prior crashed write doesn't grow
	// indefinitely. 0o644 matches the TS implementation's default file mode
	// (Node's fs.open with "w") so cross-version state files are byte-equal.
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(contents); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := AtomicRename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	FsyncDir(dst)
	return nil
}

// uniqueAtomicTmpCounter is a process-wide monotonic counter for unique tmp
// filenames in the atomic-write staging dir.
var uniqueAtomicTmpCounter atomic.Uint64

// UniqueAtomicTmpPath builds a unique tmp staging path inside tmpDir for an
// atomic write of dest. PID + monotonic counter + random suffix prevents
// concurrent writers from clobbering each other's staging file even if the
// lock is bypassed (defense-in-depth). The dest basename is embedded for
// diagnostics — operators can tell from the staging filename which final
// write was in progress at crash time.
func UniqueAtomicTmpPath(tmpDir, dest string) string {
	counter := uniqueAtomicTmpCounter.Add(1) - 1
	tag := filepath.Base(dest)
	return filepath.Join(tmpDir, fmt.Sprintf("%s.%d.%d-%s.tmp", tag, os.Getpid(), counter, randomNonce()))
}

// RmRf recursively removes target, tolerating ENOENT. Used by reconciliation
// (drop a stale .pending/) and post-success cleanup (drop the rolled-aside
// .prior/). Mirrors os.RemoveAll which already maps "not present" to nil.
func RmRf(target string) error {
	return os.RemoveAll(target)
}

// PathExists reports whether target exists. Maps ENOENT to (false, nil); any
// other error (permission, IO) propagates so callers can distinguish "missing"
// from "broken filesystem".
func PathExists(target string) (bool, error) {
	_, err := os.Lstat(target)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
