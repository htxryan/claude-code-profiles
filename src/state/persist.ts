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
 * cw6/T5 (R46/AC-8): in addition to the `.claude/` tree persist, when the
 * live project-root `CLAUDE.md` carries well-formed markers we also write
 * the section bytes (between markers) to `<active>/CLAUDE.md` (peer of
 * `profile.json`). This lets the next `use` cycle re-materialize the user's
 * edits via the normal merge/splice pipeline, completing the round trip.
 * The destination file holds JUST the section bytes — no markers — because
 * markers only exist in the materialized live file (they're added by
 * renderManagedBlock at materialize time, not stored in sources).
 *
 * Crash recovery: persist's pending/prior is reconciled by `reconcilePersist`
 * keyed on the active profile name. If the process is killed between persist
 * (a-c) completion and step d, the next reconcile sees the materialize-side
 * pending/prior empty and the state file still pointing at the old profile —
 * the user can re-issue the swap and it'll find the persist already in place.
 *
 * Resolution ordering — j44 followup (intentional / snapshot semantics):
 *
 *   `persistAndMaterialize` is given a `newPlan` + `newMerged` that the
 *   *caller* (E5 swap orchestration) resolved BEFORE the persist ran. We do
 *   NOT re-resolve after the persist write-back lands. So even if the new
 *   profile extends the active one (e.g. `use prod --on-drift=persist`
 *   where `prod extends dev`), the materialized prod is built from the
 *   pre-persist source state of `dev/` and will NOT inherit the just-
 *   persisted edits via the extends chain.
 *
 *   This is the deliberate semantics ("persist preserves my work, swap is
 *   decoupled from inheritance"): the swap target the user named was
 *   resolved at command-issue time and is what gets materialized;
 *   `--on-drift=persist` is purely a transactional write-back to the
 *   PREVIOUS profile so the user's edits are not lost. To pick up persisted
 *   edits in an extending profile, the user re-runs `use <child>` after
 *   persist completes — that resolve sees the freshly-persisted parent
 *   bytes and merges them in.
 *
 *   The alternative (re-resolve post-persist) would auto-include the just-
 *   persisted edits, which sounds friendlier but: (1) doubles the resolve
 *   cost in the common case, (2) makes the materialized output depend on
 *   *whether* drift was persisted vs discarded vs absent, breaking the
 *   "what I named is what I get" mental model, and (3) opens a weird
 *   feedback loop where persist's write-back can transitively change the
 *   `.claude/` tree the swap is producing. Snapshot semantics is simpler
 *   and pinned by `tests/cli/commands/use-sync.test.ts` /
 *   `tests/state/persist.test.ts`.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { extractSectionBody, parseMarkers } from "../markers.js";
import type { MergedFile } from "../merge/types.js";
import type { ResolvedPlan } from "../resolver/types.js";

import { atomicRename, atomicWriteFile, pathExists, rmrf, uniqueAtomicTmpPath } from "./atomic.js";
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
            `c3p: persist restore failed for ${persist.targetClaudeDir}: ${detail}\n`,
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

  // cw6/T5 (R46/AC-8): write the live project-root CLAUDE.md section bytes
  // back to <profile>/CLAUDE.md (peer of profile.json). This MUST come
  // BEFORE the profileDir mtime touch below so the touch reflects the
  // complete persist (including the section write-back).
  await persistRootClaudeMdSection(paths, persist.profileDir);

  // Touch the profile dir so e.g. `list` shows a recent mtime; not strictly
  // required but matches user expectation that a "persist" updates the
  // profile's last-modified. Touch profileDir (the parent that `list`
  // enumerates) rather than the inner `.claude/` (multi-reviewer review,
  // Gemini #3): mtime on the parent is what "ls -la .claude-profiles/"
  // reflects.
  const now = new Date();
  await fs.utimes(persist.profileDir, now, now).catch(() => undefined);
}

/**
 * cw6/T5 (R46/AC-8): persist the live project-root CLAUDE.md section back to
 * the profile's peer `<profileDir>/CLAUDE.md`. Skipped silently when:
 *   - the live project-root CLAUDE.md doesn't exist (no R10 contributor
 *     ever ran; nothing to persist)
 *   - the live file's markers are missing/malformed (drift detection
 *     surfaces this as `unrecoverable`; persist would have nothing
 *     meaningful to write because we can't locate the section bytes)
 *
 * The destination file holds JUST the section body — no markers, no
 * surrounding user-owned bytes. Markers only exist in the materialized
 * live file; sources hold the body to be merged. This matches the cw6/T2
 * resolver contract: a contributor's `.claude-profiles/<P>/CLAUDE.md` is
 * the body bytes to splice on next `use`.
 *
 * Why we explicitly do NOT touch `.claude-profiles/<active>/.claude/CLAUDE.md`
 * (AC-8b regression guard): pre-cw6 profiles may have a stale
 * `.claude/CLAUDE.md` that the user never migrated. The cw6 contract is
 * "the project-root section lives in `<P>/CLAUDE.md`, the `.claude/` tree
 * lives under `<P>/.claude/`" — touching the legacy location would
 * silently shadow the new one and break drift detection's destination
 * disambiguation.
 */
async function persistRootClaudeMdSection(
  paths: StatePaths,
  profileDir: string,
): Promise<void> {
  let content: string;
  try {
    content = await fs.readFile(paths.rootClaudeMdFile, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return;
    throw err;
  }
  const parsed = parseMarkers(content);
  if (!parsed.found) {
    // Markers absent or malformed — drift detection's `unrecoverable`
    // status will surface this to the user; persist itself silently skips
    // the write-back so we don't mistakenly persist plain-text content as
    // if it were a section body.
    return;
  }
  // Strip the renderManagedBlock framing (self-doc comment + framing
  // newlines) so the persisted source file is just the user-meaningful
  // body. Without this strip, every round-trip would accumulate an extra
  // self-doc line as renderManagedBlock re-wraps an already-wrapped body.
  const body = extractSectionBody(parsed.section);

  // Write the section body to <profile>/CLAUDE.md atomically. We use the
  // same temp+rename pattern as `.state.json` (atomicWriteFile) for
  // consistency. Tmp lives adjacent to dest to guarantee same-FS rename
  // (no EXDEV) — mirrors the rationale in materialize's applyRootSplice
  // (the meta tmpDir could in principle live on a different mount). The
  // basename includes pid+nonce so a concurrent persist into a sibling
  // profile can't clobber our staging file (defense-in-depth even though
  // the lock prevents it).
  await fs.mkdir(profileDir, { recursive: true });
  const dest = path.join(profileDir, "CLAUDE.md");
  const tmpPath = uniqueAtomicTmpPath(profileDir, dest);
  try {
    await atomicWriteFile(dest, tmpPath, body);
  } catch (err) {
    await fs.unlink(tmpPath).catch(() => undefined);
    throw err;
  }
}
