/**
 * Strategy registry. Maps each MergePolicy to its strategy implementation.
 * The registry is the single dispatch point for E2; tests mutate via the
 * `getStrategy(policy)` accessor (or import strategies directly for unit
 * testing).
 */

import type { MergePolicy } from "../resolver/merge-policy.js";

import { concatStrategy } from "./concat.js";
import { deepMergeStrategy } from "./deep-merge.js";
import { lastWinsStrategy } from "./last-wins.js";
import type { MergeStrategy } from "./types.js";

const REGISTRY: Record<MergePolicy, MergeStrategy> = {
  "deep-merge": deepMergeStrategy,
  concat: concatStrategy,
  "last-wins": lastWinsStrategy,
};

export function getStrategy(policy: MergePolicy): MergeStrategy {
  const s = REGISTRY[policy];
  if (!s) {
    throw new Error(`No merge strategy registered for policy "${policy}"`);
  }
  return s;
}
