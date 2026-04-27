/**
 * AC-17: errors must always name the offending file/profile/path.
 *
 * Parameterized over each error class the CLI maps. Each row asserts the
 * formatted string contains the per-class identifying token (profile name,
 * include path, conflict path + contributors, lock holder PID, etc.).
 *
 * Catches a class of regressions where an error subclass's ctor is "improved"
 * to use a generic message and silently strips the file/profile/path the §7
 * quality bar mandates.
 */

import { describe, expect, it } from "vitest";

import { formatError } from "../../src/cli/format.js";
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

describe("formatError — AC-17 (always names file/profile/path)", () => {
  it("MissingProfileError: contains the profile name", () => {
    const out = formatError(new MissingProfileError("ghost", "leaf"));
    expect(out).toContain("ghost");
    expect(out).toContain("leaf");
  });

  it("CycleError: lists every cycle member", () => {
    const out = formatError(new CycleError(["a", "b", "c", "a"]));
    expect(out).toContain("a");
    expect(out).toContain("b");
    expect(out).toContain("c");
  });

  it("MissingIncludeError: contains raw + resolved + referencer", () => {
    const out = formatError(new MissingIncludeError("compX", "/abs/compX", "leaf"));
    expect(out).toContain("compX");
    expect(out).toContain("/abs/compX");
    expect(out).toContain("leaf");
  });

  it("ConflictError: contains relPath + every contributor", () => {
    const out = formatError(new ConflictError("settings.local.json", ["c1", "c2"]));
    expect(out).toContain("settings.local.json");
    expect(out).toContain("c1");
    expect(out).toContain("c2");
  });

  it("InvalidManifestError: contains the manifest path + detail", () => {
    const out = formatError(
      new InvalidManifestError("/p/.claude-profiles/x/profile.json", "Unexpected token"),
    );
    expect(out).toContain("profile.json");
    expect(out).toContain("Unexpected token");
  });

  it("InvalidSettingsJsonError: contains relPath + contributor", () => {
    const out = formatError(
      new InvalidSettingsJsonError("settings.json", "compZ", "expected object got array"),
    );
    expect(out).toContain("settings.json");
    expect(out).toContain("compZ");
  });

  it("MergeReadFailedError: contains relPath + contributor + abs path", () => {
    const out = formatError(
      new MergeReadFailedError("agents/x.md", "leaf", "/abs/agents/x.md", "ENOENT"),
    );
    expect(out).toContain("agents/x.md");
    expect(out).toContain("leaf");
    expect(out).toContain("/abs/agents/x.md");
  });

  it("LockHeldError: contains lock path + holder PID + timestamp", () => {
    const out = formatError(
      new LockHeldError("/p/.claude-profiles/.meta/lock", 4242, "2026-04-25T12:34:56.789Z"),
    );
    expect(out).toContain(".meta/lock");
    expect(out).toContain("4242");
    expect(out).toContain("2026-04-25T12:34:56.789Z");
  });

  // ppo: LockHeldError must name the next step (wait, or hand-clean the
  // lock if the PID is dead). The PID + timestamp from the §7 quality bar
  // are still present (covered by the previous case).
  it("LockHeldError: includes a remediation suggestion", () => {
    const out = formatError(
      new LockHeldError("/p/.claude-profiles/.meta/lock", 4242, "2026-04-25T12:34:56.789Z"),
    );
    expect(out.toLowerCase()).toContain("wait for the other process");
    // Tells the user where to delete the lock by name.
    expect(out).toContain("/p/.claude-profiles/.meta/lock");
  });

  it("Unknown error: still produces the claude-profiles: prefix", () => {
    const out = formatError(new Error("something broke"));
    expect(out.startsWith("claude-profiles:")).toBe(true);
    expect(out).toContain("something broke");
  });

  it("Non-Error value: degrades to String() representation", () => {
    const out = formatError("plain string");
    expect(out).toContain("plain string");
  });
});
