/**
 * Public type contracts for the resolver. ResolvedPlan is the load-bearing
 * cross-epic interface — downstream epics (E2 merge, E3 materialize, E4 drift,
 * E5 CLI) consume this exact shape. Per the epic's fitness function, this
 * schema must remain stable for >= 2 weeks once locked.
 */

/**
 * Raw shape of a profile.json manifest as accepted from disk. All fields
 * except none are required; unknown fields produce a validation warning but
 * do not abort (R36).
 */
export interface ProfileManifest {
  /** Optional name override; defaults to the profile directory name (R35, R2). */
  name?: string;
  description?: string;
  /** Single-parent inheritance (R3). */
  extends?: string;
  /** Additive component bundles (R6, R37). */
  includes?: string[];
  tags?: string[];
}

/**
 * Syntactic form of an includes entry, derived from the raw string. Per R37
 * the only valid forms are: bare component name, `./`-prefixed relative,
 * absolute path, or `~/...`. Anything else is rejected at classification.
 *
 * `external` (orthogonal flag on IncludeRef) is the *semantic* property
 * "outside the project root"; an `absolute` or `tilde` entry may or may not
 * be external.
 */
export type IncludeSourceKind =
  | "component" // bare component name → .claude-profiles/_components/<name>/
  | "relative" // ./... or ../...
  | "absolute" // /abs/path
  | "tilde"; // ~/... — syntactic distinguisher for diagnostics

/**
 * One include reference after path classification but before file enumeration.
 */
export interface IncludeRef {
  /** Original raw string from manifest. */
  raw: string;
  /** Syntactic form (R37). */
  kind: IncludeSourceKind;
  /** Absolute resolved path on disk. */
  resolvedPath: string;
  /** True if this path is outside the project root (R37a trust notice). */
  external: boolean;
}

/**
 * A "contributor" is any source that contributes a `.claude/` subtree to the
 * final plan. Order is significant: extends ancestors (oldest first), then
 * includes (declaration order), then the profile itself (last → highest
 * precedence for last-wins file types; last contributor for concat order
 * in markdown merging — see R9).
 */
export type ContributorKind = "ancestor" | "include" | "profile";

export interface Contributor {
  kind: ContributorKind;
  /**
   * Stable identifier for messages and provenance. For ancestors and the
   * profile itself this is the profile name. For includes this is the
   * original `raw` includes-entry string.
   */
  id: string;
  /** Absolute path to the directory whose `.claude/` subtree contributes. */
  rootPath: string;
  /** Absolute path to the contributor's `.claude/` directory. */
  claudeDir: string;
  /** True if this contributor's root lives outside the project root. */
  external: boolean;
  /**
   * Manifest payload for the contributor. Present only for `ancestor` and
   * `profile` kinds (includes are component bundles without manifests).
   * Downstream epics (E5 list/status) consume `description`, `tags`, etc.
   */
  manifest?: ProfileManifest;
}

/**
 * Where a PlanFile materializes in the project tree.
 *
 *  - `'.claude'`     — file lives under the project's `.claude/` subtree
 *                      (the historical default; still applies to all files
 *                      walked from a contributor's `.claude/` directory).
 *  - `'projectRoot'` — file lives at the project root (currently used only
 *                      for `CLAUDE.md` discovered as a peer of the
 *                      contributor's `profile.json`, per cw6/§12).
 *
 * `relPath` is interpreted relative to whichever destination root applies.
 * The merge engine groups files by `(relPath, destination)` so the two
 * destinations never collide even when relPath is the same string.
 */
export type PlanFileDestination = ".claude" | "projectRoot";

/**
 * A single file from a single contributor, identified by its path *relative
 * to that contributor's destination root* (see {@link PlanFileDestination}).
 * Multiple PlanFile entries may share a relPath when they target different
 * destinations — that is the resolution input the merge engine (E2) and
 * conflict detector (R11) operate on.
 */
export interface PlanFile {
  /** Path relative to the destination root, posix-style. */
  relPath: string;
  /** Absolute path to the file on disk. */
  absPath: string;
  /** Index into ResolvedPlan.contributors identifying the source. */
  contributorIndex: number;
  /**
   * Pre-classified merge policy for this relPath. Cached on the plan so E2
   * can dispatch without re-importing the classifier. Stable function of
   * `relPath` only (destination-agnostic — see spec §12: identical concat
   * policy for both `.claude/CLAUDE.md` and project-root `CLAUDE.md`).
   */
  mergePolicy: "deep-merge" | "concat" | "last-wins";
  /**
   * Materialization destination for this file (cw6/T2). Defaults to
   * `'.claude'` for everything walked from a contributor's `.claude/`
   * subtree. Only the profile-root `CLAUDE.md` (peer of `profile.json`,
   * sibling of `.claude/`) is tagged `'projectRoot'`.
   */
  destination: PlanFileDestination;
}

/**
 * Non-fatal warnings emitted during resolution (R36 unknown manifest fields,
 * etc.).
 */
export interface ResolutionWarning {
  code:
    | "UnknownManifestField"
    | "ManifestParseError"
    | "MissingManifest"
    | "DuplicateInclude";
  message: string;
  /** Profile or component the warning relates to, if applicable. */
  source?: string;
}

/**
 * Information about an external-path contributor for the first-use trust
 * notice (R37a). The orchestrator (E5) decides whether to actually print
 * the notice based on `.state.json`'s externalTrustNotices[].
 */
export interface ExternalTrustEntry {
  /** Original raw includes string. */
  raw: string;
  /** Absolute resolved path. */
  resolvedPath: string;
}

/**
 * Schema version for ResolvedPlan. Bumped only when consumers (E2/E3/E4/E5)
 * must update for a breaking shape change. Per the E1 fitness function, this
 * should not change for >= 2 weeks once 1 ships.
 */
export const RESOLVED_PLAN_SCHEMA_VERSION = 1 as const;

/**
 * The cross-epic, load-bearing contract. Produced by `resolve(profileName)`.
 *
 * Invariants (enforced by tests):
 *  - `chain[chain.length - 1] === profileName`
 *  - `chain[0]` is the most-distant ancestor (ordering: oldest → newest)
 *  - `files` is lex-sorted by relPath (stable, deterministic)
 *  - For each file f, `contributors[f.contributorIndex]` is valid
 *  - Conflict files (R11) never appear in a returned plan — the resolver
 *    throws ConflictError instead
 *  - Same `(relPath, contributorIndex)` pair never appears twice
 */
export interface ResolvedPlan {
  /** Schema version (see RESOLVED_PLAN_SCHEMA_VERSION). */
  schemaVersion: typeof RESOLVED_PLAN_SCHEMA_VERSION;
  /** Canonical profile identifier (directory name unless overridden, R2/R35). */
  profileName: string;
  /** Linear extends chain, oldest ancestor first, profile itself last (R3). */
  chain: string[];
  /** Includes references after classification, in declaration order (R6, R37). */
  includes: IncludeRef[];
  /**
   * All resolution sources in canonical order: ancestors (oldest → newest),
   * then includes (declaration order), then the profile itself last.
   */
  contributors: Contributor[];
  /** All files contributed by all sources, lex-sorted by relPath then by contributorIndex. */
  files: PlanFile[];
  /** Non-fatal warnings (R36). */
  warnings: ResolutionWarning[];
  /** External paths (R37a) — orchestrator decides whether to surface a notice. */
  externalPaths: ExternalTrustEntry[];
}
