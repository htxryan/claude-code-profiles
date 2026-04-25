import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { MergedFile } from "../../src/merge/types.js";
import { resolve } from "../../src/resolver/resolve.js";
import { pathExists } from "../../src/state/atomic.js";
import { materialize } from "../../src/state/materialize.js";
import { buildStatePaths } from "../../src/state/paths.js";
import { persistAndMaterialize, persistLiveIntoProfile } from "../../src/state/persist.js";
import { makeFixture } from "../helpers/fixture.js";

async function setup() {
  const fx = await makeFixture({
    profiles: {
      base: {
        manifest: { name: "base" },
        files: { "CLAUDE.md": "BASE\n" },
      },
      v2: {
        manifest: { name: "v2", extends: "base" },
        files: { "CLAUDE.md": "V2\n" },
      },
    },
  });
  return fx;
}

describe("persist transactional pair (R22b)", () => {
  let fx: Awaited<ReturnType<typeof setup>>;
  beforeEach(async () => {
    fx = await setup();
  });
  afterEach(async () => {
    await fx.cleanup();
  });

  it("persistLiveIntoProfile copies live .claude/ into the profile dir", async () => {
    const paths = buildStatePaths(fx.projectRoot);
    // Stage a live `.claude/` simulating the user's edits.
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "USER-EDITS\n");
    await fs.writeFile(path.join(paths.claudeDir, "newfile.md"), "ADDED");

    await persistLiveIntoProfile(paths, "base");

    const profileClaude = path.join(paths.profilesDir, "base", ".claude");
    expect(
      await fs.readFile(path.join(profileClaude, "CLAUDE.md"), "utf8"),
    ).toBe("USER-EDITS\n");
    expect(
      await fs.readFile(path.join(profileClaude, "newfile.md"), "utf8"),
    ).toBe("ADDED");
    // pending/prior cleaned up.
    expect(await pathExists(path.join(paths.profilesDir, "base", ".pending"))).toBe(false);
    expect(await pathExists(path.join(paths.profilesDir, "base", ".prior"))).toBe(false);
  });

  it("persistAndMaterialize saves live state then swaps to new profile", async () => {
    const paths = buildStatePaths(fx.projectRoot);

    // Initial materialize of "base"
    const basePlan = await resolve("base", { projectRoot: fx.projectRoot });
    const baseMerged: MergedFile[] = [
      { path: "CLAUDE.md", bytes: Buffer.from("BASE\n"), contributors: ["base"], mergePolicy: "concat" },
    ];
    await materialize(paths, basePlan, baseMerged);

    // User edits .claude/ (drift)
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "BASE-EDITED\n");
    await fs.writeFile(path.join(paths.claudeDir, "scratch.md"), "SCRATCH");

    // Persist + swap to v2.
    const v2Plan = await resolve("v2", { projectRoot: fx.projectRoot });
    const v2Merged: MergedFile[] = [
      { path: "CLAUDE.md", bytes: Buffer.from("BASE\nV2\n"), contributors: ["base", "v2"], mergePolicy: "concat" },
    ];
    const result = await persistAndMaterialize(paths, {
      activeProfileName: "base",
      newPlan: v2Plan,
      newMerged: v2Merged,
    });

    // Live `.claude/` is now v2's content.
    expect(
      await fs.readFile(path.join(paths.claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("BASE\nV2\n");
    // base profile has the persisted edits + scratch file.
    const baseProfileClaude = path.join(paths.profilesDir, "base", ".claude");
    expect(
      await fs.readFile(path.join(baseProfileClaude, "CLAUDE.md"), "utf8"),
    ).toBe("BASE-EDITED\n");
    expect(
      await fs.readFile(path.join(baseProfileClaude, "scratch.md"), "utf8"),
    ).toBe("SCRATCH");
    // State now points at v2.
    expect(result.state.activeProfile).toBe("v2");
  });

  it("rejects profile names containing path separators or traversal", async () => {
    const paths = buildStatePaths(fx.projectRoot);
    await expect(persistLiveIntoProfile(paths, "../escape")).rejects.toThrow(
      /Invalid profile name/,
    );
    await expect(persistLiveIntoProfile(paths, "../..")).rejects.toThrow(
      /Invalid profile name/,
    );
    await expect(persistLiveIntoProfile(paths, "a/b")).rejects.toThrow(
      /Invalid profile name/,
    );
  });

  it("persists an empty target when live .claude/ is missing", async () => {
    const paths = buildStatePaths(fx.projectRoot);
    // No live .claude/ at all.
    await persistLiveIntoProfile(paths, "base");
    const profileClaude = path.join(paths.profilesDir, "base", ".claude");
    expect(await pathExists(profileClaude)).toBe(true);
    const entries = await fs.readdir(profileClaude);
    expect(entries).toEqual([]);
  });
});
