import { promises as fs } from "node:fs";
import * as path from "node:path";

/**
 * Recursively enumerate every regular file under `dir`, returning entries
 * relative to `dir` in lex-sorted, posix-style form. Symlinks are followed
 * for directories (a deliberate v1 choice — users may symlink components in)
 * but their leaves are returned as files.
 *
 * Returns [] if `dir` does not exist or is not a directory; callers higher
 * up are responsible for validating existence (R7).
 */
export async function walkClaudeDir(
  dir: string,
): Promise<Array<{ relPath: string; absPath: string }>> {
  let stat: Awaited<ReturnType<typeof fs.stat>>;
  try {
    stat = await fs.stat(dir);
  } catch (err) {
    const e = err as NodeJS.ErrnoException;
    if (e.code === "ENOENT") return [];
    throw err;
  }
  if (!stat.isDirectory()) return [];

  const out: Array<{ relPath: string; absPath: string }> = [];
  await walk(dir, "", out);
  out.sort((a, b) => (a.relPath < b.relPath ? -1 : a.relPath > b.relPath ? 1 : 0));
  return out;
}

async function walk(
  base: string,
  rel: string,
  out: Array<{ relPath: string; absPath: string }>,
): Promise<void> {
  const here = rel === "" ? base : path.join(base, rel);
  const entries = await fs.readdir(here, { withFileTypes: true });
  for (const entry of entries) {
    const childRel = rel === "" ? entry.name : `${rel}/${entry.name}`;
    const childAbs = path.join(here, entry.name);
    if (entry.isDirectory()) {
      await walk(base, childRel, out);
    } else if (entry.isFile() || entry.isSymbolicLink()) {
      // Follow symlink-to-file by stat'ing through; if stat fails, treat as
      // if absent (best-effort; the materializer in E3 will hard-fail later).
      if (entry.isSymbolicLink()) {
        try {
          const targetStat = await fs.stat(childAbs);
          if (!targetStat.isFile()) continue;
        } catch {
          continue;
        }
      }
      out.push({ relPath: childRel, absPath: childAbs });
    }
  }
}

export async function pathExists(p: string): Promise<boolean> {
  try {
    await fs.access(p);
    return true;
  } catch {
    return false;
  }
}

export async function isDirectory(p: string): Promise<boolean> {
  try {
    const s = await fs.stat(p);
    return s.isDirectory();
  } catch {
    return false;
  }
}
