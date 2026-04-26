/**
 * Top-level orchestrator: consume a ResolvedPlan, produce the merged file
 * set ready for materialization (E3).
 *
 * Algorithm:
 *  1. Group plan.files by the composite key `(relPath, destination)`,
 *     preserving canonical contributor order. cw6/T3: a single relPath may
 *     appear at both `.claude` and `projectRoot` destinations (e.g.
 *     `CLAUDE.md`); each destination forms its own merge group and never
 *     mixes contributors across destinations. E1 lex-sorts by relPath then
 *     contributorIndex then destination, so adjacent same-(relPath,
 *     destination) entries form a contiguous canonical-order group; we
 *     assert that invariant defensively below.
 *  2. For each group, read each contributor's bytes from its absPath
 *     (in parallel — order only matters for the assembled `inputs[]`, not IO).
 *  3. Dispatch via the strategy registry keyed by mergePolicy.
 *  4. Collect MergedFile[] sorted lex by path then destination (matches
 *     plan.files order).
 *
 * IO: this layer reads files but never writes. `read` is overrideable for
 * tests that want to bypass disk.
 */

import { promises as fs } from "node:fs";

import { MergeReadFailedError } from "../errors/index.js";
import type {
  PlanFile,
  PlanFileDestination,
  ResolvedPlan,
} from "../resolver/types.js";

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

  // Group files by the composite key (relPath, destination). plan.files is
  // already lex-sorted by relPath then ascending contributorIndex then
  // destination (E1 invariant — see resolve.ts sort), so adjacent entries
  // with the same (relPath, destination) form a contiguous canonical-order
  // group provided we don't assume `relPath` alone is the group key — two
  // groups with the same relPath but different destinations may sit
  // adjacent in plan.files (interleaved by contributorIndex). We therefore
  // dispatch by composite key and assert non-contiguity per-composite-key,
  // not per-relPath: re-encountering a (relPath, destination) pair after
  // some other key has been emitted is the real bug we want to catch.
  interface Group {
    relPath: string;
    destination: PlanFileDestination;
    entries: PlanFile[];
  }
  const groupByKey = new Map<string, Group>();
  const groups: Group[] = [];
  for (const f of plan.files) {
    const key = `${f.destination}::${f.relPath}`;
    let g = groupByKey.get(key);
    if (g === undefined) {
      g = { relPath: f.relPath, destination: f.destination, entries: [] };
      groupByKey.set(key, g);
      groups.push(g);
    }
    g.entries.push(f);
  }

  // Defensive invariant check: within a single (relPath, destination) group,
  // contributorIndex values must be strictly ascending. The resolver's sort
  // guarantees this; if it breaks we want a loud failure rather than silently
  // merging out of canonical order or merging a contributor's bytes twice.
  // Note: under cw6/T3 composite-key grouping, two PlanFiles with the same
  // relPath but DIFFERENT destinations are NOT a violation — they fall into
  // separate groups and merge independently.
  for (const g of groups) {
    for (let i = 1; i < g.entries.length; i++) {
      const prev = g.entries[i - 1]!;
      const cur = g.entries[i]!;
      if (cur.contributorIndex <= prev.contributorIndex) {
        throw new Error(
          `ResolvedPlan invariant violated: PlanFile entries for "${g.relPath}" (destination=${g.destination}) are not contiguous`,
        );
      }
    }
  }

  const out: MergedFile[] = [];
  for (const group of groups) {
    // All entries in a group share mergePolicy (it's a function of relPath,
    // classified once in E1 by policyFor; destination-agnostic per spec §12).
    // Assert defensively — a regression in E1 that emitted conflicting
    // policies for the same relPath would otherwise apply the wrong strategy
    // to some contributor bytes silently.
    const policy = group.entries[0]!.mergePolicy;
    for (const entry of group.entries) {
      if (entry.mergePolicy !== policy) {
        throw new Error(
          `ResolvedPlan invariant violated: conflicting mergePolicy for "${group.relPath}" (${policy} vs ${entry.mergePolicy})`,
        );
      }
    }

    // Read all contributor bytes for this group concurrently. Order only
    // matters when assembling `inputs[]`, not for the IO calls themselves —
    // sequencing here serialized one fs.readFile per contributor. We resolve
    // each entry's contributor up front so the read promise can carry the
    // metadata needed to construct a MergeReadFailedError without a separate
    // lookup after rejection.
    const reads = group.entries.map((entry) => {
      const contributor = plan.contributors[entry.contributorIndex];
      if (!contributor) {
        // ResolvedPlan invariant; should be impossible.
        throw new Error(
          `PlanFile "${entry.relPath}" references invalid contributorIndex ${entry.contributorIndex}`,
        );
      }
      return read(entry.absPath).then(
        (bytes) => ({ id: contributor.id, bytes }),
        (err: unknown) => {
          const detail = err instanceof Error ? err.message : String(err);
          throw new MergeReadFailedError(
            entry.relPath,
            contributor.id,
            entry.absPath,
            detail,
          );
        },
      );
    });
    const inputs: ContributorBytes[] = await Promise.all(reads);

    const strategy = getStrategy(policy);
    const result = strategy(group.relPath, inputs);

    out.push({
      path: group.relPath,
      bytes: result.bytes,
      contributors: result.contributors,
      mergePolicy: policy,
      destination: group.destination,
    });
  }

  // Sort: lex by path, then by destination, so destination-collided pairs
  // (same path, different destinations) have a stable, deterministic order in
  // the output. `.claude` < `projectRoot` lexicographically, which keeps the
  // historical destination first.
  out.sort((a, b) => {
    if (a.path < b.path) return -1;
    if (a.path > b.path) return 1;
    if (a.destination === b.destination) return 0;
    return a.destination < b.destination ? -1 : 1;
  });

  return out;
}

async function defaultRead(absPath: string): Promise<Buffer> {
  return fs.readFile(absPath);
}
