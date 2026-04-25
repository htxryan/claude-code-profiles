import { describe, expect, it } from "vitest";

import { isMergeable, policyFor } from "../../src/resolver/merge-policy.js";

describe("policyFor()", () => {
  it("returns deep-merge for settings.json at any depth", () => {
    expect(policyFor("settings.json")).toBe("deep-merge");
    expect(policyFor("nested/settings.json")).toBe("deep-merge");
  });

  it("returns concat for *.md files", () => {
    expect(policyFor("CLAUDE.md")).toBe("concat");
    expect(policyFor("notes/foo.md")).toBe("concat");
    expect(policyFor("MIXED.MD")).toBe("concat");
  });

  it("returns last-wins for everything else", () => {
    expect(policyFor("agents/foo.json")).toBe("last-wins");
    expect(policyFor("commands/x.sh")).toBe("last-wins");
    expect(policyFor("plugin.json")).toBe("last-wins");
  });
});

describe("isMergeable()", () => {
  it("matches policyFor's classification", () => {
    expect(isMergeable("settings.json")).toBe(true);
    expect(isMergeable("CLAUDE.md")).toBe(true);
    expect(isMergeable("agents/foo.json")).toBe(false);
  });
});
