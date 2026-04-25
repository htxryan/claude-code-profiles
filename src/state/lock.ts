/**
 * Lockfile primitive (R41 / R41a / R41b / R41c).
 *
 * Contract:
 *  - Acquire writes `<PID> <ISO-8601>` to `.claude-profiles/.lock` atomically
 *    (write to a per-attempt unique tmp path, fsync, rename), only if no live
 *    holder exists.
 *  - If `.lock` exists with a live PID (kill -0 succeeds), abort with
 *    `LockHeldError` naming the holder.
 *  - If `.lock` exists with a dead PID, atomically replace it (stale-recovery).
 *  - Release deletes the lock file (idempotent — safe to call from signal
 *    handler and from finally block).
 *  - Signal handlers (SIGINT/SIGTERM) call release synchronously and exit.
 *
 * Reads (R43) bypass the lock entirely — they're never serialized through
 * acquireLock. The lock is only acquired before mutating ops.
 *
 * Concurrency notes:
 *  - Per-attempt tmp paths (`.lock.<pid>.<nonce>.tmp`) prevent two acquirers
 *    racing on a shared `.lock.tmp` (multi-reviewer P1, Gemini #1).
 *  - Verify-read after rename handles the case where another acquirer's
 *    rename came after ours: a live holder triggers LockHeldError; a dead
 *    holder triggers a bounded retry to re-claim (multi-reviewer P1, Codex #1).
 *  - On Windows, `process.kill(pid, 0)` can be ambiguous for cross-session or
 *    recycled PIDs. We supplement with a max-age threshold so a long-stale
 *    lock from a recycled PID is reclaimable (multi-reviewer P1, Gemini #2).
 */

import { promises as fs, unlinkSync } from "node:fs";
import * as os from "node:os";
import * as process from "node:process";

import { atomicWriteFile, pathExists } from "./atomic.js";
import type { StatePaths } from "./paths.js";
import type { LockHandle } from "./types.js";

/**
 * Maximum time a lock is considered live on Windows when `kill(0)` succeeds.
 * 1 hour is generous for a CLI op (sub-second normally) but conservative
 * enough that a recycled PID from yesterday won't perpetually block.
 *
 * Not applied on POSIX where kill(0) semantics are unambiguous (ESRCH means
 * "PID definitely does not exist", and PID recycling is bounded by PID_MAX
 * which is high enough to make collisions rare in practice).
 */
const WINDOWS_LOCK_MAX_AGE_MS = 60 * 60 * 1000;

/** Bounded retry count for stale-on-stale acquire races. */
const ACQUIRE_MAX_RETRIES = 3;

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
 *
 * On Windows, `kill(0)` can be a false positive for cross-session or recycled
 * PIDs. We supplement with a max-age check: if the lock is older than
 * `WINDOWS_LOCK_MAX_AGE_MS`, we treat it as stale even when kill(0) succeeds.
 */
function isPidAlive(pid: number, timestamp: string): boolean {
  try {
    process.kill(pid, 0);
    if (process.platform === "win32") {
      const tsMs = Date.parse(timestamp);
      if (Number.isFinite(tsMs) && Date.now() - tsMs > WINDOWS_LOCK_MAX_AGE_MS) {
        return false;
      }
    }
    return true;
  } catch (err: unknown) {
    const code = (err as NodeJS.ErrnoException).code;
    if (code === "ESRCH") return false;
    if (code === "EPERM") return true;
    // Unknown — be conservative on POSIX, but on Windows fall through to the
    // age check so a wedged lock is recoverable.
    if (process.platform === "win32") {
      const tsMs = Date.parse(timestamp);
      if (Number.isFinite(tsMs) && Date.now() - tsMs > WINDOWS_LOCK_MAX_AGE_MS) {
        return false;
      }
    }
    return true;
  }
}

/**
 * Build a unique tmp path for a single acquire attempt. PID + monotonic
 * counter + random suffix gives strong uniqueness even under fork() bursts
 * where multiple children would otherwise share `process.pid`.
 */
let attemptCounter = 0;
function uniqueTmpPath(lockFile: string): string {
  const nonce = `${attemptCounter++}-${Math.random().toString(36).slice(2, 10)}`;
  return `${lockFile}.${process.pid}.${nonce}.tmp`;
}

/**
 * Acquire the project lock. Returns a `LockHandle` with idempotent release.
 * `signalHandlers` defaults to true — registers SIGINT/SIGTERM handlers that
 * release the lock and exit (R41c). Tests pass false so they can synchronize
 * on release without process-level handlers.
 */
export async function acquireLock(
  paths: StatePaths,
  opts: { signalHandlers?: boolean } = {},
): Promise<LockHandle> {
  await fs.mkdir(paths.profilesDir, { recursive: true });

  const pid = process.pid;
  let lastError: Error | undefined;
  for (let attempt = 0; attempt < ACQUIRE_MAX_RETRIES; attempt++) {
    // Pre-check: if a live holder exists, fail fast without writing.
    if (await pathExists(paths.lockFile)) {
      let raw = "";
      try {
        raw = await fs.readFile(paths.lockFile, "utf8");
      } catch (err: unknown) {
        if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
        // ENOENT race — fall through to write.
      }
      if (raw.length > 0) {
        const parsed = parseLockContents(raw);
        if (parsed && isPidAlive(parsed.pid, parsed.timestamp)) {
          throw new LockHeldError(paths.lockFile, parsed.pid, parsed.timestamp);
        }
        // Unparseable or dead PID — fall through and overwrite.
      }
    }

    const timestamp = new Date().toISOString();
    const contents = `${pid} ${timestamp}${os.EOL}`;
    const tmpPath = uniqueTmpPath(paths.lockFile);
    try {
      await atomicWriteFile(paths.lockFile, tmpPath, contents);
    } catch (err) {
      // Best-effort cleanup of our own tmp file in case write succeeded but
      // rename did not. Other tmp paths (other attempts) are not our concern.
      await fs.unlink(tmpPath).catch(() => undefined);
      throw err;
    }

    // Verify the rename actually landed our content. A racing acquirer's
    // rename may have come after ours, in which case `.lock` reflects them.
    const verifyRaw = await fs.readFile(paths.lockFile, "utf8");
    const verified = parseLockContents(verifyRaw);
    if (verified && verified.pid === pid && verified.timestamp === timestamp) {
      // We won. Build the handle.
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
          if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
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

    // Lost the race. If the winner is alive, fail fast. Otherwise retry —
    // the winner crashed before releasing.
    if (verified && isPidAlive(verified.pid, verified.timestamp)) {
      throw new LockHeldError(paths.lockFile, verified.pid, verified.timestamp);
    }
    if (!verified) {
      lastError = new LockCorruptError(
        paths.lockFile,
        "lock file content unrecognised after acquire",
      );
      continue;
    }
    // Dead PID; loop will retry the whole acquire from scratch.
    lastError = new LockHeldError(
      paths.lockFile,
      verified.pid,
      verified.timestamp,
    );
  }

  // Exhausted retries — surface the last seen error.
  throw lastError ?? new LockCorruptError(paths.lockFile, "acquire retries exhausted");
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
