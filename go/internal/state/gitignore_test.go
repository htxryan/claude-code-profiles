package state_test

import (
	"os"
	"strings"
	"testing"

	"github.com/htxryan/c3p/internal/state"
)

// TestEnsureGitignoreEntries_CreatesFile covers init-on-missing: a project
// without a .gitignore gets one with both managed entries.
func TestEnsureGitignoreEntries_CreatesFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	res, err := state.EnsureGitignoreEntries(paths)
	if err != nil {
		t.Fatalf("EnsureGitignoreEntries: %v", err)
	}
	if !res.Created {
		t.Fatalf("created = false; expected true")
	}
	if len(res.Added) != len(state.GitignoreEntries) {
		t.Fatalf("added %v, want %v", res.Added, state.GitignoreEntries)
	}
	body, _ := os.ReadFile(paths.GitignoreFile)
	for _, e := range state.GitignoreEntries {
		if !strings.Contains(string(body), e) {
			t.Fatalf("gitignore missing %q:\n%s", e, body)
		}
	}
}

// TestEnsureGitignoreEntries_Idempotent covers re-running on a populated file:
// no duplicates added.
func TestEnsureGitignoreEntries_Idempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if _, err := state.EnsureGitignoreEntries(paths); err != nil {
		t.Fatalf("first call: %v", err)
	}
	first, _ := os.ReadFile(paths.GitignoreFile)
	res2, err := state.EnsureGitignoreEntries(paths)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(res2.Added) != 0 {
		t.Fatalf("second call added %v, want []", res2.Added)
	}
	second, _ := os.ReadFile(paths.GitignoreFile)
	if string(first) != string(second) {
		t.Fatalf("file mutated on second call:\nfirst=%q\nsecond=%q", first, second)
	}
}

// TestEnsureGitignoreEntries_PreservesExistingContent asserts append-only:
// existing entries are kept; new entries appended.
func TestEnsureGitignoreEntries_PreservesExistingContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.WriteFile(paths.GitignoreFile, []byte("node_modules/\n.idea/\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := state.EnsureGitignoreEntries(paths); err != nil {
		t.Fatalf("EnsureGitignoreEntries: %v", err)
	}
	body, _ := os.ReadFile(paths.GitignoreFile)
	for _, expect := range []string{"node_modules/", ".idea/", ".claude/", ".claude-profiles/.meta/"} {
		if !strings.Contains(string(body), expect) {
			t.Fatalf("gitignore missing %q:\n%s", expect, body)
		}
	}
}

// TestEnsureGitignoreEntries_NoTmpAtProjectRoot is the regression guard for
// "tmp staging at project root would show up in `git status`": tmp must live
// under .meta/tmp/, never at root.
func TestEnsureGitignoreEntries_NoTmpAtProjectRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if _, err := state.EnsureGitignoreEntries(paths); err != nil {
		t.Fatalf("EnsureGitignoreEntries: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("tmp file %q at project root", e.Name())
		}
	}
}

// TestEnsureGitignoreEntries_AddsOnlyMissing covers the partial-presence
// case: some entries already present; only the missing ones are appended.
func TestEnsureGitignoreEntries_AddsOnlyMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.WriteFile(paths.GitignoreFile, []byte(".claude/\n.claude-profiles/.meta/\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := state.EnsureGitignoreEntries(paths)
	if err != nil {
		t.Fatalf("EnsureGitignoreEntries: %v", err)
	}
	if len(res.Added) != 1 {
		t.Fatalf("added = %v, want only one missing entry", res.Added)
	}
	if res.Added[0] != "CLAUDE.md.*.tmp" {
		t.Fatalf("added = %q, want CLAUDE.md.*.tmp", res.Added[0])
	}
}
