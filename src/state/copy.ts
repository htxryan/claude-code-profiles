/**
 * Cross-platform tree copy. R39 mandates copy-only on Windows (no symlinks);
 * macOS/Linux also use copy because the user-visible drift workflow relies
 * on `.claude/` being a real tree readers can stat without surprise.
 *
 * Implementation notes:
 *  - We use Node's `fs.cp` (Node ≥ 16.7 stable as of Node 20) with
 *    `recursive: true` and `force: true`. fs.cp is the maintained API; the
 *    older `fs.copyFile` only handles single files and would force us to
 *    re-implement directory walking for directory inputs.
 *  - We fan-out reads per file (Promise.all in batches) to parallelize IO;
 *    R38 budgets 1000 files in 2s on a developer laptop. fs.cp itself is
 *    sequential under the hood, so for the materialization path that writes
 *    *new bytes* (already-merged content) we use `writeFiles` which fans out.
 *  - For "raw copy of an existing tree" (persist live `.claude/` into a
 *    profile dir, discard backup snapshot) we use `fs.cp` directly — that
 *    code path doesn't have pre-merged bytes, only on-disk source files.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import type { MergedFile } from "../merge/types.js";

import { fsyncDir } from "./atomic.js";

/**
 * Recursively copy `src` directory to `dest`, creating `dest` and any parent
 * dirs. Existing files at `dest` are overwritten (force: true). The target
 * directory should be the destination *root*, not its parent.
 */
export async function copyTree(src: string, dest: string): Promise<void> {
  await fs.mkdir(path.dirname(dest), { recursive: true });
  await fs.cp(src, dest, {
    recursive: true,
    force: true,
    // We don't need symlink dereferencing — `.claude/` should not contain
    // user-created symlinks, and any that exist are preserved as-is so the
    // copy is byte-faithful.
    dereference: false,
    errorOnExist: false,
  });
}

/**
 * Write a list of MergedFile bytes into `targetDir`. Creates intermediate
 * directories as needed. Concurrency is bounded to keep file-descriptor use
 * sane on large profiles (R38 budget assumes 1000 files; default concurrency
 * 16 is well within OS fd limits).
 *
 * The returned promise resolves only after every file is durable on disk —
 * we fsync each file before close (the strategy is per-file rather than a
 * single global fsync at end, so a partial-batch crash leaves a coherent
 * subset on disk that the pending/prior protocol can either commit or
 * discard atomically).
 */
export async function writeFiles(
  targetDir: string,
  files: ReadonlyArray<MergedFile>,
  concurrency = 16,
): Promise<void> {
  await fs.mkdir(targetDir, { recursive: true });
  // Pre-create unique parent dirs once to avoid race-on-mkdir between
  // concurrent file writes that share a parent. We do this before fanning
  // out writes so the concurrency loop is a pure file-write batch.
  const parentDirs = new Set<string>();
  for (const f of files) {
    parentDirs.add(path.dirname(path.join(targetDir, f.path)));
  }
  for (const dir of parentDirs) {
    await fs.mkdir(dir, { recursive: true });
  }

  let nextIndex = 0;
  const workers: Promise<void>[] = [];
  const writeOne = async (f: MergedFile): Promise<void> => {
    const abs = path.join(targetDir, f.path);
    const handle = await fs.open(abs, "w");
    try {
      await handle.writeFile(f.bytes);
      await handle.sync();
    } finally {
      await handle.close();
    }
  };
  for (let w = 0; w < Math.min(concurrency, files.length); w++) {
    workers.push(
      (async () => {
        while (true) {
          const i = nextIndex++;
          if (i >= files.length) return;
          await writeOne(files[i]!);
        }
      })(),
    );
  }
  await Promise.all(workers);

  // fsync the target dir once at the end — POSIX best-effort to make the
  // collection of new files (and the dir entry) durable as a unit.
  fsyncDir(targetDir);
}
