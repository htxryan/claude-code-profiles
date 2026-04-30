import { describe, expect, it } from "vitest";

import {
  CliNotImplementedError,
  CliUserError,
  EXIT_CONFLICT,
  EXIT_OK,
  EXIT_SYSTEM_ERROR,
  EXIT_USER_ERROR,
  exitCodeFor,
} from "../../src/cli/exit.js";
import {
  ConflictError,
  CycleError,
  InvalidManifestError,
  InvalidSettingsJsonError,
  MergeReadFailedError,
  MissingIncludeError,
  MissingProfileError,
} from "../../src/errors/index.js";
import { LockHeldError } from "../../src/state/lock.js";

describe("exitCodeFor — exit-code matrix", () => {
  it("CliUserError carries its own exit code (defaults 1)", () => {
    expect(exitCodeFor(new CliUserError("bad"))).toBe(EXIT_USER_ERROR);
    expect(exitCodeFor(new CliUserError("forced", EXIT_CONFLICT))).toBe(EXIT_CONFLICT);
  });

  it("CliNotImplementedError → exit 2 (system class)", () => {
    expect(exitCodeFor(new CliNotImplementedError("init", "E6"))).toBe(EXIT_SYSTEM_ERROR);
  });

  it("LockHeldError → exit 3 (the project is occupied)", () => {
    expect(exitCodeFor(new LockHeldError("/p/.lock", 123, "2026-04-25T00:00:00Z"))).toBe(
      EXIT_CONFLICT,
    );
  });

  it("structural ResolverError subclasses → exit 3", () => {
    expect(exitCodeFor(new ConflictError("a/b", ["x", "y"]))).toBe(EXIT_CONFLICT);
    expect(exitCodeFor(new CycleError(["a", "b", "a"]))).toBe(EXIT_CONFLICT);
    expect(exitCodeFor(new MissingIncludeError("c", "/p/c", "z"))).toBe(EXIT_CONFLICT);
    expect(exitCodeFor(new InvalidManifestError("/p/profile.json", "bad json"))).toBe(
      EXIT_CONFLICT,
    );
  });

  it("MissingProfileError split: CLI typo → 1, structural extends-miss → 3", () => {
    // No referencedBy → user typed a name on the CLI that doesn't exist
    // (e.g. `c3p use ghst` instead of `ghost`). Fixable by
    // editing the invocation, so exit 1.
    expect(exitCodeFor(new MissingProfileError("ghst"))).toBe(EXIT_USER_ERROR);
    // referencedBy set → some manifest's extends chain points at a profile
    // that doesn't exist. Structural fault; user has to edit a profile.json.
    expect(exitCodeFor(new MissingProfileError("missing", "child"))).toBe(EXIT_CONFLICT);
  });

  it("MergeError subclasses → exit 2 (runtime drift, not user input)", () => {
    expect(exitCodeFor(new InvalidSettingsJsonError("settings.json", "x", "bad"))).toBe(
      EXIT_SYSTEM_ERROR,
    );
    expect(
      exitCodeFor(new MergeReadFailedError("a", "x", "/abs/a", "ENOENT")),
    ).toBe(EXIT_SYSTEM_ERROR);
  });

  it("Unknown error → exit 2", () => {
    expect(exitCodeFor(new Error("oops"))).toBe(EXIT_SYSTEM_ERROR);
    expect(exitCodeFor("string error")).toBe(EXIT_SYSTEM_ERROR);
    expect(exitCodeFor(undefined)).toBe(EXIT_SYSTEM_ERROR);
  });

  it("exit codes are stable integers", () => {
    expect(EXIT_OK).toBe(0);
    expect(EXIT_USER_ERROR).toBe(1);
    expect(EXIT_SYSTEM_ERROR).toBe(2);
    expect(EXIT_CONFLICT).toBe(3);
  });
});
