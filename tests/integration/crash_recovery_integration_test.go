package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/c3p/tests/integration/helpers"
)

// IV/T8 — translation of TS crash-recovery.test.ts (F2 gap closure #3).
// Pins the 2 mandatory pre-1.0 crash-recovery cases (port spec §8):
//
//   (a) post-state.json.tmp-write-pre-rename — the writer crashed after
//       fsync but before atomic rename. The next invocation must reconcile.
//   (b) mid-`.claude/`→`.prior/` rename — the materializer crashed mid
//       backup-then-promote dance. The next invocation must reconcile.
//
// Approach: simulate the crashed state on disk (orphan tmp / orphan prior /
// orphan pending), then run the next CLI invocation and assert it
// recovers cleanly.
//
// Suffix `_integration` to disambiguate from internal/state/
// crash_recovery_test.go (which exercises the reconcile primitive
// directly, in-process).

// setupActive seeds an active profile via the CLI. Returns the project root.
// `which` selects which of {"a", "b"} to materialize first.
func setupActive(t *testing.T, which string) string {
	t.Helper()
	fx := helpers.MakeFixture(t, helpers.FixtureSpec{
		Profiles: map[string]helpers.ProfileSpec{
			"a": {
				Manifest: map[string]any{"name": "a"},
				Files:    map[string]string{"CLAUDE.md": "A\n", "settings.json": `{"v":"a"}`},
			},
			"b": {
				Manifest: map[string]any{"name": "b"},
				Files:    map[string]string{"CLAUDE.md": "B\n", "settings.json": `{"v":"b"}`},
			},
		},
	})
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", fx.ProjectRoot, "use", which}})
	if r.ExitCode != 0 {
		t.Fatalf("setup use %s: want 0, got %d (stderr=%q)", which, r.ExitCode, r.Stderr)
	}
	return fx.ProjectRoot
}

// TestCrashRecoveryIntegration_OrphanStateTmpUseSucceeds — case (a).
// An orphan `.meta/state.json.tmp` (left by a crashed atomic write) must
// not confuse the next swap; the canonical state.json is updated and the
// active profile reflects the new selection.
func TestCrashRecoveryIntegration_OrphanStateTmpUseSucceeds(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActive(t, "a")
	stateDir := filepath.Join(root, ".claude-profiles", ".meta")
	tmpFile := filepath.Join(stateDir, "state.json.tmp")
	if err := os.WriteFile(tmpFile, []byte("GARBAGE-FROM-CRASHED-WRITE"), 0o644); err != nil {
		t.Fatalf("write orphan tmp: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", root, "use", "b"}})
	if r.ExitCode != 0 {
		t.Fatalf("use b after orphan tmp: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	stateBytes, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var state struct {
		ActiveProfile string `json:"activeProfile"`
	}
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatalf("parse state.json: %v (raw=%q)", err, stateBytes)
	}
	if state.ActiveProfile != "b" {
		t.Errorf("activeProfile: want 'b', got %q", state.ActiveProfile)
	}
}

// TestCrashRecoveryIntegration_OrphanStateTmpStatusReadsCanonical — read
// verbs (status) must read state.json, not the orphan tmp.
func TestCrashRecoveryIntegration_OrphanStateTmpStatusReadsCanonical(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActive(t, "a")
	if err := os.WriteFile(filepath.Join(root, ".claude-profiles", ".meta", "state.json.tmp"), []byte("GARBAGE"), 0o644); err != nil {
		t.Fatalf("write orphan tmp: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", root, "status"}})
	if r.ExitCode != 0 {
		t.Fatalf("status with orphan tmp: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "active profile: a") {
		t.Errorf("status stdout missing 'active profile: a': %q", r.Stdout)
	}
}

// TestCrashRecoveryIntegration_OrphanInTmpDirDoesNotBreakUse — orphan in
// the canonical staging dir (`.meta/tmp/<unique>`) must not break the next
// op; the bin's per-call unique tmps don't collide with the orphan.
func TestCrashRecoveryIntegration_OrphanInTmpDirDoesNotBreakUse(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActive(t, "a")
	tmpDir := filepath.Join(root, ".claude-profiles", ".meta", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "orphan-xyz"), []byte("GARBAGE"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", root, "use", "b"}})
	if r.ExitCode != 0 {
		t.Fatalf("use b after orphan in tmp dir: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}

// TestCrashRecoveryIntegration_OrphanPriorMissingLiveRecovers — case (b),
// crash between rename steps: live `.claude/` was renamed to `.meta/prior`
// but the next rename never happened. The next swap must restore live
// from prior, then materialize the new selection. We pass --on-drift=
// discard because reconcile-from-prior may surface as drift (the live
// tree was missing, so the bin sees a fresh-tree-no-state edge).
func TestCrashRecoveryIntegration_OrphanPriorMissingLiveRecovers(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActive(t, "a")
	live := filepath.Join(root, ".claude")
	prior := filepath.Join(root, ".claude-profiles", ".meta", "prior")
	if err := os.Rename(live, prior); err != nil {
		t.Fatalf("rename live → prior: %v", err)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatalf("live should be gone after rename: stat=%v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", root, "--on-drift=discard", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b after orphan prior: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	body, err := os.ReadFile(filepath.Join(live, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live CLAUDE.md: %v", err)
	}
	if string(body) != "B\n" {
		t.Errorf("live CLAUDE.md: want \"B\\n\", got %q", body)
	}
	// prior/ is gone (post-success cleanup).
	if _, err := os.Stat(prior); !os.IsNotExist(err) {
		t.Errorf("prior/ should be cleaned: stat=%v", err)
	}
}

// TestCrashRecoveryIntegration_OrphanPriorWithLivePresentRecovers —
// crash AFTER promote, BEFORE prior cleanup. live `.claude/` is the new
// content, prior/ has the old content. The next swap reconciles cleanly.
func TestCrashRecoveryIntegration_OrphanPriorWithLivePresentRecovers(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActive(t, "a")
	prior := filepath.Join(root, ".claude-profiles", ".meta", "prior")
	if err := os.MkdirAll(prior, 0o755); err != nil {
		t.Fatalf("mkdir prior: %v", err)
	}
	// Snapshot the previous successful state into prior/ — same content as
	// the recorded fingerprint for "a", simulating "prior reflects the
	// last successful materialize".
	if err := os.WriteFile(filepath.Join(prior, "CLAUDE.md"), []byte("A\n"), 0o644); err != nil {
		t.Fatalf("write prior CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prior, "settings.json"), []byte(`{"v":"a"}`), 0o644); err != nil {
		t.Fatalf("write prior settings.json: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", root, "--on-drift=discard", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("use b after orphan prior+live: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	body, err := os.ReadFile(filepath.Join(root, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live CLAUDE.md: %v", err)
	}
	if string(body) != "B\n" {
		t.Errorf("live CLAUDE.md: want \"B\\n\", got %q", body)
	}
	if _, err := os.Stat(prior); !os.IsNotExist(err) {
		t.Errorf("prior/ should be cleaned: stat=%v", err)
	}
}

// TestCrashRecoveryIntegration_OrphanPendingRecovers — crash mid-build:
// the materializer wrote partial content to `.meta/pending/` then died.
// The next swap must clean the orphan and proceed.
func TestCrashRecoveryIntegration_OrphanPendingRecovers(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActive(t, "a")
	pending := filepath.Join(root, ".claude-profiles", ".meta", "pending")
	if err := os.MkdirAll(pending, 0o755); err != nil {
		t.Fatalf("mkdir pending: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pending, "CLAUDE.md"), []byte("PARTIAL\n"), 0o644); err != nil {
		t.Fatalf("write partial: %v", err)
	}

	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", root, "use", "b"}})
	if r.ExitCode != 0 {
		t.Fatalf("use b after orphan pending: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	body, err := os.ReadFile(filepath.Join(root, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read live CLAUDE.md: %v", err)
	}
	if string(body) != "B\n" {
		t.Errorf("live CLAUDE.md: want \"B\\n\", got %q", body)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Errorf("pending/ should be cleaned: stat=%v", err)
	}
}

// TestCrashRecoveryIntegration_MissingStateFileGracefulReinit — the
// state.json file was deleted entirely (e.g. user `rm -rf .meta`). The
// next status invocation must report NoActive without crashing, and a
// follow-up `use` must succeed.
func TestCrashRecoveryIntegration_MissingStateFileGracefulReinit(t *testing.T) {
	helpers.EnsureBuilt(t)
	root := setupActive(t, "a")
	stateFile := filepath.Join(root, ".claude-profiles", ".meta", "state.json")
	if err := os.Remove(stateFile); err != nil {
		t.Fatalf("remove state.json: %v", err)
	}
	r := mustRun(t, helpers.SpawnOptions{Args: []string{"--cwd", root, "status"}})
	if r.ExitCode != 0 {
		t.Fatalf("status after rm state.json: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "(none)") {
		t.Errorf("status stdout missing '(none)': %q", r.Stdout)
	}
	// Follow-up `use` must succeed (the prior live tree is drift, so we
	// pass --on-drift=discard).
	r = mustRun(t, helpers.SpawnOptions{
		Args: []string{"--cwd", root, "--on-drift=discard", "use", "b"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("follow-up use b: want 0, got %d (stderr=%q)", r.ExitCode, r.Stderr)
	}
}
