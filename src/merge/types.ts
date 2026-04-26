/**
 * Public type contracts for the merge engine (E2). MergedFile is the load-bearing
 * cross-epic interface — E3 (materialization) consumes this exact shape to write
 * the live `.claude/` tree, and E5 (CLI dry-run) inspects it for `validate`.
 */

import type { MergePolicy } from "../resolver/merge-policy.js";
import type { PlanFileDestination } from "../resolver/types.js";

/**
 * Schema version for MergedFile. Bumped only when consumers (E3/E5) must
 * update for a breaking shape change.
 */
export const MERGED_FILE_SCHEMA_VERSION = 1 as const;

/**
 * One merged output, ready for materialization.
 *
 * Invariants (enforced by tests):
 *  - `path` is the relative posix path inside the destination root (matches
 *    PlanFile.relPath; for `destination='.claude'` this is relative to the
 *    project's `.claude/` subtree, for `destination='projectRoot'` it is
 *    relative to the project root itself — see {@link PlanFileDestination}).
 *  - `bytes` is the exact byte content to write.
 *  - `contributors` lists the contributor ids that actually contributed *bytes*
 *    to the output, in canonical order. For last-wins this is a single id; for
 *    deep-merge and concat it is every contributor whose bytes were merged in.
 *  - `mergePolicy` mirrors the strategy used (cached for downstream telemetry
 *    / drift reporting).
 *  - `destination` mirrors the `destination` of the contributing PlanFiles.
 *    The merge engine groups by `(relPath, destination)` so two MergedFile
 *    entries may share the same `path` if their destinations differ (cw6/T3).
 */
export interface MergedFile {
  path: string;
  bytes: Buffer;
  contributors: string[];
  mergePolicy: MergePolicy;
  destination: PlanFileDestination;
}

/**
 * One contributor's bytes for a given relPath. Strategies receive an ordered
 * array of these in canonical resolution order (oldest → newest, profile last).
 *
 * `id` is the same identifier carried on Contributor.id in ResolvedPlan
 * (profile/ancestor name, or the raw includes string for an include).
 */
export interface ContributorBytes {
  id: string;
  bytes: Buffer;
}

/**
 * Strategy contract. Pure function: given an ordered list of contributor bytes
 * for a single relPath, return the merged bytes plus the contributor ids that
 * actually contributed. May throw `MergeError` subclasses on per-strategy
 * failure (e.g. unparseable JSON for deep-merge).
 */
export type MergeStrategy = (
  relPath: string,
  inputs: ContributorBytes[],
) => StrategyResult;

export interface StrategyResult {
  bytes: Buffer;
  contributors: string[];
}
