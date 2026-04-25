/**
 * E2 fitness-function gate: hooks-precedence integration test.
 *
 * Per the epic: "hooks-precedence integration test stays green across spec
 * edits". This file is the durable surface that proves R12 wins over R8 at
 * `hooks.<EventName>` end-to-end (resolve → merge), AND proves R10/R11
 * boundaries for non-mergeable files in the same wired-up flow.
 *
 * If you find yourself wanting to relax this test, stop — the next epic
 * (E3 materialize) is depending on this guarantee.
 */

import { describe, expect, it } from "vitest";

import { ConflictError } from "../../src/errors/index.js";
import { merge } from "../../src/merge/merge.js";
import { resolve } from "../../src/resolver/resolve.js";
import { makeFixture } from "../helpers/fixture.js";

describe("E2 fitness function: hooks-precedence integration (R12 vs R8)", () => {
  it("end-to-end: R12 concat wins over R8 array-replace at hooks.<EventName> across the canonical chain", async () => {
    const f = await makeFixture({
      profiles: {
        base: {
          manifest: {},
          files: {
            "settings.json": JSON.stringify({
              ui: { theme: "dark" }, // R8 deep-merge survives
              tools: ["base-only"], // R8 array-replace survives
              hooks: {
                PreToolUse: [{ src: "base", run: "warn" }],
              },
            }),
          },
        },
        extended: {
          manifest: { extends: "base" },
          files: {
            "settings.json": JSON.stringify({
              hooks: {
                PreToolUse: [{ src: "extended", run: "audit" }],
              },
            }),
          },
        },
        leaf: {
          manifest: { extends: "extended", includes: ["compA"] },
          files: {
            "settings.json": JSON.stringify({
              tools: ["leaf-replaces"], // R8: replaces base's tools
              hooks: {
                PreToolUse: [{ src: "leaf", run: "block" }],
                PostToolUse: [{ src: "leaf", run: "log" }],
              },
            }),
          },
        },
      },
      components: {
        compA: {
          files: {
            "settings.json": JSON.stringify({
              hooks: {
                PreToolUse: [{ src: "compA", run: "trace" }],
              },
            }),
          },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      const settings = merged.find((m) => m.path === "settings.json")!;
      expect(settings).toBeTruthy();
      expect(settings.mergePolicy).toBe("deep-merge");

      const parsed = JSON.parse(settings.bytes.toString("utf8"));

      // R8 deep-merge survives unrelated keys.
      expect(parsed.ui).toEqual({ theme: "dark" });

      // R8 array-replace at non-hook path: leaf overwrites base's tools[].
      expect(parsed.tools).toEqual(["leaf-replaces"]);

      // R12 carve-out: PreToolUse concatenated in canonical order
      // (base, extended, compA, leaf). R12 explicitly beats R8's array-replace.
      expect(parsed.hooks.PreToolUse).toEqual([
        { src: "base", run: "warn" },
        { src: "extended", run: "audit" },
        { src: "compA", run: "trace" },
        { src: "leaf", run: "block" },
      ]);

      // Different event from one contributor still flows through.
      expect(parsed.hooks.PostToolUse).toEqual([{ src: "leaf", run: "log" }]);

      // Provenance lists every contributor whose settings.json fed the merge.
      expect(settings.contributors).toEqual([
        "base",
        "extended",
        "compA",
        "leaf",
      ]);
    } finally {
      await f.cleanup();
    }
  });

  it("ancestor-only conflicts on last-wins files do NOT throw — leaf wins (R10)", async () => {
    const f = await makeFixture({
      profiles: {
        base: { manifest: {}, files: { "agents/x.json": '{"v":"base"}' } },
        mid: {
          manifest: { extends: "base" },
          files: { "agents/x.json": '{"v":"mid"}' },
        },
        leaf: {
          manifest: { extends: "mid" },
          files: { "agents/x.json": '{"v":"leaf"}' },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      const a = merged.find((m) => m.path === "agents/x.json")!;
      expect(a.bytes.toString("utf8")).toBe('{"v":"leaf"}');
      expect(a.contributors).toEqual(["leaf"]);
    } finally {
      await f.cleanup();
    }
  });

  it("include-vs-include conflict on a non-mergeable path is caught at resolve, never reaching merge (R11)", async () => {
    const f = await makeFixture({
      profiles: {
        leaf: {
          manifest: { includes: ["compA", "compB"] },
          files: {},
        },
      },
      components: {
        compA: { files: { "agents/x.json": "A" } },
        compB: { files: { "agents/x.json": "B" } },
      },
    });
    try {
      // E1's resolver throws; merge is never called.
      await expect(
        resolve("leaf", { projectRoot: f.projectRoot }),
      ).rejects.toBeInstanceOf(ConflictError);
    } finally {
      await f.cleanup();
    }
  });

  it("profile overrides include-conflict on non-mergeable path; merge sees only the profile (R11 carve-out)", async () => {
    const f = await makeFixture({
      profiles: {
        leaf: {
          manifest: { includes: ["compA", "compB"] },
          files: { "agents/x.json": '{"from":"leaf"}' },
        },
      },
      components: {
        compA: { files: { "agents/x.json": "A" } },
        compB: { files: { "agents/x.json": "B" } },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      const a = merged.find((m) => m.path === "agents/x.json")!;
      expect(a.contributors).toEqual(["leaf"]);
      expect(a.bytes.toString("utf8")).toBe('{"from":"leaf"}');
    } finally {
      await f.cleanup();
    }
  });

  it("cross-pollinated chain: same path appears in includes (concat) and ancestors (concat) — order is canonical", async () => {
    // *.md path is mergeable, so includes don't conflict; this exercises the
    // 5-contributor concat order end-to-end.
    const f = await makeFixture({
      profiles: {
        base: { manifest: {}, files: { "CLAUDE.md": "B\n" } },
        leaf: {
          manifest: { extends: "base", includes: ["compA", "compB"] },
          files: { "CLAUDE.md": "L\n" },
        },
      },
      components: {
        compA: { files: { "CLAUDE.md": "A\n" } },
        compB: { files: { "CLAUDE.md": "Bcomp\n" } },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      const md = merged.find((m) => m.path === "CLAUDE.md")!;
      expect(md.bytes.toString("utf8")).toBe("B\nA\nBcomp\nL\n");
      expect(md.contributors).toEqual(["base", "compA", "compB", "leaf"]);
    } finally {
      await f.cleanup();
    }
  });
});
