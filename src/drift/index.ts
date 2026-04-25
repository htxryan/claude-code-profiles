/**
 * Public API surface for E4: Drift.
 *
 * Downstream epics (E5 CLI, E6 hook installer) consume only this surface.
 *
 * Cross-epic contracts produced by E4:
 *   - DriftReport (with schemaVersion) — per-file drift + metrics
 *   - GateChoice / GateOutcome — three-way gate state machine output
 *   - applyGate — orchestrator that dispatches discard/persist/abort
 *   - preCommitWarn — fail-open hook entry point
 *
 * Read-only callers (the `drift` command, `status`) only need detectDrift.
 * Mutating callers (the `use`/`sync` swap orchestration) compose the four:
 *   detectDrift → decideGate → (E5 prompts if needed) → applyGate
 */

export {
  DRIFT_REPORT_SCHEMA_VERSION,
  type DriftEntry,
  type DriftReport,
  type DriftStatus,
  type GateChoice,
  type GateInput,
  type GateMode,
  type GateOutcome,
} from "./types.js";

export { detectDrift } from "./detect.js";
export { decideGate } from "./gate.js";
export {
  applyGate,
  type ApplyGateAction,
  type ApplyGateOptions,
  type ApplyGateResult,
} from "./apply.js";
export { preCommitWarn, type PreCommitWarnResult } from "./pre-commit.js";
