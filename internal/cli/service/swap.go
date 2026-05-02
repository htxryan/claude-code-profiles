// Package service hosts the swap orchestrator — the canonical use/sync flow:
//
//	Resolve → Merge → DetectDrift → DecideGate → [Lock] DetectDrift again
//	         → ApplyGate → state-write
//
// Lock discipline (per drift package + state.WithLock contract):
//   - DetectDrift runs ONCE outside the lock to drive the prompt (read-only,
//     R43)
//   - The lock brackets a SECOND DetectDrift (ground-truth) + ApplyGate +
//     state-write so all mutations are atomic-across-destinations
//
// Non-TTY invariant: when GateMode == NonInteractive AND drift exists AND no
// --on-drift= flag, DecideGate auto-aborts → the orchestrator returns a
// CliUserError so D7 surfaces it as exit 1.
//
// Mirrors src/cli/service/swap.ts.
package service

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/htxryan/c3p/internal/drift"
	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/merge"
	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

// SwapOptions bundles the orchestrator's inputs.
type SwapOptions struct {
	Paths state.StatePaths

	// TargetProfile is the profile being switched TO (use) or re-materialized
	// (sync — caller resolves the active profile name and supplies it here).
	TargetProfile string

	// Mode controls whether DecideGate may emit Prompt outcomes.
	Mode drift.GateMode

	// OnDriftFlag is the parsed --on-drift= value; empty means unset.
	OnDriftFlag drift.GateChoice

	// SignalHandlers wires SIGINT/SIGTERM lock-release. Tests pass false.
	SignalHandlers bool

	// IsSync distinguishes use-vs-sync for user messaging.
	IsSync bool

	// WaitMs requests the lock acquire to poll. Zero = fail-fast.
	WaitMs int64

	// PromptIn / PromptOut are the streams the interactive prompt writes
	// to / reads from. nil defaults to os.Stdin / os.Stderr.
	PromptIn  io.Reader
	PromptOut io.Writer

	// PromptFunc lets D7 inject a deterministic prompter for tests. When
	// non-nil it overrides the os.Stdin-backed default.
	PromptFunc func() drift.GateChoice

	// OnPhase is fired with progress hints ("resolving plan", "applying
	// changes", etc.). nil swallows them.
	OnPhase func(text string)
}

// SwapResult is the orchestrator's structured outcome.
type SwapResult struct {
	Action            drift.ApplyGateAction
	BackupSnapshot    *string
	MaterializeResult *state.MaterializeResult
	// Drift is the report observed under the lock (the ground-truth one,
	// not the outside-lock pre-prompt detect). When the gate aborted before
	// re-detection it's the original report instead.
	Drift drift.DriftReport
	// Plan is the resolved plan we materialized (or attempted to). Nil when
	// resolution itself failed.
	Plan *resolver.ResolvedPlan
	// ResolverWarnings is the set of warnings emitted during Resolve, surfaced
	// up to D7 for human/JSON rendering.
	ResolverWarnings []resolver.ResolutionWarning
}

// RunSwap executes the orchestration. The lock IS acquired inside this
// function — callers (use, sync command handlers) do NOT need to wrap.
//
// Errors returned are routed by D7's ExitCodeFor:
//   - *pipelineerrors.MissingProfileError (CLI typo / structural) → 1 or 3
//   - *state.LockHeldError                                        → 3
//   - *cli.CliUserError                                           → 1 (or 3)
//   - other PipelineError                                         → per code
func RunSwap(opts SwapOptions) (SwapResult, error) {
	phase := func(s string) {
		if opts.OnPhase != nil {
			opts.OnPhase(s)
		}
	}

	// 1. Resolve.
	phase("resolving plan...")
	plan, err := resolver.Resolve(opts.TargetProfile, resolver.ResolveOptions{ProjectRoot: opts.Paths.ProjectRoot})
	if err != nil {
		return SwapResult{}, err
	}
	// 2. Merge — supply a disk-backed Read.
	phase("merging contributors...")
	merged, err := merge.Merge(plan, merge.Options{
		Read: func(absPath string) ([]byte, error) {
			return os.ReadFile(absPath)
		},
	})
	if err != nil {
		return SwapResult{}, err
	}

	// 3. Read pre-swap state for the active profile name (used by
	//    persist gate path).
	preState, err := state.ReadStateFile(opts.Paths)
	if err != nil {
		return SwapResult{}, err
	}
	activeProfileName := ""
	if preState.State.ActiveProfile != nil {
		activeProfileName = *preState.State.ActiveProfile
	}

	// 4. DetectDrift outside the lock — drives the prompt without holding
	//    a writer slot.
	phase("checking drift...")
	report, err := drift.DetectDrift(opts.Paths)
	if err != nil {
		return SwapResult{}, err
	}

	// 5. DecideGate.
	outcome := drift.DecideGate(drift.GateInput{
		Report:      report,
		Mode:        opts.Mode,
		OnDriftFlag: opts.OnDriftFlag,
	})

	// 5a. Non-TTY without --on-drift on a drifted tree: DecideGate
	//     returned auto+abort. Surface as exit 1 with a clear message.
	if outcome.Kind == drift.GateOutcomeAuto && outcome.Choice == drift.GateChoiceAbort &&
		opts.Mode == drift.GateModeNonInteractive && opts.OnDriftFlag == "" &&
		report.FingerprintOk && len(report.Entries) > 0 {
		return SwapResult{
				Action: drift.ApplyActionAborted,
				Drift:  report,
				Plan:   plan,
			},
			newSwapAbortError("drift detected and no --on-drift= passed in non-interactive mode (R21 hard-block); pass --on-drift=discard|persist|abort or run interactively")
	}

	// 5b. Resolved choice (either auto or via prompt).
	choice := outcome.Choice
	if outcome.Kind == drift.GateOutcomePrompt {
		if opts.PromptFunc != nil {
			choice = opts.PromptFunc()
		} else {
			// Lazy import: prompt.go lives under internal/cli, but service is
			// a sub-package and may not import its parent. The CLI handler
			// passes a PromptFunc closure that calls cli.PromptGateChoice; if
			// the closure is nil here, fall back to abort (defensive).
			choice = drift.GateChoiceAbort
		}
	}

	if choice == drift.GateChoiceAbort {
		return SwapResult{
				Action: drift.ApplyActionAborted,
				Drift:  report,
				Plan:   plan,
			},
			newSwapAbortError("drift abort: the live .claude/ tree was left untouched")
	}

	// 6. Acquire the lock and re-detect inside it for ground-truth.
	acquireOpts := state.AcquireOptions{SignalHandlers: opts.SignalHandlers}
	if opts.WaitMs > 0 {
		acquireOpts.Wait = &state.WaitOptions{
			TotalMs: opts.WaitMs,
		}
	}

	var result SwapResult
	result.Plan = plan
	result.ResolverWarnings = plan.Warnings
	err = state.WithLock(context.Background(), opts.Paths, acquireOpts, func(_ *state.LockHandle) error {
		phase("re-checking drift under lock...")
		ground, dErr := drift.DetectDrift(opts.Paths)
		if dErr != nil {
			return dErr
		}
		result.Drift = ground

		// Re-decide based on ground-truth. The OnDriftFlag still applies.
		groundOutcome := drift.DecideGate(drift.GateInput{
			Report:      ground,
			Mode:        opts.Mode,
			OnDriftFlag: opts.OnDriftFlag,
		})
		groundChoice := groundOutcome.Choice
		// PR24 (drift snapshot immutability): the user's outside-lock
		// answer must survive a ground re-decide.
		//   - outside.Kind == Prompt → user was actually shown a prompt
		//     and picked discard/persist/abort. Honour that even if ground
		//     now says no-drift (a concurrent sync cleaned the tree while
		//     we were prompting). The user-authorised action wins.
		//   - outside.Kind == NoDrift but ground.Kind == Prompt → drift
		//     appeared in the gap between detect-outside and lock acquire.
		//     The user was NOT prompted (we never showed them this drift),
		//     so applying the no-drift "proceed" path would silently
		//     destroy the new drift without a backup. Auto-abort instead.
		if outcome.Kind == drift.GateOutcomePrompt {
			groundChoice = choice
		} else if outcome.Kind == drift.GateOutcomeNoDrift && groundOutcome.Kind == drift.GateOutcomePrompt {
			result.Action = drift.ApplyActionAborted
			return newSwapAbortError("drift appeared between pre-lock check and lock acquire; aborting to preserve uncommitted edits — re-run the swap (you'll see the drift gate)")
		}
		if groundChoice == drift.GateChoiceAbort {
			result.Action = drift.ApplyActionAborted
			return newSwapAbortError("drift abort: the live .claude/ tree was left untouched")
		}

		applyResult, aErr := drift.ApplyGate(groundChoice, drift.ApplyGateOptions{
			Paths:             opts.Paths,
			Plan:              *plan,
			Merged:            merged,
			ActiveProfileName: activeProfileName,
		})
		if aErr != nil {
			result.Action = applyResult.Action
			result.BackupSnapshot = applyResult.BackupSnapshot
			result.MaterializeResult = applyResult.MaterializeResult
			return aErr
		}
		result.Action = applyResult.Action
		result.BackupSnapshot = applyResult.BackupSnapshot
		result.MaterializeResult = applyResult.MaterializeResult
		return nil
	})
	if err != nil {
		return result, err
	}
	return result, nil
}

// SwapAbortError is the marker returned when the gate aborts. The CLI maps
// it to exit code 1 (user-error class) via ExitCodeFor; tests
// can errors.As to identify "the swap chose abort" specifically.
type SwapAbortError struct{ Message string }

func (e *SwapAbortError) Error() string { return e.Message }

func newSwapAbortError(msg string) error { return &SwapAbortError{Message: msg} }

// IsSwapAbort reports whether err (or any wrapped) is a *SwapAbortError.
func IsSwapAbort(err error) bool {
	var s *SwapAbortError
	return errors.As(err, &s)
}

// EnsureNotMissingProfile is a small helper: if err is a top-level CLI typo
// (MissingProfileError with empty referencedBy), the caller's outer layer
// can use this to know whether to attach typo suggestions.
func EnsureNotMissingProfile(err error) (*pipelineerrors.MissingProfileError, bool) {
	var mpe *pipelineerrors.MissingProfileError
	if errors.As(err, &mpe) && mpe.ReferencedBy == "" {
		return mpe, true
	}
	return nil, false
}

// FormatActiveProfileBeforeSwap returns the active profile name from
// state.json or "" when no profile is active. Side-effect-free (read-only).
func FormatActiveProfileBeforeSwap(paths state.StatePaths) string {
	res, err := state.ReadStateFile(paths)
	if err != nil {
		return ""
	}
	if res.State.ActiveProfile == nil {
		return ""
	}
	return *res.State.ActiveProfile
}

// MergedFiles is exported for tests; callers normally don't need it.
type MergedFiles = []merge.MergedFile

// ResolvedPlan is exported for tests.
type ResolvedPlan = resolver.ResolvedPlan

