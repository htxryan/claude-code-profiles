/**
 * E7 fitness function: gate state machine matrix (NON-INTERACTIVE rows).
 *
 * The drift gate composes three R21–R24 choices (discard / persist / abort)
 * with two execution modes (interactive / non-interactive). The full
 * documented matrix is 3×2, but this file only exercises the
 * **non-interactive** row at the binary surface — that's what the spawn
 * runner can drive without a pseudo-TTY:
 *
 *   |                 | discard | persist | abort |
 *   | non-interactive |    ✓    |    ✓    |   ✓   |
 *   | interactive     |    —    |    —    |   —   |   ← unit-level only
 *
 * Interactive cells are covered at the unit level in tests/drift/gate.test.ts
 * (decideGate has interactive vs non-interactive branches with stub prompts);
 * a binary-surface pseudo-TTY harness is deferred (would need node-pty).
 *
 * What this matrix DOES verify across the spawn boundary:
 *   - non-interactive without --on-drift: exit 1 + flag mentioned
 *   - non-interactive --on-drift=discard: exit 0 + new content + backup snapshot
 *   - non-interactive --on-drift=persist: exit 0 + active profile updated + new content
 *   - non-interactive --on-drift=abort: exit 1 + state unchanged
 *   - sync (active profile re-materialise) under each --on-drift= choice
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { ensureBuilt, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

/** Two-profile fixture with `a` active and `.claude/CLAUDE.md` drifted to "EDIT\n". */
async function setupDrifted() {
  const f = await makeFixture({
    profiles: {
      a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      b: { manifest: { name: "b" }, files: { "CLAUDE.md": "B\n" } },
    },
  });
  const planA = await resolve("a", { projectRoot: f.projectRoot });
  const m = await merge(planA);
  await materialize(buildStatePaths(f.projectRoot), planA, m);
  await fs.writeFile(path.join(f.projectRoot, ".claude", "CLAUDE.md"), "EDIT\n");
  return f;
}

describe("E7 gate state machine matrix — non-interactive cells", () => {
  it("non-interactive without --on-drift → exit 1 + stderr names the flag", async () => {
    await ensureBuilt();
    fx = await setupDrifted();
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "b"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("--on-drift=");
    // Live tree is unchanged (the abort precedes any write).
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("EDIT\n");
  });

  it("non-interactive --on-drift=discard → exit 0 + new content + backup snapshot equals pre-swap edited tree", async () => {
    await ensureBuilt();
    fx = await setupDrifted();
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=discard", "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Switched to b");
    // Backup snapshot must be on disk under .claude-profiles/.backup/, and
    // the snapshot bytes must equal the drifted live tree at the moment of
    // discard (R23a — pre-swap snapshot, not the post-swap content).
    const backupDir = path.join(fx.projectRoot, ".claude-profiles", ".backup");
    // Filter to directories and sort by name (snapshot dirs are timestamped
    // and lexicographically sortable). A stray file in .backup/ must not
    // confuse the lookup; the production prune path applies the same
    // filter (see src/state/backup.ts:pruneOldSnapshots).
    const dirents = await fs.readdir(backupDir, { withFileTypes: true });
    const snapshotDirs = dirents
      .filter((d) => d.isDirectory())
      .map((d) => d.name)
      .sort();
    expect(snapshotDirs.length).toBeGreaterThan(0);
    const snapshot = await fs.readFile(
      path.join(backupDir, snapshotDirs[0]!, "CLAUDE.md"),
      "utf8",
    );
    expect(snapshot).toBe("EDIT\n");
    // Live tree now matches b.
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
  });

  it("non-interactive --on-drift=persist → exit 0 + drift saved to active profile + new content live", async () => {
    await ensureBuilt();
    fx = await setupDrifted();
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=persist", "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    // The previously-active profile a now has the edited content baked in.
    expect(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", "a", ".claude", "CLAUDE.md"),
        "utf8",
      ),
    ).toBe("EDIT\n");
    // Live tree is now b's content.
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
  });

  it("non-interactive --on-drift=abort → exit 1 + state and live tree unchanged", async () => {
    await ensureBuilt();
    fx = await setupDrifted();
    const stateBefore = await fs.readFile(
      path.join(fx.projectRoot, ".claude-profiles", ".state.json"),
      "utf8",
    );
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=abort", "use", "b"],
    });
    expect(r.exitCode).toBe(1);
    // .claude/ untouched
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("EDIT\n");
    // .state.json untouched
    const stateAfter = await fs.readFile(
      path.join(fx.projectRoot, ".claude-profiles", ".state.json"),
      "utf8",
    );
    expect(stateAfter).toBe(stateBefore);
  });
});

describe("E7 gate state machine matrix — sync (re-materialise active) cells", () => {
  it("sync without --on-drift on a drifted active → exit 1 (S12 gate)", async () => {
    await ensureBuilt();
    fx = await setupDrifted();
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "sync"] });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("--on-drift=");
  });

  it("sync --on-drift=discard on drifted active → exit 0 + live restored to source", async () => {
    await ensureBuilt();
    fx = await setupDrifted();
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=discard", "sync"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Synced a");
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("A\n");
  });

  it("sync --on-drift=abort on drifted active → exit 1 + live tree unchanged", async () => {
    await ensureBuilt();
    fx = await setupDrifted();
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=abort", "sync"],
    });
    expect(r.exitCode).toBe(1);
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("EDIT\n");
  });

  it("sync on clean active → no-op success (no drift to gate on)", async () => {
    await ensureBuilt();
    const f = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      },
    });
    fx = f;
    const planA = await resolve("a", { projectRoot: f.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(f.projectRoot), planA, m);
    const r = await runCli({ args: ["--cwd", f.projectRoot, "sync"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Synced a");
  });
});
