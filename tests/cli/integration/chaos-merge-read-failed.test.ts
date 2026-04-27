/**
 * Chaos test: MergeReadFailedError abort path (R16, R16a, R41c).
 *
 * MergeReadFailedError is raised by src/merge/merge.ts when a contributor's
 * file is enumerated by the resolver (visible at walk time) but cannot be
 * read at merge time. In production this happens when a contributor file is
 * deleted between resolution and merge — typically because an external
 * component sits on a network mount, in a peer-edited git worktree, or in
 * a temp dir that another process is racing.
 *
 * The handler must satisfy three guarantees that this file pins:
 *
 *   1. Exit code 2 (system error class — see src/cli/exit.ts:exitCodeFor).
 *   2. No partial filesystem writes: neither `.pending/` nor `.prior/`
 *      remains, and the live `.claude/` is byte-identical to its pre-state
 *      (atomic-across-destinations: even if the projectRoot side would
 *      have splice succeeded, no bytes were written there either).
 *   3. The lock file is released (R41c) so a follow-up `use` succeeds
 *      without manual cleanup.
 *
 * Reverting any of: the rmrf-on-throw cleanup of `pendingDir` in
 * materialize.ts, the lock-release on signal/throw in src/state/lock.ts,
 * or the atomic-across-destinations guarantee in materialize.ts must
 * surface as a failure here.
 *
 * How we trigger MergeReadFailedError without process injection: chmod 000
 * on the target file. The walker uses fs.readdir+entry.isFile() (which
 * doesn't read content), so the file is visible to resolve. merge then
 * calls fs.readFile which fails with EACCES → MergeReadFailedError.
 *
 * POSIX-only: chmod is a no-op on Windows. The test skips itself there.
 */

import { promises as fs, type Dirent } from "node:fs";
import * as path from "node:path";
import process from "node:process";

import { afterEach, describe, expect, it } from "vitest";

import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { ensureBuilt, runCli } from "./spawn.js";

const POSIX = process.platform !== "win32";

let fx: Fixture | undefined;
afterEach(async () => {
  // Restore permissions so cleanup can rm the tmp tree even if the test
  // chmod'd a file unreadable.
  if (fx) {
    try {
      const profilesDir = path.join(fx.projectRoot, ".claude-profiles");
      await chmodAllReadable(profilesDir);
    } catch {
      /* best-effort */
    }
    await fx.cleanup();
  }
  fx = undefined;
});

async function chmodAllReadable(root: string): Promise<void> {
  let entries: string[];
  try {
    entries = await fs.readdir(root);
  } catch {
    return;
  }
  for (const e of entries) {
    const p = path.join(root, e);
    try {
      const st = await fs.lstat(p);
      await fs.chmod(p, 0o755);
      if (st.isDirectory()) await chmodAllReadable(p);
    } catch {
      /* best-effort */
    }
  }
}

describe.skipIf(!POSIX)("chaos: MergeReadFailedError → clean abort (R16, R41c)", () => {
  it("`use b` with an unreadable contributor file → exit 2, no .pending/.prior, lock released", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: { "CLAUDE.md": "A\n" },
        },
        b: {
          manifest: { name: "b" },
          files: { "CLAUDE.md": "B\n", "agents/x.md": "X\n" },
        },
      },
    });
    // Materialise `a` so we have a defined "pre-state" to compare against
    // after the failed swap.
    const planA = await resolve("a", { projectRoot: fx.projectRoot });
    const ma = await merge(planA);
    await materialize(buildStatePaths(fx.projectRoot), planA, ma);

    // Capture the live tree byte-identically before the chaotic swap.
    const claudeDir = path.join(fx.projectRoot, ".claude");
    const preClaude = await readTree(claudeDir);

    // Make one of b's contributor files unreadable. Walker sees it (readdir
    // doesn't need read perms on the file), merge fails to read it, and
    // throws MergeReadFailedError.
    const targetFile = path.join(
      fx.projectRoot,
      ".claude-profiles",
      "b",
      ".claude",
      "agents",
      "x.md",
    );
    await fs.chmod(targetFile, 0o000);

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });

    // (1) Exit code 2 — MergeError → EXIT_SYSTEM_ERROR per exit.ts.
    expect(r.exitCode).toBe(2);
    // The error message names the relPath so the user can find the
    // offending file. Don't pin the full path (varies with tmp).
    expect(r.stderr).toMatch(/agents\/x\.md|x\.md/);

    // (2) Atomic abort — no partial writes anywhere.
    const metaDir = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    await expect(
      fs.access(path.join(metaDir, "pending")),
    ).rejects.toMatchObject({ code: "ENOENT" });
    await expect(
      fs.access(path.join(metaDir, "prior")),
    ).rejects.toMatchObject({ code: "ENOENT" });

    // Live `.claude/` byte-identical to pre-state.
    const postClaude = await readTree(claudeDir);
    expect(postClaude).toEqual(preClaude);

    // (3) Lock released (R41c). Restore permissions first so a follow-up
    // call doesn't trip the same chaos before we get to assert.
    await fs.chmod(targetFile, 0o644);
    await expect(
      fs.access(path.join(metaDir, "lock")),
    ).rejects.toMatchObject({ code: "ENOENT" });

    // Follow-up `use b` succeeds without manual recovery.
    const r2 = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r2.exitCode).toBe(0);
    expect(r2.stdout).toContain("Switched to b");
    expect(
      await fs.readFile(path.join(claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
  });

  it("MergeReadFailed before any active state: live tree absent stays absent", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        b: {
          manifest: { name: "b" },
          files: { "CLAUDE.md": "B\n" },
        },
      },
    });
    // No prior materialize → `.claude/` does not exist on disk.
    const claudeDir = path.join(fx.projectRoot, ".claude");
    await expect(fs.access(claudeDir)).rejects.toMatchObject({ code: "ENOENT" });

    const targetFile = path.join(
      fx.projectRoot,
      ".claude-profiles",
      "b",
      ".claude",
      "CLAUDE.md",
    );
    await fs.chmod(targetFile, 0o000);

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(2);
    // Live `.claude/` must NOT have been created mid-failure (atomic abort).
    await expect(fs.access(claudeDir)).rejects.toMatchObject({ code: "ENOENT" });

    const metaDir = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    await expect(
      fs.access(path.join(metaDir, "pending")),
    ).rejects.toMatchObject({ code: "ENOENT" });
    // No state.json should have been written either — the swap aborted
    // before reaching the post-swap state-write step.
    await expect(
      fs.access(path.join(metaDir, "state.json")),
    ).rejects.toMatchObject({ code: "ENOENT" });

    // Restore so cleanup works.
    await fs.chmod(targetFile, 0o644);
  });
});

/**
 * Recursively read every file under `root` keyed by relative posix path.
 * Symlinks resolved through fs.readFile (the same way materialize does).
 * Returns {} for a missing root so callers can compare across pre/post
 * states uniformly.
 */
async function readTree(root: string): Promise<Record<string, string>> {
  const out: Record<string, string> = {};
  async function walk(rel: string): Promise<void> {
    const here = rel === "" ? root : path.join(root, rel);
    let entries: Dirent[];
    try {
      entries = (await fs.readdir(here, { withFileTypes: true })) as Dirent[];
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") return;
      throw err;
    }
    for (const e of entries) {
      const name = String(e.name);
      const childRel = rel === "" ? name : `${rel}/${name}`;
      const childAbs = path.join(here, name);
      if (e.isDirectory()) {
        await walk(childRel);
      } else if (e.isFile()) {
        out[childRel] = await fs.readFile(childAbs, "utf8");
      }
    }
  }
  await walk("");
  return out;
}
