/**
 * Gate-choice application (R22, R22a, R22b, R23, R23a, R24). Given a resolved
 * GateChoice, dispatch to the right E3 primitive:
 *   - no-drift-proceed  → materialize directly (no backup)
 *   - discard           → snapshotForDiscard + materialize (R23a backup)
 *   - persist           → persistAndMaterialize (R22b transactional pair)
 *   - abort             → no-op (R24)
 *
 * Lock precondition: the caller (E5 swap orchestration) must hold the project
 * lock around this call. We don't acquire it here so that the entire swap
 * sequence — drift detect + gate decide + gate apply + state-write — is
 * bracketed by a single lock acquisition (E3 invariant: lock brackets the
 * rename pair AND the state-write).
 *
 * Read-only callers (the `drift` command, pre-commit warn) don't need this
 * module — they call `detectDrift` directly.
 */

import type { MergedFile } from "../merge/types.js";
import type { ResolvedPlan } from "../resolver/types.js";
import { snapshotForDiscard } from "../state/backup.js";
import { materialize, type MaterializeResult } from "../state/materialize.js";
import type { StatePaths } from "../state/paths.js";
import { persistAndMaterialize } from "../state/persist.js";

import type { GateChoice } from "./types.js";

export interface ApplyGateOptions {
  paths: StatePaths;
  /** New plan (target of the swap). */
  plan: ResolvedPlan;
  /** Merged files for the new plan. */
  merged: ReadonlyArray<MergedFile>;
  /**
   * Active profile name from `.state.json` BEFORE the swap. Required when
   * `choice === "persist"` — that's the profile the live `.claude/` is
   * persisted *into*. Null is acceptable for discard / no-drift / abort
   * paths. Persist with null active is a programmer error (gate decision
   * never returns persist on NoActive).
   */
  activeProfileName: string | null;
}

export type ApplyGateAction =
  | "materialized"
  | "persisted-and-materialized"
  | "aborted";

export interface ApplyGateResult {
  action: ApplyGateAction;
  /** Backup snapshot path if discard branch took one; null otherwise. */
  backupSnapshot: string | null;
  /** Materialize result, or null on abort. */
  materializeResult: MaterializeResult | null;
}

/**
 * Apply the user's (or auto-resolved) gate choice. Returns a structured
 * result so the orchestrator can build user-facing messages without
 * re-deriving state.
 */
export async function applyGate(
  choice: GateChoice,
  opts: ApplyGateOptions,
): Promise<ApplyGateResult> {
  switch (choice) {
    case "abort":
      return { action: "aborted", backupSnapshot: null, materializeResult: null };

    case "no-drift-proceed": {
      const r = await materialize(opts.paths, opts.plan, opts.merged);
      return {
        action: "materialized",
        backupSnapshot: null,
        materializeResult: r,
      };
    }

    case "discard": {
      // Snapshot BEFORE the rename so the backup captures pre-swap content
      // (R23a). materialize() then performs the pending/prior rename — by
      // the time .claude/ is overwritten, the snapshot is already on disk.
      const backup = await snapshotForDiscard(opts.paths);
      const r = await materialize(opts.paths, opts.plan, opts.merged, {}, backup);
      return {
        action: "materialized",
        backupSnapshot: backup,
        materializeResult: r,
      };
    }

    case "persist": {
      if (opts.activeProfileName === null) {
        // Defense-in-depth: decideGate() returns "no-drift-proceed" when
        // fingerprintOk is false (which implies activeProfile is null). If
        // we land here, a caller bypassed the decider with a hand-crafted
        // choice. Refuse rather than write into a profile-less directory.
        throw new Error(
          "persist gate choice requires an active profile in .state.json — none recorded",
        );
      }
      const r = await persistAndMaterialize(opts.paths, {
        activeProfileName: opts.activeProfileName,
        newPlan: opts.plan,
        newMerged: opts.merged,
      });
      return {
        action: "persisted-and-materialized",
        backupSnapshot: null,
        materializeResult: r,
      };
    }
  }
}
