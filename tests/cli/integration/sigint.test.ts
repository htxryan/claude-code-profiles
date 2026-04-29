/**
 * AC-15: SIGINT during a held lock releases the lock cleanly (no orphaned
 * `.lock` after Ctrl-C). Exercises the production code path: spawns a node
 * subprocess that calls the same `withLock` the swap orchestrator uses, then
 * sends SIGINT and verifies the lock file is gone and the exit code follows
 * the conventional 128 + signo (R41c).
 *
 * Note: this validates the lock module's signal handler in the *real*
 * subprocess context. The vitest worker can't host signal handlers (lesson:
 * `process.on('SIGINT')` fails inside vitest), which is precisely why every
 * other test passes `signalHandlers: false` — but here we spawn a true
 * Node process where handlers do fire.
 */

import { spawn } from "node:child_process";
import { promises as fs } from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

import { afterEach, describe, expect, it } from "vitest";

import { buildStatePaths } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const HOLDER = path.resolve(HERE, "lock-holder.mjs");
const DIST_LOCK = path.resolve(HERE, "..", "..", "..", "dist", "state", "lock.js");

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

async function ensureBuilt() {
  try {
    await fs.access(DIST_LOCK);
  } catch {
    throw new Error(
      `dist/ not built — run \`npm run build\` before integration tests`,
    );
  }
}

// Windows lacks POSIX SIGINT/SIGTERM semantics — Node maps them onto
// unrelated console events, exit codes don't follow `128 + signo`, and the
// hand-off between the spawned holder and the test harness is unreliable.
// The behavior these tests pin is a POSIX invariant; skip the suite there.
const describePosix = process.platform === "win32" ? describe.skip : describe;

describePosix("SIGINT releases lock (AC-15)", () => {
  it("spawned holder receives SIGINT → exit 130 + lock file removed", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);

    const child = spawn(process.execPath, [HOLDER, fx.projectRoot], {
      stdio: ["ignore", "pipe", "pipe"],
    });

    // Wait for the holder to write "LOCKED\n" — once we see that, we know
    // withLock has acquired and registered the signal handler.
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("timeout waiting for LOCKED")), 5000);
      child.stdout.on("data", (chunk: Buffer) => {
        if (chunk.toString().includes("LOCKED")) {
          clearTimeout(timer);
          resolve();
        }
      });
      child.on("error", (err) => {
        clearTimeout(timer);
        reject(err);
      });
    });

    // Sanity check: lock file is on disk, holder's PID is in it.
    const before = await fs.readFile(paths.lockFile, "utf8");
    expect(before).toMatch(new RegExp(`^${child.pid}\\s`));

    // Send SIGINT and wait for the child to exit.
    const exitInfo = await new Promise<{ code: number | null; signal: NodeJS.Signals | null }>(
      (resolve) => {
        child.on("close", (code, signal) =>
          resolve({ code, signal }),
        );
        child.kill("SIGINT");
      },
    );

    // The lock module's signal handler calls process.exit(128 + 2) = 130.
    expect(exitInfo.code).toBe(130);

    // The lock file must be gone.
    await expect(fs.access(paths.lockFile)).rejects.toMatchObject({ code: "ENOENT" });
  }, 10_000);

  // yd8 / AC-4: when a peer holds the lock, the CLI must surface a single
  // actionable message naming PID, age, and a literal --wait remediation.
  // We spawn the lock-holder, then run `claude-profiles use` against the
  // same project to assert exit 3 + the enriched stderr message.
  it("yd8 AC-4: lock-held UX names PID, age, and the --wait remediation", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "X\n" } },
      },
    });

    const holder = spawn(process.execPath, [HOLDER, fx.projectRoot], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    try {
      // Wait for the holder to acquire.
      await new Promise<void>((resolve, reject) => {
        const t = setTimeout(() => reject(new Error("timeout LOCKED")), 5000);
        holder.stdout.on("data", (chunk: Buffer) => {
          if (chunk.toString().includes("LOCKED")) {
            clearTimeout(t);
            resolve();
          }
        });
      });
      // Now spawn a `use` invocation that will hit the lock.
      const { runCli } = await import("./spawn.js");
      const r = await runCli({
        args: ["--cwd", fx.projectRoot, "use", "a"],
        timeoutMs: 5000,
      });
      // Lock-held maps to exit 3 (peer holds project).
      expect(r.exitCode).toBe(3);
      // Stderr message must include the PID + remediation.
      expect(r.stderr).toContain(`PID ${holder.pid}`);
      expect(r.stderr).toContain("--wait");
      expect(r.stderr).toContain("ago");
    } finally {
      holder.kill("SIGTERM");
      await new Promise((resolve) => holder.on("close", resolve));
    }
  }, 10_000);

  it("SIGTERM also releases the lock (exit 143)", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);

    const child = spawn(process.execPath, [HOLDER, fx.projectRoot], {
      stdio: ["ignore", "pipe", "pipe"],
    });

    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("timeout waiting for LOCKED")), 5000);
      child.stdout.on("data", (chunk: Buffer) => {
        if (chunk.toString().includes("LOCKED")) {
          clearTimeout(timer);
          resolve();
        }
      });
    });

    const exitInfo = await new Promise<{ code: number | null }>((resolve) => {
      child.on("close", (code) => resolve({ code }));
      child.kill("SIGTERM");
    });

    expect(exitInfo.code).toBe(143); // 128 + 15
    await expect(fs.access(paths.lockFile)).rejects.toMatchObject({ code: "ENOENT" });
  }, 10_000);
});
