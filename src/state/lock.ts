/**
 * Lockfile primitive (R41 / R41a / R41b / R41c).
 *
 * Contract:
 *  - Acquire writes `<PID> <ISO-8601>` to `.claude-profiles/.lock` atomically
 *    (write to .lock.tmp, rename), only if no live holder exists.
 *  - If `.lock` exists with a live PID (kill -0 succeeds), abort with
 *    `LockHeldError` naming the holder.
 *  - If `.lock` exists with a dead PID, atomically replace it (stale-recovery).
 *  - Release deletes the lock file (idempotent — safe to call from signal
 *    handler and from finally block).
 *  - Signal handlers (SIGINT/SIGTERM) call release synchronously and re-emit
 *    the signal so the process exits with the conventional code.
 *
 * Reads (R43) bypass the lock entirely — they're never serialized through
 * acquireLock. The lock is only acquired before mutating ops.
 */

import { promises as fs, unlinkSync } from "node:fs";
import * as os from "node:os";
import * as process from "node:process";

import { atomicWriteFile, pathExists } from "./atomic.js";
import type { StatePaths } from "./paths.js";
import type { LockHandle } from "./types.js";

/**
 * Raised when `.lock` exists and the holding PID is still alive (R41a). The
 * caller (E5 CLI) renders this as a non-zero exit with the holder's PID and
 * timestamp per the §7 quality bar.
 */
export class LockHeldError extends Error {
  readonly holderPid: number;
  readonly holderTimestamp: string;
  readonly lockPath: string;

  constructor(lockPath: string, holderPid: number, holderTimestamp: string) {
    super(
      `Lock at "${lockPath}" is held by PID ${holderPid} (acquired at ${holderTimestamp})`,
    );
    this.name = "LockHeldError";
    this.lockPath = lockPath;
    this.holderPid = holderPid;
    this.holderTimestamp = holderTimestamp;
  }
}

/**
 * Raised when the lock file content is corrupted (not parseable as
 * `<PID> <timestamp>`). We treat this as stale and replace, but expose the
 * detail for diagnostics.
 */
export class LockCorruptError extends Error {
  readonly detail: string;
  constructor(lockPath: string, detail: string) {
    super(`Lock file at "${lockPath}" is corrupt: ${detail}`);
    this.name = "LockCorruptError";
    this.detail = detail;
  }
}

interface ParsedLock {
  pid: number;
  timestamp: string;
}

function parseLockContents(raw: string): ParsedLock | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const space = trimmed.indexOf(" ");
  if (space <= 0) return null;
  const pidStr = trimmed.slice(0, space);
  const ts = trimmed.slice(space + 1).trim();
  const pid = Number.parseInt(pidStr, 10);
  if (!Number.isFinite(pid) || pid <= 0) return null;
  if (!ts) return null;
  return { pid, timestamp: ts };
}

/**
 * Stale-detection: kill(pid, 0) returns silently if the process exists,
 * throws ESRCH if it does not. We wrap to a boolean. EPERM (process exists
 * but we don't have permission to signal) is treated as alive — conservative
 * choice that prefers "abort and ask the user" over "stomp on someone else's
 * lock". The user can `rm` the lock manually if they're sure.
 */
function isPidAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err: unknown) {
    const code = (err as NodeJS.ErrnoException).code;
    if (code === "ESRCH") return false;
    if (code === "EPERM") return true;
    // Unknown — be conservative.
    return true;
  }
}

/**
 * Acquire the project lock. Returns a `LockHandle` with idempotent release.
 * `signalHandlers` defaults to true — registers SIGINT/SIGTERM handlers that
 * release the lock and re-emit the signal (R41c). Tests pass false so they
 * can synchronize on release without process-level handlers.
 */
export async function acquireLock(
  paths: StatePaths,
  opts: { signalHandlers?: boolean } = {},
): Promise<LockHandle> {
  await fs.mkdir(paths.profilesDir, { recursive: true });

  // Check existing lock (if any). Stale recovery is built into the loop:
  // we replace a dead-PID lock and proceed. We never loop on retry — a single
  // attempt is enough; concurrent acquirers will see one of: ENOENT (try
  // again), live PID (LockHeldError), dead PID (replace).
  if (await pathExists(paths.lockFile)) {
    let raw: string;
    try {
      raw = await fs.readFile(paths.lockFile, "utf8");
    } catch (err: unknown) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") {
        // Race: holder released between exists check and read. Fall through.
        raw = "";
      } else {
        throw err;
      }
    }
    if (raw.length > 0) {
      const parsed = parseLockContents(raw);
      if (!parsed) {
        // Treat unparseable as stale; replace. (Don't propagate
        // LockCorruptError — recovery is the right behavior here.)
      } else if (isPidAlive(parsed.pid)) {
        throw new LockHeldError(paths.lockFile, parsed.pid, parsed.timestamp);
      }
      // else: dead PID, fall through and overwrite.
    }
  }

  // Atomic write of our claim. The temp+rename ensures another process never
  // reads a half-written lock file. The rename overwrites any stale lock that
  // survived the existence check above.
  const pid = process.pid;
  const timestamp = new Date().toISOString();
  const contents = `${pid} ${timestamp}${os.EOL}`;
  await atomicWriteFile(paths.lockFile, paths.lockFileTmp, contents);

  // Verify we actually acquired it (concurrent acquirer may have raced us).
  // If the lock now contains a different PID, we lost the race; re-check
  // liveness and either back off or claim a stale slot.
  const verifyRaw = await fs.readFile(paths.lockFile, "utf8");
  const verified = parseLockContents(verifyRaw);
  if (!verified || verified.pid !== pid || verified.timestamp !== timestamp) {
    // Someone else's writeFile beat ours after our verification. Their lock
    // is now canonical — abort with LockHeldError naming whatever's there.
    if (verified && isPidAlive(verified.pid)) {
      throw new LockHeldError(paths.lockFile, verified.pid, verified.timestamp);
    }
    // If verified is null or the holder is dead, fall through with a warning
    // — stale-on-stale is rare enough we don't loop.
    if (!verified) {
      throw new LockCorruptError(
        paths.lockFile,
        "lock file content unrecognised after acquire",
      );
    }
  }

  let released = false;
  let signalCleanup: (() => void) | undefined;

  const release = async (): Promise<void> => {
    if (released) return;
    released = true;
    if (signalCleanup) {
      signalCleanup();
      signalCleanup = undefined;
    }
    try {
      await fs.unlink(paths.lockFile);
    } catch (err: unknown) {
      // Already gone (e.g. concurrent reconciliation cleaned it). Idempotent.
      if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
        throw err;
      }
    }
  };

  if (opts.signalHandlers !== false) {
    signalCleanup = registerSignalRelease(paths.lockFile, () => {
      released = true;
    });
  }

  return {
    path: paths.lockFile,
    pid,
    acquiredAt: timestamp,
    release,
  };
}

/**
 * Convenience wrapper: acquire, run `fn`, always release (even on throw).
 * Mirrors the using/with idiom Node lacks. Used by every E3 mutating op.
 */
export async function withLock<T>(
  paths: StatePaths,
  fn: (handle: LockHandle) => Promise<T>,
  opts: { signalHandlers?: boolean } = {},
): Promise<T> {
  const handle = await acquireLock(paths, opts);
  try {
    return await fn(handle);
  } finally {
    await handle.release();
  }
}

/**
 * Register synchronous signal handlers that delete the lock file and exit
 * with the conventional 128+signo code (R41c). Returns a cleanup function
 * the caller invokes during normal release to unregister so we don't leak
 * listeners across multiple acquire/release cycles in a single process.
 *
 * Sync `unlinkSync` so we release even when the event loop is wedged.
 */
function registerSignalRelease(lockPath: string, markReleased: () => void): () => void {
  const onSignal = (signal: NodeJS.Signals): void => {
    try {
      try {
        unlinkSync(lockPath);
      } catch {
        // Already gone or not accessible — ignore.
      }
      markReleased();
    } finally {
      // Conventional exit code: 128 + signo. Node maps SIGINT=2, SIGTERM=15.
      const signo = signal === "SIGINT" ? 2 : signal === "SIGTERM" ? 15 : 1;
      process.exit(128 + signo);
    }
  };
  const sigintHandler = (): void => onSignal("SIGINT");
  const sigtermHandler = (): void => onSignal("SIGTERM");
  process.on("SIGINT", sigintHandler);
  process.on("SIGTERM", sigtermHandler);
  return (): void => {
    process.removeListener("SIGINT", sigintHandler);
    process.removeListener("SIGTERM", sigtermHandler);
  };
}

