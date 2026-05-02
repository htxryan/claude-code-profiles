package integration_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// TestGateMatrix — IV/T3 translation of TS gate-matrix.test.ts.
//
// E7 fitness function: the drift gate state machine for non-interactive
// rows. The full documented matrix is 3x2 (discard/persist/abort x
// interactive/non-interactive); this file exercises the non-interactive
// row + sync re-materialise paths at the binary surface. Interactive
// branches are unit-level only (would need a pty harness).
//
// TS used in-process imports (resolve + merge + materialize) to set up
// "active = a, .claude/ drifted to EDIT". The Go translation drives the
// same setup through the CLI: `use a` materialises, then we write the
// drift directly. No internal Go imports.

// setupDrifted creates a two-profile fixture with `a` active and
// .claude/CLAUDE.md drifted to "EDIT\n". Mirrors the TS helper.
func setupDrifted(t *testing.T) *helpers.Fixture {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"CLAUDE.md": "A\n"}},
			"b": {Manifest: map[string]any{"name": "b"}, Files: map[string]string{"CLAUDE.md": "B\n"}},
		},
	})
	// Materialise `a` via the CLI.
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("setup use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Drift the live tree.
	if err := os.WriteFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"), []byte("EDIT\n"), 0o644); err != nil {
		t.Fatalf("write drift: %v", err)
	}
	return fx
}

// TestGateMatrix_NonInteractive_NoFlag — without --on-drift, exit 1 +
// stderr names the flag; live tree unchanged.
func TestGateMatrix_NonInteractive_NoFlag(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDrifted(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "b"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("use b no flag: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--on-drift=") {
		t.Errorf("stderr missing --on-drift= guidance: %q", r.Stderr)
	}
	got, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(got) != "EDIT\n" {
		t.Errorf("live tree mutated: want EDIT, got %q", got)
	}
}

// TestGateMatrix_NonInteractive_Discard — --on-drift=discard: exit 0 +
// new content live + backup snapshot equals pre-swap edited tree
// (R23a — pre-swap snapshot, not post-swap content).
func TestGateMatrix_NonInteractive_Discard(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDrifted(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=discard", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b discard: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go bin lowercases verb: "switched to b" (TS: "Switched to b").
	if !strings.Contains(strings.ToLower(r.Stdout), "switched to b") {
		t.Errorf("stdout missing 'switched to b': %q", r.Stdout)
	}
	// Backup snapshot directory must contain at least one timestamped dir,
	// and the snapshot bytes must equal the pre-swap drifted tree.
	backupDir := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "backup")
	dirents, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	var snapshotDirs []string
	for _, d := range dirents {
		if d.IsDir() {
			snapshotDirs = append(snapshotDirs, d.Name())
		}
	}
	if len(snapshotDirs) == 0 {
		t.Fatalf("no snapshot dir under %s", backupDir)
	}
	sort.Strings(snapshotDirs)
	snapshot, err := os.ReadFile(filepath.Join(backupDir, snapshotDirs[0], "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if string(snapshot) != "EDIT\n" {
		t.Errorf("snapshot bytes: want EDIT, got %q", snapshot)
	}
	// Live tree now matches b.
	live, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(live) != "B\n" {
		t.Errorf("live tree: want B, got %q", live)
	}
}

// TestGateMatrix_NonInteractive_Persist — --on-drift=persist: exit 0 +
// drift saved to active profile + new content live.
func TestGateMatrix_NonInteractive_Persist(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDrifted(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=persist", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b persist: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Previously-active profile a now has the edited content baked in.
	persisted, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude-profiles", "a", ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read persisted a: %v", err)
	}
	if string(persisted) != "EDIT\n" {
		t.Errorf("persisted a: want EDIT, got %q", persisted)
	}
	live, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(live) != "B\n" {
		t.Errorf("live: want B, got %q", live)
	}
}

// TestGateMatrix_NonInteractive_Abort — --on-drift=abort: exit 1 +
// state.json and live tree unchanged.
func TestGateMatrix_NonInteractive_Abort(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDrifted(t)
	statePath := filepath.Join(fx.ProjectRoot, ".claude-profiles", ".meta", "state.json")
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state.json before: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=abort", "use", "b"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("use b abort: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	live, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(live) != "EDIT\n" {
		t.Errorf("live tree mutated: want EDIT, got %q", live)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state.json after: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Errorf("state.json mutated under abort:\nbefore: %q\nafter:  %q", stateBefore, stateAfter)
	}
}

// TestGateMatrix_Sync_NoFlag — sync on a drifted active without
// --on-drift → exit 1 (S12 gate).
func TestGateMatrix_Sync_NoFlag(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDrifted(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "sync"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("sync no flag: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "--on-drift=") {
		t.Errorf("stderr missing --on-drift= guidance: %q", r.Stderr)
	}
}

// TestGateMatrix_Sync_Discard — sync --on-drift=discard on drifted
// active → exit 0 + live restored to source.
func TestGateMatrix_Sync_Discard(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDrifted(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=discard", "sync"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("sync discard: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	// Go bin: "synced a" (lowercase). TS bin used "Synced a".
	if !strings.Contains(strings.ToLower(r.Stdout), "synced a") {
		t.Errorf("stdout missing 'synced a': %q", r.Stdout)
	}
	live, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(live) != "A\n" {
		t.Errorf("live: want A, got %q", live)
	}
}

// TestGateMatrix_Sync_Abort — sync --on-drift=abort → exit 1 + live
// tree unchanged.
func TestGateMatrix_Sync_Abort(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := setupDrifted(t)
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "--on-drift=abort", "sync"},
	})
	if r.ExitCode != 1 {
		t.Fatalf("sync abort: want 1, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	live, err := os.ReadFile(filepath.Join(fx.ProjectRoot, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(live) != "EDIT\n" {
		t.Errorf("live tree mutated: want EDIT, got %q", live)
	}
}

// TestGateMatrix_Sync_CleanActive — sync on a clean active is a no-op
// success (no drift → no gate).
func TestGateMatrix_Sync_CleanActive(t *testing.T) {
	helpers.EnsureBuilt(t)
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {Manifest: map[string]any{"name": "a"}, Files: map[string]string{"CLAUDE.md": "A\n"}},
		},
	})
	// Materialise a via the CLI (no drift → clean state).
	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "use", "a"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use a: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	r = mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", fx.ProjectRoot, "sync"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("sync clean: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "synced a") {
		t.Errorf("stdout missing 'synced a': %q", r.Stdout)
	}
}
