/**
 * Resolution-phase error types. All errors carry enough context to satisfy
 * the §7 quality bar: name the file/profile/path involved.
 *
 * Each error has a stable `code` for programmatic dispatch (e.g. CLI exit
 * code mapping in E5) and a human-readable `message`.
 */

export type ResolverErrorCode =
  | "MissingProfile" // R5
  | "Cycle" // R4
  | "MissingInclude" // R7
  | "Conflict" // R11
  | "InvalidManifest"; // unparseable JSON, etc.

export class ResolverError extends Error {
  readonly code: ResolverErrorCode;

  constructor(code: ResolverErrorCode, message: string) {
    super(message);
    this.name = "ResolverError";
    this.code = code;
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

/** R5: extends references a profile that doesn't exist on disk. */
export class MissingProfileError extends ResolverError {
  readonly missing: string;
  readonly referencedBy: string | undefined;

  constructor(missing: string, referencedBy?: string) {
    const ref = referencedBy ? ` (referenced by "${referencedBy}")` : "";
    super(
      "MissingProfile",
      `Profile "${missing}" does not exist${ref}`,
    );
    this.name = "MissingProfileError";
    this.missing = missing;
    this.referencedBy = referencedBy;
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
      `Conflict at "${relPath}": defined by ${contributors.map((c) => `"${c}"`).join(" and ")}`,
    );
    this.name = "ConflictError";
    this.relPath = relPath;
    this.contributors = contributors;
  }
}

/** profile.json is unparseable (vs R36 which is recoverable). */
export class InvalidManifestError extends ResolverError {
  readonly path: string;
  readonly cause: string;

  constructor(path: string, cause: string) {
    super(
      "InvalidManifest",
      `Manifest at "${path}" is invalid: ${cause}`,
    );
    this.name = "InvalidManifestError";
    this.path = path;
    this.cause = cause;
  }
}
