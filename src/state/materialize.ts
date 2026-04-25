/**
 * Materialization orchestrator (R13 / R14 / R14a / R16 / R16a / R22b / R38).
 *
 * Given a ResolvedPlan + MergedFile[] (produced by E1+E2), apply the three-
 * step pending/prior rename:
 *   (a) write merged bytes to `.claude-profiles/.pending/`
 *   (b) atomically rename existing `.claude/` to `.claude-profiles/.prior/`
 *   (c) atomically rename `.pending/` to `.claude/`
 *
 * On success, write `.state.json` (atomic) and remove `.prior/` in the
 * background. On failure between (a) and (b), remove `.pending/`. On failure
 * between (b) and (c), rename `.prior/` back. Any inconsistency observed at
 * the *next* CLI invocation is fixed by `reconcileMaterialize` (R16a).
 *
 * Locking: this function does NOT acquire the lock — the caller (E5 swap
 * orchestration) wraps the whole sequence (drift gate, materialize, persist
 * if requested, state write) in `withLock`. Reads bypass the lock (R43).
 *
 * Concurrency: a single materialize call is serialized internally — files
 * are written with bounded concurrency (see copy.ts), but no caller-visible
 * parallelism. Lock + state-write fences serialize ACROSS calls.
 */

import { promises as fs } from "node:fs";

import type { MergedFile } from "../merge/types.js";
import type { ResolvedPlan } from "../resolver/types.js";

import { atomicRename, pathExists, rmrf } from "./atomic.js";
import { writeFiles } from "./copy.js";
import {
  fingerprintFromMergedFiles,
  recordMtimes,
} from "./fingerprint.js";
import type { StatePaths } from "./paths.js";
import { reconcileMaterialize } from "./reconcile.js";
import { readStateFile, writeStateFile } from "./state-file.js";
import {
  STATE_FILE_SCHEMA_VERSION,
  type ExternalTrustNotice,
  type FingerprintEntry,
  type ResolvedSourceRef,
  type StateFile,
} from "./types.js";

export interface MaterializeOptions {
  /**
   * If true, do not delete `.prior/` after success. Used by tests that want
   * to inspect the rolled-aside dir; never used in production.
   */
  retainPriorForTests?: boolean;
}

export interface MaterializeResult {
  /** New state file written to disk after successful materialization. */
  state: StateFile;
  /** Snapshot path created if discard-backup was requested by caller. Null otherwise. */
  backupSnapshot: string | null;
}

/**
 * Apply `merged` to disk as the new `.claude/` tree, atomically. Records the
 * resolved sources and fingerprint into `.state.json`. Caller-supplied
 * `discardBackup` is the path returned by `snapshotForDiscard` (E4 calls
 * that *before* materialize on the discard-gate path so it lands in the
 * state record). Pass null when no snapshot was taken (clean swap, sync, etc.).
 */
export async function materialize(
  paths: StatePaths,
  plan: ResolvedPlan,
  merged: ReadonlyArray<MergedFile>,
  opts: MaterializeOptions = {},
  discardBackup: string | null = null,
): Promise<MaterializeResult> {
  // Reconcile any leftover .pending/.prior from a prior crashed run BEFORE
  // we start writing. If reconciliation took action, the live `.claude/` is
  // now consistent with whatever the previous successful state was.
  await reconcileMaterialize(paths);

  // Step a: write merged bytes to pending. We rmrf any leftover (paranoia
  // on top of reconcile's clean) so a stale pending from a non-CCP source
  // doesn't pollute the new tree.
  await rmrf(paths.pendingDir);
  try {
    await writeFiles(paths.pendingDir, merged);
  } catch (err: unknown) {
    // Step a failed before we touched live state — clean up and rethrow.
    await rmrf(paths.pendingDir).catch(() => undefined);
    throw err;
  }

  // Step b: rename existing live `.claude/` to `.prior/` if it exists.
  const liveExists = await pathExists(paths.claudeDir);
  if (liveExists) {
    // Defensive: if `.prior/` somehow exists at this point (reconcile
    // missed it, or a test left it behind), drop it. atomic-rename into an
    // existing target is an error on Windows.
    await rmrf(paths.priorDir);
    try {
      await atomicRename(paths.claudeDir, paths.priorDir);
    } catch (err: unknown) {
      // Step b failed; clean up pending and rethrow. The live `.claude/` is
      // unchanged at this point.
      await rmrf(paths.pendingDir).catch(() => undefined);
      throw err;
    }
  }

  // Step c: rename pending → claudeDir. If this fails, restore from prior.
  try {
    await atomicRename(paths.pendingDir, paths.claudeDir);
  } catch (err: unknown) {
    // Restore prior. We've already wiped the in-progress live state.
    if (await pathExists(paths.priorDir)) {
      await atomicRename(paths.priorDir, paths.claudeDir).catch(() => undefined);
    }
    await rmrf(paths.pendingDir).catch(() => undefined);
    throw err;
  }

  // Step c succeeded. Build the state file BEFORE removing prior so a crash
  // during state-write still leaves a recoverable artifact (next run sees
  // .claude/ + .prior/ -> reconcile will detect as "fresh materialize landed
  // but prior cleanup didn't run"; a follow-up reconcile will just drop the
  // .prior/ since the new state file is consistent with .claude/).

  // Compute fingerprint from merged bytes (we have them in memory) and then
  // overlay mtimes from the freshly-renamed live tree.
  let fingerprint = fingerprintFromMergedFiles(merged);
  fingerprint = await recordMtimes(paths.claudeDir, fingerprint);

  // Preserve external-trust notices from the prior state and add any new
  // ones for external paths in this plan that haven't been noticed before.
  const prior = await readStateFile(paths);
  const trustNotices = mergeExternalTrustNotices(
    prior.state.externalTrustNotices,
    plan,
  );

  const newState: StateFile = {
    schemaVersion: STATE_FILE_SCHEMA_VERSION,
    activeProfile: plan.profileName,
    materializedAt: new Date().toISOString(),
    resolvedSources: plan.contributors.map(toResolvedSourceRef),
    fingerprint,
    externalTrustNotices: trustNotices,
  };
  await writeStateFile(paths, newState);

  // Background-drop prior dir. We await for tests determinism but production
  // crash injection between this and the next op is recoverable (reconcile
  // would see no `.prior/` because rmrf is idempotent).
  if (!opts.retainPriorForTests) {
    if (await pathExists(paths.priorDir)) {
      await rmrf(paths.priorDir);
    }
  }

  return { state: newState, backupSnapshot: discardBackup };
}

function toResolvedSourceRef(c: {
  id: string;
  kind: "ancestor" | "include" | "profile";
  rootPath: string;
  external: boolean;
}): ResolvedSourceRef {
  return {
    id: c.id,
    kind: c.kind,
    rootPath: c.rootPath,
    external: c.external,
  };
}

/**
 * R37a: external-trust notices are recorded in `.state.json` so they aren't
 * re-printed on every swap. We merge: keep all existing notices, append a
 * notice for each external path in the plan that's not already recorded.
 */
function mergeExternalTrustNotices(
  existing: ReadonlyArray<ExternalTrustNotice>,
  plan: ResolvedPlan,
): ExternalTrustNotice[] {
  const seen = new Set(existing.map((e) => e.resolvedPath));
  const merged: ExternalTrustNotice[] = [...existing];
  const now = new Date().toISOString();
  for (const ext of plan.externalPaths) {
    if (!seen.has(ext.resolvedPath)) {
      seen.add(ext.resolvedPath);
      merged.push({
        raw: ext.raw,
        resolvedPath: ext.resolvedPath,
        noticedAt: now,
      });
    }
  }
  return merged;
}

/**
 * Read the recorded fingerprint from the active state file. Helper for E4
 * drift detection — returns an empty fingerprint when no state exists.
 */
export async function readRecordedFingerprint(
  paths: StatePaths,
): Promise<Record<string, FingerprintEntry>> {
  const { state } = await readStateFile(paths);
  return state.fingerprint.files;
}

/**
 * Verify (used by tests / `validate` dry-runs) that the live tree's stat
 * snapshot is internally consistent — every recorded file exists with the
 * recorded size. Returns `true` iff so. Lighter than a full fingerprint
 * comparison; used to gate "are we in a clean materialized state" UX.
 */
export async function isLiveConsistentWithRecord(paths: StatePaths): Promise<boolean> {
  const { state } = await readStateFile(paths);
  if (state.activeProfile === null) return false;
  for (const [relPath, entry] of Object.entries(state.fingerprint.files)) {
    try {
      const stat = await fs.stat(`${paths.claudeDir}/${relPath}`);
      if (stat.size !== entry.size) return false;
    } catch {
      return false;
    }
  }
  return true;
}
