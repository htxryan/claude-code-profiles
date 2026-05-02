package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/internal/drift"
	pipelineerrors "github.com/htxryan/claude-code-config-profiles/internal/errors"
	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

// TestSwapMissingProfileSurfacesPipelineError ensures resolution failures
// propagate verbatim so the CLI's outer ExitCodeFor classifies them.
func TestSwapMissingProfileSurfacesPipelineError(t *testing.T) {
	tmp := t.TempDir()
	paths := state.BuildStatePaths(tmp)
	if err := os.MkdirAll(paths.ProfilesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := RunSwap(SwapOptions{
		Paths:         paths,
		TargetProfile: "ghost",
		Mode:          drift.GateModeNonInteractive,
		OnDriftFlag:   drift.GateChoiceAbort,
	})
	if err == nil {
		t.Fatalf("expected MissingProfileError, got nil")
	}
	var mpe *pipelineerrors.MissingProfileError
	if !errors.As(err, &mpe) {
		t.Fatalf("expected *MissingProfileError, got %T: %v", err, err)
	}
	if mpe.Missing != "ghost" {
		t.Fatalf("missing name mismatch: %q", mpe.Missing)
	}
}

// TestSwapNonInteractiveAbortsWithoutFlagOnDrift covers the PR29 hard-block.
// Setup a project with a materialized profile, mutate live .claude/, then
// run a sync-equivalent in non-interactive mode without --on-drift=.
func TestSwapNonInteractiveAbortsWithoutFlagOnDrift(t *testing.T) {
	tmp := t.TempDir()
	paths := state.BuildStatePaths(tmp)
	if err := os.MkdirAll(filepath.Join(paths.ProfilesDir, "p", ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ProfilesDir, "p", "profile.json"), []byte(`{"name":"p"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ProfilesDir, "p", ".claude", "f.md"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// First swap to establish state.
	_, err := RunSwap(SwapOptions{
		Paths:         paths,
		TargetProfile: "p",
		Mode:          drift.GateModeNonInteractive,
		OnDriftFlag:   drift.GateChoiceAbort,
	})
	if err != nil {
		t.Fatalf("first swap: %v", err)
	}
	// Mutate live to create drift.
	if err := os.WriteFile(filepath.Join(tmp, ".claude", "f.md"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = RunSwap(SwapOptions{
		Paths:         paths,
		TargetProfile: "p",
		Mode:          drift.GateModeNonInteractive,
		OnDriftFlag:   "",
	})
	if !IsSwapAbort(err) {
		t.Fatalf("expected SwapAbortError, got %v", err)
	}
}

// TestSwapWithDiscardFlagDoesntPrompt verifies the flag wins over prompt.
func TestSwapWithDiscardFlagDoesntPrompt(t *testing.T) {
	tmp := t.TempDir()
	paths := state.BuildStatePaths(tmp)
	if err := os.MkdirAll(filepath.Join(paths.ProfilesDir, "p", ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ProfilesDir, "p", "profile.json"), []byte(`{"name":"p"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ProfilesDir, "p", ".claude", "f.md"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunSwap(SwapOptions{
		Paths: paths, TargetProfile: "p",
		Mode: drift.GateModeNonInteractive, OnDriftFlag: drift.GateChoiceAbort,
	}); err != nil {
		t.Fatalf("first swap: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".claude", "f.md"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use interactive mode but pass --on-drift=discard — the flag wins.
	promptCalled := false
	result, err := RunSwap(SwapOptions{
		Paths: paths, TargetProfile: "p",
		Mode:        drift.GateModeInteractive,
		OnDriftFlag: drift.GateChoiceDiscard,
		PromptFunc: func() drift.GateChoice {
			promptCalled = true
			return drift.GateChoiceAbort
		},
	})
	if err != nil {
		t.Fatalf("swap with --on-drift=discard: %v", err)
	}
	if promptCalled {
		t.Fatalf("--on-drift=discard should not invoke the prompt")
	}
	if result.Action != drift.ApplyActionMaterialized {
		t.Fatalf("want materialized, got %q", result.Action)
	}
	if result.BackupSnapshot == nil {
		t.Fatalf("discard path should have a backup snapshot")
	}
}
