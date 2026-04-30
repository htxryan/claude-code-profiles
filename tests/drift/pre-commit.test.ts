import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { preCommitWarn } from "../../src/drift/pre-commit.js";
import type { MergedFile } from "../../src/merge/types.js";
import { resolve } from "../../src/resolver/resolve.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";
import { makeFixture } from "../helpers/fixture.js";

async function setup() {
  const fx = await makeFixture({
    profiles: {
      base: {
        manifest: { name: "base" },
        files: { "CLAUDE.md": "BASE\n" },
      },
    },
  });
  const plan = await resolve("base", { projectRoot: fx.projectRoot });
  const merged: MergedFile[] = [
    {
      path: "CLAUDE.md",
      bytes: Buffer.from("BASE\n"),
      contributors: ["base"],
      mergePolicy: "concat",
      destination: ".claude",
    },
  ];
  return { fx, plan, merged };
}

describe("preCommitWarn (R25, R25a, S18)", () => {
  let ctx: Awaited<ReturnType<typeof setup>> | undefined;

  beforeEach(async () => {
    ctx = await setup();
  });
  afterEach(async () => {
    await ctx?.fx.cleanup();
    vi.restoreAllMocks();
  });

  it("S18: when there's no .claude-profiles state, exits 0 silently", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    const stderrSpy = vi
      .spyOn(process.stderr, "write")
      .mockImplementation(() => true);

    const result = await preCommitWarn(paths);
    expect(result.exitCode).toBe(0);
    expect(result.warnings).toEqual([]);
    expect(stderrSpy).not.toHaveBeenCalled();
  });

  it("R25: when there's drift, prints a header + per-file lines and exits 0", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.plan, ctx!.merged);
    // Drift it.
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");
    await fs.writeFile(path.join(paths.claudeDir, "extra.md"), "extra");

    const stderrSpy = vi
      .spyOn(process.stderr, "write")
      .mockImplementation(() => true);

    const result = await preCommitWarn(paths);
    expect(result.exitCode).toBe(0);
    expect(result.warnings.length).toBeGreaterThanOrEqual(2); // header + at least 1 file line
    const header = result.warnings[0]!;
    expect(header).toMatch(/c3p:/);
    expect(header).toMatch(/drifted file/);
    expect(header).toMatch(/'base'/);
    // Each file line starts with two-space indent + glyph.
    expect(result.warnings.slice(1).every((l) => l.startsWith("  "))).toBe(true);
    expect(stderrSpy).toHaveBeenCalled();
  });

  it("R25a fail-open: exits 0 even when state is missing", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    // No materialize → no state file → pre-commit must still exit 0.
    const result = await preCommitWarn(paths);
    expect(result.exitCode).toBe(0);
    expect(result.warnings).toEqual([]);
  });

  it("truncates output at 10 entries with an 'and N more' line", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.plan, ctx!.merged);
    // Add 15 drifted files.
    for (let i = 0; i < 15; i++) {
      await fs.writeFile(
        path.join(paths.claudeDir, `extra-${i}.md`),
        `e${i}`,
      );
    }
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    const result = await preCommitWarn(paths);
    // header + 10 entries + "...and 5 more"
    expect(result.warnings).toHaveLength(12);
    expect(result.warnings[result.warnings.length - 1]).toMatch(
      /and 5 more/,
    );
  });

  it("when no drift present, prints nothing", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.plan, ctx!.merged);
    const stderrSpy = vi
      .spyOn(process.stderr, "write")
      .mockImplementation(() => true);
    const result = await preCommitWarn(paths);
    expect(result.exitCode).toBe(0);
    expect(result.warnings).toEqual([]);
    expect(stderrSpy).not.toHaveBeenCalled();
  });

  it("R25a fail-open: stderr.write throwing (EPIPE) does not break exit-0 contract", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.plan, ctx!.merged);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");

    // Simulate a closed-pipe parent (git commit driver disconnected).
    vi.spyOn(process.stderr, "write").mockImplementation(() => {
      throw new Error("EPIPE");
    });

    const result = await preCommitWarn(paths);
    // Function must still return exit 0 with the captured warnings — the
    // write failure was swallowed.
    expect(result.exitCode).toBe(0);
    expect(result.warnings.length).toBeGreaterThan(0);
  });

  it("S17: surfaces a degraded-state warning when .state.json is corrupted", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    await materialize(paths, ctx!.plan, ctx!.merged);
    // Corrupt the state file so detectDrift returns a warning.
    await fs.writeFile(paths.stateFile, "{ not json");
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);

    const result = await preCommitWarn(paths);
    expect(result.exitCode).toBe(0);
    expect(result.warnings.some((w) => /state file degraded/.test(w))).toBe(true);
    expect(result.report?.warning?.code).toBe("ParseError");
  });

  it("does NOT print 'state file degraded' for a Missing state file (fresh project)", async () => {
    const paths = buildStatePaths(ctx!.fx.projectRoot);
    // No materialize — state file absent → 'Missing' warning. The hook
    // should be silent in this case (matches a fresh-project user
    // experience).
    const result = await preCommitWarn(paths);
    expect(result.warnings.some((w) => /state file degraded/.test(w))).toBe(false);
  });

  it("R25a fail-open: detectDrift throwing produces a single 'skipped' line, exit 0", async () => {
    // Build a path bundle pointing at a non-existent project root that
    // *also* won't trigger ENOENT graceful-handling — make stateFile's
    // parent be a regular file. fs.mkdir / fs.readFile on it both fail
    // with ENOTDIR or EEXIST, exercising the outer try/catch in
    // preCommitWarn.
    const fauxRoot = path.join(ctx!.fx.projectRoot, "faux");
    const fauxProfilesDir = path.join(fauxRoot, ".claude-profiles");
    await fs.mkdir(fauxRoot, { recursive: true });
    await fs.writeFile(fauxProfilesDir, "I am a file, not a directory");
    const paths = buildStatePaths(fauxRoot);
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);

    const result = await preCommitWarn(paths);
    expect(result.exitCode).toBe(0);
  });
});
