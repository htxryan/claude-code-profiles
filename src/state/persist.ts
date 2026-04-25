/**
 * Persist transactional pair (R22b). When the user selects "persist" at the
 * drift gate, we copy the live `.claude/` tree into the active profile's
 * `.claude/` directory using the SAME pending/prior protocol as materialize,
 * then proceed to materialize the new target.
 *
 * The protocol:
 *   (a) write live `.claude/` contents into `<active>/.pending/`
 *   (b) atomically rename `<active>/.claude/` to `<active>/.prior/` (if exists)
 *   (c) atomically rename `<active>/.pending/` to `<active>/.claude/`
 *   (d) materialize the new target (overwrites root `.claude/`)
 *   (e) update `.state.json` with the new active profile
 *
 * Crash recovery: persist's pending/prior is reconciled by `reconcilePersist`
 * keyed on the active profile name. If the process is killed between persist
 * (a-c) completion and step d, the next reconcile sees the materialize-side
 * pending/prior empty and the state file still pointing at the old profile —
 * the user can re-issue the swap and it'll find the persist already in place.
 */

import { promises as fs } from "node:fs";

import type { MergedFile } from "../merge/types.js";
import type { ResolvedPlan } from "../resolver/types.js";

import { atomicRename, pathExists, rmrf } from "./atomic.js";
import { copyTree } from "./copy.js";
import { materialize, type MaterializeResult } from "./materialize.js";
import { buildPersistPaths, type StatePaths } from "./paths.js";
import { reconcileMaterialize, reconcilePersist } from "./reconcile.js";

export interface PersistAndMaterializeOptions {
  /** Profile that owns the live `.claude/` (we copy live into THIS profile). */
  activeProfileName: string;
  /** New plan (target of the swap). */
  newPlan: ResolvedPlan;
  /** Merged files for the new plan. */
  newMerged: ReadonlyArray<MergedFile>;
}

/**
 * Persist live `.claude/` into `<activeProfileName>/.claude/`, then
 * materialize the new plan as live `.claude/`. This is the "drift → persist"
 * gate flow (R22 / R22a / R22b).
 *
 * Lock precondition: caller MUST hold the project lock (E5 swap orchestration
 * wraps this in `withLock`). The function does not acquire its own lock so
 * the persist + materialize pair is bracketed by a single lock acquisition,
 * matching the spec's "lock brackets the rename pair AND the state-write"
 * invariant (multi-reviewer P1, Codex #2).
 */
export async function persistAndMaterialize(
  paths: StatePaths,
  opts: PersistAndMaterializeOptions,
): Promise<MaterializeResult> {
  // Reconciliation order (multi-reviewer P2, Gemini #4): materialize-side
  // first, then persist-side. The previous order risked persisting a
  // partially-reconciled `.claude/` (a prior-restored state) into the
  // profile. With this order, reconcileMaterialize fixes the live tree
  // first, then persistLiveIntoProfile copies a coherent snapshot.
  await reconcileMaterialize(paths);
  await reconcilePersist(paths, opts.activeProfileName);

  await persistLiveIntoProfile(paths, opts.activeProfileName);

  return materialize(paths, opts.newPlan, opts.newMerged);
}

/**
 * The persist half of the pair, exposed for tests and for callers (E4) that
 * want to persist without immediately swapping. The flow is identical to
 * materialize's own pending/prior, scoped to the active profile's directory.
 */
export async function persistLiveIntoProfile(
  paths: StatePaths,
  activeProfileName: string,
): Promise<void> {
  const persist = buildPersistPaths(paths, activeProfileName);

  // The profile directory is expected to exist (the active profile got us
  // here). Defensive create — if it doesn't, the persist still works.
  await fs.mkdir(persist.profileDir, { recursive: true });

  // R22 says we copy the entire live `.claude/` tree (including added/deleted
  // files relative to resolved sources) into the active profile's directory.
  // Step a: stage in pending. The copyTree handles missing source by
  // creating an empty pending — that's the legitimate "user deleted .claude/
  // entirely" state, and persisting an empty tree is correct here.
  await rmrf(persist.pendingDir);
  if (await pathExists(paths.claudeDir)) {
    await copyTree(paths.claudeDir, persist.pendingDir);
  } else {
    // Empty pending dir to make the rename steps below uniform.
    await fs.mkdir(persist.pendingDir, { recursive: true });
  }

  // Step b: rename existing target to prior, if it exists.
  const targetExists = await pathExists(persist.targetClaudeDir);
  if (targetExists) {
    await rmrf(persist.priorDir); // defensive
    await atomicRename(persist.targetClaudeDir, persist.priorDir);
  }

  // Step c: rename pending → target. On failure, restore prior.
  try {
    await atomicRename(persist.pendingDir, persist.targetClaudeDir);
  } catch (err: unknown) {
    if (await pathExists(persist.priorDir)) {
      // Surface restore failures to stderr (multi-reviewer P3, Gemini #6) —
      // the original step-c failure is the primary error, but a failed
      // restore leaves the profile dir in an unrecoverable state and the
      // user needs to know.
      await atomicRename(persist.priorDir, persist.targetClaudeDir).catch(
        (restoreErr: unknown) => {
          const detail =
            restoreErr instanceof Error ? restoreErr.message : String(restoreErr);
          process.stderr.write(
            `claude-profiles: persist restore failed for ${persist.targetClaudeDir}: ${detail}\n`,
          );
        },
      );
    }
    await rmrf(persist.pendingDir).catch(() => undefined);
    throw err;
  }

  // Success — drop prior. (No state-file update at this layer; the caller's
  // subsequent materialize writes the new state.)
  if (await pathExists(persist.priorDir)) {
    await rmrf(persist.priorDir);
  }

  // Touch the profile dir so e.g. `list` shows a recent mtime; not strictly
  // required but matches user expectation that a "persist" updates the
  // profile's last-modified.
  await fs.utimes(persist.targetClaudeDir, new Date(), new Date()).catch(() => undefined);
}
