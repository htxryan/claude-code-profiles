/**
 * Gate-choice application (R22, R22a, R22b, R23, R23a, R24). Given a resolved
 * GateChoice, dispatch to the right E3 primitive:
 *   - no-drift-proceed  → materialize directly (no backup)
 *   - discard           → snapshotForDiscard + materialize (R23a backup)
 *   - persist           → persistAndMaterialize (R22b transactional pair)
 *   - abort             → no-op (R24)
 *
 * **Lock precondition**: the caller (E5 swap orchestration) MUST hold the
 * project lock around this call. We don't acquire it here so that the entire
 * swap sequence — drift detect + gate decide + gate apply + state-write — is
 * bracketed by a single `withLock` (E3 invariant: lock brackets the rename
 * pair AND the state-write). E5's swap dispatcher is responsible for the
 * `withLock(paths, async () => { ... applyGate ... })` shape; calling
 * `applyGate` outside a lock is a programmer error and breaks the rename-
 * pair atomicity invariant.
 *
 * **TOCTOU between detect and apply** (multi-reviewer P0-1 / P1-2): the
 * recommended orchestration shape is to run `detectDrift` BOTH outside the
 * lock (to drive the interactive prompt without blocking other readers per
 * R43) AND once more inside `withLock` immediately before `applyGate`. The
 * inside-lock report is the ground truth: the snapshot/persist captures
 * whatever's live at lock acquisition time, which is what the user
 * effectively chose by selecting their gate option. The outside-lock report
 * may show fewer or more entries than the inside-lock one if a parallel
 * editor was active — both states are equally legitimate "what was drifted
 * at the moment of decision" snapshots.
 *
 * Read-only callers (the `drift` command, pre-commit warn) don't need this
 * module — they call `detectDrift` directly without the lock.
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

    default: {
      // Exhaustiveness guard (multi-reviewer P1-4): if GateChoice grows a
      // new variant, TypeScript flags this assignment at compile time so a
      // missing case can't slip through with a silent `undefined` return.
      const _exhaustive: never = choice;
      throw new Error(`unreachable: unknown gate choice ${String(_exhaustive)}`);
    }
  }
}
