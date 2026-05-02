package drift_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/htxryan/c3p/internal/drift"
	"github.com/htxryan/c3p/internal/state"
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
