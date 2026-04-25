import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { detectDrift } from "../../src/drift/detect.js";
import type { MergedFile } from "../../src/merge/types.js";
import { resolve } from "../../src/resolver/resolve.js";
import type { ResolvedPlan } from "../../src/resolver/types.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";
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
  const merged: MergedFile[] = [
    {
      path: "CLAUDE.md",
      bytes: Buffer.from("BASE\nLEAF\n"),
      contributors: ["base", "leaf"],
      mergePolicy: "concat",
    },
    {
      path: "agents/a.md",
      bytes: Buffer.from("AGENT-A"),
      contributors: ["base"],
      mergePolicy: "last-wins",
    },
  ];
  return { fx, plan, merged };
}

describe("detectDrift (R18, R19, R20)", () => {
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

  it("returns fingerprintOk:false when state has no active profile", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    const report = await detectDrift(paths);
    expect(report.fingerprintOk).toBe(false);
    expect(report.active).toBeNull();
    expect(report.entries).toEqual([]);
    expect(report.scannedFiles).toBe(0);
  });

  it("returns no entries when live tree matches recorded fingerprint", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    const report = await detectDrift(paths);
    expect(report.fingerprintOk).toBe(true);
    expect(report.active).toBe("leaf");
    expect(report.entries).toEqual([]);
    expect(report.scannedFiles).toBe(2);
    // Both files should hit the fast path (mtime+size unchanged).
    expect(report.fastPathHits).toBe(2);
    expect(report.slowPathHits).toBe(0);
  });

  it("R19: detects modified files (slow-path hash check)", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);

    // Change content but keep length identical so size matches → forces hash.
    const target = path.join(paths.claudeDir, "CLAUDE.md");
    const recorded = await fs.stat(target);
    await fs.writeFile(target, Buffer.from("XXXX\nYYYY\n"));
    // Restore mtime so the metadata fast-path *would* claim unchanged — that
    // forces the slow-path branch only via size or hash divergence. To
    // reliably hit the slow path we change content AND mtime.
    await fs.utimes(target, recorded.atime, new Date(recorded.mtimeMs + 1000));

    const report = await detectDrift(paths);
    expect(report.fingerprintOk).toBe(true);
    expect(report.entries).toHaveLength(1);
    expect(report.entries[0]).toMatchObject({
      relPath: "CLAUDE.md",
      status: "modified",
    });
    expect(report.slowPathHits).toBeGreaterThanOrEqual(1);
  });

  it("R19: detects added files", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    await fs.writeFile(path.join(paths.claudeDir, "scratch.md"), "scratch");

    const report = await detectDrift(paths);
    expect(report.entries).toHaveLength(1);
    expect(report.entries[0]).toMatchObject({
      relPath: "scratch.md",
      status: "added",
    });
  });

  it("R19: detects deleted files", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    await fs.unlink(path.join(paths.claudeDir, "agents/a.md"));

    const report = await detectDrift(paths);
    expect(report.entries).toHaveLength(1);
    expect(report.entries[0]).toMatchObject({
      relPath: "agents/a.md",
      status: "deleted",
    });
  });

  it("R20: per-entry provenance includes the recorded resolved sources", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "drifted\n");

    const report = await detectDrift(paths);
    expect(report.entries).toHaveLength(1);
    const entry = report.entries[0]!;
    // Provenance is the source-set granularity: contributors recorded at last
    // materialize. For leaf (extends base) we expect both ancestor and profile.
    const ids = entry.provenance.map((p) => p.id).sort();
    expect(ids).toEqual(["base", "leaf"]);
  });

  it("entries are lex-sorted by relPath", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    await materialize(paths, plan, merged);
    await fs.writeFile(path.join(paths.claudeDir, "z-late.md"), "z");
    await fs.writeFile(path.join(paths.claudeDir, "a-early.md"), "a");
    await fs.writeFile(path.join(paths.claudeDir, "m-middle.md"), "m");

    const report = await detectDrift(paths);
    const paths_ = report.entries.map((e) => e.relPath);
    expect(paths_).toEqual([...paths_].sort());
  });

  it("schemaVersion is the current constant", async () => {
    const paths = buildStatePaths(fx!.projectRoot);
    const report = await detectDrift(paths);
    expect(report.schemaVersion).toBe(1);
  });
});
