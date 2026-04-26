/**
 * Lockfile primitive (R41 / R41a / R41b / R41c).
 *
 * Contract:
 *  - Acquire creates `.claude-profiles/.meta/lock` via `fs.open(path, 'wx')` —
 *    O_CREAT | O_EXCL atomic exclusive create. Only one process can succeed;
 *    losers see EEXIST.
 *  - If `.lock` exists with a live PID (kill -0 succeeds), abort with
 *    `LockHeldError` naming the holder.
 *  - If `.lock` exists with a dead PID, recover via rename-aside: atomically
 *    rename `.lock` to a unique side path. Only one acquirer can win the
 *    rename; others see ENOENT and retry. The winner re-confirms the side
 *    file is stale, unlinks it, and retries the exclusive create.
 *  - Release deletes the lock file (idempotent — safe to call from signal
 *    handler and from finally block).
 *  - Signal handlers (SIGINT/SIGTERM) call release synchronously and exit.
 *
 * Reads (R43) bypass the lock entirely — they're never serialized through
 * acquireLock. The lock is only acquired before mutating ops.
 *
 * Concurrency notes:
 *  - The exclusive-create primitive (`fs.open` with `'wx'`) eliminates the
 *    rename-replaces-existing TOCTOU that the previous design suffered from
 *    (Codex #1, Gemini #1). `fs.rename` overwrites; `open(wx)` does not.
 *  - Stale-lock recovery uses rename-aside: rename atomically transfers
 *    ownership of the lock path to a side file owned by the winner. After
 *    that, no other acquirer can mistakenly delete the side file because
 *    only the winner knows its name.
 *  - On Windows, `process.kill(pid, 0)` can be ambiguous for cross-session or
 *    recycled PIDs. We supplement with a max-age threshold so a long-stale
 *    lock from a recycled PID is reclaimable (multi-reviewer P1, Gemini #2).
 */

import { promises as fs, unlinkSync } from "node:fs";
import * as os from "node:os";
// Default import (NOT namespace import) — `process.on` is an EventEmitter
// method that loses `this` binding via namespace import in Node.js ESM
// (`import * as process from "node:process"` exposes process internals like
// `_events` but not the EventEmitter prototype methods). The namespace form
// happens to work in test contexts where signal handlers are disabled, but
// fails at runtime when bin.js actually tries to register a signal handler.
import process from "node:process";

import { fsyncDir } from "./atomic.js";
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
 * Build a unique side-file path used to atomically transfer ownership of a
 * stale lock file before discarding it. PID + monotonic counter + random
 * suffix gives strong uniqueness even under fork() bursts where multiple
 * children would otherwise share `process.pid`.
 */
let attemptCounter = 0;
function uniqueSidePath(lockFile: string): string {
  const nonce = `${attemptCounter++}-${Math.random().toString(36).slice(2, 10)}`;
  return `${lockFile}.stale.${process.pid}.${nonce}`;
}

/**
 * Atomically claim the lock file via O_CREAT | O_EXCL. Returns the timestamp
 * we wrote on success, or null if the file already existed (EEXIST). Other
 * filesystem errors propagate.
 */
async function tryClaimExclusive(lockFile: string): Promise<string | null> {
  const timestamp = new Date().toISOString();
  const contents = `${process.pid} ${timestamp}${os.EOL}`;
  let handle: import("node:fs").promises.FileHandle | undefined;
  try {
    handle = await fs.open(lockFile, "wx");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "EEXIST") return null;
    throw err;
  }
  try {
    await handle.writeFile(contents);
    await handle.sync();
  } catch (err) {
    // Best-effort cleanup so we don't leave a partial lock that would later
    // appear to be held by us (PID alive — confusing for stale recovery).
    try {
      await handle.close();
    } catch {
      // ignore
    }
    handle = undefined;
    await fs.unlink(lockFile).catch(() => undefined);
    throw err;
  }
  await handle.close();
  fsyncDir(lockFile);
  return timestamp;
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
  await fs.mkdir(paths.metaDir, { recursive: true });

  const pid = process.pid;
  let lastError: Error | undefined;
  for (let attempt = 0; attempt < ACQUIRE_MAX_RETRIES; attempt++) {
    // Atomic exclusive create. Only one process can succeed at any moment;
    // losers see EEXIST and fall through to liveness inspection. Replaces
    // the previous rename-based claim which had a replace-existing TOCTOU
    // (Codex #1, Gemini #1).
    const claimedAt = await tryClaimExclusive(paths.lockFile);
    if (claimedAt !== null) {
      return buildHandle(paths.lockFile, pid, claimedAt, opts);
    }

    // Lock exists. Read its contents to determine liveness.
    let raw = "";
    try {
      raw = await fs.readFile(paths.lockFile, "utf8");
    } catch (err: unknown) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") {
        // Disappeared between EEXIST and read — retry the claim.
        continue;
      }
      throw err;
    }
    const parsed = parseLockContents(raw);
    if (parsed && isPidAlive(parsed.pid, parsed.timestamp)) {
      throw new LockHeldError(paths.lockFile, parsed.pid, parsed.timestamp);
    }

    // Stale or corrupt — break it via rename-aside. The rename atomically
    // transfers ownership of the lock path to a side file owned by us; no
    // other acquirer can mistakenly delete our side file because only we
    // know its unique name. After confirming the side file is stale, we
    // unlink it and retry the exclusive create from scratch.
    const sidePath = uniqueSidePath(paths.lockFile);
    try {
      await fs.rename(paths.lockFile, sidePath);
    } catch (err: unknown) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") {
        // Someone else broke the stale lock first; retry.
        continue;
      }
      throw err;
    }

    // Re-confirm staleness from the side file (defense-in-depth: a live PID
    // could have written between our liveness check above and our rename).
    let sideRaw = "";
    try {
      sideRaw = await fs.readFile(sidePath, "utf8");
    } catch {
      // Side file vanished or unreadable — treat as stale and unlink.
    }
    const sideParsed = parseLockContents(sideRaw);
    if (sideParsed && isPidAlive(sideParsed.pid, sideParsed.timestamp)) {
      // We accidentally moved aside a LIVE lock. Try to put it back; if the
      // lock path is already occupied, the live holder lost their race and
      // we discard their bytes (acceptable — we cannot atomically rename
      // the side file back without overwriting). Throw LockHeldError.
      try {
        await fs.rename(sidePath, paths.lockFile);
      } catch {
        await fs.unlink(sidePath).catch(() => undefined);
      }
      throw new LockHeldError(
        paths.lockFile,
        sideParsed.pid,
        sideParsed.timestamp,
      );
    }
    await fs.unlink(sidePath).catch(() => undefined);

    if (!sideParsed) {
      lastError = new LockCorruptError(
        paths.lockFile,
        "lock file content unrecognised; stale recovery applied",
      );
    }
    // Loop continues — next iteration retries the exclusive create.
  }

  // Exhausted retries — surface the last seen error.
  throw lastError ?? new LockCorruptError(paths.lockFile, "acquire retries exhausted");
}

function buildHandle(
  lockFile: string,
  pid: number,
  acquiredAt: string,
  opts: { signalHandlers?: boolean },
): LockHandle {
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
      await fs.unlink(lockFile);
    } catch (err: unknown) {
      if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
    }
  };
  if (opts.signalHandlers !== false) {
    signalCleanup = registerSignalRelease(lockFile, () => {
      released = true;
    });
  }
  return {
    path: lockFile,
    pid,
    acquiredAt,
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
