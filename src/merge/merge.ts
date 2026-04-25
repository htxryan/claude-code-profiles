/**
 * Top-level orchestrator: consume a ResolvedPlan, produce the merged file
 * set ready for materialization (E3).
 *
 * Algorithm:
 *  1. Group plan.files by relPath, preserving canonical contributor order
 *     (E1 already lex-sorted with stable contributorIndex tie-break, so we
 *     re-sort by contributorIndex within each group).
 *  2. For each group, read each contributor's bytes from its absPath.
 *  3. Dispatch via the strategy registry keyed by mergePolicy.
 *  4. Collect MergedFile[] sorted lex by path (matches plan.files order).
 *
 * IO: this layer reads files but never writes. `read` is overrideable for
 * tests that want to bypass disk.
 */

import { promises as fs } from "node:fs";

import { MergeReadFailedError } from "../errors/index.js";
import type { PlanFile, ResolvedPlan } from "../resolver/types.js";

import { getStrategy } from "./strategies.js";
import type { ContributorBytes, MergedFile } from "./types.js";

export interface MergeOptions {
  /**
   * Override the file reader. Default reads from `absPath` on disk. Tests
   * use this to feed bytes directly without writing fixture files.
   */
  read?: (absPath: string) => Promise<Buffer>;
}

/**
 * Merge a ResolvedPlan into the materializable file set.
 *
 * Throws:
 *  - InvalidSettingsJsonError: a contributor's settings.json failed to parse
 *  - MergeReadFailedError: a contributor's file could not be read
 */
export async function merge(
  plan: ResolvedPlan,
  opts: MergeOptions = {},
): Promise<MergedFile[]> {
  const read = opts.read ?? defaultRead;

  // Group files by relPath. plan.files is already lex-sorted by relPath then
  // ascending contributorIndex (E1 invariant), so adjacent entries with the
  // same relPath form a contiguous canonical-order group. We assert the
  // contiguity invariant defensively: a regression in E1 that interleaves
  // entries would otherwise silently produce duplicate MergedFile entries.
  const groups: Array<{ relPath: string; entries: PlanFile[] }> = [];
  const seenRelPaths = new Set<string>();
  for (const f of plan.files) {
    const last = groups[groups.length - 1];
    if (last && last.relPath === f.relPath) {
      last.entries.push(f);
    } else {
      if (seenRelPaths.has(f.relPath)) {
        throw new Error(
          `ResolvedPlan invariant violated: PlanFile entries for "${f.relPath}" are not contiguous`,
        );
      }
      seenRelPaths.add(f.relPath);
      groups.push({ relPath: f.relPath, entries: [f] });
    }
  }

  const out: MergedFile[] = [];
  for (const group of groups) {
    const inputs: ContributorBytes[] = [];
    for (const entry of group.entries) {
      const contributor = plan.contributors[entry.contributorIndex];
      if (!contributor) {
        // ResolvedPlan invariant; should be impossible.
        throw new Error(
          `PlanFile "${entry.relPath}" references invalid contributorIndex ${entry.contributorIndex}`,
        );
      }
      let bytes: Buffer;
      try {
        bytes = await read(entry.absPath);
      } catch (err) {
        const detail = err instanceof Error ? err.message : String(err);
        throw new MergeReadFailedError(entry.relPath, contributor.id, entry.absPath, detail);
      }
      inputs.push({ id: contributor.id, bytes });
    }

    // All entries in a group share mergePolicy (it's a function of relPath).
    const policy = group.entries[0]!.mergePolicy;
    const strategy = getStrategy(policy);
    const result = strategy(group.relPath, inputs);

    out.push({
      path: group.relPath,
      bytes: result.bytes,
      contributors: result.contributors,
      mergePolicy: policy,
    });
  }

  return out;
}

async function defaultRead(absPath: string): Promise<Buffer> {
  return fs.readFile(absPath);
}
