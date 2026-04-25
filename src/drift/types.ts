/**
 * E4 cross-epic contracts: DriftReport, DriftEntry, GateChoice, GateOutcome.
 *
 * DriftReport is the load-bearing structure consumed by E5 (CLI status/diff/
 * drift commands and swap orchestration) and by the pre-commit hook's warn
 * path (R25, R25a). The schema is versioned from day one (lesson L05660762):
 * additive optional fields don't bump the version; breaking shape changes do.
 *
 * The gate state machine is a pure function of (DriftReport, mode, optional
 * --on-drift= flag): the IO half (the actual interactive prompt) is owned by
 * E5, but the *decision logic* — when to prompt, when to auto-abort, what's
 * acceptable in non-interactive — lives here so the gate's invariants
 * (hard-blocking; non-interactive never blocks indefinitely) are colocated
 * with the report shape they protect.
 */

import type { ResolvedSourceRef } from "../state/types.js";

/**
 * Schema version for `DriftReport`. Bumped only when E5 must be updated for
 * a breaking shape change. Adding optional fields is a non-breaking change.
 */
export const DRIFT_REPORT_SCHEMA_VERSION = 1 as const;

/**
 * Per-file drift status (R19). Mirrors `DriftKind` in E3 but excludes the
 * `unchanged` value — DriftReport.entries only ever contains drift, not
 * intact files (those are summarized via the count metrics).
 */
export type DriftStatus = "modified" | "added" | "deleted";

/**
 * One drifted file in the report. `provenance` is the set of contributors
 * recorded in `.state.json` at the last successful materialization (R20).
 *
 * v1 reports source-set granularity (the union of contributors that produced
 * the active materialization), not per-file granularity. Per-file provenance
 * would require either re-resolving the plan at drift time (expensive) or
 * storing per-file contributors in `.state.json` (storage explosion). The
 * source-set granularity matches the user's mental model: "the active
 * profile <name> drifted, here are the sources it was built from."
 */
export interface DriftEntry {
  /** Path relative to `.claude/`, posix-style — matches MergedFile.path. */
  relPath: string;
  /** Modified / Added / Deleted (R19). */
  status: DriftStatus;
  /**
   * The materialization sources from `.state.json` (R20). Empty only when
   * `.state.json` is absent or schema-mismatched — drift detection still
   * runs, but provenance is not recoverable.
   */
  provenance: ResolvedSourceRef[];
}

/**
 * The cross-epic, load-bearing contract for the drift bounded context. Per
 * the epic interface declaration:
 *   DriftReport { active, fingerprintOk, entries, scannedFiles, fastPathHits, slowPathHits }
 *
 * Invariants (enforced by tests):
 *  - `entries` is lex-sorted by relPath
 *  - `entries` never contains an `unchanged` status (filtered out)
 *  - `fastPathHits + slowPathHits === scannedFiles + (deleted count)` —
 *    deletions count as slow-path hits because the metadata walk didn't see
 *    them; they came from the recorded fingerprint
 *  - When `fingerprintOk === false` → `entries.length === 0` and counts are 0
 */
export interface DriftReport {
  schemaVersion: typeof DRIFT_REPORT_SCHEMA_VERSION;
  /** Active profile name from `.state.json`, or null if NoActive. */
  active: string | null;
  /**
   * True iff `.state.json` was readable AND `activeProfile` is non-null.
   * False signals "no meaningful drift check possible" — typically NoActive
   * or schema mismatch (R42 graceful degradation). Downstream callers
   * (gate, pre-commit) treat false as "no drift to gate on".
   */
  fingerprintOk: boolean;
  /** Per-file drift entries (status ≠ unchanged). R19, R20. */
  entries: DriftEntry[];
  /** Total live files scanned. */
  scannedFiles: number;
  /** Files matched by the metadata fast path (R18). */
  fastPathHits: number;
  /** Files that required content-hash comparison or were add/delete. */
  slowPathHits: number;
}

/**
 * The user's three choices at the drift gate (R21, R23, R24). Plus the
 * implicit "no-drift-proceed" outcome when there's nothing to gate on —
 * exposed as a distinct value so the orchestrator's switch covers all cases.
 */
export type GateChoice = "discard" | "persist" | "abort" | "no-drift-proceed";

/**
 * Whether the current session can interactively prompt the user. CLI sets
 * this from `process.stdin.isTTY` (E5).
 */
export type GateMode = "interactive" | "non-interactive";

/**
 * Inputs to the gate decision function. `onDriftFlag` is the parsed value of
 * `--on-drift=<choice>` from argv, propagated by E5 — when present, it
 * overrides the interactive prompt in BOTH modes (a user who explicitly
 * picked an answer doesn't need to re-confirm it).
 */
export interface GateInput {
  report: DriftReport;
  mode: GateMode;
  onDriftFlag?: "discard" | "persist" | "abort";
}

/**
 * Output of the gate decision. `kind === "prompt"` means E5 must prompt the
 * user and feed the answer back into `applyGate(choice, ...)`. The other
 * kinds resolve without prompting.
 *
 * Hard-block invariant (epic): non-interactive sessions never reach `prompt`
 * — they auto-abort or honor `onDriftFlag`. Tested explicitly.
 */
export interface GateOutcome {
  kind: "no-drift" | "auto" | "prompt";
  /** Null when kind === "prompt" (caller decides). */
  choice: GateChoice | null;
  /** Human-readable reason for the outcome — surfaced in audit / debug logs. */
  reason: string;
}
