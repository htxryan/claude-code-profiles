package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

func TestAtomicRename_Success(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := state.AtomicRename(src, dst); err != nil {
		t.Fatalf("AtomicRename: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("dst contents = %q, want %q", got, "hello")
	}
	if exists, _ := state.PathExists(src); exists {
		t.Fatalf("src still exists after rename")
	}
}

func TestAtomicRename_OverwritesDestination(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("write dst: %v", err)
	}
	if err := state.AtomicRename(src, dst); err != nil {
		t.Fatalf("AtomicRename: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("dst contents = %q, want %q", got, "new")
	}
}

// TestAtomicRename_CrossDeviceTypedError verifies PR13: an EXDEV from the
// kernel is mapped to ErrCrossDevice, not silently retried.
//
// Linux exposes /dev/shm and /tmp on different filesystems on most distros;
// macOS exposes only /tmp. We construct a cross-FS rename when possible and
// skip otherwise — the Windows-conditional cell is exercised by
// windows_test.go.
func TestAtomicRename_CrossDeviceTypedError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skipf("cross-FS detection requires distinct filesystems; skipping on %s", runtime.GOOS)
	}
	if _, err := os.Stat("/dev/shm"); err != nil {
		t.Skip("/dev/shm not present; need a separate filesystem for EXDEV")
	}

	src, err := os.CreateTemp("/dev/shm", "c3p-atomic-*")
	if err != nil {
		t.Skipf("/dev/shm not writable: %v", err)
	}
	defer os.Remove(src.Name())
	if _, err := src.WriteString("payload"); err != nil {
		src.Close()
		t.Fatalf("write tmp: %v", err)
	}
	src.Close()

	dst := filepath.Join(t.TempDir(), "dst")
	err = state.AtomicRename(src.Name(), dst)
	if err == nil {
		t.Fatalf("expected cross-device error, got nil")
	}
	if !errors.Is(err, state.ErrCrossDevice) {
		t.Fatalf("error %v does not wrap ErrCrossDevice", err)
	}
	var cde *state.CrossDeviceError
	if !errors.As(err, &cde) {
		t.Fatalf("error %v not a *CrossDeviceError", err)
	}
	if cde.Src != src.Name() || cde.Dst != dst {
		t.Fatalf("CrossDeviceError paths %q→%q, want %q→%q", cde.Src, cde.Dst, src.Name(), dst)
	}
	if !strings.Contains(cde.Error(), "cross-device") {
		t.Fatalf("error message %q missing 'cross-device'", cde.Error())
	}
}

func TestAtomicWriteFile_WritesAndCleansUpTmp(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "state.json")
	tmpPath := filepath.Join(tmp, "state.tmp")

	if err := state.AtomicWriteFile(dst, tmpPath, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("dst = %q, want %q", got, `{"a":1}`)
	}
	if exists, _ := state.PathExists(tmpPath); exists {
		t.Fatalf("tmp path %q still exists after rename", tmpPath)
	}
}

func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "state.json")
	tmpPath := filepath.Join(tmp, "state.tmp")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}
	if err := state.AtomicWriteFile(dst, tmpPath, []byte("new")); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new" {
		t.Fatalf("dst = %q, want %q", got, "new")
	}
}

func TestUniqueAtomicTmpPath_EmbedsBasenameAndPID(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	p := state.UniqueAtomicTmpPath(tmp, "/some/dir/state.json")
	base := filepath.Base(p)
	if !strings.HasPrefix(base, "state.json.") {
		t.Errorf("base %q missing dest basename prefix", base)
	}
	if !strings.HasSuffix(base, ".tmp") {
		t.Errorf("base %q missing .tmp suffix", base)
	}
	pidStr := strings.Split(base, ".")[2]
	if pidStr == "" {
		t.Errorf("base %q missing PID slot", base)
	}
}

func TestUniqueAtomicTmpPath_UniqueAcrossCalls(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		p := state.UniqueAtomicTmpPath(tmp, "x")
		if seen[p] {
			t.Fatalf("duplicate tmp path %q at iteration %d", p, i)
		}
		seen[p] = true
	}
}

func TestRmRf_RemovesTreeAndToleratesMissing(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "tree")
	if err := os.MkdirAll(filepath.Join(target, "a", "b"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "a", "b", "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := state.RmRf(target); err != nil {
		t.Fatalf("RmRf: %v", err)
	}
	if exists, _ := state.PathExists(target); exists {
		t.Fatalf("target %q still exists", target)
	}
	// Idempotent: missing target is not an error.
	if err := state.RmRf(target); err != nil {
		t.Fatalf("RmRf on missing target: %v", err)
	}
}

func TestPathExists(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	exists, err := state.PathExists(tmp)
	if err != nil {
		t.Fatalf("PathExists existing: %v", err)
	}
	if !exists {
		t.Fatalf("expected tempdir to exist")
	}
	missing := filepath.Join(tmp, "nope")
	exists, err = state.PathExists(missing)
	if err != nil {
		t.Fatalf("PathExists missing: %v", err)
	}
	if exists {
		t.Fatalf("expected missing path to report not-exists")
	}
}
