package drift

import "fmt"

// DecideGate is a pure decision function: given a DriftReport, the session's
// interactivity, and an optional --on-drift= flag, decide whether to:
//   - proceed without gating (no-drift)
//   - resolve directly via the flag or auto-abort (auto)
//   - require an interactive prompt (prompt) — caller (D7) drives the
//     prompt and feeds the result back into ApplyGate.
//
// Decision table (priority order):
//  1. report.FingerprintOk == false                → no-drift
//  2. len(report.Entries) == 0                     → no-drift
//  3. OnDriftFlag set                              → auto (use flag)
//  4. mode == non-interactive (no flag)            → auto (abort)
//  5. mode == interactive (drift, no flag)         → prompt
//
// The flag wins over interactive mode (rule 3 before rule 5): a user who
// passed --on-drift=discard doesn't want to be re-prompted "are you sure".
//
// We deliberately do NOT short-circuit on "all entries are unrecoverable":
// the gate doesn't know the target plan, so it can't tell whether the new
// plan even contributes a project-root section. When the new plan has no
// projectRoot file, Materialize treats missing/malformed root markers as a
// documented opt-out (preflightEmptyRootSplice returns nil and the splice
// is skipped) — preempting that here would block legitimate `use` flows
// switching to a profile with no root section. If markers are gone AND the
// new plan needs them, Materialize's preflight (R45) will refuse. That's
// the right place for the failure to land.
//
// Hard-block invariants (tested):
//   - Gate never returns prompt without drift to gate on.
//   - Non-interactive sessions never return prompt — they auto-abort or
//     honor the flag (no infinite block).
func DecideGate(input GateInput) GateOutcome {
	if !input.Report.FingerprintOk || len(input.Report.Entries) == 0 {
		reason := "no drift detected"
		if !input.Report.FingerprintOk {
			reason = "no active profile (fingerprint not available)"
		}
		return GateOutcome{
			Kind:   GateOutcomeNoDrift,
			Choice: GateChoiceNoDriftProceed,
			Reason: reason,
		}
	}

	if input.OnDriftFlag != "" {
		// Defense-in-depth: only the three CLI-visible choices are
		// acceptable as flag values. no-drift-proceed is an internal sentinel
		// (the no-drift outcome path) and must never be passed by a caller —
		// passing it here with drift entries present would dispatch to the
		// no-snapshot Materialize path and silently discard user edits
		// without a backup. Refuse rather than silently corrupt.
		if !isValidOnDriftFlag(input.OnDriftFlag) {
			return GateOutcome{
				Kind:   GateOutcomeAuto,
				Choice: GateChoiceAbort,
				Reason: fmt.Sprintf("invalid --on-drift=%s flag (must be discard|persist|abort); aborting", input.OnDriftFlag),
			}
		}
		return GateOutcome{
			Kind:   GateOutcomeAuto,
			Choice: input.OnDriftFlag,
			Reason: fmt.Sprintf("--on-drift=%s flag", input.OnDriftFlag),
		}
	}

	if input.Mode == GateModeNonInteractive {
		return GateOutcome{
			Kind:   GateOutcomeAuto,
			Choice: GateChoiceAbort,
			Reason: "non-interactive session without --on-drift= flag (R21 hard-block)",
		}
	}

	return GateOutcome{
		Kind:   GateOutcomePrompt,
		Choice: "",
		Reason: "interactive prompt required",
	}
}

// isValidOnDriftFlag reports whether c is one of the three CLI-visible
// gate choices a user can pass via --on-drift=<choice>. Returns false for
// no-drift-proceed (internal sentinel) and any other GateChoice variant.
func isValidOnDriftFlag(c GateChoice) bool {
	switch c {
	case GateChoiceDiscard, GateChoicePersist, GateChoiceAbort:
		return true
	}
	return false
}
