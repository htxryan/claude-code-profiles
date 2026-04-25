/**
 * `diff <a> [<b>]` command (R32, R40). Compares the resolved file lists of
 * two profiles. With one arg, the active profile is the second operand.
 *
 * The diff is computed on the *merged* output (post-E2): the user wants to
 * know "what would actually land in `.claude/` if I switched from b to a",
 * which is byte equality after merge — not raw plan-file presence.
 *
 * Categories:
 *   - added: relPath exists in a, not in b
 *   - removed: relPath exists in b, not in a
 *   - changed: exists in both, bytes differ
 */

import { merge } from "../../merge/index.js";
import { resolve } from "../../resolver/index.js";
import { buildStatePaths, readStateFile } from "../../state/index.js";
import { CliUserError } from "../exit.js";
import type { OutputChannel } from "../output.js";

export interface DiffEntry {
  relPath: string;
  status: "added" | "removed" | "changed";
}

export interface DiffPayload {
  a: string;
  b: string;
  entries: DiffEntry[];
  totals: { added: number; removed: number; changed: number };
}

export interface DiffOptions {
  cwd: string;
  output: OutputChannel;
  a: string;
  b: string | null;
}

export async function runDiff(opts: DiffOptions): Promise<number> {
  const a = opts.a;
  let b = opts.b;
  if (b === null) {
    const { state } = await readStateFile(buildStatePaths(opts.cwd));
    if (state.activeProfile === null) {
      throw new CliUserError(
        `diff: no second profile given and no active profile to compare against`,
      );
    }
    b = state.activeProfile;
  }
  if (a === b) {
    // Trivial case — empty diff
    if (opts.output.jsonMode) {
      const payload: DiffPayload = {
        a,
        b,
        entries: [],
        totals: { added: 0, removed: 0, changed: 0 },
      };
      opts.output.json(payload);
    } else {
      opts.output.print(`a=${a} b=${b}: identical (same profile)`);
    }
    return 0;
  }

  const planA = await resolve(a, { projectRoot: opts.cwd });
  const planB = await resolve(b, { projectRoot: opts.cwd });
  const mergedA = await merge(planA);
  const mergedB = await merge(planB);

  const mapA = new Map(mergedA.map((m) => [m.path, m.bytes]));
  const mapB = new Map(mergedB.map((m) => [m.path, m.bytes]));

  const all = new Set<string>([...mapA.keys(), ...mapB.keys()]);
  const entries: DiffEntry[] = [];
  const totals = { added: 0, removed: 0, changed: 0 };
  for (const rel of [...all].sort()) {
    const ba = mapA.get(rel);
    const bb = mapB.get(rel);
    if (ba && !bb) {
      entries.push({ relPath: rel, status: "added" });
      totals.added++;
    } else if (!ba && bb) {
      entries.push({ relPath: rel, status: "removed" });
      totals.removed++;
    } else if (ba && bb && !ba.equals(bb)) {
      entries.push({ relPath: rel, status: "changed" });
      totals.changed++;
    }
  }

  if (opts.output.jsonMode) {
    const payload: DiffPayload = { a, b, entries, totals };
    opts.output.json(payload);
    return 0;
  }

  if (entries.length === 0) {
    opts.output.print(`a=${a} b=${b}: identical`);
    return 0;
  }
  opts.output.print(
    `a=${a} b=${b}: ${entries.length} change(s) (${totals.added} added, ${totals.removed} removed, ${totals.changed} changed)`,
  );
  for (const e of entries) {
    const sigil = e.status === "added" ? "+" : e.status === "removed" ? "-" : "~";
    opts.output.print(`  ${sigil} ${e.relPath}`);
  }
  return 0;
}
