/**
 * yd8 / AC-2: pre-swap plan summary tests. Validates the diff math against
 * controlled merged-file inputs and live-tree fixtures.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  formatPlanSummaryLine,
  summarizePlan,
} from "../../src/cli/plan-summary.js";
import type { MergedFile } from "../../src/merge/types.js";
import { buildStatePaths } from "../../src/state/paths.js";
import { makeFixture, type Fixture } from "../helpers/fixture.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

function mf(p: string, body: string): MergedFile {
  return {
    path: p,
    bytes: Buffer.from(body, "utf8"),
    contributors: ["x"],
    mergePolicy: "last-wins",
    destination: ".claude",
  };
}

describe("summarizePlan — yd8 AC-2", () => {
  it("zero diff when merged matches the live tree exactly", async () => {
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "x.md"), "X\n");
    const summary = await summarizePlan(paths, [mf("x.md", "X\n")]);
    expect(summary).toMatchObject({
      replace: 0,
      add: 0,
      delete: 0,
      bytesAdded: 0,
      bytesRemoved: 0,
    });
    expect(formatPlanSummaryLine(summary)).toBeNull();
  });

  it("counts adds, deletes, and replaces with byte deltas", async () => {
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);
    await fs.mkdir(paths.claudeDir, { recursive: true });
    // live: a.md (5 bytes), b.md (3 bytes)
    await fs.writeFile(path.join(paths.claudeDir, "a.md"), "AAAAA");
    await fs.writeFile(path.join(paths.claudeDir, "b.md"), "BBB");
    // merged: a.md (different bytes, 6 bytes), c.md (new, 4 bytes); b.md gone.
    const summary = await summarizePlan(paths, [
      mf("a.md", "AAAAAA"),
      mf("c.md", "CCCC"),
    ]);
    expect(summary.replace).toBe(1);
    expect(summary.add).toBe(1);
    expect(summary.delete).toBe(1);
    // bytesAdded: full new size of replaced (6) + new size of added (4) = 10
    expect(summary.bytesAdded).toBe(10);
    // bytesRemoved: full live size of replaced (5) + size of deleted (3) = 8
    expect(summary.bytesRemoved).toBe(8);
    expect(summary.replaceSample).toEqual(["a.md"]);
    expect(summary.addSample).toEqual(["c.md"]);
    expect(summary.deleteSample).toEqual(["b.md"]);
  });

  it("renders human-readable line for non-empty summary", async () => {
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "a.md"), "old");
    const summary = await summarizePlan(paths, [mf("a.md", "newcontent")]);
    const line = formatPlanSummaryLine(summary);
    expect(line).toContain("replace 1");
    expect(line).toContain("add 0");
    expect(line).toContain("delete 0");
    expect(line).toContain("+10");
    expect(line).toContain("-3");
  });

  it("ignores projectRoot-destination merged entries (CLAUDE.md handled separately)", async () => {
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "x.md"), "X\n");
    const summary = await summarizePlan(paths, [
      mf("x.md", "X\n"),
      {
        path: "CLAUDE.md",
        bytes: Buffer.from("body\n"),
        contributors: ["x"],
        mergePolicy: "last-wins",
        destination: "projectRoot",
      },
    ]);
    expect(summary.add).toBe(0);
    expect(summary.replace).toBe(0);
    expect(summary.delete).toBe(0);
  });

  it("walks nested directories", async () => {
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);
    await fs.mkdir(path.join(paths.claudeDir, "agents"), { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "agents", "foo.md"), "F\n");
    const summary = await summarizePlan(paths, [
      mf("agents/foo.md", "F\n"), // unchanged
      mf("agents/bar.md", "B\n"), // added
    ]);
    expect(summary.add).toBe(1);
    expect(summary.replace).toBe(0);
    expect(summary.delete).toBe(0);
    expect(summary.addSample).toEqual(["agents/bar.md"]);
  });

  it("handles an absent .claude/ directory as 'all-add'", async () => {
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);
    // Don't create paths.claudeDir.
    const summary = await summarizePlan(paths, [mf("a.md", "A")]);
    expect(summary.add).toBe(1);
    expect(summary.delete).toBe(0);
    expect(summary.bytesAdded).toBe(1);
  });
});
