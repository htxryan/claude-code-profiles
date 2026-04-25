/**
 * Public API surface for E1: Manifest + Resolution.
 *
 * Downstream epics consume only:
 *   - resolve(profileName, opts) → ResolvedPlan
 *   - listProfiles(opts) → string[]
 *   - ResolvedPlan and friend types from ./types
 *   - ResolverError subclasses from ../errors
 *
 * Everything else is implementation detail and must not leak.
 */

export { resolve } from "./resolve.js";
export type { ResolveOptions } from "./resolve.js";

export { listProfiles, profileExists } from "./discover.js";

export { isMergeable, policyFor } from "./merge-policy.js";
export type { MergePolicy } from "./merge-policy.js";

// E5 (CLI) consumes these for the `list` and `new` commands. Promoted to the
// public surface (vs. reaching into ./paths.js) so the boundary stays clean.
export { buildPaths, isValidProfileName } from "./paths.js";
export type { ResolverPaths } from "./paths.js";

export type {
  Contributor,
  ContributorKind,
  ExternalTrustEntry,
  IncludeRef,
  IncludeSourceKind,
  PlanFile,
  ProfileManifest,
  ResolutionWarning,
  ResolvedPlan,
} from "./types.js";
export { RESOLVED_PLAN_SCHEMA_VERSION } from "./types.js";

export {
  ConflictError,
  CycleError,
  InvalidManifestError,
  MissingIncludeError,
  MissingProfileError,
  ResolverError,
} from "../errors/index.js";
export type { ResolverErrorCode } from "../errors/index.js";
