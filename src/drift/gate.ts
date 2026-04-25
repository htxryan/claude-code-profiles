/**
 * Drift gate state machine (R21, R23, R24). Pure decision function: given a
 * DriftReport, the session's interactivity, and an optional `--on-drift=`
 * flag, decide whether to:
 *   - proceed without gating ("no-drift")
 *   - resolve directly via the flag or auto-abort ("auto")
 *   - require an interactive prompt ("prompt") — caller (E5) drives the
 *     prompt and feeds the result into applyGate()
 *
 * Key invariants (tested explicitly):
 *   - Gate is hard-blocking: when drift exists and there's no flag, an
 *     interactive session always returns "prompt" (never an implicit default)
 *   - Non-interactive sessions never return "prompt" — they auto-abort or
 *     honor the flag (no infinite block)
 *   - "no-drift" only when fingerprintOk + entries.length === 0; degraded
 *     `fingerprintOk: false` is also treated as no-drift (nothing to gate on)
 */

import type { GateInput, GateOutcome } from "./types.js";

/**
 * Decide what the orchestrator should do at the drift gate.
 *
 * Decision table (in priority order):
 *   1. report.fingerprintOk === false                → no-drift
 *   2. report.entries.length === 0                   → no-drift
 *   3. onDriftFlag set                               → auto (use flag)
 *   4. mode === non-interactive (no flag)            → auto (abort)
 *   5. mode === interactive (drift, no flag)         → prompt
 *
 * The flag wins over interactive mode (rule 3 before rule 5): a user who
 * passed `--on-drift=discard` doesn't want to be re-prompted "are you sure".
 */
export function decideGate(input: GateInput): GateOutcome {
  if (!input.report.fingerprintOk || input.report.entries.length === 0) {
    return {
      kind: "no-drift",
      choice: "no-drift-proceed",
      reason: input.report.fingerprintOk
        ? "no drift detected"
        : "no active profile (fingerprint not available)",
    };
  }

  if (input.onDriftFlag) {
    // The flag's value type is a strict subset of GateChoice — no cast.
    return {
      kind: "auto",
      choice: input.onDriftFlag,
      reason: `--on-drift=${input.onDriftFlag} flag`,
    };
  }

  if (input.mode === "non-interactive") {
    return {
      kind: "auto",
      choice: "abort",
      reason: "non-interactive session without --on-drift= flag (R21 hard-block)",
    };
  }

  return {
    kind: "prompt",
    choice: null,
    reason: "interactive prompt required",
  };
}
