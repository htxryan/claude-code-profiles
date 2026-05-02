package drift_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/internal/drift"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

// PR25: backup notice on --quiet/--json non-interactive discard.
//
// In non-interactive mode (CI, scripts), the user has no terminal to read
// a banner — but the snapshot is the user's only recovery channel for
// unintentional discards. So ApplyGate's discard path MUST always surface
// the backup path via ApplyGateResult.BackupSnapshot, which the CLI emits
// in JSON output regardless of --quiet.
//
// We pin the property at the unit level: discard with non-empty live tree
// produces a non-empty BackupSnapshot. Whatever the CLI's verbosity flag,
// it has access to this value via the structured result.
func TestPR25_NonInteractiveDiscardSurfacesBackupPath(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"), []byte("DRIFT-IN-CI\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Simulate a non-interactive caller (CI script) that resolved gate by
	// flag: --on-drift=discard. ApplyGate doesn't care about the caller's
	// mode; the contract is that the BackupSnapshot is always populated on
	// the discard path so downstream JSON serialization can emit it.
	res, err := drift.ApplyGate(drift.GateChoiceDiscard, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.BackupSnapshot == nil {
		t.Fatalf("BackupSnapshot is nil; PR25 requires the discard path to surface a backup path even in non-interactive mode")
	}
	if exists, _ := state.PathExists(*res.BackupSnapshot); !exists {
		t.Errorf("BackupSnapshot dir does not exist on disk: %q", *res.BackupSnapshot)
	}
	// The backup must contain the user's pre-discard content.
	got, err := os.ReadFile(filepath.Join(*res.BackupSnapshot, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile snapshot: %v", err)
	}
	if string(got) != "DRIFT-IN-CI\n" {
		t.Errorf("snapshot content = %q, want DRIFT-IN-CI\\n", got)
	}
}

// PR25: discard with no live .claude/ produces a nil BackupSnapshot
// (nothing to back up — NoActive case, or active drift where the live tree
// was deleted). The CLI emits the BackupSnapshot field in JSON as `null`
// for shape consistency with TS (`string | null`).
func TestPR25_NonInteractiveDiscardWithNoActiveProducesNilBackup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	plan := makePlanFor("other")
	otherOpts := drift.ApplyGateOptions{
		Paths:             paths,
		Plan:              plan,
		Merged:            nil,
		ActiveProfileName: "",
	}
	// No prior materialize → no .claude/. SnapshotForDiscard returns nil.
	res, err := drift.ApplyGate(drift.GateChoiceDiscard, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	// The nil case is fine: there was nothing to back up, so the JSON
	// will carry `"backupSnapshot": null`. The action should still be
	// "materialized".
	if res.Action != drift.ApplyActionMaterialized {
		t.Errorf("action = %q, want materialized", res.Action)
	}
	if res.BackupSnapshot != nil {
		t.Errorf("BackupSnapshot = %q, want nil (nothing to back up)", *res.BackupSnapshot)
	}
}

// PR25: active profile with deleted live .claude/ — the user materialized
// once, then removed the entire tree (drift kind = DeletedAll). When discard
// is selected, SnapshotForDiscard MUST return nil because there is no live
// content to capture; nil is the correct surface for "no recovery channel
// available" rather than a faux empty snapshot. Pins the behavior carved
// out by SnapshotForDiscard's "no live tree → nil" contract so a future
// refactor can't quietly start emitting empty backup dirs (which would make
// retention accounting noisy and confuse users into thinking there's something
// to recover).
func TestPR25_NonInteractiveDiscardWithActiveProfileAndDeletedTreeProducesNilBackup(t *testing.T) {
	t.Parallel()
	paths, _, otherOpts := setupTwoProfiles(t)
	// Simulate active-profile-with-deleted-tree drift: the user removed
	// .claude/ outright between materialize and the next swap.
	if err := os.RemoveAll(paths.ClaudeDir); err != nil {
		t.Fatalf("RemoveAll live tree: %v", err)
	}
	if exists, _ := state.PathExists(paths.ClaudeDir); exists {
		t.Fatalf("precondition: live .claude/ still exists after RemoveAll")
	}

	res, err := drift.ApplyGate(drift.GateChoiceDiscard, otherOpts)
	if err != nil {
		t.Fatalf("ApplyGate: %v", err)
	}
	if res.Action != drift.ApplyActionMaterialized {
		t.Errorf("action = %q, want materialized (discard proceeds even with empty tree)", res.Action)
	}
	if res.BackupSnapshot != nil {
		t.Errorf("BackupSnapshot = %q, want nil (deleted tree → nothing to back up)", *res.BackupSnapshot)
	}
	// And no snapshot dirs were created on disk (the contract is "no copy
	// taken", not "empty dir created").
	snaps, err := state.ListSnapshots(paths)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("snapshots taken when nothing to back up: %v", snaps)
	}
	// And the new profile was materialized into the live tree.
	live, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile live: %v", err)
	}
	if string(live) != "OTHER\n" {
		t.Errorf("live CLAUDE.md = %q, want OTHER\\n", live)
	}
}
