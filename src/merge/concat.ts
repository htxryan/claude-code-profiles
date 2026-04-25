/**
 * R9: concat strategy for `*.md`.
 *
 * Concatenates each contributor's bytes in canonical order
 * (ancestors → includes → leaf). Each contributor's content is followed by
 * a newline if it does not already end with one — this is the only
 * "normalization" we apply. Otherwise the bytes are passed through verbatim.
 *
 * Worked example (R9): `base ← extended ← profile` with
 * `profile.includes = [compA, compB]` produces concat order
 * `base, extended, compA, compB, profile`.
 */

import type { ContributorBytes, MergeStrategy, StrategyResult } from "./types.js";

const NL = 0x0a;

export const concatStrategy: MergeStrategy = (
  relPath: string,
  inputs: ContributorBytes[],
): StrategyResult => {
  if (inputs.length === 0) {
    throw new Error(`concat invoked with no inputs for "${relPath}"`);
  }

  // Empty contributors are skipped entirely to keep `*.md` semantics aligned
  // with `settings.json`'s {} no-op: a contributor that supplied no bytes
  // should not nudge the output (no spurious blank line) and should not
  // appear in provenance.
  const nonEmpty = inputs.filter((c) => c.bytes.length > 0);

  const chunks: Buffer[] = [];
  for (let i = 0; i < nonEmpty.length; i++) {
    const buf = nonEmpty[i]!.bytes;
    chunks.push(buf);
    if (i < nonEmpty.length - 1 && buf[buf.length - 1] !== NL) {
      // Separator newline only if the preceding chunk does not already end
      // with one — avoids double-newlines for files that already terminate
      // with `\n`, while still keeping section boundaries clean otherwise.
      chunks.push(Buffer.from("\n", "utf8"));
    }
  }

  return {
    bytes: Buffer.concat(chunks),
    contributors: nonEmpty.map((c) => c.id),
  };
};
