//go:build windows

package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/htxryan/c3p/internal/state"
)

// TestWindows_LockFileExExclusion covers the Windows-conditional fitness cell
// for LockFileEx: a second AcquireLock against the same StatePaths fails with
// LockHeldError while the first holder is alive, and succeeds once it
// releases. Mirrors the cross-platform TestAcquireLock_RejectsLiveHolder but
// runs only on Windows so a regression in lock_windows.go is caught here
// rather than as a generic "lock test failed".
func TestWindows_LockFileExExclusion(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	holder, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := state.AcquireLock(paths, state.AcquireOptions{}); err == nil {
		t.Fatalf("expected LockHeldError, got nil")
	} else {
		var held *state.LockHeldError
		if !errors.As(err, &held) {
			t.Fatalf("error %v is not LockHeldError", err)
		}
	}

	if err := holder.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	// PR26: Windows release unlinks the lock file.
	if _, err := os.Stat(paths.LockFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("PR26: lock file still exists after Windows release: %v", err)
	}

	again, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	again.Release()
}

// TestWindows_StalePIDRecovery exercises the Windows-conditional stale-PID
// recovery (R41a) by seeding a lock file with a definitely-dead PID. PR26's
// inaccessible-PID + stale-mtime heuristic isn't directly testable from a Go
// unit test (we can't construct an inaccessible PID portably), but the
// happier path — dead PID + released OS lock — is.
func TestWindows_StalePIDRecovery(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Seed a stale stamp with a small dead PID. Windows tends to use higher
	// PIDs (≥4 for system); 999999 is unlikely to match a live process.
	if err := os.WriteFile(paths.LockFile, []byte("999999 2020-01-01T00:00:00Z\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	handle, err := state.AcquireLock(paths, state.AcquireOptions{})
	if err != nil {
		t.Fatalf("acquire after stale: %v", err)
	}
	defer handle.Release()

	if handle.PID != os.Getpid() {
		t.Errorf("recovered PID = %d, want %d", handle.PID, os.Getpid())
	}
}

// TestWindows_CrossDriveAtomicRename verifies the typed ErrCrossDevice path
// on Windows when source and dest live on different drives. When no second
// drive is available we skip — the cross-platform unit test exercises the
// EXDEV path on Linux so the typed-error machinery is covered there.
func TestWindows_CrossDriveAtomicRename(t *testing.T) {
	t.Parallel()
	// Look for a second drive letter (D:, E:, …) writable to us. On most CI
	// runners only C: is available, in which case we skip.
	var altRoot string
	for _, letter := range []string{"D:", "E:", "F:", "G:"} {
		probe := filepath.Join(letter+`\`, "c3p-cross-drive-probe")
		if err := os.MkdirAll(filepath.Dir(probe), 0o755); err == nil {
			altRoot = filepath.Dir(probe)
			break
		}
	}
	if altRoot == "" {
		t.Skip("no second drive available for cross-drive rename test")
	}
	src, err := os.CreateTemp(altRoot, "c3p-cd-*")
	if err != nil {
		t.Skipf("alt drive %s not writable: %v", altRoot, err)
	}
	defer os.Remove(src.Name())
	src.Close()

	dst := filepath.Join(t.TempDir(), "dst")
	err = state.AtomicRename(src.Name(), dst)
	if err == nil {
		t.Fatalf("expected ErrCrossDevice, got nil")
	}
	if !errors.Is(err, state.ErrCrossDevice) {
		t.Fatalf("error %v does not wrap ErrCrossDevice", err)
	}
}
