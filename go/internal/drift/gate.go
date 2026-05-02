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
//  3. all entries unrecoverable                    → auto (abort, run init)
//  4. OnDriftFlag set                              → auto (use flag)
//  5. mode == non-interactive (no flag)            → auto (abort)
//  6. mode == interactive (drift, no flag)         → prompt
//
// The flag wins over interactive mode (rule 4 before rule 6): a user who
// passed --on-drift=discard doesn't want to be re-prompted "are you sure".
//
// Rule 3 (all-unrecoverable auto-abort) sits BEFORE the flag check because
// neither discard nor persist can succeed when project-root markers are gone:
// Materialize would fail at preflight regardless of the user's choice. Auto-
// aborting with a "run init" reason produces an actionable diagnostic instead
// of a confusing post-prompt failure.
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

	if allUnrecoverable(input.Report.Entries) {
		return GateOutcome{
			Kind:   GateOutcomeAuto,
			Choice: GateChoiceAbort,
			Reason: "all drift entries are unrecoverable (markers missing/malformed); run `c3p init` to repair",
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

// allUnrecoverable reports true iff every entry has Status ==
// DriftStatusUnrecoverable. Empty input returns false (caller must already
// have handled the no-drift case).
func allUnrecoverable(entries []DriftEntry) bool {
	if len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		if e.Status != DriftStatusUnrecoverable {
			return false
		}
	}
	return true
}
