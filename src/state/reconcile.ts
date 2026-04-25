/**
 * Pending/prior reconciliation (R16a). Run at startup of every CLI command,
 * under the lock for mutating ops, lock-free for read-only ops (R43).
 *
 * Recovery rules:
 *  - If `.prior/` exists: a prior materialization crashed AFTER we moved the
 *    old `.claude/` aside but BEFORE we renamed `.pending/` into place. The
 *    invariant we want is "live `.claude/` is the last successful state".
 *    We restore by renaming `.prior/` back to `.claude/` (overwriting any
 *    partial `.claude/` from a half-completed step c) and discarding `.pending/`.
 *  - If only `.pending/` exists: a prior materialization crashed during step
 *    a (before we touched `.claude/`). The live state is intact. We just
 *    drop `.pending/`.
 *  - If neither exists: no-op.
 *
 * Persist (R22b) uses the same pattern with the per-profile pending/prior
 * dirs. We expose a generic `reconcilePendingPrior` that takes the three
 * paths so the materialize and persist call sites share behavior.
 */

import { promises as fs } from "node:fs";

import { atomicRename, pathExists, rmrf } from "./atomic.js";
import { buildPersistPaths, type StatePaths } from "./paths.js";

let reconcileCounter = 0;

/**
 * Outcome of a single reconciliation step. Surfaced upward so E5 can print
 * the "(reconciled crashed materialization: restored .claude/ from .prior/)"
 * notice the spec promises. `targetLabel` identifies which target was
 * reconciled — for materialize it's "<root>/.claude/", for persist it's
 * "<profile>/.claude/".
 */
export type ReconcileOutcome =
  | { kind: "none"; targetLabel: string }
  | { kind: "restored-from-prior"; targetLabel: string }
  | { kind: "discarded-pending"; targetLabel: string };

/**
 * Generic reconciliation step. Returns the action taken so the caller can
 * surface a notice. `target`, `pendingDir`, `priorDir` form the protocol's
 * three-way path triple; `targetLabel` is a human-friendly description for
 * messages.
 */
export async function reconcilePendingPrior(
  target: string,
  pendingDir: string,
  priorDir: string,
  targetLabel: string,
): Promise<ReconcileOutcome> {
  const priorExists = await pathExists(priorDir);
  const pendingExists = await pathExists(pendingDir);

  if (priorExists) {
    // The prior dir was rolled aside (step b). Two distinct on-disk states
    // can land us here:
    //   (i)  step c never ran or partially renamed — `.claude/` is missing
    //        or holds half-renamed bytes;
    //   (ii) step c finished but the cleanup of `.prior/` didn't run before
    //        a crash — `.claude/` is the freshly committed content and
    //        `.prior/` is the previous version.
    // Spec invariant ("live `.claude/` is the last successful state") says
    // restore from `.prior/` in both cases — case (ii) is rare and the
    // re-materialize after restore is idempotent.
    //
    // Window-narrowing (multi-reviewer P2, Gemini #3): rename `.claude/` to
    // a per-attempt scratch dir before renaming prior back. Concurrent
    // readers (R43) see EITHER the original `.claude/` OR the restored one,
    // never an empty/missing tree, except for the brief rename window.
    if (await pathExists(target)) {
      const scratch = `${target}.reconcile-${reconcileCounter++}-${Math.random()
        .toString(36)
        .slice(2, 8)}`;
      try {
        await atomicRename(target, scratch);
      } catch {
        // Rename failed (filesystem doesn't support move-into-existing-dir,
        // or .claude/ is locked). Fall back to rmrf — wider window but the
        // recovery still completes.
        await rmrf(target);
      }
      // Best-effort cleanup of the scratch path; if cleanup fails the user
      // sees a `.claude.reconcile-*` dir but recovery already succeeded.
      await rmrf(scratch).catch(() => undefined);
    }
    await atomicRename(priorDir, target);
    if (pendingExists) {
      await rmrf(pendingDir);
    }
    return { kind: "restored-from-prior", targetLabel };
  }

  if (pendingExists) {
    // Step a partially succeeded (or fully succeeded but step b never ran).
    // The live target is untouched; drop .pending/ to clear the slate.
    await rmrf(pendingDir);
    return { kind: "discarded-pending", targetLabel };
  }

  return { kind: "none", targetLabel };
}

/**
 * Reconcile the materialize target (root `.claude/`). Call at the start of
 * any mutating op (use, sync, persist). Cheap on the steady-state path
 * (two stat calls, both ENOENT).
 */
export async function reconcileMaterialize(paths: StatePaths): Promise<ReconcileOutcome> {
  // Ensure profilesDir exists so reads on .pending/.prior don't surface
  // unrelated ENOTDIR/ENOENT confusingly. Cheap.
  await fs.mkdir(paths.profilesDir, { recursive: true });
  return reconcilePendingPrior(
    paths.claudeDir,
    paths.pendingDir,
    paths.priorDir,
    paths.claudeDir,
  );
}

/**
 * Reconcile a per-profile persist target. Used during the persist transactional
 * pair recovery. `profileName` selects which profile dir's pending/prior to
 * inspect.
 */
export async function reconcilePersist(
  paths: StatePaths,
  profileName: string,
): Promise<ReconcileOutcome> {
  const persist = buildPersistPaths(paths, profileName);
  // If the profile dir doesn't exist, there's nothing to reconcile.
  if (!(await pathExists(persist.profileDir))) {
    return { kind: "none", targetLabel: persist.targetClaudeDir };
  }
  return reconcilePendingPrior(
    persist.targetClaudeDir,
    persist.pendingDir,
    persist.priorDir,
    persist.targetClaudeDir,
  );
}
