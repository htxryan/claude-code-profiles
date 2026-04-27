/**
 * Unit tests for `renderTable` (claude-code-profiles-pcs polish epic).
 *
 * The list command is the only caller today, but the function is exported
 * and may grow more callers. Two invariants the tests pin down:
 *
 *   1. No ragged trailing whitespace — last column is emitted raw.
 *   2. ANSI-escape-aware width measurement — bolded/coloured cells do
 *      NOT push neighbouring rows out of column alignment under TTY.
 */

import { describe, expect, it } from "vitest";

import { renderTable } from "../../src/cli/format.js";

describe("renderTable", () => {
  it("two-column rendering matches the legacy single-pad behavior", () => {
    const out = renderTable([
      ["short", "v1"],
      ["longername", "v2"],
    ]);
    expect(out).toBe(
      [
        "short       v1",
        "longername  v2",
      ].join("\n"),
    );
  });

  it("never produces ragged trailing whitespace (last column emitted raw)", () => {
    const out = renderTable([
      ["alpha", "first description"],
      ["beta", ""],
      ["gamma", "third"],
    ]);
    for (const line of out.split("\n")) {
      expect(line).toBe(line.trimEnd());
    }
  });

  it("padding ignores ANSI escapes in earlier columns", () => {
    // Cell with ANSI escapes (bold open + reset) has visible width 3 but
    // raw byte length 11. Without escape-aware measurement the next row's
    // padding would be wrong by exactly the escape-byte difference.
    const bold = "\x1b[1mfoo\x1b[0m";
    const out = renderTable([
      [bold, "row1"],
      ["foobar", "row2"],
    ]);
    const lines = out.split("\n");
    // The visible offset of "row1"/"row2" must be identical: both start
    // at the same character position when ANSI is stripped.
    const visible1 = lines[0]!.replace(/\x1b\[[0-9;]*m/g, "");
    const visible2 = lines[1]!;
    const offset1 = visible1.indexOf("row1");
    const offset2 = visible2.indexOf("row2");
    expect(offset1).toBe(offset2);
  });

  it("supports N>2 columns; pads each interior column to its own max width", () => {
    const out = renderTable([
      ["a", "alpha", "tags=[x]"],
      ["bb", "beta", ""],
      ["ccc", "gamma", "tags=[y,z]"],
    ]);
    const lines = out.split("\n");
    // All 3 lines: "<name>  <desc>  <last-col>" — last col raw.
    expect(lines).toEqual([
      "a    alpha  tags=[x]",
      "bb   beta",
      "ccc  gamma  tags=[y,z]",
    ]);
  });

  it("treats missing trailing cells as empty (per-row sparse columns)", () => {
    const out = renderTable([
      ["a", "x", "y"],
      ["bb"], // single-cell row
    ]);
    const lines = out.split("\n");
    expect(lines[0]).toBe("a   x  y");
    // Second row has no col2/col3 — but col widths remain stable so col1
    // pads to "bb  " (visible) before falling off the right edge.
    expect(lines[1]).toBe("bb");
  });

  it("empty rows array returns empty string", () => {
    expect(renderTable([])).toBe("");
  });
});
