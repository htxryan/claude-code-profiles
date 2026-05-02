package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/state"
)

// crashInjectionMerged is the canonical materialize input for the crash
// recovery tests — two .claude/-destination files exercising both the merge
// policy variants in the rename pair.
func crashInjectionMerged(content string) []merge.MergedFile {
	return []merge.MergedFile{
		mergedFile("CLAUDE.md", content),
		mergedFile("agents/x.md", "AGENT"),
	}
}

// TestCrashRecovery_PostPendingPreRenameB is one of the two MANDATORY
// pre-1.0 crash cases (PR6 #3 second mandatory case in disguise as our
// "post-.state.json.tmp-write-pre-rename" relative; the first listed
// mandatory case is exercised by TestCrashRecovery_MidPriorRenameB below).
//
// On-disk shape: .pending/ from a prior crashed write exists; live .claude/
// is missing or contains unrelated bytes. Expected: reconcile drops the
// stale pending, materialize commits the new plan cleanly with no leftover
// artifacts.
func TestCrashRecovery_PostPendingPreRenameB(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.PendingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PendingDir, "STALE.md"), []byte("STALE"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	plan := makePlan("leaf")
	merged := crashInjectionMerged("LEAF-V1")
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "LEAF-V1" {
		t.Fatalf("CLAUDE.md = %q, want LEAF-V1", got)
	}
	if exists, _ := state.PathExists(filepath.Join(paths.ClaudeDir, "STALE.md")); exists {
		t.Fatalf("stale pending bytes leaked into live tree")
	}
	if exists, _ := state.PathExists(paths.PendingDir); exists {
		t.Fatalf("pending dir survived materialize")
	}
	if exists, _ := state.PathExists(paths.PriorDir); exists {
		t.Fatalf("prior dir survived materialize")
	}
}

// TestCrashRecovery_MidPriorRenameB is the SECOND of the two mandatory
// pre-1.0 cases (PR6 #3 — "mid-.claude/→.prior/ rename"). On-disk shape:
// .prior/ exists from a previous step b that committed; .claude/ is missing
// (step c never started). Reconcile must restore from .prior/, THEN the new
// materialize commits.
func TestCrashRecovery_MidPriorRenameB(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.PriorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PriorDir, "PRIOR.md"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Pending may also have been left behind from a clobbered step a.
	if err := os.MkdirAll(paths.PendingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PendingDir, "PENDING.md"), []byte("PENDING"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	plan := makePlan("leaf")
	merged := crashInjectionMerged("LEAF-V1")
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	// After reconcile + materialize: live tree is the new plan. Neither
	// PRIOR.md nor PENDING.md persists.
	if exists, _ := state.PathExists(filepath.Join(paths.ClaudeDir, "PRIOR.md")); exists {
		t.Fatalf("PRIOR.md leaked into live tree")
	}
	if exists, _ := state.PathExists(filepath.Join(paths.ClaudeDir, "PENDING.md")); exists {
		t.Fatalf("PENDING.md leaked into live tree")
	}
	got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if string(got) != "LEAF-V1" {
		t.Fatalf("CLAUDE.md = %q", got)
	}
	if exists, _ := state.PathExists(paths.PendingDir); exists {
		t.Fatalf("pending dir survived")
	}
	if exists, _ := state.PathExists(paths.PriorDir); exists {
		t.Fatalf("prior dir survived")
	}
}

// TestCrashRecovery_StaleStateFile covers injection point 4: .claude/ swapped
// successfully but state.json was never updated (or points at a different
// profile). Re-running materialize must update the state file.
func TestCrashRecovery_StaleStateFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)

	// Simulate full step c success, but state file points elsewhere.
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("OLD-LIVE"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	staleProfile := "stale"
	staleAt := "2026-01-01T00:00:00.000Z"
	stale := state.DefaultState()
	stale.ActiveProfile = &staleProfile
	stale.MaterializedAt = &staleAt
	if err := state.WriteStateFile(paths, stale); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	plan := makePlan("leaf")
	if _, err := state.Materialize(paths, plan, crashInjectionMerged("LEAF-V1"), state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	res, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if res.State.ActiveProfile == nil || *res.State.ActiveProfile != "leaf" {
		t.Fatalf("activeProfile = %v, want leaf", res.State.ActiveProfile)
	}
}

// TestCrashRecovery_RepeatedSwapsConverge asserts the protocol is idempotent
// across many swaps — no debris accumulates and the last plan wins.
func TestCrashRecovery_RepeatedSwapsConverge(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	for i := 0; i < 5; i++ {
		plan := makePlan("leaf")
		merged := crashInjectionMerged("V" + itoa(i))
		if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, ""); err != nil {
			t.Fatalf("Materialize #%d: %v", i, err)
		}
	}
	got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if string(got) != "V4" {
		t.Fatalf("final CLAUDE.md = %q, want V4", got)
	}
	if exists, _ := state.PathExists(paths.PendingDir); exists {
		t.Fatalf("pending debris after 5 swaps")
	}
	if exists, _ := state.PathExists(paths.PriorDir); exists {
		t.Fatalf("prior debris after 5 swaps")
	}
}

// TestCrashRecovery_TmpStateWriteCrash covers injection point ".state.json.tmp
// written but rename never happened" — the tmp file is left under
// .meta/tmp/ and the live state.json is the previous version. The next
// materialize must overwrite both. We simulate by hand-crafting a stale tmp
// file to assert it doesn't confuse the writer.
func TestCrashRecovery_TmpStateWriteCrash(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.TmpDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(paths.TmpDir, "state.json.99999.0-deadbeef.tmp")
	if err := os.WriteFile(stale, []byte("{stale truncated"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan := makePlan("leaf")
	if _, err := state.Materialize(paths, plan, crashInjectionMerged("LEAF-V1"), state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	res, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if res.Warning != nil {
		t.Fatalf("read warning = %+v after recovery; expected clean read", res.Warning)
	}
	if res.State.ActiveProfile == nil || *res.State.ActiveProfile != "leaf" {
		t.Fatalf("activeProfile = %v, want leaf", res.State.ActiveProfile)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
