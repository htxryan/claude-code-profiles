package state_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/htxryan/claude-code-config-profiles/internal/state"
)

// TestReconcileUnderLock_OnlyOneObservesPriorBytes is the PR23 fitness
// function: reconciliation runs ONLY after the lock is acquired (it must be
// the first action after lock-acquire on mutating verbs). Two concurrent
// "swap" simulations against a seeded .prior/ should not both succeed in
// reading or moving prior bytes — exactly one transaction sees prior, the
// other arrives after the lock is released and observes the restored state.
//
// We model the swap as: WithLock → Reconcile → assert a sentinel value moved.
// The lock serialises the ordering; if reconcile ran outside the lock, both
// goroutines could race to atomic-rename the same .prior/ and surface
// duplicate restore notices.
func TestReconcileUnderLock_OnlyOneObservesPriorBytes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)

	// Seed a .prior/ that exactly one acquirer should observe and restore.
	if err := os.MkdirAll(paths.PriorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PriorDir, "sentinel"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var restoredCount atomic.Int32
	var noneCount atomic.Int32
	var wg sync.WaitGroup
	const N = 4
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			// Each goroutine acquires the lock with a generous wait so the
			// late arrivers don't fail; PR23 says reconcile is the FIRST
			// action after lock-acquire, so we run it inside the closure.
			err := state.WithLock(context.Background(), paths, state.AcquireOptions{
				Wait: &state.WaitOptions{TotalMs: 5_000},
			}, func(_ *state.LockHandle) error {
				out, err := state.ReconcileMaterialize(paths)
				if err != nil {
					return err
				}
				switch out.Kind {
				case state.ReconcileRestoredFromPrior:
					restoredCount.Add(1)
				case state.ReconcileNone:
					noneCount.Add(1)
				}
				// Sleep briefly so the windows actually overlap; without this
				// the loop can serialise so quickly that PR23 violations
				// wouldn't have a chance to surface.
				time.Sleep(10 * time.Millisecond)
				return nil
			})
			if err != nil {
				t.Errorf("WithLock: %v", err)
			}
		}()
	}
	wg.Wait()

	if r := restoredCount.Load(); r != 1 {
		t.Fatalf("restored count = %d, want exactly 1 (PR23 violation: more than one acquirer observed .prior/)", r)
	}
	if n := noneCount.Load(); n != N-1 {
		t.Fatalf("none count = %d, want %d", n, N-1)
	}
	got, err := os.ReadFile(filepath.Join(paths.ClaudeDir, "sentinel"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "PRIOR" {
		t.Fatalf("restored sentinel = %q, want PRIOR", got)
	}
}

// TestReconcileUnderLock_FirstActionGuard documents the PR23 contract — the
// caller MUST run reconcile inside WithLock as the first action. We can't
// enforce ordering at the language level (Go has no language-level pre-
// condition), but we capture the contract here and assert the public
// surface (PersistAndMaterialize / Materialize internally call reconcile
// before any side-effects) honours it.
func TestReconcileUnderLock_FirstActionGuard(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := state.BuildStatePaths(root)
	if err := os.MkdirAll(paths.PriorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.PriorDir, "x"), []byte("PRIOR"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	plan := makePlan("dev")
	merged := crashInjectionMerged("LEAF-V1")
	err := state.WithLock(context.Background(), paths, state.AcquireOptions{}, func(_ *state.LockHandle) error {
		_, err := state.Materialize(paths, plan, merged, state.MaterializeOptions{}, nil)
		return err
	})
	if err != nil {
		t.Fatalf("WithLock+Materialize: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(paths.ClaudeDir, "CLAUDE.md"))
	if string(got) != "LEAF-V1" {
		t.Fatalf("final CLAUDE.md = %q, want LEAF-V1 (reconcile-then-materialize order broken)", got)
	}
}
