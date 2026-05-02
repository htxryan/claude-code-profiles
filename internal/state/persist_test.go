package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/internal/markers"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/state"
)

// TestPersistLiveIntoProfile_HappyPath asserts the per-profile pending/prior
// protocol commits live .claude/ into <profile>/.claude/.
func TestPersistLiveIntoProfile_HappyPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "settings.json"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := state.PersistLiveIntoProfile(paths, "active"); err != nil {
		t.Fatalf("PersistLiveIntoProfile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(paths.ProfilesDir, "active", ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("persisted contents = %q", got)
	}
	// No leftover .pending/ or .prior/ inside the profile dir on success.
	if exists, _ := state.PathExists(filepath.Join(paths.ProfilesDir, "active", ".pending")); exists {
		t.Fatalf(".pending/ left behind")
	}
	if exists, _ := state.PathExists(filepath.Join(paths.ProfilesDir, "active", ".prior")); exists {
		t.Fatalf(".prior/ left behind")
	}
}

// TestPersistLiveIntoProfile_OverwritesExistingTarget asserts the prior
// profile contents are replaced by the new live snapshot (R22b).
func TestPersistLiveIntoProfile_OverwritesExistingTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	target := filepath.Join(paths.ProfilesDir, "active", ".claude")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "old"), []byte("OLD"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "new"), []byte("NEW"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := state.PersistLiveIntoProfile(paths, "active"); err != nil {
		t.Fatalf("PersistLiveIntoProfile: %v", err)
	}
	if exists, _ := state.PathExists(filepath.Join(target, "old")); exists {
		t.Fatalf("old file survived persist (target should be replaced)")
	}
	got, _ := os.ReadFile(filepath.Join(target, "new"))
	if string(got) != "NEW" {
		t.Fatalf("persisted = %q", got)
	}
}

// TestPersistLiveIntoProfile_EmptyLiveDir covers "user deleted .claude/
// entirely" — persist writes an empty profile target.
func TestPersistLiveIntoProfile_EmptyLiveDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := state.PersistLiveIntoProfile(paths, "active"); err != nil {
		t.Fatalf("PersistLiveIntoProfile: %v", err)
	}
	target := filepath.Join(paths.ProfilesDir, "active", ".claude")
	if exists, _ := state.PathExists(target); !exists {
		t.Fatalf("target %q not created", target)
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatalf("empty live should produce empty target; got %d entries", len(entries))
	}
}

// TestPersistLiveIntoProfile_RootClaudeMdSection covers cw6/T5 AC-8: the live
// project-root CLAUDE.md section is written back to <profile>/CLAUDE.md (peer
// of profile.json), holding JUST the body — no markers.
func TestPersistLiveIntoProfile_RootClaudeMdSection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	live := "User above\n" + markers.RenderManagedBlock("HELLO\nWORLD\n", 1) + "User below\n"
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte(live), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := state.PersistLiveIntoProfile(paths, "active"); err != nil {
		t.Fatalf("PersistLiveIntoProfile: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(paths.ProfilesDir, "active", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read profile CLAUDE.md: %v", err)
	}
	// Body is round-tripped through ExtractSectionBody which strips the
	// self-doc framing, leaving just the user-meaningful body.
	if !strings.Contains(string(body), "HELLO") {
		t.Fatalf("body missing HELLO: %q", body)
	}
	if strings.Contains(string(body), "c3p:v1:begin") {
		t.Fatalf("body should NOT contain markers: %q", body)
	}
	// AC-8b regression guard: legacy .claude/CLAUDE.md must NOT be touched
	// by the section persist (the cw6 contract says project-root section
	// lives in <profile>/CLAUDE.md, not <profile>/.claude/CLAUDE.md).
	legacy := filepath.Join(paths.ProfilesDir, "active", ".claude", "CLAUDE.md")
	if _, err := os.Stat(legacy); err == nil {
		// It's allowed to exist if it was in the live tree (CopyTree mirror),
		// but our test seeds nothing into live .claude/, so it shouldn't.
		t.Fatalf("legacy .claude/CLAUDE.md was created by section persist: %s", legacy)
	}
}

// TestPersistLiveIntoProfile_NoSectionWhenMarkersAbsent covers the silent-
// skip branch: live root CLAUDE.md exists but has no markers (user opted
// out). Persist should NOT write a body file.
func TestPersistLiveIntoProfile_NoSectionWhenMarkersAbsent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte("plain user content\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := state.PersistLiveIntoProfile(paths, "active"); err != nil {
		t.Fatalf("PersistLiveIntoProfile: %v", err)
	}
	body := filepath.Join(paths.ProfilesDir, "active", "CLAUDE.md")
	if _, err := os.Stat(body); err == nil {
		t.Fatalf("profile CLAUDE.md was written despite missing markers")
	}
}

// TestPersistAndMaterialize_FullCycle is the canonical drift-persist flow:
// persist live, then materialize a new plan. New live is the new plan; the
// old plan's .claude/ is captured under .claude-profiles/<old>/.
func TestPersistAndMaterialize_FullCycle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	// Seed live .claude/ with content the user has edited (the "drift").
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "drifted.md"), []byte("USER EDIT"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	plan := makePlan("newprof")
	merged := []merge.MergedFile{mergedFile("clean.md", "CLEAN")}
	res, err := state.PersistAndMaterialize(paths, state.PersistOptions{
		ActiveProfileName: "oldprof",
		NewPlan:           plan,
		NewMerged:         merged,
	})
	if err != nil {
		t.Fatalf("PersistAndMaterialize: %v", err)
	}
	// New live = clean.md (drifted.md is gone — replaced by new tree).
	if exists, _ := state.PathExists(filepath.Join(paths.ClaudeDir, "drifted.md")); exists {
		t.Fatalf("drifted.md survived materialize")
	}
	got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "clean.md"))
	if string(got) != "CLEAN" {
		t.Fatalf("clean.md = %q", got)
	}
	// Persisted snapshot lives under .claude-profiles/oldprof/.claude/drifted.md.
	persisted, err := os.ReadFile(filepath.Join(paths.ProfilesDir, "oldprof", ".claude", "drifted.md"))
	if err != nil {
		t.Fatalf("read persisted: %v", err)
	}
	if string(persisted) != "USER EDIT" {
		t.Fatalf("persisted edit = %q, want USER EDIT", persisted)
	}
	if res.State.ActiveProfile == nil || *res.State.ActiveProfile != "newprof" {
		t.Fatalf("activeProfile = %v, want newprof", res.State.ActiveProfile)
	}
}
