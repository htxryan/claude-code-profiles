package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MaxRetainedSnapshots caps the number of discard backups kept under
// .meta/backup/ — oldest are pruned first so disk usage is bounded (R23a).
const MaxRetainedSnapshots = 5

// SnapshotForDiscard copies the live .claude/ tree to
// .claude-profiles/.meta/backup/<ISO>/ and prunes to keep at most
// MaxRetainedSnapshots (R23a). Returns the absolute path of the new snapshot
// for the one-line CLI notice; returns "" with no error when .claude/ doesn't
// exist (NoActive state being discarded — nothing to back up).
func SnapshotForDiscard(paths StatePaths) (string, error) {
	exists, err := PathExists(paths.ClaudeDir)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	if err := os.MkdirAll(paths.BackupDir, 0o755); err != nil {
		return "", err
	}
	// ISO timestamps are millisecond-precise so two snapshots within the same
	// ms collide; copyTree's silent merge would corrupt retention accounting.
	// Append a counter suffix `.1`, `.2`, ... until a free slot is found.
	baseStamp := isoStampSafeForFs(time.Now())
	dest := filepath.Join(paths.BackupDir, baseStamp)
	for i := 1; ; i++ {
		exists, err := PathExists(dest)
		if err != nil {
			return "", err
		}
		if !exists {
			break
		}
		dest = filepath.Join(paths.BackupDir, fmt.Sprintf("%s.%d", baseStamp, i))
	}
	if err := CopyTree(paths.ClaudeDir, dest); err != nil {
		return "", err
	}
	if err := pruneOldSnapshots(paths.BackupDir, MaxRetainedSnapshots); err != nil {
		return "", err
	}
	return dest, nil
}

// isoStampSafeForFs returns an ISO-8601 timestamp safe to use as a directory
// name on every supported OS. Windows forbids `:` in path components, so we
// replace colons with `-`. Format: 2026-04-25T12-34-56.789Z.
//
// Uses FormatTimestamp (PR2) for the millisecond fraction to keep snapshot
// names ordered identically to the materializedAt field in state.json.
func isoStampSafeForFs(t time.Time) string {
	return strings.ReplaceAll(FormatTimestamp(t), ":", "-")
}

// pruneOldSnapshots keeps at most `keep` snapshots in backupDir, pruning
// oldest first. Sort is by directory name (ISO timestamps lexically sort
// chronologically for the chosen format).
func pruneOldSnapshots(backupDir string, keep int) error {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for len(names) > keep {
		oldest := names[0]
		names = names[1:]
		if err := RmRf(filepath.Join(backupDir, oldest)); err != nil {
			return err
		}
	}
	return nil
}

// ListSnapshots returns the absolute paths of current snapshots in backupDir,
// sorted oldest-first (matching the sort key used by pruneOldSnapshots).
// Returns an empty slice with no error when the backup dir doesn't exist.
func ListSnapshots(paths StatePaths) ([]string, error) {
	exists, err := PathExists(paths.BackupDir)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	entries, err := os.ReadDir(paths.BackupDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(paths.BackupDir, n)
	}
	return out, nil
}
