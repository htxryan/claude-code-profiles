import { describe, expect, it } from "vitest";

import {
  MergeError,
  MergeReadFailedError,
  PipelineError,
  ResolverError,
} from "../../src/errors/index.js";
import { merge } from "../../src/merge/merge.js";
import { resolve } from "../../src/resolver/resolve.js";
import { makeFixture } from "../helpers/fixture.js";

function indexByPath(merged: { path: string }[]) {
  return new Map(merged.map((m) => [m.path, m as never]));
}

describe("merge() orchestrator over ResolvedPlan", () => {
  it("produces one MergedFile per relPath, lex-sorted", async () => {
    const f = await makeFixture({
      profiles: {
        leaf: {
          manifest: {},
          files: {
            "CLAUDE.md": "# leaf\n",
            "settings.json": JSON.stringify({ x: 1 }),
            "agents/foo.txt": "foo",
            "z.txt": "z",
          },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      // Plan files are lex-sorted; merged should follow that.
      const paths = merged.map((m) => m.path);
      expect(paths).toEqual([...paths].sort());
      // No duplicate paths.
      expect(new Set(paths).size).toBe(paths.length);
      const m = indexByPath(merged);
      expect(m.has("CLAUDE.md")).toBe(true);
      expect(m.has("settings.json")).toBe(true);
      expect(m.has("agents/foo.txt")).toBe(true);
      expect(m.has("z.txt")).toBe(true);
    } finally {
      await f.cleanup();
    }
  });

  it("dispatches to deep-merge for settings.json across the chain", async () => {
    const f = await makeFixture({
      profiles: {
        base: {
          manifest: {},
          files: {
            "settings.json": JSON.stringify({ ui: { theme: "dark" }, keep: "yes" }),
          },
        },
        leaf: {
          manifest: { extends: "base" },
          files: {
            "settings.json": JSON.stringify({ ui: { font: "mono" } }),
          },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      const settings = merged.find((m) => m.path === "settings.json")!;
      expect(settings.mergePolicy).toBe("deep-merge");
      expect(JSON.parse(settings.bytes.toString("utf8"))).toEqual({
        ui: { theme: "dark", font: "mono" },
        keep: "yes",
      });
      expect(settings.contributors).toEqual(["base", "leaf"]);
    } finally {
      await f.cleanup();
    }
  });

  it("dispatches to concat for *.md across the canonical chain order (R9 worked example)", async () => {
    const f = await makeFixture({
      profiles: {
        base: { manifest: {}, files: { "CLAUDE.md": "BASE\n" } },
        extended: {
          manifest: { extends: "base" },
          files: { "CLAUDE.md": "EXTENDED\n" },
        },
        leaf: {
          manifest: {
            extends: "extended",
            includes: ["compA", "compB"],
          },
          files: { "CLAUDE.md": "LEAF\n" },
        },
      },
      components: {
        compA: { files: { "CLAUDE.md": "COMPA\n" } },
        compB: { files: { "CLAUDE.md": "COMPB\n" } },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      const md = merged.find((m) => m.path === "CLAUDE.md")!;
      expect(md.mergePolicy).toBe("concat");
      expect(md.bytes.toString("utf8")).toBe(
        "BASE\nEXTENDED\nCOMPA\nCOMPB\nLEAF\n",
      );
      expect(md.contributors).toEqual([
        "base",
        "extended",
        "compA",
        "compB",
        "leaf",
      ]);
    } finally {
      await f.cleanup();
    }
  });

  it("dispatches to last-wins for non-mergeable files; only profile contributor wins over ancestor", async () => {
    const f = await makeFixture({
      profiles: {
        base: { manifest: {}, files: { "agents/foo.json": '{"v":"base"}' } },
        leaf: {
          manifest: { extends: "base" },
          files: { "agents/foo.json": '{"v":"leaf"}' },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      const f1 = merged.find((m) => m.path === "agents/foo.json")!;
      expect(f1.mergePolicy).toBe("last-wins");
      expect(f1.bytes.toString("utf8")).toBe('{"v":"leaf"}');
      expect(f1.contributors).toEqual(["leaf"]);
    } finally {
      await f.cleanup();
    }
  });

  it("supports a custom read function (no disk IO)", async () => {
    const f = await makeFixture({
      profiles: {
        leaf: {
          manifest: {},
          files: { "settings.json": "ignored" },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan, {
        read: async () => Buffer.from(JSON.stringify({ injected: true })),
      });
      const settings = merged.find((m) => m.path === "settings.json")!;
      expect(JSON.parse(settings.bytes.toString("utf8"))).toEqual({
        injected: true,
      });
    } finally {
      await f.cleanup();
    }
  });

  it("wraps read errors as MergeReadFailedError with contributor + path context", async () => {
    const f = await makeFixture({
      profiles: {
        leaf: { manifest: {}, files: { "CLAUDE.md": "x" } },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const failingRead = async () => {
        throw new Error("disk gremlins");
      };
      await expect(merge(plan, { read: failingRead })).rejects.toBeInstanceOf(
        MergeReadFailedError,
      );
      try {
        await merge(plan, { read: failingRead });
        expect.fail("should have thrown");
      } catch (err) {
        const e = err as MergeReadFailedError;
        expect(e.contributor).toBe("leaf");
        expect(e.relPath).toBe("CLAUDE.md");
        expect(e.message).toContain("disk gremlins");
      }
    } finally {
      await f.cleanup();
    }
  });

  it("returns an empty array for a profile with no files", async () => {
    const f = await makeFixture({
      profiles: { empty: { manifest: {}, files: {} } },
    });
    try {
      const plan = await resolve("empty", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      expect(merged).toEqual([]);
    } finally {
      await f.cleanup();
    }
  });

  it("classifies settings.json at any depth as deep-merge (E1 policyFor invariant)", async () => {
    const f = await makeFixture({
      profiles: {
        base: {
          manifest: {},
          files: { "subdir/settings.json": JSON.stringify({ a: 1, c: 1 }) },
        },
        leaf: {
          manifest: { extends: "base" },
          files: { "subdir/settings.json": JSON.stringify({ a: 2, b: 3 }) },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      const merged = await merge(plan);
      const m = merged.find((x) => x.path === "subdir/settings.json")!;
      expect(m.mergePolicy).toBe("deep-merge");
      expect(JSON.parse(m.bytes.toString("utf8"))).toEqual({ a: 2, b: 3, c: 1 });
    } finally {
      await f.cleanup();
    }
  });

  it("throws on conflicting mergePolicy within a single relPath group (defensive invariant guard)", async () => {
    const f = await makeFixture({
      profiles: {
        base: {
          manifest: {},
          files: { "settings.json": JSON.stringify({ a: 1 }) },
        },
        leaf: {
          manifest: { extends: "base" },
          files: { "settings.json": JSON.stringify({ b: 2 }) },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      // Synthesize a malformed plan: same relPath but two different policies
      // across contributors. E1 should never produce this (policy is a pure
      // function of relPath), but the orchestrator must fail loud if it does.
      const malformed = {
        ...plan,
        files: plan.files.map((f, i) => ({
          ...f,
          mergePolicy: i === 0 ? "deep-merge" : "last-wins",
        })),
      } as typeof plan;
      await expect(merge(malformed)).rejects.toThrow(/conflicting mergePolicy/);
    } finally {
      await f.cleanup();
    }
  });

  it("MergeReadFailedError is a MergeError, not a ResolverError (callers can filter by phase)", async () => {
    const f = await makeFixture({
      profiles: { leaf: { manifest: {}, files: { "CLAUDE.md": "x" } } },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      try {
        await merge(plan, {
          read: async () => {
            throw new Error("boom");
          },
        });
        expect.fail("should have thrown");
      } catch (err) {
        expect(err).toBeInstanceOf(MergeReadFailedError);
        expect(err).toBeInstanceOf(MergeError);
        expect(err).toBeInstanceOf(PipelineError);
        expect(err).not.toBeInstanceOf(ResolverError);
      }
    } finally {
      await f.cleanup();
    }
  });

  it("throws on non-contiguous plan.files entries (defensive invariant guard)", async () => {
    const f = await makeFixture({
      profiles: {
        leaf: {
          manifest: {},
          files: { "a.txt": "1", "b.txt": "2" },
        },
      },
    });
    try {
      const plan = await resolve("leaf", { projectRoot: f.projectRoot });
      // Synthesize a malformed plan that interleaves two entries with the
      // same relPath and a different relPath in between. This should never
      // happen via E1, but the orchestrator should fail loudly if it does.
      const dup = { ...plan.files[0]! };
      const malformed = {
        ...plan,
        files: [plan.files[0]!, plan.files[1]!, dup],
      };
      await expect(merge(malformed)).rejects.toThrow(/not contiguous/);
    } finally {
      await f.cleanup();
    }
  });
});
