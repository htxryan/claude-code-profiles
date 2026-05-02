package state_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

func TestBuildStatePaths_Layout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := state.BuildStatePaths(root)

	if p.ProjectRoot != root {
		t.Fatalf("ProjectRoot = %q, want %q", p.ProjectRoot, root)
	}
	if p.ProfilesDir != filepath.Join(root, ".claude-profiles") {
		t.Errorf("ProfilesDir = %q", p.ProfilesDir)
	}
	if p.MetaDir != filepath.Join(root, ".claude-profiles", ".meta") {
		t.Errorf("MetaDir = %q", p.MetaDir)
	}
	if p.ClaudeDir != filepath.Join(root, ".claude") {
		t.Errorf("ClaudeDir = %q", p.ClaudeDir)
	}
	if p.StateFile != filepath.Join(p.MetaDir, "state.json") {
		t.Errorf("StateFile = %q", p.StateFile)
	}
	if p.LockFile != filepath.Join(p.MetaDir, "lock") {
		t.Errorf("LockFile = %q", p.LockFile)
	}
	if p.TmpDir != filepath.Join(p.MetaDir, "tmp") {
		t.Errorf("TmpDir = %q", p.TmpDir)
	}
	if p.PendingDir != filepath.Join(p.MetaDir, "pending") {
		t.Errorf("PendingDir = %q", p.PendingDir)
	}
	if p.PriorDir != filepath.Join(p.MetaDir, "prior") {
		t.Errorf("PriorDir = %q", p.PriorDir)
	}
	if p.BackupDir != filepath.Join(p.MetaDir, "backup") {
		t.Errorf("BackupDir = %q", p.BackupDir)
	}
	if p.GitignoreFile != filepath.Join(root, ".gitignore") {
		t.Errorf("GitignoreFile = %q", p.GitignoreFile)
	}
	if p.RootClaudeMdFile != filepath.Join(root, "CLAUDE.md") {
		t.Errorf("RootClaudeMdFile = %q", p.RootClaudeMdFile)
	}
}

func TestBuildPersistPaths_Validation(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())

	good, err := state.BuildPersistPaths(paths, "work")
	if err != nil {
		t.Fatalf("valid name: %v", err)
	}
	if good.ProfileDir != filepath.Join(paths.ProfilesDir, "work") {
		t.Errorf("ProfileDir = %q", good.ProfileDir)
	}
	if good.TargetClaudeDir != filepath.Join(paths.ProfilesDir, "work", ".claude") {
		t.Errorf("TargetClaudeDir = %q", good.TargetClaudeDir)
	}

	bad := []string{
		"",        // empty
		".",       // dot
		"..",      // dot dot
		".hidden", // leading dot
		"a/b",     // posix sep
		"a\\b",    // win sep
		"con",     // windows reserved
		"COM1",    // windows reserved
		"foo\x00", // NUL
	}
	for _, name := range bad {
		if _, err := state.BuildPersistPaths(paths, name); err == nil {
			t.Errorf("BuildPersistPaths(%q): expected validation error, got nil", name)
		}
	}
}

func TestRootClaudeMdTmpPath_PatternRoundTrip(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	tmp := state.RootClaudeMdTmpPath(paths)
	if filepath.Dir(tmp) != filepath.Dir(paths.RootClaudeMdFile) {
		t.Fatalf("tmp dir %q must match CLAUDE.md dir %q", filepath.Dir(tmp), filepath.Dir(paths.RootClaudeMdFile))
	}
	base := filepath.Base(tmp)
	if !strings.HasPrefix(base, "CLAUDE.md.") || !strings.HasSuffix(base, ".tmp") {
		t.Errorf("tmp basename %q does not match expected shape", base)
	}
	if !state.IsRootClaudeMdTmpName(base) {
		t.Errorf("IsRootClaudeMdTmpName(%q) = false, want true", base)
	}

	// Negative cases — user-authored .tmp files must not be swept.
	for _, name := range []string{
		"CLAUDE.md",
		"CLAUDE.md.bak",
		"backup.tmp",
		"CLAUDE.md.tmp",                 // missing pid+counter+random
		"CLAUDE.md.123.tmp",             // missing counter+random
		"CLAUDE.md.123.456.tmp",         // missing random suffix
		"CLAUDE.md.abc.0-deadbeef.tmp",  // pid non-numeric
		"CLAUDE.md.123.0-DEADBEEF.tmp",  // random uppercase
	} {
		if state.IsRootClaudeMdTmpName(name) {
			t.Errorf("IsRootClaudeMdTmpName(%q) = true, want false", name)
		}
	}
}

func TestRootClaudeMdTmpPath_UniqueAcrossCalls(t *testing.T) {
	t.Parallel()
	paths := state.BuildStatePaths(t.TempDir())
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		p := state.RootClaudeMdTmpPath(paths)
		if seen[p] {
			t.Fatalf("duplicate tmp path %q at iteration %d", p, i)
		}
		seen[p] = true
	}
}
