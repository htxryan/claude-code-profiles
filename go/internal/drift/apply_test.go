package drift_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/internal/drift"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

// derefActive returns "" when the DriftReport's Active field is nil,
// otherwise the dereferenced value. Used by tests that fold the active
// profile name into ApplyGateOptions where empty == "no active profile".
func derefActive(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// makePlanFor returns a minimal plan for ApplyGate persist tests. Note
// the activeProfile string is what gets recorded in state.json after
// materialize; ApplyGate's persist path uses it to build the persist target.
func makePlanFor(profileName string) resolver.ResolvedPlan {
	return resolver.ResolvedPlan{
		SchemaVersion: resolver.ResolvedPlanSchemaVersion,
		ProfileName:   profileName,
		Contributors: []resolver.Contributor{
			{Kind: resolver.ContributorProfile, ID: profileName, RootPath: "/abs/" + profileName},
		},
		Files:         []resolver.PlanFile{},
		Warnings:      []resolver.ResolutionWarning{},
		ExternalPaths: []resolver.ExternalTrustEntry{},
	}
}

func setupTwoProfiles(t *testing.T) (state.StatePaths, drift.ApplyGateOptions, drift.ApplyGateOptions) {
	t.Helper()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)

	leafPlan := makePlanFor("leaf")
	leafMerged := []merge.MergedFile{
		driftMergedFile("CLAUDE.md", "BASE\nLEAF\n"),
		driftMergedFile("agents/a.md", "AGENT-A"),
	}
	otherPlan := makePlanFor("other")
	otherMerged := []merge.MergedFile{
		driftMergedFile("CLAUDE.md", "OTHER\n"),
	}
	// Materialize leaf as the starting state.
	if _, err := state.Materialize(paths, leafPlan, leafMerged, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize leaf: %v", err)
	}
	leafOpts := drift.ApplyGateOptions{
		Paths: paths, Plan: leafPlan, Merged: leafMerged, ActiveProfileName: "leaf",
	}
	otherOpts := drift.ApplyGateOptions{
		Paths: paths, Plan: otherPlan, Merged: otherMerged, ActiveProfileName: "leaf",
	}
	return paths, leafOpts, otherOpts
}

// R24: abort makes no FS changes.
func TestApplyGate_R24_AbortNoFsChanges(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	target := filepath.Join(paths.ClaudeDir, "CLAUDE.md")
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	res, err := drift.ApplyGate(drift.GateChoiceAbort, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionAborted {
		t.Errorf("action = %q, want aborted", res.Action)
	}
	if res.MaterializeResult != nil {
		t.Errorf("materializeResult = %v, want nil on abort", res.MaterializeResult)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile after: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("file changed during abort: %q -> %q", before, after)
	}
}

// no-drift-proceed materializes without taking a snapshot.
func TestApplyGate_NoDriftProceedMaterializesWithoutSnapshot(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	res, err := drift.ApplyGate(drift.GateChoiceNoDriftProceed, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionMaterialized {
		t.Errorf("action = %q, want materialized", res.Action)
	}
	if res.BackupSnapshot != "" {
		t.Errorf("backupSnapshot = %q, want empty on no-drift", res.BackupSnapshot)
	}
	live, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(live) != "OTHER\n" {
		t.Errorf("live CLAUDE.md = %q, want OTHER\\n", live)
	}
	snaps, err := state.ListSnapshots(paths)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("snapshots taken on no-drift path: %v", snaps)
	}
}

// R23 + R23a: discard takes a backup snapshot before materializing.
func TestApplyGate_R23_DiscardBacksUpBeforeMaterialize(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	target := filepath.Join(paths.ClaudeDir, "CLAUDE.md")
	if err := os.WriteFile(target, []byte("DRIFTED\n"), 0o644); err != nil {
		t.Fatalf("WriteFile drift: %v", err)
	}

	res, err := drift.ApplyGate(drift.GateChoiceDiscard, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionMaterialized {
		t.Errorf("action = %q, want materialized", res.Action)
	}
	if res.BackupSnapshot == "" {
		t.Fatalf("backupSnapshot empty; expected discard to record one")
	}
	if exists, _ := state.PathExists(res.BackupSnapshot); !exists {
		t.Errorf("backup dir does not exist on disk: %q", res.BackupSnapshot)
	}
	// Snapshot must contain the drifted content (taken before materialize).
	snapshotClaudeMd, err := os.ReadFile(filepath.Join(res.BackupSnapshot, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile snapshot: %v", err)
	}
	if string(snapshotClaudeMd) != "DRIFTED\n" {
		t.Errorf("snapshot content = %q, want DRIFTED\\n", snapshotClaudeMd)
	}
	// Live tree is now the new profile.
	live, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile live: %v", err)
	}
	if string(live) != "OTHER\n" {
		t.Errorf("live = %q, want OTHER\\n", live)
	}
}

// R22 + R22a + R22b: persist copies live tree into active profile, then materializes new.
func TestApplyGate_R22_PersistCopiesIntoActiveProfile(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "scratch.md"), []byte("scratch from live\n"), 0o644); err != nil {
		t.Fatalf("WriteFile scratch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("EDITED LEAF\n"), 0o644); err != nil {
		t.Fatalf("WriteFile edit: %v", err)
	}

	res, err := drift.ApplyGate(drift.GateChoicePersist, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionPersistedAndMaterialized {
		t.Errorf("action = %q, want persisted-and-materialized", res.Action)
	}

	persistedDir := filepath.Join(paths.ProfilesDir, "leaf", ".claude")
	pcm, err := os.ReadFile(filepath.Join(persistedDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile persisted: %v", err)
	}
	if string(pcm) != "EDITED LEAF\n" {
		t.Errorf("persisted CLAUDE.md = %q, want EDITED LEAF\\n", pcm)
	}
	pscratch, err := os.ReadFile(filepath.Join(persistedDir, "scratch.md"))
	if err != nil {
		t.Fatalf("ReadFile persisted scratch: %v", err)
	}
	if string(pscratch) != "scratch from live\n" {
		t.Errorf("persisted scratch = %q, want %q", pscratch, "scratch from live\n")
	}
	live, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile live: %v", err)
	}
	if string(live) != "OTHER\n" {
		t.Errorf("live = %q, want OTHER\\n", live)
	}
	r, err := state.ReadStateFile(paths)
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if r.State.ActiveProfile == nil || *r.State.ActiveProfile != "other" {
		t.Errorf("active profile = %v, want other", r.State.ActiveProfile)
	}
}

// Persist with no active profile is a programmer error: defense-in-depth.
func TestApplyGate_PersistRequiresActiveProfile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	opts := drift.ApplyGateOptions{
		Paths:             paths,
		Plan:              makePlanFor("other"),
		Merged:            []merge.MergedFile{driftMergedFile("CLAUDE.md", "OTHER\n")},
		ActiveProfileName: "",
	}
	_, err := drift.ApplyGate(drift.GateChoicePersist, opts)
	if err == nil {
		t.Fatalf("expected error for persist with empty activeProfileName")
	}
	if !strings.Contains(err.Error(), "active profile") {
		t.Errorf("error message = %q, want substring 'active profile'", err.Error())
	}
}

// PR25: when SnapshotForDiscard succeeds but Materialize then fails (here
// because the project-root markers were removed mid-run), ApplyGate must
// still surface the backup path in the result. The user's edits are on disk
// at that path; without the path, D7 cannot tell the user where their work
// went and the snapshot dir gets orphaned.
func TestApplyGate_DiscardSurfacesBackupOnMaterializeFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)

	// Stand up a project with a project-root section so Materialize takes
	// the splice path (which has a preflight that fails on missing markers).
	beforeMarkers := "# project\n\n"
	managed := "<!-- c3p:v1:begin -->\nORIG\n<!-- c3p:v1:end -->\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(beforeMarkers+managed), 0o644); err != nil {
		t.Fatalf("WriteFile root CLAUDE.md: %v", err)
	}
	plan := makePlanFor("leaf")
	merged := []merge.MergedFile{
		{
			Path:          "CLAUDE.md",
			Bytes:         []byte("ORIG"),
			Contributors:  []string{"leaf"},
			MergePolicy:   resolver.MergePolicyConcat,
			Destination:   resolver.DestinationProjectRoot,
			SchemaVersion: merge.MergedFileSchemaVersion,
		},
	}
	if _, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, ""); err != nil {
		t.Fatalf("Materialize initial: %v", err)
	}
	// User adds drift to .claude/ AND strips the project-root markers — the
	// snapshot will succeed (it copies .claude/ as-is) but the subsequent
	// Materialize will fail at preflight because the markers are gone.
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "drift.md"), []byte("user work\n"), 0o644); err != nil {
		t.Fatalf("WriteFile drift: %v", err)
	}
	if err := os.WriteFile(paths.RootClaudeMdFile, []byte("# project\n\nNo markers anymore.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile strip markers: %v", err)
	}

	opts := drift.ApplyGateOptions{
		Paths: paths, Plan: plan, Merged: merged, ActiveProfileName: "leaf",
	}
	res, err := drift.ApplyGate(drift.GateChoiceDiscard, opts)
	if err == nil {
		t.Fatalf("expected Materialize to fail when markers are missing, got nil error")
	}
	if res.Action != drift.ApplyActionAborted {
		t.Errorf("action = %q, want aborted on post-snapshot failure", res.Action)
	}
	if res.BackupSnapshot == "" {
		t.Fatalf("BackupSnapshot empty on Materialize failure; user's edits are orphaned")
	}
	if exists, _ := state.PathExists(res.BackupSnapshot); !exists {
		t.Errorf("backup dir missing on disk: %q", res.BackupSnapshot)
	}
	// And the user's drift.md must be inside the backup so they can recover it.
	if _, rerr := os.Stat(filepath.Join(res.BackupSnapshot, "drift.md")); rerr != nil {
		t.Errorf("backup missing user file drift.md: %v", rerr)
	}
}

func TestApplyGate_UnknownChoiceErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	opts := drift.ApplyGateOptions{
		Paths:  paths,
		Plan:   makePlanFor("x"),
		Merged: []merge.MergedFile{},
	}
	_, err := drift.ApplyGate(drift.GateChoice("garbage"), opts)
	if err == nil {
		t.Fatalf("expected error for unknown choice")
	}
}
