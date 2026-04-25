import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { listSnapshots, snapshotForDiscard } from "../../src/state/backup.js";
import { buildStatePaths } from "../../src/state/paths.js";

describe("discard backup snapshots (R23a)", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-backup-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  it("returns null when there is no live .claude/ to back up", async () => {
    const paths = buildStatePaths(root);
    const snap = await snapshotForDiscard(paths);
    expect(snap).toBeNull();
  });

  it("snapshots the live .claude/ tree to .backup/<ISO>/", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "v1");
    await fs.mkdir(path.join(paths.claudeDir, "agents"), { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "agents", "x.md"), "AGENT");

    const snap = await snapshotForDiscard(paths);
    expect(snap).not.toBeNull();
    expect(await fs.readFile(path.join(snap!, "CLAUDE.md"), "utf8")).toBe("v1");
    expect(await fs.readFile(path.join(snap!, "agents", "x.md"), "utf8")).toBe("AGENT");
  });

  it("retains at most 5 snapshots, pruning oldest first", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "f"), "x");

    // Create 6 snapshots; the directory naming includes a hi-res timestamp,
    // and we sleep just enough between them to guarantee distinct names.
    const created: string[] = [];
    for (let i = 0; i < 6; i++) {
      const snap = await snapshotForDiscard(paths);
      created.push(snap!);
      await new Promise((r) => setTimeout(r, 5));
    }

    const remaining = await listSnapshots(paths);
    expect(remaining.length).toBe(5);
    // Oldest should be pruned.
    expect(remaining).not.toContain(created[0]);
    // Newest should be retained.
    expect(remaining).toContain(created[created.length - 1]);
  });

  it("uses a Windows-safe directory name (no colons)", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.claudeDir, { recursive: true });
    await fs.writeFile(path.join(paths.claudeDir, "f"), "x");
    const snap = await snapshotForDiscard(paths);
    expect(snap).not.toBeNull();
    expect(path.basename(snap!)).not.toContain(":");
  });
});
