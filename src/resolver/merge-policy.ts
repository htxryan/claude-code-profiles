/**
 * E1 only consults merge policy to decide whether a file is *mergeable*
 * (i.e. exempt from R11 conflict detection). The actual byte-level merge is
 * E2's job. Keeping the classifier here keeps E1 self-contained for testing.
 */

export type MergePolicy = "deep-merge" | "concat" | "last-wins";

/**
 * Map a path inside `.claude/` to its merge policy.
 *
 *  - `settings.json` (any depth) → deep-merge  (R8/R12)
 *  - `*.md`                       → concat     (R9)
 *  - everything else              → last-wins  (R10)
 *
 * "last-wins" files participate in conflict detection (R11). The other two
 * are always mergeable and never conflict.
 */
export function policyFor(relPath: string): MergePolicy {
  const base = basename(relPath);
  if (base === "settings.json") return "deep-merge";
  if (relPath.toLowerCase().endsWith(".md")) return "concat";
  return "last-wins";
}

export function isMergeable(relPath: string): boolean {
  return policyFor(relPath) !== "last-wins";
}

function basename(relPath: string): string {
  const i = relPath.lastIndexOf("/");
  return i === -1 ? relPath : relPath.slice(i + 1);
}
