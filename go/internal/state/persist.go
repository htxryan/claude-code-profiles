package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/htxryan/c3p/internal/markers"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
)

// PersistOptions configure PersistAndMaterialize.
type PersistOptions struct {
	// ActiveProfileName owns the live .claude/ (we copy live into THIS
	// profile). Required.
	ActiveProfileName string
	// NewPlan is the target of the swap (the plan whose merged bytes we
	// materialize after the persist completes). Required.
	NewPlan resolver.ResolvedPlan
	// NewMerged is the merged files for the new plan. Required.
	NewMerged []merge.MergedFile
}

// PersistAndMaterialize is the "drift → persist" gate flow (R22 / R22a /
// R22b): persist live .claude/ into <activeProfileName>/.claude/, then
// materialize the new plan as live .claude/.
//
// Lock precondition: caller MUST hold the project lock (D7 swap orchestration
// wraps this in WithLock). The function does not acquire its own lock so
// the persist + materialize pair is bracketed by a single lock acquisition,
// matching the spec's "lock brackets the rename pair AND the state-write"
// invariant.
//
// Reconciliation order: materialize-side first, then persist-side. The
// reverse risks persisting a partially-reconciled .claude/ (a prior-restored
// state) into the profile.
//
// Resolution ordering — snapshot semantics: we do NOT re-resolve after the
// persist write-back lands. So even if the new profile extends the active
// one (e.g. `use prod --on-drift=persist` where prod extends dev), the
// materialized prod is built from the pre-persist source state of dev/ and
// will NOT inherit the just-persisted edits via the extends chain. To pick
// up persisted edits in an extending profile, the user re-runs `use <child>`
// after persist completes.
func PersistAndMaterialize(paths StatePaths, opts PersistOptions) (MaterializeResult, error) {
	if _, err := ReconcileMaterialize(paths); err != nil {
		return MaterializeResult{}, err
	}
	if _, err := ReconcilePersist(paths, opts.ActiveProfileName); err != nil {
		return MaterializeResult{}, err
	}

	if err := PersistLiveIntoProfile(paths, opts.ActiveProfileName); err != nil {
		return MaterializeResult{}, err
	}

	return Materialize(paths, opts.NewPlan, opts.NewMerged, MaterializeOptions{}, "")
}

// PersistLiveIntoProfile is the persist half of the pair, exposed for tests
// and for callers (D6) that want to persist without immediately swapping.
// The flow is identical to materialize's own pending/prior, scoped to the
// active profile's directory:
//   (a) write live .claude/ contents into <active>/.pending/
//   (b) atomically rename <active>/.claude/ to <active>/.prior/ (if exists)
//   (c) atomically rename <active>/.pending/ to <active>/.claude/
//
// Plus (cw6/T5 R46/AC-8): when the live project-root CLAUDE.md carries
// well-formed markers we ALSO write the section bytes to <active>/CLAUDE.md
// (peer of profile.json). This lets the next `use` cycle re-materialize the
// user's edits via the normal merge/splice pipeline, completing the round
// trip. The destination file holds JUST the section body — no markers,
// because markers only exist in the materialized live file (added by
// RenderManagedBlock at materialize time, not stored in sources).
func PersistLiveIntoProfile(paths StatePaths, activeProfileName string) error {
	persist, err := BuildPersistPaths(paths, activeProfileName)
	if err != nil {
		return err
	}

	// Defensive create: profile dir is expected to exist (the active profile
	// got us here), but if it doesn't, the persist still works.
	if err := os.MkdirAll(persist.ProfileDir, 0o755); err != nil {
		return err
	}

	// Step a: stage in pending. R22 says copy the entire live .claude/ tree
	// (including added/deleted files relative to resolved sources) into the
	// active profile. CopyTree is missing-source-tolerant via the explicit
	// existence check — copying an empty tree is correct here when the user
	// deleted .claude/ entirely.
	if err := RmRf(persist.PendingDir); err != nil {
		return err
	}
	liveExists, err := PathExists(paths.ClaudeDir)
	if err != nil {
		return err
	}
	if liveExists {
		if err := CopyTree(paths.ClaudeDir, persist.PendingDir); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(persist.PendingDir, 0o755); err != nil {
			return err
		}
	}

	// Step b: rename existing target to prior, if it exists.
	targetExists, err := PathExists(persist.TargetClaudeDir)
	if err != nil {
		return err
	}
	if targetExists {
		if err := RmRf(persist.PriorDir); err != nil {
			return err
		}
		if err := AtomicRename(persist.TargetClaudeDir, persist.PriorDir); err != nil {
			return err
		}
	}

	// Step c: rename pending → target. On failure, restore prior. Surface
	// restore failures to stderr (the user needs to know if both the swap
	// AND the rollback failed; original step-c error remains primary).
	if err := AtomicRename(persist.PendingDir, persist.TargetClaudeDir); err != nil {
		exists, _ := PathExists(persist.PriorDir)
		if exists {
			if restoreErr := AtomicRename(persist.PriorDir, persist.TargetClaudeDir); restoreErr != nil {
				writePersistRestoreStderr(persist.TargetClaudeDir, restoreErr)
			}
		}
		_ = RmRf(persist.PendingDir)
		return err
	}

	// Success — drop prior. (No state-file update here; the caller's
	// subsequent materialize writes the new state.)
	priorExists, err := PathExists(persist.PriorDir)
	if err != nil {
		return err
	}
	if priorExists {
		if err := RmRf(persist.PriorDir); err != nil {
			return err
		}
	}

	// cw6/T5 (R46/AC-8): persist the live project-root CLAUDE.md section back
	// to <profile>/CLAUDE.md. MUST come BEFORE the profileDir mtime touch so
	// the touch reflects the complete persist.
	if err := persistRootClaudeMdSection(paths, persist.ProfileDir); err != nil {
		return err
	}

	// Touch profileDir so e.g. `list` shows a recent mtime; not strictly
	// required but matches user expectation that "persist" updates the
	// profile's last-modified. Touch the parent (which `list` enumerates),
	// not the inner .claude/.
	now := time.Now()
	_ = os.Chtimes(persist.ProfileDir, now, now)
	return nil
}

// writePersistRestoreStderr is replaceable in tests so persistence-restore
// failure messages can be captured without polluting CI stderr.
var writePersistRestoreStderr = func(targetClaudeDir string, err error) {
	fmt.Fprintf(stderrSink(), "c3p: persist restore failed for %s: %v\n", targetClaudeDir, err)
}

// persistRootClaudeMdSection persists the live project-root CLAUDE.md section
// back to the profile's peer <profileDir>/CLAUDE.md (R46/AC-8). Skipped
// silently when:
//   - the live project-root CLAUDE.md doesn't exist (no R10 contributor ever
//     ran; nothing to persist)
//   - the live file's markers are missing/malformed (drift detection
//     surfaces this as `unrecoverable`; persist would have nothing meaningful
//     to write because we can't locate the section bytes)
//
// The destination file holds JUST the section body — no markers, no
// surrounding user-owned bytes. Markers only exist in the materialized live
// file; sources hold the body to be merged. This matches the cw6/T2 resolver
// contract: a contributor's .claude-profiles/<P>/CLAUDE.md is the body bytes
// to splice on next `use`.
//
// Why we explicitly do NOT touch
// .claude-profiles/<active>/.claude/CLAUDE.md (AC-8b regression guard): pre-
// cw6 profiles may have a stale .claude/CLAUDE.md the user never migrated.
// The cw6 contract is "the project-root section lives in <P>/CLAUDE.md, the
// .claude/ tree lives under <P>/.claude/" — touching the legacy location
// would silently shadow the new one and break drift detection's destination
// disambiguation.
func persistRootClaudeMdSection(paths StatePaths, profileDir string) error {
	contentBytes, err := os.ReadFile(paths.RootClaudeMdFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	parsed := markers.ParseMarkers(string(contentBytes))
	if parsed.Status != markers.StatusValid {
		return nil
	}
	body := markers.ExtractSectionBody(parsed.Section)

	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(profileDir, "CLAUDE.md")
	tmpPath := UniqueAtomicTmpPath(profileDir, dest)
	if err := AtomicWriteFile(dest, tmpPath, []byte(body)); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
