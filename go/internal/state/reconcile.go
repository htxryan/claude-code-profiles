package state

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// ReconcileOutcomeKind enumerates the result of a single pending/prior
// reconciliation step. CLI dispatch (D7) surfaces "(reconciled crashed
// materialization: restored .claude/ from .prior/)" notices from these.
type ReconcileOutcomeKind string

const (
	ReconcileNone               ReconcileOutcomeKind = "none"
	ReconcileRestoredFromPrior  ReconcileOutcomeKind = "restored-from-prior"
	ReconcileDiscardedPending   ReconcileOutcomeKind = "discarded-pending"
)

// ReconcileOutcome carries the per-target result of reconcilation. TargetLabel
// is a human-friendly description used in messages.
type ReconcileOutcome struct {
	Kind        ReconcileOutcomeKind
	TargetLabel string
}

// reconcileCounter monotonically tags scratch dirs so concurrent reconciles
// (defense-in-depth — only one should hold the lock at a time) never collide.
var reconcileCounter atomic.Uint64

// ReconcilePendingPrior performs the generic three-way pending/prior recovery.
//
// Recovery rules:
//   - If .prior/ exists: a prior materialization crashed AFTER step b but
//     potentially BEFORE step c committed. Spec invariant ("live target is
//     the last successful state") says restore from .prior/ in BOTH cases:
//     (i)  step c never ran or partially landed bytes — target is missing
//          or holds half-renamed bytes;
//     (ii) step c finished but .prior/ cleanup didn't run before crash —
//          target is the freshly committed content.
//     Re-materialize after restore is idempotent so case (ii) is safe.
//   - If only .pending/ exists: step a partially succeeded (or step b never
//     ran). Live target is intact — drop .pending/.
//   - If neither exists: no-op.
//
// Window-narrowing: rename target to a per-attempt scratch dir BEFORE
// renaming prior back. Concurrent readers (R43) see EITHER the original
// target OR the restored one, never an empty/missing tree, except for the
// brief rename window. If the rename-aside fails (e.g. FS doesn't support
// move-into-existing-dir, or target is held), fall back to RmRf — wider
// window but recovery still completes.
//
// Order matters: live bytes get moved to scratch, prior is renamed back to
// target, THEN scratch is discarded. Cleaning up scratch BEFORE the prior
// restore would permanently lose the original live bytes if the restore
// fails.
func ReconcilePendingPrior(target, pendingDir, priorDir, targetLabel string) (ReconcileOutcome, error) {
	priorExists, err := PathExists(priorDir)
	if err != nil {
		return ReconcileOutcome{}, err
	}
	pendingExists, err := PathExists(pendingDir)
	if err != nil {
		return ReconcileOutcome{}, err
	}

	if priorExists {
		var scratch string
		targetExists, err := PathExists(target)
		if err != nil {
			return ReconcileOutcome{}, err
		}
		if targetExists {
			scratch = uniqueScratchPath(target)
			if renameErr := AtomicRename(target, scratch); renameErr != nil {
				// Rename-aside failed — fall back to RmRf. Wider window but
				// recovery completes. No scratch to retain.
				if rmErr := RmRf(target); rmErr != nil {
					return ReconcileOutcome{}, fmt.Errorf("reconcile rmrf target after rename-aside failure: %w", rmErr)
				}
				scratch = ""
			}
		}
		if err := AtomicRename(priorDir, target); err != nil {
			return ReconcileOutcome{}, fmt.Errorf("reconcile rename prior back to target: %w", err)
		}
		if scratch != "" {
			// Discard the held-aside live bytes. If cleanup fails, the user
			// sees a `.claude.reconcile-*` dir but the live tree is correct.
			_ = RmRf(scratch)
		}
		if pendingExists {
			if err := RmRf(pendingDir); err != nil {
				return ReconcileOutcome{}, fmt.Errorf("reconcile drop pending after restore: %w", err)
			}
		}
		return ReconcileOutcome{Kind: ReconcileRestoredFromPrior, TargetLabel: targetLabel}, nil
	}

	if pendingExists {
		if err := RmRf(pendingDir); err != nil {
			return ReconcileOutcome{}, fmt.Errorf("reconcile drop pending: %w", err)
		}
		return ReconcileOutcome{Kind: ReconcileDiscardedPending, TargetLabel: targetLabel}, nil
	}

	return ReconcileOutcome{Kind: ReconcileNone, TargetLabel: targetLabel}, nil
}

// uniqueScratchPath returns a peer-of-target path safe for the rename-aside.
// PID + atomic counter + random suffix prevents collisions even under
// crash-spurious leftover scratch dirs from previous runs.
func uniqueScratchPath(target string) string {
	counter := reconcileCounter.Add(1) - 1
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails on supported platforms; if it does, fall
		// back to a fixed suffix — the counter alone gives us uniqueness in
		// a single process.
		b = [3]byte{0, 0, 0}
	}
	return fmt.Sprintf("%s.reconcile-%d-%d-%s", target, os.Getpid(), counter, hex.EncodeToString(b[:]))
}

// ReconcileMaterialize reconciles the materialize target (root .claude/).
// Call at the start of any mutating op (use, sync, persist) AFTER the lock is
// acquired (PR23). Cheap on the steady-state path (two stat calls, both
// ENOENT).
//
// Also sweeps any leftover <projectRoot>/CLAUDE.md.*.tmp files from a crashed
// section-splice write (R45 atomic rollback). Best-effort: if unlink fails
// we swallow because the live root CLAUDE.md is fine; only debris remains.
func ReconcileMaterialize(paths StatePaths) (ReconcileOutcome, error) {
	if err := os.MkdirAll(paths.MetaDir, 0o755); err != nil {
		return ReconcileOutcome{}, err
	}
	sweepRootClaudeMdTmps(paths)
	return ReconcilePendingPrior(
		paths.ClaudeDir,
		paths.PendingDir,
		paths.PriorDir,
		paths.ClaudeDir,
	)
}

// sweepRootClaudeMdTmps best-effort cleans up leftover section-splice tmp
// files for project-root CLAUDE.md (R45 crash recovery). Pattern matches the
// shape of RootClaudeMdTmpPath via IsRootClaudeMdTmpName so unrelated user
// .tmp files are never swept.
//
// Errors swallowed: live CLAUDE.md is untouched; the worst case of a failed
// sweep is stale debris on disk, not a blocker. The reconcile contract is
// "fix what you can; don't refuse to start the CLI because of debris from an
// unrelated process".
func sweepRootClaudeMdTmps(paths StatePaths) {
	entries, err := os.ReadDir(paths.ProjectRoot)
	if err != nil {
		// If projectRoot is unreadable, the rest of the CLI fails loudly —
		// no point surfacing the failure here.
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !IsRootClaudeMdTmpName(e.Name()) {
			continue
		}
		_ = os.Remove(filepath.Join(paths.ProjectRoot, e.Name()))
	}
}

// ReconcilePersist reconciles a per-profile persist target. Used during the
// persist transactional pair recovery. profileName selects which profile dir
// to inspect; an invalid name returns the validation error so a malicious or
// corrupt active-profile pointer can't escape the profilesDir.
//
// Returns ReconcileNone with no error when the profile dir doesn't exist —
// nothing to reconcile.
func ReconcilePersist(paths StatePaths, profileName string) (ReconcileOutcome, error) {
	persist, err := BuildPersistPaths(paths, profileName)
	if err != nil {
		return ReconcileOutcome{}, err
	}
	exists, err := PathExists(persist.ProfileDir)
	if err != nil {
		return ReconcileOutcome{}, err
	}
	if !exists {
		return ReconcileOutcome{Kind: ReconcileNone, TargetLabel: persist.TargetClaudeDir}, nil
	}
	return ReconcilePendingPrior(
		persist.TargetClaudeDir,
		persist.PendingDir,
		persist.PriorDir,
		persist.TargetClaudeDir,
	)
}

// IsSchemaTooNewError reports whether err is (or wraps) a *SchemaTooNewError.
// Provided for callers that want a one-call test without spelling errors.As.
func IsSchemaTooNewError(err error) bool {
	var target *SchemaTooNewError
	return errors.As(err, &target)
}
