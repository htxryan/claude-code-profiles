import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { applyGate } from "../../src/drift/apply.js";
import type { MergedFile } from "../../src/merge/types.js";
import { resolve } from "../../src/resolver/resolve.js";
import { pathExists } from "../../src/state/atomic.js";
import { listSnapshots } from "../../src/state/backup.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";
import { readStateFile } from "../../src/state/state-file.js";
import { makeFixture } from "../helpers/fixture.js";

async function setupTwoProfiles() {
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
      other: {
        manifest: { name: "other" },
        files: { "CLAUDE.md": "OTHER\n", "agents/b.md": "AGENT-B" },
      },
    },
  });
  const leafPlan = await resolve("leaf", { projectRoot: fx.projectRoot });
  const leafMerged: MergedFile[] = [
    {
      path: "CLAUDE.md",
      bytes: Buffer.from("BASE\nLEAF\n"),
      contributors: ["base", "leaf"],
      mergePolicy: "concat",
      destination: ".claude",
    },
    {
      path: "agents/a.md",
      bytes: Buffer.from("AGENT-A"),
      contributors: ["base"],
      mergePolicy: "last-wins",
      destination: ".claude",
    },
  ];
  const otherPlan = await resolve("other", { projectRoot: fx.projectRoot });
  const otherMerged: MergedFile[] = [
    {
      path: "CLAUDE.md",
      bytes: Buffer.from("OTHER\n"),
      contributors: ["other"],
      mergePolicy: "concat",
      destination: ".claude",
    },
    {
      path: "agents/b.md",
      bytes: Buffer.from("AGENT-B"),
      contributors: ["other"],
      mergePolicy: "last-wins",
      destination: ".claude",
    },
  ];
  return { fx, leafPlan, leafMerged, otherPlan, otherMerged };
}

describe("applyGate (R22, R22a, R22b, R23, R23a, R24)", () => {
  let ctx: Awaited<ReturnType<typeof setupTwoProfiles>> | undefined;

  beforeEach(async () => {
    ctx = await setupTwoProfiles();
  });
  afterEach(async () => {
    await ctx?.fx.cleanup();
  });

  it("R24: abort makes no FS changes", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    const before = await fs.readFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "utf8",
    );
    const result = await applyGate("abort", {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: "leaf",
    });
    expect(result.action).toBe("aborted");
    expect(result.materializeResult).toBeNull();
    const after = await fs.readFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "utf8",
    );
    expect(after).toBe(before);
  });

  it("no-drift-proceed: materializes without taking a snapshot", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    const result = await applyGate("no-drift-proceed", {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: "leaf",
    });
    expect(result.action).toBe("materialized");
    expect(result.backupSnapshot).toBeNull();
    const liveContent = await fs.readFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "utf8",
    );
    expect(liveContent).toBe("OTHER\n");
    // No backup taken on the no-drift path.
    const snapshots = await listSnapshots(paths);
    expect(snapshots).toEqual([]);
  });

  it("R23 + R23a: discard takes a backup snapshot before materializing", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    // Drift the live tree.
    await fs.writeFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "DRIFTED\n",
    );

    const result = await applyGate("discard", {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: "leaf",
    });
    expect(result.action).toBe("materialized");
    expect(result.backupSnapshot).not.toBeNull();
    expect(await pathExists(result.backupSnapshot!)).toBe(true);
    // The snapshot must contain the drifted content (taken before materialize).
    const snapshotClaudeMd = await fs.readFile(
      path.join(result.backupSnapshot!, "CLAUDE.md"),
      "utf8",
    );
    expect(snapshotClaudeMd).toBe("DRIFTED\n");
    // Live tree is now the new profile.
    const liveContent = await fs.readFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "utf8",
    );
    expect(liveContent).toBe("OTHER\n");
  });

  it("R22 + R22a + R22b: persist copies live tree into active profile, then materializes new", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    // User scratches an extra file into the live tree.
    await fs.writeFile(
      path.join(paths.claudeDir, "scratch.md"),
      "scratch from live\n",
    );
    await fs.writeFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "EDITED LEAF\n",
    );

    const result = await applyGate("persist", {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: "leaf",
    });
    expect(result.action).toBe("persisted-and-materialized");

    // The active profile (leaf) should now hold the persisted live tree.
    const persistedDir = path.join(
      ctx!.fx.projectRoot,
      ".claude-profiles",
      "leaf",
      ".claude",
    );
    expect(
      await fs.readFile(path.join(persistedDir, "CLAUDE.md"), "utf8"),
    ).toBe("EDITED LEAF\n");
    expect(
      await fs.readFile(path.join(persistedDir, "scratch.md"), "utf8"),
    ).toBe("scratch from live\n");

    // Live tree is now the new profile.
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("OTHER\n");
    // State now reflects the new profile.
    const r = await readStateFile(paths);
    expect(r.state.activeProfile).toBe("other");
  });

  it("persist with no active profile throws (defense-in-depth)", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await expect(
      applyGate("persist", {
        paths,
        plan: ctx!.otherPlan,
        merged: ctx!.otherMerged,
        activeProfileName: null,
      }),
    ).rejects.toThrow(/persist gate choice requires an active profile/);
  });
});
