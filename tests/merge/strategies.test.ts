import { describe, expect, it } from "vitest";

import { concatStrategy } from "../../src/merge/concat.js";
import { deepMergeStrategy } from "../../src/merge/deep-merge.js";
import { lastWinsStrategy } from "../../src/merge/last-wins.js";
import { getStrategy } from "../../src/merge/strategies.js";

describe("strategy registry", () => {
  it("returns deepMergeStrategy for 'deep-merge'", () => {
    expect(getStrategy("deep-merge")).toBe(deepMergeStrategy);
  });

  it("returns concatStrategy for 'concat'", () => {
    expect(getStrategy("concat")).toBe(concatStrategy);
  });

  it("returns lastWinsStrategy for 'last-wins'", () => {
    expect(getStrategy("last-wins")).toBe(lastWinsStrategy);
  });

  it("throws for unknown policy", () => {
    expect(() => getStrategy("unknown" as never)).toThrow();
  });
});
