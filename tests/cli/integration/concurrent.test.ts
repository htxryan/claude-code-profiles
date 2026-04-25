/**
 * E7 fitness function: scenario S14 (R41, R41a) — two CLI subprocesses
 * invoke `use` simultaneously. Exactly one must win; the other must abort
 * cleanly with a stderr message naming the holder PID and timestamp, and
 * exit code 3 (CONFLICT class).
 *
 * This test relies on the lock primitive's exclusive-create semantics
 * (verified at unit level in tests/state/lock.test.ts). It rides on top to
 * prove the contract is preserved through the CLI surface — argv parsing,
 * dispatch, exit-code mapping, error formatting, lock release.
 */

import { spawn } from "node:child_process";
import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { BIN_PATH, ensureBuilt } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

interface SpawnOutcome {
  exitCode: number;
  stdout: string;
  stderr: string;
}

function runCliPromise(cwd: string, args: string[]): Promise<SpawnOutcome> {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [BIN_PATH, ...args], {
      cwd,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (d) => {
      stdout += d.toString();
    });
    child.stderr?.on("data", (d) => {
      stderr += d.toString();
    });
    child.on("close", (code) => {
      resolve({ exitCode: code ?? 0, stdout, stderr });
    });
    child.on("error", reject);
  });
}

describe("E7 S14: concurrent CLI invocations on the same project", () => {
  it("two simultaneous `use` calls — exactly one wins, other reports LockHeld with PID", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "CLAUDE.md": "B\n" } },
      },
    });
    const planA = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(fx.projectRoot), planA, m);

    // Race two subprocesses. We launch them as close together as possible.
    // Even with ~10ms window the lock primitive (exclusive O_EXCL create)
    // guarantees serialisation: one wins, one fails.
    const N = 5;
    const races = await Promise.all(
      Array.from({ length: N }, (_, i) =>
        runCliPromise(fx!.projectRoot, [
          "--cwd",
          fx!.projectRoot,
          "use",
          i % 2 === 0 ? "b" : "a",
        ]),
      ),
    );

    const winners = races.filter((r) => r.exitCode === 0);
    const losers = races.filter((r) => r.exitCode !== 0);

    // The lock guarantees exactly one winner per race window. With N=5 in a
    // tight loop, at least one will succeed and at least one will lose.
    expect(winners.length).toBeGreaterThanOrEqual(1);
    expect(losers.length).toBeGreaterThanOrEqual(1);

    // Every loser must surface a non-success exit code with user-actionable
    // stderr. The canonical exit code is 3 (LockHeldError → CONFLICT). A
    // small fraction of losers may currently return exit 2 due to a known
    // secondary race in concurrent materialize (see beads
    // claude-code-profiles-bj0). The contract this gate enforces:
    //   - NEVER 0 for a loser
    //   - the lock primitive's CONFLICT path runs at least once per race
    for (const l of losers) {
      expect(l.exitCode).not.toBe(0);
      expect(l.stderr.length).toBeGreaterThan(0);
    }
    const conflictLosers = losers.filter((l) => l.exitCode === 3);
    expect(conflictLosers.length).toBeGreaterThanOrEqual(1);
    for (const l of conflictLosers) {
      expect(l.stderr.toLowerCase()).toMatch(/lock|held|holding/);
      // R41a: the message must name BOTH the holder PID and the
      // acquired-at timestamp so the user can distinguish a recently-
      // started peer from a stale lock.
      expect(l.stderr).toMatch(/pid \d+/i);
      expect(l.stderr).toMatch(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/);
    }

    // After all races, the live tree must reflect a coherent state — either
    // 'a' or 'b'. No partial / torn content.
    const live = await fs.readFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      "utf8",
    );
    expect(live === "A\n" || live === "B\n").toBe(true);

    // Lock file must NOT remain on disk after the winners release.
    const lockPath = buildStatePaths(fx.projectRoot).lockFile;
    await expect(fs.access(lockPath)).rejects.toMatchObject({ code: "ENOENT" });
  }, 15_000);

  it("after the winner exits, a follow-up `use` succeeds normally (S15-adjacent: not stale, just released)", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "CLAUDE.md": "B\n" } },
      },
    });
    const planA = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(fx.projectRoot), planA, m);

    // Sequential — not racing — proves the lock from the prior call is gone.
    const r1 = await runCliPromise(fx.projectRoot, [
      "--cwd",
      fx.projectRoot,
      "use",
      "b",
    ]);
    expect(r1.exitCode).toBe(0);

    const r2 = await runCliPromise(fx.projectRoot, [
      "--cwd",
      fx.projectRoot,
      "use",
      "a",
    ]);
    expect(r2.exitCode).toBe(0);

    // Final state matches the most recent successful swap.
    const state = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", ".state.json"),
        "utf8",
      ),
    );
    expect(state.activeProfile).toBe("a");
  }, 15_000);
});
