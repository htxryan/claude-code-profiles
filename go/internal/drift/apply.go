package drift

import (
	"errors"

	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

// ApplyGateAction is the orchestrator-visible outcome of ApplyGate.
type ApplyGateAction string

const (
	ApplyActionMaterialized            ApplyGateAction = "materialized"
	ApplyActionPersistedAndMaterialized ApplyGateAction = "persisted-and-materialized"
	ApplyActionAborted                 ApplyGateAction = "aborted"
)

// ApplyGateOptions bundles inputs to ApplyGate. ActiveProfileName is the
// active profile name from .state.json BEFORE the swap; required when
// Choice == persist (that's the profile the live .claude/ is persisted
// into). Empty is acceptable for discard / no-drift / abort.
type ApplyGateOptions struct {
	Paths             state.StatePaths
	Plan              resolver.ResolvedPlan
	Merged            []merge.MergedFile
	ActiveProfileName string
}

// ApplyGateResult is the structured outcome so the orchestrator can build
// user-facing messages without re-deriving state.
//
// BackupSnapshot is *string (TS parity with `string | null`): nil signals
// "no snapshot taken" (abort, no-drift, persist, or discard with no live
// .claude/ to back up). Non-nil holds the absolute path of the snapshot dir.
// D7's JSON output emits `null` vs `"<path>"` to surface the distinction.
type ApplyGateResult struct {
	Action            ApplyGateAction
	BackupSnapshot    *string
	MaterializeResult *state.MaterializeResult
}

// ApplyGate dispatches the user's (or auto-resolved) gate choice:
//   - no-drift-proceed → Materialize directly (no backup)
//   - discard          → SnapshotForDiscard + Materialize (R23a backup)
//   - persist          → PersistAndMaterialize (R22b transactional pair)
//   - abort            → no-op (R24)
//
// Lock precondition: caller (D7 swap orchestration) MUST hold the project
// lock around this call. We don't acquire it here so the entire swap
// sequence — drift detect + gate decide + gate apply + state-write — is
// bracketed by a single WithLock. Calling outside a lock is a programmer
// error and breaks the rename-pair atomicity invariant.
//
// PR24 (drift snapshot immutability): the caller MUST NOT re-detect drift
// between display-time and apply-time. The orchestrator runs detect once
// inside the lock and applies the resulting choice; if the user picked from
// a stale outside-lock report, it remains the same DriftReport instance
// passed to display (the gate decision is what the user authorized).
func ApplyGate(choice GateChoice, opts ApplyGateOptions) (ApplyGateResult, error) {
	switch choice {
	case GateChoiceAbort:
		return ApplyGateResult{
			Action:            ApplyActionAborted,
			BackupSnapshot:    nil,
			MaterializeResult: nil,
		}, nil

	case GateChoiceNoDriftProceed:
		r, err := state.Materialize(opts.Paths, opts.Plan, opts.Merged, state.MaterializeOptions{}, nil)
		if err != nil {
			return ApplyGateResult{}, err
		}
		return ApplyGateResult{
			Action:            ApplyActionMaterialized,
			BackupSnapshot:    nil,
			MaterializeResult: &r,
		}, nil

	case GateChoiceDiscard:
		// Snapshot BEFORE the rename so the backup captures pre-swap content
		// (R23a). Materialize then performs the pending/prior rename — by
		// the time .claude/ is overwritten, the snapshot is already on disk.
		// SnapshotForDiscard returns nil when .claude/ doesn't exist (NoActive
		// or active-with-deleted-tree drift): there's nothing to back up, and
		// nil is propagated to the caller as "no snapshot taken" (TS parity).
		backup, err := state.SnapshotForDiscard(opts.Paths)
		if err != nil {
			return ApplyGateResult{}, err
		}
		r, err := state.Materialize(opts.Paths, opts.Plan, opts.Merged, state.MaterializeOptions{}, backup)
		if err != nil {
			// PR25: surface the backup path even when Materialize fails after
			// snapshot. The user's edits are on disk at `*backup`; D7 needs the
			// path to render "your edits were saved at <path>" alongside the
			// failure. Returning a zero-value result here would orphan the
			// snapshot dir without any caller able to find it. When backup is
			// nil (nothing to back up), pass nil through unchanged.
			return ApplyGateResult{
				Action:         ApplyActionAborted,
				BackupSnapshot: backup,
			}, err
		}
		return ApplyGateResult{
			Action:            ApplyActionMaterialized,
			BackupSnapshot:    backup,
			MaterializeResult: &r,
		}, nil

	case GateChoicePersist:
		if opts.ActiveProfileName == "" {
			// Defense-in-depth: DecideGate returns no-drift-proceed when
			// FingerprintOk is false (which implies activeProfile is null).
			// If we land here, a caller bypassed the decider with a hand-
			// crafted choice. Refuse rather than write into a profile-less
			// directory.
			return ApplyGateResult{}, errors.New("persist gate choice requires an active profile in .state.json — none recorded")
		}
		r, err := state.PersistAndMaterialize(opts.Paths, state.PersistOptions{
			ActiveProfileName: opts.ActiveProfileName,
			NewPlan:           opts.Plan,
			NewMerged:         opts.Merged,
		})
		if err != nil {
			return ApplyGateResult{}, err
		}
		return ApplyGateResult{
			Action:            ApplyActionPersistedAndMaterialized,
			BackupSnapshot:    nil,
			MaterializeResult: &r,
		}, nil

	default:
		return ApplyGateResult{}, errors.New("unreachable: unknown gate choice " + string(choice))
	}
}
