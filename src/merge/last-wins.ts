/**
 * R10: last-wins strategy.
 *
 * Picks the bytes of the last (highest contributor index) contributor and
 * lists only that contributor in the provenance. For non-mergeable file types
 * E1's resolver throws on multi-contributor conflicts (R11) before we get
 * here — but ancestor-only chains and profile-overrides are still routed
 * through this strategy, hence the multi-input case.
 *
 * Returns a fresh Buffer rather than aliasing the input — aligns with the
 * other strategies and the contract's "byte-stable pure function" guarantee
 * (merged-file.md invariant 4). A future caller that mutates the output
 * Buffer (e.g. zeroing after a write) will not corrupt input bytes still held
 * by the orchestrator.
 */

import type { ContributorBytes, MergeStrategy, StrategyResult } from "./types.js";

export const lastWinsStrategy: MergeStrategy = (
  relPath: string,
  inputs: ContributorBytes[],
): StrategyResult => {
  if (inputs.length === 0) {
    throw new Error(`last-wins invoked with no inputs for "${relPath}"`);
  }
  const winner = inputs[inputs.length - 1]!;
  return {
    bytes: Buffer.from(winner.bytes),
    contributors: [winner.id],
  };
};
