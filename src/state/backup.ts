/**
 * Discard backup snapshots (R23a). Before destroying drifted content via the
 * "discard" gate option, the materializer snapshots the live `.claude/` to
 * `.claude-profiles/.backup/<ISO-8601>/`. Retains at most 5; prunes oldest
 * first.
 *
 * Snapshots are not advertised in the CLI surface beyond a one-line
 * "(snapshot saved to ...)" notice — the user can browse the directory
 * manually if they want to restore. The directory is gitignored (R23a) so
 * the snapshots don't pollute the working tree.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { copyTree } from "./copy.js";
import type { StatePaths } from "./paths.js";
import { pathExists, rmrf } from "./atomic.js";

const MAX_RETAINED_SNAPSHOTS = 5;

/**
 * Snapshot the live `.claude/` to `.claude-profiles/.backup/<ISO>/` and
 * prune to keep at most 5. Returns the absolute path of the new snapshot
 * dir for the one-line CLI notice.
 *
 * If `.claude/` doesn't exist (NoActive state being discarded?) we return
 * null and skip — there's nothing to back up.
 */
export async function snapshotForDiscard(paths: StatePaths): Promise<string | null> {
  if (!(await pathExists(paths.claudeDir))) return null;

  await fs.mkdir(paths.backupDir, { recursive: true });
  const stamp = isoStampSafeForFs();
  const dest = path.join(paths.backupDir, stamp);

  await copyTree(paths.claudeDir, dest);
  await pruneOldSnapshots(paths.backupDir, MAX_RETAINED_SNAPSHOTS);

  return dest;
}

/**
 * Build an ISO-8601 timestamp safe to use as a directory name on all three
 * supported OSes. Windows forbids `:` in path components, so we replace
 * colons with `-`. Format: `2026-04-25T12-34-56.789Z`.
 */
function isoStampSafeForFs(): string {
  return new Date().toISOString().replace(/:/g, "-");
}

/**
 * Keep at most `keep` snapshots in `backupDir`, pruning oldest first.
 * Sorts by directory name (which is an ISO timestamp — lexical sort matches
 * chronological sort for the chosen format).
 */
async function pruneOldSnapshots(backupDir: string, keep: number): Promise<void> {
  let entries: string[];
  try {
    const dirents = await fs.readdir(backupDir, { withFileTypes: true });
    entries = dirents.filter((d) => d.isDirectory()).map((d) => d.name);
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return;
    throw err;
  }
  entries.sort(); // ascending — oldest first
  while (entries.length > keep) {
    const oldest = entries.shift()!;
    await rmrf(path.join(backupDir, oldest));
  }
}

/**
 * List current snapshots, oldest-first, for tests and CLI introspection.
 * Returns absolute paths.
 */
export async function listSnapshots(paths: StatePaths): Promise<string[]> {
  if (!(await pathExists(paths.backupDir))) return [];
  const dirents = await fs.readdir(paths.backupDir, { withFileTypes: true });
  return dirents
    .filter((d) => d.isDirectory())
    .map((d) => d.name)
    .sort()
    .map((n) => path.join(paths.backupDir, n));
}
