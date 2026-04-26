/**
 * Exit-code policy (E5 fitness function: matrix stays stable).
 *
 *   0 — success
 *   1 — user error: bad argv, drift abort, non-TTY swap with no --on-drift,
 *       validation failure, missing profile name on the CLI (typo — the user
 *       can fix it by editing their invocation)
 *   2 — system error: unexpected IO/permission/internal fault, including
 *       not-yet-implemented stubs (init/hook in E5)
 *   3 — conflict / cycle / structural-missing: ResolverError that signals
 *       the manifest graph itself is broken (cycle, missing profile reached
 *       via an extends chain, missing include); LockHeldError also maps here
 *       (a peer process holds the project, conceptually a "missing slot")
 *
 * The 'missing profile' case is split: when the user types
 * `claude-profiles use ghst` (typo for `ghost`), the resolver throws
 * MissingProfileError with referencedBy === undefined — that's a CLI typo
 * and exits 1. When a manifest declares `extends: "nope"`, the resolver
 * throws the same error class but with referencedBy set — that's a
 * structural manifest failure and exits 3.
 *
 * Mapping is centralised so individual command handlers don't reinvent it.
 */

import { LockHeldError } from "../state/lock.js";

import {
  ConflictError,
  CycleError,
  MissingIncludeError,
  MissingProfileError,
  MergeError,
  PipelineError,
  ResolverError,
} from "../errors/index.js";

export const EXIT_OK = 0;
export const EXIT_USER_ERROR = 1;
export const EXIT_SYSTEM_ERROR = 2;
export const EXIT_CONFLICT = 3;

/** Fixed CLI exit codes — the test suite asserts each maps to the right value. */
export type ExitCode =
  | typeof EXIT_OK
  | typeof EXIT_USER_ERROR
  | typeof EXIT_SYSTEM_ERROR
  | typeof EXIT_CONFLICT;

/**
 * Marker thrown by command handlers to signal a user-error exit without a
 * specific error subclass (e.g. drift abort, validation fail). Carries the
 * intended exit code so the dispatcher doesn't have to guess.
 */
export class CliUserError extends Error {
  readonly exitCode: ExitCode;
  constructor(message: string, exitCode: ExitCode = EXIT_USER_ERROR) {
    super(message);
    this.name = "CliUserError";
    this.exitCode = exitCode;
  }
}

/**
 * Marker for "not yet implemented in this epic" — used by E5 stubs for the
 * init/hook verbs (E6 owns those). Maps to exit 2 (system error class) so
 * callers don't conflate it with bad user input.
 */
export class CliNotImplementedError extends Error {
  constructor(verb: string, owner: string) {
    super(`${verb} is not yet implemented (owned by ${owner})`);
    this.name = "CliNotImplementedError";
  }
}

/**
 * Map any error thrown by the command pipeline to a CLI exit code.
 *
 * - CliUserError carries its own code (lets callers force exit 1 vs 3).
 * - MissingProfileError → 1 if it's a CLI typo (referencedBy undefined),
 *   3 if it's structural (manifest extends chain points at nothing).
 * - Other ResolverError subclasses → 3 (conflict/cycle/missing-include).
 * - LockHeldError → 3 (the project is occupied; semantically "no slot for us").
 * - MergeError → 2 (runtime drift, not user-input fault).
 * - CliNotImplementedError → 2 (system error class).
 * - Anything else → 2 (unexpected system fault).
 */
export function exitCodeFor(err: unknown): ExitCode {
  if (err instanceof CliUserError) return err.exitCode;
  if (err instanceof CliNotImplementedError) return EXIT_SYSTEM_ERROR;
  if (err instanceof LockHeldError) return EXIT_CONFLICT;
  if (err instanceof MissingProfileError) {
    // Distinguish "user typed a name that doesn't exist" (typo, fixable by
    // editing argv → exit 1) from "manifest's extends chain points at a
    // profile that doesn't exist" (structural fault → exit 3). The resolver
    // sets referencedBy only when the missing name was reached transitively.
    return err.referencedBy === undefined ? EXIT_USER_ERROR : EXIT_CONFLICT;
  }
  if (
    err instanceof ConflictError ||
    err instanceof CycleError ||
    err instanceof MissingIncludeError
  ) {
    return EXIT_CONFLICT;
  }
  if (err instanceof ResolverError) return EXIT_CONFLICT;
  if (err instanceof MergeError) return EXIT_SYSTEM_ERROR;
  if (err instanceof PipelineError) return EXIT_SYSTEM_ERROR;
  return EXIT_SYSTEM_ERROR;
}
