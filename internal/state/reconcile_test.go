package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

func TestReconcileMaterialize_NoneWhenAllAbsent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	out, err := state.ReconcileMaterialize(paths)
	if err != nil {
		t.Fatalf("ReconcileMaterialize: %v", err)
	}
	if out.Kind != state.ReconcileNone {
		t.Fatalf("kind = %q, want none", out.Kind)
	}
}

// TestReconcileMaterialize_RestoresFromPriorWhenClaudePartial covers the
// classic crash-after-step-b case: .prior/ exists AND .claude/ holds half-
// renamed bytes. Spec invariant ("live target is the last successful state")
// requires restoring from .prior/.
func TestReconcileMaterialize_RestoresFromPriorWhenClaudePartial(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.PriorDir, 0o755); err != nil {
		t.Fatalf("mkdir prior: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PriorDir, "a"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "a"), []byte("PARTIAL"), 0o644); err != nil {
		t.Fatalf("write claude: %v", err)
	}

	out, err := state.ReconcileMaterialize(paths)
	if err != nil {
		t.Fatalf("ReconcileMaterialize: %v", err)
	}
	if out.Kind != state.ReconcileRestoredFromPrior {
		t.Fatalf("kind = %q, want restored-from-prior", out.Kind)
	}
	if exists, _ := state.PathExists(paths.PriorDir); exists {
		t.Fatalf("prior dir still exists after restore")
	}
	got, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "a"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if string(got) != "PRIOR" {
		t.Fatalf("restored contents = %q, want %q", got, "PRIOR")
	}
}

// TestReconcileMaterialize_RestoresFromPriorWhenClaudeMissing covers the
// case where step b moved live aside but step c never began (or its
// pending-bytes write failed). Restoring from .prior/ is correct.
func TestReconcileMaterialize_RestoresFromPriorWhenClaudeMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.PriorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PriorDir, "a"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := state.ReconcileMaterialize(paths)
	if err != nil {
		t.Fatalf("ReconcileMaterialize: %v", err)
	}
	if out.Kind != state.ReconcileRestoredFromPrior {
		t.Fatalf("kind = %q, want restored-from-prior", out.Kind)
	}
	if exists, _ := state.PathExists(paths.PriorDir); exists {
		t.Fatalf("prior dir still exists")
	}
	got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "a"))
	if string(got) != "PRIOR" {
		t.Fatalf("restored contents = %q, want PRIOR", got)
	}
}

// TestReconcileMaterialize_DiscardsPendingWhenOnlyPending covers step-a-only
// crash: live target untouched, drop .pending/.
func TestReconcileMaterialize_DiscardsPendingWhenOnlyPending(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.PendingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PendingDir, "a"), []byte("P"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "live"), []byte("LIVE"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := state.ReconcileMaterialize(paths)
	if err != nil {
		t.Fatalf("ReconcileMaterialize: %v", err)
	}
	if out.Kind != state.ReconcileDiscardedPending {
		t.Fatalf("kind = %q, want discarded-pending", out.Kind)
	}
	if exists, _ := state.PathExists(paths.PendingDir); exists {
		t.Fatalf("pending still exists after discard")
	}
	got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "live"))
	if string(got) != "LIVE" {
		t.Fatalf("live contents touched: %q", got)
	}
}

// TestReconcileMaterialize_RestoreClearsPending covers the case where .prior/
// AND .pending/ both exist (rare double-step-then-crash). Restoration takes
// priority; .pending/ is dropped after.
func TestReconcileMaterialize_RestoreClearsPending(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.PriorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PriorDir, "a"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(paths.PendingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PendingDir, "a"), []byte("PENDING"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := state.ReconcileMaterialize(paths)
	if err != nil {
		t.Fatalf("ReconcileMaterialize: %v", err)
	}
	if out.Kind != state.ReconcileRestoredFromPrior {
		t.Fatalf("kind = %q, want restored-from-prior", out.Kind)
	}
	if exists, _ := state.PathExists(paths.PendingDir); exists {
		t.Fatalf("pending still exists")
	}
	if exists, _ := state.PathExists(paths.PriorDir); exists {
		t.Fatalf("prior still exists")
	}
}

// TestReconcileMaterialize_NoScratchDebris asserts the rename-aside cleans up.
func TestReconcileMaterialize_NoScratchDebris(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.PriorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PriorDir, "a"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "a"), []byte("PARTIAL"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := state.ReconcileMaterialize(paths); err != nil {
		t.Fatalf("ReconcileMaterialize: %v", err)
	}
	dir := filepath.Dir(paths.ClaudeDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".claude.reconcile-") {
			t.Fatalf("scratch dir %q left behind", e.Name())
		}
	}
}

// TestReconcilePersist_MissingProfileDirIsNone documents the contract: a
// missing profile dir is not an error — the active profile may have just been
// removed; reconcile is a no-op.
func TestReconcilePersist_MissingProfileDirIsNone(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	out, err := state.ReconcilePersist(paths, "nope")
	if err != nil {
		t.Fatalf("ReconcilePersist: %v", err)
	}
	if out.Kind != state.ReconcileNone {
		t.Fatalf("kind = %q, want none", out.Kind)
	}
}

// TestReconcilePersist_RestoresPerProfilePrior covers the per-profile
// pending/prior protocol used by R22b's transactional pair.
func TestReconcilePersist_RestoresPerProfilePrior(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	profileDir := filepath.Join(paths.ProfilesDir, "myprofile")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	prior := filepath.Join(profileDir, ".prior")
	if err := os.MkdirAll(prior, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prior, "x"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := state.ReconcilePersist(paths, "myprofile")
	if err != nil {
		t.Fatalf("ReconcilePersist: %v", err)
	}
	if out.Kind != state.ReconcileRestoredFromPrior {
		t.Fatalf("kind = %q, want restored-from-prior", out.Kind)
	}
	got, err := os.ReadFile(filepath.Join(profileDir, ".claude", "x"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "PRIOR" {
		t.Fatalf("contents = %q, want PRIOR", got)
	}
}

// TestReconcilePersist_RejectsTraversalProfileName is defense-in-depth: even
// though resolver validation usually catches this upstream, reconcile must
// not be tricked into operating on a path outside the profilesDir.
func TestReconcilePersist_RejectsTraversalProfileName(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if _, err := state.ReconcilePersist(paths, "../escape"); err == nil {
		t.Fatalf("expected error for traversal name")
	}
}

// TestReconcileMaterialize_SweepsRootClaudeMdTmps covers R45 crash recovery:
// leftover CLAUDE.md.*.tmp files from a crashed splice are removed, while
// unrelated .tmp files are preserved.
func TestReconcileMaterialize_SweepsRootClaudeMdTmps(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	// Create one matching tmp and one unrelated .tmp.
	matching := state.RootClaudeMdTmpPath(paths)
	if err := os.WriteFile(matching, []byte("debris"), 0o644); err != nil {
		t.Fatalf("write matching: %v", err)
	}
	unrelated := filepath.Join(paths.ProjectRoot, "user.tmp")
	if err := os.WriteFile(unrelated, []byte("user-owned"), 0o644); err != nil {
		t.Fatalf("write unrelated: %v", err)
	}
	if _, err := state.ReconcileMaterialize(paths); err != nil {
		t.Fatalf("ReconcileMaterialize: %v", err)
	}
	if exists, _ := state.PathExists(matching); exists {
		t.Fatalf("matching tmp not swept")
	}
	if exists, _ := state.PathExists(unrelated); !exists {
		t.Fatalf("unrelated .tmp swept (regression — sweep too aggressive)")
	}
}

// TestReconcileMaterialize_ScratchRestoredWhenPriorRenameFails is the fitness
// function for the safety property in ReconcilePendingPrior: if the prior →
// target rename fails, the rolled-aside scratch dir holding the original live
// bytes MUST be renamed back to the canonical target. PR1 invariant is "live
// target is the last successful state" — leaving target absent and scratch
// stranded would violate it. Without this test, a refactor of the rename pair
// could silently regress the recovery branch.
//
// Asserts: (a) target ends up with original LIVE bytes (scratch was restored);
// (b) priorDir still exists (caller didn't get to discard it); (c) the
// underlying prior-restore error surfaces to the caller.
func TestReconcileMaterialize_ScratchRestoredWhenPriorRenameFails(t *testing.T) {
	root := t.TempDir()
	paths := state.BuildStatePaths(root)

	// Seed live target with a file we'll prove ends up back where it started.
	if err := os.MkdirAll(paths.ClaudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeDir, "live.md"), []byte("LIVE"), 0o644); err != nil {
		t.Fatalf("write live: %v", err)
	}

	// Seed prior so reconcile takes the restore-from-prior branch.
	if err := os.MkdirAll(paths.PriorDir, 0o755); err != nil {
		t.Fatalf("mkdir prior: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PriorDir, "prior.md"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	injected := errors.New("simulated prior→target rename failure")
	restore := state.SetTestRenamePriorToTarget(func(_, _ string) error {
		return injected
	})
	defer restore()

	_, err := state.ReconcileMaterialize(paths)
	if err == nil {
		t.Fatalf("expected error from injected rename failure, got nil")
	}
	if !errors.Is(err, injected) {
		t.Fatalf("expected wrapped %v, got %v", injected, err)
	}

	// (a) target was restored from scratch — live.md should be back.
	got, readErr := os.ReadFile(filepath.Join(paths.ClaudeDir, "live.md"))
	if readErr != nil {
		t.Fatalf("read live.md after recovery: %v (live tree was not restored)", readErr)
	}
	if string(got) != "LIVE" {
		t.Fatalf("live.md = %q, want LIVE (scratch restore failed)", got)
	}

	// (b) prior still exists (the failing rename didn't move it; cleanup didn't run).
	if exists, _ := state.PathExists(paths.PriorDir); !exists {
		t.Fatalf("priorDir gone after failed rename — should still be on disk")
	}

	// (c) no scratch debris should remain alongside target after the
	// recovery rename completed.
	dir := filepath.Dir(paths.ClaudeDir)
	entries, readDirErr := os.ReadDir(dir)
	if readDirErr != nil {
		t.Fatalf("readdir: %v", readDirErr)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".claude.reconcile-") {
			t.Fatalf("scratch %q left behind after successful scratch-restore", e.Name())
		}
	}
}
