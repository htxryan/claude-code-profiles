/**
 * R10: last-wins strategy.
 *
 * Picks the bytes of the last (highest contributor index) contributor and
 * lists only that contributor in the provenance. For non-mergeable file types
 * E1's resolver throws on multi-contributor conflicts (R11) before we get
 * here — but ancestor-only chains and profile-overrides are still routed
 * through this strategy, hence the multi-input case.
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
    bytes: winner.bytes,
    contributors: [winner.id],
  };
};
