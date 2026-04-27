/**
 * Materialization orchestrator (R13 / R14 / R14a / R16 / R16a / R22b / R38 /
 * R45 / R46).
 *
 * Given a ResolvedPlan + MergedFile[] (produced by E1+E2), apply the three-
 * step pending/prior rename for `.claude/` destination files:
 *   (a) write merged bytes to `.claude-profiles/.meta/pending/`
 *   (b) atomically rename existing `.claude/` to `.claude-profiles/.meta/prior/`
 *   (c) atomically rename `.meta/pending/` to `.claude/`
 *
 * On success, write `.state.json` (atomic) and remove `.prior/` in the
 * background. On failure between (a) and (b), remove `.pending/`. On failure
 * between (b) and (c), rename `.prior/` back. Any inconsistency observed at
 * the *next* CLI invocation is fixed by `reconcileMaterialize` (R16a).
 *
 * cw6/T4 (R45/R46): MergedFile entries with `destination === 'projectRoot'`
 * are NOT included in the pending/prior whole-tree write. Instead, after
 * step c succeeds, materialize splices the merged section bytes into the
 * project-root `CLAUDE.md` between the `<!-- claude-profiles:vN:begin/end -->`
 * markers via temp-file + atomic rename (preserving every byte outside the
 * markers). The marker presence check runs as a PRE-FLIGHT before any
 * side-effects in either destination; missing/malformed markers abort the
 * WHOLE materialize (R45 atomic-across-destinations) — neither `.claude/`
 * NOR project-root CLAUDE.md is written, exit 1 with an actionable message.
 *
 * Locking precondition: caller MUST hold the project lock (E5 swap
 * orchestration wraps drift gate + materialize + persist + state-write in a
 * single `withLock`). The lock brackets BOTH writes (`.claude/` rename pair
 * AND the projectRoot section splice) AND the state-write so partial-success
 * windows are not observable (multi-reviewer P1, Codex #2; R45).
 * Reads bypass the lock (R43).
 *
 * Concurrency: a single materialize call is serialized internally — files
 * are written with bounded concurrency (see copy.ts), but no caller-visible
 * parallelism. Lock + state-write fences serialize ACROSS calls.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { RootClaudeMdMarkersMissingError } from "../errors/index.js";
import { parseMarkers, renderManagedBlock } from "../markers.js";
import type { MergedFile } from "../merge/types.js";
import type { ResolvedPlan } from "../resolver/types.js";

import { atomicRename, fsyncDir, pathExists, rmrf } from "./atomic.js";
import { writeFiles } from "./copy.js";
import {
  fingerprintFromMergedFiles,
  hashBytes,
  recordMtimes,
} from "./fingerprint.js";
import { rootClaudeMdTmpPath, type StatePaths } from "./paths.js";
import { reconcileMaterialize } from "./reconcile.js";
import { readStateFile, writeStateFile } from "./state-file.js";
import {
  STATE_FILE_SCHEMA_VERSION,
  type ExternalTrustNotice,
  type FingerprintEntry,
  type ResolvedSourceRef,
  type SectionFingerprint,
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

  // cw6/T4 (R45): split merged by destination. The `.claude/` group goes
  // through the historical pending/prior whole-tree protocol; the
  // `projectRoot` group goes through a section-splice on the live root
  // CLAUDE.md. Both writes happen under the caller's lock so the pair is
  // atomic-across-destinations from a concurrent reader's POV.
  const claudeMerged: MergedFile[] = [];
  const rootMerged: MergedFile[] = [];
  for (const m of merged) {
    if (m.destination === "projectRoot") {
      rootMerged.push(m);
    } else {
      claudeMerged.push(m);
    }
  }

  // R45 PRE-FLIGHT (CRITICAL ORDERING): if any projectRoot files are in this
  // plan, verify the live root CLAUDE.md has well-formed markers BEFORE we
  // touch anything. If markers are missing/malformed, abort the WHOLE
  // materialize (atomic-across-destinations) — neither destination must see
  // any side-effect. This must run BEFORE step a (`.pending/` write) and
  // BEFORE step b (live `.claude/` → `.prior/` rename).
  //
  // The pre-check intentionally returns the parsed before/after slices so
  // we can splice without re-reading the file (avoids a TOCTOU window where
  // user edits could interleave between the marker check and the splice
  // write — the in-memory slices are the bytes we'll preserve).
  //
  // P1-B: we ALSO need to consider the case where the new plan contributes
  // NO projectRoot file but the PRIOR materialize did — in that case the
  // live root CLAUDE.md still has the prior profile's section bytes
  // between the markers, which the new active profile does not own. We
  // splice an EMPTY section in to clear them. The trigger is "prior state
  // had a non-null rootClaudeMdSection". If the live file has no markers
  // (because the user removed them after opting out per the migration
  // doc), the splice plan is null and we leave the file alone — there's
  // nothing for us to write.
  //
  // Reading prior state up-front (rather than at the post-splice state-
  // build below) lets us decide whether the empty-splice path applies
  // before any side-effects happen.
  const prior = await readStateFile(paths);
  let rootSplicePlan: RootSplicePlan | null = null;
  if (rootMerged.length > 0) {
    rootSplicePlan = await preflightRootSplice(paths, rootMerged);
  } else if (prior.state.rootClaudeMdSection !== null) {
    // Prior materialize wrote a section; new plan does not. Try to stage an
    // empty-section splice. preflightEmptyRootSplice returns null if the
    // live file is missing or its markers were removed — in either of those
    // cases there is nothing for us to clear, so we no-op (the user's edit
    // to remove markers is a documented opt-out path).
    rootSplicePlan = await preflightEmptyRootSplice(paths);
  }

  // Step a: write `.claude/`-destination merged bytes to pending. We rmrf
  // any leftover (paranoia on top of reconcile's clean) so a stale pending
  // from a non-CCP source doesn't pollute the new tree.
  //
  // Empty `claudeMerged` (R45a edge: a profile that contributes ONLY a
  // project-root CLAUDE.md, with no `.claude/` files) is handled — writeFiles
  // creates the empty pending dir and the rename swap below produces an
  // empty `.claude/`. That matches the spec's "we own .claude/ entirely"
  // contract: if the active profile contributes nothing to .claude/, the
  // live .claude/ is empty.
  await rmrf(paths.pendingDir);
  try {
    await writeFiles(paths.pendingDir, claudeMerged);
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
  //
  // bj0 followup: under heavy contention the rename can race with stale FS
  // state from a prior crashed materialize that the entrypoint reconcile
  // missed (because pending wasn't an issue at reconcile time). If
  // pendingDir disappeared between step a and step c (transient ENOENT —
  // observed empirically on macOS APFS under N=20 simultaneous swaps),
  // retry once after re-running reconcileMaterialize. The retry is bounded
  // (at most one extra attempt) so a genuine FS fault still surfaces.
  try {
    await atomicRename(paths.pendingDir, paths.claudeDir);
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      // Re-stage pending and retry once. We already have the merged bytes
      // in memory, so re-running step a is idempotent. Reconcile cleans any
      // leftover prior/pending from a peer race.
      try {
        await reconcileMaterialize(paths);
        await rmrf(paths.pendingDir).catch(() => undefined);
        await writeFiles(paths.pendingDir, claudeMerged);
        await atomicRename(paths.pendingDir, paths.claudeDir);
        // Retry succeeded; carry on as if step c worked first time.
      } catch {
        // Retry failed too — fall through to the rollback path below with
        // the ORIGINAL error (more diagnostic than the retry's error).
        await attemptStepCRollback(paths);
        throw err;
      }
    } else {
      await attemptStepCRollback(paths);
      throw err;
    }
  }

  // cw6/T4 step b' (the "step c'" splice): write the section bytes into
  // project-root CLAUDE.md if the plan contributed any. This happens AFTER
  // the .claude/ swap so a failure here leaves the .claude/ swap committed
  // (the `.prior/` cleanup hasn't run yet, so reconcile + re-run is safe)
  // — but we also surface the splice error to the caller so the user knows
  // the projectRoot side did NOT land.
  //
  // We could move this BEFORE step b/c, but that creates a different
  // failure mode: a projectRoot splice that lands followed by a .claude/
  // swap that fails would leave the user with a half-applied profile (root
  // CLAUDE.md updated, .claude/ unchanged). Doing the splice last means a
  // failure here is recoverable on the next `use` (markers are still valid;
  // re-running materialize re-applies the splice idempotently). The tradeoff
  // is documented; the test suite covers both crash points.
  let rootSectionFingerprint: SectionFingerprint | null = null;
  if (rootSplicePlan !== null) {
    const fp = await applyRootSplice(paths, rootSplicePlan);
    // P1-B: when this is the empty-splice path (new plan has no projectRoot
    // contributor), record `null` rather than the empty-section fingerprint.
    // The state field tracks "is there a managed section we own" — and the
    // answer for the new plan is no. The bytes are still cleared on disk
    // (the splice ran), but state correctly reflects "no contribution".
    if (rootMerged.length > 0) {
      rootSectionFingerprint = fp;
    }
  }

  // Step c succeeded. Build the state file BEFORE removing prior so a crash
  // during state-write still leaves a recoverable artifact (next run sees
  // .claude/ + .prior/ -> reconcile will detect as "fresh materialize landed
  // but prior cleanup didn't run"; a follow-up reconcile will just drop the
  // .prior/ since the new state file is consistent with .claude/).

  // Compute fingerprint from merged bytes (we have them in memory) and then
  // overlay mtimes from the freshly-renamed live tree. Whole-file
  // fingerprinting per R18/R19 applies ONLY to `.claude/` destination files;
  // the projectRoot section uses a separate section-only fingerprint (R46).
  let fingerprint = fingerprintFromMergedFiles(claudeMerged);
  fingerprint = await recordMtimes(paths.claudeDir, fingerprint);

  // Preserve external-trust notices from the prior state and add any new
  // ones for external paths in this plan that haven't been noticed before.
  // (P1-B: `prior` is read above pre-side-effects for the empty-splice
  // decision; reuse it here.)
  const trustNotices = mergeExternalTrustNotices(
    prior.state.externalTrustNotices,
    plan,
  );

  // cw6/T4 (R46): preserve any prior section fingerprint when the new plan
  // doesn't contribute a projectRoot CLAUDE.md. This keeps drift detection
  // honest across `use leaf-with-root → use leaf-without-root → use leaf-
  // with-root` cycles: if the new plan has no root contribution, the field
  // becomes null (we never wrote a section, so there's nothing to track).
  // If the new plan DID contribute one, we use the freshly computed
  // fingerprint from the splice that just landed.
  const newState: StateFile = {
    schemaVersion: STATE_FILE_SCHEMA_VERSION,
    activeProfile: plan.profileName,
    materializedAt: new Date().toISOString(),
    resolvedSources: plan.contributors.map(toResolvedSourceRef),
    fingerprint,
    externalTrustNotices: trustNotices,
    rootClaudeMdSection: rootSectionFingerprint,
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
      // path.join handles the posix-relPath / OS-native rootDir mismatch
      // (multi-reviewer P3, Gemini #9) — Node tolerates mixed separators on
      // Windows but consistency with the rest of the module matters.
      const stat = await fs.stat(path.join(paths.claudeDir, relPath));
      if (stat.size !== entry.size) return false;
    } catch {
      return false;
    }
  }
  return true;
}

/**
 * Pre-flight + plan for the projectRoot section splice (cw6/T4).
 *
 * Captures the live file's parsed before/after slices in memory at the
 * moment we verify the markers, plus the new section bytes we'll write.
 * The pre-flight runs BEFORE any side-effects in materialize so a missing-
 * marker abort leaves both `.claude/` and projectRoot CLAUDE.md untouched
 * (R45 atomic-across-destinations invariant).
 *
 * Why we hold the parsed slices rather than re-read at write time: re-
 * reading would open a TOCTOU window where a user edit between pre-flight
 * and splice could inject malformed markers — the splice would then either
 * fail or write to a different position than the pre-flight checked. By
 * snapshotting in memory under the lock, the splice is deterministic.
 */
interface RootSplicePlan {
  /** Path to the live file we'll write back. */
  filePath: string;
  /** Markers version captured from the parse (echoed back into the new file). */
  version: number;
  /** Bytes above the `:begin` marker; preserved byte-for-byte. */
  before: string;
  /** Bytes below the `:end` marker; preserved byte-for-byte. */
  after: string;
  /**
   * The merged section body to splice between markers. Concatenation of all
   * `projectRoot`-destination MergedFile bytes — currently always exactly
   * one entry (CLAUDE.md), but the array form is robust against future
   * additions.
   */
  sectionBytes: string;
}

/**
 * Read the live project-root CLAUDE.md, verify markers, and stage a splice
 * plan. Throws CliUserError(exit 1) with the spec-mandated remediation
 * message if the file is absent or markers are missing/malformed (R44/R45).
 *
 * Uses `parseMarkers` from src/markers.ts (the single source-of-truth for
 * the regex). The error message names the file path and references
 * `claude-profiles init` as the remediation per spec §12.4.
 */
async function preflightRootSplice(
  paths: StatePaths,
  rootMerged: ReadonlyArray<MergedFile>,
): Promise<RootSplicePlan> {
  const filePath = paths.rootClaudeMdFile;
  let content: string;
  try {
    content = await fs.readFile(filePath, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      throw new RootClaudeMdMarkersMissingError(filePath);
    }
    throw err;
  }
  const parsed = parseMarkers(content);
  if (!parsed.found) {
    // Both "absent" (file present, no markers) and "malformed" (partial /
    // multi-block / version-mismatch) collapse to the same actionable
    // message per spec §12.4: the user's remediation is identical.
    throw new RootClaudeMdMarkersMissingError(filePath);
  }

  // Concat all rootMerged entries' bytes. In practice rootMerged is always
  // a single CLAUDE.md entry today (the merge engine groups by path), but
  // the loop is robust if the spec ever expands what lives at projectRoot.
  // Sort by path for determinism (E2 already lex-sorts; this is defense in
  // depth so a future caller can't observe nondeterministic splice order).
  const sortedRoot = [...rootMerged].sort((a, b) => a.path.localeCompare(b.path));
  const sectionBytes = sortedRoot.map((m) => m.bytes.toString("utf8")).join("");

  return {
    filePath,
    version: parsed.version,
    before: parsed.before,
    after: parsed.after,
    sectionBytes,
  };
}

/**
 * P1-B: stage an empty-section splice when the new plan contributes nothing
 * to projectRoot but the prior state had a section. This clears stale
 * section bytes from a previously-active root-contributing profile so the
 * new (non-root-contributing) profile's environment doesn't leak the old
 * profile's instructions.
 *
 * Returns null when the splice should be skipped — specifically:
 *   - root CLAUDE.md is missing (user removed it, or never ran init); or
 *   - markers are absent/malformed (user opted out per the migration doc by
 *     deleting the marker block).
 *
 * Both of those are documented opt-out paths in
 * docs/migration/cw6-section-ownership.md §"Opting out". A null return
 * means no write happens, which is correct: there is nothing for us to
 * clear.
 *
 * Unlike preflightRootSplice we do NOT throw on missing markers here —
 * the user's prior plan needed them, but the new plan does not, and the
 * pre-flight error message ("run init") would be misleading for a `use`
 * that no longer touches projectRoot.
 */
async function preflightEmptyRootSplice(
  paths: StatePaths,
): Promise<RootSplicePlan | null> {
  const filePath = paths.rootClaudeMdFile;
  let content: string;
  try {
    content = await fs.readFile(filePath, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return null;
    }
    throw err;
  }
  const parsed = parseMarkers(content);
  if (!parsed.found) {
    // Markers absent or malformed → opted out / never opted in. No-op.
    return null;
  }
  return {
    filePath,
    version: parsed.version,
    before: parsed.before,
    after: parsed.after,
    sectionBytes: "",
  };
}

/**
 * Step c rollback (bj0 followup): try to restore the rolled-aside `.prior/`
 * back to `.claude/`. If `.claude/` is non-empty (a partial step c landed
 * bytes there), rmrf it first so the rename doesn't fail with ENOTEMPTY.
 * Errors are surfaced via stderr because the user needs to know if both
 * the swap AND the rollback failed (multi-reviewer P3, Gemini #6).
 */
async function attemptStepCRollback(paths: StatePaths): Promise<void> {
  if (!(await pathExists(paths.priorDir))) {
    // No prior to restore — nothing to roll back. Caller will surface the
    // original step-c error.
    await rmrf(paths.pendingDir).catch(() => undefined);
    return;
  }
  // bj0: if step c partially renamed bytes into `.claude/` (rare, but
  // observed under heavy contention), the symmetric `prior → claude`
  // rename would fail with ENOTEMPTY. Drop the partial target first.
  if (await pathExists(paths.claudeDir)) {
    await rmrf(paths.claudeDir).catch(() => undefined);
  }
  try {
    await atomicRename(paths.priorDir, paths.claudeDir);
  } catch (restoreErr: unknown) {
    const detail =
      restoreErr instanceof Error ? restoreErr.message : String(restoreErr);
    process.stderr.write(
      `claude-profiles: rollback failed restoring ${paths.claudeDir}: ${detail}\n`,
    );
  }
  await rmrf(paths.pendingDir).catch(() => undefined);
}

/**
 * Apply a previously-staged splice plan: render the managed block from the
 * new section bytes, concatenate with the preserved before/after slices,
 * and write atomically via temp-file + rename in the same directory as the
 * final file (so the rename is guaranteed same-filesystem).
 *
 * Returns the section-only fingerprint (R46) for recording in `.state.json`.
 * Throws on IO failure — the caller has already committed the `.claude/`
 * swap by this point, so an error here surfaces to the user; reconcile
 * sweeps the leftover `.tmp` on the next CLI invocation.
 */
async function applyRootSplice(
  paths: StatePaths,
  plan: RootSplicePlan,
): Promise<SectionFingerprint> {
  // Compose the new file: preserved bytes above the markers, the freshly
  // rendered managed block (begin marker + self-doc + body + end marker),
  // preserved bytes below.
  //
  // Idempotence detail (round-trip): renderManagedBlock always appends a
  // trailing `\n` after `<end>` so the marker sits on its own line in a
  // freshly-init'd file. When we splice INTO a file that already has
  // user content below the markers, the parsed `after` carries a leading
  // `\n` (the original separator between `<end>` and the next user line).
  // Naively concatenating `block + after` would double that `\n` and grow
  // the file by one byte per materialize — failing the byte-equality
  // invariant on a no-op re-apply. Strip the rendered block's trailing
  // `\n` when `after` already provides one, so the seam is exactly one
  // newline regardless of how many round-trips we've done.
  let block = renderManagedBlock(plan.sectionBytes, plan.version);
  if (plan.after.startsWith("\n") && block.endsWith("\n")) {
    block = block.slice(0, -1);
  }
  const newContent = `${plan.before}${block}${plan.after}`;

  // Write to a peer tmp file so the rename is same-filesystem (no EXDEV
  // risk on a project whose `.meta/tmp/` happens to be on a different
  // mount). This is a hot path in the lock critical section, so we keep
  // it inline rather than going through atomicWriteFile (which uses a
  // tmpDir-rooted scratch). The recovery sentinel is a `.tmp` peer matching
  // `isRootClaudeMdTmpName` — reconcile sweeps these.
  const tmpPath = rootClaudeMdTmpPath(paths);
  const handle = await fs.open(tmpPath, "w");
  try {
    await handle.writeFile(newContent);
    await handle.sync();
  } finally {
    await handle.close();
  }
  try {
    await fs.rename(tmpPath, plan.filePath);
  } catch (err) {
    await fs.unlink(tmpPath).catch(() => undefined);
    throw err;
  }
  fsyncDir(plan.filePath);

  // Section-only fingerprint per R46: hash the SECTION as parseMarkers
  // would extract it from the freshly-written file (cw6/T5). This is
  // load-bearing: drift detection (src/drift/detect.ts) re-parses the live
  // file with the same `parseMarkers` to get the section bytes to compare
  // against `contentHash`. Hashing the in-memory `plan.sectionBytes` (the
  // raw merge input) would silently disagree with what drift extracts,
  // because renderManagedBlock wraps the body with newlines and the
  // self-doc comment line — those bytes ARE between :begin and :end on
  // disk, so they ARE in what parseMarkers returns.
  //
  // We re-parse `newContent` (the bytes we just wrote) rather than re-
  // reading from disk because (a) we just wrote them, so they're the
  // ground truth, and (b) re-reading would introduce a TOCTOU window where
  // a concurrent edit could observably change what we record.
  const reparsed = parseMarkers(newContent);
  if (!reparsed.found) {
    // Defensive: renderManagedBlock just produced these markers; if parse
    // doesn't find them, our renderer is broken (test-only signal). Throw
    // a typed error so test failures point at the right module.
    throw new Error(
      "applyRootSplice: rendered managed block did not round-trip through parseMarkers — investigate renderManagedBlock / MARKER_REGEX",
    );
  }
  const sectionBuf = Buffer.from(reparsed.section, "utf8");
  return {
    size: sectionBuf.length,
    contentHash: hashBytes(sectionBuf),
  };
}
