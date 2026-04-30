/**
 * `.gitignore` management for E3-owned artifacts (R15, R23a `.backup/`).
 *
 * After `c3p init` the project root's `.gitignore` must list
 * `.claude/` and `.claude-profiles/.meta/` (the umbrella for state, lock,
 * tmp, pending, prior, backup). The init flow lives in E6 but the WRITER
 * lives here so init and any other caller (e.g. recovery diagnostic that
 * detects missing entries) share a single implementation.
 *
 * Idempotent: if the entries already exist (with or without trailing
 * comments, leading whitespace) we don't add duplicates. We append, never
 * rewrite — preserving user-managed gitignore content.
 */

import { promises as fs } from "node:fs";

import { atomicWriteFile, uniqueAtomicTmpPath } from "./atomic.js";
import type { StatePaths } from "./paths.js";

/**
 * Required gitignore entries for E3 artifacts. Order is the order they're
 * appended on first write, with a leading section header so users can see
 * what added them.
 */
export const E3_GITIGNORE_ENTRIES = [
  ".claude/",
  ".claude-profiles/.meta/",
] as const;

const SECTION_HEADER = "# Added by c3p";

/**
 * Result of an idempotent gitignore update. `added` lists the entries we
 * appended; empty array means everything was already present.
 */
export interface GitignoreUpdate {
  added: string[];
  /** True if the file did not exist before this call. */
  created: boolean;
}

/**
 * Ensure all E3 entries appear in `.gitignore`. Creates the file if missing.
 * Appends only the entries that aren't already listed (matched by exact-line
 * equality after trim — comments and indented variants don't count as
 * matches and would be flagged as missing).
 */
export async function ensureGitignoreEntries(paths: StatePaths): Promise<GitignoreUpdate> {
  let existing: string;
  let created = false;
  try {
    existing = await fs.readFile(paths.gitignoreFile, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      existing = "";
      created = true;
    } else {
      throw err;
    }
  }

  const present = new Set(
    existing
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter((l) => l.length > 0 && !l.startsWith("#")),
  );

  const toAdd = E3_GITIGNORE_ENTRIES.filter((e) => !present.has(e));
  if (toAdd.length === 0) {
    return { added: [], created };
  }

  // Append; ensure a single blank-line separator between existing content
  // and our section header so the file stays readable.
  //
  // Atomic write (multi-reviewer P2, Codex #6): use atomicWriteFile so a
  // crash mid-write doesn't truncate the user's `.gitignore`. Consistent
  // with the rest of E3's write discipline.
  //
  // Tmp staging is placed inside `.claude-profiles/.meta/tmp/` rather than
  // next to `.gitignore` (Sonnet review #4, Gemini #2): a `.gitignore.tmp` at
  // the project root would be visible in `git status` after a crash, and
  // adding `.gitignore.tmp` to the gitignore is circular (the file we're
  // writing IS the gitignore). Cross-filesystem rename is not a concern in
  // practice — `.claude-profiles/` is a sibling of `.gitignore`, both inside
  // the project.
  await fs.mkdir(paths.tmpDir, { recursive: true });
  const trimmed = existing.replace(/\s+$/, "");
  const sep = trimmed.length === 0 ? "" : "\n\n";
  const block = `${sep}${SECTION_HEADER}\n${toAdd.join("\n")}\n`;
  const next = trimmed + block;
  const tmpPath = uniqueAtomicTmpPath(paths.tmpDir, paths.gitignoreFile);
  try {
    await atomicWriteFile(paths.gitignoreFile, tmpPath, next);
  } catch (err) {
    await fs.unlink(tmpPath).catch(() => undefined);
    throw err;
  }
  return { added: [...toAdd], created };
}
