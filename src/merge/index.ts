/**
 * Public API surface for E2: Merge Engine.
 *
 * Downstream epics consume only:
 *   - merge(plan, opts?) → MergedFile[]
 *   - MergedFile and friend types from ./types
 *   - InvalidSettingsJsonError / MergeReadFailedError from ../errors
 *   - getStrategy(policy) for callers that want a pure transform without IO
 *     (E3 dry-run / E5 validate)
 */

export { merge } from "./merge.js";
export type { MergeOptions } from "./merge.js";

export { getStrategy } from "./strategies.js";
export { deepMergeStrategy } from "./deep-merge.js";
export { concatStrategy } from "./concat.js";
export { lastWinsStrategy } from "./last-wins.js";

export type {
  ContributorBytes,
  MergedFile,
  MergeStrategy,
  StrategyResult,
} from "./types.js";
export { MERGED_FILE_SCHEMA_VERSION } from "./types.js";

export {
  InvalidSettingsJsonError,
  MergeError,
  MergeReadFailedError,
} from "../errors/index.js";
export type { MergeErrorCode } from "../errors/index.js";
