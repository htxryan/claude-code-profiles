package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/htxryan/claude-code-config-profiles/internal/merge"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

func TestCopyTree_PreservesContentAndMode(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	mustWrite(t, filepath.Join(src, "a.txt"), "alpha", 0o644)
	mustWrite(t, filepath.Join(src, "sub", "b.sh"), "echo hi", 0o755)
	mustWrite(t, filepath.Join(src, "sub", "c.txt"), "gamma", 0o600)

	if err := state.CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	cases := []struct {
		path string
		want string
		mode os.FileMode
	}{
		{"a.txt", "alpha", 0o644},
		{filepath.Join("sub", "b.sh"), "echo hi", 0o755},
		{filepath.Join("sub", "c.txt"), "gamma", 0o600},
	}
	for _, c := range cases {
		got, err := os.ReadFile(filepath.Join(dst, c.path))
		if err != nil {
			t.Errorf("%s: read: %v", c.path, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s: contents = %q, want %q", c.path, got, c.want)
		}
		info, err := os.Stat(filepath.Join(dst, c.path))
		if err != nil {
			t.Errorf("%s: stat: %v", c.path, err)
			continue
		}
		if runtime.GOOS == "windows" {
			// Windows file modes don't carry POSIX bits; skip the mode assertion.
			continue
		}
		if info.Mode().Perm() != c.mode {
			t.Errorf("%s: mode = %v, want %v", c.path, info.Mode().Perm(), c.mode)
		}
	}
}

func TestCopyTree_PreservesMtime(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")
	mustWrite(t, filepath.Join(src, "f"), "x", 0o644)

	srcInfo, err := os.Stat(filepath.Join(src, "f"))
	if err != nil {
		t.Fatalf("stat src: %v", err)
	}

	if err := state.CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	dstInfo, err := os.Stat(filepath.Join(dst, "f"))
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	// Allow 1ms slop for FS mtime granularity (some FSes truncate to second).
	if delta := dstInfo.ModTime().Sub(srcInfo.ModTime()); delta < -2 || delta > 2 {
		// The Sub returns a Duration; treat seconds-resolution FS as equal.
		if dstInfo.ModTime().Unix() != srcInfo.ModTime().Unix() {
			t.Errorf("mtime drift: src=%v dst=%v", srcInfo.ModTime(), dstInfo.ModTime())
		}
	}
}

func TestCopyTree_DereferencesSymlinks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevation on Windows; covered by R39 copy-only contract")
	}
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "real.txt"), "real", 0o644)
	link := filepath.Join(src, "link.txt")
	if err := os.Symlink("real.txt", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := state.CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	info, err := os.Lstat(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatalf("lstat link copy: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected dereferenced regular file at %q, got symlink", filepath.Join(dst, "link.txt"))
	}
	got, err := os.ReadFile(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatalf("read link copy: %v", err)
	}
	if string(got) != "real" {
		t.Errorf("link copy contents = %q, want %q", got, "real")
	}
}

// TestCopyTree_RecursesIntoSymlinkedDirectory locks down the bug discovered
// in code review: filepath.Walk does NOT descend into symlinked directories
// even when info is dereferenced, so a user symlink pointing at a populated
// directory was being copied as an empty directory. The hand-rolled recursion
// must descend so the contained files land in the destination.
func TestCopyTree_RecursesIntoSymlinkedDirectory(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevation on Windows")
	}
	src := t.TempDir()
	target := filepath.Join(src, "real")
	mustWrite(t, filepath.Join(target, "inner.txt"), "inside", 0o644)
	mustWrite(t, filepath.Join(target, "sub", "deeper.txt"), "deep", 0o644)
	if err := os.Symlink("real", filepath.Join(src, "linkdir")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := state.CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "linkdir", "inner.txt"))
	if err != nil {
		t.Fatalf("read linkdir/inner.txt: %v", err)
	}
	if string(got) != "inside" {
		t.Errorf("linkdir/inner.txt = %q, want %q", got, "inside")
	}
	deep, err := os.ReadFile(filepath.Join(dst, "linkdir", "sub", "deeper.txt"))
	if err != nil {
		t.Fatalf("read linkdir/sub/deeper.txt: %v", err)
	}
	if string(deep) != "deep" {
		t.Errorf("linkdir/sub/deeper.txt = %q, want %q", deep, "deep")
	}
}

// TestCopyTree_PreservesDirectoryMtime locks down the bug discovered in code
// review: setting a directory's mtime before its children are written gets
// clobbered by the child writes (POSIX bumps a directory's mtime on every
// child create). The post-order restoration pass must run after all files
// land so the source mtime survives.
func TestCopyTree_PreservesDirectoryMtime(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "sub", "a.txt"), "a", 0o644)
	mustWrite(t, filepath.Join(src, "sub", "b.txt"), "b", 0o644)

	// Stamp the source dir to a known historical time so we can detect a
	// child-write clobber. One hour in the past avoids any clock-skew slop.
	historical := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(src, "sub"), historical, historical); err != nil {
		t.Fatalf("chtimes src dir: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	if err := state.CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	info, err := os.Stat(filepath.Join(dst, "sub"))
	if err != nil {
		t.Fatalf("stat dst dir: %v", err)
	}
	// Allow 1s slop for FS mtime granularity.
	delta := info.ModTime().Sub(historical)
	if delta < -2*time.Second || delta > 2*time.Second {
		t.Errorf("dst dir mtime = %v, want %v (delta %v)", info.ModTime(), historical, delta)
	}
}

// TestCopyTree_DetectsSymlinkCycle locks down the cycle-detection guard:
// without it, a symlink whose resolved target lies on the recursion stack
// caused unbounded recursion (TS reference fs.cp dereferences AND detects
// cycles; the Go port now matches). We expect a typed *CycleError, not a
// stack overflow.
func TestCopyTree_DetectsSymlinkCycle(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevation on Windows")
	}
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "leaf.txt"), "leaf", 0o644)
	// Cycle: src/loop -> src (descend into src via loop, see loop again, ...).
	if err := os.Symlink(src, filepath.Join(src, "loop")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	err := state.CopyTree(src, dst)
	if err == nil {
		t.Fatalf("expected cycle error, got nil")
	}
	var cycle *state.CycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("err = %v, want *state.CycleError", err)
	}
}

// TestWriteFiles_RejectsPathTraversal verifies the defense-in-depth check:
// any caller (now or future) bypassing the resolver and passing an absolute
// path or "../" escape MUST be rejected before any FS work happens.
func TestWriteFiles_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path string
	}{
		{"absolute", "/etc/passwd"},
		{"parent escape", "../escape.txt"},
		{"nested parent escape", "sub/../../escape.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dst := t.TempDir()
			err := state.WriteFiles(dst, []merge.MergedFile{
				{Path: tc.path, Bytes: []byte("x")},
			})
			if err == nil {
				t.Fatalf("expected rejection of %q, got nil", tc.path)
			}
		})
	}
}

func TestWriteFiles_WritesAllAndCreatesParents(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	files := []merge.MergedFile{
		{Path: "a.txt", Bytes: []byte("alpha")},
		{Path: "sub/b.txt", Bytes: []byte("beta")},
		{Path: "sub/deep/c.txt", Bytes: []byte("gamma")},
	}
	if err := state.WriteFiles(dst, files); err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}

	got := scanTree(t, dst)
	want := map[string]string{
		"a.txt":           "alpha",
		"sub/b.txt":       "beta",
		"sub/deep/c.txt":  "gamma",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("file %q = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		extras := []string{}
		for k := range got {
			if _, ok := want[k]; !ok {
				extras = append(extras, k)
			}
		}
		sort.Strings(extras)
		t.Errorf("unexpected files written: %v", extras)
	}
}

func mustWrite(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %q: %v", path, err)
	}
}

func scanTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		bytes, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(bytes)
		return nil
	})
	if err != nil {
		t.Fatalf("scanTree: %v", err)
	}
	return out
}
