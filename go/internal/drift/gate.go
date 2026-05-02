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
