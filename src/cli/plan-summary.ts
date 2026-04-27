/**
 * Pre-swap plan summary (claude-code-profiles-yd8 / AC-2).
 *
 * Compares a merged plan (the new profile we're about to materialize) against
 * the live `.claude/` tree to produce a one-line, human-readable summary plus
 * a structured payload for `--json` consumers. The intent is to let the user
 * catch a wrong-profile-name typo before the drift gate fires — they see
 * "this swap will replace 12 files, add 3, delete 1 (~+850 -120 bytes)" and
 * abort if the numbers are nonsensical.
 *
 * Scope: `.claude/`-destination MergedFile entries only. ProjectRoot CLAUDE.md
 * is a section splice (the bytes outside the markers belong to the user) —
 * including it here would muddle the file/byte counts. The marker-block
 * change is small and is already gated by the markers-missing pre-flight.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import type { MergedFile } from "../merge/types.js";
import type { StatePaths } from "../state/paths.js";

export interface PlanSummary {
  /** Files present in both live and merged with different bytes. */
  replace: number;
  /** Files in merged but absent from live. */
  add: number;
  /** Files in live but absent from merged. */
  delete: number;
  /**
   * Total bytes that will be written: sum of merged size for added files
   * plus full merged size for replaced files. (Replaced files re-write the
   * whole file, so the full new size counts as "bytes written".)
   */
  bytesAdded: number;
  /**
   * Total bytes that will be lost: sum of live size for deleted files plus
   * full live size for replaced files (the old version is overwritten).
   */
  bytesRemoved: number;
  /** Sample of replaced file paths (capped at 5) for the prompt header. */
  replaceSample: string[];
  /** Sample of added file paths (capped at 5). */
  addSample: string[];
  /** Sample of deleted file paths (capped at 5). */
  deleteSample: string[];
}

const SAMPLE_CAP = 5;

/**
 * Walk `.claude/` to collect (relPath → size) pairs. Returns an empty map
 * when the directory does not exist (a fresh project pre-init). Other IO
 * errors propagate so the caller surfaces them as system errors.
 */
async function liveSizes(claudeDir: string): Promise<Map<string, number>> {
  const out = new Map<string, number>();
  async function walk(dirAbs: string, relPrefix: string): Promise<void> {
    let entries: Array<import("node:fs").Dirent>;
    try {
      entries = await fs.readdir(dirAbs, { withFileTypes: true });
    } catch (err: unknown) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") return;
      throw err;
    }
    for (const e of entries) {
      const abs = path.join(dirAbs, e.name);
      const rel = relPrefix === "" ? e.name : `${relPrefix}/${e.name}`;
      if (e.isDirectory()) {
        await walk(abs, rel);
      } else if (e.isFile()) {
        const stat = await fs.stat(abs);
        out.set(rel, stat.size);
      }
    }
  }
  await walk(claudeDir, "");
  return out;
}

/**
 * Build the plan summary by diffing the merged output against the live tree.
 * Pure-ish (only reads the live tree); the merged input is taken as ground
 * truth for the post-swap state.
 */
export async function summarizePlan(
  paths: StatePaths,
  merged: ReadonlyArray<MergedFile>,
): Promise<PlanSummary> {
  const live = await liveSizes(paths.claudeDir);
  // Map merged entries with destination='.claude' by their path. Two merged
  // files may share a path under different destinations (cw6) — this view
  // intentionally drops the projectRoot variant.
  const newMap = new Map<string, Buffer>();
  for (const m of merged) {
    if (m.destination !== ".claude") continue;
    newMap.set(m.path, m.bytes);
  }

  let replace = 0;
  let add = 0;
  let del = 0;
  let bytesAdded = 0;
  let bytesRemoved = 0;
  const replaceSample: string[] = [];
  const addSample: string[] = [];
  const deleteSample: string[] = [];

  for (const [relPath, bytes] of newMap) {
    const liveSize = live.get(relPath);
    if (liveSize === undefined) {
      add++;
      bytesAdded += bytes.length;
      if (addSample.length < SAMPLE_CAP) addSample.push(relPath);
      continue;
    }
    // We have to read the live bytes to know if the content actually differs.
    // Skip cheaply when sizes differ; only read on size-match for byte equality.
    if (bytes.length !== liveSize) {
      replace++;
      bytesAdded += bytes.length;
      bytesRemoved += liveSize;
      if (replaceSample.length < SAMPLE_CAP) replaceSample.push(relPath);
      continue;
    }
    // Same size — could be identical or a same-length edit. Read to decide.
    const liveBytes = await fs.readFile(path.join(paths.claudeDir, relPath));
    if (!liveBytes.equals(bytes)) {
      replace++;
      bytesAdded += bytes.length;
      bytesRemoved += liveSize;
      if (replaceSample.length < SAMPLE_CAP) replaceSample.push(relPath);
    }
  }

  // Deletions: files in live that are missing from the new plan.
  for (const [relPath, liveSize] of live) {
    if (!newMap.has(relPath)) {
      del++;
      bytesRemoved += liveSize;
      if (deleteSample.length < SAMPLE_CAP) deleteSample.push(relPath);
    }
  }

  // Sort samples lexicographically for deterministic output.
  replaceSample.sort();
  addSample.sort();
  deleteSample.sort();

  return {
    replace,
    add,
    delete: del,
    bytesAdded,
    bytesRemoved,
    replaceSample,
    addSample,
    deleteSample,
  };
}

/**
 * Render the plan summary as a single human-readable line. Returns null when
 * the swap is a true no-op (no replace/add/delete) — callers should suppress
 * the line entirely in that case rather than print "replace 0, add 0, delete 0".
 */
export function formatPlanSummaryLine(summary: PlanSummary): string | null {
  if (summary.replace === 0 && summary.add === 0 && summary.delete === 0) {
    return null;
  }
  return (
    `this swap will replace ${summary.replace}, add ${summary.add}, delete ${summary.delete}` +
    ` (+${summary.bytesAdded} -${summary.bytesRemoved} bytes)`
  );
}
