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

describe("policyFor() — destination-agnostic for CLAUDE.md (cw6/T2)", () => {
  // Per docs/specs/claude-code-profiles.md §12, the merge policy for CLAUDE.md
  // is identical regardless of destination ('.claude' vs 'projectRoot'):
  // both use 'concat'. The destination only changes how merge GROUPS files,
  // not how it CLASSIFIES them. policyFor() takes a relPath and returns the
  // same policy for both destinations.
  it("returns concat for CLAUDE.md regardless of destination", () => {
    // Both destinations carry the same relPath 'CLAUDE.md' from the walker's
    // perspective; the policy depends only on that relPath.
    expect(policyFor("CLAUDE.md")).toBe("concat");
  });
});
