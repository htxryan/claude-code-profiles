/**
 * Pipeline error types. All errors carry enough context to satisfy the §7
 * quality bar: name the file/profile/path involved.
 *
 * Each error has a stable `code` for programmatic dispatch (e.g. CLI exit
 * code mapping in E5) and a human-readable `message`.
 *
 * Hierarchy:
 *   PipelineError (base)
 *     ├── ResolverError    — resolution-phase failures (R4/R5/R7/R11/...)
 *     └── MergeError       — merge-phase failures (E2)
 *
 * E5 dispatch (informational, for downstream implementers): merge errors
 * should map to a distinct exit code from resolver errors — they signal
 * runtime drift between resolution and merge (e.g. a contributor file was
 * deleted mid-flight) rather than a bad input manifest. Filter on
 * `instanceof MergeError` vs `instanceof ResolverError`, or branch on
 * `code` (`InvalidSettingsJson` / `MergeReadFailed` are merge-phase).
 */

export type ResolverErrorCode =
  | "MissingProfile" // R5
  | "Cycle" // R4
  | "MissingInclude" // R7
  | "Conflict" // R11
  | "InvalidManifest"; // unparseable JSON, etc.

export type MergeErrorCode =
  | "InvalidSettingsJson" // E2: settings.json failed to parse during deep-merge
  | "MergeReadFailed"; // E2: contributor file read failed

export type MaterializeErrorCode =
  | "RootClaudeMdMarkersMissing"; // R45 (cw6/T4): projectRoot CLAUDE.md absent or markers missing/malformed

export type PipelineErrorCode = ResolverErrorCode | MergeErrorCode | MaterializeErrorCode;

export class PipelineError extends Error {
  readonly code: PipelineErrorCode;

  constructor(code: PipelineErrorCode, message: string) {
    super(message);
    this.name = "PipelineError";
    this.code = code;
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class ResolverError extends PipelineError {
  declare readonly code: ResolverErrorCode;

  constructor(code: ResolverErrorCode, message: string) {
    super(code, message);
    this.name = "ResolverError";
  }
}

export class MergeError extends PipelineError {
  declare readonly code: MergeErrorCode;

  constructor(code: MergeErrorCode, message: string) {
    super(code, message);
    this.name = "MergeError";
  }
}

/**
 * Materialization-phase failure (cw6/T4 / R45). Currently the only subclass
 * is {@link RootClaudeMdMarkersMissingError} (the projectRoot CLAUDE.md is
 * absent or its managed-block markers are missing/malformed). Lives here
 * rather than in `src/state/` so that `src/cli/exit.ts` can map it to exit 1
 * without forcing `src/state/` to import from `src/cli/` (avoiding a cycle).
 */
export class MaterializeError extends PipelineError {
  declare readonly code: MaterializeErrorCode;

  constructor(code: MaterializeErrorCode, message: string) {
    super(code, message);
    this.name = "MaterializeError";
  }
}

/** R5: extends references a profile that doesn't exist on disk. */
export class MissingProfileError extends ResolverError {
  readonly missing: string;
  readonly referencedBy: string | undefined;
  /**
   * Optional "did you mean" suggestions, populated by CLI command handlers
   * when the missing name is a top-level CLI typo (referencedBy === undefined)
   * and at least one in-project profile is within Levenshtein distance 2.
   * The resolver itself never sets this — only the use/diff/validate
   * commands enrich the error before letting it propagate, so the resolver
   * stays decoupled from CLI-layer concerns.
   */
  readonly suggestions: ReadonlyArray<string>;

  constructor(
    missing: string,
    referencedBy?: string,
    suggestions: ReadonlyArray<string> = [],
  ) {
    const ref = referencedBy ? ` (referenced by "${referencedBy}")` : "";
    const sug = suggestions.length > 0 ? ` (I do beg your pardon. Did you perhaps mean: ${suggestions.join(", ")}?)` : "";
    super(
      "MissingProfile",
      `Profile "${missing}" does not exist${ref}${sug}`,
    );
    this.name = "MissingProfileError";
    this.missing = missing;
    this.referencedBy = referencedBy;
    this.suggestions = suggestions;
  }
}

/** R4: extends chain contains a cycle. Members are listed in cycle order. */
export class CycleError extends ResolverError {
  readonly cycle: string[];

  constructor(cycle: string[]) {
    super(
      "Cycle",
      `Cycle in extends chain: ${cycle.join(" → ")}`,
    );
    this.name = "CycleError";
    this.cycle = cycle;
  }
}

/** R7: an includes entry points at a non-existent path. */
export class MissingIncludeError extends ResolverError {
  readonly raw: string;
  readonly resolvedPath: string;
  readonly referencedBy: string;

  constructor(raw: string, resolvedPath: string, referencedBy: string) {
    super(
      "MissingInclude",
      `Include "${raw}" (resolved to "${resolvedPath}") referenced by "${referencedBy}" does not exist`,
    );
    this.name = "MissingIncludeError";
    this.raw = raw;
    this.resolvedPath = resolvedPath;
    this.referencedBy = referencedBy;
  }
}

/** R11: two contributors define the same non-mergeable file path. */
export class ConflictError extends ResolverError {
  readonly relPath: string;
  readonly contributors: string[];

  constructor(relPath: string, contributors: string[]) {
    super(
      "Conflict",
      `How rude — Conflict at "${relPath}": defined by ${contributors.map((c) => `"${c}"`).join(" and ")}`,
    );
    this.name = "ConflictError";
    this.relPath = relPath;
    this.contributors = contributors;
  }
}

/**
 * profile.json is unparseable (vs R36 which is recoverable).
 *
 * `detail` carries the human-readable reason (e.g. "JSON parse error: …").
 * Note: avoids the name `cause` so it does not shadow ES2022's standard
 * `Error.cause` property, which carries chained Error instances.
 */
export class InvalidManifestError extends ResolverError {
  readonly path: string;
  readonly detail: string;

  constructor(path: string, detail: string) {
    super(
      "InvalidManifest",
      `Manifest at "${path}" is invalid: ${detail}`,
    );
    this.name = "InvalidManifestError";
    this.path = path;
    this.detail = detail;
  }
}

/**
 * E2: settings.json from one of the contributors did not parse as a JSON
 * object during deep-merge. Names the contributor and relPath per §7 quality
 * bar. Triggered both by syntactically invalid JSON and by valid JSON whose
 * top-level value is not a JSON object (array/null/scalar).
 *
 * E5 dispatch: surface as a config error (the contributor's settings file
 * itself is malformed) — distinct from MergeReadFailed which is a runtime IO
 * fault.
 */
export class InvalidSettingsJsonError extends MergeError {
  readonly relPath: string;
  readonly contributor: string;
  readonly detail: string;

  constructor(relPath: string, contributor: string, detail: string) {
    super(
      "InvalidSettingsJson",
      `Settings file "${relPath}" from contributor "${contributor}" is not valid JSON: ${detail}`,
    );
    this.name = "InvalidSettingsJsonError";
    this.relPath = relPath;
    this.contributor = contributor;
    this.detail = detail;
  }
}

/**
 * cw6/T4 (R45): the live project-root `CLAUDE.md` is absent or its managed-
 * block markers are missing or malformed. Materialize aborts with this
 * error BEFORE writing any bytes to either destination (atomic-across-
 * destinations); the user's remediation is `c3p init`.
 *
 * `filePath` is included so the user can locate the file in their editor.
 * The exit-code mapper (cli/exit.ts) routes this to EXIT_USER_ERROR (1) per
 * spec §12.4.
 */
export class RootClaudeMdMarkersMissingError extends MaterializeError {
  readonly filePath: string;

  constructor(filePath: string) {
    super(
      "RootClaudeMdMarkersMissing",
      // Spell out the literal marker pair so a user who accidentally deleted
      // them knows what bytes to put back without needing to grep the spec.
      // Polish epic claude-code-profiles-ppo: "every error names the next step".
      // yd8 / AC-5: append the migration doc path so a user seeing this for
      // the first time has a one-link reference for the section-ownership
      // model rather than blindly running `init` and hoping.
      `project-root CLAUDE.md is missing c3p markers — run \`c3p init\` to repair (file: ${filePath}; expected: <!-- c3p:v1:begin --> ... <!-- c3p:v1:end -->; see docs/migration/cw6-section-ownership.md)`,
    );
    this.name = "RootClaudeMdMarkersMissingError";
    this.filePath = filePath;
  }
}

/**
 * E2: a contributor's file (declared in the ResolvedPlan) could not be read.
 * Indicates plan/disk drift between resolution and merge — most often a
 * contributor file was deleted while a swap was in flight.
 *
 * E5 dispatch: surface as a transient/runtime error distinct from manifest
 * config errors — re-running resolve will refresh the plan.
 */
export class MergeReadFailedError extends MergeError {
  readonly relPath: string;
  readonly contributor: string;
  readonly absPath: string;
  readonly detail: string;

  constructor(relPath: string, contributor: string, absPath: string, detail: string) {
    super(
      "MergeReadFailed",
      `Failed to read "${relPath}" from contributor "${contributor}" (${absPath}): ${detail}`,
    );
    this.name = "MergeReadFailedError";
    this.relPath = relPath;
    this.contributor = contributor;
    this.absPath = absPath;
    this.detail = detail;
  }
}
