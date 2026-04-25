import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  ConflictError,
  CycleError,
  InvalidManifestError,
  MissingIncludeError,
  MissingProfileError,
  resolve,
} from "../../src/resolver/index.js";
import { makeFixture, type Fixture } from "../helpers/fixture.js";

describe("resolve()", () => {
  let fx: Fixture | undefined;

  afterEach(async () => {
    if (fx) {
      await fx.cleanup();
      fx = undefined;
    }
  });

  describe("ResolvedPlan invariants", () => {
    it("returns a plan whose chain ends with the requested profile (R3)", async () => {
      fx = await makeFixture({
        profiles: {
          base: { manifest: {}, files: { "a.txt": "a" } },
          mid: { manifest: { extends: "base" }, files: { "b.txt": "b" } },
          leaf: { manifest: { extends: "mid" }, files: { "c.txt": "c" } },
        },
      });

      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });

      expect(plan.profileName).toBe("leaf");
      expect(plan.chain).toEqual(["base", "mid", "leaf"]);
      expect(plan.chain[plan.chain.length - 1]).toBe(plan.profileName);
    });

    it("sorts files lex by relPath (E1 invariant)", async () => {
      fx = await makeFixture({
        profiles: {
          p: {
            manifest: {},
            files: { "z.txt": "z", "a.txt": "a", "m/b.txt": "b", "m/a.txt": "a" },
          },
        },
      });

      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const paths = plan.files.map((f) => f.relPath);
      const sorted = [...paths].sort();
      expect(paths).toEqual(sorted);
    });

    it("includes a profile contributor as the last entry for the leaf", async () => {
      fx = await makeFixture({
        profiles: {
          base: { manifest: {}, files: { "a.txt": "a" } },
          leaf: { manifest: { extends: "base" }, files: { "b.txt": "b" } },
        },
      });
      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
      const last = plan.contributors[plan.contributors.length - 1]!;
      expect(last.kind).toBe("profile");
      expect(last.id).toBe("leaf");
    });

    it("never produces duplicate (relPath, contributorIndex) pairs", async () => {
      fx = await makeFixture({
        profiles: {
          base: { manifest: {}, files: { "a.txt": "1" } },
          leaf: { manifest: { extends: "base" }, files: { "a.txt": "2" } },
        },
      });
      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
      const seen = new Set<string>();
      for (const f of plan.files) {
        const key = `${f.relPath}::${f.contributorIndex}`;
        expect(seen.has(key)).toBe(false);
        seen.add(key);
      }
    });

    it("each PlanFile.contributorIndex points to a real contributor", async () => {
      fx = await makeFixture({
        profiles: {
          a: { manifest: {}, files: { "x": "x" } },
          b: { manifest: { extends: "a" }, files: { "y": "y" } },
        },
      });
      const plan = await resolve("b", { projectRoot: fx.projectRoot });
      for (const f of plan.files) {
        expect(plan.contributors[f.contributorIndex]).toBeDefined();
      }
    });
  });

  describe("R1/R2 — discovery & identity", () => {
    it("R1: exposes profile by directory name", async () => {
      fx = await makeFixture({
        profiles: { foo: { manifest: {} } },
      });
      const plan = await resolve("foo", { projectRoot: fx.projectRoot });
      expect(plan.profileName).toBe("foo");
    });

    it("uses directory name even if manifest.name says otherwise (R2 takes precedence over R35 default)", async () => {
      // Per R2 the directory name is the canonical identifier; manifest.name
      // is only a *default* for cases where the manifest is the source of
      // truth. resolve()'s first arg is the canonical id, so chain reflects
      // that.
      fx = await makeFixture({
        profiles: { foo: { manifest: { name: "renamed" } } },
      });
      const plan = await resolve("foo", { projectRoot: fx.projectRoot });
      expect(plan.profileName).toBe("foo");
      expect(plan.chain).toEqual(["foo"]);
    });
  });

  describe("R3/R4/R5 — extends chain", () => {
    it("R3: builds linear oldest-first chain", async () => {
      fx = await makeFixture({
        profiles: {
          a: { manifest: {} },
          b: { manifest: { extends: "a" } },
          c: { manifest: { extends: "b" } },
        },
      });
      const plan = await resolve("c", { projectRoot: fx.projectRoot });
      expect(plan.chain).toEqual(["a", "b", "c"]);
    });

    it("R4: throws CycleError naming members on a 2-cycle", async () => {
      fx = await makeFixture({
        profiles: {
          a: { manifest: { extends: "b" } },
          b: { manifest: { extends: "a" } },
        },
      });
      await expect(resolve("a", { projectRoot: fx.projectRoot })).rejects.toThrow(CycleError);
      try {
        await resolve("a", { projectRoot: fx.projectRoot });
      } catch (err) {
        expect(err).toBeInstanceOf(CycleError);
        const e = err as CycleError;
        expect(e.cycle).toContain("a");
        expect(e.cycle).toContain("b");
        expect(e.message).toContain("a");
        expect(e.message).toContain("b");
      }
    });

    it("R4: detects a self-loop", async () => {
      fx = await makeFixture({
        profiles: { a: { manifest: { extends: "a" } } },
      });
      await expect(resolve("a", { projectRoot: fx.projectRoot })).rejects.toThrow(CycleError);
    });

    it("R4: detects a 3-cycle and lists members in cycle order", async () => {
      fx = await makeFixture({
        profiles: {
          a: { manifest: { extends: "b" } },
          b: { manifest: { extends: "c" } },
          c: { manifest: { extends: "a" } },
        },
      });
      try {
        await resolve("a", { projectRoot: fx.projectRoot });
        throw new Error("expected throw");
      } catch (err) {
        expect(err).toBeInstanceOf(CycleError);
        const e = err as CycleError;
        // Members listed
        expect(new Set(e.cycle)).toEqual(new Set(["a", "b", "c"]));
      }
    });

    it("R5: throws MissingProfileError naming the missing profile", async () => {
      fx = await makeFixture({
        profiles: { a: { manifest: { extends: "ghost" } } },
      });
      try {
        await resolve("a", { projectRoot: fx.projectRoot });
        throw new Error("expected throw");
      } catch (err) {
        expect(err).toBeInstanceOf(MissingProfileError);
        const e = err as MissingProfileError;
        expect(e.missing).toBe("ghost");
        expect(e.referencedBy).toBe("a");
        expect(e.message).toContain("ghost");
      }
    });

    it("R5: throws MissingProfileError when the requested profile itself is missing", async () => {
      fx = await makeFixture({ profiles: {} });
      try {
        await resolve("nope", { projectRoot: fx.projectRoot });
        throw new Error("expected throw");
      } catch (err) {
        expect(err).toBeInstanceOf(MissingProfileError);
        const e = err as MissingProfileError;
        expect(e.missing).toBe("nope");
      }
    });
  });

  describe("R6/R7 — includes", () => {
    it("R6: applies a single component additively", async () => {
      fx = await makeFixture({
        profiles: {
          p: { manifest: { includes: ["compA"] }, files: { "p.txt": "p" } },
        },
        components: {
          compA: { files: { "ca.txt": "from compA" } },
        },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const paths = plan.files.map((f) => f.relPath);
      expect(paths).toContain("p.txt");
      expect(paths).toContain("ca.txt");
    });

    it("R6/R9: leaf profile is the last contributor; its includes precede it", async () => {
      // Per R9 worked example: base, extended, compA, compB, profile.
      fx = await makeFixture({
        profiles: {
          base: { manifest: {}, files: { "z": "z" } },
          extended: { manifest: { extends: "base" }, files: { "z": "z" } },
          leaf: {
            manifest: { extends: "extended", includes: ["compA", "compB"] },
            files: { "z": "z" },
          },
        },
        components: {
          compA: { files: { "z": "z" } },
          compB: { files: { "z": "z" } },
        },
      });
      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
      const ids = plan.contributors.map((c) => c.id);
      expect(ids).toEqual(["base", "extended", "compA", "compB", "leaf"]);
      // Profile is last
      expect(plan.contributors[plan.contributors.length - 1]!.kind).toBe("profile");
    });

    it("R7: throws MissingIncludeError for an unknown bare component", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["ghost"] } } },
      });
      try {
        await resolve("p", { projectRoot: fx.projectRoot });
        throw new Error("expected throw");
      } catch (err) {
        expect(err).toBeInstanceOf(MissingIncludeError);
        const e = err as MissingIncludeError;
        expect(e.raw).toBe("ghost");
        expect(e.referencedBy).toBe("p");
      }
    });

    it("R7: throws for a missing relative include", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["./neighbor"] } } },
      });
      await expect(resolve("p", { projectRoot: fx.projectRoot })).rejects.toThrow(
        MissingIncludeError,
      );
    });

    it("R7: throws for a missing absolute include", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["/this/does/not/exist/anywhere"] } } },
      });
      await expect(resolve("p", { projectRoot: fx.projectRoot })).rejects.toThrow(
        MissingIncludeError,
      );
    });
  });

  describe("R37 — include kinds", () => {
    it("classifies bare names as components", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["compA"] } } },
        components: { compA: { files: { f: "1" } } },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      expect(plan.includes[0]!.kind).toBe("component");
      expect(plan.includes[0]!.external).toBe(false);
    });

    it("classifies ./ prefix as relative, resolved from referencing profile dir", async () => {
      // Sibling profile dir referenced via relative path — simulate by
      // putting the include target in a sibling under .claude-profiles.
      fx = await makeFixture({
        profiles: {
          p: { manifest: { includes: ["./../sib"] } },
          sib: { manifest: {}, files: { "x": "x" } },
        },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const ref = plan.includes[0]!;
      expect(ref.kind).toBe("relative");
      // Resolves to .claude-profiles/sib
      expect(ref.resolvedPath).toMatch(/sib$/);
    });

    it("classifies absolute paths inside project root as absolute (non-external)", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: {} }, sib: { manifest: {}, files: { f: "1" } } },
      });
      const sibAbs = path.join(fx.projectRoot, ".claude-profiles", "sib");
      // Mutate the manifest by re-writing fixture (we can use absolute path
      // directly since it's within project root).
      const { promises: fsp } = await import("node:fs");
      await fsp.writeFile(
        path.join(fx.projectRoot, ".claude-profiles", "p", "profile.json"),
        JSON.stringify({ includes: [sibAbs] }),
      );
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      expect(plan.includes[0]!.kind).toBe("absolute");
      expect(plan.includes[0]!.external).toBe(false);
    });

    it("flags external absolute paths via the external flag (kind stays absolute)", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: {} } },
        external: { ext1: { files: { "a": "1" } } },
      });
      const extAbs = path.join(fx.externalRoot, "ext1");
      const { promises: fsp } = await import("node:fs");
      await fsp.writeFile(
        path.join(fx.projectRoot, ".claude-profiles", "p", "profile.json"),
        JSON.stringify({ includes: [extAbs] }),
      );
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const ref = plan.includes[0]!;
      expect(ref.external).toBe(true);
      expect(ref.kind).toBe("absolute");
    });
  });

  describe("R37a — external trust", () => {
    it("populates externalPaths once per external resolved path", async () => {
      // Use settings.json (mergeable) so duplicate includes of the same
      // external dir don't trip R11; the test is about dedup of the trust
      // notice list, not about conflict semantics.
      fx = await makeFixture({
        profiles: { p: { manifest: {} } },
        external: { ext1: { files: { "settings.json": "{}" } } },
      });
      const extAbs = path.join(fx.externalRoot, "ext1");
      const { promises: fsp } = await import("node:fs");
      await fsp.writeFile(
        path.join(fx.projectRoot, ".claude-profiles", "p", "profile.json"),
        JSON.stringify({ includes: [extAbs, extAbs] }),
      );
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      expect(plan.externalPaths.length).toBe(1);
      expect(plan.externalPaths[0]!.resolvedPath).toBe(path.resolve(extAbs));
    });

    it("does not surface in-repo includes as external", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["compA"] } } },
        components: { compA: { files: { "f": "1" } } },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      expect(plan.externalPaths).toEqual([]);
    });
  });

  describe("R11 — conflict detection", () => {
    it("R11: two includes defining the same non-mergeable file → ConflictError", async () => {
      fx = await makeFixture({
        profiles: {
          p: { manifest: { includes: ["a", "b"] } },
        },
        components: {
          a: { files: { "agents/foo.json": "{}" } },
          b: { files: { "agents/foo.json": "{}" } },
        },
      });
      try {
        await resolve("p", { projectRoot: fx.projectRoot });
        throw new Error("expected throw");
      } catch (err) {
        expect(err).toBeInstanceOf(ConflictError);
        const e = err as ConflictError;
        expect(e.relPath).toBe("agents/foo.json");
        expect(e.contributors.sort()).toEqual(["a", "b"]);
      }
    });

    it("R11: include + extends ancestor on same non-mergeable path → ConflictError", async () => {
      fx = await makeFixture({
        profiles: {
          base: { manifest: {}, files: { "agents/x.json": "{}" } },
          p: { manifest: { extends: "base", includes: ["compA"] } },
        },
        components: {
          compA: { files: { "agents/x.json": "{}" } },
        },
      });
      await expect(resolve("p", { projectRoot: fx.projectRoot })).rejects.toThrow(
        ConflictError,
      );
    });

    it("R11: profile itself overrides — no conflict even with two includes contributing the path", async () => {
      fx = await makeFixture({
        profiles: {
          p: {
            manifest: { includes: ["a", "b"] },
            files: { "agents/foo.json": "{}" },
          },
        },
        components: {
          a: { files: { "agents/foo.json": "{}" } },
          b: { files: { "agents/foo.json": "{}" } },
        },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      // No throw. All three contributors should appear.
      const fooFiles = plan.files.filter((f) => f.relPath === "agents/foo.json");
      expect(fooFiles.length).toBe(3);
    });

    it("R11: ancestor-vs-ancestor on non-mergeable path is NOT a conflict (R10 last-wins)", async () => {
      fx = await makeFixture({
        profiles: {
          base: { manifest: {}, files: { "agents/x.json": "{}" } },
          mid: { manifest: { extends: "base" }, files: { "agents/x.json": "{}" } },
          leaf: { manifest: { extends: "mid" } },
        },
      });
      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
      const xs = plan.files.filter((f) => f.relPath === "agents/x.json");
      expect(xs.length).toBe(2);
    });

    it("R11: mergeable files (settings.json) do NOT trigger conflict even with multiple includes", async () => {
      fx = await makeFixture({
        profiles: {
          p: { manifest: { includes: ["a", "b"] } },
        },
        components: {
          a: { files: { "settings.json": "{}" } },
          b: { files: { "settings.json": "{}" } },
        },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const settings = plan.files.filter((f) => f.relPath === "settings.json");
      expect(settings.length).toBe(2);
    });

    it("R11: mergeable files (CLAUDE.md) do NOT trigger conflict even with two includes", async () => {
      fx = await makeFixture({
        profiles: {
          p: { manifest: { includes: ["a", "b"] } },
        },
        components: {
          a: { files: { "CLAUDE.md": "from a" } },
          b: { files: { "CLAUDE.md": "from b" } },
        },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const md = plan.files.filter((f) => f.relPath === "CLAUDE.md");
      expect(md.length).toBe(2);
    });
  });

  describe("R35/R36 — manifest validation", () => {
    it("R35: accepts all optional fields", async () => {
      fx = await makeFixture({
        profiles: {
          p: {
            manifest: {
              name: "p-renamed",
              description: "desc",
              extends: undefined,
              includes: [],
              tags: ["t1", "t2"],
            },
          },
        },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      expect(plan.warnings.filter((w) => w.code === "UnknownManifestField")).toEqual([]);
    });

    it("R36: unknown field produces a warning, does not abort", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { extraField: 42 } as Record<string, unknown> } },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const warns = plan.warnings.filter((w) => w.code === "UnknownManifestField");
      expect(warns.length).toBe(1);
      expect(warns[0]!.message).toContain("extraField");
      expect(warns[0]!.source).toBe("p");
    });

    it("R36: missing manifest is treated as defaults with a warning", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: null, files: { "a": "1" } } },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const warns = plan.warnings.filter((w) => w.code === "MissingManifest");
      expect(warns.length).toBe(1);
    });

    it("aborts on unparseable JSON (distinct from R36 which is recoverable)", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: "{not valid json" } },
      });
      await expect(resolve("p", { projectRoot: fx.projectRoot })).rejects.toThrow(
        InvalidManifestError,
      );
    });

    it("aborts on type-mismatched fields", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { extends: 42 } as unknown as Record<string, unknown> } },
      });
      await expect(resolve("p", { projectRoot: fx.projectRoot })).rejects.toThrow(
        InvalidManifestError,
      );
    });

    it("aborts when includes is not an array of strings", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["ok", 42] } as Record<string, unknown> } },
      });
      await expect(resolve("p", { projectRoot: fx.projectRoot })).rejects.toThrow(
        InvalidManifestError,
      );
    });
  });

  describe("ordering / R9 worked example", () => {
    it("matches the R9 canonical concat order: base, extended, compA, compB, profile", async () => {
      fx = await makeFixture({
        profiles: {
          base: { manifest: {}, files: { "CLAUDE.md": "BASE\n" } },
          extended: {
            manifest: { extends: "base" },
            files: { "CLAUDE.md": "EXTENDED\n" },
          },
          leaf: {
            manifest: { extends: "extended", includes: ["compA", "compB"] },
            files: { "CLAUDE.md": "LEAF\n" },
          },
        },
        components: {
          compA: { files: { "CLAUDE.md": "COMPA\n" } },
          compB: { files: { "CLAUDE.md": "COMPB\n" } },
        },
      });
      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
      const orderedIds = plan.files
        .filter((f) => f.relPath === "CLAUDE.md")
        .sort((a, b) => a.contributorIndex - b.contributorIndex)
        .map((f) => plan.contributors[f.contributorIndex]!.id);
      expect(orderedIds).toEqual(["base", "extended", "compA", "compB", "leaf"]);
    });
  });

  describe("ordering / ancestor-declared includes", () => {
    // The R9 worked example only constrains the leaf's includes ordering.
    // The resolver's interpretation for ancestors: each ancestor profile's
    // includes are emitted immediately *after* that ancestor in the
    // contributor sequence, before the next descendant.
    it("emits an ancestor's includes immediately after the ancestor", async () => {
      fx = await makeFixture({
        profiles: {
          base: {
            manifest: { includes: ["compBase"] },
            files: { "z": "z-base" },
          },
          mid: {
            manifest: { extends: "base" },
            files: { "z": "z-mid" },
          },
          leaf: {
            manifest: { extends: "mid", includes: ["compLeaf"] },
            files: { "z": "z-leaf" },
          },
        },
        components: {
          compBase: { files: { "z": "z-compBase" } },
          compLeaf: { files: { "z": "z-compLeaf" } },
        },
      });
      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
      const ids = plan.contributors.map((c) => c.id);
      // base, base's includes, mid, leaf's includes, leaf
      expect(ids).toEqual(["base", "compBase", "mid", "compLeaf", "leaf"]);
      const kinds = plan.contributors.map((c) => c.kind);
      expect(kinds).toEqual(["ancestor", "include", "ancestor", "include", "profile"]);
    });
  });

  describe("ResolvedPlan.schemaVersion", () => {
    it("stamps schemaVersion on every plan", async () => {
      fx = await makeFixture({ profiles: { p: { manifest: {} } } });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      expect(plan.schemaVersion).toBe(1);
    });
  });

  describe("Contributor.manifest passthrough", () => {
    it("attaches the parsed manifest to ancestor and profile contributors", async () => {
      fx = await makeFixture({
        profiles: {
          base: { manifest: { description: "the base", tags: ["b"] } },
          leaf: {
            manifest: { extends: "base", description: "the leaf", tags: ["l"] },
          },
        },
      });
      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
      const baseC = plan.contributors.find((c) => c.id === "base");
      const leafC = plan.contributors.find((c) => c.id === "leaf");
      expect(baseC?.manifest?.description).toBe("the base");
      expect(baseC?.manifest?.tags).toEqual(["b"]);
      expect(leafC?.manifest?.description).toBe("the leaf");
    });

    it("does not attach a manifest to include contributors", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["compA"] } } },
        components: { compA: { files: { "f": "1" } } },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const inc = plan.contributors.find((c) => c.kind === "include");
      expect(inc?.manifest).toBeUndefined();
    });
  });

  describe("PlanFile.mergePolicy", () => {
    it("classifies each PlanFile by its policy", async () => {
      fx = await makeFixture({
        profiles: {
          p: {
            manifest: {},
            files: {
              "settings.json": "{}",
              "CLAUDE.md": "x",
              "agents/foo.json": "{}",
            },
          },
        },
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      const byPath = Object.fromEntries(plan.files.map((f) => [f.relPath, f.mergePolicy]));
      expect(byPath["settings.json"]).toBe("deep-merge");
      expect(byPath["CLAUDE.md"]).toBe("concat");
      expect(byPath["agents/foo.json"]).toBe("last-wins");
    });
  });

  describe("error messages quality (§7 quality bar)", () => {
    it("ConflictError names the path and both contributors in the message", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["a", "b"] } } },
        components: {
          a: { files: { "agents/foo.json": "{}" } },
          b: { files: { "agents/foo.json": "{}" } },
        },
      });
      try {
        await resolve("p", { projectRoot: fx.projectRoot });
        throw new Error("expected throw");
      } catch (err) {
        const e = err as ConflictError;
        expect(e.message).toContain("agents/foo.json");
        expect(e.message).toContain("a");
        expect(e.message).toContain("b");
      }
    });

    it("MissingIncludeError names the raw, resolved path, and referencer", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: { includes: ["./not-here"] } } },
      });
      try {
        await resolve("p", { projectRoot: fx.projectRoot });
        throw new Error("expected throw");
      } catch (err) {
        const e = err as MissingIncludeError;
        expect(e.message).toContain("./not-here");
        expect(e.message).toContain("not-here");
        expect(e.message).toContain("p");
      }
    });
  });

  describe("warning aggregation across the chain", () => {
    it("aggregates UnknownManifestField warnings from every profile in the chain", async () => {
      fx = await makeFixture({
        profiles: {
          base: {
            manifest: { weirdBase: 1 } as Record<string, unknown>,
          },
          leaf: {
            manifest: { extends: "base", weirdLeaf: 2 } as Record<string, unknown>,
          },
        },
      });
      const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
      const sources = plan.warnings
        .filter((w) => w.code === "UnknownManifestField")
        .map((w) => w.source);
      expect(new Set(sources)).toEqual(new Set(["base", "leaf"]));
    });
  });

  describe("empty edge cases", () => {
    it("works for a profile with no manifest fields and no files", async () => {
      fx = await makeFixture({ profiles: { p: { manifest: {} } } });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      expect(plan.chain).toEqual(["p"]);
      expect(plan.files).toEqual([]);
    });

    it("works for a profile with no .claude/ directory", async () => {
      fx = await makeFixture({
        profiles: { p: { manifest: {} } }, // no files
      });
      const plan = await resolve("p", { projectRoot: fx.projectRoot });
      expect(plan.files).toEqual([]);
    });
  });
});
