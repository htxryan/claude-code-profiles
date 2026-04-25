/**
 * `.gitignore` management for E3-owned artifacts (R15, R23a `.backup/`).
 *
 * After `claude-profiles init` the project root's `.gitignore` must list
 * `.claude/`, `.claude-profiles/.state.json`, and `.claude-profiles/.backup/`.
 * The init flow lives in E6 but the WRITER lives here so init and any other
 * caller (e.g. recovery diagnostic that detects missing entries) share a
 * single implementation.
 *
 * Idempotent: if the entries already exist (with or without trailing
 * comments, leading whitespace) we don't add duplicates. We append, never
 * rewrite — preserving user-managed gitignore content.
 */

import { promises as fs } from "node:fs";

import type { StatePaths } from "./paths.js";

/**
 * Required gitignore entries for E3 artifacts. Order is the order they're
 * appended on first write, with a leading section header so users can see
 * what added them.
 */
export const E3_GITIGNORE_ENTRIES = [
  ".claude/",
  ".claude-profiles/.state.json",
  ".claude-profiles/.lock",
  ".claude-profiles/.pending/",
  ".claude-profiles/.prior/",
  ".claude-profiles/.backup/",
] as const;

const SECTION_HEADER = "# Added by claude-profiles";

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
  const trimmed = existing.replace(/\s+$/, "");
  const sep = trimmed.length === 0 ? "" : "\n\n";
  const block = `${sep}${SECTION_HEADER}\n${toAdd.join("\n")}\n`;
  const next = trimmed + block;
  await fs.writeFile(paths.gitignoreFile, next);
  return { added: [...toAdd], created };
}
