import { describe, expect, it } from "vitest";

import { concatStrategy } from "../../src/merge/concat.js";
import type { ContributorBytes } from "../../src/merge/types.js";

function inputs(...entries: Array<[string, string]>): ContributorBytes[] {
  return entries.map(([id, text]) => ({ id, bytes: Buffer.from(text, "utf8") }));
}

describe("concatStrategy (R9)", () => {
  it("concatenates contributors in order, preserving trailing newlines", () => {
    const r = concatStrategy(
      "CLAUDE.md",
      inputs(
        ["base", "# base\nbase content\n"],
        ["leaf", "# leaf\nleaf content\n"],
      ),
    );
    expect(r.bytes.toString("utf8")).toBe(
      "# base\nbase content\n# leaf\nleaf content\n",
    );
    expect(r.contributors).toEqual(["base", "leaf"]);
  });

  it("matches the R9 worked example: base ← extended ← profile + compA + compB", () => {
    const r = concatStrategy(
      "CLAUDE.md",
      inputs(
        ["base", "BASE\n"],
        ["extended", "EXTENDED\n"],
        ["compA", "COMPA\n"],
        ["compB", "COMPB\n"],
        ["leaf", "LEAF\n"],
      ),
    );
    expect(r.bytes.toString("utf8")).toBe(
      "BASE\nEXTENDED\nCOMPA\nCOMPB\nLEAF\n",
    );
    expect(r.contributors).toEqual([
      "base",
      "extended",
      "compA",
      "compB",
      "leaf",
    ]);
  });

  it("inserts a separator newline when a chunk does not end with one", () => {
    const r = concatStrategy(
      "CLAUDE.md",
      inputs(["a", "no-newline"], ["b", "after\n"]),
    );
    expect(r.bytes.toString("utf8")).toBe("no-newline\nafter\n");
  });

  it("does not double newlines for chunks that already end with \\n", () => {
    const r = concatStrategy(
      "CLAUDE.md",
      inputs(["a", "ends-with-nl\n"], ["b", "x\n"]),
    );
    expect(r.bytes.toString("utf8")).toBe("ends-with-nl\nx\n");
  });

  it("handles a single contributor unchanged", () => {
    const r = concatStrategy("CLAUDE.md", inputs(["only", "solo content\n"]));
    expect(r.bytes.toString("utf8")).toBe("solo content\n");
    expect(r.contributors).toEqual(["only"]);
  });

  it("preserves binary-safe bytes (no string normalization)", () => {
    // Edge: a UTF-8 byte that, in some normalizations, would be reinterpreted.
    const utf8 = Buffer.from("café\n", "utf8");
    const r = concatStrategy(
      "notes/é.md",
      [
        { id: "a", bytes: utf8 },
        { id: "b", bytes: Buffer.from("X\n", "utf8") },
      ],
    );
    // First 5 bytes are "café\n" — verify byte-for-byte.
    expect(Buffer.compare(r.bytes.subarray(0, utf8.length), utf8)).toBe(0);
  });

  it("skips empty contributors entirely (no spurious blank lines, not in provenance)", () => {
    const r = concatStrategy(
      "CLAUDE.md",
      inputs(["a", ""], ["b", "after\n"]),
    );
    expect(r.bytes.toString("utf8")).toBe("after\n");
    expect(r.contributors).toEqual(["b"]);
  });

  it("an empty middle chunk does not break joining of surrounding chunks", () => {
    const r = concatStrategy(
      "CLAUDE.md",
      inputs(["a", "X\n"], ["b", ""], ["c", "Y\n"]),
    );
    expect(r.bytes.toString("utf8")).toBe("X\nY\n");
    expect(r.contributors).toEqual(["a", "c"]);
  });
});
