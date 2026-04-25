/**
 * Atomic-rename and atomic-write primitives. Per the senior-engineer flag on
 * E3, cross-platform atomic rename is the dominant risk; centralizing it here
 * lets reconciliation, materialization, and state-write all share semantics.
 *
 * Cross-platform notes (Node ≥ 20):
 *  - POSIX (macOS/Linux): `fs.rename` calls rename(2). For files, atomic on
 *    same filesystem; replaces the destination atomically. For directories,
 *    rename() into an EXISTING directory is an error on most POSIX systems —
 *    we defend against that by renaming the existing dir out of the way first
 *    (the pending/prior protocol's whole purpose).
 *  - Windows: `fs.rename` uses MoveFileExW under the hood; Node ≥ 20 passes
 *    MOVEFILE_REPLACE_EXISTING for files. For directories, MoveFileExW does
 *    NOT support REPLACE_EXISTING — same defense (move-prior-out-of-way) is
 *    required, which makes the protocol portable by construction.
 *  - Cross-filesystem rename: ENXDEV/EXDEV. The pending/prior protocol places
 *    all staging dirs as siblings of the target inside the same FS, so this
 *    error indicates a misconfiguration (e.g. project root on a mount point
 *    with `.claude/` symlinked elsewhere). We surface the error verbatim.
 *  - fsync of parent directory: required on POSIX to make a rename durable
 *    across crashes. Node has no `fsyncDir` API; we open(O_DIRECTORY)+fsync
 *    via `fs.openSync`+`fs.fsyncSync`. No-op on Windows where directory
 *    descriptor fsync is not meaningful (NTFS journals the metadata change).
 */

import { promises as fs, openSync, fsyncSync, closeSync } from "node:fs";
import * as path from "node:path";

/**
 * fsync the directory containing `target`. Best-effort: on Windows, on
 * platforms that disallow opening directories, or when the directory has
 * already been removed by an earlier step, this swallows the error rather
 * than failing the surrounding operation. The crash-injection fitness
 * function exercises the durable case on POSIX.
 */
export function fsyncDir(target: string): void {
  const dir = path.dirname(target);
  let fd: number | undefined;
  try {
    fd = openSync(dir, "r");
    fsyncSync(fd);
  } catch {
    // Best-effort. Windows refuses to open dir for fsync; non-POSIX edge cases
    // (network mounts, certain virtualized FSes) similarly. The pending/prior
    // protocol still gives us crash-recovery; fsync just narrows the window.
  } finally {
    if (fd !== undefined) {
      try {
        closeSync(fd);
      } catch {
        // ignore
      }
    }
  }
}

/**
 * Atomic file rename. Wraps fs.rename + parent-dir fsync to make the rename
 * durable. Errors are not caught; callers want to see EXDEV / ENOENT so they
 * can respond (e.g. reconciliation distinguishes "missing prior" from "real
 * IO error").
 */
export async function atomicRename(src: string, dest: string): Promise<void> {
  await fs.rename(src, dest);
  fsyncDir(dest);
}

/**
 * Atomic file write: write to `<dest>.tmp`, fsync the file, rename to dest,
 * fsync the parent directory. Used for `.state.json` (R14a) and the lock
 * file. `tmpPath` is taken explicitly rather than computed so callers can
 * keep a stable cleanup target across retries.
 */
export async function atomicWriteFile(
  dest: string,
  tmpPath: string,
  contents: Buffer | string,
): Promise<void> {
  // Open with `w` (truncate) so a leftover tmp from a prior crashed write
  // doesn't accumulate. fsync before rename to flush bytes to disk before
  // the rename is observable.
  const handle = await fs.open(tmpPath, "w");
  try {
    if (typeof contents === "string") {
      await handle.writeFile(contents);
    } else {
      await handle.writeFile(contents);
    }
    await handle.sync();
  } finally {
    await handle.close();
  }
  await fs.rename(tmpPath, dest);
  fsyncDir(dest);
}

/**
 * Recursively remove a directory tree. Tolerates missing target. Used by
 * reconciliation (drop a stale .pending/) and by the post-success cleanup
 * (drop the rolled-aside .prior/). Background-friendly: callers may fire-
 * and-forget.
 */
export async function rmrf(target: string): Promise<void> {
  await fs.rm(target, { recursive: true, force: true });
}

/**
 * Predicate: does `target` exist? Helper for reconciliation logic where we
 * do not need stat metadata. `fs.access` throws on ENOENT; we map to bool.
 */
export async function pathExists(target: string): Promise<boolean> {
  try {
    await fs.access(target);
    return true;
  } catch {
    return false;
  }
}
