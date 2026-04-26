/**
 * End-to-end gate scenario tests (epic fitness function): S3, S4, S6 stay
 * green; persist split-brain (S15-extension) stays green across SIGINT
 * injection.
 *
 * These integrate detect → decideGate → applyGate as the swap orchestrator
 * (E5) will compose them. Per-step unit coverage lives in detect.test.ts,
 * gate.test.ts, apply.test.ts.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { applyGate } from "../../src/drift/apply.js";
import { detectDrift } from "../../src/drift/detect.js";
import { decideGate } from "../../src/drift/gate.js";
import type { MergedFile } from "../../src/merge/types.js";
import { resolve } from "../../src/resolver/resolve.js";
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
        files: { "CLAUDE.md": "OTHER\n" },
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
  ];
  return { fx, leafPlan, leafMerged, otherPlan, otherMerged };
}

describe("E4 integration scenarios", () => {
  let ctx: Awaited<ReturnType<typeof setupTwoProfiles>> | undefined;

  beforeEach(async () => {
    ctx = await setupTwoProfiles();
  });
  afterEach(async () => {
    await ctx?.fx.cleanup();
  });

  it("S3: drift gate — discard → live tree replaced, edits in snapshot", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    await fs.writeFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "DRIFT FROM USER\n",
    );

    const report = await detectDrift(paths);
    expect(report.entries).toHaveLength(1);

    const decision = decideGate({ report, mode: "interactive" });
    // Interactive + drift + no flag → prompt. Simulate the user picking
    // discard.
    expect(decision.kind).toBe("prompt");
    const result = await applyGate("discard", {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: report.active,
    });

    expect(result.action).toBe("materialized");
    expect(result.backupSnapshot).not.toBeNull();
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("OTHER\n");
    expect(
      await fs.readFile(path.join(result.backupSnapshot!, "CLAUDE.md"), "utf8"),
    ).toBe("DRIFT FROM USER\n");
    const snaps = await listSnapshots(paths);
    expect(snaps).toHaveLength(1);
  });

  it("S4: drift gate — persist → live tree saved into active profile, then swap", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    // User adds a brand-new file plus edits CLAUDE.md.
    await fs.writeFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "EDITED LEAF\n",
    );
    await fs.writeFile(path.join(paths.claudeDir, "scratch.md"), "scratch\n");

    const report = await detectDrift(paths);
    expect(report.entries.map((e) => e.relPath).sort()).toEqual([
      "CLAUDE.md",
      "scratch.md",
    ]);

    const result = await applyGate("persist", {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: report.active,
    });
    expect(result.action).toBe("persisted-and-materialized");

    // Active profile dir now reflects live state at moment of persist.
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
    ).toBe("scratch\n");
    // Live tree is now the new profile.
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("OTHER\n");
    // No snapshot taken on persist path (only discard takes one).
    const snaps = await listSnapshots(paths);
    expect(snaps).toEqual([]);
  });

  it("S6: drift gate — abort → no FS change, state unchanged", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    await fs.writeFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "DRIFT\n",
    );
    const stateBefore = await readStateFile(paths);
    const beforeBytes = await fs.readFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "utf8",
    );

    const report = await detectDrift(paths);
    const result = await applyGate("abort", {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: report.active,
    });
    expect(result.action).toBe("aborted");

    const stateAfter = await readStateFile(paths);
    expect(stateAfter.state.activeProfile).toBe(stateBefore.state.activeProfile);
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe(beforeBytes);
  });

  it("S15-extension: persist split-brain — if killed mid-flow, next swap reconciles cleanly", async () => {
    // The persist transactional pair (R22b) writes via pending/prior. If the
    // process dies between persist completion and materialize, the next CLI
    // run reconciles. We simulate this by leaving a `.pending/` dir under
    // the active profile and verifying that a follow-up persist produces a
    // consistent result.
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    await fs.writeFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "EDITED\n",
    );

    // Inject a stale persist-side `.pending/` to simulate a half-finished
    // prior run.
    const profileDir = path.join(
      ctx!.fx.projectRoot,
      ".claude-profiles",
      "leaf",
    );
    const stalePending = path.join(profileDir, ".pending");
    await fs.mkdir(stalePending, { recursive: true });
    await fs.writeFile(path.join(stalePending, "STALE_GHOST"), "ghost");

    // Now perform the persist gate normally; the orchestration should
    // reconcile (drop stale .pending) before staging fresh content. We don't
    // expect any STALE_GHOST in the final persisted profile.
    const result = await applyGate("persist", {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: "leaf",
    });
    expect(result.action).toBe("persisted-and-materialized");

    const persistedDir = path.join(profileDir, ".claude");
    const ghost = path.join(persistedDir, "STALE_GHOST");
    await expect(fs.stat(ghost)).rejects.toMatchObject({ code: "ENOENT" });
    expect(
      await fs.readFile(path.join(persistedDir, "CLAUDE.md"), "utf8"),
    ).toBe("EDITED\n");
  });

  it("non-interactive auto-abort flow: drift + non-interactive + no flag → no change", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    await fs.writeFile(
      path.join(paths.claudeDir, "CLAUDE.md"),
      "DRIFT\n",
    );

    const report = await detectDrift(paths);
    const decision = decideGate({ report, mode: "non-interactive" });
    expect(decision.kind).toBe("auto");
    expect(decision.choice).toBe("abort");

    const result = await applyGate(decision.choice!, {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: report.active,
    });
    expect(result.action).toBe("aborted");
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("DRIFT\n");
  });

  it("clean-swap flow: no drift → no-drift-proceed → materialize", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.leafPlan, ctx!.leafMerged);
    // No edits — clean swap.

    const report = await detectDrift(paths);
    expect(report.entries).toEqual([]);
    const decision = decideGate({ report, mode: "interactive" });
    expect(decision.kind).toBe("no-drift");
    expect(decision.choice).toBe("no-drift-proceed");

    const result = await applyGate(decision.choice!, {
      paths,
      plan: ctx!.otherPlan,
      merged: ctx!.otherMerged,
      activeProfileName: report.active,
    });
    expect(result.action).toBe("materialized");
    expect(result.backupSnapshot).toBeNull();
    const r = await readStateFile(paths);
    expect(r.state.activeProfile).toBe("other");
  });
});
