/**
 * R39 / CONTRIBUTING.md: profile names that Windows refuses to mount must
 * be rejected on every host so a profile authored on Linux/macOS remains
 * usable on Windows. This file pins both layers of the validation:
 *
 *   1. `isValidProfileName` (resolver) — the predicate that drives
 *      assertValidProfileName + the resolver's MissingProfileError path.
 *   2. `buildPersistPaths` (state) — the persist boundary that re-validates
 *      defense-in-depth so a caller bypassing the resolver still cannot
 *      land a directory under a reserved name.
 *
 * Reverting either guard (the WIN_RESERVED regex in resolver/paths.ts or
 * the `isWindowsReservedName` re-check in state/paths.ts) must surface as
 * a failure here.
 *
 * Platform-agnostic by construction: every assertion is a pure-function
 * check, so the file produces the same result on macOS / Linux / Windows
 * without spawning a process or touching the real filesystem.
 */

import { describe, expect, it } from "vitest";

import {
  isValidProfileName,
  isWindowsReservedName,
} from "../../src/resolver/paths.js";
import { buildPersistPaths, buildStatePaths } from "../../src/state/paths.js";

describe("isValidProfileName — Windows-reserved DOS device names (R39)", () => {
  // The full DOS-device family per CONTRIBUTING.md "Reserved filenames".
  // COM0 / LPT0 are intentionally NOT reserved (only 1-9), so they remain
  // valid names — that's a regression-guard against a future tightening
  // that accidentally rejects a previously-valid profile.
  const RESERVED = [
    "CON",
    "PRN",
    "AUX",
    "NUL",
    "COM1",
    "COM5",
    "COM9",
    "LPT1",
    "LPT5",
    "LPT9",
  ];

  for (const name of RESERVED) {
    it(`rejects "${name}" (bare DOS device name)`, () => {
      expect(isValidProfileName(name)).toBe(false);
      expect(isWindowsReservedName(name)).toBe(true);
    });

    it(`rejects "${name.toLowerCase()}" (case-insensitive — Win32 is)`, () => {
      expect(isValidProfileName(name.toLowerCase())).toBe(false);
    });

    it(`rejects "${name}.txt" (Windows treats extension as the device)`, () => {
      expect(isValidProfileName(`${name}.txt`)).toBe(false);
    });
  }

  it("rejects names with multiple-dot extensions on a reserved stem", () => {
    // "CON.tar.gz" → Windows still reads it as the CON device.
    expect(isValidProfileName("CON.tar.gz")).toBe(false);
  });

  it("does NOT reject COM0 / LPT0 (only 1-9 are reserved)", () => {
    expect(isValidProfileName("COM0")).toBe(true);
    expect(isValidProfileName("LPT0")).toBe(true);
  });

  it("does NOT reject names that merely contain a reserved word", () => {
    // Substring match would over-reject. The validator should only fire on
    // the exact stem (with optional extension).
    expect(isValidProfileName("BACON")).toBe(true);
    expect(isValidProfileName("CONfig")).toBe(true);
    expect(isValidProfileName("printer")).toBe(true);
    expect(isValidProfileName("auxiliary")).toBe(true);
  });
});

describe("isValidProfileName — trailing dot/space (Windows path normalisation)", () => {
  it("rejects a trailing dot (Win32 silently strips it)", () => {
    expect(isValidProfileName("foo.")).toBe(false);
  });

  it("rejects a trailing space (Win32 silently strips it)", () => {
    expect(isValidProfileName("foo ")).toBe(false);
  });

  it("rejects multiple trailing dots", () => {
    expect(isValidProfileName("foo..")).toBe(false);
  });

  it("does NOT reject internal dots or spaces", () => {
    expect(isValidProfileName("foo.bar")).toBe(true);
    expect(isValidProfileName("foo bar")).toBe(true);
  });
});

describe("isValidProfileName — defense-in-depth on NUL bytes", () => {
  it("rejects NUL bytes anywhere in the name", () => {
    expect(isValidProfileName("foo\0bar")).toBe(false);
    expect(isValidProfileName("\0")).toBe(false);
  });
});

describe("isValidProfileName — every name-taking verb sees the same predicate", () => {
  // Whitebox cross-verification: the verbs that take a profile name
  // (new/use/diff/validate) all route through assertValidProfileName, which
  // calls isValidProfileName. Pinning the predicate's behaviour for each
  // reserved name automatically covers each verb's pre-flight without
  // spawning four subprocesses per case (which would dominate runtime).
  for (const name of ["CON", "PRN", "AUX", "NUL", "COM1", "LPT9", "con.txt"]) {
    it(`every verb pre-flight rejects "${name}" (predicate-level)`, () => {
      expect(isValidProfileName(name)).toBe(false);
    });
  }
});

describe("buildPersistPaths — defense-in-depth Windows-reserved rejection", () => {
  // The persist boundary re-validates so a caller bypassing the resolver
  // (e.g. a future internal pathway that constructs a name from external
  // input) still cannot land a directory under a Windows-reserved name.
  // The resolver-level rejection in isValidProfileName is the primary gate;
  // this re-check is belt-and-braces.
  const paths = buildStatePaths("/tmp/ccp-windows-reserved");

  for (const name of ["CON", "PRN", "AUX", "NUL", "COM1", "LPT9"]) {
    it(`throws when constructing persist paths for reserved "${name}"`, () => {
      expect(() => buildPersistPaths(paths, name)).toThrow(
        /Invalid profile name for persist target/,
      );
    });
  }

  it(`throws on case-insensitive variants ("con", "lpt1")`, () => {
    expect(() => buildPersistPaths(paths, "con")).toThrow();
    expect(() => buildPersistPaths(paths, "lpt1")).toThrow();
  });

  it(`throws on reserved-with-extension ("PRN.txt")`, () => {
    expect(() => buildPersistPaths(paths, "PRN.txt")).toThrow();
  });

  it(`throws on NUL bytes in name`, () => {
    expect(() => buildPersistPaths(paths, "foo\0bar")).toThrow();
  });

  it(`throws on path-traversal-shaped name (defense-in-depth)`, () => {
    expect(() => buildPersistPaths(paths, "..")).toThrow();
    expect(() => buildPersistPaths(paths, "a/b")).toThrow();
    expect(() => buildPersistPaths(paths, "a\\b")).toThrow();
  });

  it(`accepts an ordinary name`, () => {
    const r = buildPersistPaths(paths, "ordinary");
    expect(r.profileDir).toContain("ordinary");
    expect(r.targetClaudeDir).toContain(".claude");
  });
});
