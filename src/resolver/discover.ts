import { promises as fs } from "node:fs";
import * as path from "node:path";

import { buildPaths } from "./paths.js";

export interface DiscoverOptions {
  projectRoot: string;
}

/**
 * R1: enumerate profiles by scanning top-level directories of
 * `.claude-profiles/`, excluding entries beginning with `_` or `.`.
 *
 * Returns directory names (= canonical profile identifiers per R2), sorted
 * lexicographically for deterministic output.
 */
export async function listProfiles(opts: DiscoverOptions): Promise<string[]> {
  const paths = buildPaths(opts.projectRoot);
  let entries: import("node:fs").Dirent[];
  try {
    entries = await fs.readdir(paths.profilesDir, { withFileTypes: true });
  } catch (err) {
    const e = err as NodeJS.ErrnoException;
    if (e.code === "ENOENT") return [];
    throw err;
  }

  const names: string[] = [];
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    if (entry.name.startsWith("_") || entry.name.startsWith(".")) continue;
    // R2: directory name is canonical id. We additionally require the dir to
    // be reachable via stat (it may be a symlink). Existence is implied by
    // readdir; treat readable directories as profiles.
    names.push(entry.name);
  }

  names.sort();
  return names;
}

/**
 * Returns true if `.claude-profiles/<name>/` exists and is a directory.
 */
export async function profileExists(name: string, projectRoot: string): Promise<boolean> {
  const paths = buildPaths(projectRoot);
  const dir = path.join(paths.profilesDir, name);
  try {
    const s = await fs.stat(dir);
    return s.isDirectory();
  } catch {
    return false;
  }
}
