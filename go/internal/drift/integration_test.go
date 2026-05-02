package drift_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/htxryan/c3p/internal/drift"
	"github.com/htxryan/c3p/internal/state"
)

// S3: drift gate — discard → live tree replaced, edits in snapshot.
func TestIntegration_S3_DiscardReplacesAndSnapshots(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("DRIFT FROM USER\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(report.Entries))
	}
	decision := drift.DecideGate(drift.GateInput{Report: report, Mode: drift.GateModeInteractive})
	if decision.Kind != drift.GateOutcomePrompt {
		t.Errorf("decision kind = %q, want prompt", decision.Kind)
	}
	// Simulate the user picking discard.
	otherOpts.ActiveProfileName = report.Active
	res, err := drift.ApplyGate(drift.GateChoiceDiscard, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionMaterialized {
		t.Errorf("action = %q, want materialized", res.Action)
	}
	if res.BackupSnapshot == "" {
		t.Fatalf("backupSnapshot empty")
	}
	live, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile live: %v", err)
	}
	if string(live) != "OTHER\n" {
		t.Errorf("live = %q, want OTHER\\n", live)
	}
	got, err := os.ReadFile(filepath.Join(res.BackupSnapshot, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile snapshot: %v", err)
	}
	if string(got) != "DRIFT FROM USER\n" {
		t.Errorf("snapshot = %q, want DRIFT FROM USER\\n", got)
	}
	snaps, err := state.ListSnapshots(paths)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("snaps = %d, want 1", len(snaps))
	}
}

// S4: drift gate — persist → live tree saved into active profile, then swap.
func TestIntegration_S4_PersistSavesIntoActiveProfile(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("EDITED LEAF\n"), 0o644); err != nil {
		t.Fatalf("WriteFile edit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "scratch.md"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile scratch: %v", err)
	}

	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	gotPaths := []string{}
	for _, e := range report.Entries {
		gotPaths = append(gotPaths, e.RelPath)
	}
	want := []string{"CLAUDE.md", "scratch.md"}
	if len(gotPaths) != len(want) || gotPaths[0] != want[0] || gotPaths[1] != want[1] {
		t.Errorf("entry paths = %v, want %v", gotPaths, want)
	}

	otherOpts.ActiveProfileName = report.Active
	res, err := drift.ApplyGate(drift.GateChoicePersist, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionPersistedAndMaterialized {
		t.Errorf("action = %q, want persisted-and-materialized", res.Action)
	}
	persistedDir := filepath.Join(paths.ProfilesDir, "leaf", ".claude")
	if got, _ := os.ReadFile(filepath.Join(persistedDir, "CLAUDE.md")); string(got) != "EDITED LEAF\n" {
		t.Errorf("persisted CLAUDE.md = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(persistedDir, "scratch.md")); string(got) != "scratch\n" {
		t.Errorf("persisted scratch = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md")); string(got) != "OTHER\n" {
		t.Errorf("live = %q, want OTHER\\n", got)
	}
	snaps, err := state.ListSnapshots(paths)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("snaps = %d, want 0 on persist path", len(snaps))
	}
}

// S6: drift gate — abort → no FS change, state unchanged.
func TestIntegration_S6_AbortLeavesStateUnchanged(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("DRIFT\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	stateBefore, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	beforeBytes, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	otherOpts.ActiveProfileName = report.Active
	res, err := drift.ApplyGate(drift.GateChoiceAbort, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionAborted {
		t.Errorf("action = %q, want aborted", res.Action)
	}
	stateAfter, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if (stateAfter.State.ActiveProfile == nil) != (stateBefore.State.ActiveProfile == nil) {
		t.Errorf("active profile presence changed across abort")
	}
	if stateBefore.State.ActiveProfile != nil && *stateAfter.State.ActiveProfile != *stateBefore.State.ActiveProfile {
		t.Errorf("active profile = %q, want %q (abort should not change state)",
			*stateAfter.State.ActiveProfile, *stateBefore.State.ActiveProfile)
	}
	got, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile after: %v", err)
	}
	if string(got) != string(beforeBytes) {
		t.Errorf("file changed across abort: %q -> %q", beforeBytes, got)
	}
}

// S15-extension: persist split-brain — if killed mid-flow, next swap reconciles.
func TestIntegration_S15Ext_PersistReconcilesAfterStalePending(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("EDITED\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Inject a stale persist-side .pending/ to simulate a half-finished
	// prior run.
	profileDir := filepath.Join(paths.ProfilesDir, "leaf")
	stalePending := filepath.Join(profileDir, ".pending")
	if err := os.MkdirAll(stalePending, 0o755); err != nil {
		t.Fatalf("MkdirAll stalePending: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stalePending, "STALE_GHOST"), []byte("ghost"), 0o644); err != nil {
		t.Fatalf("WriteFile ghost: %v", err)
	}

	otherOpts.ActiveProfileName = "leaf"
	res, err := drift.ApplyGate(drift.GateChoicePersist, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionPersistedAndMaterialized {
		t.Errorf("action = %q, want persisted-and-materialized", res.Action)
	}
	persistedDir := filepath.Join(profileDir, ".claude")
	ghost := filepath.Join(persistedDir, "STALE_GHOST")
	if _, err := os.Stat(ghost); !os.IsNotExist(err) {
		t.Errorf("STALE_GHOST should not exist in persisted dir: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(persistedDir, "CLAUDE.md")); string(got) != "EDITED\n" {
		t.Errorf("persisted CLAUDE.md = %q, want EDITED\\n", got)
	}
}

// Non-interactive auto-abort flow: drift + non-interactive + no flag → no change.
func TestIntegration_NonInteractiveAutoAbortNoChange(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("DRIFT\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	decision := drift.DecideGate(drift.GateInput{Report: report, Mode: drift.GateModeNonInteractive})
	if decision.Kind != drift.GateOutcomeAuto {
		t.Errorf("decision kind = %q, want auto", decision.Kind)
	}
	if decision.Choice != drift.GateChoiceAbort {
		t.Errorf("decision choice = %q, want abort", decision.Choice)
	}
	otherOpts.ActiveProfileName = report.Active
	res, err := drift.ApplyGate(decision.Choice, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionAborted {
		t.Errorf("action = %q, want aborted", res.Action)
	}
	if got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md")); string(got) != "DRIFT\n" {
		t.Errorf("live = %q, want DRIFT\\n", got)
	}
}

// Clean-swap flow: no drift → no-drift-proceed → materialize.
func TestIntegration_CleanSwapNoDriftProceed(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	report, err := drift.DetectDrift(paths)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(report.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(report.Entries))
	}
	decision := drift.DecideGate(drift.GateInput{Report: report, Mode: drift.GateModeInteractive})
	if decision.Kind != drift.GateOutcomeNoDrift {
		t.Errorf("decision kind = %q, want no-drift", decision.Kind)
	}
	otherOpts.ActiveProfileName = report.Active
	res, err := drift.ApplyGate(decision.Choice, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionMaterialized {
		t.Errorf("action = %q, want materialized", res.Action)
	}
	if res.BackupSnapshot != "" {
		t.Errorf("backupSnapshot = %q, want empty", res.BackupSnapshot)
	}
	r, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if r.State.ActiveProfile == nil || *r.State.ActiveProfile != "other" {
		t.Errorf("active profile = %v, want other", r.State.ActiveProfile)
	}
}
