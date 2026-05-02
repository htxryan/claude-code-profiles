package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/internal/state"
)

func seedClaudeDir(t *testing.T, paths state.StatePaths) {
	t.Helper()
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "settings.json"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestSnapshotForDiscard_Basic asserts a snapshot copies live .claude/ into
// .meta/backup/<stamp>/ and returns the new path.
func TestSnapshotForDiscard_Basic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	seedClaudeDir(t, paths)
	dest, err := state.SnapshotForDiscard(paths)
	if err != nil {
		t.Fatalf("SnapshotForDiscard: %v", err)
	}
	if dest == nil {
		t.Fatalf("dest nil; expected snapshot path")
	}
	if !strings.HasPrefix(*dest, paths.BackupDir) {
		t.Fatalf("dest %q not under %q", *dest, paths.BackupDir)
	}
	got, err := os.ReadFile(filepath.Join(*dest, "settings.json"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("snapshot contents = %q", got)
	}
}

// TestSnapshotForDiscard_NoLiveDirReturnsNil covers the NoActive corner: no
// live .claude/ → return nil (TS-parity with `null`) with no error.
func TestSnapshotForDiscard_NoLiveDirReturnsNil(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	dest, err := state.SnapshotForDiscard(paths)
	if err != nil {
		t.Fatalf("SnapshotForDiscard: %v", err)
	}
	if dest != nil {
		t.Fatalf("dest = %q, want nil", *dest)
	}
}

// TestSnapshotForDiscard_PrunesToFive asserts retention: snapshot count never
// exceeds MaxRetainedSnapshots; oldest is dropped first.
func TestSnapshotForDiscard_PrunesToFive(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	seedClaudeDir(t, paths)
	for i := 0; i < state.MaxRetainedSnapshots+3; i++ {
		if _, err := state.SnapshotForDiscard(paths); err != nil {
			t.Fatalf("SnapshotForDiscard #%d: %v", i, err)
		}
	}
	snaps, err := state.ListSnapshots(paths)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != state.MaxRetainedSnapshots {
		t.Fatalf("kept %d snapshots, want %d", len(snaps), state.MaxRetainedSnapshots)
	}
}

// TestSnapshotForDiscard_NoColonsInName covers the Windows-safe name: ISO
// timestamps would normally embed `:` which is invalid in Windows path
// components.
func TestSnapshotForDiscard_NoColonsInName(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	seedClaudeDir(t, paths)
	dest, err := state.SnapshotForDiscard(paths)
	if err != nil {
		t.Fatalf("SnapshotForDiscard: %v", err)
	}
	if dest == nil {
		t.Fatalf("dest nil; expected snapshot path")
	}
	base := filepath.Base(*dest)
	if strings.Contains(base, ":") {
		t.Fatalf("snapshot name %q contains a colon (invalid on Windows)", base)
	}
}
