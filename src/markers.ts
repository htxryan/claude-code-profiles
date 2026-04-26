/**
 * Single source of truth for the project-root `CLAUDE.md` managed-block
 * markers (cw6 / spec §12). Every consumer — `init` (this file), `validate`
 * (R44), `materialize` (R45), `drift detect` (R46) — reads regex + helpers
 * from here. There must be exactly one place this regex lives.
 *
 * Spec §12.3 canonical regex:
 *
 *     <!-- claude-profiles:v(\d+):begin([^>]*)-->([\s\S]*?)<!-- claude-profiles:v\1:end\2-->
 *
 * Capture groups:
 *   1. Version number (must match between :begin and :end via backref).
 *   2. Optional namespace tail (text between version and `-->`, e.g. " " in
 *      the canonical form; reserved for future namespacing).
 *   3. The managed body — bytes the tool owns. Non-greedy so multiple managed
 *      blocks in a single file (a future possibility) do not collapse into
 *      one match.
 *
 * v1 contract: a well-formed file contains exactly one match. Zero matches
 * means markers are absent (R44 → init). More than one match is reserved and
 * rejected as malformed by parseMarkers (spec §12.3).
 */

/**
 * Canonical marker regex from spec §12.3. Use this constant, never inline a
 * copy elsewhere — that would split source-of-truth and risk skew between
 * consumers (init writes one shape, validate looks for another). The literal
 * here is verbatim from the spec; if the spec changes, this constant changes,
 * and every consumer benefits automatically.
 */
export const MARKER_REGEX =
  /<!-- claude-profiles:v(\d+):begin([^>]*)-->([\s\S]*?)<!-- claude-profiles:v\1:end\2-->/;

/**
 * Successful parse: the file contained exactly one well-formed marker pair.
 * `before` and `after` are the bytes outside the markers (preserved on every
 * write); `section` is the body between them. Combined: `before + <begin> +
 * section + <end> + after === original content`.
 */
export interface ParseFound {
  found: true;
  before: string;
  section: string;
  after: string;
  /** Captured version integer (parsed from the `:vN:` field). */
  version: number;
}

/**
 * Failed parse. `absent` distinguishes "user has not run init yet" from
 * `malformed` — only `malformed` is reportable as a hard error in v1
 * (validate, materialize). Init treats both the same: write fresh.
 */
export interface ParseFailed {
  found: false;
  reason: "absent" | "malformed";
}

export type ParseResult = ParseFound | ParseFailed;

/**
 * Parse a CLAUDE.md (or arbitrary text) for the managed marker block.
 *
 * Why we manually verify "exactly one match" rather than rely on the regex:
 * the canonical regex without the `g` flag returns the FIRST match, which
 * makes the call site cheap, but a file with multiple `:begin`/`:end` pairs
 * would silently parse as if only the first one mattered. Spec §12.3 reserves
 * multi-block files as malformed; we check explicitly with a global re-scan
 * so that decision is made here once rather than scattered across consumers.
 *
 * Malformed cases caught:
 *   - lone `:begin` without matching `:end`
 *   - lone `:end` without matching `:begin`
 *   - version mismatch (begin v1 / end v2) — handled implicitly via the `\1`
 *     backref in the regex; the lone-marker check below catches the leftover.
 *   - more than one well-formed block.
 */
export function parseMarkers(content: string): ParseResult {
  // Single-match check: the canonical regex (no `g`) returns null if no
  // well-formed block exists.
  const m = content.match(MARKER_REGEX);
  if (m === null) {
    // Distinguish absent vs malformed: a lone marker (begin OR end) means the
    // user (or a buggy tool) wrote a partial block — we should not silently
    // treat that as "absent" because re-running init would happily append a
    // second block, producing a malformed multi-block file.
    if (/<!-- claude-profiles:v\d+:(begin|end)/.test(content)) {
      return { found: false, reason: "malformed" };
    }
    return { found: false, reason: "absent" };
  }

  // Multi-block check: spec §12.3 reserves "more than one match" as malformed
  // for v1. Use a global flag to count occurrences. We could push this into
  // the regex itself, but the cost of one extra scan is trivial vs the
  // clarity of doing the check here and explaining why.
  const globalRegex = new RegExp(MARKER_REGEX.source, "g");
  let count = 0;
  while (globalRegex.exec(content) !== null) {
    count++;
    if (count > 1) {
      return { found: false, reason: "malformed" };
    }
  }

  // Splice the file: everything before the match index is `before`; from
  // match.index + matched length onward is `after`. m.index is guaranteed
  // defined for a non-null String.match result.
  const startIdx = m.index ?? 0;
  const endIdx = startIdx + m[0].length;
  const version = Number.parseInt(m[1] ?? "1", 10);
  return {
    found: true,
    before: content.slice(0, startIdx),
    section: m[3] ?? "",
    after: content.slice(endIdx),
    version,
  };
}

/**
 * Build the canonical managed block (begin marker + self-doc comment + body +
 * end marker) for the given section bytes. Default version is 1; `version` is
 * accepted explicitly so future migrations (v2+) can produce the right shape.
 *
 * The self-documenting comment line is part of the spec §12.2 "recommended
 * form" — it tells the next human reading the file what the block is and how
 * to regenerate it, which dramatically reduces the chance of someone editing
 * between the markers and watching their changes vanish on the next `use`.
 *
 * Body format: we surround the body with newlines so the result is always
 * well-formed even when `sectionBytes` is empty (the empty-section parse-test
 * checks this round-trip).
 */
export function renderManagedBlock(sectionBytes: string, version = 1): string {
  const begin = `<!-- claude-profiles:v${version}:begin -->`;
  const end = `<!-- claude-profiles:v${version}:end -->`;
  const selfDoc =
    "<!-- Managed block. Do not edit between markers — changes are overwritten on next `claude-profiles use`. -->";
  // Always emit a leading + trailing newline around the body so the markers
  // sit on their own lines regardless of whether sectionBytes is empty,
  // newline-prefixed, etc.
  const body = sectionBytes.length === 0 ? "\n" : `\n${sectionBytes}\n`;
  return `${begin}\n${selfDoc}\n${body}${end}\n`;
}

/**
 * For init: take existing CLAUDE.md content and ensure markers are present.
 *
 *   - If markers already exist (well-formed): return input unchanged (no-op).
 *   - If markers are absent: append a fresh empty managed block at end-of-
 *     file, preserving every byte above byte-for-byte. We add a single
 *     separating newline ONLY when the existing content does not already end
 *     in a newline, so we never silently mutate the user's trailing-newline
 *     convention.
 *
 * Malformed input is treated like absent: the safest action for init is to
 * leave the broken bytes alone above and append a new block — but this case
 * really shouldn't happen in practice because validate (R44) catches
 * malformed before init can do anything wrong. Init's job here is "guarantee
 * markers exist", not "clean up after a broken upstream tool".
 */
export function injectMarkersIntoFile(content: string): string {
  const parsed = parseMarkers(content);
  if (parsed.found) {
    // Idempotent: well-formed markers already present → no-op.
    return content;
  }
  // Append a fresh empty managed block. Preserve the exact trailing-newline
  // state of `content`: if it ends in `\n` we don't double up; if it doesn't,
  // we insert a single separator so the begin marker lands on its own line.
  const separator = content.length === 0 || content.endsWith("\n") ? "" : "\n";
  return `${content}${separator}${renderManagedBlock("")}`;
}
