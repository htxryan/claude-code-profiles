import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  acquireLock,
  LockHeldError,
  withLock,
} from "../../src/state/lock.js";
import { buildStatePaths } from "../../src/state/paths.js";

describe("lock primitive", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-lock-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  it("acquires and releases the lock", async () => {
    const paths = buildStatePaths(root);
    const lock = await acquireLock(paths, { signalHandlers: false });
    expect(lock.pid).toBe(process.pid);
    expect(typeof lock.acquiredAt).toBe("string");
    const content = await fs.readFile(paths.lockFile, "utf8");
    expect(content).toMatch(new RegExp(`^${process.pid}\\s+\\d{4}-`));
    await lock.release();
    await expect(fs.access(paths.lockFile)).rejects.toThrow();
  });

  it("R41a: throws LockHeldError when another live PID holds the lock", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.metaDir, { recursive: true });
    // Use this process's PID — it's by definition alive.
    const ts = new Date().toISOString();
    await fs.writeFile(paths.lockFile, `${process.pid} ${ts}\n`);
    await expect(acquireLock(paths, { signalHandlers: false })).rejects.toBeInstanceOf(
      LockHeldError,
    );
  });

  it("R41b: replaces a stale lock from a dead PID", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.metaDir, { recursive: true });
    // PID 0 doesn't exist — guaranteed dead. (kill(0,0) raises ESRCH on every OS.)
    // We use 99999999 which is well above any normal PID range.
    await fs.writeFile(paths.lockFile, `99999999 2026-01-01T00:00:00.000Z\n`);
    const lock = await acquireLock(paths, { signalHandlers: false });
    expect(lock.pid).toBe(process.pid);
    await lock.release();
  });

  it("treats unparseable lock as stale and replaces", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.metaDir, { recursive: true });
    await fs.writeFile(paths.lockFile, "garbage");
    const lock = await acquireLock(paths, { signalHandlers: false });
    expect(lock.pid).toBe(process.pid);
    await lock.release();
  });

  it("release is idempotent", async () => {
    const paths = buildStatePaths(root);
    const lock = await acquireLock(paths, { signalHandlers: false });
    await lock.release();
    await expect(lock.release()).resolves.toBeUndefined();
  });

  it("withLock releases on success", async () => {
    const paths = buildStatePaths(root);
    const result = await withLock(
      paths,
      async () => {
        const c = await fs.readFile(paths.lockFile, "utf8");
        expect(c).toContain(`${process.pid}`);
        return 42;
      },
      { signalHandlers: false },
    );
    expect(result).toBe(42);
    await expect(fs.access(paths.lockFile)).rejects.toThrow();
  });

  it("acquires without leaving any tmp/side files behind", async () => {
    const paths = buildStatePaths(root);
    const lock = await acquireLock(paths, { signalHandlers: false });
    // The lock now uses fs.open(wx) for atomic exclusive create — no tmp
    // file is involved on the success path. After acquire, only `.lock`
    // should be present in profilesDir.
    const entries = await fs.readdir(paths.profilesDir);
    const debris = entries.filter(
      (e) => e.endsWith(".tmp") || e.includes(".stale.") || e.includes(".reconcile-"),
    );
    expect(debris).toEqual([]);
    await lock.release();
  });

  it("withLock releases on throw", async () => {
    const paths = buildStatePaths(root);
    await expect(
      withLock(
        paths,
        async () => {
          throw new Error("boom");
        },
        { signalHandlers: false },
      ),
    ).rejects.toThrow("boom");
    await expect(fs.access(paths.lockFile)).rejects.toThrow();
  });

  // Regression: rename-based claim had a replace-existing TOCTOU where two
  // concurrent acquirers could both succeed (Codex review #1, Gemini #1).
  // The exclusive-create primitive should serialize all parallel attempts
  // through O_EXCL; only one can win, the rest must throw LockHeldError.
  it("under heavy contention exactly one acquirer wins; others fail-fast", async () => {
    const paths = buildStatePaths(root);
    const N = 25;
    const results = await Promise.allSettled(
      Array.from({ length: N }, () =>
        acquireLock(paths, { signalHandlers: false }),
      ),
    );
    const winners = results.filter((r) => r.status === "fulfilled");
    const losers = results.filter((r) => r.status === "rejected");
    expect(winners.length).toBe(1);
    expect(losers.length).toBe(N - 1);
    for (const l of losers) {
      const reason = (l as PromiseRejectedResult).reason;
      expect(reason).toBeInstanceOf(LockHeldError);
    }
    const winner = (winners[0] as PromiseFulfilledResult<Awaited<ReturnType<typeof acquireLock>>>)
      .value;
    await winner.release();
  });
});
