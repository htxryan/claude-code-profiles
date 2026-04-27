/**
 * Unit tests for the "did you mean?" helpers (claude-code-profiles-ppo).
 * Pure, deterministic — no fixtures or filesystem.
 */

import { describe, expect, it } from "vitest";

import {
  formatDidYouMean,
  formatInvalidProfileNameMessage,
  levenshtein,
  suggestProfiles,
} from "../../src/cli/suggest.js";

describe("levenshtein", () => {
  it("zero for equal strings", () => {
    expect(levenshtein("", "")).toBe(0);
    expect(levenshtein("ghost", "ghost")).toBe(0);
  });

  it("string length when one side is empty", () => {
    expect(levenshtein("", "abc")).toBe(3);
    expect(levenshtein("abc", "")).toBe(3);
  });

  it("counts single edits", () => {
    expect(levenshtein("ghost", "ghst")).toBe(1); // delete o
    expect(levenshtein("ghost", "ghosts")).toBe(1); // insert s
    expect(levenshtein("ghost", "ghast")).toBe(1); // substitute o→a
  });

  it("transposition costs 2 (substitution-substitution)", () => {
    // Plain Levenshtein (not Damerau): swapping two adjacent chars is 2 edits.
    expect(levenshtein("hte", "the")).toBe(2);
  });

  it("symmetric", () => {
    expect(levenshtein("kitten", "sitting")).toBe(levenshtein("sitting", "kitten"));
    expect(levenshtein("kitten", "sitting")).toBe(3);
  });
});

describe("suggestProfiles", () => {
  it("returns close match for one-edit typo", () => {
    expect(suggestProfiles("ghst", ["ghost", "alpha", "beta"])).toEqual(["ghost"]);
  });

  it("returns matches sorted by distance, ties broken lex", () => {
    // "abc" vs candidates: "abd" (d=1), "abe" (d=1), "axc" (d=1), "zzz" (d=3 → out)
    expect(suggestProfiles("abc", ["zzz", "axc", "abe", "abd"])).toEqual([
      "abd",
      "abe",
      "axc",
    ]);
  });

  it("caps at max=3", () => {
    expect(
      suggestProfiles("abcd", ["abce", "abcf", "abcg", "abch", "abci"]),
    ).toHaveLength(3);
  });

  it("respects custom max", () => {
    expect(suggestProfiles("abcd", ["abce", "abcf", "abcg"], 2)).toHaveLength(2);
  });

  it("returns empty when nothing within distance 2", () => {
    expect(suggestProfiles("alpha", ["zebra", "uvwxy"])).toEqual([]);
  });

  it("returns empty when candidate list is empty", () => {
    expect(suggestProfiles("alpha", [])).toEqual([]);
  });

  it("skips exact match if it appears in candidates", () => {
    // Defensive: caller usually filters but we shouldn't suggest the query
    // back to the user if it sneaks through.
    expect(suggestProfiles("ghost", ["ghost", "ghst"])).toEqual(["ghst"]);
  });
});

describe("formatDidYouMean", () => {
  it("empty array → empty string", () => {
    expect(formatDidYouMean([])).toBe("");
  });

  it("single suggestion", () => {
    expect(formatDidYouMean(["ghost"])).toBe("did you mean: ghost?");
  });

  it("multiple suggestions, comma-separated", () => {
    expect(formatDidYouMean(["a", "b", "c"])).toBe("did you mean: a, b, c?");
  });
});

describe("formatInvalidProfileNameMessage", () => {
  it("includes verb, name, and the disallowed-char phrase", () => {
    const out = formatInvalidProfileNameMessage("use", "a/b");
    expect(out).toContain('use: invalid profile name "a/b"');
    expect(out).toContain("contains /, \\, leading . or _");
    // claude-code-profiles-36o: surface Windows-reserved-name guidance so
    // a profile named "CON" gets actionable wording rather than just the
    // generic separator hint.
    expect(out).toMatch(/CON\/PRN\/AUX\/NUL\/COM1-9\/LPT1-9/);
  });

  it("varies the verb", () => {
    expect(formatInvalidProfileNameMessage("diff", ".hidden")).toContain("diff:");
    expect(formatInvalidProfileNameMessage("validate", "_x")).toContain("validate:");
  });
});
