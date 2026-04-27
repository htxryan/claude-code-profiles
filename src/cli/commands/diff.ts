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
import { renderUnifiedDiff } from "../preview.js";
import { assertValidProfileName, enrichMissingProfileError } from "../suggest.js";

export interface DiffEntry {
  relPath: string;
  status: "added" | "removed" | "changed";
  /** azp: byte size of this entry on the `a` side (null for `removed`). */
  bytesA: number | null;
  /** azp: byte size of this entry on the `b` side (null for `added`). */
  bytesB: number | null;
}

export interface DiffPayload {
  a: string;
  b: string;
  entries: DiffEntry[];
  totals: {
    added: number;
    removed: number;
    changed: number;
    /** azp: total bytes contributed by `added` entries (size of files only in a). */
    addedBytes: number;
    /** azp: total bytes contributed by `removed` entries (size of files only in b). */
    removedBytes: number;
    /** azp: sum of |bytesA - bytesB| across `changed` entries — the size delta magnitude. */
    changedBytes: number;
  };
}

export interface DiffOptions {
  cwd: string;
  output: OutputChannel;
  a: string;
  b: string | null;
  /** azp: when true, render unified-diff content for each `changed` entry. */
  preview?: boolean;
}

export async function runDiff(opts: DiffOptions): Promise<number> {
  const a = opts.a;
  let b = opts.b;
  // ppo: validate user-supplied names BEFORE touching the filesystem so a
  // path-traversal-shaped name fails with the standardized invalid-name
  // message rather than a vague "Profile does not exist".
  assertValidProfileName("diff", a);
  if (b !== null) assertValidProfileName("diff", b);
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
        totals: {
          added: 0,
          removed: 0,
          changed: 0,
          addedBytes: 0,
          removedBytes: 0,
          changedBytes: 0,
        },
      };
      opts.output.json(payload);
    } else {
      opts.output.print(`a=${a} b=${b}: identical (same profile)`);
    }
    return 0;
  }

  // ppo: enrich top-level typo against `a`/`b` with did-you-mean suggestions.
  // The second positional name `b` may have been auto-filled from
  // state.activeProfile above; even so, treat it as a top-level lookup —
  // a stale state could name a profile the user has since renamed, and a
  // "did you mean" still helps.
  const planA = await resolveWithSuggestions(a, opts.cwd);
  const planB = await resolveWithSuggestions(b, opts.cwd);
  const mergedA = await merge(planA);
  const mergedB = await merge(planB);

  const mapA = new Map(mergedA.map((m) => [m.path, m.bytes]));
  const mapB = new Map(mergedB.map((m) => [m.path, m.bytes]));

  const all = new Set<string>([...mapA.keys(), ...mapB.keys()]);
  const entries: DiffEntry[] = [];
  const totals = {
    added: 0,
    removed: 0,
    changed: 0,
    addedBytes: 0,
    removedBytes: 0,
    changedBytes: 0,
  };
  // azp: keep the buffers around for `--preview` rendering. We can't recompute
  // them from the entries without re-merging.
  const buffers = new Map<string, { a: Buffer | undefined; b: Buffer | undefined }>();
  for (const rel of [...all].sort()) {
    const ba = mapA.get(rel);
    const bb = mapB.get(rel);
    if (ba && !bb) {
      entries.push({ relPath: rel, status: "added", bytesA: ba.length, bytesB: null });
      totals.added++;
      totals.addedBytes += ba.length;
      buffers.set(rel, { a: ba, b: undefined });
    } else if (!ba && bb) {
      entries.push({ relPath: rel, status: "removed", bytesA: null, bytesB: bb.length });
      totals.removed++;
      totals.removedBytes += bb.length;
      buffers.set(rel, { a: undefined, b: bb });
    } else if (ba && bb && !ba.equals(bb)) {
      entries.push({ relPath: rel, status: "changed", bytesA: ba.length, bytesB: bb.length });
      totals.changed++;
      // ~bytes is the magnitude of the size delta. Two reviewers could read
      // "~310" as either "the changed files together total 310 bytes" or
      // "the changes net 310 bytes either way". We pick the latter (the
      // intuitive "magnitude of change") and document via README that the
      // number is `sum |bytesA - bytesB|`.
      totals.changedBytes += Math.abs(ba.length - bb.length);
      buffers.set(rel, { a: ba, b: bb });
    }
  }

  if (opts.output.jsonMode) {
    const payload: DiffPayload = { a, b, entries, totals };
    opts.output.json(payload);
    return 0;
  }

  if (entries.length === 0) {
    // The total file count tells the reader "they really do agree on N
    // files" — without it, "identical" is ambiguous (zero files vs.
    // matching content). Use the union size; when a == b on disk the two
    // maps have the same key set, so this collapses to either map's size.
    const filesInBoth = new Set<string>([...mapA.keys(), ...mapB.keys()]).size;
    opts.output.print(`a=${a} b=${b}: identical (${filesInBoth} files in both)`);
    return 0;
  }
  opts.output.print(
    `a=${a} b=${b}: ${entries.length} changes (${totals.added} added, ${totals.removed} removed, ${totals.changed} changed) (+${totals.addedBytes} -${totals.removedBytes} ~${totals.changedBytes} bytes)`,
  );
  for (const e of entries) {
    const sigil = e.status === "added" ? "+" : e.status === "removed" ? "-" : "~";
    // Single-char sigils today render as `  + path`; the leading two spaces
    // form the sigil "column" and keep relPath aligned across rows.
    opts.output.print(`  ${sigil} ${e.relPath}`);
    // azp: --preview attaches a unified-diff body for `changed` entries
    // (added/removed entries have no opposing buffer to diff against; the
    // byte count in the entry is sufficient signal there).
    if (opts.preview && e.status === "changed") {
      const buf = buffers.get(e.relPath);
      if (buf?.a !== undefined && buf?.b !== undefined) {
        const body = renderUnifiedDiff(buf.b, buf.a);
        for (const line of body.split("\n")) {
          opts.output.print(`      ${line}`);
        }
      }
    }
  }
  return 0;
}

async function resolveWithSuggestions(name: string, cwd: string): Promise<ResolvedPlanForDiff> {
  try {
    return await resolve(name, { projectRoot: cwd });
  } catch (err) {
    throw await enrichMissingProfileError(err, cwd, name);
  }
}

// Minimal local alias so we don't have to plumb the resolver's exported
// ResolvedPlan type through the diff command's surface (it never escapes).
type ResolvedPlanForDiff = Awaited<ReturnType<typeof resolve>>;
