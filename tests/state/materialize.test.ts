import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { MergedFile } from "../../src/merge/types.js";
import { resolve } from "../../src/resolver/resolve.js";
import type { ResolvedPlan } from "../../src/resolver/types.js";
import { pathExists } from "../../src/state/atomic.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";
import { readStateFile } from "../../src/state/state-file.js";
import { makeFixture } from "../helpers/fixture.js";

async function setup() {
  const fx = await makeFixture({
    profiles: {
      base: {
        manifest: { name: "base" },
        files: { "CLAUDE.md": "BASE\n", "agents/a.md": "AGENT-A" },
      },
      leaf: {
        manifest: { name: "leaf", extends: "base" },
        files: { "CLAUDE.md": "LEAF\n" },
      },
    },
  });
  const plan = await resolve("leaf", { projectRoot: fx.projectRoot });
  // For materialize tests we don't actually need to run the merger; we
  // construct simple MergedFiles manually so we can isolate E3 behavior.
  const merged: MergedFile[] = [
    { path: "CLAUDE.md", bytes: Buffer.from("BASE\nLEAF\n"), contributors: ["base", "leaf"], mergePolicy: "concat" },
    { path: "agents/a.md", bytes: Buffer.from("AGENT-A"), contributors: ["base"], mergePolicy: "last-wins" },
  ];
  return { fx, plan, merged };
}

describe("materialize (R13/R14/R16)", () => {
  let fx: Awaited<ReturnType<typeof setup>>["fx"] | undefined;
  let plan: ResolvedPlan;
  let merged: MergedFile[];
  beforeEach(async () => {
    const s = await setup();
    fx = s.fx;
    plan = s.plan;
    merged = s.merged;
  });
  afterEach(async () => {
    await fx?.cleanup();
  });

  it("writes merged files into the live .claude/", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("BASE\nLEAF\n");
    expect(
      await fs.readFile(path.join(paths.claudeDir, "agents/a.md"), "utf8"),
    ).toBe("AGENT-A");
  });

  it("R14: writes .state.json with schemaVersion + activeProfile", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    const r = await readStateFile(paths);
    expect(r.warning).toBeNull();
    expect(r.state.schemaVersion).toBe(1);
    expect(r.state.activeProfile).toBe("leaf");
    expect(typeof r.state.materializedAt).toBe("string");
    expect(Object.keys(r.state.fingerprint.files).sort()).toEqual([
      "CLAUDE.md",
      "agents/a.md",
    ]);
  });

  it("R16: cleans up .pending/ and .prior/ on success", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    expect(await pathExists(paths.pendingDir)).toBe(false);
    expect(await pathExists(paths.priorDir)).toBe(false);
  });

  it("R16: replaces an existing .claude/ atomically", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    // Pre-create a stale live tree.
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "STALE.md"), "STALE");
    await materialize(paths, plan, merged);
    // Stale file should be gone (replaced via rename).
    expect(await pathExists(path.join(paths.claudeDir, "STALE.md"))).toBe(false);
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("BASE\nLEAF\n");
  });

  it("R16a: reconciles a leftover .pending/ before materializing", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await fs.mkdir(paths.pendingDir, { recursive: true });
    await fs.writeFile(path.join(paths.pendingDir, "STALE_PENDING"), "x");
    await materialize(paths, plan, merged);
    // The stale pending file must not survive into the live tree.
    expect(await pathExists(path.join(paths.claudeDir, "STALE_PENDING"))).toBe(false);
  });

  it("records fingerprint with size + content hash + mtime", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    const r = await readStateFile(paths);
    const e = r.state.fingerprint.files["CLAUDE.md"]!;
    expect(e.size).toBe(Buffer.from("BASE\nLEAF\n").length);
    expect(e.contentHash.length).toBe(64); // sha256 hex
    expect(e.mtimeMs).toBeGreaterThan(0);
  });

  it("R37a: records external trust notice for plan.externalPaths once", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    const planWithExt: ResolvedPlan = {
      ...plan,
      externalPaths: [
        { raw: "~/external", resolvedPath: "/abs/external" },
      ],
    };
    await materialize(paths, planWithExt, merged);
    const r1 = await readStateFile(paths);
    expect(r1.state.externalTrustNotices).toHaveLength(1);
    expect(r1.state.externalTrustNotices[0]?.resolvedPath).toBe("/abs/external");

    // Re-materialize with the same external — should not duplicate.
    await materialize(paths, planWithExt, merged);
    const r2 = await readStateFile(paths);
    expect(r2.state.externalTrustNotices).toHaveLength(1);
  });
});
