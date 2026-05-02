package state

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/htxryan/c3p/internal/merge"
)

// dirMtime is the post-order mtime restoration record. We collect one per
// directory copied and replay them deepest-first after every file is in place
// so child writes don't clobber the parent's recorded source mtime.
type dirMtime struct {
	path  string
	mtime time.Time
}

// CopyTree recursively copies src to dst, creating dst and any parent dirs.
// Preserves file mode bits and modification time (R39). Existing files at
// dst are overwritten.
//
// Symlink handling: dereferenced — including symlinks-to-directories, which
// are descended into so the user's data is captured (matches the TS reference
// fs.cp({dereference: true})). filepath.Walk would have stopped at a symlink
// to a directory, leaving the destination empty, so we hand-roll the recursion.
// On Windows, copying symlinks themselves requires elevation or Developer
// Mode and frequently fails for non-admin users; dereferencing keeps the
// discard-backup and persist paths cross-platform reliable.
//
// Directory mtimes are applied in a post-order pass after all child writes
// complete. POSIX bumps a directory's mtime on every child create/rename, so
// any Chtimes call on a directory before its children land gets clobbered.
//
// Mirrors src/state/copy.ts:copyTree.
func CopyTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	var dirs []dirMtime
	if err := copyTreeRecursive(src, dst, &dirs); err != nil {
		return err
	}
	// Deepest-first: a directory's mtime restoration must outlive every child
	// write that happens inside it. Sorting by descending path length gives
	// us deterministic post-order without a second walk.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i].path) > len(dirs[j].path) })
	for _, d := range dirs {
		if err := os.Chtimes(d.path, d.mtime, d.mtime); err != nil {
			return fmt.Errorf("preserving mtime on %q: %w", d.path, err)
		}
	}
	return nil
}

// copyTreeRecursive performs the depth-first copy of src into dst. Symlinks
// are resolved via os.Stat so that symlinks-to-directories descend into the
// pointed-at tree (the divergence from filepath.Walk that the TS reference
// gets for free via fs.cp({dereference: true})). dirs accumulates directory
// mtime records for the post-order restoration pass at the top level.
func copyTreeRecursive(src, dst string, dirs *[]dirMtime) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	resolved := info
	if info.Mode()&os.ModeSymlink != 0 {
		if resolved, err = os.Stat(src); err != nil {
			return fmt.Errorf("dereferencing symlink %q: %w", src, err)
		}
	}

	switch {
	case resolved.IsDir():
		if err := os.MkdirAll(dst, resolved.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			childSrc := filepath.Join(src, e.Name())
			childDst := filepath.Join(dst, e.Name())
			if err := copyTreeRecursive(childSrc, childDst, dirs); err != nil {
				return err
			}
		}
		*dirs = append(*dirs, dirMtime{path: dst, mtime: resolved.ModTime()})
		return nil
	case resolved.Mode().IsRegular():
		return copyRegularFile(src, dst, resolved)
	default:
		// Devices, sockets, fifos — never expected inside .claude/, but if
		// the user sticks one there we silently skip rather than aborting
		// the whole copy. Drift detection will surface the divergence.
		return nil
	}
}

// copyRegularFile copies a single file from src to dst, preserving mode bits
// and mtime (R39). dst's parent must already exist.
func copyRegularFile(src, dst string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// chmod again — OpenFile with O_CREATE applies mode masked by umask. We
	// want the exact source mode bits (R39 quality bar) so the discard-backup
	// round-trip preserves +x on scripts.
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// WriteFiles writes a list of MergedFile bytes into targetDir, fsyncing each
// file and finally the parent dir. Creates intermediate directories as needed.
// Mirrors src/state/copy.ts:writeFiles minus the worker-pool concurrency
// (Go's runtime overhead per goroutine is low enough that we keep the
// implementation single-threaded for now; D5 can revisit if benchmarks
// demand it).
//
// The returned error is the first failure; partial writes are left in place
// so the pending/prior protocol can either commit or discard atomically.
func WriteFiles(targetDir string, files []merge.MergedFile) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	// Pre-create unique parent dirs once before we start opening files. This
	// avoids race-on-mkdir between concurrent writers in any future parallel
	// implementation, and gives us one place to surface mkdir errors.
	parents := make(map[string]struct{}, len(files))
	for _, f := range files {
		parents[filepath.Dir(filepath.Join(targetDir, f.Path))] = struct{}{}
	}
	parentList := make([]string, 0, len(parents))
	for p := range parents {
		parentList = append(parentList, p)
	}
	sort.Strings(parentList)
	for _, p := range parentList {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}

	for _, f := range files {
		abs := filepath.Join(targetDir, f.Path)
		if err := writeFileSynced(abs, f.Bytes); err != nil {
			return fmt.Errorf("writing %q: %w", f.Path, err)
		}
	}

	// fsync the target dir once at the end — POSIX best-effort to make the
	// collection of new files (and the dir entry) durable as a unit.
	FsyncDir(targetDir)
	return nil
}

// writeFileSynced opens dst for write, dumps bytes, fsyncs, and closes. The
// per-file fsync makes a partial-batch crash leave a coherent subset on disk
// that the pending/prior protocol can commit or discard atomically.
func writeFileSynced(dst string, bytes []byte) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := out.Write(bytes); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
