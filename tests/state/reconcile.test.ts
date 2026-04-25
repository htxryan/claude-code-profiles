import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { pathExists } from "../../src/state/atomic.js";
import { buildStatePaths } from "../../src/state/paths.js";
import {
  reconcileMaterialize,
  reconcilePersist,
} from "../../src/state/reconcile.js";

describe("reconcile (R16a)", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-reconcile-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  it("none: no .pending or .prior leaves outcome=none", async () => {
    const paths = buildStatePaths(root);
    const out = await reconcileMaterialize(paths);
    expect(out.kind).toBe("none");
  });

  it("restores from .prior when both .prior and .claude exist (partial step c)", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.priorDir, { recursive: true });
    await fs.writeFile(path.join(paths.priorDir, "a"), "PRIOR");
    // .claude/ left in a half-renamed state.
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "a"), "PARTIAL");

    const out = await reconcileMaterialize(paths);
    expect(out.kind).toBe("restored-from-prior");
    expect(await pathExists(paths.priorDir)).toBe(false);
    const restored = await fs.readFile(path.join(paths.claudeDir, "a"), "utf8");
    expect(restored).toBe("PRIOR");
  });

  it("restores from .prior when only .prior exists (step b crashed before c started)", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.priorDir, { recursive: true });
    await fs.writeFile(path.join(paths.priorDir, "a"), "PRIOR");
    // .claude/ does not exist.
    const out = await reconcileMaterialize(paths);
    expect(out.kind).toBe("restored-from-prior");
    expect(await pathExists(paths.priorDir)).toBe(false);
    expect(
      await fs.readFile(path.join(paths.claudeDir, "a"), "utf8"),
    ).toBe("PRIOR");
  });

  it("discards .pending when only .pending exists (step a partial)", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.pendingDir, { recursive: true });
    await fs.writeFile(path.join(paths.pendingDir, "a"), "PARTIAL");
    // .claude/ has live content unrelated to pending.
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "live"), "LIVE");

    const out = await reconcileMaterialize(paths);
    expect(out.kind).toBe("discarded-pending");
    expect(await pathExists(paths.pendingDir)).toBe(false);
    expect(
      await fs.readFile(path.join(paths.claudeDir, "live"), "utf8"),
    ).toBe("LIVE");
  });

  it("clears .pending after a .prior restore", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.priorDir, { recursive: true });
    await fs.writeFile(path.join(paths.priorDir, "a"), "PRIOR");
    await fs.mkdir(paths.pendingDir, { recursive: true });
    await fs.writeFile(path.join(paths.pendingDir, "a"), "PENDING");
    const out = await reconcileMaterialize(paths);
    expect(out.kind).toBe("restored-from-prior");
    expect(await pathExists(paths.pendingDir)).toBe(false);
    expect(await pathExists(paths.priorDir)).toBe(false);
  });

  it("reconcilePersist: missing profile dir is none", async () => {
    const paths = buildStatePaths(root);
    const out = await reconcilePersist(paths, "nope");
    expect(out.kind).toBe("none");
  });

  it("reconcilePersist: restores per-profile prior", async () => {
    const paths = buildStatePaths(root);
    const profileDir = path.join(paths.profilesDir, "myprofile");
    await fs.mkdir(profileDir, { recursive: true });
    const target = path.join(profileDir, ".claude");
    const prior = path.join(profileDir, ".prior");
    await fs.mkdir(prior);
    await fs.writeFile(path.join(prior, "x"), "PRIOR");
    const out = await reconcilePersist(paths, "myprofile");
    expect(out.kind).toBe("restored-from-prior");
    expect(await fs.readFile(path.join(target, "x"), "utf8")).toBe("PRIOR");
  });
});
