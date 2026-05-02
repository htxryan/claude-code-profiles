package resolver

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// WalkEntry is a single file from walkClaudeDir / walkProfileRoot.
type WalkEntry struct {
	RelPath string
	AbsPath string
}

// WalkClaudeDir recursively enumerates every regular file under dir,
// returning entries relative to dir in lex-sorted, posix-style form.
//
// Symlink behavior (v1):
//   - Symlinks-to-files are followed and returned as files.
//   - Symlinks-to-directories inside a contributor's `.claude/` subtree are
//     NOT traversed (no cycle protection, so this avoids accidental loops).
//
// Returns nil if dir does not exist or is not a directory.
func WalkClaudeDir(dir string) ([]WalkEntry, error) {
	st, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !st.IsDir() {
		return nil, nil
	}

	var out []WalkEntry
	if err := walkInto(dir, "", &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

func walkInto(base, rel string, out *[]WalkEntry) error {
	here := base
	if rel != "" {
		here = filepath.Join(base, filepath.FromSlash(rel))
	}
	entries, err := os.ReadDir(here)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		childRel := entry.Name()
		if rel != "" {
			childRel = path.Join(rel, entry.Name())
		}
		childAbs := filepath.Join(here, entry.Name())

		mode := entry.Type()
		if mode.IsDir() {
			if err := walkInto(base, childRel, out); err != nil {
				return err
			}
			continue
		}
		if mode.IsRegular() {
			*out = append(*out, WalkEntry{RelPath: childRel, AbsPath: childAbs})
			continue
		}
		if mode&os.ModeSymlink != 0 {
			// Symlink: stat through to determine target type. Do NOT recurse
			// into symlinked directories (cycle protection).
			target, err := os.Stat(childAbs)
			if err != nil {
				continue
			}
			if target.Mode().IsRegular() {
				*out = append(*out, WalkEntry{RelPath: childRel, AbsPath: childAbs})
			}
			continue
		}
	}
	return nil
}

// IsDirectory returns true iff p exists and is a directory.
func IsDirectory(p string) bool {
	s, err := os.Stat(p)
	if err != nil {
		return false
	}
	return s.IsDir()
}

// WalkProfileRoot discovers files at the profile root (peer of
// profile.json, sibling of `.claude/`) that materialize to the project
// root rather than under `.claude/`. Per cw6/§12 / R44–R46 the only such
// file in v1 is CLAUDE.md.
func WalkProfileRoot(profileDir string) ([]WalkEntry, error) {
	candidate := filepath.Join(profileDir, "CLAUDE.md")
	st, err := os.Stat(candidate)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, nil
	}
	return []WalkEntry{{RelPath: "CLAUDE.md", AbsPath: candidate}}, nil
}
